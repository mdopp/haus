package pages

import (
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/mdopp/haus/internal/ha/hafake"
)

// a house with what haus#64 is about: a dimmer on and one off, a lamp
// that only switches, a blind with a position, one wired the wrong way
// round, a cover HA does not say what it is, a garage door
func dimHouse(t *testing.T, fake *hafake.HA) {
	t.Helper()
	fake.SetRegistries([]map[string]any{{"area_id": "ess", "name": "Esszimmer"}},
		[]map[string]any{{"id": "dev-bambi", "area_id": "ess"}},
		[]map[string]any{
			{"entity_id": "light.bambi", "device_id": "dev-bambi"},
			{"entity_id": "light.dimmer", "area_id": "ess"},
			{"entity_id": "light.schalter", "area_id": "ess"},
			{"entity_id": "cover.rollo", "area_id": "ess"},
			{"entity_id": "cover.esszimmer", "area_id": "ess"},
			{"entity_id": "cover.ohne", "area_id": "ess"},
			{"entity_id": "cover.garage", "area_id": "ess"},
		})
	fake.Set("light.bambi", "on", map[string]any{"friendly_name": "Bambi", "brightness": 153.0, "supported_color_modes": []any{"brightness"}})
	fake.Set("light.dimmer", "off", map[string]any{"friendly_name": "Z-Wave-Dimmer", "supported_color_modes": []any{"brightness"}})
	fake.Set("light.schalter", "on", map[string]any{"friendly_name": "Stehlampe", "supported_color_modes": []any{"onoff"}})
	fake.Set("cover.rollo", "open", map[string]any{"friendly_name": "Rollo", "device_class": "shutter", "current_position": 40.0, "supported_features": 15.0})
	fake.Set("cover.esszimmer", "open", map[string]any{"friendly_name": "Esszimmerjalousie", "device_class": "blind", "current_position": 100.0, "supported_features": 15.0})
	fake.Set("cover.ohne", "closed", map[string]any{"friendly_name": "Unbekannt zu", "current_position": 0.0, "supported_features": 15.0})
	fake.Set("cover.garage", "closed", map[string]any{"friendly_name": "Garage", "device_class": "garage", "current_position": 0.0, "supported_features": 15.0})
}

// haus#64: a dimmer has a slider 0–100 % beside its switch (a tap still
// switches), a blind with a position its slider and Hoch, Stopp, Runter;
// a lamp that only switches, a cover HA does not say what it is, and a
// garage have none of it. The slider is a plain form (light.turn_on,
// cover.set_cover_position), sent when the finger lets go.
func TestRaeumeDimsAndMovesCovers(t *testing.T) {
	fake, hc := house(t)
	dimHouse(t, fake)
	_, body, _ := familyPage(t, Raeume, StartConfig{HA: hc}, "GET", "/raeume", "family")
	ess := section(t, body, "karte-esszimmer")
	mustContain(t, ess,
		`role=switch aria-checked=true aria-label="Bambi"`,
		`<form method=post action=/schalten class=regler><input type=hidden name=geraet value="light.bambi"><input type=hidden name=tun value="light.turn_on">`,
		`type=range name=wert min=0 max=100 step=1 value=60 aria-label="Bambi Helligkeit"`, `<output>60 %</output>`,
		`name=geraet value="light.dimmer"><input type=hidden name=tun value="light.turn_on"><input type=hidden name=zurueck value="/raeume#karte-esszimmer"><span class=k>Helligkeit</span><input type=range name=wert min=0 max=100 step=1 value=0`,
		`name=geraet value="cover.rollo"><input type=hidden name=tun value="cover.set_cover_position">`, `value=40 aria-label="Rollo Position"`,
		`name=geraet value="cover.rollo"><input type=hidden name=tun value="cover.open_cover">`, "<button>Hoch</button>",
		`name=geraet value="cover.rollo"><input type=hidden name=tun value="cover.stop_cover">`, "<button>Stopp</button>",
		`name=geraet value="cover.rollo"><input type=hidden name=tun value="cover.close_cover">`, "<button>Runter</button>",
		"<noscript><button>Setzen</button></noscript>", `onchange="this.form.requestSubmit`)
	mustNotContain(t, ess, `value="light.schalter"><input type=hidden name=tun value="light.turn_on"><input type=hidden name=zurueck value="/raeume#karte-esszimmer"><span`,
		`value="cover.ohne"`, `value="cover.garage"`, "<script")
	if n := strings.Count(ess, "class=regler"); n != 4 { // Bambi, Dimmer, Rollo, Esszimmerjalousie
		t.Fatalf("%d sliders, want 4:\n%s", n, ess)
	}

	// the slider and the buttons switch through the page's rule
	h := Schalten(StartConfig{HA: hc})
	for _, f := range []url.Values{
		{"geraet": {"light.bambi"}, "tun": {"light.turn_on"}, "wert": {"40"}, "zurueck": {"/raeume#karte-esszimmer"}},
		{"geraet": {"light.dimmer"}, "tun": {"light.turn_on"}, "wert": {"0"}},
		{"geraet": {"light.bambi"}, "tun": {"light.turn_on"}},
		{"geraet": {"cover.rollo"}, "tun": {"cover.set_cover_position"}, "wert": {"75"}},
		{"geraet": {"cover.rollo"}, "tun": {"cover.open_cover"}},
		{"geraet": {"cover.rollo"}, "tun": {"cover.stop_cover"}},
		{"geraet": {"cover.garage"}, "tun": {"cover.close_cover"}}, // closing is free, even a garage
	} {
		if rec := post(t, h, "/schalten", f, "anna", nil); rec.Code != http.StatusSeeOther {
			t.Fatalf("%v → %d", f, rec.Code)
		}
	}
	var got []string
	for _, c := range fake.Calls() {
		got = append(got, c.Domain+"."+c.Service+" "+fmtData(c.Data))
	}
	want := []string{
		"light.turn_on brightness_pct:40 entity_id:light.bambi",
		"light.turn_off entity_id:light.dimmer",
		"light.turn_on entity_id:light.bambi",
		"cover.set_cover_position entity_id:cover.rollo position:75",
		"cover.open_cover entity_id:cover.rollo",
		"cover.stop_cover entity_id:cover.rollo",
		"cover.close_cover entity_id:cover.garage",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("HA got\n%v\nwant\n%v", got, want)
	}
	// a cover HA does not say what it is is not opened from a page
	if rec := post(t, h, "/schalten", url.Values{"geraet": {"cover.ohne"}, "tun": {"cover.set_cover_position"}, "wert": {"50"}}, "anna", nil); rec.Code != http.StatusForbidden {
		t.Fatalf("classless cover → %d, want 403", rec.Code)
	}
}

