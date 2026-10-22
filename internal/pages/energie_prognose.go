package pages

import (
	"html"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mdopp/haus/internal/cards"
)

// The forecast as a chart (the owner, 2026-10-22: "ein Diagramm, das die
// weiteren Monate prognostiziert darstellt"): this year month by month,
// what was measured solid, the rest hatched and marked geschätzt, last
// year's months as a stroke; the year-end figures above it, each a link
// that picks the chart's series. The estimate is prognose()'s of haus#65,
// month by month: the rest of the year as the same days came last year
// when HA has them, else at the daily mean so far.

// progMonat is one month of the forecast: what was measured (until
// yesterday), what is estimated (from today), last year's month.
type progMonat struct {
	ist, schaetz, vorjahr          float64
	hasIst, hasSchaetz, hasVorjahr bool
}

// progReihe is one series' forecast: its months, what came so far, the
// estimated rest, from when it counts, how it estimates, and last
// year's total when HA has all of last year.
type progReihe struct {
	key, name    string
	m            [12]progMonat
	bisher, rest float64
	since, first time.Time
	wieVorjahr   bool
	vorjahr      float64
	hasVorjahr   bool
}

// prognoseReihe is a series' forecast; false when it has nothing this
// year yet. Only whole days count: today is not over.
func prognoseReihe(today, yearStart time.Time, loc *time.Location, key, name string, days map[int64]float64) (progReihe, bool) {
	first, ok := firstDay(days, loc)
	if !ok {
		return progReihe{}, false
	}
	since := yearStart
	if first.After(since) {
		since = first
	}
	if !since.Before(today) {
		return progReihe{}, false // nothing this year yet
	}
	r := progReihe{key: key, name: name, since: since, first: first}
	r.bisher, _ = sumDays(days, since, today)
	r.wieVorjahr = !back(today, 1, 0, false).Before(first)
	n := math.Round(today.Sub(since).Hours() / 24)
	later := func(a, b time.Time) time.Time {
		if a.After(b) {
			return a
		}
		return b
	}
	earlier := func(a, b time.Time) time.Time {
		if a.Before(b) {
			return a
		}
		return b
	}
	for i := range r.m {
		ms := time.Date(yearStart.Year(), time.Month(i+1), 1, 0, 0, 0, 0, loc)
		me := ms.AddDate(0, 1, 0)
		m := &r.m[i]
		if a, z := later(ms, since), earlier(me, today); a.Before(z) {
			m.ist, _ = sumDays(days, a, z)
			m.hasIst = true
		}
		if a := later(ms, today); a.Before(me) {
			if r.wieVorjahr {
				m.schaetz, _ = sumDays(days, back(a, 1, 0, false), back(me, 1, 0, true))
			} else {
				m.schaetz = r.bisher / n * math.Round(me.Sub(a).Hours()/24)
			}
			m.hasSchaetz = true
			r.rest += m.schaetz
		}
		if ls := ms.AddDate(-1, 0, 0); !ls.Before(first) { // HA has all of last year's month
			m.vorjahr, _ = sumDays(days, ls, ls.AddDate(0, 1, 0))
			m.hasVorjahr = true
		}
	}
	if ly := yearStart.AddDate(-1, 0, 0); !ly.Before(first) {
		r.vorjahr, _ = sumDays(days, ly, yearStart)
		r.hasVorjahr = true
	}
	return r, true
}

// vergleichAb is the first year whose end can be held against a whole
// previous year: two years after the first, one when HA starts on a 1
// January.
func (r progReihe) vergleichAb() int {
	y, m, d := r.first.Date()
	if m == time.January && d == 1 {
		return y + 1
	}
	return y + 2
}

// prognoseOf is the series the forecast chart draws (?prognose=…); ""
// when it names none the page knows.
func prognoseOf(r *http.Request) string {
	v := r.URL.Query().Get("prognose")
	for _, s := range energySeries {
		if s.key == v {
			return v
		}
	}
	return ""
}

