package pages

import (
	"html"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/mdopp/haus/internal/cards"
)

// The forecast (haus#65) as the history's own chart shows it (the owner
// on #73: "warum nicht direkt ins erste Diagramm mit allen Werten?"): in
// Jahr the months still to come, in Monat the days, drawn hatched in the
// chart's stacked shape (schaetze); the year-end figures right under the
// Jahr chart (prognoseHTML). The estimate is prognose()'s of haus#65: the
// rest of the year as the same days came last year when HA has them, else
// at the daily mean so far.

// schaetzer estimates one series' days from today on.
type schaetzer struct {
	days         map[int64]float64
	since, first time.Time
	bisher, n    float64 // what came on the whole days so far, and how many
	wieVorjahr   bool
}

// schaetzerFor is a series' estimator; false when it has nothing this
// year yet. Only whole days count: today is not over.
func schaetzerFor(today, yearStart time.Time, loc *time.Location, days map[int64]float64) (schaetzer, bool) {
	first, ok := firstDay(days, loc)
	if !ok {
		return schaetzer{}, false
	}
	since := yearStart
	if first.After(since) {
		since = first
	}
	if !since.Before(today) {
		return schaetzer{}, false
	}
	e := schaetzer{days: days, since: since, first: first}
	e.bisher, _ = sumDays(days, since, today)
	e.n = math.Round(today.Sub(since).Hours() / 24)
	e.wieVorjahr = !back(today, 1, 0, false).Before(first)
	return e, true
}

// est is the estimate for the days [a, z), both from today on.
func (e schaetzer) est(a, z time.Time) float64 {
	if e.wieVorjahr {
		v, _ := sumDays(e.days, back(a, 1, 0, false), back(z, 1, 0, true))
		return v
	}
	return e.bisher / e.n * math.Round(z.Sub(a).Hours()/24)
}

// progReihe is one series' year-end forecast.
type progReihe struct {
	key, name    string
	bisher, rest float64
	since, first time.Time
	wieVorjahr   bool
	vorjahr      float64
	hasVorjahr   bool
}

// prognoseReihe is a series' year-end forecast; false when it has nothing
// this year yet.
func prognoseReihe(today, yearStart time.Time, loc *time.Location, key, name string, days map[int64]float64) (progReihe, bool) {
	e, ok := schaetzerFor(today, yearStart, loc, days)
	if !ok {
		return progReihe{}, false
	}
	r := progReihe{key: key, name: name, bisher: e.bisher, since: e.since, first: e.first, wieVorjahr: e.wieVorjahr,
		rest: e.est(today, yearStart.AddDate(1, 0, 0))}
	if ly := yearStart.AddDate(-1, 0, 0); !ly.Before(e.first) {
		r.vorjahr, _ = sumDays(days, ly, yearStart)
		r.hasVorjahr = true
	}
	return r, true
}

// schaetze puts the estimate into the Jahr and Monat chart's bars: each
// series' rest of its period from today on, today's part already measured
// taken off; and last year's same period — what the house used
// (Eigenverbrauch + Bezug) and what went into the grid — when HA has all
// of it. heute is where now falls, in bars from
// the left; −1 for a span without an estimate.
func schaetze(view string, now time.Time, loc *time.Location, daily amounts, bs []bucket) (heute float64) {
	if view != "jahr" && view != "monat" {
		return -1
	}
	today := midnight(now, loc)
	yearStart := time.Date(today.Year(), time.January, 1, 0, 0, 0, 0, loc)
	est := map[string]schaetzer{}
	for _, s := range energySeries {
		if e, ok := schaetzerFor(today, yearStart, loc, daily[s.key]); ok {
			est[s.key] = e
		}
	}
	if len(est) == 0 {
		return -1
	}
	for i := range bs {
		var from, to time.Time
		if view == "jahr" {
			from = time.Date(today.Year(), time.Month(i+1), 1, 0, 0, 0, 0, loc)
			to = from.AddDate(0, 1, 0)
		} else {
			from = time.Date(today.Year(), today.Month(), i+1, 0, 0, 0, 0, loc)
			to = from.AddDate(0, 0, 1)
		}
		if !today.Before(from) && today.Before(to) {
			heute = float64(i) + now.Sub(from).Hours()/to.Sub(from).Hours()
		}
		if a := maxTime(from, today); a.Before(to) {
			for k, e := range est {
				v := e.est(a, to)
				if !from.After(today) {
					v -= daily[k][today.Unix()] // today so far is in the bar already
				}
				if bs[i].est == nil {
					bs[i].est = map[string]float64{}
				}
				bs[i].est[k] = math.Max(0, v)
			}
		}
		// last year's same period, when HA has all of it
		ls, le := back(from, 1, 0, false), back(to, 1, 0, true)
		lastYear := func(keys ...string) (float64, bool) {
			all, any := true, false
			var sum float64
			for _, k := range keys {
				first, ok := firstDay(daily[k], loc)
				if !ok {
					continue
				}
				any, all = true, all && !ls.Before(first)
				v, _ := sumDays(daily[k], ls, le)
				sum += v
			}
			return sum, any && all
		}
		bs[i].vorjahr, bs[i].hasVorjahr = lastYear("eigen", "bezug")
		bs[i].vorjahrEinsp, bs[i].hasVorjahrEins = lastYear("einsp")
	}
	return heute
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
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

// prognoseHTML is the year-end figures: one line on how it is estimated,
// then each series' figure, its delta against last year when HA has all
// of last year.
func prognoseHTML(today, yearStart time.Time, loc *time.Location, daily amounts) string {
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
	sel := reihen[0]
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
	b.WriteString("<p class=hinweis>" + how + "</p><ul class=zahlen>")
	for _, r := range reihen {
		total := r.bisher + r.rest
		chip, detail := "", "bisher "+energyWords(r.bisher)
		if r.hasVorjahr {
			chip = deltaChip(r.key, total, r.vorjahr)
			detail += " · " + strconv.Itoa(year-1) + ": " + energyWords(r.vorjahr)
		}
		b.WriteString(zahlItem(r.name, "etwa "+roughWords(total), chip, detail))
	}
	b.WriteString("</ul></section>")
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
