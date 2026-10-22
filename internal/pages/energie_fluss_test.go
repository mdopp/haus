package pages

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/haus/internal/energie"
	"github.com/mdopp/haus/internal/ha/hafake"
)

// flowLegs sets the four flow legs (W) by the fallback's friendly names;
// "" leaves a leg out.
func flowLegs(t *testing.T, fake *hafake.HA, pv, house, grid, battery string) {
	t.Helper()
	power := func(name string) map[string]any {
		return map[string]any{"friendly_name": name, "device_class": "power", "unit_of_measurement": "W"}
	}
	for _, l := range []struct{ id, v, name string }{{"sensor.pv", pv, "Aktuell erzeugter PV-Strom"}, {"sensor.haus", house, "Aktueller Hausverbrauch"},
		{"sensor.netz", grid, "Aktuelle Netz Leistung"}, {"sensor.akku", battery, "Aktuelle Akku Leistung"}} {
		if l.v != "" {
			fake.Set(l.id, l.v, power(l.name))
		}
	}
}

// between is the part of s from start up to the first end after it.
func between(t *testing.T, s, start, end string) string {
	t.Helper()
	i := strings.Index(s, start)
	if i < 0 {
		t.Fatalf("no %q in:\n%s", start, s)
	}
	j := strings.Index(s[i:], end)
	if j < 0 {
		t.Fatalf("no %q after %q", end, start)
	}
	return s[i : i+j]
}

// strokeOf is the width an edge is drawn with.
func strokeOf(t *testing.T, s, edge string) float64 {
	t.Helper()
	g := between(t, s, `<g class="kante `+edge+`">`, "</g>")
	m := regexp.MustCompile(`stroke-width=([0-9.]+)`).FindStringSubmatch(g)
	if m == nil {
		t.Fatalf("edge %s has no width: %s", edge, g)
	}
	v, _ := strconv.ParseFloat(m[1], 64)
	return v
}

// The owner (2026-10-22): "Spinnen-Diagramme, die Eingang / Ausgang /
// Verbrauch / Erzeugung übersichtlich darstellen". Jetzt is one picture:
// Solar, Netz, Akku and Haus as nodes, and the ways the power takes
// between them — the sun into the house first, then into the battery,
// then into the grid; what the house needs beyond that from the battery,
// then from the grid. Each way is as thick as it carries, its arrow says
// where to; the same as a table, and Autarkie and Eigenverbrauchsquote.
func TestEnergieJetztFlowsFromTheLegs(t *testing.T) {
	for _, c := range []struct {
		name, pv, house, grid, bat string
		want, not                  []string
	}{
		{"a sunny noon", "4000", "1500", "-1500", "1000",
			[]string{"<tr><th scope=row>Solar erzeugt</th><td>4 kW</td></tr>", "<tr><th scope=row>Haus verbraucht</th><td>1,5 kW</td></tr>",
				"<tr><th scope=row>Netz eingespeist</th><td>1,5 kW</td></tr>", "<tr><th scope=row>Akku lädt</th><td>1 kW</td></tr>",
				"<tr><th scope=row>Solar → Haus</th><td>1,5 kW</td></tr>", "<tr><th scope=row>Solar → Akku</th><td>1 kW</td></tr>",
				"<tr><th scope=row>Solar → Netz</th><td>1,5 kW</td></tr>",
				`<g class="kante solar-haus">`, `<g class="kante solar-akku">`, `<g class="kante solar-netz">`,
				"<dt>Autarkie</dt><dd class=zahl>100 %</dd>", "<dt>Eigenverbrauchsquote</dt><dd class=zahl>63 %</dd>",
				`aria-label="Jetzt: Solar erzeugt 4 kW, Haus verbraucht 1,5 kW, Netz eingespeist 1,5 kW, Akku lädt 1 kW. Solar → Haus 1,5 kW, Solar → Akku 1 kW, Solar → Netz 1,5 kW."`},
			[]string{"Netz → Haus", "Akku → Haus", `kante netz-haus`, `kante akku-haus`, "nicht erreichbar"}},
		{"a night", "0", "400", "400", "0",
			[]string{"<tr><th scope=row>Netz → Haus</th><td>400 W</td></tr>", `<g class="kante netz-haus">`, "<tr><th scope=row>Netz bezogen</th><td>400 W</td></tr>",
				"<tr><th scope=row>Akku ruht</th><td>0 W</td></tr>", "<dt>Autarkie</dt><dd class=zahl>0 %</dd>"},
			[]string{"Solar →", `kante solar-`, "<dt>Eigenverbrauchsquote</dt>"}}, // no sun, no share of it
		{"an evening on the battery", "300", "1200", "100", "-800",
			[]string{"<tr><th scope=row>Solar → Haus</th><td>300 W</td></tr>", "<tr><th scope=row>Akku → Haus</th><td>800 W</td></tr>",
				"<tr><th scope=row>Netz → Haus</th><td>100 W</td></tr>", "<tr><th scope=row>Akku entlädt</th><td>800 W</td></tr>",
				"<dt>Autarkie</dt><dd class=zahl>92 %</dd>", "<dt>Eigenverbrauchsquote</dt><dd class=zahl>100 %</dd>"},
			[]string{"Solar → Netz", "Solar → Akku"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			fake, hc := house(t)
			flowLegs(t, fake, c.pv, c.house, c.grid, c.bat)
			_, body, _ := familyPage(t, Energie, StartConfig{HA: hc}, "GET", "/energie", "family")
			mustContain(t, body, `<section class="karte fluss" aria-labelledby=karte-jetzt>`)
			jetzt := section(t, body, "karte-jetzt")
			mustContain(t, jetzt, c.want...)
			mustNotContain(t, jetzt, c.not...)
			mustContain(t, jetzt, `<g class="knoten solar">`, `<g class="knoten netz">`, `<g class="knoten akku">`, `<g class="knoten haus">`,
				"<summary>Als Tabelle</summary>")
		})
	}
	// the thickness says how much: 1,5 kW is drawn wider than 1 kW, equal to 1,5
	fake, hc := house(t)
	flowLegs(t, fake, "4000", "1500", "-1500", "1000")
	_, body, _ := familyPage(t, Energie, StartConfig{HA: hc}, "GET", "/energie", "family")
	jetzt := section(t, body, "karte-jetzt")
	if h, a, n := strokeOf(t, jetzt, "solar-haus"), strokeOf(t, jetzt, "solar-akku"), strokeOf(t, jetzt, "solar-netz"); h <= a || h != n {
		t.Fatalf("widths Haus %v, Akku %v, Netz %v: want Haus = Netz > Akku", h, a, n)
	}
	// a moving dash is only a hint, and stops for those who asked for less motion
	mustContain(t, body, "@keyframes fliesst", "@media (prefers-reduced-motion:reduce){.fluss .lauf{animation:none}}")
}

