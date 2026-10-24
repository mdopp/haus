package pages

import (
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/mdopp/haus/internal/ha/hafake"
)

// a house with three rooms (Bad has nothing to show) and a lamp in no room
func rooms(t *testing.T, fake *hafake.HA) {
	t.Helper()
	fake.SetRegistries(
		[]map[string]any{{"area_id": "kueche", "name": "Küche"}, {"area_id": "wohnzimmer", "name": "Wohnzimmer"}, {"area_id": "bad", "name": "Bad"}},
		[]map[string]any{{"id": "dev-steh", "area_id": "wohnzimmer"}},
		[]map[string]any{
			{"entity_id": "light.kueche_decke", "area_id": "kueche"},
			{"entity_id": "cover.rollo_kueche", "area_id": "kueche"},
			{"entity_id": "switch.kaffee", "area_id": "kueche"},
			{"entity_id": "sensor.kueche_temp", "area_id": "kueche"},
			{"entity_id": "light.eg", "area_id": "kueche"},
			{"entity_id": "light.stehlampe", "device_id": "dev-steh"},
			{"entity_id": "climate.wohnzimmer", "area_id": "wohnzimmer"},
			{"entity_id": "media_player.wz", "area_id": "wohnzimmer"},
			{"entity_id": "sensor.bad_feuchte", "area_id": "bad"},
		})
	fake.Set("light.kueche_decke", "on", map[string]any{"friendly_name": "Deckenlampe", "brightness": 153})
	fake.Set("cover.rollo_kueche", "open", map[string]any{"friendly_name": "Rollo Küche", "current_position": 40})
	fake.Set("switch.kaffee", "on", map[string]any{"friendly_name": "Kaffeemaschine"})
	fake.Set("sensor.kueche_temp", "21.3", map[string]any{"friendly_name": "Küchentemperatur"})
	fake.Set("light.eg", "on", map[string]any{"friendly_name": "Erdgeschoss", "entity_id": []any{"light.kueche_decke"}})
	fake.Set("light.stehlampe", "off", map[string]any{"friendly_name": "Stehlampe"})
	fake.Set("climate.wohnzimmer", "heat", map[string]any{"friendly_name": "Heizung Wohnzimmer", "current_temperature": 21.5, "temperature": 22})
	fake.Set("media_player.wz", "playing", map[string]any{"friendly_name": "Sonos", "media_title": "Abbey Road", "media_artist": "The Beatles"})
	fake.Set("sensor.bad_feuchte", "61", map[string]any{"friendly_name": "Feuchte"})
	fake.Set("light.keller", "on", map[string]any{"friendly_name": "Kellerlicht"})
}

// haus#12: Räume — one card per HA area with its lights, heating, covers
// and media, the area from the entity or else its device; what has no room
// in one "Ohne Raum" card at the end. What switches is in
// TestRaeumeSwitchesLampsInPlace.
func TestRaeumeOneCardPerRoom(t *testing.T) {
	fake, hc := house(t)
	rooms(t, fake)
	code, body, _ := familyPage(t, Raeume, StartConfig{HA: hc}, "GET", "/raeume", "family")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	mustContain(t, body,
		"<h2 id=karte-kueche>Küche</h2>", "<h2 id=karte-wohnzimmer>Wohnzimmer</h2>", "<h2 id=karte-ohne-raum>Ohne Raum</h2>",
		"Deckenlampe", "an · 60 %",
		"Rollo Küche", "offen · 40 %",
		"Stehlampe", "aus",
		"Heizung Wohnzimmer", "21,5 °C · Soll 22 °C",
		"Sonos", "Abbey Road · The Beatles",
		"Kellerlicht")
	// each thing leads to its own page (haus#28)
	mustContain(t, body, `<a href="/raeume/light.kueche_decke">Deckenlampe</a>`, `<a href="/raeume/media_player.wz">Sonos</a>`)
	mustNotContain(t, body, "Bad</h2>", "Kaffeemaschine", "Küchentemperatur", "Feuchte", "Erdgeschoss", "<script>alert")
	// rooms by name, Ohne Raum last
	before(t, body, "karte-kueche", "karte-wohnzimmer")
	before(t, body, "karte-wohnzimmer", "karte-ohne-raum")
	// each thing in its own room
	before(t, body, "karte-kueche", "Deckenlampe")
	before(t, body, "Rollo Küche", "karte-wohnzimmer")
	before(t, body, "karte-wohnzimmer", "Stehlampe")
	before(t, body, "karte-laeuft-gerade", "karte-kueche") // what plays is on top, once (owner 2026-10-24)
	before(t, body, "Sonos", "karte-kueche")
	before(t, body, "karte-ohne-raum", "Kellerlicht")
	// within a room: lights, heating, covers, media
	before(t, body, "Deckenlampe", "Rollo Küche")
	before(t, body, "Stehlampe", "Heizung Wohnzimmer")
	if strings.Count(body, ">Sonos</a>") != 1 {
		t.Error("the playing Sonos twice")
	}
}