// fmtData is a call's data as "key:value …", by key.
func fmtData(m map[string]any) string {
	var parts []string
	for k, v := range m {
		parts = append(parts, k+":"+fmt.Sprint(v))
	}
	sort.Strings(parts)
	return strings.Join(parts, " ")
}

// haus#64: a cover wired the wrong way round (HAUS_COVER_INVERTED) is
// shown and set turned over: HA's "open at 100" is closed at 0 on the
// page, Hoch is HA's close, a position is 100 − the slider's. Every other
// cover is as HA says.
func TestInvertedCoverIsTurnedOver(t *testing.T) {
	fake, hc := house(t)
	dimHouse(t, fake)
	cfg := StartConfig{HA: hc, Inverted: InvertedFromList(" cover.esszimmer, light.bambi,kaputt")}
	if !reflect.DeepEqual(cfg.Inverted, map[string]bool{"cover.esszimmer": true}) {
		t.Fatalf("inverted %v: only a cover's id counts", cfg.Inverted)
	}
	_, body, _ := familyPage(t, Raeume, cfg, "GET", "/raeume", "family")
	ess := section(t, body, "karte-esszimmer")
	mustContain(t, ess, "Esszimmerjalousie</a> <span class=k>geschlossen</span>", `value=0 aria-label="Esszimmerjalousie Position"`,
		"Rollo</a> <span class=k>offen · 40 %</span>", `value=40 aria-label="Rollo Position"`)
	_, body, _ = familyPage(t, Ding, cfg, "GET", "/raeume/cover.esszimmer", "family")
	mustContain(t, body, "geschlossen", `value=0 aria-label="Esszimmerjalousie Position"`)

	h := Schalten(cfg)
	for _, f := range []url.Values{
		{"geraet": {"cover.esszimmer"}, "tun": {"cover.open_cover"}},
		{"geraet": {"cover.esszimmer"}, "tun": {"cover.close_cover"}},
		{"geraet": {"cover.esszimmer"}, "tun": {"cover.set_cover_position"}, "wert": {"30"}},
		{"geraet": {"cover.esszimmer"}, "tun": {"cover.stop_cover"}},
		{"geraet": {"cover.rollo"}, "tun": {"cover.open_cover"}},
	} {
		if rec := post(t, h, "/schalten", f, "anna", nil); rec.Code != http.StatusSeeOther {
			t.Fatalf("%v → %d", f, rec.Code)
		}
	}
	var got []string
	for _, c := range fake.Calls() {
		got = append(got, c.Domain+"."+c.Service+" "+fmtData(c.Data))
	}
	want := []string{
		"cover.close_cover entity_id:cover.esszimmer",
		"cover.open_cover entity_id:cover.esszimmer",
		"cover.set_cover_position entity_id:cover.esszimmer position:70",
		"cover.stop_cover entity_id:cover.esszimmer",
		"cover.open_cover entity_id:cover.rollo",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("HA got\n%v\nwant\n%v", got, want)
	}
}

