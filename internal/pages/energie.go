package pages

import (
	"context"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mdopp/haus/internal/cards"
	"github.com/mdopp/haus/internal/energie"
	"github.com/mdopp/haus/internal/ha"
)

// What Energie says when it cannot show the house's power.
const (
	saysNoEnergy      = "Die Stromzähler antworten gerade nicht."
	saysNoMeters      = "Für dieses Haus ist noch kein Stromzähler eingerichtet."
	saysNoEnergyToday = "Was heute zusammenkam, lässt sich gerade nicht lesen."
)

// counterWords are the fallback's counters in the family's words, by the
// friendly name they were found by; the template's counters keep the
// template's labels.
var counterWords = map[string]string{"PV Erzeugung": "Erzeugt", "Netzbezug": "Aus dem Netz", "Netzeinspeisung": "Ins Netz",
	"Batterie laden": "In den Akku", "Batterie entladen": "Aus dem Akku"}

// counterOrder is Heute's order for the fallback's counters: made, then
// drawn and fed in, then into and out of the battery.
var counterOrder = map[string]int{"PV Erzeugung": 1, "Netzbezug": 2, "Netzeinspeisung": 3, "Batterie laden": 4, "Batterie entladen": 5}

// Energie is the house's power (haus#12): Jetzt, what the house uses and
// what the sun, the grid and the battery do right now, as the flow
// picture (energie_fluss.go); Heute, what each counter added since
// midnight, and what the house used from them, while the history does
// not show today itself. The sensors are package energie's choice, the widgets'
// energy picture's too. None found is one plain line; HA not answering is
// one plain line; its history alone not answering keeps Jetzt. A path
// under /energie/ is a 404 with the way back.
func Energie(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !showOnly(w, r, "Energie") {
			return
		}
		if cfg.HA == nil {
			notSetHA.write(w, r, cfg, "Energie")
			return
		}
		if r.URL.Path != "/energie" {
			missing{http.StatusNotFound, "Diese Seite gibt es nicht.", "Energie", "/energie", "Energie"}.write(w, r, cfg)
			return
		}
		var (
			states []ha.State
			haErr  error
		)
		fetch(r, func(ctx context.Context) { states, haErr = cfg.HA.States(ctx) })
		logErrs("energie", map[string]error{"home assistant": haErr})
		b := familyHead(r, cfg, "Energie", "Energie")
		b.WriteString(refreshEvery(60)) // Jetzt follows the sun; the history is HA's statistics, not every second
		b.WriteString("<h1>Energie</h1>")
		if haErr != nil {
			b.WriteString("<p class=k>" + saysNoEnergy + "</p>")
			writePage(w, http.StatusOK, b)
			return
		}
		legs, counters, _ := cfg.Energy.Pick(states)
		stats := cfg.Energy.History(counters)
		if len(legs) == 0 && len(counters) == 0 && !stats.Any() {
			b.WriteString("<p class=k>" + saysNoMeters + "</p>")
			writePage(w, http.StatusOK, b)
			return
		}
		now, loc := cfg.clock()
		today := midnight(now, loc)
		view := zeitraumOf(r)
		var (
			todayAdded      map[string]float64
			histErr         error
			daily, hourly   amounts
			dailyE, hourlyE error
		)
		var reads []func(context.Context)
		if len(counters) > 0 {
			reads = append(reads, func(context.Context) {
				todayAdded, histErr = addedSince(r, cfg.HA, counters, today, now)
			})
		}
		if stats.Any() { // the history from HA's long-term statistics (haus#65)
			reads = append(reads, func(ctx context.Context) {
				// last year's January on: every comparison and the forecast
				daily, dailyE = statsFor(ctx, cfg.HA, stats, time.Date(today.Year()-1, time.January, 1, 0, 0, 0, 0, loc), today.AddDate(0, 0, 1), "day", loc)
			})
			if view == "tag" {
				reads = append(reads, func(ctx context.Context) {
					hourly, hourlyE = statsFor(ctx, cfg.HA, stats, today, today.AddDate(0, 0, 1), "hour", loc)
				})
			}
		}
		fetch(r, reads...)
		logErrs("energie", map[string]error{"history": histErr, "statistics": dailyE, "hourly statistics": hourlyE})
		b.WriteString(verlaufStyle)
		if len(legs) > 0 {
			now := flussJetzt(legs)
			if now.da["akku"] {
				now.fuell, _, now.hasFuell = cfg.Energy.Charge(states)
			}
			b.WriteString(flussHTML(now))
		}
		err := dailyE
		if err == nil {
			err = hourlyE
		}
		// the history's Tag says what Heute's tiles say, from the same
		// counters: once is enough
		shown := stats.Any() && err == nil && (len(daily) > 0 || len(hourly) > 0)
		if len(counters) > 0 && !shown {
			b.WriteString(heuteCard(counters, todayAdded, histErr).HTML())
		}
		if stats.Any() {
			b.WriteString("<div id=verlauf></div>")
			b.WriteString(verlaufHTML(view, now, loc, daily, hourly, err))
		}
		writePage(w, http.StatusOK, b)
	}
}

