package napi

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mdopp/homes/internal/energie"
	"github.com/mdopp/homes/internal/ha"
)

// The energy picture (GET /napi/portal/energy → {ok, energy:{flow, totals,
// circuits, charge}}): the "now" flow from current-power sensors (W), the lifetime
// counters (kWh) as totals, each with what it added since the house's
// midnight (`today`, kWh, as the Energie page's Heute; left out when HA's
// history does not say), every other power sensor as a circuit, and how
// full the battery is (`charge`, its sensor in %, as the Energie page's Akku
// ring; left out without a battery leg or a number from 0 to 100). Which
// sensors those are is package energie's choice, the Energie page's too.

// EnergyMap names the energy entities (package energie).
type EnergyMap = energie.Map

// EnergyTotal is one labelled lifetime counter.
type EnergyTotal = energie.Total

// EnergyFromEnv reads HOMES_ENERGY_FLOW and HOMES_ENERGY_TOTALS.
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
	today := h.today(r.Context(), counters)
	for _, c := range counters {
		m := meter(c.State)
		m["label"] = c.Label
		if v, ok := today[c.State.EntityID]; ok {
			m["today"] = v
		}
		totals = append(totals, m)
	}
	energy := map[string]any{"flow": flow, "totals": totals}
	if charge, ok := h.charge(legs, states); ok {
		energy["charge"] = charge
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
	energy["circuits"] = circuits
	answer(w, http.StatusOK, map[string]any{"ok": true, "energy": energy})
}

// charge is the battery's charge as a meter (its state the percent), when
// the flow has a battery leg and the sensor says a number from 0 to 100 —
// the same sensor the Energie page rings its Akku node with
// (energie.Map.Charge).
func (h *homeAPI) charge(legs []energie.Leg, states []ha.State) (map[string]any, bool) {
	for _, l := range legs {
		if l.Key != "battery" {
			continue
		}
		if _, s, ok := h.sources.Charge(states); ok {
			m := meter(s)
			m["label"] = "Akku-Füllstand"
			return m, true
		}
		return nil, false
	}
	return nil, false
}

// historyWait is how long the energy picture waits for today's history; a
// counter whose history is later is shown without today.
const historyWait = 5 * time.Second

// today is what each counter added since the house's midnight, in kWh
// (energie.Added), read from HA's history at once; a counter whose
// history fails or has no reading since midnight is left out.
func (h *homeAPI) today(ctx context.Context, counters []energie.Counter) map[string]float64 {
	loc := h.loc
	if loc == nil {
		loc = time.Local
	}
	now := time.Now()
	if h.now != nil {
		now = h.now()
	}
	y, m, d := now.In(loc).Date()
	start := time.Date(y, m, d, 0, 0, 0, 0, loc)
	ctx, cancel := context.WithTimeout(ctx, historyWait)
	defer cancel()
	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		out = map[string]float64{}
	)
	for _, c := range counters {
		wg.Add(1)
		go func() {
			defer wg.Done()
			points, err := h.ha.History(ctx, c.State.EntityID, start, now.Add(time.Minute))
			if err != nil {
				return
			}
			if v, ok := energie.Added(points, c.State); ok {
				mu.Lock()
				out[c.State.EntityID] = v
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return out
}
