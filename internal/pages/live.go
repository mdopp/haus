package pages

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mdopp/haus/internal/cards"
	"github.com/mdopp/haus/internal/ha"
)

// LivePath is where a family page listens for changes (owner, 2026-10-24:
// push, not polling): server-sent events, one "geaendert" per thing that
// changed in Home Assistant, its entity id as data. hausScript fetches the
// page again in place when the thing is on it (or, on Start, any thing).
const LivePath = "/live"

// livePing keeps proxies and the phone's carrier from closing a quiet stream.
var livePing = 30 * time.Second

// Live is the pages' stream: one state_changed subscription to Home
// Assistant while anyone listens, each change to every listener. What
// pages show only: actuators, people; sensors change every few seconds
// and Energie asks on its own.
type Live struct {
	HA *ha.Client

	mu   sync.Mutex
	subs map[chan string]bool
	stop context.CancelFunc
}

// liveDomains are the things whose change a page follows.
func liveDomain(id string) bool {
	d, _, _ := strings.Cut(id, ".")
	return d != "sensor" && d != "binary_sensor" && (cards.Domains[d] || d == "person")
}

func (l *Live) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "Nur zum Zuhören.", http.StatusMethodNotAllowed)
		return
	}
	if r.Header.Get("Remote-User") == "" || l.HA == nil {
		http.Error(w, "Kein Strom ohne Anmeldung.", http.StatusForbidden)
		return
	}
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(": da\n\n"))
	if rc.Flush() != nil {
		return
	}
	ch := l.join()
	defer l.leave(ch)
	ping := time.NewTicker(livePing)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			w.Write([]byte(": ping\n\n"))
		case id := <-ch:
			w.Write([]byte("event: geaendert\ndata: " + id + "\n\n"))
		}
		if rc.Flush() != nil {
			return
		}
	}
}

// join adds a listener; the first starts the subscription.
func (l *Live) join() chan string {
	ch := make(chan string, 32)
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.subs == nil {
		l.subs = map[chan string]bool{}
	}
	l.subs[ch] = true
	if l.stop == nil {
		ctx, cancel := context.WithCancel(context.Background())
		l.stop = cancel
		go l.run(ctx)
	}
	return ch
}

// leave removes a listener; the last ends the subscription.
func (l *Live) leave(ch chan string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.subs, ch)
	if len(l.subs) == 0 && l.stop != nil {
		l.stop()
		l.stop = nil
	}
}

// run holds the subscription, reconnecting with a capped backoff.
func (l *Live) run(ctx context.Context) {
	backoff := 2 * time.Second
	for ctx.Err() == nil {
		err := l.HA.Subscribe(ctx, func(s ha.State) {
			if !liveDomain(s.EntityID) {
				return
			}
			l.mu.Lock()
			defer l.mu.Unlock()
			for ch := range l.subs {
				select {
				case ch <- s.EntityID:
				default: // a listener that does not read loses a change; its page still asks now and then
				}
			}
		})
		if ctx.Err() != nil {
			return
		}
		log.Printf("haus: live: HA state_changed: %v", err)
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

// liveRefresh marks a page that follows Home Assistant by push (scope
// "seite": a change of a thing on the page; "alle": any thing, as Start
// counts what is on), and asks again every n seconds as a fallback and
// for what is not Home Assistant's.
func liveRefresh(scope string, n int) string {
	return `<div hidden data-auffrischen=` + strconv.Itoa(n) + ` data-live=` + scope + `></div>`
}
