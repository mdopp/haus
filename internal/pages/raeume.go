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
// device's; what has none in one "Ohne Raum" card at the end. Read-only in
// this cut: switching comes with the decision where actions live. HA not
// answering, or its rooms not readable yet, leaves the cards out: a card
// that put every lamp in "Ohne Raum" because the registry failed would be
// wrong, not just short.
func Raeume(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !showOnly(w, r, "Räume") {
			return
		}
		if cfg.HA == nil {
			toStart(w, r)
			return
		}
		var (
			states  []ha.State
			haErr   error
			reg     ha.Registry
			roomsOK bool
		)
		fetch(r,
			func(ctx context.Context) { states, haErr = cfg.HA.States(ctx) },
			func(ctx context.Context) { reg, roomsOK = cfg.HA.CachedRegistry(ctx) })
		logErrs("räume", map[string]error{"home assistant": haErr})
		if haErr == nil && !roomsOK {
			log.Printf("haus: räume page: home assistant's rooms are not known yet; the cards are left out")
		}
		b := familyHead(r, cfg, "Räume", "Räume")
		b.WriteString("<h1 class=sr>Räume</h1>")
		switch {
		case haErr != nil: // left out; the log has it
		case !roomsOK:
			b.WriteString("<p class=k>Die Räume werden gerade gelesen — gleich nochmal versuchen.</p>")
		default:
			for _, c := range roomCards(states, reg) {
				b.WriteString(c.HTML())
			}
		}
		writePage(w, http.StatusOK, b)
	}
}

// roomCards groups the states by room: rooms by name, "Ohne Raum" last;
// in a room lights, heating, covers, media, each by name. What HA hides
// (hidden, config, diagnostic) is left out.
func roomCards(states []ha.State, reg ha.Registry) []cards.Card {
	type thing struct {
		kind         int
		name, detail string
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
		byRoom[room] = append(byRoom[room], thing{kind, friendly(s), roomDetail(domain, s, reg.TempUnit)})
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
		for i, t := range things {
			if i == maxRoomRows {
				break
			}
			c.Add(t.name, t.detail)
		}
		more(&c, len(things)-maxRoomRows)
		out = append(out, c)
	}
	return out
}

// roomDetail says a thing's state in the family's words. houseUnit is HA's
// temperature unit; an entity's own temperature_unit wins, °C is the last
// resort.
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
		unit := "°C"
		if u, _ := s.Attributes["temperature_unit"].(string); trimRunes(oneLine(u), 8) != "" {
			unit = trimRunes(oneLine(u), 8)
		} else if houseUnit != "" {
			unit = trimRunes(oneLine(houseUnit), 8)
		}
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
	}
	return trimRunes(oneLine(s.State), 40)
}

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
