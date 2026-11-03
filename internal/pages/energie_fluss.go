package pages

import (
	"html"
	"math"
	"strconv"
	"strings"

	"github.com/mdopp/homes/internal/cards"
	"github.com/mdopp/homes/internal/energie"
)

// The flow picture (the owner, 2026-10-22: "Spinnen-Diagramme, die
// Eingang / Ausgang / Verbrauch / Erzeugung übersichtlich darstellen";
// 2026-11-03, the Android widget's second round: "Das Design kann gerne
// auch auf die Webseite"): Solar, Netz, Akku and Haus as pills spread
// over the width — Solar on top, Netz left, Akku right, Haus below — each
// with its number in bold and its name in its colour with what it does
// now; how full the battery is as a ring around its pill. Only the ways
// the power takes now are drawn, from the source's border to the
// destination's, as thick as they carry, the arrowhead at the
// destination pointing in. The same as a table for a screen reader. The
// history draws its span the same way, in kWh (owner, 2026-11-03): every
// way with a total, no battery ring (a charge is a moment, not a span),
// and the span's Autarkie under it.

// flussKnoten are the picture's nodes in the order its words name them.
var flussKnoten = []struct{ key, name string }{{"solar", "Solar"}, {"haus", "Haus"}, {"netz", "Netz"}, {"akku", "Akku"}}

// fluss is what the picture draws: which nodes the house has (da), which
// of them are known, why one is not, and the amounts — power in W now,
// or energy in kWh over a span.
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
	// how full the battery is now, in %: a ring around the Akku pill
	fuell    float64
	hasFuell bool
}

// kanten are the ways the power takes.
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
	return f
}

// flussSpanne is a span's amounts from its bars: what the sun made, what
// came from the grid and went into it; the house used Solar − Ins Netz +
// Netzbezug. The history reads no battery. A series the history has but
// this span has no value for is "keine Werte", and the house then is not
// known either.
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

// least is the smallest amount the picture draws: a watt now, 50 Wh over
// a span (it reads "0,1 kWh").
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

// kanten are the ways, when every node the house has is known: now, the
// sun into the house first, then into the battery, then into the grid;
// what the house needs beyond it from the battery, then from the grid,
// and a battery charging beyond the sun from the grid. Over a span the
// counters say it: what went into the grid came from the sun, the rest
// of the sun stayed, the grid's Bezug went into the house.
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
		k.solarHaus = f.solar - k.solarNetz
		k.netzHaus = f.bezug
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

// satz is what a pill says under its number: the node's name (drawn in
// its colour) with the words before and after it — "Solar erzeugt",
// "ins Netz", "Netz ruht".
type satz struct{ vor, name, nach string }

func (s satz) String() string { return s.vor + s.name + s.nach }

// knotenText is a pill's number and sentence; a node not known has no
// number, only its reason. The words read for a moment and for a span
// alike: "Solar erzeugt", "Haus verbraucht", "ins Netz"; over a span the
// grid's both ways are its number and the second number under the pill
// (zweit).
func (f fluss) knotenText(key, name string) (zahl string, s satz) {
	s.name = name
	if !f.known[key] {
		return "", s
	}
	switch key {
	case "solar":
		return f.words(f.solar), satz{"", name, " erzeugt"}
	case "haus":
		return f.words(f.haus), satz{"", name, " verbraucht"}
	case "netz":
		switch {
		case f.spanne && f.hasBezug:
			return f.words(f.bezug), satz{"aus dem ", name, ""}
		case f.spanne, f.einsp > 0:
			return f.words(f.einsp), satz{"ins ", name, ""}
		case f.bezug > 0:
			return f.words(f.bezug), satz{"aus dem ", name, ""}
		}
		return f.words(0), satz{"", name, " ruht"}
	case "akku":
		switch {
		case f.spanne && f.laden > 0:
			return f.words(f.laden), satz{"", name, " geladen"}
		case f.spanne && f.entladen > 0:
			return f.words(f.entladen), satz{"", name, " entladen"}
		case f.laden > 0:
			return f.words(f.laden), satz{"", name, " lädt"}
		case f.entladen > 0:
			return f.words(f.entladen), satz{"", name, " entlädt"}
		}
		return f.words(0), satz{"", name, " ruht"}
	}
	return "", s
}

// zweit is the grid's other way over a span, under its pill: what went
// into it when the pill says what came from it; "" otherwise.
func (f fluss) zweit() string {
	if f.spanne && f.known["netz"] && f.hasBezug && f.hasEinsp {
		return f.words(f.einsp) + " ins Netz"
	}
	return ""
}