// A leg HA cannot reach is drawn muted with its reason, never as a zero,
// and no way is drawn that would need it, nor a key figure; a house
// without a battery has no Akku at all.
func TestEnergieJetztWithoutALeg(t *testing.T) {
	fake, hc := house(t)
	flowLegs(t, fake, "2000", "800", "-1200", "unavailable")
	_, body, _ := familyPage(t, Energie, StartConfig{HA: hc}, "GET", "/energie", "family")
	jetzt := section(t, body, "karte-jetzt")
	akku := between(t, jetzt, `<g class="knoten akku aus">`, "</g>")
	mustContain(t, akku, "nicht erreichbar")
	mustNotContain(t, akku, " W<", "kW<")
	mustContain(t, jetzt, "<tr><th scope=row>Akku</th><td>nicht erreichbar</td></tr>", "<tr><th scope=row>Solar erzeugt</th><td>2 kW</td></tr>",
		"<p class=hinweis>Akku nicht erreichbar: wohin der Strom fließt, zeigt das Bild wieder, sobald er antwortet.</p>")
	mustNotContain(t, jetzt, `<g class="kante`, "→", "<dt>Autarkie</dt>", "Akku lädt", "Akku ruht")

	fake2, hc2 := house(t)
	flowLegs(t, fake2, "2000", "800", "-1200", "")
	_, body, _ = familyPage(t, Energie, StartConfig{HA: hc2}, "GET", "/energie", "family")
	jetzt = section(t, body, "karte-jetzt")
	mustContain(t, jetzt, "<tr><th scope=row>Solar → Haus</th><td>800 W</td></tr>", "<tr><th scope=row>Solar → Netz</th><td>1,2 kW</td></tr>",
		"<dt>Eigenverbrauchsquote</dt><dd class=zahl>40 %</dd>")
	mustNotContain(t, jetzt, "Akku", "knoten akku")
}

