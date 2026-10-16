// Package energie names the house's energy sensors in Home Assistant
// (haus#10, haus#12): the "now" flow from current-power sensors (W) and
// the lifetime counters (kWh). The widgets' /napi/portal/energy and the
// family's Energie page read the same choice from here.
//
// Which sensors those are is the house's, so the template names them by
// entity id (FromEnv). Without that, the flow and counters are found by
// the friendly names one house used — a fallback, not a convention.
package energie

import (
	"fmt"
	"os"
	"strings"

	"github.com/mdopp/haus/internal/ha"
)

// Legs are the four flow legs, in the order the app draws them; Sense
// fixes the sign: supply ≥0 produced, draw ≥0 used, grid +W drawn from
// the grid −W fed in, battery +W charging −W discharging.
var Legs = []struct{ Key, Label, Name, Sense string }{
	{"pv", "PV", "Aktuell erzeugter PV-Strom", "supply"},
	{"house", "Haus", "Aktueller Hausverbrauch", "draw"},
	{"grid", "Netz", "Aktuelle Netz Leistung", "grid"},
	{"battery", "Akku", "Aktuelle Akku Leistung", "battery"},
}

// Fallback counters: the friendly name one house used → its label.
var fallbackCounters = []struct{ Name, Label string }{
	{"PV Erzeugung", "PV-Erzeugung"},
	{"Netzeinspeisung", "Einspeisung"},
	{"Netzbezug", "Netzbezug"},
	{"Batterie laden", "Batterie geladen"},
	{"Batterie entladen", "Batterie entladen"},
}

// Map names the energy entities: Flow by leg key, Totals as labelled
// counters in the order given. Empty: the fallback.
type Map struct {
	Flow   map[string]string
	Totals []Total
}

// Total is one labelled lifetime counter.
type Total struct{ Label, Entity string }

// Set says the template named the entities.
func (m Map) Set() bool { return len(m.Flow) > 0 || len(m.Totals) > 0 }

// FromEnv reads HAUS_ENERGY_FLOW (pv=sensor.x,house=…,grid=…,battery=…)
// and HAUS_ENERGY_TOTALS (Label=sensor.y,…). Both unset: the fallback.
func FromEnv() (Map, error) {
	m := Map{Flow: map[string]string{}}
	legs := map[string]bool{}
	for _, f := range Legs {
		legs[f.Key] = true
	}
	for _, item := range strings.Split(os.Getenv("HAUS_ENERGY_FLOW"), ",") {
		if item = strings.TrimSpace(item); item == "" {
			continue
		}
		k, e, ok := strings.Cut(item, "=")
		k, e = strings.TrimSpace(k), strings.TrimSpace(e)
		if !ok || !legs[k] || !ha.EntityRE.MatchString(e) {
			return Map{}, fmt.Errorf("HAUS_ENERGY_FLOW %q: want pv|house|grid|battery=<entity_id>", item)
		}
		m.Flow[k] = e
	}
	for _, item := range strings.Split(os.Getenv("HAUS_ENERGY_TOTALS"), ",") {
		if item = strings.TrimSpace(item); item == "" {
			continue
		}
		i := strings.LastIndex(item, "=")
		if i <= 0 {
			return Map{}, fmt.Errorf("HAUS_ENERGY_TOTALS %q: want <label>=<entity_id>", item)
		}
		l, e := strings.TrimSpace(item[:i]), strings.TrimSpace(item[i+1:])
		if l == "" || !ha.EntityRE.MatchString(e) {
			return Map{}, fmt.Errorf("HAUS_ENERGY_TOTALS %q: want <label>=<entity_id>", item)
		}
		m.Totals = append(m.Totals, Total{Label: l, Entity: e})
	}
	return m, nil
}

// Leg is one flow leg found in the states.
type Leg struct {
	Key, Label, Sense string
	State             ha.State
}

// Counter is one lifetime counter found in the states. Fallback is the
// friendly name it was found by ("" when the template named it).
type Counter struct {
	Label, Fallback string
	State           ha.State
}

// Pick finds the flow legs and the counters in states: the template's
// entities when it named any, else the fallback's friendly names (a power
// sensor for a leg, an energy sensor for a counter). Used holds the flow
// legs' entities.
func (m Map) Pick(states []ha.State) (flow []Leg, totals []Counter, used map[string]bool) {
	used = map[string]bool{}
	flow, totals = []Leg{}, []Counter{}
	if m.Set() {
		byID := map[string]ha.State{}
		for _, s := range states {
			byID[s.EntityID] = s
		}
		for _, f := range Legs {
			if s, ok := byID[m.Flow[f.Key]]; ok {
				flow = append(flow, Leg{f.Key, f.Label, f.Sense, s})
				used[s.EntityID] = true
			}
		}
		for _, t := range m.Totals {
			if s, ok := byID[t.Entity]; ok {
				totals = append(totals, Counter{Label: t.Label, State: s})
				used[s.EntityID] = true
			}
		}
		return flow, totals, used
	}
	legs := map[string]ha.State{}
	counters := map[string]ha.State{}
	for _, s := range states {
		if !strings.HasPrefix(s.EntityID, "sensor.") {
			continue
		}
		name, _ := s.Attributes["friendly_name"].(string)
		if name == "" {
			name = s.EntityID
		}
		switch DeviceClass(s) {
		case "power":
			for _, f := range Legs {
				if _, seen := legs[f.Key]; f.Name == name && !seen {
					legs[f.Key] = s
					used[s.EntityID] = true
				}
			}
		case "energy":
			for _, t := range fallbackCounters {
				if _, seen := counters[name]; t.Name == name && !seen {
					counters[name] = s
				}
			}
		}
	}
	for _, f := range Legs {
		if s, ok := legs[f.Key]; ok {
			flow = append(flow, Leg{f.Key, f.Label, f.Sense, s})
		}
	}
	for _, t := range fallbackCounters {
		if s, ok := counters[t.Name]; ok {
			totals = append(totals, Counter{Label: t.Label, Fallback: t.Name, State: s})
		}
	}
	return flow, totals, used
}

// DeviceClass is a sensor's device_class, lower case.
func DeviceClass(s ha.State) string {
	dc, _ := s.Attributes["device_class"].(string)
	return strings.ToLower(dc)
}
