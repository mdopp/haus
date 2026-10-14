package napi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/mdopp/haus/internal/ha"
)

// The realtime stream (haus#10): each paired device tells Haus which
// entities its widgets show (POST /napi/portal/watch) and holds one SSE
// stream (GET /napi/portal/events). While anyone listens, Haus holds one
// state_changed subscription to HA and sends a device a card_state frame
// for the entities it watches, and nothing else.

const (
	watchTTL       = time.Hour        // the app re-posts while widgets exist
	sensorInterval = 10 * time.Second // a sensor card at most this often, the last value always
	defaultPing    = 90 * time.Second // ': ping' keeps proxy and carrier NAT open
)

type watchSet struct {
	ids   map[string]bool
	until time.Time
}

type listener struct {
	device string
	ch     chan []byte
}

type live struct {
	ha   *ha.Client
	ping time.Duration

	mu      sync.Mutex
	watches map[string]watchSet // device → entities
	subs    map[*listener]bool
	stop    context.CancelFunc   // the HA subscription, while anyone listens
	last    map[string]string    // entity → last card sent (without updated_at_ms)
	ready   map[string]time.Time // sensor entity → earliest next send
	pending map[string]map[string]any
}

func newLive(c *ha.Client, ping time.Duration) *live {
	if ping <= 0 {
		ping = defaultPing
	}
	return &live{ha: c, ping: ping, watches: map[string]watchSet{}, subs: map[*listener]bool{},
		last: map[string]string{}, ready: map[string]time.Time{}, pending: map[string]map[string]any{}}
}

func (l *live) routes(mux *http.ServeMux, auth func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("POST /napi/portal/watch", auth(l.watch))
	mux.HandleFunc("GET /napi/portal/events", auth(l.events))
}

// a device is its token; Haus keeps only a digest of it in memory
func deviceOf(r *http.Request) string {
	sum := sha256.Sum256([]byte(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")))
	return hex.EncodeToString(sum[:])
}

// POST /napi/portal/watch {entity_ids:[…]} → {ok}: replaces this device's set.
func (l *live) watch(w http.ResponseWriter, r *http.Request) {
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
	me := &listener{device: deviceOf(r), ch: make(chan []byte, 64)}
	l.join(me)
	defer l.leave(me)
	tick := time.NewTicker(l.ping)
	defer tick.Stop()
	for {
		var b []byte
		select {
		case <-r.Context().Done():
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
	delete(l.subs, me)
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
		l.last, l.ready, l.pending = map[string]string{}, map[string]time.Time{}, map[string]map[string]any{}
		l.mu.Unlock()
		began := time.Now()
		err := l.ha.Subscribe(ctx, l.changed)
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

// changed is one state_changed: drop it unless someone watches the entity
// and its card shows something new; a sensor goes out at most every
// sensorInterval, its latest value always.
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
	l.last[s.EntityID] = string(b)
	if c["domain"] != "sensor" {
		l.send(s.EntityID, c)
		return
	}
	now := time.Now()
	if ready := l.ready[s.EntityID]; !now.Before(ready) {
		l.ready[s.EntityID] = now.Add(sensorInterval)
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
	l.ready[entity] = time.Now().Add(sensorInterval)
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
