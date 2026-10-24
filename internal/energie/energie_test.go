package energie

import (
	"testing"

	"github.com/mdopp/haus/internal/ha"
)

// haus#65: HAUS_ENERGY_STATS names the history's statistic ids; unset,
// the counters Pick found are the history's, by their fallback names or
// the template's labels.
func TestHistoryIDs(t *testing.T) {
	t.Setenv("HAUS_ENERGY_FLOW", "")
	t.Setenv("HAUS_ENERGY_TOTALS", "")
	t.Setenv("HAUS_ENERGY_STATS", " bezug=sensor.senec_grid_import_energy, Einspeisung=sensor.senec_grid_export_energy,solar=sensor.senec_solar_energy")
	m, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	want := Stats{Bezug: "sensor.senec_grid_import_energy", Einspeisung: "sensor.senec_grid_export_energy", Solar: "sensor.senec_solar_energy"}
	if m.Stats != want || m.History(nil) != want || m.Set() {
		t.Fatalf("stats %+v, history %+v, set %v", m.Stats, m.History(nil), m.Set())
	}
	for _, bad := range []string{"akku=sensor.x", "bezug=", "bezug", "solar=sensor.X"} {
		t.Setenv("HAUS_ENERGY_STATS", bad)
		if _, err := FromEnv(); err == nil {
			t.Errorf("%q must refuse to start", bad)
		}
	}
	st := func(id string) ha.State { return ha.State{EntityID: id} }
	got := Map{}.History([]Counter{
		{Label: "Batterie geladen", Fallback: "Batterie laden", State: st("sensor.akku")},
		{Label: "PV-Erzeugung", Fallback: "PV Erzeugung", State: st("sensor.pv")},
		{Label: "Einspeisung", Fallback: "Netzeinspeisung", State: st("sensor.ein")},
		{Label: "Netzbezug", State: st("sensor.bezug")},
	})
	if got != (Stats{Bezug: "sensor.bezug", Einspeisung: "sensor.ein", Solar: "sensor.pv"}) {
		t.Fatalf("from the counters: %+v", got)
	}
}

// soc= names the battery's charge (owner, 2026-10-24); it is no flow leg.
func TestFromEnvTakesTheCharge(t *testing.T) {
	t.Setenv("HAUS_ENERGY_FLOW", "battery=sensor.akku,soc=sensor.akku_soc")
	t.Setenv("HAUS_ENERGY_TOTALS", "")
	t.Setenv("HAUS_ENERGY_STATS", "")
	m, err := FromEnv()
	if err != nil || m.Flow["soc"] != "sensor.akku_soc" {
		t.Fatalf("%v %v", m, err)
	}
	pct, _, ok := m.Charge([]ha.State{{EntityID: "sensor.akku_soc", State: "41"}})
	if !ok || pct != 41 {
		t.Fatalf("%v %v", pct, ok)
	}
	flow, _, _ := m.Pick([]ha.State{{EntityID: "sensor.akku", State: "-300"}, {EntityID: "sensor.akku_soc", State: "41"}})
	if len(flow) != 1 || flow[0].Key != "battery" {
		t.Fatalf("legs %v", flow)
	}
}
