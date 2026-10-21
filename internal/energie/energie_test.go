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
