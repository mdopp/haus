package pages

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/homes/internal/energie"
	"github.com/mdopp/homes/internal/ha/hafake"
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

// between is the part of s from start up to the first end after it (end
// looked for after start).
func between(t *testing.T, s, start, end string) string {
	t.Helper()
	i := strings.Index(s, start)
	if i < 0 {
		t.Fatalf("no %q in:\n%s", start, s)
	}
	j := strings.Index(s[i+len(start):], end)
	if j < 0 {
		t.Fatalf("no %q after %q", end, start)
	}
	return s[i : i+len(start)+j]
}

// strokeOf is the width a way is drawn with.
func strokeOf(t *testing.T, s, edge string) float64 {
	t.Helper()
	g := between(t, s, `<g class="kante `+edge+` `, "</g>")
	m := regexp.MustCompile(`stroke-width=([0-9.]+)`).FindStringSubmatch(g)
	if m == nil {
		t.Fatalf("edge %s has no width: %s", edge, g)
	}
	v, _ := strconv.ParseFloat(m[1], 64)
	return v
}

// nums are the numbers in s, in order.
func nums(t *testing.T, s string) []float64 {
	t.Helper()
	var out []float64
	for _, m := range regexp.MustCompile(`-?[0-9]+(?:\.[0-9]+)?`).FindAllString(s, -1) {
		v, _ := strconv.ParseFloat(m, 64)
		out = append(out, v)
	}
	return out
}

// inPill is where p lies against the stadium pill centred at c: its
// distance from the pill's straight middle line over the ends' radius —
// below 1 inside, above 1 outside.
func inPill(p, c punkt) float64 {
	r := float64(pillH) / 2
	half := float64(pillW)/2 - r
	dx := math.Max(0, math.Abs(p.x-c.x)-half)
	return math.Hypot(dx, p.y-c.y) / r
}

// arrowsPointIn checks every way drawn in the picture (the owner,
// 2026-11-03; the Android widget got this wrong twice): the line starts
// outside the source's pill, by its border; the arrowhead's tip sits
// just outside the destination's pill, a step further along is inside
// it, both back corners are farther from the destination than the tip,
// and the line ends before the tip. It answers which ways it saw.
func arrowsPointIn(t *testing.T, jetzt string) []string {
	t.Helper()
	centre := map[string]punkt{}
	for _, m := range regexp.MustCompile(`<g class="knoten (\w+)[^"]*"><rect class=pill x=([0-9.]+) y=([0-9.]+) `).FindAllStringSubmatch(jetzt, -1) {
		x, _ := strconv.ParseFloat(m[2], 64)
		y, _ := strconv.ParseFloat(m[3], 64)
		centre[m[1]] = punkt{x + pillW/2, y + pillH/2}
	}
	var seen []string
	for _, m := range regexp.MustCompile(`<g class="kante (\w+)-(\w+) [^"]*">.*?<path class=weg d="([^"]+)".*?<path class=pfeil d="([^"]+)"></path></g>`).FindAllStringSubmatch(jetzt, -1) {
		von, nach := m[1], m[2]
		seen = append(seen, von+"-"+nach)
		src, dst := centre[von], centre[nach]
		w, h := nums(t, m[3]), nums(t, m[4])
		if len(w) != 4 || len(h) != 6 {
			t.Fatalf("%s-%s: way %q, head %q", von, nach, m[3], m[4])
		}
		start, end := punkt{w[0], w[1]}, punkt{w[2], w[3]}
		tip, c1, c2 := punkt{h[0], h[1]}, punkt{h[2], h[3]}, punkt{h[4], h[5]}
		d := func(a, b punkt) float64 { return math.Hypot(a.x-b.x, a.y-b.y) }
		if v := inPill(start, src); v <= 1 || d(start, src) > d(start, dst) {
			t.Errorf("%s-%s starts at %v, not by %s's border (%v)", von, nach, start, von, v)
		}
		if inPill(start, dst) <= 1 {
			t.Errorf("%s-%s starts inside %s", von, nach, nach)
		}
		ux, uy := (tip.x-start.x)/d(tip, start), (tip.y-start.y)/d(tip, start)
		if v := inPill(tip, dst); v <= 1 || inPill(punkt{tip.x + 16*ux, tip.y + 16*uy}, dst) >= 1 {
			t.Errorf("%s-%s: the tip %v is not at %s's border (%v)", von, nach, tip, nach, v)
		}
		if d(c1, dst) <= d(tip, dst) || d(c2, dst) <= d(tip, dst) {
			t.Errorf("%s-%s: the head %v does not point into %s", von, nach, h, nach)
		}
		if d(start, end) >= d(start, tip) {
			t.Errorf("%s-%s: the line runs to %v, past the tip %v", von, nach, end, tip)
		}
	}
	return seen
}

