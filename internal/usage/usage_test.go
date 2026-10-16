package usage

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mdopp/haus/internal/usage/usagefake"
)

var day1 = time.Date(2026, 10, 16, 21, 30, 0, 0, time.UTC)

func counter(t *testing.T) (*usagefake.Fake, *Counter, *[]string) {
	t.Helper()
	fake := usagefake.New("um-token")
	t.Cleanup(fake.Close)
	c := New(fake.URL, "um-token")
	now := day1
	c.Now = func() time.Time { return now }
	var mu sync.Mutex
	logged := &[]string{}
	c.Log = func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		*logged = append(*logged, fmt.Sprintf(format, args...))
	}
	return fake, c, logged
}

// #50: counts per (app, event, UTC day), summed in memory and sent as one
// increment each on a flush; the next flush sends only what is new.
func TestCountsAggregateAndFlush(t *testing.T) {
	fake, c, _ := counter(t)
	c.Count("seite.start")
	c.Count("seite.start")
	c.Count("schalten.light.turn_off")
	c.CountIn("stimme", "erkannt")
	c.Flush(context.Background())
	want := map[usagefake.Count]int64{
		{App: "haus", Event: "seite.start", Day: "2026-10-16"}:             2,
		{App: "haus", Event: "schalten.light.turn_off", Day: "2026-10-16"}: 1,
		{App: "stimme", Event: "erkannt", Day: "2026-10-16"}:               1,
	}
	if got := fake.Counts(); !reflect.DeepEqual(got, want) {
		t.Fatalf("counts %v, want %v", got, want)
	}
	if n := len(fake.Bodies()); n != 3 {
		t.Errorf("%d requests for three counters, want one each: %v", n, fake.Bodies())
	}
	c.Flush(context.Background()) // nothing new: nothing sent
	if n := len(fake.Bodies()); n != 3 {
		t.Errorf("an empty flush sent %d more requests", n-3)
	}
	c.Count("seite.start")
	c.Flush(context.Background())
	if got := fake.Counts()[usagefake.Count{App: "haus", Event: "seite.start", Day: "2026-10-16"}]; got != 3 {
		t.Errorf("seite.start %d after a third view, want 3", got)
	}
}

// The day is UTC, as usage-metrics' window is: a count at 23:59 UTC and one
// at 00:01 are two days, whatever the house's time zone.
func TestCountsByUTCDay(t *testing.T) {
	fake, c, _ := counter(t)
	berlin, _ := time.LoadLocation("Europe/Berlin")
	now := time.Date(2026, 10, 17, 1, 59, 0, 0, berlin) // 23:59 UTC on the 16th
	c.Now = func() time.Time { return now }
	c.Count("seite.start")
	now = now.Add(2 * time.Minute)
	c.Count("seite.start")
	c.Flush(context.Background())
	got := fake.Counts()
	if got[usagefake.Count{App: "haus", Event: "seite.start", Day: "2026-10-16"}] != 1 || got[usagefake.Count{App: "haus", Event: "seite.start", Day: "2026-10-17"}] != 1 {
		t.Errorf("counts %v, want one on the 16th and one on the 17th (UTC)", got)
	}
}

// An event that is not usage-metrics' shape is never sent: no free text
// rides in an event name.
func TestBadEventsNeverLeave(t *testing.T) {
	fake, c, _ := counter(t)
	for _, e := range []string{"", "Seite.Start", "seite start", "seite/start", "anna@haus", strings.Repeat("a", 129), ".seite"} {
		c.Count(e)
		c.CountIn(e, "erkannt")
	}
	c.CountIn("Haus", "seite.start")
	c.Flush(context.Background())
	if b := fake.Bodies(); len(b) != 0 {
		t.Errorf("sent %v", b)
	}
}

// The sentence alone: whitespace collapsed, control characters gone, cut
// at 280 characters; an empty one and a bad source are not sent.
func TestMissesAreTheSentenceOnly(t *testing.T) {
	fake, c, _ := counter(t)
	c.Miss("stimme", "  Wann kommt\n\tder   Müll? ")
	c.Miss("bote", "Wann\x00 kommt der Müll?")
	c.Miss("stimme", strings.Repeat("ä", 300))
	c.Miss("stimme", " \n ")
	c.Miss("Stimme", "Licht an")
	c.Miss("", "Licht an")
	c.Flush(context.Background())
	want := []usagefake.Miss{
		{Source: "stimme", Text: "Wann kommt der Müll?", Day: "2026-10-16"},
		{Source: "bote", Text: "Wann kommt der Müll?", Day: "2026-10-16"},
		{Source: "stimme", Text: strings.Repeat("ä", 280), Day: "2026-10-16"},
	}
	if got := fake.Misses(); !reflect.DeepEqual(got, want) {
		t.Errorf("misses %q, want %q", got, want)
	}
}

// The same sentence twice is sent twice (usage-metrics counts it up); at
// most 100 sentences wait for one flush, the rest are dropped.
func TestMissesRepeatAndAreCapped(t *testing.T) {
	fake, c, _ := counter(t)
	c.Miss("stimme", "Mach das Ding")
	c.Miss("stimme", "Mach das Ding")
	for i := 0; i < 150; i++ {
		c.Miss("stimme", fmt.Sprintf("Satz %d", i))
	}
	c.Flush(context.Background())
	if n := len(fake.Misses()); n != 100 {
		t.Errorf("%d sentences sent from one flush, want the cap of 100", n)
	}
	if got := fake.Misses()[:2]; got[0].Text != "Mach das Ding" || got[1].Text != "Mach das Ding" {
		t.Errorf("the repeated sentence: %v", got)
	}
}