// heuteCard is what each counter added today, and first what the house
// used, when the fallback's counters say it: made + drawn − fed in +
// from the battery − into it. A counter without today's history is left
// out, and so is Verbraucht: a sum missing one of the house's counters
// would be wrong, not just short.
func heuteCard(counters []energie.Counter, today map[string]float64, err error) cards.Card {
	c := cards.Card{Title: "Heute", Look: cards.Tiles}
	if err != nil {
		c.Add(saysNoEnergyToday, "")
		return c
	}
	byName := map[string]float64{}
	every := true // each of the house's counters has today's value
	for _, k := range counters {
		v, ok := today[k.State.EntityID]
		every = every && ok
		if ok && k.Fallback != "" {
			byName[k.Fallback] = v
		}
	}
	pv, ok1 := byName["PV Erzeugung"]
	drawn, ok2 := byName["Netzbezug"]
	fed, ok3 := byName["Netzeinspeisung"]
	in, okIn := byName["Batterie laden"]
	out, okOut := byName["Batterie entladen"]
	if every && ok1 && ok2 && ok3 && okIn == okOut {
		if used := pv + drawn - fed + out - in; used >= 0 {
			c.Rows = append(c.Rows, cards.Row{Text: "Verbraucht", State: energyWords(used)})
		}
	}
	// the fallback's by counterOrder; the template's in its order
	sort.SliceStable(counters, func(i, j int) bool { return counterOrder[counters[i].Fallback] < counterOrder[counters[j].Fallback] })
	for _, k := range counters {
		v, ok := today[k.State.EntityID]
		if !ok {
			continue
		}
		name := k.Label
		if w := counterWords[k.Fallback]; w != "" {
			name = w
		}
		c.Rows = append(c.Rows, cards.Row{Text: trimRunes(oneLine(name), 40), State: energyWords(v)})
	}
	return c
}

// addedSince is what each counter added since start, in kWh, from HA's
// history and its state now: every rise counts; a fall below newMeter of
// the count is the counter starting over and counts from zero, a smaller
// dip is noise (HA's own rule for a total_increasing sensor). A counter
// without a reading in its history since start is left out; any history
// read failing is the error.
func addedSince(r *http.Request, c *ha.Client, counters []energie.Counter, start, now time.Time) (map[string]float64, error) {
	var (
		mu   sync.Mutex
		out  = map[string]float64{}
		errs error
	)
	reads := make([]func(context.Context), 0, len(counters))
	for _, k := range counters {
		reads = append(reads, func(ctx context.Context) {
			points, err := c.History(ctx, k.State.EntityID, start, now.Add(time.Minute))
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = err
				return
			}
			scale := kwhScale(k.State)
			var sum float64
			prev, seen, read := 0.0, false, false
			for i, p := range append(points, k.State) {
				v, err := strconv.ParseFloat(strings.TrimSpace(p.State), 64)
				if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
					continue
				}
				switch {
				case !seen:
					prev = v
				case v >= prev:
					sum += v - prev
					prev = v
				case v < newMeter*prev: // started over: counts from zero
					sum += v
					prev = v
				} // a smaller dip is noise: the count stands where it stood
				seen, read = true, read || i < len(points)
			}
			if read {
				out[k.State.EntityID] = sum * scale
			}
		})
	}
	fetch(r, reads...)
	return out, errs
}

// newMeter: a counter falling below this share of its count started over
// (a new meter, a reset); HA's recorder draws the line at 10 %.
const newMeter = 0.9

// watts is a power sensor's reading in W, from W, kW or MW.
func watts(s ha.State) (float64, bool) {
	v, err := strconv.ParseFloat(strings.TrimSpace(s.State), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	u, _ := s.Attributes["unit_of_measurement"].(string)
	switch strings.TrimSpace(u) {
	case "kW":
		v *= 1e3
	case "MW":
		v *= 1e6
	}
	return v, true
}

// kwhScale turns a counter's unit into kWh.
func kwhScale(s ha.State) float64 {
	u, _ := s.Attributes["unit_of_measurement"].(string)
	switch strings.TrimSpace(u) {
	case "Wh":
		return 1e-3
	case "MWh":
		return 1e3
	}
	return 1
}

// sign is -1, 0 or 1; below 1 W is 0.
func sign(w float64) int {
	switch {
	case w >= 1:
		return 1
	case w <= -1:
		return -1
	}
	return 0
}

// powerWords is a power the German way: "850 W" below a kilowatt, else
// "1,2 kW".
func powerWords(w float64) string {
	if math.Round(w) < 1000 {
		return strconv.Itoa(int(math.Round(w))) + " W"
	}
	return decimal(w/1000) + " kW"
}

// energyWords is an amount of energy: "12,4 kWh"; from 100 on whole,
// with the thousands marked, as a report prints it: "2.130 kWh".
func energyWords(kwh float64) string {
	if math.Abs(kwh) < 99.95 {
		return decimal(kwh) + " kWh"
	}
	n := int(math.Round(math.Abs(kwh)))
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "." + s[i:]
	}
	if kwh < 0 {
		s = "-" + s
	}
	return s + " kWh"
}
