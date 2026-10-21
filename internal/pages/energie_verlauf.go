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

// verlaufHTML is the history: the spans to choose, the span's sums, its
// chart, the chart as a table; then the comparisons.
func verlaufHTML(view string, now time.Time, loc *time.Location, daily, hourly amounts, err error) string {
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
	sums := cards.Card{Title: title, Look: cards.Tiles}
	for _, s := range energySeries {
		var sum float64
		has := false
		for _, x := range bs {
			if v, ok := x.v[s.key]; ok {
				sum, has = sum+v, true
			}
		}
		if has {
			sums.Rows = append(sums.Rows, cards.Row{Text: s.name, State: energyWords(sum)})
		}
	}
	if sums.Empty() {
		b.WriteString("<p class=k>" + saysNothingInSpan + "</p>")
	} else {
		b.WriteString(sums.HTML())
		b.WriteString(energyChart(title, bs))
	}
	for _, c := range vergleiche(now, loc, daily) {
		b.WriteString(c.HTML())
	}
	return b.String()
}

// vergleiche are the comparison cards (haus#65), from the days HA has:
// this month so far against the same days of the last, and of the same
// month last year; the year so far against last year's; and where the
// year will end, an estimate. Only complete days count (today is not
// over). A comparison whose earlier days HA does not have yet says from
// when it can be made, never a zero; one with part of them compares the
// days both have, and says from which day.
func vergleiche(now time.Time, loc *time.Location, daily amounts) []cards.Card {
	today := midnight(now, loc)
	monthStart := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, loc)
	yearStart := time.Date(today.Year(), time.January, 1, 0, 0, 0, 0, loc)
	upTo := "bis zum " + strconv.Itoa(today.AddDate(0, 0, -1).Day()) + "."
	type cmp struct {
		title         string
		from          time.Time
		years, months int
		than          func() string // the earlier span in words
		empty         string
	}
	cmps := []cmp{
		{"Dieser Monat und der letzte", monthStart, 0, 1, func() string {
			return months[back(today, 0, 1, false).Month()-1] + " " + upTo
		}, saysMonthJustBegun},
		{"Dieser Monat und der im Vorjahr", monthStart, 1, 0, func() string {
			return months[today.Month()-1] + " " + strconv.Itoa(today.Year()-1) + " " + upTo
		}, saysMonthJustBegun},
		{"Das Jahr bis heute und das Vorjahr", yearStart, 1, 0, func() string {
			return strconv.Itoa(today.Year()-1) + " bis zum " + strconv.Itoa(today.AddDate(0, 0, -1).Day()) + ". " + months[today.AddDate(0, 0, -1).Month()-1]
		}, saysYearJustBegun},
	}
	var out []cards.Card
	for _, c := range cmps {
		card := cards.Card{Title: c.title}
		if !c.from.Before(today) {
			card.Add(c.empty, "")
			out = append(out, card)
			continue
		}
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
				card.Add(s.name, "Vergleich ab "+months[at.Month()-1]+" "+strconv.Itoa(at.Year()))
				continue
			}
			cur, _ := sumDays(days, from, today)
			then, _ := sumDays(days, back(from, c.years, c.months, false), back(today, c.years, c.months, true))
			detail := c.than() + ": " + energyWords(then)
			if then > 0 {
				detail += " (jetzt " + changeWords(cur, then) + ")"
			}
			if from.After(c.from) {
				detail += " · verglichen ab dem " + strconv.Itoa(from.Day()) + ". " + months[from.Month()-1]
			}
			card.Rows = append(card.Rows, cards.Row{Text: s.name + ": " + energyWords(cur), Detail: detail})
		}
		if !card.Empty() {
			out = append(out, card)
		}
	}
	if f := prognose(today, yearStart, loc, daily); !f.Empty() {
		out = append(out, f)
	}
	return out
}

