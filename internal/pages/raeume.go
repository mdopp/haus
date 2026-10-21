package pages

import (
	"context"
	"log"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/mdopp/haus/internal/cards"
	"github.com/mdopp/haus/internal/ha"
)

// what a room card shows, in this order (laib DEKONSTRUKTION: Lichter,
// Heizung, Rollläden, Medien)
var roomKinds = map[string]int{"light": 0, "climate": 1, "cover": 2, "media_player": 3}

// maxRoomRows is the most one room card lists.
const maxRoomRows = 40

// noRoom is the card for what HA puts in no area.
const noRoom = "Ohne Raum"

// Raeume is one card per Home Assistant area (haus#12): its lights,
// heating, covers and media, the area the entity's own or else its
// device's; what has none in one "Ohne Raum" card at the end. A lamp is
// switched right on its row, and every thing is put on the person's
// Start with its ☆ (haus#12) — the forms of the thing's own page
// (haus#35), landing back on the thing's room. HA not answering, or its
// rooms not readable yet, leaves the cards out for one plain line
// (haus#33): a card that put every lamp in "Ohne Raum" because the
// registry failed would be wrong, not just short.
func Raeume(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !showOnly(w, r, "Räume") {
			return
		}
		if cfg.HA == nil {
			notSetHA.write(w, r, cfg, "Räume")
			return
		}
		var (
			states  []ha.State
			haErr   error
			reg     ha.Registry
			roomsOK bool
			favs    []string
			favErr  error
		)
		person := r.Header.Get("Remote-User")
		pins := cfg.Favorites != nil && person != ""
		reads := []func(context.Context){
			func(ctx context.Context) { states, haErr = cfg.HA.States(ctx) },
			func(ctx context.Context) { reg, roomsOK = cfg.HA.CachedRegistry(ctx) }}
		if pins {
			reads = append(reads, func(context.Context) { favs, favErr = cfg.Favorites.For(person) })
		}
		fetch(r, reads...)
		states = cfg.allAsSeen(states) // a cover wired the wrong way round, turned over (haus#64)
		logErrs("räume", map[string]error{"home assistant": haErr, "favorites": favErr})
		if haErr == nil && !roomsOK {
			log.Printf("haus: räume page: home assistant's rooms are not known yet; the cards are left out")
		}
		b := familyHead(r, cfg, "Räume", "Räume")
		b.WriteString("<h1>Räume</h1>")
		if haErr == nil { // the way on only while HA answers: a line about it never names it (laib ADR 0021)
			b.WriteString(cfg.homeAssistant().Weiter("/", saysInHA))
		}
		switch {
		case haErr != nil:
			b.WriteString("<p class=k>" + saysNoHouse + "</p>")
		case !roomsOK:
			b.WriteString("<p class=k>" + saysNoRooms + "</p>")
		default:
			var pinned map[string]bool
			switch {
			case pins && favErr != nil:
				b.WriteString("<p class=k>" + saysNoFavorites + "</p>")
			case pins:
				b.WriteString("<p class=k>" + saysPinWhat + "</p>")
				pinned = map[string]bool{}
				for _, id := range favs {
					pinned[id] = true
				}
			}
			for _, c := range roomCards(states, reg, pinned) {
				b.WriteString(c.HTML())
			}
		}
		writePage(w, http.StatusOK, b)
	}
}

// saysPinWhat is what Räume's ☆ does, once above the rooms.
const saysPinWhat = "Mit ☆ steht ein Gerät auf Start unter „Meine Geräte“."

// saysInHA is what Home Assistant has that Haus does not (haus#64).
const saysInHA = "Für alles, was Haus nicht kann: Automationen, Einstellungen, Verlauf."

// roomCards groups the states by room: rooms by name, "Ohne Raum" last;
// in a room lights, heating, covers, media, each by name, each leading to
// its own page; a lamp HA reaches with its switch. pinned is the person's
// Start (nil: no ☆). What HA hides (hidden, config, diagnostic) is left
// out.
func roomCards(states []ha.State, reg ha.Registry, pinned map[string]bool) []cards.Card {
	type thing struct {
		kind             int
		id, name, detail string
		state            ha.State
	}
	byRoom := map[string][]thing{}
	for _, s := range states {
		domain, _, _ := strings.Cut(s.EntityID, ".")
		kind, ok := roomKinds[domain]
		if !ok {
			continue
		}
		if _, group := s.Attributes["entity_id"].([]any); group {
			continue // a group is not a thing in a room; its members are
		}
		if reg.Hidden[s.EntityID] {
			continue
		}
		room := oneLine(reg.Rooms[s.EntityID])
		byRoom[room] = append(byRoom[room], thing{kind, s.EntityID, friendly(s), roomDetail(domain, s, reg.TempUnit), s})
	}
	names := make([]string, 0, len(byRoom))
	for n := range byRoom {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool {
		if (names[i] == "") != (names[j] == "") {
			return names[j] == ""
		}
		return strings.ToLower(names[i]) < strings.ToLower(names[j])
	})
	out := make([]cards.Card, 0, len(names))
	for _, n := range names {
		things := byRoom[n]
		sort.SliceStable(things, func(i, j int) bool {
			if things[i].kind != things[j].kind {
				return things[i].kind < things[j].kind
			}
			return strings.ToLower(things[i].name) < strings.ToLower(things[j].name)
		})
		title := trimRunes(n, 60)
		if n == "" {
			title = noRoom
		}
		c := cards.Card{Title: title}
		back := "/raeume#" + c.ID()
		for i, t := range things {
			if i == maxRoomRows {
				break
			}
			row := cards.Row{Text: t.name, Detail: t.detail, Href: cards.ThingPage(t.id)}
			switch domain, _, _ := strings.Cut(t.id, "."); domain {
			case "light": // a tap switches; a dimmer has its slider too (haus#64)
				row.Action = tile(domain, t.state, reg.TempUnit, back).Action
				row.Slider = dimmer(t.state, back)
			case "cover": // Hoch, Stopp, Runter and the position (haus#64)
				if bs := coverButtons(t.state, back); len(bs) > 0 {
					row.Also = bs
				}
				row.Slider = coverSlider(t.state, back)
			}
			if pinned != nil {
				row.Pin = &cards.Pin{Entity: t.id, Name: t.name, On: pinned[t.id], Back: back}
			}
			c.Rows = append(c.Rows, row)
		}
		more(&c, len(things)-maxRoomRows)
		out = append(out, c)
	}
	return out
}