// The chosen span's totals as the same picture (haus#65): today so far
// from HA's statistics — Solar 3, Einspeisung 1, Bezug 2: the house used
// 4, 2 of it from the sun, 2 from the grid; Autarkie 50 %, of the sun's
// 3 the house kept 2 (67 %). Heute's counter tiles say the same and give
// way to it.
func TestEnergieSpanAsAFlow(t *testing.T) {
	fake, hc := house(t)
	cfg := senecSince2June(t, fake)
	cfg.HA = hc
	_, body, _ := familyPage(t, Energie, cfg, "GET", "/energie", "family")
	mustContain(t, body, `<section class="karte fluss" aria-labelledby=karte-heute-bis-jetzt>`)
	tag := section(t, body, "karte-heute-bis-jetzt")
	mustContain(t, tag, "<tr><th scope=row>Solar erzeugt</th><td>3 kWh</td></tr>", "<tr><th scope=row>Netz bezogen</th><td>2 kWh</td></tr>",
		"<tr><th scope=row>Netz eingespeist</th><td>1 kWh</td></tr>", "<tr><th scope=row>Haus verbraucht</th><td>4 kWh</td></tr>",
		"<tr><th scope=row>Solar → Haus</th><td>2 kWh</td></tr>", "<tr><th scope=row>Solar → Netz</th><td>1 kWh</td></tr>",
		"<tr><th scope=row>Netz → Haus</th><td>2 kWh</td></tr>",
		"<dt>Autarkie</dt><dd class=zahl>50 %</dd>", "<dt>Eigenverbrauchsquote</dt><dd class=zahl>67 %</dd>", ">1 kWh eingespeist</text>")
	mustNotContain(t, tag, "knoten akku") // the history reads no battery

	// the house's counters too: the history shows today, Heute's tiles go
	fake2, hc2 := house(t)
	energyHouse(t, fake2)
	loc := berlin(t)
	for _, id := range []string{"sensor.pv_total", "sensor.einsp_total", "sensor.bezug_total"} {
		statDays(t, fake2, id, time.Date(2026, 10, 1, 0, 0, 0, 0, loc), time.Date(2026, 10, 14, 0, 0, 0, 0, loc), func(time.Time) float64 { return 2 }, todayHours(t, 0.1))
	}
	_, body, _ = familyPage(t, Energie, StartConfig{HA: hc2}, "GET", "/energie", "family")
	mustContain(t, body, "aria-labelledby=karte-jetzt>", "aria-labelledby=karte-heute-bis-jetzt>")
	mustNotContain(t, body, "aria-labelledby=karte-heute>")
}

// The Geschäftsbericht's colour (the owner: "farblich kodieren, als ob ich
// einen Geschäftsbericht als Vorstand lese"): each comparison a headline
// number and a delta with its arrow, sign and word, coloured by what it
// means for the household — more Solar and Eigenverbrauch is good, more
// Bezug bad, Einspeisung neither (income, but less kept).
func TestEnergieDeltasSayGoodOrBad(t *testing.T) {
	for _, c := range []struct {
		name     string
		sep, oct [3]float64 // solar, einsp, bezug a day
		want     []string
	}{
		{"more of everything", [3]float64{10, 6, 2}, [3]float64{12, 7, 3}, []string{
			`<li><span class=name>Bezug</span> <b class=zahl>39 kWh</b> <span class="delta schlecht"><span aria-hidden=true>▲</span> +50 % · schlechter</span> <span class=k>Vormonat 26 kWh</span></li>`,
			`<li><span class=name>Solar</span> <b class=zahl>156 kWh</b> <span class="delta gut"><span aria-hidden=true>▲</span> +20 % · besser</span> <span class=k>Vormonat 130 kWh</span></li>`,
			`<li><span class=name>Eigenverbrauch</span> <b class=zahl>65 kWh</b> <span class="delta gut"><span aria-hidden=true>▲</span> +25 % · besser</span>`,
			`<li><span class=name>Einspeisung</span> <b class=zahl>91 kWh</b> <span class="delta neutral"><span aria-hidden=true>▲</span> +17 %</span>`}},
		{"less of everything", [3]float64{12, 7, 3}, [3]float64{10, 6, 2}, []string{
			`<span class=name>Bezug</span> <b class=zahl>26 kWh</b> <span class="delta gut"><span aria-hidden=true>▼</span> −33 % · besser</span>`,
			`<span class=name>Solar</span> <b class=zahl>130 kWh</b> <span class="delta schlecht"><span aria-hidden=true>▼</span> −17 % · schlechter</span>`,
			`<span class=name>Einspeisung</span> <b class=zahl>78 kWh</b> <span class="delta neutral"><span aria-hidden=true>▼</span> −14 %</span>`}},
	} {
		t.Run(c.name, func(t *testing.T) {
			fake, hc := house(t)
			loc := berlin(t)
			from, to := time.Date(2026, 9, 1, 0, 0, 0, 0, loc), time.Date(2026, 10, 14, 0, 0, 0, 0, loc)
			for i, id := range []string{"sensor.solar", "sensor.einsp", "sensor.bezug"} {
				statDays(t, fake, id, from, to, func(d time.Time) float64 {
					if d.Month() == time.October {
						return c.oct[i]
					}
					return c.sep[i]
				}, nil)
			}
			cfg := StartConfig{HA: hc, Energy: energie.Map{Stats: energie.Stats{Solar: "sensor.solar", Einspeisung: "sensor.einsp", Bezug: "sensor.bezug"}}}
			_, body, _ := familyPage(t, Energie, cfg, "GET", "/energie?zeitraum=monat", "family")
			last := section(t, body, "karte-dieser-monat-und-der-letzte")
			mustContain(t, body, `<section class="karte vergleich" aria-labelledby=karte-dieser-monat-und-der-letzte>`)
			mustContain(t, last, "<p class=hinweis>Verglichen mit September bis zum 13.</p>")
			mustContain(t, last, c.want...)
		})
	}
	fake, hc := house(t)
	cfg := senecSince2June(t, fake)
	cfg.HA = hc
	_, body, _ := familyPage(t, Energie, cfg, "GET", "/energie", "family")
	// the tokens: a blue-ish green and an orange-red, dark and light, by the series colours
	mustContain(t, body, "--e-gut:", "--e-schlecht:", ".delta.gut{", ".delta.schlecht{", "font-variant-numeric:tabular-nums")
}