// prognose is where the year will end (haus#65), an estimate and said
// so: what came so far, and for the rest of the year what came on the
// same days last year when HA has them, else the mean of the days so far.
func prognose(today, yearStart time.Time, loc *time.Location, daily amounts) cards.Card {
	c := cards.Card{Title: "Prognose bis Jahresende " + strconv.Itoa(today.Year()) + " (geschätzt)"}
	if !yearStart.Before(today) {
		return c
	}
	yearEnd := yearStart.AddDate(1, 0, 0)
	for _, s := range energySeries {
		days := daily[s.key]
		first, ok := firstDay(days, loc)
		if !ok {
			continue
		}
		since := yearStart
		if first.After(since) {
			since = first
		}
		if !since.Before(today) {
			continue // nothing this year yet
		}
		sofar, _ := sumDays(days, since, today)
		var rest float64
		how := "der Rest wie im Vorjahr"
		if !back(today, 1, 0, false).Before(first) {
			rest, _ = sumDays(days, back(today, 1, 0, false), yearStart)
		} else {
			n := math.Round(today.Sub(since).Hours() / 24)
			left := math.Round(yearEnd.Sub(today).Hours() / 24)
			rest = sofar / n * left
			how = "der Rest nach dem Tagesmittel bisher"
		}
		detail := "bisher " + energyWords(sofar) + ", " + how
		if since.After(yearStart) {
			detail += " · gezählt ab dem " + strconv.Itoa(since.Day()) + ". " + months[since.Month()-1]
		}
		c.Rows = append(c.Rows, cards.Row{Text: s.name + ": etwa " + energyWords(sofar+rest), Detail: detail})
	}
	return c
}

// changeWords is now against then in words: "12 % mehr", "5 % weniger",
// "gleich viel".
func changeWords(now, then float64) string {
	p := math.Round((now - then) / then * 100)
	switch {
	case p > 0:
		return strconv.Itoa(int(p)) + " % mehr"
	case p < 0:
		return strconv.Itoa(int(-p)) + " % weniger"
	}
	return "gleich viel"
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

// verlaufStyle is the history's look, the house palette's tokens and
// three series colours of its own, checked for colour blindness and
// contrast on both grounds (dark first, light when the phone is).
const verlaufStyle = `<style>:root{--e-eigen:#c27c0e;--e-bezug:#4f8ff7;--e-einsp:#1fa592}
@media (prefers-color-scheme:light){:root{--e-eigen:#c27c0e;--e-bezug:#2f6fd6;--e-einsp:#0f8a78}}
.abschnitt{font-size:1.15rem;margin:34px 0 0}.zeitraum{display:flex;gap:6px;margin:12px 0 0;flex-wrap:wrap}
.zeitraum a{min-height:40px;display:inline-flex;align-items:center;padding:0 16px;border-radius:999px;background:var(--card);border:1px solid var(--edge);color:var(--ink);text-decoration:none;font-size:.88rem}
.zeitraum a[aria-current]{background:var(--accent-soft);border-color:transparent;color:var(--accent);font-weight:600}
.verlauf{margin:12px 0 0;padding:12px 10px 10px;background:var(--card);border:1px solid var(--edge);border-radius:14px}
.verlauf svg{display:block;width:100%;height:auto}.verlauf .eigen{fill:var(--e-eigen)}.verlauf .bezug{fill:var(--e-bezug)}.verlauf .einsp{fill:var(--e-einsp)}
.verlauf .treffer{fill:transparent}.verlauf g:hover .treffer{fill:var(--line)}.verlauf .gitter{stroke:var(--line)}.verlauf .nulllinie{stroke:var(--faint)}
.verlauf .achse{fill:var(--muted);font-size:12px;font-variant-numeric:tabular-nums}
.legende{list-style:none;margin:8px 4px 0;padding:0;display:flex;flex-wrap:wrap;gap:4px 14px;font-size:.8rem;color:var(--ink)}
.legende i{display:inline-block;width:10px;height:10px;border-radius:3px;margin-right:6px;vertical-align:-1px}.legende .eigen{background:var(--e-eigen)}.legende .bezug{background:var(--e-bezug)}.legende .einsp{background:var(--e-einsp)}
.verlauf details{margin:8px 4px 0;font-size:.82rem}.verlauf summary{cursor:pointer;color:var(--accent);min-height:32px;display:flex;align-items:center}
.verlauf table{width:100%;border-collapse:collapse;font-variant-numeric:tabular-nums}.verlauf th,.verlauf td{padding:4px 6px;border-top:1px solid var(--line);text-align:right;font-weight:400}
.verlauf th[scope=row],.verlauf thead th:first-child{text-align:left}.verlauf thead th{color:var(--muted)}
@media (min-width:900px){body>.verlauf,body>.zeitraum,body>.abschnitt{grid-column:1/-1}}</style>`

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
