package pages

import (
	"context"
	"html"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mdopp/haus/internal/cards"
	"github.com/mdopp/haus/internal/energie"
	"github.com/mdopp/haus/internal/ha"
)

// The history (haus#65): what came from the grid, went into it, what the
// sun made and what the house used of that itself, by hour, day or month,
// from HA's long-term statistics; and the comparisons a family asks
// about — this month against the last and against last year's, the year
// so far against last year's, and where the year will end.

// What the history says when it cannot show something.
const (
	saysNoEnergyHistory = "Der Verlauf lässt sich gerade nicht lesen."
	saysNoStatsYet      = "Für diese Zähler gibt es noch keinen Verlauf. Er beginnt, sobald sie Werte aufzeichnen."
	saysNothingInSpan   = "Für diesen Zeitraum gibt es noch keine Werte."
	saysMonthJustBegun  = "Der Monat hat gerade erst angefangen; der Vergleich kommt morgen."
	saysYearJustBegun   = "Das Jahr hat gerade erst angefangen; die Zahlen kommen morgen."
)

// energySeries are the history's four amounts, in the order the page
// names them: what the sun made, what the house used of it, what came
// from the grid, what went into it. Eigenverbrauch is Solar − Einspeisung.
var energySeries = []struct{ key, name string }{
	{"solar", "Solar"}, {"eigen", "Eigenverbrauch"}, {"bezug", "Bezug"}, {"einsp", "Einspeisung"},
}

// views are the history's spans: today by the hour, the last seven days,
// this month and this year, by day, by month.
var views = []struct{ key, name string }{{"tag", "Tag"}, {"woche", "Woche"}, {"monat", "Monat"}, {"jahr", "Jahr"}}

// amounts are what each series added, by the start of its period (a local
// hour or day, as Unix seconds), in kWh.
type amounts map[string]map[int64]float64

// statsFor reads what HA's long-term statistics say for the history's
// ids between from and to, by period, each period by its local start;
// Eigenverbrauch for each period that has both Solar and Einspeisung.
func statsFor(ctx context.Context, c *ha.Client, st energie.Stats, from, to time.Time, period string, loc *time.Location) (amounts, error) {
	slots := map[string]string{"solar": st.Solar, "bezug": st.Bezug, "einsp": st.Einspeisung}
	var ids []string
	seen := map[string]bool{}
	for _, id := range slots {
		if id != "" && !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}
	sort.Strings(ids)
	got, err := c.Statistics(ctx, ids, from, to, period)
	if err != nil {
		return nil, err
	}
	out := amounts{}
	for key, id := range slots {
		rows, ok := got[id]
		if id == "" || !ok {
			continue
		}
		m := map[int64]float64{}
		for _, s := range rows {
			at := s.Start.In(loc)
			if period == "day" {
				y, mo, d := at.Date()
				at = time.Date(y, mo, d, 0, 0, 0, 0, loc)
			}
			m[at.Unix()] += s.Change
		}
		out[key] = m
	}
	if solar, einsp := out["solar"], out["einsp"]; solar != nil && einsp != nil {
		eigen := map[int64]float64{}
		for at, v := range solar {
			if e, ok := einsp[at]; ok {
				eigen[at] = math.Max(0, v-e)
			}
		}
		out["eigen"] = eigen
	}
	return out, nil
}

