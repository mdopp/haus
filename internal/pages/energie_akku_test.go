package pages

import (
	"testing"

	"github.com/mdopp/haus/internal/energie"
)

// The owner (2026-10-24): Jetzt shows how full the battery is, in percent
// — in the Akku node's name, as a ring around it, in the words for a
// screen reader and in the table. SENEC's sensor by its friendly name, or
// the template's soc=; nothing without one, nothing when it says no
// number, and nothing without an Akku node.
func TestEnergieJetztShowsTheBatteryCharge(t *testing.T) {
	fake, hc := house(t)
	flowLegs(t, fake, "3200", "800", "-1600", "800")
	fake.Set("sensor.senec_battery_charge_percent", "73.6", map[string]any{"friendly_name": "Akku-Füllstand in Prozent", "device_class": "battery", "unit_of_measurement": "%"})
	fake.Set("sensor.handy_akku", "12", map[string]any{"friendly_name": "Pixel Akku", "device_class": "battery", "unit_of_measurement": "%"})
	cfg := StartConfig{HA: hc}
	_, body, _ := familyPage(t, Energie, cfg, "GET", "/energie", "family")
	jetzt := between(t, body, "<section class=\"karte fluss\"", "</section>")
	mustContain(t, jetzt, ">Akku 74 %</text>", `<circle class=fuell `, "Akku 74 % voll", "<th scope=row>Akku Füllstand</th><td>74 %</td>")
	mustNotContain(t, jetzt, "12 %")

	// the template's own sensor
	cfg.Energy = energie.Map{Flow: map[string]string{"pv": "sensor.pv", "house": "sensor.haus", "grid": "sensor.netz", "battery": "sensor.akku", "soc": "sensor.handy_akku"}}
	_, body, _ = familyPage(t, Energie, cfg, "GET", "/energie", "family")
	mustContain(t, body, ">Akku 12 %</text>")

	// not a number: no charge, the rest as before
	cfg.Energy = energie.Map{}
	fake.Set("sensor.senec_battery_charge_percent", "unavailable", map[string]any{"friendly_name": "Akku-Füllstand in Prozent", "unit_of_measurement": "%"})
	_, body, _ = familyPage(t, Energie, cfg, "GET", "/energie", "family")
	mustContain(t, body, ">Akku</text>")
	mustNotContain(t, body, "circle class=fuell", "Akku Füllstand")
}
