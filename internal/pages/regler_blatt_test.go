package pages

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// owner, 2026-11-03 (a screenshot of /raeume on the phone): a finger
// scrolling the page moved the sliders. The card has no slider: the
// level in its line ("an · 60 %") is a button that opens a sheet with
// the thing's name, a large slider and quick steps. A lamp that is off
// has no level to tap, its switch turns it on. The same on a thing's own
// page.
func TestTheLevelOpensASheetAndAnOffLampHasNone(t *testing.T) {
	fake, hc := house(t)
	dimHouse(t, fake)
	_, body, _ := familyPage(t, Raeume, StartConfig{HA: hc}, "GET", "/raeume", "family")
	ess := section(t, body, "karte-esszimmer")
	mustContain(t, ess,
		// the level in the line is the button
		`Bambi</a> <span class=k>an · <button type=button class=stufe popovertarget=regler-light-bambi aria-haspopup=dialog aria-label="Bambi Helligkeit 60 %, einstellen">60 %</button></span>`,
		`Rollo</a> <span class=k>offen · <button type=button class=stufe popovertarget=regler-cover-rollo aria-haspopup=dialog aria-label="Rollo Position 40 %, einstellen">40 %</button></span>`,
		// the sheet: hidden until then, named by the thing
		`<div popover id=regler-light-bambi class=blatt role=dialog aria-labelledby=regler-light-bambi-titel><p class=blatt-titel id=regler-light-bambi-titel>Bambi</p>`,
		`<span class=k>Helligkeit</span> <output>60 %</output></p><input type=range name=wert min=0 max=100 step=1 value=60 aria-label="Bambi Helligkeit"`,
		`onchange="this.form.requestSubmit`,
		// the steps, each its own form with its wert
		`name=geraet value="light.bambi"><input type=hidden name=tun value="light.turn_on"><input type=hidden name=zurueck value="/raeume#karte-esszimmer"><input type=hidden name=wert value=25><button data-laeuft="…" aria-label="Helligkeit 25 %">25 %</button>`,
		`aria-label="Helligkeit 100 %">100 %</button>`,
		`name=geraet value="cover.rollo"><input type=hidden name=tun value="cover.set_cover_position"><input type=hidden name=zurueck value="/raeume#karte-esszimmer"><input type=hidden name=wert value=100><button data-laeuft="…" aria-label="Position Auf">Auf</button>`,
		`<input type=hidden name=wert value=50><button data-laeuft="…" aria-label="Position ½">½</button>`,
		`<input type=hidden name=wert value=0><button data-laeuft="…" aria-label="Position Zu">Zu</button>`,
		`<button type=button class=fertig popovertarget=regler-light-bambi popovertargetaction=hide>Fertig</button></div>`)
	// no slider on the card itself: every range is inside a sheet
	for _, part := range strings.Split(ess, "<div popover")[1:] {
		if i := strings.Index(part, "Fertig</button></div>"); i < 0 || !strings.Contains(part[:i], "<input type=range") {
			t.Fatalf("a sheet without its slider and Fertig:\n%s", part)
		}
	}
	if n, m := strings.Count(ess, "<div popover"), strings.Count(ess, "<input type=range"); n != m || n != 3 { // Bambi, Rollo, Esszimmerjalousie
		t.Fatalf("%d sheets for %d sliders, want 3:\n%s", n, m, ess)
	}
	// the Z-Wave-Dimmer is off: its switch and "aus", nothing to tap, no sheet
	mustContain(t, ess, `role=switch aria-checked=false aria-label="Z-Wave-Dimmer"`, `Z-Wave-Dimmer</a> <span class=k>aus</span>`)
	mustNotContain(t, ess, `popovertarget=regler-light-dimmer`, `id=regler-light-dimmer`, `aria-label="Z-Wave-Dimmer Helligkeit"`)

	// a thing's own page: the same; off, none
	_, own, _ := familyPage(t, Ding, StartConfig{HA: hc}, "GET", "/raeume/light.bambi", "family")
	mustContain(t, own, `an · <button type=button class=stufe popovertarget=regler-light-bambi`, `<div popover id=regler-light-bambi class=blatt`)
	_, off, _ := familyPage(t, Ding, StartConfig{HA: hc}, "GET", "/raeume/light.dimmer", "family")
	mustNotContain(t, off, "<input type=range", "<div popover", "class=stufe")

	// switched on, the level is back (the live update fetches this page)
	fake.Set("light.dimmer", "on", map[string]any{"friendly_name": "Z-Wave-Dimmer", "brightness": 51.0, "supported_color_modes": []any{"brightness"}})
	_, body, _ = familyPage(t, Raeume, StartConfig{HA: hc}, "GET", "/raeume", "family")
	mustContain(t, section(t, body, "karte-esszimmer"), `Z-Wave-Dimmer</a> <span class=k>an · <button type=button class=stufe popovertarget=regler-light-dimmer`)

	// a step is a plain switch call with its wert
	h := Schalten(StartConfig{HA: hc})
	before := len(fake.Calls())
	if rec := post(t, h, "/schalten", url.Values{"geraet": {"light.bambi"}, "tun": {"light.turn_on"}, "wert": {"25"}, "zurueck": {"/raeume#karte-esszimmer"}}, "anna", nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("step → %d", rec.Code)
	}
	if calls := fake.Calls()[before:]; len(calls) != 1 || fmtData(calls[0].Data) != "brightness_pct:25 entity_id:light.bambi" {
		t.Fatalf("a step called %v", calls)
	}
}
