package pages

import (
	"strings"
	"testing"
	"time"

	"github.com/mdopp/homes/internal/energie"
	"github.com/mdopp/homes/internal/ha/hafake"
)

// a house as the fallback finds it, by friendly name: the four flow legs
// and the five counters, each counter with today's history since
// midnight (startNow is 14 Oct 10:00 in Berlin)
func energyHouse(t *testing.T, fake *hafake.HA) {
	t.Helper()
	power := func(name string) map[string]any {
		return map[string]any{"friendly_name": name, "device_class": "power", "unit_of_measurement": "W"}
	}
	kwh := func(name string) map[string]any {
		return map[string]any{"friendly_name": name, "device_class": "energy", "unit_of_measurement": "kWh"}
	}
	fake.Set("sensor.pv", "3420", power("Aktuell erzeugter PV-Strom"))
	fake.Set("sensor.haus", "1180", power("Aktueller Hausverbrauch"))
	fake.Set("sensor.netz", "-1840", power("Aktuelle Netz Leistung"))
	fake.Set("sensor.akku", "400", power("Aktuelle Akku Leistung"))
	fake.Set("sensor.kuehlschrank", "80", power("Kühlschrank"))
	fake.Set("sensor.pv_total", "5012.4", kwh("PV Erzeugung"))
	fake.Set("sensor.einsp_total", "2306", kwh("Netzeinspeisung"))
	fake.Set("sensor.bezug_total", "1803.1", kwh("Netzbezug"))
	fake.Set("sensor.akku_laden", "700.5", kwh("Batterie laden"))
	fake.Set("sensor.akku_entladen", "650.2", kwh("Batterie entladen"))
	at := func(state, when string) map[string]any { return map[string]any{"state": state, "last_changed": when} }
	fake.SetHistory("sensor.pv_total", []map[string]any{at("5000", "2026-10-13T22:00:00Z"), at("5006.1", "2026-10-14T06:00:00Z")})         // +12,4
	fake.SetHistory("sensor.einsp_total", []map[string]any{at("2300", "2026-10-13T22:00:00Z")})                                            // +6
	fake.SetHistory("sensor.bezug_total", []map[string]any{at("1800", "2026-10-13T22:00:00Z"), at("unavailable", "2026-10-14T01:00:00Z")}) // +3,1
	fake.SetHistory("sensor.akku_laden", []map[string]any{at("698", "2026-10-13T22:00:00Z")})                                              // +2,5
	fake.SetHistory("sensor.akku_entladen", []map[string]any{at("648", "2026-10-13T22:00:00Z")})                                           // +2,2
}

// haus#12: Energie says what the house uses and makes now, and what came
// together today, in the family's words: the flow legs as a picture of
// where the power goes (never a sign to decode: "ins Netz", "lädt"), the
// same as a table. No Heute block under it (owner, 2026-11-03): the
// history's Tag picture says what came together today.
func TestEnergieNowAndToday(t *testing.T) {
	fake, hc := house(t)
	energyHouse(t, fake)
	code, body, _ := familyPage(t, Energie, StartConfig{HA: hc}, "GET", "/energie", "family")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	mustContain(t, body, "<title>Energie · Homes</title>", "<h1>Energie</h1>")
	mustContain(t, body, `<section class="karte fluss" aria-labelledby=karte-jetzt>`)
	jetzt := section(t, body, "karte-jetzt")
	mustContain(t, jetzt,
		"<tr><th scope=row>Haus verbraucht</th><td>1,2 kW</td></tr>", "<tr><th scope=row>Solar erzeugt</th><td>3,4 kW</td></tr>",
		"<tr><th scope=row>ins Netz</th><td>1,8 kW</td></tr>", "<tr><th scope=row>Akku lädt</th><td>400 W</td></tr>",
		"<tr><th scope=row>Solar → Haus</th><td>1,2 kW</td></tr>", "<tr><th scope=row>Solar → Akku</th><td>400 W</td></tr>",
		"<tr><th scope=row>Solar → Netz</th><td>1,8 kW</td></tr>")
	// the Heute block is gone: no heading, no card, no number column
	mustNotContain(t, body, "<h2 id=karte-heute>", "aria-labelledby=karte-heute>", `class="karte spalte"`, "class=spalte", ".spalte")
	// no sensor's name, entity or unit word the family does not use
	mustNotContain(t, body, "sensor.", "PV-Strom", "Hausverbrauch", "Netz Leistung", "Batterie", "Kühlschrank", "-1840", "−")
	// nothing reads the counters' history since midnight any more
	if starts, _ := fake.HistoryQueries(); len(starts) != 0 {
		t.Fatalf("%d history reads, want none: %v", len(starts), starts)
	}
}

