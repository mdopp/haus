package pages

import (
	"html"
	"math"
	"strconv"
	"strings"

	"github.com/mdopp/haus/internal/cards"
	"github.com/mdopp/haus/internal/energie"
)

// The flow picture (the owner, 2026-10-22: "Spinnen-Diagramme, die
// Eingang / Ausgang / Verbrauch / Erzeugung übersichtlich darstellen"):
// Solar, Netz, Akku and Haus as round nodes, each with its number and a
// word, and the ways the power takes between them, as thick as they
// carry, an arrow saying where to. Jetzt draws the power now; the
// history draws the chosen span's amounts. Under it Autarkie and
// Eigenverbrauchsquote; the same as a table for a screen reader.

// flussKnoten are the picture's nodes in the order its words name them.
var flussKnoten = []struct{ key, name string }{{"solar", "Solar"}, {"haus", "Haus"}, {"netz", "Netz"}, {"akku", "Akku"}}

// fluss is what the picture draws: which nodes the house has (da), which
// of them are known now, why one is not, and the amounts — power in W
// now, or energy in kWh over a span.
type fluss struct {
	title                                      string
	spanne                                     bool
	da                                         map[string]bool
	known                                      map[string]bool
	off                                        map[string]string // why a node is not known: "nicht erreichbar"
	solar, haus, bezug, einsp, laden, entladen float64
	// over a span the grid has both ways; hasBezug/hasEinsp say which the
	// history reads
	hasBezug, hasEinsp bool
	// how full the battery is now, in %: shown with the Akku node
	fuell    float64
	hasFuell bool
}

// kanten are the ways the power took.
type kanten struct{ solarHaus, solarAkku, solarNetz, netzHaus, netzAkku, akkuHaus float64 }

// flussJetzt is the power now, from the flow legs: grid +W drawn −W fed
// in, battery +W charging −W discharging; under a watt is resting.
func flussJetzt(legs []energie.Leg) fluss {
	f := fluss{title: "Jetzt", da: map[string]bool{}, known: map[string]bool{}, off: map[string]string{}}
	node := map[string]string{"pv": "solar", "house": "haus", "grid": "netz", "battery": "akku"}
	for _, l := range legs {
		k := node[l.Key]
		if k == "" {
			continue
		}
		f.da[k] = true
		w, ok := watts(l.State)
		switch {
		case l.State.State == "unavailable":
			f.off[k] = "nicht erreichbar"
			continue
		case !ok:
			f.off[k] = "unbekannt"
			continue
		}
		f.known[k] = true
		switch k {
		case "solar":
			f.solar = math.Max(0, w)
		case "haus":
			f.haus = math.Max(0, w)
		case "netz":
			switch sign(w) {
			case 1:
				f.bezug = w
			case -1:
				f.einsp = -w
			}
		case "akku":
			switch sign(w) {
			case 1:
				f.laden = w
			case -1:
				f.entladen = -w
			}
		}
	}
	f.hasBezug, f.hasEinsp = true, true
	return f
}

// flussSpanne is a span's amounts from its bars: what the sun made, what
// came from the grid and went into it; the house used Solar − Einspeisung
// + Bezug. A series the history has but this span has no value for is
// "keine Werte", and the house then is not known either.
func flussSpanne(title string, bs []bucket, daily, hourly amounts) fluss {
	f := fluss{title: title, spanne: true, da: map[string]bool{}, known: map[string]bool{}, off: map[string]string{}}
	sum := map[string]float64{}
	has := map[string]bool{}
	for _, x := range bs {
		for k, v := range x.v {
			sum[k] += v
			has[k] = true
		}
	}
	named := func(k string) bool { return daily[k] != nil || hourly[k] != nil }
	f.hasBezug, f.hasEinsp = named("bezug"), named("einsp")
	f.da["solar"], f.da["netz"] = named("solar"), f.hasBezug || f.hasEinsp
	f.da["haus"] = f.da["solar"] || f.da["netz"]
	f.known["solar"] = has["solar"]
	f.known["netz"] = (!f.hasBezug || has["bezug"]) && (!f.hasEinsp || has["einsp"])
	f.known["haus"] = (!f.da["solar"] || f.known["solar"]) && (!f.da["netz"] || f.known["netz"])
	for _, k := range []string{"solar", "netz", "haus"} {
		if f.da[k] && !f.known[k] {
			f.off[k] = "keine Werte"
		}
	}
	f.solar, f.bezug, f.einsp = sum["solar"], sum["bezug"], sum["einsp"]
	f.haus = math.Max(0, f.solar-f.einsp+f.bezug)
	return f
}

