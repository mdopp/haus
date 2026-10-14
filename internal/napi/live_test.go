package napi

import (
	"context"
	"testing"
	"time"

	"github.com/mdopp/haus/internal/ha"
	"github.com/mdopp/haus/internal/ha/hafake"
)

// A resync reads HA's states while events keep coming: a snapshot older
// than an event already taken does not undo it (haus#28).
func TestAnOlderSnapshotDoesNotFlipTheActiveSetBack(t *testing.T) {
	fake := hafake.New("t")
	defer fake.Close()
	fake.Set("light.flur", "off", map[string]any{"friendly_name": "Flur"}) // last_updated 2026-10-14T10:00:00.123
	l := newLive(&ha.Client{URL: fake.URL, Token: "t"}, time.Minute, time.Second)
	l.changed(ha.State{EntityID: "light.flur", State: "on", LastUpdated: "2026-10-14T10:00:05Z"})
	l.resync(context.Background())
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.on["light.flur"] {
		t.Fatal("the older snapshot switched the light off again")
	}
}

// The active set goes only to a device whose watch says active and has
// not expired (haus#28).
func TestActiveSetNotToAnExpiredWatch(t *testing.T) {
	l := newLive(nil, time.Minute, time.Second)
	now := time.Now()
	gone := &listener{device: "gone", ch: make(chan []byte, 4), kick: make(chan struct{})}
	quiet := &listener{device: "quiet", ch: make(chan []byte, 4), kick: make(chan struct{})}
	here := &listener{device: "here", ch: make(chan []byte, 4), kick: make(chan struct{})}
	l.mu.Lock()
	l.watches["gone"] = watchSet{active: true, until: now.Add(-time.Second)}
	l.watches["quiet"] = watchSet{until: now.Add(time.Hour)}
	l.watches["here"] = watchSet{active: true, until: now.Add(time.Hour)}
	l.subs[gone], l.subs[quiet], l.subs[here] = true, true, true
	l.on["light.flur"] = true
	l.activeOut()
	l.mu.Unlock()
	if len(gone.ch) != 0 || len(quiet.ch) != 0 {
		t.Fatalf("an expired or inactive watch got the set: %d %d", len(gone.ch), len(quiet.ch))
	}
	if len(here.ch) != 1 {
		t.Fatalf("the active watch got %d frames", len(here.ch))
	}
}