// The forecast as a chart (the owner: "ein Diagramm, das die weiteren
// Monate prognostiziert darstellt"): this year's months, measured ones
// solid, the rest hatched and marked geschätzt, the month of today half
// and half with a "heute" line, last year's months as a stroke when HA
// has them; the year-end figures above it as the headline, each a link
// that picks the chart's series.
func TestEnergieForecastAsAChart(t *testing.T) {
	fake, hc := house(t)
	cfg := senecSince2June(t, fake)
	cfg.HA = hc
	_, body, _ := familyPage(t, Energie, cfg, "GET", "/energie?zeitraum=monat", "family")
	mustContain(t, body, `<section class="karte prognose" id=prognose aria-labelledby=karte-prognose-bis-jahresende-2026-geschaetzt>`)
	f := section(t, body, "karte-prognose-bis-jahresende-2026-geschaetzt")
	// Bezug: 281 kWh in 134 days, the 79 left at that mean: 446,7
	mustContain(t, f, `<a href="/energie?zeitraum=monat&amp;prognose=bezug#prognose" aria-current=true><span class=name>Bezug</span> <b class=zahl>etwa 447 kWh</b> <span class=k>bisher 281 kWh</span></a>`,
		`<a href="/energie?zeitraum=monat&amp;prognose=solar#prognose"><span class=name>Solar</span> <b class=zahl>etwa 2.130 kWh</b>`,
		"<p class=hinweis>Bis gestern gemessen, der Rest geschätzt nach dem Tagesmittel bisher · gezählt ab dem 2. Juni · Vergleich mit dem Vorjahr ab 2028</p>",
		`aria-label="Bezug 2026 Monat für Monat: gemessen bis gestern, danach geschätzt; die Zahlen stehen in der Tabelle darunter"`,
		`<g class="monat leer"><title>Mai 2026: keine Werte</title>`,
		`<g class="monat gemessen"><title>Juni 2026: 58 kWh gemessen</title>`,
		`<g class="monat teils"><title>Oktober 2026: 39 kWh gemessen, dazu etwa 38 kWh geschätzt</title>`,
		`<g class="monat geschaetzt"><title>November 2026: etwa 63 kWh geschätzt</title>`,
		"<line class=heute", ">heute</text>",
		"<tr><th scope=row>Dezember 2026</th><td>–</td><td>etwa 65 kWh</td><td>–</td></tr>",
		"<tr><th scope=row>Juni 2026</th><td>58 kWh</td><td>–</td><td>–</td></tr>")
	mustNotContain(t, f, "<line class=vorjahr", "Januar 2026</th>", "delta")
	if strings.Count(f, `class="schaetzung bezug"`) < 3 {
		t.Fatalf("October, November and December want a hatched bar:\n%s", f)
	}

	// the series named: the chart draws Solar
	_, body, _ = familyPage(t, Energie, cfg, "GET", "/energie?zeitraum=monat&prognose=solar", "family")
	f = section(t, body, "karte-prognose-bis-jahresende-2026-geschaetzt")
	mustContain(t, f, `aria-label="Solar 2026 Monat für Monat:`, `prognose=solar#prognose" aria-current=true>`, `class="ist solar"`)
	// one it does not know: Bezug, the first
	_, body, _ = familyPage(t, Energie, cfg, "GET", "/energie?zeitraum=monat&prognose=%3Cx%3E", "family")
	mustContain(t, section(t, body, "karte-prognose-bis-jahresende-2026-geschaetzt"), `aria-label="Bezug 2026 Monat für Monat:`)
	mustNotContain(t, body, "<x>")
}
