package pages

import (
	"strings"
	"testing"
)

// Owner 2026-10-24, 2026-11-07: Energie on Start is part of Im Haus' one
// line — the roof, the house, the battery's charge — leading to Energie; Geräte shows
// what plays on top, once, with its buttons landing back on Geräte.
func TestStartHasTheEnergyLineAndGeraeteWhatPlays(t *testing.T) {
	fake, hc := house(t)
	flowLegs(t, fake, "3200", "800", "-1600", "800")
	fake.Set("sensor.soc", "74", map[string]any{"friendly_name": "Akku-Füllstand in Prozent", "unit_of_measurement": "%"})
	fake.Set("media_player.wz", "playing", map[string]any{"friendly_name": "Sonos", "media_title": "Abbey Road", "supported_features": fullPlayer})
	cfg := StartConfig{HA: hc}
	_, body, _ := familyPage(t, Start, cfg, "GET", "/start", "family")
	mustContain(t, section(t, body, "karte-im-haus"), "Sonos spielt · <a href=/energie>Dach 3,2 kW · Haus 800 W · Akku 74 %</a>")
	mustNotContain(t, body, "karte-energie", "ins Netz")
	cfg.OhneEnergie = true // energie switched off: the line without it
	_, body, _ = familyPage(t, Start, cfg, "GET", "/start", "family")
	mustNotContain(t, body, "/energie>")
	cfg.OhneEnergie = false

	_, body, _ = familyPage(t, Raeume, cfg, "GET", "/raeume", "family")
	mustContain(t, body, "<title>Geräte · Homes</title>", "<h1>Geräte</h1>")
	laeuft := section(t, body, "karte-laeuft-gerade")
	mustContain(t, laeuft, `<a href="/raeume/media_player.wz">Sonos</a>`, `value="media_player.media_pause"`, `name=zurueck value="/raeume"`)
	if strings.Count(body, ">Sonos</a>") != 1 {
		t.Error("Sonos twice on Geräte")
	}
	// nothing plays: no card
	fake.Set("media_player.wz", "idle", map[string]any{"friendly_name": "Sonos"})
	_, body, _ = familyPage(t, Raeume, cfg, "GET", "/raeume", "family")
	mustNotContain(t, body, "karte-laeuft-gerade")
}