// midnight is the start of t's day in loc.
func midnight(t time.Time, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

// sumDays is what a series added on the days in [from, to).
func sumDays(days map[int64]float64, from, to time.Time) (float64, bool) {
	var sum float64
	any := false
	for at, v := range days {
		if at >= from.Unix() && at < to.Unix() {
			sum += v
			any = true
		}
	}
	return sum, any
}

// firstDay is the first day a series has a value for.
func firstDay(days map[int64]float64, loc *time.Location) (time.Time, bool) {
	first, ok := int64(0), false
	for at := range days {
		if !ok || at < first {
			first, ok = at, true
		}
	}
	return time.Unix(first, 0).In(loc), ok
}

// back is t moved back by years and months, on the same day when that
// month has it, else on its last (31 March → 28 February). end says t
// ends a span: a day the month does not have moves to the next month's
// first, so the span keeps every day the month has.
func back(t time.Time, years, months int, end bool) time.Time {
	y, m, d := t.Date()
	first := time.Date(y-years, m-time.Month(months), 1, 0, 0, 0, 0, t.Location())
	last := first.AddDate(0, 1, -1).Day()
	if d > last {
		if end {
			return first.AddDate(0, 1, 0)
		}
		d = last
	}
	return time.Date(first.Year(), first.Month(), d, t.Hour(), t.Minute(), 0, 0, t.Location())
}

// bucket is one bar of the chart: when it starts, its label on the axis
// (may be ""), its name in words, and each series' amount.
type bucket struct {
	label, long string
	v           map[string]float64
}

// buckets are the chosen span's bars: today's 24 hours, the last seven
// days, this month's days, this year's months.
func buckets(view string, now time.Time, loc *time.Location, daily, hourly amounts) (title string, out []bucket) {
	today := midnight(now, loc)
	take := func(a amounts, at time.Time, b *bucket) {
		for _, s := range energySeries {
			if v, ok := a[s.key][at.Unix()]; ok {
				b.v[s.key] += v
			}
		}
	}
	switch view {
	case "tag":
		title = "Heute, Stunde für Stunde"
		for h := today; h.Before(today.AddDate(0, 0, 1)); h = h.Add(time.Hour) {
			b := bucket{long: h.Format("15") + " Uhr", v: map[string]float64{}}
			if h.Hour()%6 == 0 {
				b.label = strconv.Itoa(h.Hour())
			}
			take(hourly, h, &b)
			out = append(out, b)
		}
	case "woche":
		title = "Die letzten sieben Tage"
		for d := today.AddDate(0, 0, -6); !d.After(today); d = d.AddDate(0, 0, 1) {
			b := bucket{label: weekdays[d.Weekday()][:2], long: weekdays[d.Weekday()] + ", " + strconv.Itoa(d.Day()) + ". " + months[d.Month()-1], v: map[string]float64{}}
			take(daily, d, &b)
			out = append(out, b)
		}
	case "monat":
		first := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, loc)
		title = months[today.Month()-1] + " " + strconv.Itoa(today.Year())
		for d := first; d.Month() == first.Month(); d = d.AddDate(0, 0, 1) {
			b := bucket{long: strconv.Itoa(d.Day()) + ". " + months[d.Month()-1], v: map[string]float64{}}
			if d.Day() == 1 || d.Day()%7 == 1 {
				b.label = strconv.Itoa(d.Day())
			}
			take(daily, d, &b)
			out = append(out, b)
		}
	default: // jahr
		title = "Das Jahr " + strconv.Itoa(today.Year())
		for m := time.January; m <= time.December; m++ {
			b := bucket{label: string([]rune(months[m-1])[:1]), long: months[m-1] + " " + strconv.Itoa(today.Year()), v: map[string]float64{}}
			from := time.Date(today.Year(), m, 1, 0, 0, 0, 0, loc)
			for _, s := range energySeries {
				if v, ok := sumDays(daily[s.key], from, from.AddDate(0, 1, 0)); ok {
					b.v[s.key] = v
				}
			}
			out = append(out, b)
		}
	}
	return title, out
}

