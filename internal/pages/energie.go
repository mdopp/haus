package pages

import (
	"context"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mdopp/homes/internal/cards"
	"github.com/mdopp/homes/internal/ha"
)

// What Energie says when it cannot show the house's power.
const (
	saysNoEnergy = "Die Stromzähler antworten gerade nicht."
	saysNoMeters = "Für dieses Haus ist noch kein Stromzähler eingerichtet."
)

// Energie is the house's power (haus#12), in the Android widget's look
// (owner, 2026-11-03): Jetzt, what the house uses and what the sun, the
// grid and the battery do right now, as the flow picture
// (energie_fluss.go); then the history, whose Tag is today so far (the
// owner, 2026-11-03: no separate Heute block, the Tag picture says it). The sensors are package energie's choice, the widgets'
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
		if len(legs) == 0 && !stats.Any() {
			b.WriteString("<p class=k>" + saysNoMeters + "</p>")
			writePage(w, http.StatusOK, b)
			return
		}
		now, loc := cfg.clock()
		today := midnight(now, loc)
		view := zeitraumOf(r)
		var (
			daily, hourly   amounts
			dailyE, hourlyE error
		)
		var reads []func(context.Context)
		if stats.Any() { // the history from HA's long-term statistics (haus#65)
			reads = append(reads, func(ctx context.Context) {
				// last year's January on: the comparisons and the forecast
				daily, dailyE = statsFor(ctx, cfg.HA, stats, time.Date(today.Year()-1, time.January, 1, 0, 0, 0, 0, loc), today.AddDate(0, 0, 1), "day", loc)
			})
			if view == "tag" {
				reads = append(reads, func(ctx context.Context) {
					hourly, hourlyE = statsFor(ctx, cfg.HA, stats, today, today.AddDate(0, 0, 1), "hour", loc)
				})
			}
		}
		fetch(r, reads...)
		logErrs("energie", map[string]error{"statistics": dailyE, "hourly statistics": hourlyE})
		b.WriteString(verlaufStyle)
		if len(legs) > 0 {
			now := flussJetzt(legs)
			if now.da["akku"] {
				now.fuell, _, now.hasFuell = cfg.Energy.Charge(states)
			}
			b.WriteString(flussHTML(now))
		}
		if stats.Any() {
			err := dailyE
			if err == nil {
				err = hourlyE
			}
			b.WriteString("<div id=verlauf></div>")
			b.WriteString(verlaufHTML(view, now, loc, daily, hourly, err))
		}
		writePage(w, http.StatusOK, b)
	}
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

// energieZeile is Energie on Start in one line (owner 2026-10-24): what
// the roof makes and the house uses now, the battery's charge and the
// grid, leading to Energie. Nothing without flow legs.
func energieZeile(cfg StartConfig, states []ha.State) string {
	legs, _, _ := cfg.Energy.Pick(states)
	if len(legs) == 0 {
		return ""
	}
	f := flussJetzt(legs)
	var top, more []string
	if f.known["solar"] {
		top = append(top, "Dach "+powerWords(f.solar))
	}
	if f.known["haus"] {
		top = append(top, "Haus "+powerWords(f.haus))
	}
	if f.da["akku"] {
		if pct, _, ok := cfg.Energy.Charge(states); ok {
			a := "Akku " + percent0(pct)
			switch {
			case f.known["akku"] && f.laden > 0:
				a += " lädt"
			case f.known["akku"] && f.entladen > 0:
				a += " entlädt"
			}
			more = append(more, a)
		}
	}
	if f.known["netz"] {
		switch {
		case f.einsp > 0:
			more = append(more, powerWords(f.einsp)+" ins Netz")
		case f.bezug > 0:
			more = append(more, powerWords(f.bezug)+" aus dem Netz")
		}
	}
	if len(top) == 0 {
		return ""
	}
	c := cards.Card{Title: "Energie"}
	c.AddLink(strings.Join(top, " · "), strings.Join(more, " · "), "/energie")
	return c.HTML()
}