// haus#64: "Weiter in Home Assistant" — Geräte links to HA's own page for
// what Haus does not do, and a thing's page to its device in HA (else
// the entity's history), from HAUS_HA_PUBLIC_URL; without it, no link.
// A cover Haus does not open says why and where it goes.
func TestGeraeteGoOnInHomeAssistant(t *testing.T) {
	fake, hc := house(t)
	dimHouse(t, fake)
	cfg := StartConfig{HA: hc, HAPublicURL: AppURL("https://home.box.test/")}
	_, body, _ := familyPage(t, Raeume, cfg, "GET", "/raeume", "family")
	mustContain(t, body, `<p class=weiter-in><a href="https://home.box.test/">In Home Assistant öffnen</a> <span class=k>Für alles, was Haus nicht kann: Automationen, Einstellungen, Verlauf.</span></p>`)
	before(t, body, "<p class=weiter-in>", "<h2 id=karte-esszimmer")
	_, body, _ = familyPage(t, Ding, cfg, "GET", "/raeume/light.bambi", "family")
	mustContain(t, body, `<a href="https://home.box.test/config/devices/device/dev-bambi">In Home Assistant öffnen</a>`,
		`value=60 aria-label="Bambi Helligkeit"`, "<button>Ausschalten</button>")
	_, body, _ = familyPage(t, Ding, cfg, "GET", "/raeume/cover.rollo", "family")
	mustContain(t, body, `<a href="https://home.box.test/history?entity_id=cover.rollo">In Home Assistant öffnen</a>`, "<button>Hoch</button>")
	_, body, _ = familyPage(t, Ding, cfg, "GET", "/raeume/cover.ohne", "family")
	mustContain(t, body, "Home Assistant sagt nicht, ob das ein Rollladen oder ein Tor ist")
	mustNotContain(t, body, "<button>Hoch</button>", "class=regler")
	_, body, _ = familyPage(t, Ding, cfg, "GET", "/raeume/cover.garage", "family")
	mustContain(t, body, "Tore und Türen öffnet Haus nicht; das geht in Home Assistant.")
	mustNotContain(t, body, "<button>Hoch</button>", "<button>Runter</button>")

	// no address, no link; HA not answering, no word of it
	_, body, _ = familyPage(t, Raeume, StartConfig{HA: hc}, "GET", "/raeume", "family")
	mustNotContain(t, body, "class=weiter-in", "In Home Assistant")
	fake.Down(true)
	_, body, _ = familyPage(t, Raeume, cfg, "GET", "/raeume", "family")
	mustNotLeak(t, body)
	for _, bad := range []string{"", "home.box.test", "javascript:alert(1)", "https://user:pw@home.box.test", "https://home.box.test/?x=1", "ftp://home.box.test"} {
		if got := AppURL(bad); got != "" {
			t.Errorf("AppURL(%q) = %q, want none", bad, got)
		}
	}
}

// haus#64: Meine Geräte says at the top how more get there, and each
// tile has its ★ to take it off again, landing back on Start.
func TestMeineGeraeteHaveTheirStar(t *testing.T) {
	fake, hc := house(t)
	dimHouse(t, fake)
	cfg := StartConfig{HA: hc, Favorites: favorites(t, `{"anna": ["light.bambi", "cover.rollo"]}`)}
	_, body := startPage(t, cfg, "family")
	g := section(t, body, "karte-meine-geraete")
	mustContain(t, g, `<p class=hinweis><a href="/raeume">Mit ☆ legst du Geräte auf die Startseite.</a></p>`,
		`<form method=post action="/raeume/light.bambi"><input type=hidden name=stern value=aus><input type=hidden name=zurueck value="/start">`,
		`aria-label="Bambi von Start nehmen">★</button>`, `aria-label="Rollo von Start nehmen">★</button>`)
	before(t, g, "hinweis", "Bambi")
	rec := post(t, Ding(cfg), "/raeume/light.bambi", url.Values{"stern": {"aus"}, "zurueck": {"/start"}}, "anna", nil)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/start" {
		t.Fatalf("unpin from Start → %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if got, _ := cfg.Favorites.For("anna"); !reflect.DeepEqual(got, []string{"cover.rollo"}) {
		t.Fatalf("anna: %v", got)
	}
}