// verlaufHTML is the history: the spans to choose, the span's totals as
// the flow picture, its chart, the chart as a table; then the
// comparisons and the forecast, which draws the series which names.
func verlaufHTML(view, which string, now time.Time, loc *time.Location, daily, hourly amounts, err error) string {
	var b strings.Builder
	b.WriteString("<h2 class=abschnitt>Verlauf</h2><nav class=zeitraum aria-label=Zeitraum>")
	for _, v := range views {
		b.WriteString("<a href=\"/energie?zeitraum=" + v.key + "#verlauf\"")
		if v.key == view {
			b.WriteString(" aria-current=page")
		}
		b.WriteString(">" + v.name + "</a>")
	}
	b.WriteString("</nav>")
	if err != nil {
		b.WriteString("<p class=k>" + saysNoEnergyHistory + "</p>")
		return b.String()
	}
	if len(daily) == 0 && len(hourly) == 0 {
		b.WriteString("<p class=k>" + saysNoStatsYet + "</p>")
		return b.String()
	}
	title, bs := buckets(view, now, loc, daily, hourly)
	any := false
	for _, x := range bs {
		any = any || len(x.v) > 0
	}
	if !any {
		b.WriteString("<p class=k>" + saysNothingInSpan + "</p>")
	} else {
		flowTitle := title
		if view == "tag" {
			flowTitle = "Heute bis jetzt" // the hours HA has summed so far
		}
		b.WriteString(flussHTML(flussSpanne(flowTitle, bs, daily, hourly)))
		b.WriteString(energyChart(title, bs))
	}
	today := midnight(now, loc)
	b.WriteString(vergleiche(now, loc, daily))
	b.WriteString(prognoseHTML(view, which, today, time.Date(today.Year(), time.January, 1, 0, 0, 0, 0, loc), loc, daily))
	return b.String()
}

// vergleiche are the comparison cards (haus#65), from the days HA has:
// this month so far against the same days of the last, and of the same
// month last year; the year so far against last year's. Only complete
// days count (today is not over). Each series is a headline number and,
// when the earlier days are there, its delta (deltaChip). A comparison
// whose earlier days HA does not have yet says from when it can be made,
// never a zero; one with part of them compares the days both have, and
// says from which day.
func vergleiche(now time.Time, loc *time.Location, daily amounts) string {
	today := midnight(now, loc)
	monthStart := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, loc)
	yearStart := time.Date(today.Year(), time.January, 1, 0, 0, 0, 0, loc)
	upTo := "bis zum " + strconv.Itoa(today.AddDate(0, 0, -1).Day()) + "."
	type cmp struct {
		title         string
		from          time.Time
		years, months int
		than          func() string // the earlier span in words
		then          string        // the earlier span in a word
		empty         string
	}
	cmps := []cmp{
		{"Dieser Monat und der letzte", monthStart, 0, 1, func() string {
			return months[back(today, 0, 1, false).Month()-1] + " " + upTo
		}, "Vormonat", saysMonthJustBegun},
		{"Dieser Monat und der im Vorjahr", monthStart, 1, 0, func() string {
			return months[today.Month()-1] + " " + strconv.Itoa(today.Year()-1) + " " + upTo
		}, "Vorjahr", saysMonthJustBegun},
		{"Das Jahr bis heute und das Vorjahr", yearStart, 1, 0, func() string {
			return strconv.Itoa(today.Year()-1) + " bis zum " + strconv.Itoa(today.AddDate(0, 0, -1).Day()) + ". " + months[today.AddDate(0, 0, -1).Month()-1]
		}, "Vorjahr", saysYearJustBegun},
	}
	var b strings.Builder
	for _, c := range cmps {
		if !c.from.Before(today) {
			card := cards.Card{Title: c.title}
			card.Add(c.empty, "")
			b.WriteString(card.HTML())
			continue
		}
		var items, open []string
		dates := map[string]bool{}
		var firstOf time.Time
		for _, s := range energySeries {
			days := daily[s.key]
			first, ok := firstDay(days, loc)
			if !ok {
				continue
			}
			from := c.from
			for from.Before(today) && back(from, c.years, c.months, false).Before(first) {
				from = from.AddDate(0, 0, 1)
			}
			if !from.Before(today) { // HA does not have the earlier days yet
				at := first.AddDate(c.years, c.months, 0)
				if at.Before(c.from) {
					at = c.from
				}
				cur, _ := sumDays(days, c.from, today)
				ab := "Vergleich ab " + months[at.Month()-1] + " " + strconv.Itoa(at.Year())
				items = append(items, zahlItem(s.name, energyWords(cur), "", ab))
				open, dates[ab], firstOf = append(open, ab), true, first
				continue
			}
			cur, _ := sumDays(days, from, today)
			then, _ := sumDays(days, back(from, c.years, c.months, false), back(today, c.years, c.months, true))
			detail := c.then + " " + energyWords(then)
			if from.After(c.from) {
				detail += " · ab dem " + strconv.Itoa(from.Day()) + ". " + months[from.Month()-1]
			}
			items = append(items, zahlItem(s.name, energyWords(cur), deltaChip(s.key, cur, then), detail))
		}
		if len(items) == 0 {
			continue
		}
		if len(open) == len(items) && len(dates) == 1 { // nothing to compare yet: one line, not a tile each
			card := cards.Card{Title: c.title}
			card.Add(open[0], "HA hat die Werte ab dem "+strconv.Itoa(firstOf.Day())+". "+months[firstOf.Month()-1]+" "+strconv.Itoa(firstOf.Year())+".")
			b.WriteString(card.HTML())
			continue
		}
		id := cards.Card{Title: c.title}.ID()
		b.WriteString(`<section class="karte vergleich" aria-labelledby=` + id + `><h2 id=` + id + `>` + html.EscapeString(c.title) + `</h2><p class=hinweis>Verglichen mit ` +
			html.EscapeString(c.than()) + `</p><ul class=zahlen>` + strings.Join(items, "") + `</ul></section>`)
	}
	return b.String()
}

