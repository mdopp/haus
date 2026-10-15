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

// legWords are the flow legs in the family's words, in the order Jetzt
// shows them: what the house uses first.
var legWords = []struct{ key, name string }{{"house", "Haus"}, {"pv", "Solaranlage"}, {"grid", "Stromnetz"}, {"battery", "Akku"}}

// counterWords are the fallback's counters in the family's words, by the
// friendly name they were found by; the template's counters keep the
// template's labels.
var counterWords = map[string]string{"PV Erzeugung": "Erzeugt", "Netzbezug": "Aus dem Netz", "Netzeinspeisung": "Ins Netz",
	"Batterie laden": "In den Akku", "Batterie entladen": "Aus dem Akku"}

// counterOrder is Heute's order for the fallback's counters: made, then
// drawn and fed in, then into and out of the battery.
var counterOrder = map[string]int{"PV Erzeugung": 1, "Netzbezug": 2, "Netzeinspeisung": 3, "Batterie laden": 4, "Batterie entladen": 5}

// Energie is the house's power (haus#12): Jetzt, what the house uses and
// what the sun, the grid and the battery do right now, as tiles in words;
// Heute, what each counter added since midnight, and what the house used
// from them. The sensors are package energie's choice, the widgets'
// energy picture's too. None found is one plain line; HA not answering is
// one plain line; its history alone not answering keeps Jetzt.
func Energie(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !showOnly(w, r, "Energie") {
			return
		}
		if cfg.HA == nil {
			toStart(w, r)
			return
		}
		var (
			states []ha.State
			haErr  error
		)
		fetch(r, func(ctx context.Context) { states, haErr = cfg.HA.States(ctx) })
		logErrs("energie", map[string]error{"home assistant": haErr})
		b := familyHead(r, cfg, "Energie", "Energie")
		b.WriteString("<h1>Energie</h1>")
		if haErr != nil {
			b.WriteString("<p class=k>" + saysNoEnergy + "</p>")
			writePage(w, http.StatusOK, b)
			return
		}
		legs, counters, _ := cfg.Energy.Pick(states)
		if len(legs) == 0 && len(counters) == 0 {
			b.WriteString("<p class=k>" + saysNoMeters + "</p>")
			writePage(w, http.StatusOK, b)
			return
		}
		b.WriteString(jetztCard(legs).HTML())
		if len(counters) > 0 {
			now, loc := cfg.clock()
			y, m, d := now.Date()
			today, err := addedSince(r, cfg.HA, counters, time.Date(y, m, d, 0, 0, 0, 0, loc), now)
			logErrs("energie", map[string]error{"history": err})
			b.WriteString(heuteCard(counters, today, err).HTML())
		}
		writePage(w, http.StatusOK, b)
	}
}

// jetztCard is each flow leg as a tile: its size, and what it does in a
// word, never a sign to read.
func jetztCard(legs []energie.Leg) cards.Card {
	c := cards.Card{Title: "Jetzt", Look: cards.Tiles}
	byKey := map[string]ha.State{}
	for _, l := range legs {
		byKey[l.Key] = l.State
	}
	for _, lw := range legWords {
		s, ok := byKey[lw.key]
		if !ok {
			continue
		}
		row := cards.Row{Text: lw.name}
		w, known := watts(s)
		switch {
		case s.State == "unavailable":
			row.State, row.Off = "Nicht erreichbar", true
		case !known:
			row.State, row.Off = "Unbekannt", true
		default:
			row.State = powerWords(math.Abs(w))
			switch lw.key {
			case "house":
				row.Detail = "Verbrauch"
			case "pv":
				row.Detail = "erzeugt"
			case "grid":
				row.Detail = map[int]string{1: "bezogen", -1: "eingespeist", 0: "ruht"}[sign(w)]
			case "battery":
				row.Detail = map[int]string{1: "lädt", -1: "entlädt", 0: "ruht"}[sign(w)]
			}
		}
		c.Rows = append(c.Rows, row)
	}
	return c
}

// heuteCard is what each counter added today, and first what the house
// used, when the fallback's counters say it: made + drawn − fed in +
// from the battery − into it. A counter without today's history is left
// out, and so is what needs it.
func heuteCard(counters []energie.Counter, today map[string]float64, err error) cards.Card {
	c := cards.Card{Title: "Heute", Look: cards.Tiles}
	if err != nil {
		c.Add(saysNoEnergyToday, "")
		return c
	}
	byName := map[string]float64{}
	for _, k := range counters {
		if v, ok := today[k.State.EntityID]; ok && k.Fallback != "" {
			byName[k.Fallback] = v
		}
	}
	pv, ok1 := byName["PV Erzeugung"]
	drawn, ok2 := byName["Netzbezug"]
	fed, ok3 := byName["Netzeinspeisung"]
	in, okIn := byName["Batterie laden"]
	out, okOut := byName["Batterie entladen"]
	if ok1 && ok2 && ok3 && okIn == okOut {
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
// history and its state now: every rise counts, a fall is the counter
// starting over and counts from zero. A counter without history since
// start is left out; any history read failing is the error.
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
			prev, seen := 0.0, false
			for _, p := range append(points, k.State) {
				v, err := strconv.ParseFloat(strings.TrimSpace(p.State), 64)
				if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
					continue
				}
				switch {
				case !seen:
				case v >= prev:
					sum += v - prev
				default:
					sum += v
				}
				prev, seen = v, true
			}
			if len(points) > 0 && seen {
				out[k.State.EntityID] = sum * scale
			}
		})
	}
	fetch(r, reads...)
	return out, errs
}

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

// energyWords is an amount of energy: "12,4 kWh".
func energyWords(kwh float64) string { return decimal(kwh) + " kWh" }
