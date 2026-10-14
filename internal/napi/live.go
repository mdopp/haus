package napi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/mdopp/haus/internal/ha"
)

// The realtime stream (haus#10): each paired device tells Haus which
// entities its widgets show (POST /napi/portal/watch) and holds one SSE
// stream (GET /napi/portal/events). While anyone listens, Haus holds one
// state_changed subscription to HA and sends a device a card_state frame
// for the entities it watches, and nothing else: the stream carries only
// card_state (laib ADR 0022 §2). Messages to people go through their chat
// channel, not the app (§3).

const (
	watchTTL           = time.Hour        // the app re-posts while widgets exist
	defaultSensorEvery = 10 * time.Second // a sensor card at most this often, the last value always
	defaultPing        = 90 * time.Second // ': ping' keeps proxy and carrier NAT open
	streamsPerDevice   = 3                // a fourth stream pushes out the device's oldest
)

type watchSet struct {
	ids   map[string]bool
	until time.Time
}

type listener struct {
	device string
	seq    uint64
	ch     chan []byte
	kick   chan struct{} // closed when a newer stream of the device pushes this one out
}

type live struct {
	ha    *ha.Client
	ping  time.Duration
	every time.Duration // sensor interval

	mu      sync.Mutex
	watches map[string]watchSet // device → entities
	subs    map[*listener]bool
	stop    context.CancelFunc // the HA subscription, while anyone listens
	seq     uint64
	last    map[string]string    // entity → last card sent (without updated_at_ms)
	lastAt  map[string]int64     // entity → updated_at_ms of that card: nothing older goes out
	ready   map[string]time.Time // sensor entity → earliest next send
	pending map[string]map[string]any
}

func newLive(c *ha.Client, ping, sensorEvery time.Duration) *live {
	if ping <= 0 {
		ping = defaultPing
	}
	if sensorEvery <= 0 {
		sensorEvery = defaultSensorEvery
	}
	l := &live{ha: c, ping: ping, every: sensorEvery, watches: map[string]watchSet{}, subs: map[*listener]bool{}}
	l.reset()
	return l
}

// reset forgets what was sent: after a (re)connect everything is new.
func (l *live) reset() {
	l.last, l.lastAt, l.ready, l.pending = map[string]string{}, map[string]int64{}, map[string]time.Time{}, map[string]map[string]any{}
}

func (l *live) routes(mux *http.ServeMux, auth func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("POST /napi/portal/watch", auth(l.watch))
	mux.HandleFunc("GET /napi/portal/events", auth(l.events))
}

// a device is its token; Haus keeps only a digest of it in memory
func deviceOf(r *http.Request) string {
	b, _ := bearerOf(r)
	sum := sha256.Sum256([]byte(b))
	return hex.EncodeToString(sum[:])
}

// POST /napi/portal/watch {entity_ids:[…]} → {ok}: replaces this device's
// set; 503 while HA does not answer.
func (l *live) watch(w http.ResponseWriter, r *http.Request) {
	if err := l.ha.Ping(r.Context()); err != nil {
		log.Printf("haus: watch: HA: %v", err)
		refuse(w, http.StatusServiceUnavailable, "watch_unavailable")
		return
	}
	var body struct {
		EntityIDs []any `json:"entity_ids"`
	}
	json.NewDecoder(io.LimitReader(r.Body, 256<<10)).Decode(&body)
	ids := map[string]bool{}
	for _, e := range body.EntityIDs {
		if s, ok := e.(string); ok && s != "" {
			ids[s] = true
		}
	}
	now := time.Now()
	l.mu.Lock()
	l.watches[deviceOf(r)] = watchSet{ids: ids, until: now.Add(watchTTL)}
	for d, ws := range l.watches {
		if now.After(ws.until) {
			delete(l.watches, d)
		}
	}
	l.mu.Unlock()
	answer(w, http.StatusOK, map[string]any{"ok": true})
}

// GET /napi/portal/events: text/event-stream, card_state frames and ': ping'.
func (l *live) events(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "no streaming", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	fl.Flush()
	me := &listener{device: deviceOf(r), ch: make(chan []byte, 64), kick: make(chan struct{})}
	l.join(me)
	defer l.leave(me)
	tick := time.NewTicker(l.ping)
	defer tick.Stop()
	for {
		var b []byte
		select {
		case <-r.Context().Done():
			return
		case <-me.kick:
			return
		case b = <-me.ch:
		case <-tick.C:
			b = []byte(": ping\n\n")
		}
		if _, err := w.Write(b); err != nil {
			return
		}
		fl.Flush()
	}
}

