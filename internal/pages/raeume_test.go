package pages

import (
	"fmt"
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
// in one "Ohne Raum" card at the end. Read-only.
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
	mustNotContain(t, body, "Bad</h2>", "Kaffeemaschine", "Küchentemperatur", "Feuchte", "Erdgeschoss",
		"<form", "<button", "<script")
	// rooms by name, Ohne Raum last
	before(t, body, "karte-kueche", "karte-wohnzimmer")
	before(t, body, "karte-wohnzimmer", "karte-ohne-raum")
	// each thing in its own room
	before(t, body, "karte-kueche", "Deckenlampe")
	before(t, body, "Rollo Küche", "karte-wohnzimmer")
	before(t, body, "karte-wohnzimmer", "Stehlampe")
	before(t, body, "Sonos", "karte-ohne-raum")
	before(t, body, "karte-ohne-raum", "Kellerlicht")
	// within a room: lights, heating, covers, media
	before(t, body, "Deckenlampe", "Rollo Küche")
	before(t, body, "Stehlampe", "Heizung Wohnzimmer")
	before(t, body, "Heizung Wohnzimmer", "Sonos")
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
// and no error on the page. The log has it.
func TestRaeumeHidesBrokenBackends(t *testing.T) {
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
