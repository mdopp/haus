package pages

import (
	"strings"
	"testing"

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
// same as a table; Heute as the widget's number column (owner,
// 2026-11-03): Erzeugt, Ins Netz, Netzbezug, Autarkie — not what went
// into the battery or came out of it, not what the house used.
func TestEnergieNowAndToday(t *testing.T) {
	fake, hc := house(t)
	energyHouse(t, fake)
	code, body, _ := familyPage(t, Energie, StartConfig{HA: hc}, "GET", "/energie", "family")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	mustContain(t, body, "<title>Energie · Homes</title>", "<h1>Energie</h1>")
	mustContain(t, body, `<section class="karte fluss" aria-labelledby=karte-jetzt>`, `<section class="karte spalte" aria-labelledby=karte-heute>`)
	jetzt := section(t, body, "karte-jetzt")
	mustContain(t, jetzt,
		"<tr><th scope=row>Haus verbraucht</th><td>1,2 kW</td></tr>", "<tr><th scope=row>Solar erzeugt</th><td>3,4 kW</td></tr>",
		"<tr><th scope=row>ins Netz</th><td>1,8 kW</td></tr>", "<tr><th scope=row>Akku lädt</th><td>400 W</td></tr>",
		"<tr><th scope=row>Solar → Haus</th><td>1,2 kW</td></tr>", "<tr><th scope=row>Solar → Akku</th><td>400 W</td></tr>",
		"<tr><th scope=row>Solar → Netz</th><td>1,8 kW</td></tr>")
	heute := section(t, body, "karte-heute")
	// the house used 12,4 − 6 + 3,1 + 2,2 − 2,5 = 9,2, of it 3,1 from the grid
	mustContain(t, heute, "<h2 id=karte-heute>Heute</h2><table><tbody>",
		`<tr><td class="zahl solar">12,4 kWh</td><th scope=row>Erzeugt</th></tr>`,
		`<tr><td class="zahl einsp">6 kWh</td><th scope=row>Ins Netz</th></tr>`,
		`<tr><td class="zahl bezug">3,1 kWh</td><th scope=row>Netzbezug</th></tr>`,
		"<tr><td class=zahl>66\u00a0%</td><th scope=row>Autarkie</th></tr>")
	before(t, heute, "Erzeugt", "Ins Netz")
	before(t, heute, "Ins Netz", "Netzbezug")
	before(t, heute, "Netzbezug", "Autarkie")
	mustNotContain(t, heute, "Akku", "Verbraucht", "Aus dem Netz", "2,5 kWh", "2,2 kWh", "9,2 kWh")
	before(t, body, "karte-jetzt", "karte-heute")
	// no sensor's name, entity or unit word the family does not use
	mustNotContain(t, body, "sensor.", "PV-Strom", "Hausverbrauch", "Netz Leistung", "Batterie", "Kühlschrank", "-1840", "−")
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
	mustContain(t, jetzt, "<tr><th scope=row>Haus verbraucht</th><td>2,5 kW</td></tr>",
		"<tr><th scope=row>Solar erzeugt</th><td>0 W</td></tr>",
		"<tr><th scope=row>aus dem Netz</th><td>950 W</td></tr>",
		"<tr><th scope=row>Akku</th><td>nicht erreichbar</td></tr>")
	mustContain(t, section(t, body, "karte-heute"), `<tr><td class="zahl solar">12,4 kWh</td><th scope=row>Erzeugt</th></tr>`)

	fake.Set("sensor.akku", "-1250", map[string]any{"friendly_name": "Aktuelle Akku Leistung", "device_class": "power", "unit_of_measurement": "W"})
	fake.Set("sensor.netz", "0", map[string]any{"friendly_name": "Aktuelle Netz Leistung", "device_class": "power", "unit_of_measurement": "W"})
	_, body, _ = familyPage(t, Energie, StartConfig{HA: hc}, "GET", "/energie", "family")
	mustContain(t, section(t, body, "karte-jetzt"), "<tr><th scope=row>Akku entlädt</th><td>1,3 kW</td></tr>", "<tr><th scope=row>Netz ruht</th><td>0 W</td></tr>")
}

// A counter that started over today (a new meter, a reset) counts from
// zero, not below; a counter HA has no history for today is left out of
// Heute, and so is the Autarkie, which needs it.
func TestEnergieTodayAcrossAReset(t *testing.T) {
	fake, hc := house(t)
	energyHouse(t, fake)
	fake.Set("sensor.bezug_total", "2.5", map[string]any{"friendly_name": "Netzbezug", "device_class": "energy", "unit_of_measurement": "kWh"})
	fake.SetHistory("sensor.bezug_total", []map[string]any{{"state": "1800", "last_changed": "2026-10-13T22:00:00Z"},
		{"state": "1801", "last_changed": "2026-10-14T03:00:00Z"}, {"state": "0", "last_changed": "2026-10-14T04:00:00Z"}}) // +1, reset, +2,5
	fake.SetHistory("sensor.einsp_total", nil)
	_, body, _ := familyPage(t, Energie, StartConfig{HA: hc}, "GET", "/energie", "family")
	heute := section(t, body, "karte-heute")
	mustContain(t, heute, `<tr><td class="zahl bezug">3,5 kWh</td><th scope=row>Netzbezug</th></tr>`, "Erzeugt")
	mustNotContain(t, heute, "Ins Netz", "Autarkie")
}

// The template names the sensors (HOMES_ENERGY_*): those count, with the
// template's own labels for the counters, and a friendly name the
// fallback would have taken does not; no Autarkie from labels Homes
// cannot read.
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
	mustContain(t, section(t, body, "karte-heute"), "<tr><td class=zahl>9 kWh</td><th scope=row>Sonnenstrom &lt;heute&gt;</th></tr>")
	mustNotContain(t, body, "knoten haus", "knoten netz", "Autarkie", "12,4 kWh", "Sonnenstrom <heute>")
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

// A lifetime counter that dips a little (a meter's jitter, a rounding)
// has not started over: HA itself counts a drop of less than 10 % as
// noise. Only a real fall — a new meter, a reset — counts from zero, and
// then from wherever the new count stands.
func TestEnergieTodayTellsANewMeterFromNoise(t *testing.T) {
	fake, hc := house(t)
	energyHouse(t, fake)
	at := func(state, when string) map[string]any { return map[string]any{"state": state, "last_changed": when} }
	kwh := func(name string) map[string]any {
		return map[string]any{"friendly_name": name, "device_class": "energy", "unit_of_measurement": "kWh"}
	}
	fake.Set("sensor.bezug_total", "2.5", kwh("Netzbezug"))
	fake.SetHistory("sensor.bezug_total", []map[string]any{at("1800", "2026-10-13T22:00:00Z"), at("1801", "2026-10-14T03:00:00Z"),
		at("0.4", "2026-10-14T04:00:00Z")}) // +1, a new meter at 0,4, +2,1
	fake.Set("sensor.einsp_total", "2303", kwh("Netzeinspeisung"))
	fake.SetHistory("sensor.einsp_total", []map[string]any{at("2300", "2026-10-13T22:00:00Z"), at("2302", "2026-10-14T03:00:00Z"),
		at("2301.9", "2026-10-14T04:00:00Z")}) // +2, a dip, +1
	_, body, _ := familyPage(t, Energie, StartConfig{HA: hc}, "GET", "/energie", "family")
	heute := section(t, body, "karte-heute")
	mustContain(t, heute, `<td class="zahl bezug">3,5 kWh</td><th scope=row>Netzbezug</th>`, `<td class="zahl einsp">3 kWh</td><th scope=row>Ins Netz</th>`)
	mustNotContain(t, heute, "230")
}

// The Autarkie needs every counter the house has: both battery counters
// without today's history leave it out, never a share that forgets the
// battery. A counter whose history holds no reading at
// all is not "0 kWh" today; it is left out.
func TestEnergieUsedNeedsEveryCounter(t *testing.T) {
	fake, hc := house(t)
	energyHouse(t, fake)
	fake.SetHistory("sensor.akku_laden", nil)
	fake.SetHistory("sensor.akku_entladen", nil)
	_, body, _ := familyPage(t, Energie, StartConfig{HA: hc}, "GET", "/energie", "family")
	heute := section(t, body, "karte-heute")
	mustContain(t, heute, "Erzeugt")
	mustNotContain(t, heute, "Autarkie", "Akku")

	fake2, hc2 := house(t)
	energyHouse(t, fake2)
	fake2.SetHistory("sensor.pv_total", []map[string]any{{"state": "unavailable", "last_changed": "2026-10-13T22:00:00Z"}})
	_, body, _ = familyPage(t, Energie, StartConfig{HA: hc2}, "GET", "/energie", "family")
	heute = section(t, body, "karte-heute")
	mustContain(t, heute, "Netzbezug")
	mustNotContain(t, heute, "Erzeugt", "Autarkie", "0 kWh")
}

// The edges of the words: MW and MWh as HA may send them, the kilowatt
// line, under a watt is resting either way, a reading that is no number
// (Inf) is not a size; a day whose counters do not add up has no
// Autarkie; a fall to 80 % is a new meter (HA's 10 % line).
func TestEnergieEdgesOfTheWords(t *testing.T) {
	fake, hc := house(t)
	energyHouse(t, fake)
	unit := func(name, class, u string) map[string]any {
		return map[string]any{"friendly_name": name, "device_class": class, "unit_of_measurement": u}
	}
	at := func(state, when string) map[string]any { return map[string]any{"state": state, "last_changed": when} }
	fake.Set("sensor.pv", "Inf", unit("Aktuell erzeugter PV-Strom", "power", "W"))
	fake.Set("sensor.haus", "0.0012", unit("Aktueller Hausverbrauch", "power", "MW"))
	fake.Set("sensor.netz", "0.4", unit("Aktuelle Netz Leistung", "power", "W"))
	fake.Set("sensor.akku", "999.6", unit("Aktuelle Akku Leistung", "power", "W"))
	fake.Set("sensor.pv_total", "5.0124", unit("PV Erzeugung", "energy", "MWh"))
	fake.SetHistory("sensor.pv_total", []map[string]any{at("5.0", "2026-10-13T22:00:00Z")})
	fake.Set("sensor.einsp_total", "2330", unit("Netzeinspeisung", "energy", "kWh")) // +30: more fed in than came in
	fake.Set("sensor.akku_laden", "801", unit("Batterie laden", "energy", "kWh"))
	fake.SetHistory("sensor.akku_laden", []map[string]any{at("1000", "2026-10-13T22:00:00Z"), at("800", "2026-10-14T02:00:00Z")})
	_, body, _ := familyPage(t, Energie, StartConfig{HA: hc}, "GET", "/energie", "family")
	jetzt := section(t, body, "karte-jetzt")
	mustContain(t, jetzt, "<tr><th scope=row>Solar</th><td>unbekannt</td></tr>", "<tr><th scope=row>Haus verbraucht</th><td>1,2 kW</td></tr>",
		"<tr><th scope=row>Netz ruht</th><td>0 W</td></tr>", "<tr><th scope=row>Akku lädt</th><td>1 kW</td></tr>")
	heute := section(t, body, "karte-heute")
	mustContain(t, heute, `<td class="zahl solar">12,4 kWh</td><th scope=row>Erzeugt</th>`)
	mustNotContain(t, heute, "Autarkie", "801 kWh")
	// the history reaches up to now, not short of it
	_, qs := fake.HistoryQueries()
	for _, q := range qs {
		if end := q.Get("end_time"); end <= "2026-10-14T08:00:00Z" {
			t.Fatalf("history ends %q, before now", end)
		}
	}

	fake.Set("sensor.netz", "-0.4", unit("Aktuelle Netz Leistung", "power", "W"))
	_, body, _ = familyPage(t, Energie, StartConfig{HA: hc}, "GET", "/energie", "family")
	mustContain(t, section(t, body, "karte-jetzt"), "<tr><th scope=row>Netz ruht</th><td>0 W</td></tr>")
}

// A house with counters and no flow sensor still has Heute.
func TestEnergieCountersWithoutFlow(t *testing.T) {
	fake, hc := house(t)
	fake.Set("sensor.pv_total", "5012.4", map[string]any{"friendly_name": "PV Erzeugung", "device_class": "energy", "unit_of_measurement": "kWh"})
	fake.SetHistory("sensor.pv_total", []map[string]any{{"state": "5000", "last_changed": "2026-10-13T22:00:00Z"}})
	_, body, _ := familyPage(t, Energie, StartConfig{HA: hc}, "GET", "/energie", "family")
	mustContain(t, section(t, body, "karte-heute"), "Erzeugt", "12,4 kWh")
	mustNotContain(t, body, "karte-jetzt", "eingerichtet")
}

// Autarkie only when the house's counters make its meaning certain: one
// battery counter without the other, or no Einspeisung counter, is no
// share.
func TestEnergieUsedOnlyWhenItsMeaningIsCertain(t *testing.T) {
	for _, gone := range []string{"sensor.akku_entladen", "sensor.einsp_total"} {
		fake, hc := house(t)
		energyHouse(t, fake)
		fake.Set(gone, "1", map[string]any{"friendly_name": "Etwas anderes", "device_class": "energy", "unit_of_measurement": "kWh"})
		_, body, _ := familyPage(t, Energie, StartConfig{HA: hc}, "GET", "/energie", "family")
		heute := section(t, body, "karte-heute")
		mustContain(t, heute, "Erzeugt")
		mustNotContain(t, heute, "Autarkie")
	}
}