// The other signs: the grid drawn from, the battery giving, nothing made;
// kW and Wh as HA sends them; a leg HA cannot reach says so.
func TestEnergieSaysEachLegInWords(t *testing.T) {
	fake, hc := house(t)
	energyHouse(t, fake)
	fake.Set("sensor.pv", "0", map[string]any{"friendly_name": "Aktuell erzeugter PV-Strom", "device_class": "power", "unit_of_measurement": "W"})
	fake.Set("sensor.haus", "2.45", map[string]any{"friendly_name": "Aktueller Hausverbrauch", "device_class": "power", "unit_of_measurement": "kW"})
	fake.Set("sensor.netz", "950", map[string]any{"friendly_name": "Aktuelle Netz Leistung", "device_class": "power", "unit_of_measurement": "W"})
	fake.Set("sensor.akku", "unavailable", map[string]any{"friendly_name": "Aktuelle Akku Leistung", "device_class": "power", "unit_of_measurement": "W"})
	fake.Set("sensor.pv_total", "5012400", map[string]any{"friendly_name": "PV Erzeugung", "device_class": "energy", "unit_of_measurement": "Wh"})
	fake.SetHistory("sensor.pv_total", []map[string]any{{"state": "5000000", "last_changed": "2026-10-13T22:00:00Z"}})
	_, body, _ := familyPage(t, Energie, StartConfig{HA: hc}, "GET", "/energie", "family")
	jetzt := section(t, body, "karte-jetzt")
	mustContain(t, jetzt, "<tr><th scope=row>Haus verbraucht</th><td>2,5 kW</td></tr>",
		"<tr><th scope=row>Solar erzeugt</th><td>0 W</td></tr>",
		"<tr><th scope=row>aus dem Netz</th><td>950 W</td></tr>",
		"<tr><th scope=row>Akku</th><td>nicht erreichbar</td></tr>")

	fake.Set("sensor.akku", "-1250", map[string]any{"friendly_name": "Aktuelle Akku Leistung", "device_class": "power", "unit_of_measurement": "W"})
	fake.Set("sensor.netz", "0", map[string]any{"friendly_name": "Aktuelle Netz Leistung", "device_class": "power", "unit_of_measurement": "W"})
	_, body, _ = familyPage(t, Energie, StartConfig{HA: hc}, "GET", "/energie", "family")
	mustContain(t, section(t, body, "karte-jetzt"), "<tr><th scope=row>Akku entlädt</th><td>1,3 kW</td></tr>", "<tr><th scope=row>Netz ruht</th><td>0 W</td></tr>")
}

// The template names the sensors (HOMES_ENERGY_*): those count, with the
// template's own sensors, and a friendly name the fallback would have
// taken does not.
func TestEnergieFromTheTemplatesSensors(t *testing.T) {
	fake, hc := house(t)
	energyHouse(t, fake)
	fake.Set("sensor.inverter", "2500", map[string]any{"friendly_name": "Inverter", "device_class": "power", "unit_of_measurement": "W"})
	fake.Set("sensor.pv_kwh", "999", map[string]any{"friendly_name": "PV lifetime", "device_class": "energy", "unit_of_measurement": "kWh"})
	fake.SetHistory("sensor.pv_kwh", []map[string]any{{"state": "990", "last_changed": "2026-10-13T22:00:00Z"}})
	cfg := StartConfig{HA: hc, Energy: energie.Map{Flow: map[string]string{"pv": "sensor.inverter"},
		Totals: []energie.Total{{Label: "Sonnenstrom <heute>", Entity: "sensor.pv_kwh"}}}}
	_, body, _ := familyPage(t, Energie, cfg, "GET", "/energie", "family")
	mustContain(t, section(t, body, "karte-jetzt"), "<tr><th scope=row>Solar erzeugt</th><td>2,5 kW</td></tr>")
	mustNotContain(t, body, "knoten haus", "knoten netz", "12,4 kWh", "Sonnenstrom <heute>")
}

// No energy sensors at all: one plain line, never a blank page.
func TestEnergieWithoutSensorsSaysSo(t *testing.T) {
	fake, hc := house(t)
	fake.Set("light.flur", "on", map[string]any{"friendly_name": "Flurlicht"})
	fake.Set("sensor.kuehlschrank", "80", map[string]any{"friendly_name": "Kühlschrank", "device_class": "power", "unit_of_measurement": "W"})
	code, body, _ := familyPage(t, Energie, StartConfig{HA: hc}, "GET", "/energie", "family")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	if n := strings.Count(body, "<p class=k>Für dieses Haus ist noch kein Stromzähler eingerichtet.</p>"); n != 1 {
		t.Fatalf("said %d times, want once:\n%s", n, body)
	}
	mustNotContain(t, body, "karte-", "Kühlschrank")
}

