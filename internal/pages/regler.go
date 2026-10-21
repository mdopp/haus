package pages

import (
	"math"
	"strings"

	"github.com/mdopp/haus/internal/cards"
	"github.com/mdopp/haus/internal/ha"
	"github.com/mdopp/haus/internal/schalter"
)

// What a cover can do, by HA's supported_features (CoverEntityFeature).
const (
	coverOpen     = 1
	coverClose    = 2
	coverPosition = 4
	coverStop     = 8
)

// dimmable says HA can dim the lamp (haus#64): it reports a brightness,
// or one of its colour modes has one (anything but onoff).
func dimmable(s ha.State) bool {
	if _, ok := number(s.Attributes["brightness"]); ok {
		return true
	}
	modes, _ := s.Attributes["supported_color_modes"].([]any)
	for _, m := range modes {
		if v, _ := m.(string); v != "" && v != "onoff" && v != "unknown" {
			return true
		}
	}
	return false
}

// brightness is a lamp's brightness in percent: 0 when it is off.
func brightness(s ha.State) int {
	v, ok := number(s.Attributes["brightness"])
	if s.State != "on" || !ok {
		return 0
	}
	return max(0, min(100, int(math.Round(v/255*100))))
}

// dimmer is a lamp's slider (haus#64): light.turn_on with its brightness,
// 0 turns it off; nil for a lamp HA cannot dim or does not reach.
func dimmer(s ha.State, back string) *cards.Slider {
	if s.State != "on" && s.State != "off" || !dimmable(s) {
		return nil
	}
	return &cards.Slider{Label: "Helligkeit", Name: friendly(s), Entity: s.EntityID, Do: "light.turn_on", Back: back, Value: brightness(s)}
}

// coverFeatures is a cover's supported_features; all when HA does not say.
func coverFeatures(s ha.State) int {
	if v, ok := number(s.Attributes["supported_features"]); ok {
		return int(v)
	}
	return coverOpen | coverClose | coverPosition | coverStop
}

// coverButtons are a cover's Hoch, Stopp and Runter (haus#64), as the
// family sees it (asSeen), each only when HA says the cover can; none for
// a cover the rule does not let open freely (a garage, a door, a gate, one
// HA does not say what it is).
func coverButtons(s ha.State, back string) []*cards.Action {
	if !schalter.CoverFree(s) || s.State == "unavailable" {
		return nil
	}
	f := coverFeatures(s)
	var out []*cards.Action
	for _, b := range []struct {
		feature   int
		label, do string
	}{{coverOpen, "Hoch", "cover.open_cover"}, {coverStop, "Stopp", "cover.stop_cover"}, {coverClose, "Runter", "cover.close_cover"}} {
		if f&b.feature != 0 {
			out = append(out, &cards.Action{Label: b.label, Entity: s.EntityID, Do: b.do, Back: back})
		}
	}
	return out
}

// coverSlider is a cover's position, 0 down to 100 up (haus#64), for a
// cover that reports one and may be set freely.
func coverSlider(s ha.State, back string) *cards.Slider {
	v, ok := number(s.Attributes["current_position"])
	if !ok || !schalter.CoverFree(s) || coverFeatures(s)&coverPosition == 0 || s.State == "unavailable" {
		return nil
	}
	return &cards.Slider{Label: "Position", Name: friendly(s), Entity: s.EntityID, Do: "cover.set_cover_position", Back: back,
		Value: max(0, min(100, int(math.Round(v))))}
}

// saysCoverInHA is a cover's page when Haus does not open it.
const (
	saysGateInHA  = "Tore und Türen öffnet Haus nicht; das geht in Home Assistant."
	saysCoverWhat = "Home Assistant sagt nicht, ob das ein Rollladen oder ein Tor ist; darum öffnet Haus es nicht. In Home Assistant beim Gerät unter „Anzeigen als“ einstellen, dann geht es hier."
)

// asSeen is a state as the family sees it (haus#64): a cover wired the
// wrong way round (cfg.Inverted) turned over — its position 100 − HA's,
// open where HA says closed, opening where HA says closing. Anything else
// as HA says.
func (cfg StartConfig) asSeen(s ha.State) ha.State {
	if !cfg.Inverted[s.EntityID] || !strings.HasPrefix(s.EntityID, "cover.") {
		return s
	}
	attrs := make(map[string]any, len(s.Attributes))
	for k, v := range s.Attributes {
		attrs[k] = v
	}
	s.Attributes = attrs
	swap := map[string]string{"open": "closed", "closed": "open", "opening": "closing", "closing": "opening"}
	if v, ok := number(attrs["current_position"]); ok {
		seen := 100 - v
		attrs["current_position"] = seen
		if s.State == "open" || s.State == "closed" {
			s.State = map[bool]string{true: "closed", false: "open"}[seen <= 0]
			return s
		}
	}
	if w, ok := swap[s.State]; ok {
		s.State = w
	}
	return s
}

// allAsSeen is asSeen for each state.
func (cfg StartConfig) allAsSeen(states []ha.State) []ha.State {
	if len(cfg.Inverted) == 0 {
		return states
	}
	out := make([]ha.State, len(states))
	for i, s := range states {
		out[i] = cfg.asSeen(s)
	}
	return out
}

// asHA is a page's cover call as HA must hear it (haus#64): for a cover
// wired the wrong way round Hoch is close, Runter open, a position 100 −
// the slider's. Anything else as the page sent it.
func (cfg StartConfig) asHA(id, do string, wert int) (string, int) {
	if !cfg.Inverted[id] || !strings.HasPrefix(id, "cover.") {
		return do, wert
	}
	switch do {
	case "cover.open_cover":
		return "cover.close_cover", wert
	case "cover.close_cover":
		return "cover.open_cover", wert
	case "cover.set_cover_position":
		return do, 100 - wert
	}
	return do, wert
}

// InvertedFromList reads HAUS_COVER_INVERTED: cover entity ids,
// comma-separated; anything not a cover's id is left out.
func InvertedFromList(list string) map[string]bool {
	out := map[string]bool{}
	for _, id := range strings.Split(list, ",") {
		if id = strings.TrimSpace(id); ha.EntityRE.MatchString(id) && strings.HasPrefix(id, "cover.") {
			out[id] = true
		}
	}
	return out
}
