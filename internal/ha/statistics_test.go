package ha

import (
	"context"
	"testing"
	"time"

	"github.com/mdopp/haus/internal/ha/hafake"
)

// haus#65: the long-term statistics over the WebSocket — what each id
// added per day (HA's own days, Europe/Berlin), oldest first, an id
// without statistics absent; the command asks for the change in kWh; a
// recorder that refuses is an error, not an empty history.
func TestStatistics(t *testing.T) {
	fake := hafake.New("tok")
	defer fake.Close()
	c := &Client{URL: fake.URL, Token: "tok"}
	loc, _ := time.LoadLocation("Europe/Berlin")
	d := func(day, hour int) time.Time { return time.Date(2026, 10, day, hour, 0, 0, 0, loc) }
	fake.SetStatistics("sensor.solar", []hafake.Stat{{Start: d(13, 10), Change: 4}, {Start: d(12, 9), Change: 1}, {Start: d(13, 0), Change: 0.5}, {Start: d(14, 23), Change: 9}})
	got, err := c.Statistics(context.Background(), []string{"sensor.solar", "sensor.leer"}, d(12, 0), d(14, 0), "day")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["sensor.leer"]; ok || len(got) != 1 {
		t.Fatalf("got %v, want only sensor.solar", got)
	}
	s := got["sensor.solar"]
	if len(s) != 2 || !s[0].Start.Equal(d(12, 0)) || s[0].Change != 1 || !s[1].Start.Equal(d(13, 0)) || s[1].Change != 4.5 {
		t.Fatalf("solar %v, want 12th 1, 13th 4.5", s)
	}
	asked := fake.StatisticsAsked()
	if len(asked) != 1 || asked[0]["period"] != "day" || asked[0]["start_time"] != "2026-10-11T22:00:00Z" {
		t.Fatalf("asked %v", asked)
	}
	if types, _ := asked[0]["types"].([]any); len(types) != 1 || types[0] != "change" {
		t.Fatalf("types %v, want change", asked[0]["types"])
	}
	if _, err := c.Statistics(context.Background(), []string{"sensor.solar"}, d(12, 0), d(14, 0), "year"); err == nil {
		t.Fatal("a period HA does not know must not be asked")
	}
	if _, err := c.Statistics(context.Background(), []string{"sensor.x; drop"}, d(12, 0), d(14, 0), "day"); err == nil {
		t.Fatal("an id that is no entity id must not be asked")
	}
	fake.StatisticsDown(true)
	if _, err := c.Statistics(context.Background(), []string{"sensor.solar"}, d(12, 0), d(14, 0), "day"); err == nil {
		t.Fatal("a refusing recorder is an error")
	}
}

// HA before 2023.3 sent a period's start as an ISO time; both read.
func TestStatTime(t *testing.T) {
	for _, raw := range []string{`1760392800000`, `1760392800000.0`, `"2025-10-13T22:00:00+00:00"`} {
		at, ok := statTime([]byte(raw))
		if !ok || !at.Equal(time.Date(2025, 10, 13, 22, 0, 0, 0, time.UTC)) {
			t.Errorf("%s → %v %v", raw, at, ok)
		}
	}
}
