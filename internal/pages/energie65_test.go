package pages

import (
	"strings"
	"testing"
	"time"

	"github.com/mdopp/haus/internal/energie"
	"github.com/mdopp/haus/internal/ha/hafake"
)

// statDays gives a statistic one value a day, noon in Berlin, on the days
// from from to to (exclusive), what perDay says for each.
func statDays(t *testing.T, fake *hafake.HA, id string, from, to time.Time, perDay func(time.Time) float64, today []hafake.Stat) {
	t.Helper()
	var pts []hafake.Stat
	for d := from; d.Before(to); d = d.AddDate(0, 0, 1) {
		pts = append(pts, hafake.Stat{Start: d.Add(12 * time.Hour), Change: perDay(d)})
	}
	fake.SetStatistics(id, append(pts, today...))
}

// todayHours is today until 10:00 (startNow), each hour what each says.
func todayHours(t *testing.T, each float64) []hafake.Stat {
	var out []hafake.Stat
	d := time.Date(2026, 10, 14, 0, 0, 0, 0, berlin(t))
	for h := 0; h < 10; h++ {
		out = append(out, hafake.Stat{Start: d.Add(time.Duration(h) * time.Hour), Change: each})
	}
	return out
}

// the owner's house (haus#65): SENEC's counters, its long-term statistics
// from 2 June 2026 on (the restore); Bezug 2 kWh a day until September,
// 3 in October; the sun 10, of which 6 went into the grid
func senecSince2June(t *testing.T, fake *hafake.HA) StartConfig {
	t.Helper()
	loc := berlin(t)
	kwh := map[string]any{"device_class": "energy", "unit_of_measurement": "kWh", "state_class": "total_increasing"}
	fake.Set("sensor.senec_grid_import_energy", "1803", kwh)
	fake.Set("sensor.senec_grid_export_energy", "2306", kwh)
	fake.Set("sensor.senec_solar_energy", "5012", kwh)
	from, today := time.Date(2026, 6, 2, 0, 0, 0, 0, loc), time.Date(2026, 10, 14, 0, 0, 0, 0, loc)
	statDays(t, fake, "sensor.senec_grid_import_energy", from, today, func(d time.Time) float64 {
		if d.Month() == time.October {
			return 3
		}
		return 2
	}, todayHours(t, 0.2))
	statDays(t, fake, "sensor.senec_grid_export_energy", from, today, func(time.Time) float64 { return 6 }, todayHours(t, 0.1))
	statDays(t, fake, "sensor.senec_solar_energy", from, today, func(time.Time) float64 { return 10 }, todayHours(t, 0.3))
	return StartConfig{Energy: energie.Map{Stats: energie.Stats{Bezug: "sensor.senec_grid_import_energy",
		Einspeisung: "sensor.senec_grid_export_energy", Solar: "sensor.senec_solar_energy"}}}
}

