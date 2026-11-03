package pages

import (
	"testing"

	"github.com/mdopp/homes/internal/energie"
)

// The owner (2026-10-24): Jetzt shows how full the battery is, in percent
// — as a ring along the Akku pill's stadium outline with its number on it (the widget's,
// 2026-11-03), in the words for a screen reader and in the table. SENEC's sensor by its friendly name, or
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
	mustContain(t, jetzt, "<text class=fuell-zahl x=324 y=99 text-anchor=middle>74\u00a0%</text>", `<path class=fuell d="M324 95H352A40 40 0 0 1 352 175H296A40 40 0 0 1 296 95Z" pathLength=100 stroke-dasharray="73.6 100">`,
		"Akku 74\u00a0% voll", "<th scope=row>Akku Füllstand</th><td>74\u00a0%</td>")
	mustNotContain(t, jetzt, "12\u00a0%")

	// the template's own sensor
	cfg.Energy = energie.Map{Flow: map[string]string{"pv": "sensor.pv", "house": "sensor.haus", "grid": "sensor.netz", "battery": "sensor.akku", "soc": "sensor.handy_akku"}}
	_, body, _ = familyPage(t, Energie, cfg, "GET", "/energie", "family")
	mustContain(t, body, "<text class=fuell-zahl x=324 y=99 text-anchor=middle>12\u00a0%</text>", `stroke-dasharray="12 100"`)

	// not a number: no charge, the rest as before
	cfg.Energy = energie.Map{}
	fake.Set("sensor.senec_battery_charge_percent", "unavailable", map[string]any{"friendly_name": "Akku-Füllstand in Prozent", "unit_of_measurement": "%"})
	_, body, _ = familyPage(t, Energie, cfg, "GET", "/energie", "family")
	mustContain(t, body, "<tspan class=nm>Akku</tspan> lädt</text>")
	mustNotContain(t, body, "class=fuell", "Akku Füllstand")
}