// The states every kind says, the family's words: a cover closed, a
// heating off, a player paused, a lamp HA cannot reach.
func TestRaeumeSaysStatesInWords(t *testing.T) {
	fake, hc := house(t)
	fake.Set("cover.garage", "closed", map[string]any{"friendly_name": "Garagentor"})
	fake.Set("cover.markise", "opening", map[string]any{"friendly_name": "Markise"})
	fake.Set("climate.bad", "off", map[string]any{"friendly_name": "Heizung Bad", "current_temperature": 19})
	fake.Set("media_player.kueche", "paused", map[string]any{"friendly_name": "Küchenradio", "media_title": "Deutschlandfunk"})
	fake.Set("media_player.tv", "off", map[string]any{"friendly_name": "Fernseher"})
	fake.Set("light.garten", "unavailable", map[string]any{"friendly_name": "Gartenlicht"})
	_, body, _ := familyPage(t, Raeume, StartConfig{HA: hc}, "GET", "/raeume", "")
	mustContain(t, body, "Garagentor", "geschlossen", "Markise", "öffnet",
		"Heizung Bad", "aus · 19 °C", "Küchenradio", "pausiert", "Fernseher",
		"Gartenlicht", "nicht erreichbar")
}

// A room's name and an entity's name are text, never markup.
func TestRaeumeEscapes(t *testing.T) {
	fake, hc := house(t)
	fake.SetRegistries([]map[string]any{{"area_id": "x", "name": "<b>Saal</b>"}}, nil,
		[]map[string]any{{"entity_id": "light.x", "area_id": "x"}})
	fake.Set("light.x", "on", map[string]any{"friendly_name": "<img src=x onerror=alert(1)>"})
	_, body, _ := familyPage(t, Raeume, StartConfig{HA: hc}, "GET", "/raeume", "")
	mustContain(t, body, "&lt;b&gt;Saal&lt;/b&gt;", "&lt;img src=x")
	mustNotContain(t, body, "<b>Saal", "<img src=x")
	// the ☆'s label for a screen reader too (haus#12)
	_, body, _ = familyPageAs(t, Raeume, StartConfig{HA: hc, Favorites: favorites(t, "")}, "GET", "/raeume", "anna", "family")
	mustContain(t, body, `aria-label="&lt;img src=x onerror=alert(1)&gt; auf Start zeigen"`)
	mustNotContain(t, body, "<b>Saal", "<img src=x")
}

// A big room without a name is cut, the count is not.
func TestRaeumeCutsLongRooms(t *testing.T) {
	fake, hc := house(t)
	for i := 0; i < 50; i++ {
		fake.Set(fmt.Sprintf("light.l%02d", i), "off", map[string]any{"friendly_name": fmt.Sprintf("Lampe %02d", i)})
	}
	_, body, _ := familyPage(t, Raeume, StartConfig{HA: hc}, "GET", "/raeume", "")
	mustContain(t, body, "Lampe 00", "Lampe 39", "und 10 weitere")
	mustNotContain(t, body, "Lampe 40", "Lampe 49")
}