// zahlItem is one key figure: its name, the number, the delta, one quiet
// line.
func zahlItem(name, zahl, chip, detail string) string {
	s := "<li><span class=name>" + html.EscapeString(name) + "</span> <b class=zahl>" + html.EscapeString(zahl) + "</b>"
	if chip != "" {
		s += " " + chip
	}
	if detail != "" {
		s += " <span class=k>" + html.EscapeString(detail) + "</span>"
	}
	return s + "</li>"
}

// goodWhenMore is which way is good for the household, by series: more
// sun and more of it kept is good, more from the grid bad; more fed in is
// neither — more paid for it, but less of the sun kept.
var goodWhenMore = map[string]int{"solar": 1, "eigen": 1, "bezug": -1, "einsp": 0}

// deltaChip is now against then the way a report says it: an arrow, the
// signed change in whole percent and, when the change is good or bad for
// the household, the word — the colour (gut, schlecht) is never alone.
// Nothing to compare with (then 0) is no chip.
func deltaChip(key string, now, then float64) string {
	if then <= 0 {
		return ""
	}
	p := math.Round((now - then) / then * 100)
	arrow, signed := "=", "±0"
	switch {
	case p > 0:
		arrow, signed = "▲", "+"+strconv.Itoa(int(p))
	case p < 0:
		arrow, signed = "▼", "−"+strconv.Itoa(int(-p))
	}
	class, word := "neutral", ""
	switch d := goodWhenMore[key] * int(math.Copysign(1, p)); {
	case p == 0:
		class, word = "gleich", "gleich"
	case goodWhenMore[key] == 0:
	case d > 0:
		class, word = "gut", "besser"
	default:
		class, word = "schlecht", "schlechter"
	}
	s := `<span class="delta ` + class + `"><span aria-hidden=true>` + arrow + `</span> ` + signed + " %"
	if word != "" {
		s += " · " + word
	}
	return s + "</span>"
}

// The chart's geometry, in the SVG's own units.
const (
	chartW, chartH = 400, 190
	chartL, chartR = 48, 4
	chartT, chartB = 8, 22
	chartGap       = 1.5 // between stacked segments
	// the units are a phone's: 400 wide is about its width, so 12 reads as
	// 10–15 px from a phone to a wide window
	chartBarShare   = 0.68
	chartMinVisible = 0.5 // a segment under half a unit is drawn as nothing
)