// kanten are the ways, when every node the house has is known: now, the
// sun into the house first, then into the battery, then into the grid;
// what the house needs beyond it from the battery, then from the grid,
// and a battery charging beyond the sun from the grid. Over a span the
// counters say it: what went into the grid came from the sun, the rest
// of the sun stayed (the battery first), the grid's Bezug went into the
// house.
func (f fluss) kanten() (kanten, bool) {
	if !f.da["haus"] {
		return kanten{}, false
	}
	for k, da := range f.da {
		if da && !f.known[k] {
			return kanten{}, false
		}
	}
	var k kanten
	if f.spanne {
		k.solarNetz = math.Min(f.solar, f.einsp)
		kept := f.solar - k.solarNetz
		k.solarAkku = math.Min(f.laden, kept)
		k.solarHaus = kept - k.solarAkku
		k.netzAkku = math.Max(0, math.Min(f.bezug, f.laden-k.solarAkku))
		k.netzHaus = f.bezug - k.netzAkku
		k.akkuHaus = f.entladen
		return k, true
	}
	rest := f.solar
	k.solarHaus = math.Min(rest, f.haus)
	rest -= k.solarHaus
	k.solarAkku = math.Min(rest, f.laden)
	rest -= k.solarAkku
	k.solarNetz = math.Min(rest, f.einsp)
	need := f.haus - k.solarHaus
	k.akkuHaus = math.Min(need, f.entladen)
	need -= k.akkuHaus
	k.netzHaus = math.Min(need, f.bezug)
	k.netzAkku = math.Max(0, math.Min(f.laden-k.solarAkku, f.bezug-k.netzHaus))
	return k, true
}

// autarkie is the share of what came into the house that did not come
// from the grid; eigenquote the share of the sun's power the house kept
// (itself or in the battery). Neither without the ways, nor without
// anything to share.
func (f fluss) autarkie(k kanten) (float64, bool) {
	in := k.solarHaus + k.akkuHaus + k.netzHaus
	if in < f.least() {
		return 0, false
	}
	return (k.solarHaus + k.akkuHaus) / in, true
}

func (f fluss) eigenquote(k kanten) (float64, bool) {
	if !f.da["solar"] || f.solar < f.least() {
		return 0, false
	}
	return (k.solarHaus + k.solarAkku) / f.solar, true
}

// least is the smallest amount the picture draws: a watt now, 50 Wh over
// a span.
func (f fluss) least() float64 {
	if f.spanne {
		return 0.05
	}
	return 1
}

// words is an amount the picture's way: W/kW now, kWh over a span.
func (f fluss) words(v float64) string {
	if f.spanne {
		return energyWords(v)
	}
	return powerWords(v)
}