// The picture's geometry, in the SVG's units (about a phone's pixels):
// pills of pillW × pillH; the battery's ring fuellAbstand outside its
// pill, fuellBreite wide; a way's air off a pill's border.
const (
	pillW, pillH = 124, 68
	fuellAbstand = 6
	fuellBreite  = 3.5
	wegLuft      = 4
)

type punkt struct{ x, y float64 }

// flussLage is where the pills sit: Solar on top, Netz at the left edge,
// Akku at the right edge, Haus below; without a battery a triangle,
// Netz below left, Haus below right; zweit leaves room for a line under
// the pills.
func flussLage(akku, zweit bool) (w, h int, at map[string]punkt) {
	extra := 0
	if zweit {
		extra = 22
	}
	if akku {
		return 396, 270 + extra, map[string]punkt{"solar": {198, 40}, "netz": {70, 135}, "akku": {324, 135}, "haus": {198, 230}}
	}
	return 396, 200 + extra, map[string]punkt{"solar": {198, 40}, "netz": {92, 160}, "haus": {304, 160}}
}

// reach is from a pill's centre to its border in the direction (ux, uy),
// a unit vector. A pill is a stadium: a rectangle with fully rounded ends
// (owner, 2026-11-03), straight for 2c in the middle, its ends half
// circles of radius r.
func reach(ux, uy float64) float64 {
	r := float64(pillH) / 2
	c := float64(pillW)/2 - r
	ax, ay := math.Abs(ux), math.Abs(uy)
	if ay > 0 && r/ay*ax <= c { // through the straight top or bottom
		return r / ay
	}
	// through an end's half circle, centred (±c, 0)
	return c*ax + math.Sqrt(c*c*ax*ax-c*c+r*r)
}

// stadium is the outline of a pill centred at p, grown by out on every
// side, as a path from the top's middle, clockwise.
func stadium(p punkt, out float64, n func(float64) string) string {
	r := float64(pillH)/2 + out
	c := float64(pillW)/2 - float64(pillH)/2
	return "M" + n(p.x) + " " + n(p.y-r) + "H" + n(p.x+c) + "A" + n(r) + " " + n(r) + " 0 0 1 " + n(p.x+c) + " " + n(p.y+r) +
		"H" + n(p.x-c) + "A" + n(r) + " " + n(r) + " 0 0 1 " + n(p.x-c) + " " + n(p.y-r) + "Z"
}

// weg is a way from von to nach: from the source pill's border to the
// destination's, along the line between their centres, a little off each
// (the battery's ring takes its own room). It never enters a pill; its
// end is where the arrow's tip sits.
func (f fluss) weg(von, nach string, at map[string]punkt) (punkt, punkt) {
	a, z := at[von], at[nach]
	dx, dy := z.x-a.x, z.y-a.y
	l := math.Hypot(dx, dy)
	ux, uy := dx/l, dy/l
	off := func(key string) float64 {
		if key == "akku" && f.hasFuell {
			return fuellAbstand + fuellBreite/2 + wegLuft
		}
		return wegLuft
	}
	s, e := reach(ux, uy)+off(von), reach(ux, uy)+off(nach)
	return punkt{a.x + ux*s, a.y + uy*s}, punkt{z.x - ux*e, z.y - uy*e}
}

// pfeilMass is the arrowhead's length along the way and its half width,
// by the way's width.
func pfeilMass(width float64) (long, half float64) { return 7 + width*0.8, 4.5 + width*0.6 }

// pfeil is the arrowhead of a way from a to z: its tip at z, the way's
// end by the destination's border, pointing into it; then its two back
// corners.
func pfeil(a, z punkt, width float64) [3]punkt {
	l := math.Hypot(z.x-a.x, z.y-a.y)
	ux, uy := (z.x-a.x)/l, (z.y-a.y)/l
	long, half := pfeilMass(width)
	bx, by := z.x-ux*long, z.y-uy*long
	return [3]punkt{z, {bx - uy*half, by + ux*half}, {bx + uy*half, by - ux*half}}
}

// flussWege are the ways, by their nodes, the colour they are drawn in
// (the source's: the sun's, the grid's, the battery's) and what they carry.
func flussWege(k kanten) []struct {
	von, nach string
	v         float64
} {
	return []struct {
		von, nach string
		v         float64
	}{
		{"solar", "haus", k.solarHaus}, {"solar", "akku", k.solarAkku}, {"solar", "netz", k.solarNetz},
		{"akku", "haus", k.akkuHaus}, {"netz", "haus", k.netzHaus}, {"netz", "akku", k.netzAkku},
	}
}

