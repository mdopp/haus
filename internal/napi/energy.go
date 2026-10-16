package napi

import (
	"net/http"
	"sort"
	"strings"

	"github.com/mdopp/haus/internal/energie"
	"github.com/mdopp/haus/internal/ha"
)

// The energy picture (GET /napi/portal/energy → {ok, energy:{flow, totals,
// circuits}}): the "now" flow from current-power sensors (W), the lifetime
// counters (kWh) as totals, every other power sensor as a circuit. Which
// sensors those are is package energie's choice, the Energie page's too.

// EnergyMap names the energy entities (package energie).
type EnergyMap = energie.Map

// EnergyTotal is one labelled lifetime counter.
type EnergyTotal = energie.Total

// EnergyFromEnv reads HAUS_ENERGY_FLOW and HAUS_ENERGY_TOTALS.
func EnergyFromEnv() (EnergyMap, error) { return energie.FromEnv() }

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

func deviceClass(s ha.State) string { return energie.DeviceClass(s) }

func (h *homeAPI) energy(w http.ResponseWriter, r *http.Request) {
	states, err := h.ha.States(r.Context())
	if err != nil {
		refuse(w, http.StatusBadGateway, "ha_unavailable")
		return
	}
	legs, counters, used := h.sources.Pick(states)
	flow, totals := []map[string]any{}, []map[string]any{}
	for _, l := range legs {
		m := meter(l.State)
		m["label"], m["sense"] = l.Label, l.Sense
		flow = append(flow, m)
	}
	for _, c := range counters {
		m := meter(c.State)
		m["label"] = c.Label
		totals = append(totals, m)
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
