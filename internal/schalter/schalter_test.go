package schalter

import (
	"context"
	"errors"
	"testing"

	"github.com/mdopp/haus/internal/ha"
	"github.com/mdopp/haus/internal/ha/hafake"
)

func house(t *testing.T) (*hafake.HA, *ha.Client) {
	t.Helper()
	fake := hafake.New("ha-token")
	t.Cleanup(fake.Close)
	fake.Set("light.kueche", "on", map[string]any{"friendly_name": "Küche"})
	fake.Set("cover.rollo", "closed", map[string]any{"device_class": "shutter"})
	fake.Set("cover.garage", "open", map[string]any{"device_class": "garage"})
	fake.Set("lock.haustuer", "locked", nil)
	fake.Set("media_player.wohnzimmer", "playing", map[string]any{"supported_features": 1})
	return fake, &ha.Client{URL: fake.URL, Token: "ha-token"}
}

// haus#35: one rule for what Haus switches, for the widgets' /napi/ha/call
// and the family's pages alike. A media player only pauses: Start's
// "Pause" is the one thing the pages need from it, and playing a URL on
// the speakers stays off the list.
func TestOneRuleForWhatHausSwitches(t *testing.T) {
	fake, hc := house(t)
	for _, c := range []struct {
		entity, service string
		data            map[string]any
		confirmed       bool
		code            int
		reason          string
	}{
		{"light.kueche", "light.turn_off", nil, false, 0, ""},
		{"cover.rollo", "cover.open_cover", nil, false, 0, ""},
		{"media_player.wohnzimmer", "media_player.media_pause", nil, false, 0, ""},
		{"media_player.wohnzimmer", "media_player.play_media", map[string]any{"media_content_id": "http://x"}, false, 400, "unsupported_service"},
		{"media_player.wohnzimmer", "media_player.turn_off", nil, false, 400, "unsupported_service"},
		{"media_player.wohnzimmer", "media_player.volume_set", map[string]any{"volume_level": 1}, true, 400, "unsupported_service"},
		{"cover.garage", "cover.open_cover", nil, false, 403, "sensitive_action"},
		{"lock.haustuer", "lock.unlock", nil, false, 403, "sensitive_action"},
		{"lock.haustuer", "lock.lock", nil, false, 403, "sensitive_action"},
		{"button.tuer", "button.press", nil, true, 400, "unsupported_domain"},
		{"person.anna", "person.reload", nil, true, 400, "unsupported_domain"},
		{"light.kueche", "switch.turn_off", nil, false, 400, "service domain does not match entity"},
		{"light.Kueche", "light.turn_off", nil, false, 400, "invalid entity_id"},
		{"light.kueche", "light", nil, false, 400, "invalid service"},
		{"light.kueche", "light.turn_on", map[string]any{"area_id": "eg"}, false, 400, "data may not name a target"},
	} {
		before := len(fake.Calls())
		err := Do(context.Background(), hc, c.entity, c.service, c.data, c.confirmed)
		var r *Refusal
		switch {
		case c.code == 0 && err != nil:
			t.Errorf("%s %s: %v, want it done", c.service, c.entity, err)
		case c.code == 0 && len(fake.Calls()) != before+1:
			t.Errorf("%s %s: HA got no call", c.service, c.entity)
		case c.code != 0 && (!errors.As(err, &r) || r.Code != c.code || r.Reason != c.reason):
			t.Errorf("%s %s: %v, want %d %s", c.service, c.entity, err, c.code, c.reason)
		case c.code != 0 && len(fake.Calls()) != before:
			t.Errorf("%s %s: refused, yet HA got a call", c.service, c.entity)
		}
	}
	got := fake.Calls()
	if c := got[len(got)-1]; c.Domain != "media_player" || c.Service != "media_pause" || c.Data["entity_id"] != "media_player.wohnzimmer" {
		t.Fatalf("HA got %+v", c)
	}
}

// HA's own trouble is HA's error, not a refusal: the caller tells "not
// allowed" from "did not answer".
func TestHATroubleIsNotARefusal(t *testing.T) {
	fake, hc := house(t)
	fake.FailCalls(500)
	err := Do(context.Background(), hc, "light.kueche", "light.turn_off", nil, false)
	var r *Refusal
	var he *ha.Error
	if err == nil || errors.As(err, &r) || !errors.As(err, &he) {
		t.Fatalf("%v, want HA's error", err)
	}
}