// usage-metrics down: that batch is dropped, one log line, not one per
// flush; after it answers again, the next failure is logged again.
func TestDownDropsAndLogsOnce(t *testing.T) {
	fake, c, logged := counter(t)
	fake.Down(true)
	c.Count("seite.start")
	c.Miss("stimme", "Mach das Ding")
	c.Flush(context.Background())
	c.Count("seite.raeume")
	c.Flush(context.Background())
	if len(*logged) != 1 {
		t.Fatalf("logged %q, want one line", *logged)
	}
	fake.Down(false)
	c.Count("seite.kalender")
	c.Flush(context.Background())
	want := map[usagefake.Count]int64{{App: "haus", Event: "seite.kalender", Day: "2026-10-16"}: 1}
	if got := fake.Counts(); !reflect.DeepEqual(got, want) {
		t.Errorf("after the outage %v, want only the new count (the old batch is dropped)", got)
	}
	if len(fake.Misses()) != 0 {
		t.Errorf("a dropped sentence arrived later: %v", fake.Misses())
	}
	fake.Down(true)
	c.Count("seite.start")
	c.Flush(context.Background())
	if len(*logged) != 2 {
		t.Errorf("logged %q, want a second line after it had answered in between", *logged)
	}
	for _, l := range *logged {
		if strings.Contains(l, "um-token") {
			t.Errorf("the token is in the log: %q", l)
		}
	}
}

// Counting never waits for usage-metrics: while a flush hangs on a slow
// server, Count returns at once.
func TestCountNeverWaits(t *testing.T) {
	release := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-release }))
	defer slow.Close()
	defer close(release)
	c := New(slow.URL, "t")
	c.Log = func(string, ...any) {}
	c.Count("seite.start")
	go c.Flush(context.Background())
	time.Sleep(50 * time.Millisecond)
	start := time.Now()
	for i := 0; i < 1000; i++ {
		c.Count("seite.start")
		c.Miss("stimme", "Licht an")
	}
	if d := time.Since(start); d > 100*time.Millisecond {
		t.Errorf("1000 counts took %v while a flush hung", d)
	}
}

// Run flushes on its own every interval, and once more when it stops.
func TestRunFlushesAndOnStop(t *testing.T) {
	fake, c, _ := counter(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx, 20*time.Millisecond); close(done) }()
	c.Count("seite.start")
	deadline := time.Now().Add(2 * time.Second)
	for len(fake.Counts()) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if len(fake.Counts()) == 0 {
		t.Fatal("Run did not flush")
	}
	c.Count("seite.box")
	cancel()
	<-done
	if fake.Counts()[usagefake.Count{App: "haus", Event: "seite.box", Day: "2026-10-16"}] != 1 {
		t.Errorf("stopping did not flush: %v", fake.Counts())
	}
}

// No usage-metrics configured: a nil counter counts nothing and is safe
// to call everywhere.
func TestNilCounter(t *testing.T) {
	var c *Counter
	c.Count("seite.start")
	c.CountIn("stimme", "erkannt")
	c.Miss("stimme", "x")
	c.Flush(context.Background())
	if _, err := c.Summary(context.Background(), 30); err == nil {
		t.Error("a nil counter's summary is no error")
	}
}

func TestFromEnv(t *testing.T) {
	for _, c := range []struct {
		url, token string
		want       bool
	}{
		{"http://host.containers.internal:8094", "tok", true},
		{"http://host.containers.internal:8094/", "tok", true},
		{"", "tok", false},
		{"http://host.containers.internal:8094", "", false},
		{"", "", false},
		{"host:8094", "tok", false},
	} {
		env := map[string]string{"HAUS_USAGE_URL": c.url, "HAUS_USAGE_TOKEN": c.token}
		got := FromEnv(func(k string) string { return env[k] })
		if (got != nil) != c.want {
			t.Errorf("FromEnv(%q, %q) = %v, want counting %v", c.url, c.token, got, c.want)
		}
		if got != nil && strings.HasSuffix(got.URL, "/") {
			t.Errorf("URL %q keeps its slash", got.URL)
		}
	}
}

// The card's reads: /summary and /misses for 30 days, with the token.
func TestReads(t *testing.T) {
	fake, c, _ := counter(t)
	fake.SetSummary(map[string]any{"days": 30, "from": "2026-09-17", "total": 5, "knownApps": []string{"haus"},
		"apps": []any{map[string]any{"app": "haus", "total": 5, "events": []any{map[string]any{"event": "seite.start", "count": 5}}}}})
	fake.SetMisses(map[string]any{"days": 30, "from": "2026-09-17", "total": 9,
		"misses": []any{map[string]any{"source": "bote", "text": "Wann kommt der Müll?", "count": 9, "lastDay": "2026-10-16"}}})
	s, err := c.Summary(context.Background(), 30)
	if err != nil {
		t.Fatal(err)
	}
	if s.Count("haus", "seite.start") != 5 || s.Count("haus", "seite.box") != 0 || !s.Known("haus") || s.Known("bote") {
		t.Errorf("summary %+v", s)
	}
	m, err := c.Misses(context.Background(), 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Misses) != 1 || m.Misses[0].Text != "Wann kommt der Müll?" || m.Misses[0].Count != 9 || m.Misses[0].Source != "bote" {
		t.Errorf("misses %+v", m)
	}
	if got := fake.Asked(); !reflect.DeepEqual(got, []string{"/summary?days=30", "/misses?days=30"}) {
		t.Errorf("asked %v", got)
	}
	fake.Down(true)
	if _, err := c.Summary(context.Background(), 30); err == nil {
		t.Error("a down usage-metrics reads as a summary")
	}
}