func (l *live) join(me *listener) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seq++
	me.seq = l.seq
	var mine []*listener
	for o := range l.subs {
		if o.device == me.device {
			mine = append(mine, o)
		}
	}
	if len(mine) >= streamsPerDevice {
		oldest := mine[0]
		for _, o := range mine {
			if o.seq < oldest.seq {
				oldest = o
			}
		}
		delete(l.subs, oldest)
		close(oldest.kick)
	}
	l.subs[me] = true
	if l.stop == nil {
		ctx, cancel := context.WithCancel(context.Background())
		l.stop = cancel
		go l.run(ctx)
	}
}

func (l *live) leave(me *listener) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.subs, me) // a kicked stream is gone already
	if len(l.subs) == 0 && l.stop != nil {
		l.stop()
		l.stop = nil
	}
}

// run holds the HA subscription, reconnecting with a capped backoff.
func (l *live) run(ctx context.Context) {
	backoff := 2 * time.Second
	for ctx.Err() == nil {
		l.mu.Lock()
		l.reset()
		l.mu.Unlock()
		began := time.Now()
		err := l.ha.SubscribeSynced(ctx, func() { l.resync(ctx) }, l.changed)
		if ctx.Err() != nil {
			return
		}
		log.Printf("haus: HA state_changed: %v", err)
		if time.Since(began) > time.Minute {
			backoff = 2 * time.Second
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff *= 2; backoff > time.Minute {
			backoff = time.Minute
		}
	}
}

// resync sends the watched entities' current cards once HA confirmed the
// subscription: what changed while Haus was not listening (first connect,
// an HA restart) arrives, and the deduplication drops the rest.
func (l *live) resync(ctx context.Context) {
	states, err := l.ha.States(ctx)
	if err != nil {
		log.Printf("haus: HA resync: %v", err)
		return
	}
	for _, s := range states {
		l.changed(s)
	}
}

// changed is one state_changed: drop it unless someone watches the entity
// and its card shows something new; a sensor goes out at most every
// l.every, its latest value always.
func (l *live) changed(s ha.State) {
	c := card(s)
	if c == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.watchers(s.EntityID)) == 0 {
		delete(l.last, s.EntityID)
		return
	}
	sig := map[string]any{}
	for k, v := range c {
		if k != "updated_at_ms" {
			sig[k] = v
		}
	}
	b, _ := json.Marshal(sig)
	if l.last[s.EntityID] == string(b) {
		return
	}
	if at, ok := c["updated_at_ms"].(int64); ok {
		if at < l.lastAt[s.EntityID] {
			return // older than what went out (a resync racing an event)
		}
		l.lastAt[s.EntityID] = at
	}
	l.last[s.EntityID] = string(b)
	if c["domain"] != "sensor" {
		l.send(s.EntityID, c)
		return
	}
	now := time.Now()
	if ready := l.ready[s.EntityID]; !now.Before(ready) {
		l.ready[s.EntityID] = now.Add(l.every)
		l.send(s.EntityID, c)
		return
	}
	if _, waiting := l.pending[s.EntityID]; !waiting {
		time.AfterFunc(l.ready[s.EntityID].Sub(now), func() { l.flush(s.EntityID) })
	}
	l.pending[s.EntityID] = c
}

func (l *live) flush(entity string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	c, ok := l.pending[entity]
	if !ok {
		return
	}
	delete(l.pending, entity)
	l.ready[entity] = time.Now().Add(l.every)
	l.send(entity, c)
}

// watchers are the open streams whose device watches entity (l.mu held).
func (l *live) watchers(entity string) []*listener {
	now := time.Now()
	var out []*listener
	for me := range l.subs {
		if ws, ok := l.watches[me.device]; ok && now.Before(ws.until) && ws.ids[entity] {
			out = append(out, me)
		}
	}
	return out
}

// send fans one card out (l.mu held). A stream too slow to take it loses
// the frame; the app re-reads its cards when it reconnects.
func (l *live) send(entity string, c map[string]any) {
	data, _ := json.Marshal(map[string]any{"entity_id": entity, "card": c})
	frame := []byte("event: card_state\ndata: " + string(data) + "\n\n")
	for _, me := range l.watchers(entity) {
		select {
		case me.ch <- frame:
		default:
		}
	}
}