// flussHTML is the picture as a card — Jetzt, or a span of the history:
// the picture, a line when a node is not reachable, over a span its
// Autarkie, and the same as a table.
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
		zahl, s := f.knotenText(n.key, n.name)
		if !f.known[n.key] {
			zeilen = append(zeilen, zeile{n.name, f.off[n.key]})
			said = append(said, n.name+" "+f.off[n.key])
			continue
		}
		zeilen = append(zeilen, zeile{s.String(), zahl})
		said = append(said, s.String()+" "+zahl)
		if n.key == "netz" && f.zweit() != "" {
			zeilen = append(zeilen, zeile{"ins Netz", f.words(f.einsp)})
			said = append(said, "ins Netz "+f.words(f.einsp))
		}
		if n.key == "akku" && f.hasFuell {
			zeilen = append(zeilen, zeile{"Akku Füllstand", percent0(f.fuell)})
			said = append(said, "Akku "+percent0(f.fuell)+" voll")
		}
	}
	type aktiv struct {
		von, nach string
		v         float64
	}
	var aktive []aktiv
	var wege []string
	big := 0.0
	if ok {
		for _, w := range flussWege(k) {
			if w.v >= f.least() && f.da[w.von] && f.da[w.nach] {
				s := nameOf[w.von] + " → " + nameOf[w.nach]
				zeilen = append(zeilen, zeile{s, f.words(w.v)})
				wege = append(wege, s+" "+f.words(w.v))
				aktive = append(aktive, aktiv{w.von, w.nach, w.v})
				big = math.Max(big, w.v)
			}
		}
	}
	label := f.title + ": " + strings.Join(said, ", ") + "."
	if len(wege) > 0 {
		label += " " + strings.Join(wege, ", ") + "."
	}
	vw, vh, at := flussLage(f.da["akku"], f.zweit() != "")
	b.WriteString(`<div class=fluss-bild><svg viewBox="0 0 ` + strconv.Itoa(vw) + " " + strconv.Itoa(vh) + `" role=img aria-label="` + html.EscapeString(label) + `">`)
	n := func(v float64) string { return strconv.FormatFloat(math.Round(v*10)/10, 'f', -1, 64) }
	// only the ways the power takes now, in the source's colour
	for _, w := range aktive {
		a, z := f.weg(w.von, w.nach, at)
		width := 3 + 4*w.v/big
		// the line stops inside the head, so its round cap never pokes past the tip
		long, _ := pfeilMass(width)
		l := math.Hypot(z.x-a.x, z.y-a.y)
		t := math.Max(0, (l-long*0.6)/l)
		e := punkt{a.x + (z.x-a.x)*t, a.y + (z.y-a.y)*t}
		d := `d="M` + n(a.x) + " " + n(a.y) + "L" + n(e.x) + " " + n(e.y) + `"`
		h := pfeil(a, z, width)
		b.WriteString(`<g class="kante ` + w.von + "-" + w.nach + ` von-` + w.von + `"><title>` + nameOf[w.von] + " → " + nameOf[w.nach] + ": " + f.words(w.v) + "</title>")
		b.WriteString(`<path class=weg ` + d + ` stroke-width=` + n(width) + `></path>`)
		b.WriteString(`<path class=lauf ` + d + ` stroke-width=` + n(math.Max(1.5, width*0.4)) + `></path>`)
		b.WriteString(`<path class=pfeil d="M` + n(h[0].x) + " " + n(h[0].y) + "L" + n(h[1].x) + " " + n(h[1].y) + "L" + n(h[2].x) + " " + n(h[2].y) + `Z"></path></g>`)
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
		b.WriteString(`<g class="` + class + `"><rect class=pill x=` + n(p.x-pillW/2) + ` y=` + n(p.y-pillH/2) + ` width=` + strconv.Itoa(pillW) +
			` height=` + strconv.Itoa(pillH) + ` rx=` + n(pillH/2) + `></rect>`)
		if kn.key == "akku" && f.hasFuell {
			// how full: a ring along the pill's outline from the top, clockwise, and its number on it
			top := punkt{p.x, p.y - float64(pillH)/2 - fuellAbstand}
			ring := `d="` + stadium(p, fuellAbstand, n) + `"`
			b.WriteString(`<path class=fuell-spur ` + ring + `></path>`)
			b.WriteString(`<path class=fuell ` + ring + ` pathLength=100 stroke-dasharray="` + n(math.Min(100, math.Max(0, f.fuell))) + ` 100"></path>`)
			b.WriteString(`<rect class=fuell-schild x=` + n(top.x-21) + ` y=` + n(top.y-9) + ` width=42 height=18 rx=9></rect>`)
			b.WriteString(`<text class=fuell-zahl x=` + n(top.x) + ` y=` + n(top.y+4) + ` text-anchor=middle>` + percent0(f.fuell) + `</text>`)
		}
		zahl, s := f.knotenText(kn.key, kn.name)
		if !f.known[kn.key] {
			b.WriteString(`<text class=wort x=` + n(p.x) + ` y=` + n(p.y-2) + ` text-anchor=middle><tspan class=nm>` + kn.name + `</tspan></text>`)
			b.WriteString(`<text class=grund x=` + n(p.x) + ` y=` + n(p.y+15) + ` text-anchor=middle>` + html.EscapeString(f.off[kn.key]) + `</text>`)
		} else {
			b.WriteString(`<text class=zahl x=` + n(p.x) + ` y=` + n(p.y-1) + ` text-anchor=middle>` + html.EscapeString(zahl) + `</text>`)
			b.WriteString(`<text class=wort x=` + n(p.x) + ` y=` + n(p.y+16) + ` text-anchor=middle>` + html.EscapeString(s.vor) +
				`<tspan class=nm>` + s.name + `</tspan>` + html.EscapeString(s.nach) + `</text>`)
			if z := f.zweit(); kn.key == "netz" && z != "" {
				b.WriteString(`<text class=zweit x=` + n(p.x) + ` y=` + n(p.y+pillH/2+18) + ` text-anchor=middle>` + html.EscapeString(z) + `</text>`)
			}
		}
		b.WriteString("</g>")
	}
	b.WriteString("</svg>")
	if f.spanne && ok && f.da["solar"] && f.da["netz"] {
		// the span's Autarkie: of what the house used, the share not from the grid
		if in := k.solarHaus + k.akkuHaus + k.netzHaus; in >= f.least() {
			b.WriteString("<p class=autarkie><b>" + share((k.solarHaus+k.akkuHaus)/in) + "</b> Autarkie</p>")
		}
	}
	b.WriteString("</div><details><summary>Als Tabelle</summary><table><tbody>")
	for _, z := range zeilen {
		b.WriteString("<tr><th scope=row>" + html.EscapeString(z.th) + "</th><td>" + html.EscapeString(z.td) + "</td></tr>")
	}
	b.WriteString("</tbody></table></details></section>")
	return b.String()
}

