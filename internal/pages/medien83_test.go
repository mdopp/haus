package pages

import (
	"strings"
	"testing"

	"github.com/mdopp/haus/internal/ha/hafake"
)

// a house where Music Assistant mirrors two Chromecasts (each its own
// device in HA, the same Cast id with dashes; the Küche's mirror comes
// first), a speaker alone, two radios whose short ids say nothing, and
// two Jellyfin sessions: one a test's leftover, one someone watching
func twinPlayersHouse(t *testing.T, fake *hafake.HA, tvState, maState string) {
	t.Helper()
	fake.SetRegistries(
		[]map[string]any{{"area_id": "wohnzimmer", "name": "Wohnzimmer"}, {"area_id": "kueche", "name": "Küche"}},
		[]map[string]any{
			{"id": "dev-tv", "area_id": "wohnzimmer", "identifiers": [][]string{{"cast", "ddd3b687070104cf22c168061d9a3d13"}}},
			{"id": "dev-tv-ma", "area_id": "wohnzimmer", "identifiers": [][]string{{"music_assistant", "ddd3b687-0701-04cf-22c1-68061d9a3d13"}}},
			{"id": "dev-kueche", "area_id": "kueche", "identifiers": [][]string{{"cast", "cf400ad00cab64f2adba0627bb6683d0"}}},
			{"id": "dev-kueche-ma", "area_id": "kueche", "identifiers": [][]string{{"music_assistant", "CF400AD0-0CAB-64F2-ADBA-0627BB6683D0"}}},
			{"id": "dev-nest", "area_id": "kueche", "identifiers": [][]string{{"cast", "82956caf161f8221becc0f578d1b5ecc"}}},
			{"id": "dev-t", "identifiers": [][]string{{"jellyfin", "t"}}},
			{"id": "dev-radio1", "identifiers": [][]string{{"dlna", "1"}}}, // short: says nothing about sameness
			{"id": "dev-radio2", "identifiers": [][]string{{"upnp", "1"}}},
			{"id": "dev-handy", "identifiers": [][]string{{"jellyfin", "annas-handy"}}},
		},
		[]map[string]any{
			{"entity_id": "media_player.tv", "device_id": "dev-tv", "platform": "cast"},
			{"entity_id": "media_player.tv_2", "device_id": "dev-tv-ma", "platform": "music_assistant"},
			{"entity_id": "media_player.kuche", "device_id": "dev-kueche", "platform": "cast"},
			{"entity_id": "media_player.a_kuche", "device_id": "dev-kueche-ma", "platform": "music_assistant"},
			{"entity_id": "media_player.nest", "device_id": "dev-nest", "platform": "cast"},
			{"entity_id": "media_player.t", "device_id": "dev-t", "platform": "jellyfin"},
			{"entity_id": "media_player.radio_bad", "device_id": "dev-radio1", "platform": "dlna_dmr"},
			{"entity_id": "media_player.radio_flur", "device_id": "dev-radio2", "platform": "upnp"},
			{"entity_id": "media_player.handy", "device_id": "dev-handy", "platform": "jellyfin"},
		})
	fake.Set("media_player.tv", tvState, map[string]any{"friendly_name": "TV", "app_name": "Netflix", "supported_features": fullPlayer})
	fake.Set("media_player.tv_2", maState, map[string]any{"friendly_name": "TV", "supported_features": fullPlayer})
	fake.Set("media_player.a_kuche", "playing", map[string]any{"friendly_name": "Küche", "media_title": "Abbey Road", "supported_features": fullPlayer})
	fake.Set("media_player.kuche", "playing", map[string]any{"friendly_name": "Küche", "media_title": "Abbey Road", "supported_features": fullPlayer})
	fake.Set("media_player.nest", "idle", map[string]any{"friendly_name": "Nest", "supported_features": fullPlayer})
	fake.Set("media_player.radio_bad", "idle", map[string]any{"friendly_name": "Radio Bad"})
	fake.Set("media_player.radio_flur", "idle", map[string]any{"friendly_name": "Radio Flur"})
	fake.Set("media_player.t", "idle", map[string]any{"friendly_name": "t"})
	fake.Set("media_player.handy", "playing", map[string]any{"friendly_name": "Annas Handy", "media_title": "Tatort", "supported_features": fullPlayer})
}

// haus#83: a Chromecast that Music Assistant mirrors is one thing, so one
// row, steered through the entity that plays — Cast's, which pauses
// Netflix too, while both play; Music Assistant's while only it does. A
// Jellyfin session is not a thing in the house: not on Start or Räume,
// on Medien only while it plays.
func TestOnePlayerPerThing(t *testing.T) {
	fake, hc := house(t)
	twinPlayersHouse(t, fake, "playing", "playing")

	_, body, _ := familyPage(t, Start, StartConfig{HA: hc}, "GET", "/start", "family")
	im := section(t, body, "karte-im-haus")
	if n := strings.Count(im, "TV spielt"); n != 1 {
		t.Errorf("%d TV rows, want 1:\n%s", n, im)
	}
	if n := strings.Count(im, "Küche spielt"); n != 1 {
		t.Errorf("%d Küche rows, want 1:\n%s", n, im)
	}
	mustContain(t, im, `value="media_player.tv"`, `value="media_player.kuche"`)
	mustNotContain(t, im, "media_player.tv_2", "media_player.a_kuche", "Annas Handy", "Tatort")

	_, body, _ = familyPage(t, Medien, StartConfig{HA: hc}, "GET", "/medien", "family")
	laeuft := section(t, body, "karte-laeuft-gerade")
	mustContain(t, laeuft, `<a href="/raeume/media_player.tv">TV</a>`, `<a href="/raeume/media_player.kuche">Küche</a>`,
		`<a href="/raeume/media_player.handy">Annas Handy</a>`)
	mustNotContain(t, body, "media_player.tv_2", "media_player.a_kuche", "media_player.t\"", ">t</a>")
	mustContain(t, section(t, body, "karte-gerade-still"), "media_player.nest", "Radio Bad", "Radio Flur")

	_, body, _ = familyPage(t, Raeume, StartConfig{HA: hc}, "GET", "/raeume", "family")
	if n := strings.Count(body, ">TV</a>"); n != 1 {
		t.Errorf("%d TV tiles on Räume, want 1", n)
	}
	mustNotContain(t, body, "media_player.tv_2", "media_player.a_kuche", "Annas Handy", ">t</a>")

	// Music Assistant plays, Cast idles: steer Music Assistant's
	twinPlayersHouse(t, fake, "idle", "playing")
	_, body, _ = familyPage(t, Start, StartConfig{HA: hc}, "GET", "/start", "family")
	im = section(t, body, "karte-im-haus")
	mustContain(t, im, `value="media_player.tv_2"`)
	mustNotContain(t, im, `value="media_player.tv"`)

	// both quiet: the device's own, once
	twinPlayersHouse(t, fake, "off", "idle")
	_, body, _ = familyPage(t, Medien, StartConfig{HA: hc}, "GET", "/medien", "family")
	still := section(t, body, "karte-gerade-still")
	mustContain(t, still, `<a href="/raeume/media_player.tv">TV</a>`)
	mustNotContain(t, still, "media_player.tv_2")
}