// The owner (2026-10-22): "Spinnen-Diagramme, die Eingang / Ausgang /
// Verbrauch / Erzeugung übersichtlich darstellen"; 2026-11-03, the
// widget's look: Solar, Netz, Akku and Haus as pills, each its number and
// its name with what it does now; only the ways the power takes now —
// the sun into the house first, then into the battery, then into the
// grid; what the house needs beyond that from the battery, then from the
// grid — each as thick as it carries, the arrowhead at the destination.
// The same as a table. No key figures under it: Heute carries the
// Autarkie, and the Eigenverbrauchsquote is gone.
func TestEnergieJetztFlowsFromTheLegs(t *testing.T) {
	for _, c := range []struct {
		name, pv, house, grid, bat string
		ways                       []string
		want, not                  []string
	}{
		{"a sunny noon", "4000", "1500", "-1500", "1000", []string{"solar-haus", "solar-akku", "solar-netz"},
			[]string{"<tr><th scope=row>Solar erzeugt</th><td>4 kW</td></tr>", "<tr><th scope=row>Haus verbraucht</th><td>1,5 kW</td></tr>",
				"<tr><th scope=row>ins Netz</th><td>1,5 kW</td></tr>", "<tr><th scope=row>Akku lädt</th><td>1 kW</td></tr>",
				"<tr><th scope=row>Solar → Haus</th><td>1,5 kW</td></tr>", "<tr><th scope=row>Solar → Akku</th><td>1 kW</td></tr>",
				"<tr><th scope=row>Solar → Netz</th><td>1,5 kW</td></tr>",
				"<text class=zahl x=198 y=39 text-anchor=middle>4 kW</text><text class=wort x=198 y=56 text-anchor=middle><tspan class=nm>Solar</tspan> erzeugt</text>",
				"<text class=wort x=70 y=151 text-anchor=middle>ins <tspan class=nm>Netz</tspan></text>",
				"<tspan class=nm>Haus</tspan> verbraucht</text>", "<tspan class=nm>Akku</tspan> lädt</text>",
				`aria-label="Jetzt: Solar erzeugt 4 kW, Haus verbraucht 1,5 kW, ins Netz 1,5 kW, Akku lädt 1 kW. Solar → Haus 1,5 kW, Solar → Akku 1 kW, Solar → Netz 1,5 kW."`},
			[]string{"Netz → Haus", "Akku → Haus"}},
		{"a night", "0", "400", "400", "0", []string{"netz-haus"},
			[]string{"<tr><th scope=row>Netz → Haus</th><td>400 W</td></tr>", "<tr><th scope=row>aus dem Netz</th><td>400 W</td></tr>",
				"<tr><th scope=row>Akku ruht</th><td>0 W</td></tr>", "aus dem <tspan class=nm>Netz</tspan></text>", "<tspan class=nm>Akku</tspan> ruht</text>"},
			[]string{"Solar →"}},
		{"an evening on the battery", "300", "1200", "100", "-800", []string{"solar-haus", "akku-haus", "netz-haus"},
			[]string{"<tr><th scope=row>Solar → Haus</th><td>300 W</td></tr>", "<tr><th scope=row>Akku → Haus</th><td>800 W</td></tr>",
				"<tr><th scope=row>Netz → Haus</th><td>100 W</td></tr>", "<tr><th scope=row>Akku entlädt</th><td>800 W</td></tr>"},
			[]string{"Solar → Netz", "Solar → Akku"}},
		{"the grid charges the battery", "0", "300", "1300", "1000", []string{"netz-haus", "netz-akku"},
			[]string{"<tr><th scope=row>Netz → Akku</th><td>1 kW</td></tr>", "<tr><th scope=row>Netz → Haus</th><td>300 W</td></tr>"},
			[]string{"Solar →", "Akku → Haus"}},
		{"the grid resting", "500", "500", "0", "0", []string{"solar-haus"},
			[]string{"<tr><th scope=row>Netz ruht</th><td>0 W</td></tr>", "<tspan class=nm>Netz</tspan> ruht</text>"},
			nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			fake, hc := house(t)
			flowLegs(t, fake, c.pv, c.house, c.grid, c.bat)
			_, body, _ := familyPage(t, Energie, StartConfig{HA: hc}, "GET", "/energie", "family")
			mustContain(t, body, `<section class="karte fluss" aria-labelledby=karte-jetzt>`)
			jetzt := section(t, body, "karte-jetzt")
			mustContain(t, jetzt, c.want...)
			mustNotContain(t, jetzt, c.not...)
			// stadium pills, rectangles with fully rounded ends (owner, 2026-11-03)
			mustContain(t, jetzt, `<g class="knoten solar"><rect class=pill x=136 y=6 width=124 height=68 rx=34>`, `<g class="knoten netz"><rect class=pill x=8 y=101 `,
				`<g class="knoten akku"><rect class=pill x=262 y=101 `, `<g class="knoten haus"><rect class=pill x=136 y=196 `, "<summary>Als Tabelle</summary>")
			// only the ways the power takes, each pointing into its destination
			if got := arrowsPointIn(t, jetzt); strings.Join(got, " ") != strings.Join(c.ways, " ") {
				t.Fatalf("ways drawn %v, want %v", got, c.ways)
			}
			mustNotContain(t, jetzt, "class=spur", "<dt>", "Eigenverbrauch", "Autarkie", "<circle", "<ellipse")
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
	// the widget's card: black, no border, in the dark; the light keeps the page's white
	mustContain(t, body, "--e-karte:#000", "--e-karte:var(--card)", ".fluss-bild{background:var(--e-karte);border-radius:18px;")
}

// The arrowhead sits at the destination for every way the picture can
// draw, with the battery's ring around its pill and without, and in the
// triangle of a house without a battery.
func TestEnergieArrowsEndAtTheDestination(t *testing.T) {
	for _, c := range []struct {
		pv, house, grid, bat string
		charge               bool
	}{
		{"4000", "1500", "-1500", "1000", true}, {"4000", "1500", "-1500", "1000", false},
		{"300", "1200", "100", "-800", true}, {"0", "300", "1300", "1000", true}, {"3000", "800", "-2200", "", false},
	} {
		fake, hc := house(t)
		flowLegs(t, fake, c.pv, c.house, c.grid, c.bat)
		if c.charge {
			fake.Set("sensor.soc", "83", map[string]any{"friendly_name": "Akku-Füllstand in Prozent", "device_class": "battery", "unit_of_measurement": "%"})
		}
		_, body, _ := familyPage(t, Energie, StartConfig{HA: hc}, "GET", "/energie", "family")
		if len(arrowsPointIn(t, section(t, body, "karte-jetzt"))) == 0 {
			t.Fatalf("%+v: no way drawn", c)
		}
	}
	// the stadium's border: straight on top, round at the ends
	if r := reach(0, 1); r != pillH/2 {
		t.Fatalf("reach down %v, want %v", r, pillH/2)
	}
	if r := reach(1, 0); math.Abs(r-pillW/2) > 1e-9 {
		t.Fatalf("reach sideways %v, want %v", r, pillW/2)
	}
	// the pure geometry: the head's tip is the way's end, its corners behind it
	h := pfeil(punkt{0, 0}, punkt{100, 0}, 5)
	if h[0] != (punkt{100, 0}) || h[1].x >= 100 || h[2].x >= 100 || h[1].y != -h[2].y {
		t.Fatalf("head %v: want the tip at the end, the corners behind it", h)
	}
}

// A leg HA cannot reach is drawn muted with its reason, never as a zero,
// and no way is drawn that would need it; a house without a battery has
// no Akku at all, and its pills a triangle.
func TestEnergieJetztWithoutALeg(t *testing.T) {
	fake, hc := house(t)
	flowLegs(t, fake, "2000", "800", "-1200", "unavailable")
	_, body, _ := familyPage(t, Energie, StartConfig{HA: hc}, "GET", "/energie", "family")
	jetzt := section(t, body, "karte-jetzt")
	akku := between(t, jetzt, `<g class="knoten akku aus">`, "</g>")
	mustContain(t, akku, "<tspan class=nm>Akku</tspan></text>", "<text class=grund x=324 y=150 text-anchor=middle>nicht erreichbar</text>")
	mustNotContain(t, akku, " W<", "kW<", "class=zahl")
	mustContain(t, jetzt, "<tr><th scope=row>Akku</th><td>nicht erreichbar</td></tr>", "<tr><th scope=row>Solar erzeugt</th><td>2 kW</td></tr>",
		"<p class=hinweis>Akku nicht erreichbar: wohin der Strom fließt, zeigt das Bild wieder, sobald er antwortet.</p>")
	mustNotContain(t, jetzt, `<g class="kante`, "→", "Akku lädt", "Akku ruht")

	fake2, hc2 := house(t)
	flowLegs(t, fake2, "2000", "800", "-1200", "")
	_, body, _ = familyPage(t, Energie, StartConfig{HA: hc2}, "GET", "/energie", "family")
	jetzt = section(t, body, "karte-jetzt")
	mustContain(t, jetzt, "<tr><th scope=row>Solar → Haus</th><td>800 W</td></tr>", "<tr><th scope=row>Solar → Netz</th><td>1,2 kW</td></tr>",
		`viewBox="0 0 396 200"`, `<g class="knoten netz"><rect class=pill x=30 y=126 `, `<g class="knoten haus"><rect class=pill x=242 y=126 `)
	mustNotContain(t, jetzt, "Akku", "knoten akku")
}

// The chosen span's totals (haus#65) as Jetzt's picture, in kWh (owner,
// 2026-11-03: the history keeps its flow graphic, drawn like Jetzt): the
// stadium pills, the words that read for a span — "Solar erzeugt", "aus
// dem Netz" with what went "ins Netz" under the grid's pill, "Haus
// verbraucht" — every way with a total, its arrowhead at the destination,
// no battery ring, and the span's Autarkie. Tag too: today so far from
// HA's statistics — Solar 3, Einspeisung 1, Bezug 2: the house used 4,
// 2 of it from the sun, 2 from the grid. No number column for a span:
// the picture says it.
func TestEnergieSpanAsAFlow(t *testing.T) {
	fake, hc := house(t)
	cfg := senecSince2June(t, fake)
	cfg.HA = hc
	_, body, _ := familyPage(t, Energie, cfg, "GET", "/energie", "family")
	mustContain(t, body, `<section class="karte fluss" aria-labelledby=karte-heute-bis-jetzt>`)
	tag := section(t, body, "karte-heute-bis-jetzt")
	mustContain(t, tag, "<tr><th scope=row>Solar erzeugt</th><td>3 kWh</td></tr>", "<tr><th scope=row>aus dem Netz</th><td>2 kWh</td></tr>",
		"<tr><th scope=row>ins Netz</th><td>1 kWh</td></tr>", "<tr><th scope=row>Haus verbraucht</th><td>4 kWh</td></tr>",
		"<tr><th scope=row>Solar → Haus</th><td>2 kWh</td></tr>", "<tr><th scope=row>Solar → Netz</th><td>1 kWh</td></tr>",
		"<tr><th scope=row>Netz → Haus</th><td>2 kWh</td></tr>",
		"aus dem <tspan class=nm>Netz</tspan></text><text class=zweit x=92 y=212 text-anchor=middle>1 kWh ins Netz</text>",
		`viewBox="0 0 396 222"`, `<g class="knoten solar"><rect class=pill `, "<p class=autarkie><b>50\u00a0%</b> Autarkie</p>",
		`aria-label="Heute bis jetzt: Solar erzeugt 3 kWh, Haus verbraucht 4 kWh, aus dem Netz 2 kWh, ins Netz 1 kWh. Solar → Haus 2 kWh, Solar → Netz 1 kWh, Netz → Haus 2 kWh."`)
	mustNotContain(t, tag, "knoten akku", "class=fuell", "<ellipse") // the history reads no battery, and a span has no charge
	if got := arrowsPointIn(t, tag); strings.Join(got, " ") != "solar-haus solar-netz netz-haus" {
		t.Fatalf("ways drawn %v", got)
	}
	// Heute's column stays with Jetzt, from the statistics here
	mustContain(t, section(t, body, "karte-heute"), `<tr><td class="zahl solar">3 kWh</td><th scope=row>Erzeugt</th></tr>`)
	before(t, body, "karte-heute-bis-jetzt", "<figure class=verlauf>")

	// Monat: Solar 13×10+3, Einspeisung 13×6+1, Bezug 13×3+2; the house used 133 − 79 + 41 = 95, 54 of it from the sun
	_, body, _ = familyPage(t, Energie, cfg, "GET", "/energie?zeitraum=monat", "family")
	mon := section(t, body, "karte-oktober-2026-bis-heute")
	mustContain(t, mon, "<tr><th scope=row>Solar erzeugt</th><td>133 kWh</td></tr>", "<tr><th scope=row>ins Netz</th><td>79 kWh</td></tr>",
		"<tr><th scope=row>aus dem Netz</th><td>41 kWh</td></tr>", "<tr><th scope=row>Haus verbraucht</th><td>95 kWh</td></tr>",
		"<p class=autarkie><b>57\u00a0%</b> Autarkie</p>")
	arrowsPointIn(t, mon)
	if n := strings.Count(body, `class="karte spalte"`); n != 1 {
		t.Fatalf("%d number columns, want Heute's alone", n)
	}
	// the arrow's width says how much: 54 into the house is wider than 41 from the grid
	if s, g := strokeOf(t, mon, "solar-haus"), strokeOf(t, mon, "netz-haus"); s <= g {
		t.Fatalf("widths Solar → Haus %v, Netz → Haus %v", s, g)
	}

	// only Bezug in the history: the grid into the house, no Autarkie without the sun
	fake2, hc2 := house(t)
	loc := berlin(t)
	statDays(t, fake2, "sensor.bezug", time.Date(2026, 10, 1, 0, 0, 0, 0, loc), time.Date(2026, 10, 14, 0, 0, 0, 0, loc), func(time.Time) float64 { return 2 }, nil)
	_, body, _ = familyPage(t, Energie, StartConfig{HA: hc2, Energy: energie.Map{Stats: energie.Stats{Bezug: "sensor.bezug"}}}, "GET", "/energie?zeitraum=woche", "family")
	week := section(t, body, "karte-die-letzten-sieben-tage")
	mustContain(t, week, "<tr><th scope=row>aus dem Netz</th><td>12 kWh</td></tr>", "<tr><th scope=row>Netz → Haus</th><td>12 kWh</td></tr>")
	mustNotContain(t, week, "Autarkie", "class=zweit", "Solar")
}

// The owner (2026-11-03): "'Dieser Monat und der letzte' … ein Vergleich
// mit dem Vormonat bringt bei PV nicht viel. Kann weg. Eigenverbrauch ist
// mir nicht wichtig." Neither is on the page in any span: no comparison
// with the month before, no Eigenverbrauch series, tile, legend, table
// column or quota; Heute has no battery rows; the picture draws no quiet
// tracks for ways the power does not take.
func TestEnergieRemovedSectionsAreGone(t *testing.T) {
	fake, hc := house(t)
	energyHouse(t, fake)
	loc := berlin(t)
	for _, id := range []string{"sensor.pv_total", "sensor.einsp_total", "sensor.bezug_total"} {
		statDays(t, fake, id, time.Date(2025, 1, 1, 0, 0, 0, 0, loc), time.Date(2026, 10, 14, 0, 0, 0, 0, loc), func(time.Time) float64 { return 4 }, todayHours(t, 0.1))
	}
	for _, z := range []string{"tag", "woche", "monat", "jahr"} {
		_, body, _ := familyPage(t, Energie, StartConfig{HA: hc}, "GET", "/energie?zeitraum="+z, "family")
		mustContain(t, body, "karte-jetzt", "karte-heute", "<figure class=verlauf>")
		mustNotContain(t, body, "Dieser Monat und der letzte", "karte-dieser-monat-und-der-letzte", "Vormonat",
			"Eigenverbrauch", "eigenverbrauch", "class=eigen", "--e-eigen", "schraffur-eigen",
			"In den Akku", "Aus dem Akku", "Verbraucht<", "class=spur", "kennzahlen")
	}
	// what stays: the comparisons with last year, and the forecast
	_, body, _ := familyPage(t, Energie, StartConfig{HA: hc}, "GET", "/energie?zeitraum=monat", "family")
	mustContain(t, body, "karte-dieser-monat-und-der-im-vorjahr", "karte-das-jahr-bis-heute-und-das-vorjahr", "karte-prognose-bis-jahresende-2026-geschaetzt")
}

// The Geschäftsbericht's colour (the owner: "farblich kodieren, als ob ich
// einen Geschäftsbericht als Vorstand lese"): each comparison a headline
// number and a delta with its arrow, sign and word, coloured by what it
// means for the household — more made is good, more from the grid bad,
// more into it neither (income, but less kept). This October against
// last year's.
func TestEnergieDeltasSayGoodOrBad(t *testing.T) {
	for _, c := range []struct {
		name         string
		before, then [3]float64 // solar, einsp, bezug a day, in 2025 and in 2026
		want         []string
	}{
		{"more of everything", [3]float64{10, 6, 2}, [3]float64{12, 7, 3}, []string{
			`<li><span class=name>Netzbezug</span> <b class=zahl>39 kWh</b> <span class="delta schlecht"><span aria-hidden=true>▲</span> +50 % · schlechter</span> <span class=k>Vorjahr 26 kWh</span></li>`,
			`<li><span class=name>Erzeugt</span> <b class=zahl>156 kWh</b> <span class="delta gut"><span aria-hidden=true>▲</span> +20 % · besser</span> <span class=k>Vorjahr 130 kWh</span></li>`,
			`<li><span class=name>Ins Netz</span> <b class=zahl>91 kWh</b> <span class="delta neutral"><span aria-hidden=true>▲</span> +17 %</span>`}},
		{"less of everything", [3]float64{12, 7, 3}, [3]float64{10, 6, 2}, []string{
			`<span class=name>Netzbezug</span> <b class=zahl>26 kWh</b> <span class="delta gut"><span aria-hidden=true>▼</span> −33 % · besser</span>`,
			`<span class=name>Erzeugt</span> <b class=zahl>130 kWh</b> <span class="delta schlecht"><span aria-hidden=true>▼</span> −17 % · schlechter</span>`,
			`<span class=name>Ins Netz</span> <b class=zahl>78 kWh</b> <span class="delta neutral"><span aria-hidden=true>▼</span> −14 %</span>`}},
	} {
		t.Run(c.name, func(t *testing.T) {
			fake, hc := house(t)
			loc := berlin(t)
			from, to := time.Date(2025, 10, 1, 0, 0, 0, 0, loc), time.Date(2026, 10, 14, 0, 0, 0, 0, loc)
			for i, id := range []string{"sensor.solar", "sensor.einsp", "sensor.bezug"} {
				statDays(t, fake, id, from, to, func(d time.Time) float64 {
					if d.Year() == 2026 {
						return c.then[i]
					}
					return c.before[i]
				}, nil)
			}
			cfg := StartConfig{HA: hc, Energy: energie.Map{Stats: energie.Stats{Solar: "sensor.solar", Einspeisung: "sensor.einsp", Bezug: "sensor.bezug"}}}
			_, body, _ := familyPage(t, Energie, cfg, "GET", "/energie?zeitraum=monat", "family")
			prev := section(t, body, "karte-dieser-monat-und-der-im-vorjahr")
			mustContain(t, body, `<section class="karte vergleich" aria-labelledby=karte-dieser-monat-und-der-im-vorjahr>`)
			mustContain(t, prev, "<p class=hinweis>Verglichen mit Oktober 2025 bis zum 13.</p>")
			mustContain(t, prev, c.want...)
		})
	}
	fake, hc := house(t)
	cfg := senecSince2June(t, fake)
	cfg.HA = hc
	_, body, _ := familyPage(t, Energie, cfg, "GET", "/energie", "family")
	// the tokens: a blue-ish green and an orange-red, dark and light, by the series colours
	mustContain(t, body, "--e-gut:", "--e-schlecht:", ".delta.gut{", ".delta.schlecht{", "font-variant-numeric:tabular-nums")
}

// The forecast in the history's own chart (the owner, on #73: "warum
// nicht direkt ins erste Diagramm mit allen Werten?"): in Jahr the months
// still to come, and the rest of this month, are drawn hatched in the
// same stacked shape, each series in its colour, said "geschätzt" in the
// tooltip, the table and the legend; a "heute" line; last year's month a
// stroke when HA has all of it. The estimate is the forecast's: the rest
// at the daily mean so far here (HA starts on 2 June), today's part
// already measured taken off. The year-end figures are the chart's
// headline, right under it; the span's column stays "bis heute".
func TestEnergieYearChartCarriesTheForecast(t *testing.T) {
	fake, hc := house(t)
	cfg := senecSince2June(t, fake)
	cfg.HA = hc
	_, body, _ := familyPage(t, Energie, cfg, "GET", "/energie?zeitraum=jahr", "family")
	fig := between(t, body, "<figure class=verlauf>", "</figure>")
	mustContain(t, fig,
		`aria-label="Das Jahr 2026 als Balken, der Rest des Jahres geschätzt (schraffiert); die Zahlen stehen in der Tabelle darunter"`,
		"<title>Juni 2026: Erzeugt 290 kWh · Ins Netz 174 kWh · Netzbezug 58 kWh</title>",
		"<title>Oktober 2026: Erzeugt 133 kWh · Ins Netz 79 kWh · Netzbezug 41 kWh; dazu geschätzt: Erzeugt etwa 177 kWh · Ins Netz etwa 107 kWh · Netzbezug etwa 36 kWh</title>",
		"<g class=geschaetzt><title>November 2026, geschätzt: Erzeugt etwa 300 kWh · Ins Netz etwa 180 kWh · Netzbezug etwa 63 kWh</title>",
		`<rect class="schaetzung solar"`, `<rect class="schaetzung bezug"`, `<rect class="schaetzung einsp"`,
		"<line class=heute", ">heute</text>", "<li><i class=schaetzung></i>geschätzt</li>",
		"<tr><th scope=row>November 2026</th><td>etwa 300 kWh</td><td>etwa 180 kWh</td><td>etwa 63 kWh</td></tr>",
		"<tr><th scope=row>Oktober 2026</th><td>133 kWh + etwa 177 kWh</td><td>79 kWh + etwa 107 kWh</td><td>41 kWh + etwa 36 kWh</td></tr>")
	mustNotContain(t, fig, "class=vorjahr", "<th scope=col>Vorjahr</th>", "<th scope=row>Mai 2026</th>")
	if n := strings.Count(fig, `<rect class="schaetzung bezug"`); n != 3 {
		t.Fatalf("%d hatched Bezug bars, want October, November and December", n)
	}
	// the span's own numbers are measured only, and say so
	mustContain(t, section(t, body, "karte-das-jahr-2026-bis-heute"), "<tr><th scope=row>aus dem Netz</th><td>283 kWh</td></tr>")
	// the year-end figures: the chart's headline, right under it, no switch
	f := section(t, body, "karte-prognose-bis-jahresende-2026-geschaetzt")
	mustContain(t, f, "<li><span class=name>Netzbezug</span> <b class=zahl>etwa 447 kWh</b> <span class=k>bisher 281 kWh</span></li>",
		"<li><span class=name>Erzeugt</span> <b class=zahl>etwa 2.130 kWh</b>")
	before(t, body, "<figure class=verlauf>", "karte-prognose-bis-jahresende-2026-geschaetzt")
	before(t, body, "karte-prognose-bis-jahresende-2026-geschaetzt", "karte-dieser-monat-und-der-im-vorjahr")
	mustNotContain(t, body, "prognose=", "prognosebild", "<a href=\"/energie?zeitraum=jahr&amp;")

	// Monat: the days still to come hatched, by the same rule
	_, body, _ = familyPage(t, Energie, cfg, "GET", "/energie?zeitraum=monat", "family")
	fig = between(t, body, "<figure class=verlauf>", "</figure>")
	mustContain(t, fig, "<g class=geschaetzt><title>15. Oktober, geschätzt: Erzeugt etwa 10 kWh · Ins Netz etwa 6 kWh · Netzbezug etwa 2,1 kWh</title>",
		"<line class=heute", `<rect class="schaetzung bezug"`)
	mustContain(t, body, `aria-labelledby=karte-oktober-2026-bis-heute>`)
	before(t, body, "karte-dieser-monat-und-der-im-vorjahr", "karte-prognose-bis-jahresende-2026-geschaetzt")
	// Woche and Tag are what was, nothing estimated in them
	for _, z := range []string{"woche", "tag"} {
		_, body, _ = familyPage(t, Energie, cfg, "GET", "/energie?zeitraum="+z, "family")
		mustNotContain(t, between(t, body, "<figure class=verlauf>", "</figure>"), "geschätzt", "schaetzung", "class=heute")
	}
}

// With last year's days in HA the rest is last year's same days, and each
// month's column carries last year's total as a stroke.
func TestEnergieYearChartWithLastYear(t *testing.T) {
	fake, hc := house(t)
	loc := berlin(t)
	statDays(t, fake, "sensor.bezug", time.Date(2025, 1, 1, 0, 0, 0, 0, loc), time.Date(2026, 10, 14, 0, 0, 0, 0, loc),
		func(d time.Time) float64 { return float64(d.Year() - 2024) }, nil) // 1 a day in 2025, 2 in 2026
	cfg := StartConfig{HA: hc, Energy: energie.Map{Stats: energie.Stats{Bezug: "sensor.bezug"}}}
	_, body, _ := familyPage(t, Energie, cfg, "GET", "/energie?zeitraum=jahr", "family")
	fig := between(t, body, "<figure class=verlauf>", "</figure>")
	mustContain(t, fig, "<title>Januar 2026: Netzbezug 62 kWh · Vorjahr 31 kWh</title>",
		"<title>Oktober 2026: Netzbezug 26 kWh; dazu geschätzt: Netzbezug etwa 18 kWh · Vorjahr 31 kWh</title>",
		"<title>November 2026, geschätzt: Netzbezug etwa 30 kWh · Vorjahr 30 kWh</title>",
		`<path class="vorjahr rein" d="M`, "<li><i class=vorjahr></i>2025</li>", "<th scope=col>Vorjahr</th>",
		"<tr><th scope=row>November 2026</th><td>etwa 30 kWh</td><td>30 kWh</td></tr>")
	// last year is a line across the months, one stretch: all twelve there
	line := between(t, fig, `<path class="vorjahr rein" d="`, `"`)
	if m, l := strings.Count(line, "M"), strings.Count(line, "L"); m != 1 || l != 11 {
		t.Fatalf("last year's line %q: want one stretch through 12 months", line)
	}
	mustNotContain(t, fig, "<line class=vorjahr", "vorjahr einsp") // no Einspeisung here, no line for it
	// Monat: last year's same days, the same line
	_, body, _ = familyPage(t, Energie, cfg, "GET", "/energie?zeitraum=monat", "family")
	fig = between(t, body, "<figure class=verlauf>", "</figure>")
	mustContain(t, fig, "<title>15. Oktober, geschätzt: Netzbezug etwa 1 kWh · Vorjahr 1 kWh</title>")
	if l := strings.Count(between(t, fig, `<path class="vorjahr rein" d="`, `"`), "L"); l != 30 {
		t.Fatalf("%d steps in October's line, want 30", l)
	}
	mustContain(t, section(t, body, "karte-prognose-bis-jahresende-2026-geschaetzt"),
		`<b class=zahl>etwa 651 kWh</b> <span class="delta schlecht"><span aria-hidden=true>▲</span> +78 % · schlechter</span> <span class=k>bisher 572 kWh · 2025: 365 kWh</span>`)
}

// Last year's line has gaps where HA does not have the whole month: HA
// from 15 March 2025 on draws it from April; Einspeisung gets a line of
// its own below the zero line, and both are in the tooltip and the table.
func TestEnergieLastYearLineHasGaps(t *testing.T) {
	fake, hc := house(t)
	loc := berlin(t)
	from, to := time.Date(2025, 3, 15, 0, 0, 0, 0, loc), time.Date(2026, 10, 14, 0, 0, 0, 0, loc)
	statDays(t, fake, "sensor.bezug", from, to, func(time.Time) float64 { return 2 }, nil)
	statDays(t, fake, "sensor.einsp", from, to, func(time.Time) float64 { return 5 }, nil)
	statDays(t, fake, "sensor.solar", from, to, func(time.Time) float64 { return 8 }, nil)
	cfg := StartConfig{HA: hc, Energy: energie.Map{Stats: energie.Stats{Bezug: "sensor.bezug", Einspeisung: "sensor.einsp", Solar: "sensor.solar"}}}
	_, body, _ := familyPage(t, Energie, cfg, "GET", "/energie?zeitraum=jahr", "family")
	fig := between(t, body, "<figure class=verlauf>", "</figure>")
	for _, c := range []string{"rein", "einsp"} {
		line := between(t, fig, `<path class="vorjahr `+c+`" d="`, `"`)
		if m, l := strings.Count(line, "M"), strings.Count(line, "L"); m != 1 || l != 8 {
			t.Fatalf("%s line %q: want April to December, one stretch", c, line)
		}
	}
	// April 2025: came in 30 × (8 + 2) = 300, fed in 30 × 5
	mustContain(t, fig, "<title>April 2026: Erzeugt 240 kWh · Ins Netz 150 kWh · Netzbezug 60 kWh · Vorjahr 300 kWh, eingespeist 150 kWh</title>",
		"<th scope=col>Vorjahr</th><th scope=col>Vorjahr eingespeist</th>",
		"<tr><th scope=row>März 2026</th><td>248 kWh</td><td>155 kWh</td><td>62 kWh</td><td>–</td><td>–</td></tr>")
	mustContain(t, fig, "<title>März 2026: Erzeugt 248 kWh · Ins Netz 155 kWh · Netzbezug 62 kWh</title>")
}