// haus#65: Energie's history from HA's long-term statistics — the day by
// the hour, the week, the month and the year by day and by month — with
// Bezug, Einspeisung, Solar and Eigenverbrauch (Solar − Einspeisung), as
// sums and as a chart drawn by Haus itself (SVG, no script, a legend, the
// numbers as a table).
func TestEnergieHistoryByDayWeekMonthYear(t *testing.T) {
	fake, hc := house(t)
	cfg := senecSince2June(t, fake)
	cfg.HA = hc
	code, body, _ := familyPage(t, Energie, cfg, "GET", "/energie", "family")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	mustContain(t, body, "<h2 class=abschnitt>Verlauf</h2>", `<a href="/energie?zeitraum=tag#verlauf" aria-current=page>Tag</a>`,
		`<a href="/energie?zeitraum=woche#verlauf">Woche</a>`, `<a href="/energie?zeitraum=jahr#verlauf">Jahr</a>`)
	tag := section(t, body, "karte-heute-stunde-fuer-stunde")
	mustContain(t, tag, "<span>Solar</span>", "<b class=zustand>3 kWh</b>", "<span>Eigenverbrauch</span>", "<b class=zustand>2 kWh</b>",
		"<span>Bezug</span>", "<span>Einspeisung</span>", "<b class=zustand>1 kWh</b>")
	before(t, tag, "Solar", "Eigenverbrauch")
	before(t, tag, "Eigenverbrauch", "Bezug")
	before(t, tag, "Bezug", "Einspeisung")
	mustContain(t, body, "<figure class=verlauf><svg", `role=img aria-label="Heute, Stunde für Stunde als Balken; die Zahlen stehen in der Tabelle darunter"`,
		"<title>09 Uhr: Solar 0,3 kWh · Eigenverbrauch 0,2 kWh · Bezug 0,2 kWh · Einspeisung 0,1 kWh</title>",
		"<rect class=eigen", "<rect class=bezug", "<rect class=einsp",
		"<li><i class=eigen></i>Eigenverbrauch (von der Sonne)</li>", "<summary>Als Tabelle</summary>", "<th scope=row>09 Uhr</th>",
		"--e-eigen:#c27c0e", "prefers-color-scheme:light")
	mustNotContain(t, body, "<script", "sensor.senec", "Home Assistant")
	asked := fake.StatisticsAsked()
	periods := map[string]bool{}
	for _, a := range asked {
		periods[a["period"].(string)] = true
		if units, _ := a["units"].(map[string]any); units["energy"] != "kWh" {
			t.Errorf("asked without kWh: %v", a)
		}
	}
	if !periods["hour"] || !periods["day"] {
		t.Fatalf("asked %v, want the day by the hour and the days", asked)
	}

	// the week: 8 to 14 October, by day — six days of 3 and today's 2
	_, body, _ = familyPage(t, Energie, cfg, "GET", "/energie?zeitraum=woche", "family")
	week := section(t, body, "karte-die-letzten-sieben-tage")
	mustContain(t, week, "<b class=zustand>20 kWh</b>", "<b class=zustand>63 kWh</b>") // Bezug 6×3+2, Solar 6×10+3
	mustContain(t, body, "<title>Mittwoch, 14. Oktober: Solar 3 kWh")
	// the month and the year
	_, body, _ = familyPage(t, Energie, cfg, "GET", "/energie?zeitraum=monat", "family")
	mustContain(t, section(t, body, "karte-oktober-2026"), "<b class=zustand>41 kWh</b>") // Bezug 13×3+2
	_, body, _ = familyPage(t, Energie, cfg, "GET", "/energie?zeitraum=jahr", "family")
	year := section(t, body, "karte-das-jahr-2026")
	mustContain(t, year, "<b class=zustand>283 kWh</b>") // Bezug 121×2 + 13×3 + 2
	mustContain(t, body, "<th scope=row>Juni 2026</th>", "<title>Januar 2026</title>")
	mustNotContain(t, body, "<th scope=row>Januar 2026</th>") // no row for a month without values
}

// haus#65: the comparisons — this month against the last and against the
// same month last year, the year so far against last year's — count
// whole days only, and say from when they can be made when HA does not
// have the earlier days yet ("Vergleich ab Juni 2027"), never a zero. The
// forecast is marked as an estimate: the daily mean so far, counted from
// the first day HA has.
func TestEnergieComparesWhatItHas(t *testing.T) {
	fake, hc := house(t)
	cfg := senecSince2June(t, fake)
	cfg.HA = hc
	_, body, _ := familyPage(t, Energie, cfg, "GET", "/energie?zeitraum=monat", "family")
	last := section(t, body, "karte-dieser-monat-und-der-letzte")
	mustContain(t, last, "Bezug: 39 kWh", "September bis zum 13.: 26 kWh (jetzt 50 % mehr)",
		"Solar: 130 kWh", "September bis zum 13.: 130 kWh (jetzt gleich viel)", "Eigenverbrauch: 52 kWh")
	prev := section(t, body, "karte-dieser-monat-und-der-im-vorjahr")
	mustContain(t, prev, "Bezug", "Vergleich ab Juni 2027")
	mustNotContain(t, prev, "0 kWh")
	ytd := section(t, body, "karte-das-jahr-bis-heute-und-das-vorjahr")
	mustContain(t, ytd, "Vergleich ab Juni 2027")
	f := section(t, body, "karte-prognose-bis-jahresende-2026-geschaetzt")
	// Bezug: 281 kWh in 134 days, the 79 left at that mean: 446,7
	mustContain(t, f, "Bezug: etwa 446,7 kWh", "bisher 281 kWh, der Rest nach dem Tagesmittel bisher · gezählt ab dem 2. Juni",
		"Solar: etwa 2130 kWh")
}

