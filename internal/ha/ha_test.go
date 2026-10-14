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
	t.Setenv("HAUS_HA_URL", "http://host.containers.internal:8123")
	if _, err := FromEnv(); err == nil {
		t.Fatal("a URL without a token must refuse to start")
	}
	t.Setenv("HAUS_HA_TOKEN", "llat")
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