// HA in trouble, or its rooms unreadable, leaves the rooms out — no card
// that puts every lamp in "Ohne Raum" because the registry did not answer,
// and no error on the page. The log has it. Each says so in one plain
// line, HA not answering in Start's words (haus#33).
func TestRaeumeSaysPlainlyWhatDoesNotAnswer(t *testing.T) {
	fake, hc := house(t)
	rooms(t, fake)
	fake.RegistriesDown(true)
	code, body, _ := familyPage(t, Raeume, StartConfig{HA: hc}, "GET", "/raeume", "")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	mustNotContain(t, body, "karte-", "Ohne Raum", "Deckenlampe", "registry", "class=rot")
	// one plain line instead of an empty page
	mustContain(t, body, "Die Räume werden gerade gelesen — gleich nochmal versuchen.")

	fake2, hc2 := house(t)
	rooms(t, fake2)
	fake2.Down(true)
	code, body, _ = familyPage(t, Raeume, StartConfig{HA: hc2}, "GET", "/raeume", "")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	mustNotContain(t, body, "karte-", "boom", "HA 500", "class=rot", "gerade gelesen")
	if !strings.Contains(body, "<h1") {
		t.Fatal("the page still comes")
	}
	if n := strings.Count(body, "<p class=k>Licht und Geräte antworten gerade nicht.</p>"); n != 1 {
		t.Fatalf("HA not answering said %d times, want once:\n%s", n, body)
	}
	mustNotLeak(t, body, "Deckenlampe", host(hc2.URL))
}

// The temperature's unit is Home Assistant's: the entity's own
// temperature_unit, else the house's unit system; °C only when HA says
// neither.
func TestRaeumeTemperatureUnitIsHAs(t *testing.T) {
	fake, hc := house(t)
	fake.SetTemperatureUnit("°F")
	fake.Set("climate.wz", "heat", map[string]any{"friendly_name": "Heizung", "current_temperature": 70.5, "temperature": 72})
	fake.Set("climate.bad", "heat", map[string]any{"friendly_name": "Bad", "current_temperature": 21, "temperature": 22, "temperature_unit": "°C"})
	_, body, _ := familyPage(t, Raeume, StartConfig{HA: hc}, "GET", "/raeume", "")
	mustContain(t, body, "70,5 °F · Soll 72 °F", "21 °C · Soll 22 °C")

	fake2, hc2 := house(t)
	fake2.SetTemperatureUnit("") // /api/config does not answer
	fake2.Set("climate.wz", "heat", map[string]any{"friendly_name": "Heizung", "current_temperature": 21.5, "temperature": 22})
	_, body, _ = familyPage(t, Raeume, StartConfig{HA: hc2}, "GET", "/raeume", "")
	mustContain(t, body, "21,5 °C · Soll 22 °C")
}

// What HA hides — hidden_by set, or a config or diagnostic entity — is
// not on the family's page either.
func TestRaeumeLeavesOutWhatHAHides(t *testing.T) {
	fake, hc := house(t)
	fake.SetRegistries([]map[string]any{{"area_id": "kueche", "name": "Küche"}}, nil,
		[]map[string]any{
			{"entity_id": "light.decke", "area_id": "kueche"},
			{"entity_id": "light.alt", "area_id": "kueche", "hidden_by": "user"},
			{"entity_id": "light.status_led", "area_id": "kueche", "entity_category": "config"},
			{"entity_id": "media_player.diag", "entity_category": "diagnostic"},
			{"entity_id": "cover.rollo", "hidden_by": "integration"},
		})
	fake.Set("light.decke", "on", map[string]any{"friendly_name": "Deckenlampe"})
	fake.Set("light.alt", "on", map[string]any{"friendly_name": "Alte Lampe"})
	fake.Set("light.status_led", "on", map[string]any{"friendly_name": "Status-LED"})
	fake.Set("media_player.diag", "playing", map[string]any{"friendly_name": "Diagnoseplayer"})
	fake.Set("cover.rollo", "open", map[string]any{"friendly_name": "Verstecktes Rollo"})
	_, body, _ := familyPage(t, Raeume, StartConfig{HA: hc}, "GET", "/raeume", "")
	mustContain(t, body, "Deckenlampe")
	mustNotContain(t, body, "Alte Lampe", "Status-LED", "Diagnoseplayer", "Verstecktes Rollo", "Ohne Raum")
}