// knotenText is a node's number and word, and for the grid over a span
// the other way as a second number and word.
func (f fluss) knotenText(key string) (zahl, wort, zweitZahl, zweitWort string) {
	if !f.known[key] {
		return "–", f.off[key], "", ""
	}
	switch key {
	case "solar":
		return f.words(f.solar), "erzeugt", "", ""
	case "haus":
		return f.words(f.haus), "verbraucht", "", ""
	case "netz":
		switch {
		case f.spanne && f.hasBezug && f.hasEinsp:
			return f.words(f.bezug), "bezogen", f.words(f.einsp), "eingespeist"
		case f.spanne && f.hasEinsp:
			return f.words(f.einsp), "eingespeist", "", ""
		case f.spanne, f.bezug > 0:
			return f.words(f.bezug), "bezogen", "", ""
		case f.einsp > 0:
			return f.words(f.einsp), "eingespeist", "", ""
		}
		return f.words(0), "ruht", "", ""
	case "akku":
		switch {
		case f.laden > 0:
			return f.words(f.laden), "lädt", "", ""
		case f.entladen > 0:
			return f.words(f.entladen), "entlädt", "", ""
		}
		return f.words(0), "ruht", "", ""
	}
	return "", "", "", ""
}

// The picture's geometry, in the SVG's units (about a phone's pixels).
const flussR = 42

type punkt struct{ x, y float64 }

// flussLage is where the nodes sit: Solar on top, Netz left, Akku right,
// Haus below; without a battery a triangle, Haus on the right.
func flussLage(akku bool) (w, h int, at map[string]punkt) {
	if akku {
		return 360, 356, map[string]punkt{"solar": {180, 70}, "netz": {62, 180}, "akku": {298, 180}, "haus": {180, 282}}
	}
	return 360, 300, map[string]punkt{"solar": {180, 70}, "netz": {82, 226}, "haus": {278, 226}}
}

// flussWege are the ways, by their nodes, the series colour they are
// drawn in (Eigenverbrauch, Einspeisung, Bezug, the battery) and what
// they carried.
func flussWege(k kanten) []struct {
	von, nach, farbe string
	v                float64
} {
	return []struct {
		von, nach, farbe string
		v                float64
	}{
		{"solar", "haus", "eigen", k.solarHaus}, {"solar", "akku", "eigen", k.solarAkku}, {"solar", "netz", "einsp", k.solarNetz},
		{"akku", "haus", "akku", k.akkuHaus}, {"netz", "haus", "bezug", k.netzHaus}, {"netz", "akku", "bezug", k.netzAkku},
	}
}