// prognoseHTML is the forecast card: the year-end figures (each picks
// the chart), one line on how it is estimated, the chart, the table.
func prognoseHTML(view, which string, today, yearStart time.Time, loc *time.Location, daily amounts) string {
	if !yearStart.Before(today) {
		return ""
	}
	var reihen []progReihe
	for _, s := range energySeries {
		if r, ok := prognoseReihe(today, yearStart, loc, s.key, s.name, daily[s.key]); ok {
			reihen = append(reihen, r)
		}
	}
	if len(reihen) == 0 {
		return ""
	}
	if which == "" {
		which = "bezug"
	}
	sel := reihen[0]
	for _, r := range reihen {
		if r.key == which {
			sel = r
		}
	}
	year := today.Year()
	title := "Prognose bis Jahresende " + strconv.Itoa(year) + " (geschätzt)"
	id := cards.Card{Title: title}.ID()
	var b strings.Builder
	b.WriteString(`<section class="karte prognose" id=prognose aria-labelledby=` + id + `><h2 id=` + id + `>` + html.EscapeString(title) + `</h2>`)
	how := "Bis gestern gemessen, der Rest geschätzt "
	if sel.wieVorjahr {
		how += "wie im Vorjahr"
	} else {
		how += "nach dem Tagesmittel bisher"
	}
	if sel.since.After(yearStart) {
		how += " · gezählt ab dem " + strconv.Itoa(sel.since.Day()) + ". " + months[sel.since.Month()-1]
	}
	if !sel.hasVorjahr {
		how += " · Vergleich mit dem Vorjahr ab " + strconv.Itoa(sel.vergleichAb())
	}
	b.WriteString("<p class=hinweis>" + how + "</p><div class=prognose-inhalt><ul class=\"zahlen wahl\">")
	for _, r := range reihen {
		b.WriteString(`<li><a href="/energie?zeitraum=` + view + `&amp;prognose=` + r.key + `#prognose"`)
		if r.key == sel.key {
			b.WriteString(" aria-current=true")
		}
		total := r.bisher + r.rest
		b.WriteString("><span class=name>" + r.name + "</span> <b class=zahl>etwa " + roughWords(total) + "</b>")
		detail := "bisher " + energyWords(r.bisher)
		if r.hasVorjahr {
			b.WriteString(" " + deltaChip(r.key, total, r.vorjahr))
			detail += " · " + strconv.Itoa(year-1) + ": " + energyWords(r.vorjahr)
		}
		b.WriteString(" <span class=k>" + detail + "</span></a></li>")
	}
	b.WriteString("</ul>" + prognoseChart(sel, today, loc) + "</div></section>")
	return b.String()
}

// The forecast chart's geometry, in the SVG's units (a phone's width).
const (
	progW, progH = 400, 206
	progL, progR = 58, 6
	progT, progB = 24, 22
)

