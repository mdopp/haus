package ha

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/haus/internal/ha/hafake"
)

func TestFromEnv(t *testing.T) {
	t.Setenv("HAUS_HA_URL", "")
	t.Setenv("HAUS_HA_TOKEN", "")
	if c, err := FromEnv(); c != nil || err != nil {
		t.Fatalf("unset: %v %v", c, err)
	}
	// the laib template always names HA's address; the token is the one step the
	// admin takes (laib secret set HAUS_HA_TOKEN). Without it the paths stay on
	// the bridge: off, not a crash loop.
	t.Setenv("HAUS_HA_URL", "http://host.containers.internal:8123")
	if c, err := FromEnv(); c != nil || err != nil {
		t.Fatalf("a URL without a token is off, not an error: %v %v", c, err)
	}
	t.Setenv("HAUS_HA_TOKEN", "llat")
	for _, bad := range []string{"ftp://host:8123", "host.containers.internal:8123", "http://"} {
		t.Setenv("HAUS_HA_URL", bad)
		if _, err := FromEnv(); err == nil {
			t.Fatalf("%q must refuse to start: http or https with a host", bad)
		}
	}
	t.Setenv("HAUS_HA_URL", "http://host.containers.internal:8123")
	c, err := FromEnv()
	if err != nil || c.URL != "http://host.containers.internal:8123" || c.Token != "llat" {
		t.Fatalf("set: %+v %v", c, err)
	}
}

func TestStateAndUnknown(t *testing.T) {
	fake := hafake.New("tok")
	defer fake.Close()
	fake.Set("light.a", "on", map[string]any{"friendly_name": "A"})
	c := &Client{URL: fake.URL, Token: "tok"}
	s, err := c.State(context.Background(), "light.a")
	if err != nil || s.State != "on" || s.Attributes["friendly_name"] != "A" || s.LastUpdated == "" {
		t.Fatalf("state: %+v %v", s, err)
	}
	if _, err := c.State(context.Background(), "light.b"); err != ErrNotFound {
		t.Fatalf("unknown: %v", err)
	}
	if _, err := (&Client{URL: fake.URL, Token: "wrong"}).State(context.Background(), "light.a"); err == nil || err == ErrNotFound {
		t.Fatalf("a wrong token is an error, not a missing entity: %v", err)
	}
}

// The registries come over the WebSocket; a big house is a big frame.
func TestAreasOverTheWebSocket(t *testing.T) {
	fake := hafake.New("tok")
	defer fake.Close()
	var entities []map[string]any
	for i := 0; i < 3000; i++ {
		entities = append(entities, map[string]any{"entity_id": fmt.Sprintf("sensor.s%04d", i), "device_id": "d1", "name": strings.Repeat("x", 20)})
	}
	entities = append(entities, map[string]any{"entity_id": "light.k", "area_id": "kueche", "device_id": "d1"})
	fake.SetRegistries([]map[string]any{{"area_id": "kueche", "name": "Küche"}, {"area_id": "bad", "name": "Bad"}},
		[]map[string]any{{"id": "d1", "area_id": "bad"}}, entities)
	areas, err := (&Client{URL: fake.URL, Token: "tok"}).Areas(context.Background())
	if err != nil || areas["light.k"] != "Küche" || areas["sensor.s2999"] != "Bad" || len(areas) != 3001 {
		t.Fatalf("areas: %d %v", len(areas), err)
	}
	if _, err := (&Client{URL: fake.URL, Token: "wrong"}).Areas(context.Background()); err == nil {
		t.Fatal("auth_invalid must be an error")
	}
}

// haus#12: the rooms, what HA hides and the house's temperature unit are
// read once and kept for a while, shared by the /napi paths and the family's
// pages. A read that never worked says so (Räume then leaves the rooms
// out); a failed refresh keeps the last good one. The unit is optional: a
// config HA does not answer costs the unit, not the rooms.
func TestCachedRegistry(t *testing.T) {
	fake := hafake.New("tok")
	defer fake.Close()
	fake.SetRegistries([]map[string]any{{"area_id": "kueche", "name": "Küche"}}, nil,
		[]map[string]any{{"entity_id": "light.k", "area_id": "kueche"},
			{"entity_id": "light.versteckt", "area_id": "kueche", "hidden_by": "user"},
			{"entity_id": "switch.identify", "entity_category": "config"},
			{"entity_id": "sensor.rssi", "entity_category": "diagnostic"},
			{"entity_id": "light.offen", "hidden_by": nil, "entity_category": nil}})
	fake.SetTemperatureUnit("°F")
	fake.RegistriesDown(true)
	c := &Client{URL: fake.URL, Token: "tok"}
	if reg, ok := c.CachedRegistry(context.Background()); ok || len(reg.Rooms) != 0 {
		t.Fatalf("a registry that never answered: %+v %v", reg, ok)
	}

	fake.RegistriesDown(false)
	c2 := &Client{URL: fake.URL, Token: "tok"}
	reg, ok := c2.CachedRegistry(context.Background())
	if !ok || reg.Rooms["light.k"] != "Küche" || reg.TempUnit != "°F" {
		t.Fatalf("first read: %+v %v", reg, ok)
	}
	for id, want := range map[string]bool{"light.versteckt": true, "switch.identify": true, "sensor.rssi": true, "light.offen": false, "light.k": false} {
		if reg.Hidden[id] != want {
			t.Errorf("hidden %s = %v, want %v", id, reg.Hidden[id], want)
		}
	}
	// a room renamed in HA is seen after the cache's minutes, not on every page
	fake.SetRegistries([]map[string]any{{"area_id": "kueche", "name": "Wohnküche"}}, nil,
		[]map[string]any{{"entity_id": "light.k", "area_id": "kueche"}})
	if reg, ok := c2.CachedRegistry(context.Background()); !ok || reg.Rooms["light.k"] != "Küche" {
		t.Fatalf("cached read: %+v %v", reg, ok)
	}

	fake.SetTemperatureUnit("")
	c3 := &Client{URL: fake.URL, Token: "tok"}
	if reg, ok := c3.CachedRegistry(context.Background()); !ok || reg.TempUnit != "" || reg.Rooms["light.k"] != "Wohnküche" {
		t.Fatalf("without /api/config: %+v %v", reg, ok)
	}
}

func TestSubscribe(t *testing.T) {
	fake := hafake.New("tok")
	defer fake.Close()
	c := &Client{URL: fake.URL, Token: "tok"}
	ctx, cancel := context.WithCancel(context.Background())
	got := make(chan State, 4)
	done := make(chan error, 1)
	go func() { done <- c.Subscribe(ctx, func(s State) { got <- s }) }()
	if !fake.WaitSubscribed(1) {
		t.Fatal("never subscribed")
	}
	fake.Change("cover.x", "open", map[string]any{"device_class": "garage"})
	select {
	case s := <-got:
		if s.EntityID != "cover.x" || s.State != "open" || s.Attributes["device_class"] != "garage" {
			t.Fatalf("event: %+v", s)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no state_changed")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Subscribe did not return on cancel")
	}
}