// haus#12: a lamp is switched right in Räume — a switch on its row, the
// same /schalten form the thing's own page and Start use, landing back on
// the lamp's room. Only what a page switches has one: a lamp HA reaches,
// not a group, a cover, a heating or a player.
func TestRaeumeSwitchesLampsInPlace(t *testing.T) {
	fake, hc := house(t)
	rooms(t, fake)
	fake.Set("light.garten", "unavailable", map[string]any{"friendly_name": "Gartenlicht"})
	_, body, _ := familyPage(t, Raeume, StartConfig{HA: hc}, "GET", "/raeume", "family")
	kueche, wz, ohne := section(t, body, "karte-kueche"), section(t, body, "karte-wohnzimmer"), section(t, body, "karte-ohne-raum")
	mustContain(t, kueche, `<form method=post action=/schalten>`, `name=geraet value="light.kueche_decke"`, `name=tun value="light.turn_off"`,
		`name=zurueck value="/raeume#karte-kueche"`, `role=switch aria-checked=true aria-label="Deckenlampe"`)
	mustContain(t, wz, `name=geraet value="light.stehlampe"`, `name=tun value="light.turn_on"`, `name=zurueck value="/raeume#karte-wohnzimmer"`,
		`role=switch aria-checked=false aria-label="Stehlampe"`)
	mustContain(t, ohne, `name=geraet value="light.keller"`, `name=zurueck value="/raeume#karte-ohne-raum"`)
	// one switch a lamp, nothing for the rest; the dimmer's slider is
	// TestRaeumeDimsAndMovesCovers'
	if n := strings.Count(body, "role=switch"); n != 3 {
		t.Fatalf("%d switches, want 3 (Deckenlampe, Stehlampe, Kellerlicht):\n%s", n, body)
	}
	if n := strings.Count(body, "action=/schalten"); n != 4 {
		t.Fatalf("%d forms, want 4 (three switches, Deckenlampe's slider):\n%s", n, body)
	}
	mustNotContain(t, body, `value="cover.`, `value="climate.`, `value="media_player.`, `value="light.eg"`, `value="light.garten"`,
		"<script>alert")
	// HA not answering: no switch at all
	fake.Down(true)
	_, body, _ = familyPage(t, Raeume, StartConfig{HA: hc}, "GET", "/raeume", "family")
	mustNotContain(t, body, "<form", "role=switch")
}

// haus#12: the ☆ that puts a thing on the person's Start is right in
// Räume, on every thing's row, the same form the thing's own page sends;
// it lands back on the thing's room, and the row then shows ★. One line
// says what ☆ does.
func TestRaeumePinsFromTheList(t *testing.T) {
	fake, hc := house(t)
	rooms(t, fake)
	cfg := StartConfig{HA: hc, Favorites: favorites(t, `{"anna": ["climate.wohnzimmer"], "ben": ["light.keller"]}`)}
	_, body, _ := familyPage(t, Raeume, cfg, "GET", "/raeume", "family")
	mustContain(t, body, "Mit ☆ steht ein Gerät auf Start unter „Meine Geräte“.")
	kueche, wz := section(t, body, "karte-kueche"), section(t, body, "karte-wohnzimmer")
	mustContain(t, kueche, `<form method=post action="/raeume/light.kueche_decke">`, `name=stern value=an`,
		`name=zurueck value="/raeume#karte-kueche"`, `aria-label="Deckenlampe auf Start zeigen">☆</button>`,
		`<form method=post action="/raeume/cover.rollo_kueche">`)
	mustContain(t, wz, `<form method=post action="/raeume/climate.wohnzimmer">`, `name=stern value=aus`,
		`aria-label="Heizung Wohnzimmer von Start nehmen">★</button>`)
	mustContain(t, section(t, body, "karte-laeuft-gerade"), `<form method=post action="/raeume/media_player.wz">`) // playing: on top, with its ☆
	// one ☆ a thing: Deckenlampe, Rollo, Stehlampe, Heizung, Sonos, Kellerlicht
	if n := strings.Count(body, "name=stern"); n != 6 {
		t.Fatalf("%d stars, want 6:\n%s", n, body)
	}
	// ben's pin is not anna's
	mustNotContain(t, section(t, body, "karte-ohne-raum"), "★")
	before(t, body, "Mit ☆", "karte-kueche")

	// pinning from Räume lands back on the room
	rec := post(t, Ding(cfg), "/raeume/light.kueche_decke", url.Values{"stern": {"an"}, "zurueck": {"/raeume#karte-kueche"}}, "anna", nil)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/raeume#karte-kueche" {
		t.Fatalf("pin from Räume → %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if got, _ := cfg.Favorites.For("anna"); !reflect.DeepEqual(got, []string{"climate.wohnzimmer", "light.kueche_decke"}) {
		t.Fatalf("anna: %v", got)
	}
	_, body, _ = familyPage(t, Raeume, cfg, "GET", "/raeume", "family")
	mustContain(t, section(t, body, "karte-kueche"), `aria-label="Deckenlampe von Start nehmen">★</button>`)
	rec = post(t, Ding(cfg), "/raeume/climate.wohnzimmer", url.Values{"stern": {"aus"}, "zurueck": {"/raeume#karte-wohnzimmer"}}, "anna", nil)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/raeume#karte-wohnzimmer" {
		t.Fatalf("unpin from Räume → %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if got, _ := cfg.Favorites.For("anna"); !reflect.DeepEqual(got, []string{"light.kueche_decke"}) {
		t.Fatalf("anna after unpinning: %v", got)
	}
	// back leads only to Räume or the thing's own page
	for _, back := range []string{"https://boese.example/", "//boese.example", "/box", "/raeume/../box", "/raeume#x", `/raeume#karte-"><b>`,
		"/raeume?x", "/raeume/light.keller", "javascript:alert(1)",
		"https://boese.example/raeume", "//boese.example/raeume#karte-kueche", `/\boese.example/raeume`, "javascript:/raeume"} {
		rec := post(t, Ding(cfg), "/raeume/light.kueche_decke", url.Values{"stern": {"an"}, "zurueck": {back}}, "anna", nil)
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/raeume/light.kueche_decke" {
			t.Errorf("zurueck %q → %d %q, want the thing's page", back, rec.Code, rec.Header().Get("Location"))
		}
	}
	if len(fake.Calls()) != 0 {
		t.Fatalf("pinning switched something: %v", calls(fake))
	}
}