// prognoseChart draws one series' months: measured solid, estimated
// hatched on top, last year a stroke, today a dashed line. Each month
// names its numbers on hover; the table says the same.
func prognoseChart(r progReihe, today time.Time, loc *time.Location) string {
	year := today.Year()
	top, vorjahr := 0.0, false
	for _, m := range r.m {
		top = math.Max(top, math.Max(m.ist+m.schaetz, m.vorjahr))
		vorjahr = vorjahr || m.hasVorjahr
	}
	if top == 0 {
		top = 1
	}
	plotH := float64(progH - progT - progB)
	scale := plotH / top
	base := float64(progH - progB)
	step := float64(progW-progL-progR) / 12
	barW := step * 0.62
	f := func(v float64) string { return strconv.FormatFloat(math.Round(v*10)/10, 'f', -1, 64) }
	label := r.name + " " + strconv.Itoa(year) + " Monat für Monat: gemessen bis gestern, danach geschätzt"
	if vorjahr {
		label += ", das Vorjahr als Strich"
	}
	var b strings.Builder
	b.WriteString(`<figure class="prognosebild ` + r.key + `"><svg viewBox="0 0 ` + strconv.Itoa(progW) + " " + strconv.Itoa(progH) + `" role=img aria-label="` +
		html.EscapeString(label+"; die Zahlen stehen in der Tabelle darunter") + `">`)
	b.WriteString(`<defs><pattern id=schraffur width=6 height=6 patternUnits=userSpaceOnUse patternTransform="rotate(45)"><rect class=strich width=2.4 height=6></rect></pattern></defs>`)
	b.WriteString(`<line class=gitter x1=` + strconv.Itoa(progL) + ` x2=` + strconv.Itoa(progW-progR) + ` y1=` + strconv.Itoa(progT) + ` y2=` + strconv.Itoa(progT) + `></line>`)
	b.WriteString(`<text class=achse x=` + strconv.Itoa(progL-6) + ` y=` + strconv.Itoa(progT+4) + ` text-anchor=end>` + shortKWh(top) + `</text>`)
	// today: where in its month
	mi := int(today.Month()) - 1
	ms := time.Date(year, today.Month(), 1, 0, 0, 0, 0, loc)
	frac := today.Sub(ms).Hours() / ms.AddDate(0, 1, 0).Sub(ms).Hours()
	hx := float64(progL) + step*(float64(mi)+frac)
	b.WriteString(`<line class=heute x1=` + f(hx) + ` x2=` + f(hx) + ` y1=` + strconv.Itoa(progT-6) + ` y2=` + f(base) + `></line>`)
	b.WriteString(`<text class=heute-text x=` + f(math.Min(math.Max(hx, progL+14), progW-progR-14)) + ` y=` + strconv.Itoa(progT-10) + ` text-anchor=middle>heute</text>`)
	var rows strings.Builder
	for i, m := range r.m {
		cx := float64(progL) + step*float64(i) + step/2
		left := cx - barW/2
		name := months[i] + " " + strconv.Itoa(year)
		class, tip := "leer", "keine Werte"
		switch {
		case m.hasIst && m.hasSchaetz:
			class, tip = "teils", energyWords(m.ist)+" gemessen, dazu etwa "+roughWords(m.schaetz)+" geschätzt"
		case m.hasIst:
			class, tip = "gemessen", energyWords(m.ist)+" gemessen"
		case m.hasSchaetz:
			class, tip = "geschaetzt", "etwa "+roughWords(m.schaetz)+" geschätzt"
		}
		if m.hasVorjahr {
			tip += " · Vorjahr " + energyWords(m.vorjahr)
		}
		b.WriteString(`<g class="monat ` + class + `"><title>` + html.EscapeString(name+": "+tip) + `</title>`)
		b.WriteString(`<rect class=treffer x=` + f(cx-step/2) + ` y=` + strconv.Itoa(progT) + ` width=` + f(step) + ` height=` + f(plotH) + `></rect>`)
		y := base
		if h := m.ist * scale; m.hasIst && h >= chartMinVisible {
			b.WriteString(`<rect class="ist ` + r.key + `" x=` + f(left) + ` y=` + f(y-h) + ` width=` + f(barW) + ` height=` + f(h) + ` rx=2></rect>`)
			y -= h + chartGap
		}
		if h := m.schaetz * scale; m.hasSchaetz && h >= chartMinVisible {
			b.WriteString(`<rect class="schaetzung ` + r.key + `" x=` + f(left+0.6) + ` y=` + f(y-h+0.6) + ` width=` + f(barW-1.2) + ` height=` + f(math.Max(0, h-1.2)) + ` rx=2></rect>`)
		}
		if m.hasVorjahr {
			vy := base - m.vorjahr*scale
			b.WriteString(`<line class=vorjahr x1=` + f(left-3) + ` x2=` + f(left+barW+3) + ` y1=` + f(vy) + ` y2=` + f(vy) + `></line>`)
		}
		b.WriteString(`<text class=achse x=` + f(cx) + ` y=` + strconv.Itoa(progH-6) + ` text-anchor=middle>` + string([]rune(months[i])[:1]) + `</text></g>`)
		if class == "leer" && !m.hasVorjahr {
			continue
		}
		cell := func(has bool, s string) string {
			if !has {
				return "<td>–</td>"
			}
			return "<td>" + s + "</td>"
		}
		rows.WriteString("<tr><th scope=row>" + name + "</th>" + cell(m.hasIst, energyWords(m.ist)) + cell(m.hasSchaetz, "etwa "+roughWords(m.schaetz)) +
			cell(m.hasVorjahr, energyWords(m.vorjahr)) + "</tr>")
	}
	b.WriteString(`<line class=nulllinie x1=` + strconv.Itoa(progL) + ` x2=` + strconv.Itoa(progW-progR) + ` y1=` + f(base) + ` y2=` + f(base) + `></line>`)
	b.WriteString("</svg><figcaption><ul class=legende><li><i class=\"ist " + r.key + "\"></i>gemessen</li><li><i class=\"schaetzung " + r.key + "\"></i>geschätzt</li>")
	if vorjahr {
		b.WriteString("<li><i class=vorjahr></i>" + strconv.Itoa(year-1) + "</li>")
	}
	b.WriteString("</ul></figcaption><details><summary>Als Tabelle</summary><table><thead><tr><th scope=col>Monat</th><th scope=col>Gemessen</th><th scope=col>Geschätzt</th><th scope=col>Vorjahr</th></tr></thead><tbody>")
	b.WriteString(rows.String() + "</tbody></table></details></figure>")
	return b.String()
}

// roughWords is an estimate: whole kWh from 10 on — an estimate has no
// tenths to claim.
func roughWords(kwh float64) string {
	if math.Abs(kwh) < 9.95 {
		return energyWords(kwh)
	}
	return energyWords(math.Round(kwh))
}