// energyChart draws the span (haus#65): one bar a period, what the house
// used above the line — what it took from the sun (Eigenverbrauch) and
// from the grid (Bezug) — and what went into the grid below it. Each bar
// names its numbers on hover (a title); the legend names the colours; the
// table under it says the same in numbers. No script, no library.
func energyChart(title string, bs []bucket) string {
	var up, down float64
	for _, b := range bs {
		up = math.Max(up, b.v["eigen"]+b.v["bezug"])
		down = math.Max(down, b.v["einsp"])
	}
	if up+down == 0 {
		up = 1
	}
	plotH := float64(chartH - chartT - chartB)
	scale := plotH / (up + down)
	base := float64(chartT) + up*scale
	plotW := float64(chartW - chartL - chartR)
	step := plotW / float64(len(bs))
	barW := math.Max(2, step*chartBarShare)
	f := func(v float64) string { return strconv.FormatFloat(math.Round(v*10)/10, 'f', -1, 64) }
	var b strings.Builder
	b.WriteString(`<figure class=verlauf><svg viewBox="0 0 ` + strconv.Itoa(chartW) + " " + strconv.Itoa(chartH) + `" role=img aria-label="` +
		html.EscapeString(title+" als Balken; die Zahlen stehen in der Tabelle darunter") + `">`)
	// the scale: the top, the line, the bottom when something went out
	grid := func(y float64, label string) {
		b.WriteString(`<line class=gitter x1=` + strconv.Itoa(chartL) + ` x2=` + strconv.Itoa(chartW-chartR) + ` y1=` + f(y) + ` y2=` + f(y) + `></line>`)
		b.WriteString(`<text class=achse x=` + strconv.Itoa(chartL-6) + ` y=` + f(y+4) + ` text-anchor=end>` + html.EscapeString(label) + `</text>`)
	}
	grid(float64(chartT), shortKWh(up))
	if down > 0 {
		grid(float64(chartH-chartB), shortKWh(down))
	}
	for i, x := range bs {
		cx := float64(chartL) + step*float64(i) + step/2
		left := cx - barW/2
		var tip []string
		for _, s := range energySeries {
			if v, ok := x.v[s.key]; ok {
				tip = append(tip, s.name+" "+energyWords(v))
			}
		}
		b.WriteString("<g><title>" + html.EscapeString(x.long))
		if len(tip) > 0 {
			b.WriteString(": " + html.EscapeString(strings.Join(tip, " · ")))
		}
		b.WriteString("</title>")
		// the whole column answers the finger, not only the bar
		b.WriteString(`<rect class=treffer x=` + f(cx-step/2) + ` y=` + strconv.Itoa(chartT) + ` width=` + f(step) + ` height=` + f(plotH) + `></rect>`)
		y := base
		for _, seg := range []string{"eigen", "bezug"} {
			h := x.v[seg] * scale
			if h < chartMinVisible {
				continue
			}
			if y < base {
				y -= chartGap
				h = math.Max(0, h-chartGap)
			}
			b.WriteString(`<rect class=` + seg + ` x=` + f(left) + ` y=` + f(y-h) + ` width=` + f(barW) + ` height=` + f(h) + ` rx=2></rect>`)
			y -= h
		}
		if h := x.v["einsp"] * scale; h >= chartMinVisible {
			b.WriteString(`<rect class=einsp x=` + f(left) + ` y=` + f(base+chartGap/2) + ` width=` + f(barW) + ` height=` + f(math.Max(0, h-chartGap/2)) + ` rx=2></rect>`)
		}
		if x.label != "" {
			b.WriteString(`<text class=achse x=` + f(cx) + ` y=` + strconv.Itoa(chartH-6) + ` text-anchor=middle>` + html.EscapeString(x.label) + `</text>`)
		}
		b.WriteString("</g>")
	}
	b.WriteString(`<line class=nulllinie x1=` + strconv.Itoa(chartL) + ` x2=` + strconv.Itoa(chartW-chartR) + ` y1=` + f(base) + ` y2=` + f(base) + `></line>`)
	// the legend and the table: only the series this span has
	has := map[string]bool{}
	for _, x := range bs {
		for k := range x.v {
			has[k] = true
		}
	}
	b.WriteString("</svg><figcaption><ul class=legende>")
	for _, l := range []struct{ key, says string }{{"eigen", "Eigenverbrauch (von der Sonne)"}, {"bezug", "Bezug (aus dem Netz)"}, {"einsp", "Einspeisung (ins Netz)"}} {
		if has[l.key] {
			b.WriteString("<li><i class=" + l.key + "></i>" + l.says + "</li>")
		}
	}
	b.WriteString("</ul></figcaption>")
	// the same in numbers
	b.WriteString("<details><summary>Als Tabelle</summary><table><thead><tr><th scope=col>Zeit</th>")
	for _, s := range energySeries {
		if has[s.key] {
			b.WriteString("<th scope=col>" + s.name + "</th>")
		}
	}
	b.WriteString("</tr></thead><tbody>")
	for _, x := range bs {
		if len(x.v) == 0 {
			continue
		}
		b.WriteString("<tr><th scope=row>" + html.EscapeString(x.long) + "</th>")
		for _, s := range energySeries {
			switch v, ok := x.v[s.key]; {
			case ok:
				b.WriteString("<td>" + energyWords(v) + "</td>")
			case has[s.key]:
				b.WriteString("<td>–</td>")
			}
		}
		b.WriteString("</tr>")
	}
	b.WriteString("</tbody></table></details></figure>")
	return b.String()
}