// With last year's days in HA, the comparisons have numbers and the
// forecast takes the rest of the year as it came last year.
func TestEnergieComparesWithLastYear(t *testing.T) {
	fake, hc := house(t)
	loc := berlin(t)
	from, today := time.Date(2025, 1, 1, 0, 0, 0, 0, loc), time.Date(2026, 10, 14, 0, 0, 0, 0, loc)
	statDays(t, fake, "sensor.bezug", from, today, func(d time.Time) float64 { return float64(d.Year() - 2024) }, nil) // 1 a day in 2025, 2 in 2026
	cfg := StartConfig{HA: hc, Energy: energie.Map{Stats: energie.Stats{Bezug: "sensor.bezug"}}}
	_, body, _ := familyPage(t, Energie, cfg, "GET", "/energie?zeitraum=jahr", "family")
	mustContain(t, section(t, body, "karte-dieser-monat-und-der-im-vorjahr"), "Bezug: 26 kWh", "Oktober 2025 bis zum 13.: 13 kWh (jetzt 100 % mehr)")
	mustContain(t, section(t, body, "karte-das-jahr-bis-heute-und-das-vorjahr"), "Bezug: 572 kWh", "2025 bis zum 13. Oktober: 286 kWh (jetzt 100 % mehr)")
	f := section(t, body, "karte-prognose-bis-jahresende-2026-geschaetzt")
	mustContain(t, f, "Bezug: etwa 651 kWh", "bisher 572 kWh, der Rest wie im Vorjahr")
	mustNotContain(t, f, "gezählt ab")
	// only Bezug named: no other series, no Eigenverbrauch without Solar and Einspeisung
	mustNotContain(t, body, "Solar", "Eigenverbrauch")
}

// HA's statistics not answering is one plain line where the history
// would be; Jetzt and Heute stay. No statistics yet says so. Without
// HAUS_ENERGY_STATS the fallback's counters are the history's.
func TestEnergieHistorySaysPlainlyWhatIsMissing(t *testing.T) {
	fake, hc := house(t)
	energyHouse(t, fake)
	fake.StatisticsDown(true)
	_, body, _ := familyPage(t, Energie, StartConfig{HA: hc}, "GET", "/energie", "family")
	mustContain(t, section(t, body, "karte-jetzt"), "1,2 kW")
	if n := strings.Count(body, "<p class=k>Der Verlauf lässt sich gerade nicht lesen.</p>"); n != 1 {
		t.Fatalf("said %d times, want once", n)
	}
	mustNotLeak(t, body, "recorder")
	asked := fake.StatisticsAsked()
	ids := asked[0]["statistic_ids"].([]any)
	if len(ids) != 3 || ids[0] != "sensor.bezug_total" || ids[1] != "sensor.einsp_total" || ids[2] != "sensor.pv_total" {
		t.Fatalf("asked for %v, want the fallback's three counters", ids)
	}
	fake.StatisticsDown(false)
	_, body, _ = familyPage(t, Energie, StartConfig{HA: hc}, "GET", "/energie", "family")
	mustContain(t, body, "Für diese Zähler gibt es noch keinen Verlauf.")
	mustNotContain(t, body, "<figure", "Vergleich")
}