// spaltenZeile is one row of a number column: the number, what it is, and
// the series colour it is drawn in ("" the ink's).
type spaltenZeile struct{ zahl, name, farbe string }

// spalteHTML is a number column as a card, the widget's (owner,
// 2026-11-03): the title centred above, each row its number first and its
// name second, on one vertical axis — the numbers end on it, the names
// start from it. note is one plain line instead of the rows.
func spalteHTML(title string, rows []spaltenZeile, note string) string {
	if len(rows) == 0 && note == "" {
		return ""
	}
	id := cards.Card{Title: title}.ID()
	var b strings.Builder
	b.WriteString(`<section class="karte spalte" aria-labelledby=` + id + `><h2 id=` + id + `>` + html.EscapeString(title) + `</h2>`)
	if note != "" {
		b.WriteString("<p class=k>" + html.EscapeString(note) + "</p></section>")
		return b.String()
	}
	b.WriteString("<table><tbody>")
	for _, r := range rows {
		class := "zahl"
		if r.farbe != "" {
			class = `"zahl ` + r.farbe + `"`
		}
		b.WriteString("<tr><td class=" + class + ">" + html.EscapeString(r.zahl) + "</td><th scope=row>" + html.EscapeString(r.name) + "</th></tr>")
	}
	b.WriteString("</tbody></table></section>")
	return b.String()
}

// mengen are amounts over a span in kWh by series (solar, einsp, bezug),
// only those read.
type mengen map[string]float64

// spaltenZeilen are a span's rows: Erzeugt, Ins Netz, Netzbezug, each when
// read, and Autarkie when sure is set and all three are read — the share
// of what the house used (made − fed in + drawn, + from the battery −
// into it, when the house has battery counters) that did not come from
// the grid.
func spaltenZeilen(m mengen, akku float64, sure bool) []spaltenZeile {
	var rows []spaltenZeile
	for _, s := range energySeries {
		if v, ok := m[s.key]; ok {
			rows = append(rows, spaltenZeile{energyWords(v), s.name, s.key})
		}
	}
	solar, ok1 := m["solar"]
	einsp, ok2 := m["einsp"]
	bezug, ok3 := m["bezug"]
	if sure && ok1 && ok2 && ok3 {
		if used := solar - einsp + bezug + akku; used >= 0.05 {
			rows = append(rows, spaltenZeile{share(math.Min(1, math.Max(0, (used-bezug)/used))), "Autarkie", ""})
		}
	}
	return rows
}

// percent0 is a percent value as a whole number: "74 %".
func percent0(v float64) string { return strconv.Itoa(int(math.Round(v))) + " %" }

// share is a share as whole percent: "63 %".
func share(v float64) string { return strconv.Itoa(int(math.Round(v*100))) + " %" }
