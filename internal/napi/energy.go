package napi

import (
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/mdopp/haus/internal/ha"
)

// The energy picture (GET /napi/portal/energy → {ok, energy:{flow, totals,
// circuits}}): the "now" flow from current-power sensors (W), the lifetime
// counters (kWh) as totals, every other power sensor as a circuit.
//
// Which sensors those are is the house's, so the template names them by
// entity id (EnergyFromEnv). Without that, the flow and totals are found by
// the friendly names one house used — a fallback, not a convention.

// the four flow legs, in the order the app draws them; sense fixes the sign
var energyFlow = []struct{ key, label, name, sense string }{
	{"pv", "PV", "Aktuell erzeugter PV-Strom", "supply"},     // ≥0 erzeugt
	{"house", "Haus", "Aktueller Hausverbrauch", "draw"},     // ≥0 verbraucht
	{"grid", "Netz", "Aktuelle Netz Leistung", "grid"},       // +W Bezug, −W Einspeisung
	{"battery", "Akku", "Aktuelle Akku Leistung", "battery"}, // −W entlädt, +W lädt
}

// the fallback's lifetime counters: friendly name → label
var energyTotals = []struct{ name, label string }{
	{"PV Erzeugung", "PV-Erzeugung"},
	{"Netzeinspeisung", "Einspeisung"},
	{"Netzbezug", "Netzbezug"},
	{"Batterie laden", "Batterie geladen"},
	{"Batterie entladen", "Batterie entladen"},
}

// EnergyMap names the energy entities: Flow by leg (pv, house, grid,
// battery), Totals as labelled counters in the order given.
type EnergyMap struct {
	Flow   map[string]string
	Totals []EnergyTotal
}

// EnergyTotal is one labelled lifetime counter.
type EnergyTotal struct{ Label, Entity string }

func (m EnergyMap) set() bool { return len(m.Flow) > 0 || len(m.Totals) > 0 }

// EnergyFromEnv reads HAUS_ENERGY_FLOW (pv=sensor.x,house=…,grid=…,battery=…)
// and HAUS_ENERGY_TOTALS (Label=sensor.y,…). Both unset: the fallback.
func EnergyFromEnv() (EnergyMap, error) {
	m := EnergyMap{Flow: map[string]string{}}
	legs := map[string]bool{}
	for _, f := range energyFlow {
		legs[f.key] = true
	}
	for _, item := range strings.Split(os.Getenv("HAUS_ENERGY_FLOW"), ",") {
		if item = strings.TrimSpace(item); item == "" {
			continue
		}
		k, e, ok := strings.Cut(item, "=")
		k, e = strings.TrimSpace(k), strings.TrimSpace(e)
		if !ok || !legs[k] || !entityRE.MatchString(e) {
			return EnergyMap{}, fmt.Errorf("HAUS_ENERGY_FLOW %q: want pv|house|grid|battery=<entity_id>", item)
		}
		m.Flow[k] = e
	}
	for _, item := range strings.Split(os.Getenv("HAUS_ENERGY_TOTALS"), ",") {
		if item = strings.TrimSpace(item); item == "" {
			continue
		}
		i := strings.LastIndex(item, "=")
		if i <= 0 {
			return EnergyMap{}, fmt.Errorf("HAUS_ENERGY_TOTALS %q: want <label>=<entity_id>", item)
		}
		l, e := strings.TrimSpace(item[:i]), strings.TrimSpace(item[i+1:])
		if l == "" || !entityRE.MatchString(e) {
			return EnergyMap{}, fmt.Errorf("HAUS_ENERGY_TOTALS %q: want <label>=<entity_id>", item)
		}
		m.Totals = append(m.Totals, EnergyTotal{Label: l, Entity: e})
	}
	return m, nil
}

// meter is one sensor as the energy picture shows it (no nulls).
func meter(s ha.State) map[string]any {
	name, _ := s.Attributes["friendly_name"].(string)
	if name == "" {
		name = s.EntityID
	}
	m := map[string]any{"entity_id": s.EntityID, "name": name, "state": s.State}
	if dc := deviceClass(s); dc != "" {
		m["device_class"] = dc
	}
	if u := s.Attributes["unit_of_measurement"]; u != nil {
		m["unit"] = u
	}
	return m
}

func deviceClass(s ha.State) string {
	dc, _ := s.Attributes["device_class"].(string)
	return strings.ToLower(dc)
}

func (h *homeAPI) energy(w http.ResponseWriter, r *http.Request) {
	states, err := h.ha.States(r.Context())
	if err != nil {
		refuse(w, http.StatusBadGateway, "ha_unavailable")
		return
	}
	var flow, totals []map[string]any
	used := map[string]bool{}
	if h.sources.set() {
		flow, totals = h.configured(states, used)
	} else {
		flow, totals = byFriendlyName(states, used)
	}
	circuits := []map[string]any{}
	for _, s := range states {
		if strings.HasPrefix(s.EntityID, "sensor.") && deviceClass(s) == "power" && !used[s.EntityID] {
			circuits = append(circuits, meter(s))
		}
	}
	sort.SliceStable(circuits, func(i, j int) bool {
		return strings.ToLower(circuits[i]["name"].(string)) < strings.ToLower(circuits[j]["name"].(string))
	})
	answer(w, http.StatusOK, map[string]any{"ok": true, "energy": map[string]any{"flow": flow, "totals": totals, "circuits": circuits}})
}

// configured: the template's entity ids.
func (h *homeAPI) configured(states []ha.State, used map[string]bool) (flow, totals []map[string]any) {
	byID := map[string]ha.State{}
	for _, s := range states {
		byID[s.EntityID] = s
	}
	flow, totals = []map[string]any{}, []map[string]any{}
	for _, f := range energyFlow {
		s, ok := byID[h.sources.Flow[f.key]]
		if !ok {
			continue
		}
		m := meter(s)
		m["label"], m["sense"] = f.label, f.sense
		flow = append(flow, m)
		used[s.EntityID] = true
	}
	for _, t := range h.sources.Totals {
		s, ok := byID[t.Entity]
		if !ok {
			continue
		}
		m := meter(s)
		m["label"] = t.Label
		totals = append(totals, m)
		used[s.EntityID] = true
	}
	return flow, totals
}

// byFriendlyName: the fallback, by exact friendly name and device class.
func byFriendlyName(states []ha.State, used map[string]bool) (flow, totals []map[string]any) {
	legs := map[string]map[string]any{}
	counters := map[string]map[string]any{}
	for _, s := range states {
		if !strings.HasPrefix(s.EntityID, "sensor.") {
			continue
		}
		m := meter(s)
		name := m["name"].(string)
		switch deviceClass(s) {
		case "power":
			for _, f := range energyFlow {
				if f.name == name && legs[f.key] == nil {
					legs[f.key] = m
					used[s.EntityID] = true
				}
			}
		case "energy":
			for _, t := range energyTotals {
				if t.name == name && counters[name] == nil {
					m["label"] = t.label
					counters[name] = m
				}
			}
		}
	}
	flow, totals = []map[string]any{}, []map[string]any{}
	for _, f := range energyFlow {
		if m := legs[f.key]; m != nil {
			m["label"], m["sense"] = f.label, f.sense
			flow = append(flow, m)
		}
	}
	for _, t := range energyTotals {
		if m := counters[t.name]; m != nil {
			totals = append(totals, m)
		}
	}
	return flow, totals
}