// HA not answering: one plain line, no cards, nothing of HA on the page.
// The counters' history not answering does not matter to the page.
func TestEnergieSaysPlainlyWhatDoesNotAnswer(t *testing.T) {
	fake, hc := house(t)
	energyHouse(t, fake)
	fake.HistoryDown(true)
	code, body, _ := familyPage(t, Energie, StartConfig{HA: hc}, "GET", "/energie", "family")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	mustContain(t, section(t, body, "karte-jetzt"), "1,2 kW")
	mustNotContain(t, body, "Was heute zusammenkam")
	mustNotLeak(t, body, "recorder", host(hc.URL))

	fake.Down(true)
	code, body, _ = familyPage(t, Energie, StartConfig{HA: hc}, "GET", "/energie", "family")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	if n := strings.Count(body, "<p class=k>Die Stromzähler antworten gerade nicht.</p>"); n != 1 {
		t.Fatalf("said %d times, want once:\n%s", n, body)
	}
	mustNotContain(t, body, "karte-", "kWh", "eingerichtet")
	mustNotLeak(t, body, host(hc.URL))
}

// A path under Energie that Haus does not serve is a 404 with the way
// back, never Start pretending all is well (haus#12).
func TestEnergieUnknownPathIsA404(t *testing.T) {
	fake, hc := house(t)
	energyHouse(t, fake)
	for _, p := range []string{"/energie/", "/energie/heute", "/energie/x/y"} {
		code, body, _ := familyPage(t, Energie, StartConfig{HA: hc, Module: modulListe(t, "geraete", "energie")}, "GET", p, "family")
		if code != 404 {
			t.Fatalf("%s → %d, want 404", p, code)
		}
		mustContain(t, body, "<p>Diese Seite gibt es nicht.</p>", "<p><a href=/energie>Energie</a></p>", "href=/energie aria-current=page>")
		mustNotContain(t, body, "kWh", "karte-")
	}
}

// The edges of the words: MW and MWh as HA may send them, the kilowatt
// line, under a watt is resting either way, a reading that is no number
// (Inf) is not a size.
func TestEnergieEdgesOfTheWords(t *testing.T) {
	fake, hc := house(t)
	energyHouse(t, fake)
	unit := func(name, class, u string) map[string]any {
		return map[string]any{"friendly_name": name, "device_class": class, "unit_of_measurement": u}
	}
	fake.Set("sensor.pv", "Inf", unit("Aktuell erzeugter PV-Strom", "power", "W"))
	fake.Set("sensor.haus", "0.0012", unit("Aktueller Hausverbrauch", "power", "MW"))
	fake.Set("sensor.netz", "0.4", unit("Aktuelle Netz Leistung", "power", "W"))
	fake.Set("sensor.akku", "999.6", unit("Aktuelle Akku Leistung", "power", "W"))
	_, body, _ := familyPage(t, Energie, StartConfig{HA: hc}, "GET", "/energie", "family")
	jetzt := section(t, body, "karte-jetzt")
	mustContain(t, jetzt, "<tr><th scope=row>Solar</th><td>unbekannt</td></tr>", "<tr><th scope=row>Haus verbraucht</th><td>1,2 kW</td></tr>",
		"<tr><th scope=row>Netz ruht</th><td>0 W</td></tr>", "<tr><th scope=row>Akku lädt</th><td>1 kW</td></tr>")

	fake.Set("sensor.netz", "-0.4", unit("Aktuelle Netz Leistung", "power", "W"))
	_, body, _ = familyPage(t, Energie, StartConfig{HA: hc}, "GET", "/energie", "family")
	mustContain(t, section(t, body, "karte-jetzt"), "<tr><th scope=row>Netz ruht</th><td>0 W</td></tr>")
}

// A house with counters and no flow sensor still has its history: the
// Tag picture of what the sun made today.
func TestEnergieCountersWithoutFlow(t *testing.T) {
	fake, hc := house(t)
	fake.Set("sensor.pv_total", "5012.4", map[string]any{"friendly_name": "PV Erzeugung", "device_class": "energy", "unit_of_measurement": "kWh"})
	statDays(t, fake, "sensor.pv_total", time.Date(2026, 10, 1, 0, 0, 0, 0, berlin(t)), time.Date(2026, 10, 14, 0, 0, 0, 0, berlin(t)), func(time.Time) float64 { return 5 }, todayHours(t, 0.5))
	_, body, _ := familyPage(t, Energie, StartConfig{HA: hc}, "GET", "/energie", "family")
	mustContain(t, section(t, body, "karte-heute-bis-jetzt"), "<tr><th scope=row>Solar erzeugt</th><td>5 kWh</td></tr>")
	mustNotContain(t, body, "karte-jetzt", "eingerichtet", "aria-labelledby=karte-heute>")
}