// No ☆ in Räume without a list to pin to: no store, nobody asking. A
// favorites file that does not read says so once, the switches stay.
func TestRaeumePinsOnlyWhatItCan(t *testing.T) {
	fake, hc := house(t)
	rooms(t, fake)
	for _, c := range []struct {
		cfg  StartConfig
		user string
	}{{StartConfig{HA: hc}, "anna"}, {StartConfig{HA: hc, Favorites: favorites(t, "")}, ""}} {
		_, body, _ := familyPageAs(t, Raeume, c.cfg, "GET", "/raeume", c.user, "family")
		mustNotContain(t, body, "☆", "★", "name=stern")
		mustContain(t, body, "role=switch")
	}
	_, body, _ := familyPage(t, Raeume, StartConfig{HA: hc, Favorites: favorites(t, `{"anna": [`)}, "GET", "/raeume", "family")
	mustNotContain(t, body, "☆", "★", "name=stern")
	if n := strings.Count(body, "Deine Favoriten lassen sich gerade nicht lesen."); n != 1 {
		t.Fatalf("the broken file said %d times, want once:\n%s", n, body)
	}
	mustContain(t, body, "role=switch", "Deckenlampe")
	mustNotLeak(t, body, "favoriten.json", "unexpected", "JSON")
}

// A room whose name has no letter a card id keeps ("🛋", "客厅") is the
// card "karte"; a lamp switched or a thing pinned there still lands back
// on it, not on Start or the thing's page (haus#12).
func TestRaeumeRoomWithoutLettersLandsBackOnIt(t *testing.T) {
	fake, hc := house(t)
	fake.SetRegistries([]map[string]any{{"area_id": "x", "name": "🛋"}}, nil,
		[]map[string]any{{"entity_id": "light.sofa", "area_id": "x"}})
	fake.Set("light.sofa", "on", map[string]any{"friendly_name": "Sofalicht"})
	cfg := StartConfig{HA: hc, Favorites: favorites(t, "")}
	_, body, _ := familyPageAs(t, Raeume, cfg, "GET", "/raeume", "anna", "family")
	mustContain(t, body, `id=karte`, `name=zurueck value="/raeume#karte"`)
	rec := post(t, Schalten(cfg), "/schalten", url.Values{"geraet": {"light.sofa"}, "tun": {"light.turn_off"}, "zurueck": {"/raeume#karte"}}, "anna", nil)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/raeume#karte" {
		t.Errorf("switch → %d %q, want /raeume#karte", rec.Code, rec.Header().Get("Location"))
	}
	rec = post(t, Ding(cfg), "/raeume/light.sofa", url.Values{"stern": {"an"}, "zurueck": {"/raeume#karte"}}, "anna", nil)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/raeume#karte" {
		t.Errorf("pin → %d %q, want /raeume#karte", rec.Code, rec.Header().Get("Location"))
	}
}