// flussHTML is the picture as a card: the SVG, the key figures, a line
// when a node is not known, and the same as a table.
func flussHTML(f fluss) string {
	any := false
	for _, da := range f.da {
		any = any || da
	}
	if !any {
		return ""
	}
	k, ok := f.kanten()
	nameOf := map[string]string{}
	for _, n := range flussKnoten {
		nameOf[n.key] = n.name
	}
	id := cards.Card{Title: f.title}.ID()
	var b strings.Builder
	b.WriteString(`<section class="karte fluss" aria-labelledby=` + id + `><h2 id=` + id + `>` + html.EscapeString(f.title) + `</h2>`)
	for _, n := range flussKnoten {
		if f.da[n.key] && f.off[n.key] == "nicht erreichbar" {
			b.WriteString("<p class=hinweis>" + n.name + " nicht erreichbar: wohin der Strom fließt, zeigt das Bild wieder, sobald " +
				map[string]string{"solar": "die Anlage", "haus": "der Zähler", "netz": "der Zähler", "akku": "er"}[n.key] + " antwortet.</p>")
			break
		}
	}
	// the words: each node, then each way
	type zeile struct{ th, td string }
	var zeilen []zeile
	var said []string
	for _, n := range flussKnoten {
		if !f.da[n.key] {
			continue
		}
		zahl, wort, zz, zw := f.knotenText(n.key)
		if !f.known[n.key] {
			zeilen = append(zeilen, zeile{n.name, wort})
			said = append(said, n.name+" "+wort)
			continue
		}
		if n.key == "akku" && f.hasFuell {
			zeilen = append(zeilen, zeile{"Akku Füllstand", percent0(f.fuell)})
			said = append(said, "Akku "+percent0(f.fuell)+" voll")
		}
		zeilen = append(zeilen, zeile{n.name + " " + wort, zahl})
		said = append(said, n.name+" "+wort+" "+zahl)
		if zz != "" {
			zeilen = append(zeilen, zeile{n.name + " " + zw, zz})
			said = append(said, n.name+" "+zw+" "+zz)
		}
	}
	var wege []string
	big := math.Max(math.Max(f.solar, f.haus), math.Max(math.Max(f.bezug, f.einsp), math.Max(f.laden, f.entladen)))
	if ok {
		for _, w := range flussWege(k) {
			if w.v >= f.least() && f.da[w.von] && f.da[w.nach] {
				s := nameOf[w.von] + " → " + nameOf[w.nach]
				zeilen = append(zeilen, zeile{s, f.words(w.v)})
				wege = append(wege, s+" "+f.words(w.v))
			}
		}
	}
	label := f.title + ": " + strings.Join(said, ", ") + "."
	if len(wege) > 0 {
		label += " " + strings.Join(wege, ", ") + "."
	}
	vw, vh, at := flussLage(f.da["akku"])
	b.WriteString(`<div class=fluss-bild><svg viewBox="0 0 ` + strconv.Itoa(vw) + " " + strconv.Itoa(vh) + `" role=img aria-label="` + html.EscapeString(label) + `">`)
	n := func(v float64) string { return strconv.FormatFloat(math.Round(v*10)/10, 'f', -1, 64) }
	// the tracks first: every way the house could have, quiet
	for _, w := range flussWege(kanten{}) {
		if !f.da[w.von] || !f.da[w.nach] || (w.von == "netz" && w.nach == "akku") {
			continue
		}
		a, z := ends(at[w.von], at[w.nach])
		b.WriteString(`<path class=spur d="M` + n(a.x) + " " + n(a.y) + "L" + n(z.x) + " " + n(z.y) + `"></path>`)
	}
	if ok {
		for _, w := range flussWege(k) {
			if w.v < f.least() || !f.da[w.von] || !f.da[w.nach] {
				continue
			}
			a, z := ends(at[w.von], at[w.nach])
			width := 2.5
			if big > 0 {
				width += 9 * w.v / big
			}
			d := `d="M` + n(a.x) + " " + n(a.y) + "L" + n(z.x) + " " + n(z.y) + `"`
			b.WriteString(`<g class="kante ` + w.von + "-" + w.nach + `"><title>` + nameOf[w.von] + " → " + nameOf[w.nach] + ": " + f.words(w.v) + "</title>")
			b.WriteString(`<path class="weg ` + w.farbe + `" ` + d + ` stroke-width=` + n(width) + `></path>`)
			b.WriteString(`<path class="lauf ` + w.farbe + `" ` + d + ` stroke-width=` + n(math.Max(1.5, width*0.45)) + `></path>`)
			// the arrow at the way's middle, pointing where the power goes
			dx, dy := z.x-a.x, z.y-a.y
			l := math.Hypot(dx, dy)
			ux, uy := dx/l, dy/l
			mx, my := (a.x+z.x)/2, (a.y+z.y)/2
			long, half := 7+width*0.7, 4.5+width*0.55
			tip := punkt{mx + ux*long/2, my + uy*long/2}
			back := punkt{mx - ux*long/2, my - uy*long/2}
			b.WriteString(`<path class="pfeil ` + w.farbe + `" d="M` + n(tip.x) + " " + n(tip.y) + "L" + n(back.x-uy*half) + " " + n(back.y+ux*half) +
				"L" + n(back.x+uy*half) + " " + n(back.y-ux*half) + `Z"></path></g>`)
		}
	}
	for _, kn := range flussKnoten {
		if !f.da[kn.key] {
			continue
		}
		p := at[kn.key]
		class := "knoten " + kn.key
		if !f.known[kn.key] {
			class += " aus"
		}
		zahl, wort, zz, zw := f.knotenText(kn.key)
		b.WriteString(`<g class="` + class + `"><circle cx=` + n(p.x) + ` cy=` + n(p.y) + ` r=` + strconv.Itoa(flussR) + `></circle>`)
		nameY := p.y - flussR - 10
		if kn.key == "haus" && f.da["akku"] {
			nameY = p.y + flussR + 20
		}
		name := kn.name
		if kn.key == "akku" && f.hasFuell {
			// how full: in its name, and as a ring around it from the top, clockwise
			name += " " + percent0(f.fuell)
			ring := 2 * math.Pi * float64(flussR+5)
			b.WriteString(`<circle class=fuell cx=` + n(p.x) + ` cy=` + n(p.y) + ` r=` + strconv.Itoa(flussR+5) +
				` stroke-dasharray="` + n(ring*f.fuell/100) + ` ` + n(ring) + `" transform="rotate(-90 ` + n(p.x) + ` ` + n(p.y) + `)"></circle>`)
		}
		b.WriteString(`<text class=name x=` + n(p.x) + ` y=` + n(nameY) + ` text-anchor=middle>` + name + `</text>`)
		zahlClass := "zahl"
		switch n := len([]rune(zahl)); {
		case !f.known[kn.key]:
			zahlClass = "zahl leer"
		case n > 8: // "6.513 kWh" still fits the circle
			zahlClass = "zahl lang"
		}
		b.WriteString(`<text class="` + zahlClass + `" x=` + n(p.x) + ` y=` + n(p.y+3) + ` text-anchor=middle>` + html.EscapeString(zahl) + `</text>`)
		b.WriteString(`<text class=wort x=` + n(p.x) + ` y=` + n(p.y+20) + ` text-anchor=middle>` + html.EscapeString(wort) + `</text>`)
		if zz != "" {
			b.WriteString(`<text class=zweit x=` + n(p.x) + ` y=` + n(p.y+flussR+18) + ` text-anchor=middle>` + html.EscapeString(zz+" "+zw) + `</text>`)
		}
		b.WriteString("</g>")
	}
	b.WriteString("</svg>")
	// the key figures, report-style
	var kz strings.Builder
	if ok {
		if a, has := f.autarkie(k); has {
			kz.WriteString("<div><dt>Autarkie</dt><dd class=zahl>" + share(a) + "</dd><dd class=k>des Verbrauchs ohne das Netz gedeckt</dd></div>")
		}
		if q, has := f.eigenquote(k); has {
			kz.WriteString("<div><dt>Eigenverbrauchsquote</dt><dd class=zahl>" + share(q) + "</dd><dd class=k>des Solarstroms selbst genutzt</dd></div>")
		}
	}
	if kz.Len() > 0 {
		b.WriteString("<dl class=kennzahlen>" + kz.String() + "</dl>")
	}
	b.WriteString("</div><details><summary>Als Tabelle</summary><table><tbody>")
	for _, z := range zeilen {
		b.WriteString("<tr><th scope=row>" + html.EscapeString(z.th) + "</th><td>" + html.EscapeString(z.td) + "</td></tr>")
	}
	b.WriteString("</tbody></table></details></section>")
	return b.String()
}

// ends are a way's ends, on the edge of each node's circle, a little off.
func ends(a, z punkt) (punkt, punkt) {
	dx, dy := z.x-a.x, z.y-a.y
	l := math.Hypot(dx, dy)
	off := float64(flussR) + 5
	return punkt{a.x + dx/l*off, a.y + dy/l*off}, punkt{z.x - dx/l*off, z.y - dy/l*off}
}

// percent0 is a percent value as a whole number: "74 %".
func percent0(v float64) string { return strconv.Itoa(int(math.Round(v))) + "\u00a0%" }

// share is a share as whole percent: "63 %".
func share(v float64) string { return strconv.Itoa(int(math.Round(v*100))) + " %" }
