package pages

import (
	"errors"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/mdopp/haus/internal/cards"
	"github.com/mdopp/haus/internal/ha"
	"github.com/mdopp/haus/internal/schalter"
)

// pageDoes is what a family page may ask Home Assistant to do (haus#35):
// a lamp or a switch on and off, a player's pause, and on Medien
// (haus#12) its going on and its volume a step down or up — narrower than
// package schalter's rule, which every call still passes, never wider.
var pageDoes = map[string]bool{"light.turn_on": true, "light.turn_off": true, "switch.turn_on": true, "switch.turn_off": true,
	"media_player.media_pause": true, "media_player.media_play": true, "media_player.volume_down": true, "media_player.volume_up": true,
	// Geräte's covers (haus#64): Hoch, Stopp, Runter and the position
	"cover.open_cover": true, "cover.stop_cover": true, "cover.close_cover": true, "cover.set_cover_position": true}

// takesWert is what a slider sets (haus#64), wert 0–100: a lamp's
// brightness (light.turn_on's brightness_pct; 0 is off), a cover's
// position. set_cover_position wants one; turn_on goes without.
var takesWert = map[string]string{"light.turn_on": "brightness_pct", "cover.set_cover_position": "position"}

// allLightsOff is Im Haus's "Alle aus": every lamp Start counted as on.
const allLightsOff = "alle-lichter-aus"

// What a switch that did not happen says.
const (
	saysSwitchFailed = "Das hat nicht geklappt: Licht und Geräte antworten gerade nicht."
	saysNotSwitched  = "Das lässt sich hier nicht schalten."
)

// Schalten is where a family page's switch posts (cards.SwitchPath): one
// thing, one service of pageDoes, through package schalter's rule — the
// widgets' own — and back to the page it came from (303, so a reload
// sends nothing again). Only a person, only from Haus's own page; the
// form's other fields go nowhere. It decides nothing about the box
// (haus#1): it switches the house.
func Schalten(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "Schalten geht nur mit dem Knopf auf der Seite.", http.StatusMethodNotAllowed)
			return
		}
		if cfg.HA == nil {
			http.Redirect(w, r, "/start", http.StatusSeeOther)
			return
		}
		if !fromHaus(r) {
			http.Error(w, "Schalten geht nur von Haus' eigener Seite.", http.StatusForbidden)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Das Formular ist nicht lesbar.", http.StatusBadRequest)
			return
		}
		id, do := r.PostForm.Get("geraet"), r.PostForm.Get("tun")
		if pageDoes[do] || do == allLightsOff && id == "" {
			event := "schalten." + do // what was pressed, never on what or by whom (haus#50)
			if do == "light.turn_on" && r.PostForm.Get("wert") != "" {
				event = "schalten.light.dimmen" // a dimmer's slider (haus#64)
			}
			cfg.Usage.Count(event)
		}
		back := backTo(r.PostForm.Get("zurueck"))
		var err error
		switch {
		case do == allLightsOff && id == "":
			var states []ha.State
			if states, err = cfg.HA.States(r.Context()); err == nil {
				reg, _ := cfg.HA.CachedRegistry(r.Context())
				for _, l := range litLamps(states, reg.Hidden) {
					if err = schalter.Do(r.Context(), cfg.HA, l.EntityID, "light.turn_off", nil, false); err != nil {
						break
					}
				}
			}
		case pageDoes[do]:
			var data map[string]any
			wert, given, ok := wertOf(r.PostForm.Get("wert"))
			switch {
			case !ok || given && takesWert[do] == "":
				err = &schalter.Refusal{Code: http.StatusBadRequest, Reason: "wert is 0–100, for a slider only"}
			case do == "cover.set_cover_position" && !given:
				err = &schalter.Refusal{Code: http.StatusBadRequest, Reason: "set_cover_position wants wert"}
			}
			if err != nil {
				break
			}
			call, wert := cfg.asHA(id, do, wert) // a cover wired the wrong way round (haus#64)
			switch {
			case call == "light.turn_on" && given && wert == 0:
				call = "light.turn_off"
			case given:
				data = map[string]any{takesWert[call]: wert}
			}
			err = schalter.Do(r.Context(), cfg.HA, id, call, data, false)
		default:
			err = &schalter.Refusal{Code: http.StatusBadRequest, Reason: "not a page's"}
		}
		if err == nil {
			http.Redirect(w, r, back, http.StatusSeeOther)
			return
		}
		log.Printf("haus: schalten %q on %q: %v", do, id, err)
		m := missing{http.StatusOK, saysSwitchFailed, "Zuhause", back, "Zurück"}
		switch {
		case back == medienPath:
			m.page = "Medien"
		case back != "/start":
			m.page = "Räume"
		}
		var no *schalter.Refusal
		if errors.As(err, &no) {
			m.code, m.say = no.Code, saysNotSwitched
		}
		m.write(w, r, cfg)
	}
}

// wertOf reads a slider's wert: 0–100 (ok), or none given (ok, !given).
func wertOf(v string) (wert int, given, ok bool) {
	if v == "" {
		return 0, false, true
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 || n > 100 {
		return 0, true, false
	}
	return n, true, true
}

// backTo is where a switch lands: Start, Räume (one room on it), Medien,
// or the page of a thing that has one; anything else is Start.
func backTo(path string) string {
	if path == medienPath || inRaeume.MatchString(path) {
		return path
	}
	if id, ok := strings.CutPrefix(path, cards.ThingPages); ok && ha.EntityRE.MatchString(id) {
		if domain, _, _ := strings.Cut(id, "."); cards.Domains[domain] {
			return cards.ThingPage(id)
		}
	}
	return "/start"
}

// inRaeume is Räume, or one room's card on it (cards.Card.ID).
var inRaeume = regexp.MustCompile(`^/raeume(#karte(-[a-z0-9]+)*)?$`)