// roomDetail says a thing's state in the family's words: the room kinds,
// and the other things a widget shows (a thing's own page, haus#28).
// houseUnit is HA's temperature unit; an entity's own temperature_unit
// wins, °C is the last resort.
func roomDetail(domain string, s ha.State, houseUnit string) string {
	switch s.State {
	case "unavailable":
		return "nicht erreichbar"
	case "unknown", "":
		return "unbekannt"
	}
	switch domain {
	case "light":
		switch s.State {
		case "on":
			if v, ok := number(s.Attributes["brightness"]); ok {
				return "an · " + strconv.Itoa(int(math.Round(v/255*100))) + " %"
			}
			return "an"
		case "off":
			return "aus"
		}
	case "climate":
		unit := tempUnit(s, houseUnit)
		var parts []string
		if s.State == "off" {
			parts = append(parts, "aus")
		}
		if v, ok := number(s.Attributes["current_temperature"]); ok {
			parts = append(parts, decimal(v)+" "+unit)
		}
		if v, ok := number(s.Attributes["temperature"]); ok && s.State != "off" {
			parts = append(parts, "Soll "+decimal(v)+" "+unit)
		}
		if len(parts) == 0 {
			return "an"
		}
		return strings.Join(parts, " · ")
	case "cover":
		word := map[string]string{"open": "offen", "closed": "geschlossen", "opening": "öffnet", "closing": "schließt", "stopped": "angehalten"}[s.State]
		if word == "" {
			break
		}
		if v, ok := number(s.Attributes["current_position"]); ok && s.State == "open" && v < 100 {
			word += " · " + strconv.Itoa(int(math.Round(v))) + " %"
		}
		return word
	case "media_player":
		word := map[string]string{"playing": "spielt", "paused": "pausiert", "idle": "bereit", "on": "an", "off": "aus", "standby": "Standby", "buffering": "lädt"}[s.State]
		if word == "" {
			break
		}
		if s.State == "playing" || s.State == "paused" {
			title, _ := s.Attributes["media_title"].(string)
			artist, _ := s.Attributes["media_artist"].(string)
			what := trimRunes(oneLine(title), 80)
			if a := trimRunes(oneLine(artist), 60); a != "" {
				if what != "" {
					what += " · "
				}
				what += a
			}
			if what != "" {
				word += ": " + what
			}
		}
		return word
	case "switch":
		if w := map[string]string{"on": "an", "off": "aus"}[s.State]; w != "" {
			return w
		}
	case "binary_sensor":
		dc, _ := s.Attributes["device_class"].(string)
		words := map[string]string{"on": "an", "off": "aus"}
		if openings[dc] {
			words = map[string]string{"on": "offen", "off": "geschlossen"}
		}
		if w := words[s.State]; w != "" {
			return w
		}
	case "lock":
		if w := map[string]string{"locked": "verriegelt", "unlocked": "offen", "locking": "verriegelt gerade", "unlocking": "öffnet",
			"jammed": "klemmt", "open": "offen", "opening": "öffnet"}[s.State]; w != "" {
			return w
		}
	case "sensor":
		if u, _ := s.Attributes["unit_of_measurement"].(string); trimRunes(oneLine(u), 8) != "" {
			return trimRunes(oneLine(s.State), 40) + " " + trimRunes(oneLine(u), 8)
		}
	}
	return trimRunes(oneLine(s.State), 40)
}

// tempUnit is a heating's temperature unit: its own temperature_unit,
// else the house's (HA's unit system), °C as the last resort.
func tempUnit(s ha.State, houseUnit string) string {
	if u, _ := s.Attributes["temperature_unit"].(string); trimRunes(oneLine(u), 8) != "" {
		return trimRunes(oneLine(u), 8)
	}
	if houseUnit != "" {
		return trimRunes(oneLine(houseUnit), 8)
	}
	return "°C"
}

// openings are the binary sensors that say open or closed, not on or off.
var openings = map[string]bool{"door": true, "garage_door": true, "opening": true, "window": true}

// number reads an attribute HA sends as a JSON number.
func number(v any) (float64, bool) {
	f, ok := v.(float64)
	if !ok || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	return f, true
}

// decimal is a number the German way, to one place: 21,5 · 22.
func decimal(v float64) string {
	return strings.Replace(strconv.FormatFloat(math.Round(v*10)/10, 'f', -1, 64), ".", ",", 1)
}
