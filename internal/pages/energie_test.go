package pages

import (
	"strings"
	"testing"

	"github.com/mdopp/haus/internal/energie"
	"github.com/mdopp/haus/internal/ha/hafake"
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
// together today, in the family's words: the flow legs as tiles (never a
// sign to decode: "eingespeist", "lädt"), today's counters since
// midnight, and what the house used today from them.
func TestEnergieNowAndToday(t *testing.T) {
	fake, hc := house(t)
	energyHouse(t, fake)
	code, body, _ := familyPage(t, Energie, StartConfig{HA: hc}, "GET", "/energie", "family")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	mustContain(t, body, "<title>Energie · Zuhause</title>", "<h1>Energie</h1>")
	mustContain(t, body, `<section class="karte kacheln" aria-labelledby=karte-jetzt>`, `<section class="karte kacheln" aria-labelledby=karte-heute>`)
	jetzt := section(t, body, "karte-jetzt")
	mustContain(t, jetzt,
		"<span>Haus</span>", "<b class=zustand>1,2 kW</b> <span class=k>Verbrauch</span>",
		"<span>Solaranlage</span>", "<b class=zustand>3,4 kW</b> <span class=k>erzeugt</span>",
		"<span>Stromnetz</span>", "<b class=zustand>1,8 kW</b> <span class=k>eingespeist</span>",
		"<span>Akku</span>", "<b class=zustand>400 W</b> <span class=k>lädt</span>")
	before(t, jetzt, "Haus", "Solaranlage")
	before(t, jetzt, "Solaranlage", "Stromnetz")
	before(t, jetzt, "Stromnetz", "Akku")
	heute := section(t, body, "karte-heute")
	// verbraucht = 12,4 + 3,1 − 6 + 2,2 − 2,5 = 9,2
	mustContain(t, heute,
		"<span>Verbraucht</span>", "<b class=zustand>9,2 kWh</b>",
		"<span>Erzeugt</span>", "<b class=zustand>12,4 kWh</b>",
		"<span>Aus dem Netz</span>", "<b class=zustand>3,1 kWh</b>",
		"<span>Ins Netz</span>", "<b class=zustand>6 kWh</b>",
		"<span>In den Akku</span>", "<b class=zustand>2,5 kWh</b>",
		"<span>Aus dem Akku</span>", "<b class=zustand>2,2 kWh</b>")
	before(t, heute, "Verbraucht", "Erzeugt")
	before(t, heute, "Erzeugt", "Aus dem Netz")
	before(t, heute, "Aus dem Netz", "Ins Netz")
	before(t, heute, "Ins Netz", "In den Akku")
	before(t, heute, "In den Akku", "Aus dem Akku")
	before(t, body, "karte-jetzt", "karte-heute")
	// no sensor's name, entity or unit word the family does not use
	mustNotContain(t, body, "sensor.", "PV-Strom", "Hausverbrauch", "Netz Leistung", "Netzbezug", "Batterie", "Kühlschrank", "-1840", "−")
	// today is since midnight in the house's time zone, one history read a counter
	starts, _ := fake.HistoryQueries()
	if len(starts) != 5 {
		t.Fatalf("%d history reads, want 5: %v", len(starts), starts)
	}
	for _, s := range starts {
		if s != "2026-10-13T22:00:00Z" {
			t.Fatalf("today starts at %q, want local midnight 2026-10-13T22:00:00Z", s)
		}
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
	mustContain(t, jetzt, "<b class=zustand>2,5 kW</b> <span class=k>Verbrauch</span>",
		"<b class=zustand>0 W</b> <span class=k>erzeugt</span>",
		"<b class=zustand>950 W</b> <span class=k>bezogen</span>",
		"<span>Akku</span>", "<b class=zustand>Nicht erreichbar</b>")
	mustContain(t, section(t, body, "karte-heute"), "<span>Erzeugt</span>", "<b class=zustand>12,4 kWh</b>")

	fake.Set("sensor.akku", "-1250", map[string]any{"friendly_name": "Aktuelle Akku Leistung", "device_class": "power", "unit_of_measurement": "W"})
	fake.Set("sensor.netz", "0", map[string]any{"friendly_name": "Aktuelle Netz Leistung", "device_class": "power", "unit_of_measurement": "W"})
	_, body, _ = familyPage(t, Energie, StartConfig{HA: hc}, "GET", "/energie", "family")
	mustContain(t, section(t, body, "karte-jetzt"), "<b class=zustand>1,3 kW</b> <span class=k>entlädt</span>", "<b class=zustand>0 W</b> <span class=k>ruht</span>")
}

// A counter that started over today (a new meter, a reset) counts from
// zero, not below; a counter HA has no history for today is left out of
// Heute, and so is what the house used, which needs it.
func TestEnergieTodayAcrossAReset(t *testing.T) {
	fake, hc := house(t)
	energyHouse(t, fake)
	fake.Set("sensor.bezug_total", "2.5", map[string]any{"friendly_name": "Netzbezug", "device_class": "energy", "unit_of_measurement": "kWh"})
	fake.SetHistory("sensor.bezug_total", []map[string]any{{"state": "1800", "last_changed": "2026-10-13T22:00:00Z"},
		{"state": "1801", "last_changed": "2026-10-14T03:00:00Z"}, {"state": "0", "last_changed": "2026-10-14T04:00:00Z"}}) // +1, reset, +2,5
	fake.SetHistory("sensor.einsp_total", nil)
	_, body, _ := familyPage(t, Energie, StartConfig{HA: hc}, "GET", "/energie", "family")
	heute := section(t, body, "karte-heute")
	mustContain(t, heute, "<span>Aus dem Netz</span>", "<b class=zustand>3,5 kWh</b>", "<span>Erzeugt</span>")
	mustNotContain(t, heute, "Ins Netz", "Verbraucht")
}

// The template names the sensors (HAUS_ENERGY_*): those count, with the
// template's own labels for the counters, and a friendly name the
// fallback would have taken does not; nothing is added up from labels
// Haus cannot read.
func TestEnergieFromTheTemplatesSensors(t *testing.T) {
	fake, hc := house(t)
	energyHouse(t, fake)
	fake.Set("sensor.inverter", "2500", map[string]any{"friendly_name": "Inverter", "device_class": "power", "unit_of_measurement": "W"})
	fake.Set("sensor.pv_kwh", "999", map[string]any{"friendly_name": "PV lifetime", "device_class": "energy", "unit_of_measurement": "kWh"})
	fake.SetHistory("sensor.pv_kwh", []map[string]any{{"state": "990", "last_changed": "2026-10-13T22:00:00Z"}})
	cfg := StartConfig{HA: hc, Energy: energie.Map{Flow: map[string]string{"pv": "sensor.inverter"},
		Totals: []energie.Total{{Label: "Sonnenstrom <heute>", Entity: "sensor.pv_kwh"}}}}
	_, body, _ := familyPage(t, Energie, cfg, "GET", "/energie", "family")
	mustContain(t, section(t, body, "karte-jetzt"), "<span>Solaranlage</span>", "<b class=zustand>2,5 kW</b>")
	mustContain(t, section(t, body, "karte-heute"), "<span>Sonnenstrom &lt;heute&gt;</span>", "<b class=zustand>9 kWh</b>")
	mustNotContain(t, body, "<span>Haus</span>", "Stromnetz", "Verbraucht", "12,4 kWh", "Sonnenstrom <heute>")
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
// The history alone not answering keeps Jetzt and says what Heute lacks.
func TestEnergieSaysPlainlyWhatDoesNotAnswer(t *testing.T) {
	fake, hc := house(t)
	energyHouse(t, fake)
	fake.HistoryDown(true)
	code, body, _ := familyPage(t, Energie, StartConfig{HA: hc}, "GET", "/energie", "family")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	mustContain(t, section(t, body, "karte-jetzt"), "1,2 kW")
	heute := section(t, body, "karte-heute")
	mustContain(t, heute, "Was heute zusammenkam, lässt sich gerade nicht lesen.")
	mustNotContain(t, heute, "kWh")
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
		code, body, _ := familyPage(t, Energie, StartConfig{HA: hc}, "GET", p, "family")
		if code != 404 {
			t.Fatalf("%s → %d, want 404", p, code)
		}
		mustContain(t, body, "<p>Diese Seite gibt es nicht.</p>", "<p><a href=/energie>Energie</a></p>", "href=/energie aria-current=page>")
		mustNotContain(t, body, "kWh", "karte-")
	}
}