// shortKWh is an axis label: "12 kWh", "0,8 kWh".
func shortKWh(v float64) string {
	if v >= 10 {
		return strconv.Itoa(int(math.Round(v))) + " kWh"
	}
	return decimal(v) + " kWh"
}

// verlaufStyle is the Energie page's look, the house palette's tokens
// and its own: the series colours (checked for colour blindness and
// contrast on both grounds, dark first, light when the phone is), and the
// report's two (haus#65, the owner: "als ob ich einen Geschäftsbericht
// lese"): --e-gut a blue-ish green, --e-schlecht an orange-red, AA as
// text on the card and on their own chip (dark 8,9:1 and 7,2:1, light
// 5,2:1 and 5,4:1), apart for red-green colour blindness too — and never
// alone: the arrow, the sign and the word say it as well.
const verlaufStyle = `<style>:root{--e-eigen:#c27c0e;--e-bezug:#4f8ff7;--e-einsp:#1fa592;--e-solar:#e3b341;--e-akku:#a78bfa;
--e-gut:#40c8c0;--e-schlecht:#ff7a5c;--e-gut-soft:rgba(64,200,192,.10);--e-schlecht-soft:rgba(255,122,92,.10)}
@media (prefers-color-scheme:light){:root{--e-eigen:#c27c0e;--e-bezug:#2f6fd6;--e-einsp:#0f8a78;--e-solar:#b07d00;--e-akku:#7c4ddb;
--e-gut:#00788a;--e-schlecht:#c4352b;--e-gut-soft:rgba(0,120,138,.08);--e-schlecht-soft:rgba(196,53,43,.08)}}
.abschnitt{font-size:1.15rem;margin:34px 0 0}.zeitraum{display:flex;gap:6px;margin:12px 0 0;flex-wrap:wrap}
.zeitraum a{min-height:40px;display:inline-flex;align-items:center;padding:0 16px;border-radius:999px;background:var(--card);border:1px solid var(--edge);color:var(--ink);text-decoration:none;font-size:.88rem}
.zeitraum a[aria-current]{background:var(--accent-soft);border-color:transparent;color:var(--accent);font-weight:600}
.verlauf,.prognosebild{margin:12px 0 0;padding:12px 10px 10px;background:var(--card);border:1px solid var(--edge);border-radius:14px}
.verlauf svg,.prognosebild svg{display:block;width:100%;height:auto}.verlauf .eigen{fill:var(--e-eigen)}.verlauf .bezug{fill:var(--e-bezug)}.verlauf .einsp{fill:var(--e-einsp)}
.verlauf .treffer,.prognosebild .treffer{fill:transparent}.verlauf g:hover .treffer,.prognosebild g:hover .treffer{fill:var(--line)}
.verlauf .gitter,.prognosebild .gitter{stroke:var(--line)}.verlauf .nulllinie,.prognosebild .nulllinie{stroke:var(--faint)}
.verlauf .achse,.prognosebild .achse{fill:var(--muted);font-size:12px;font-variant-numeric:tabular-nums}
.legende{list-style:none;margin:8px 4px 0;padding:0;display:flex;flex-wrap:wrap;gap:4px 14px;font-size:.8rem;color:var(--ink)}
.legende i{display:inline-block;width:10px;height:10px;border-radius:3px;margin-right:6px;vertical-align:-1px}.legende .eigen{background:var(--e-eigen)}.legende .bezug{background:var(--e-bezug)}.legende .einsp{background:var(--e-einsp)}
.verlauf details,.fluss details,.prognosebild details{margin:8px 4px 0;font-size:.82rem}.verlauf summary,.fluss summary,.prognosebild summary{cursor:pointer;color:var(--accent);min-height:32px;display:flex;align-items:center}
.verlauf table,.fluss table,.prognosebild table{width:100%;border-collapse:collapse;font-variant-numeric:tabular-nums}
.verlauf th,.verlauf td,.fluss th,.fluss td,.prognosebild th,.prognosebild td{padding:4px 6px;border-top:1px solid var(--line);text-align:right;font-weight:400}
.verlauf th[scope=row],.verlauf thead th:first-child,.fluss th,.prognosebild th[scope=row],.prognosebild thead th:first-child{text-align:left}.verlauf thead th,.prognosebild thead th{color:var(--muted)}
.fluss-bild{background:var(--card);border:1px solid var(--edge);border-radius:14px;padding:6px 8px 12px}
.fluss svg{display:block;width:100%;max-width:380px;height:auto;margin:0 auto}
.fluss .spur{fill:none;stroke:var(--track);stroke-width:1.5;stroke-dasharray:2 5}
.fluss .eigen{--c:var(--e-eigen)}.fluss .einsp{--c:var(--e-einsp)}.fluss .bezug{--c:var(--e-bezug)}.fluss .akku{--c:var(--e-akku)}
.fluss .weg{fill:none;stroke:var(--c);stroke-linecap:round;opacity:.38}.fluss .pfeil{fill:var(--c)}
.fluss .lauf{fill:none;stroke:var(--c);stroke-linecap:round;stroke-dasharray:1 11;animation:fliesst 1.6s linear infinite}
@keyframes fliesst{to{stroke-dashoffset:-12}}@media (prefers-reduced-motion:reduce){.fluss .lauf{animation:none}}
.knoten.solar{--c:var(--e-solar)}.knoten.netz{--c:var(--e-bezug)}.knoten.akku{--c:var(--e-akku)}.knoten.haus{--c:var(--accent)}
.knoten circle{fill:var(--card);stroke:var(--c);stroke-width:3}.knoten.aus circle{stroke:var(--faint);stroke-dasharray:4 4;stroke-width:2}
.knoten .name{fill:var(--muted);font-size:11px;font-weight:600;letter-spacing:.1em;text-transform:uppercase}
.knoten .zahl{fill:var(--ink);font-size:17px;font-weight:700;font-variant-numeric:tabular-nums}.knoten .zahl.leer{fill:var(--faint)}.knoten .zahl.lang{font-size:13.5px}
.knoten .wort{fill:var(--muted);font-size:11.5px}.knoten .zweit{fill:var(--muted);font-size:12px;font-variant-numeric:tabular-nums}
.kennzahlen{display:grid;grid-template-columns:1fr 1fr;gap:0 12px;margin:6px 6px 0;padding:10px 0 0;border-top:1px solid var(--line)}
.kennzahlen div{min-width:0}.kennzahlen dt{font-size:.82rem;font-weight:600;color:var(--muted);overflow-wrap:anywhere}
.kennzahlen dd{margin:0}.kennzahlen .zahl{font-size:2rem;font-weight:700;line-height:1.15;font-variant-numeric:tabular-nums;color:var(--ink)}.kennzahlen .k{font-size:.8rem}
.karte ul.zahlen{background:none;border:0;border-radius:0;display:grid;grid-template-columns:repeat(auto-fill,minmax(148px,1fr));gap:8px}
.karte .zahlen li,.karte .zahlen li:first-child{background:var(--card);border:1px solid var(--edge);border-radius:14px;padding:11px 13px 12px;display:flex;flex-direction:column;align-items:flex-start;gap:3px}
.karte .zahlen a{display:flex;flex-direction:column;align-items:flex-start;gap:3px}.karte .wahl .zahl{font-size:1.15rem}.karte ul.legende{background:none;border:0;border-radius:0}.karte .legende li{padding:0;border:0;min-height:0}.zahlen .name{font-size:.72rem;font-weight:600;letter-spacing:.08em;text-transform:uppercase;color:var(--muted)}
.zahlen .zahl{font-size:1.45rem;font-weight:700;line-height:1.2;font-variant-numeric:tabular-nums;color:var(--ink)}.karte .zahlen .k{font-size:.78rem;margin:0}
.wahl li:has(a[aria-current]){border-color:var(--accent);box-shadow:inset 0 0 0 1px var(--accent)}.wahl a[aria-current] .name{color:var(--accent)}
.delta{display:inline-flex;align-items:center;gap:4px;padding:1px 8px;border-radius:999px;font-size:.78rem;font-weight:600;font-variant-numeric:tabular-nums;white-space:nowrap}
.delta.gut{color:var(--e-gut);background:var(--e-gut-soft)}.delta.schlecht{color:var(--e-schlecht);background:var(--e-schlecht-soft)}.delta.neutral,.delta.gleich{color:var(--muted);background:var(--line)}
.prognose .prognosebild{margin-top:8px}.prognosebild.solar{--c:var(--e-solar)}.prognosebild.eigen{--c:var(--e-eigen)}.prognosebild.bezug{--c:var(--e-bezug)}.prognosebild.einsp{--c:var(--e-einsp)}
.prognosebild .ist{fill:var(--c)}.prognosebild .strich{fill:var(--c)}.prognosebild .schaetzung{fill:url(#schraffur);stroke:var(--c);stroke-width:1.2}
.prognosebild .vorjahr{stroke:var(--ink);stroke-width:2.5;stroke-linecap:round}.prognosebild .heute{stroke:var(--accent);stroke-width:1.2;stroke-dasharray:3 3}
.prognosebild .heute-text{fill:var(--accent);font-size:11px;font-weight:600}
.legende .ist{background:var(--c)}.legende .schaetzung{background:repeating-linear-gradient(45deg,var(--c) 0 2px,transparent 2px 5px);box-shadow:inset 0 0 0 1px var(--c)}
.legende .vorjahr{height:3px;vertical-align:3px;background:var(--ink);border-radius:2px}
@media (min-width:900px){body>.zeitraum,body>.abschnitt,body>.prognose,body>.vergleich{grid-column:1/-1}body>.verlauf{grid-column:auto;margin-top:46px;align-self:start}
.vergleich ul.zahlen{grid-template-columns:repeat(4,minmax(0,1fr))}
.prognose-inhalt{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1.4fr);gap:16px;align-items:start}.prognose-inhalt .zahlen{grid-template-columns:repeat(2,minmax(0,1fr))}.prognose .prognosebild{margin-top:0}}</style>`

// zeitraumOf is the span the page asks for (?zeitraum=…); the day when
// it names none it knows.
func zeitraumOf(r *http.Request) string {
	v := r.URL.Query().Get("zeitraum")
	for _, z := range views {
		if z.key == v {
			return v
		}
	}
	return "tag"
}
