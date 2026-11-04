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
// the thing's name, a large slider and quick steps. The same on a
// thing's own page.
func TestTheLevelOpensASheet(t *testing.T) {
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
	for _, part := range strings.Split(ess, "<div popover id=regler")[1:] {
		if i := strings.Index(part, "Fertig</button></div>"); i < 0 || !strings.Contains(part[:i], "<input type=range") {
			t.Fatalf("a sheet without its slider and Fertig:\n%s", part)
		}
	}
	if n, m := strings.Count(ess, "<div popover id=regler"), strings.Count(ess, "<input type=range"); n != m || n != 4 { // Bambi, the Dimmer (off), Rollo, Esszimmerjalousie
		t.Fatalf("%d sheets for %d sliders, want 4:\n%s", n, m, ess)
	}
	_, own, _ := familyPage(t, Ding, StartConfig{HA: hc}, "GET", "/raeume/light.bambi", "family")
	mustContain(t, own, `an · <button type=button class=stufe popovertarget=regler-light-bambi`, `<div popover id=regler-light-bambi class=blatt`)

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

// owner, 2026-11-04 (a screenshot of /raeume): a lamp that is off said
// "aus" and nothing opened its sheet. A lamp HA can dim has "aus" as the
// button, accented like "60 %", and its sheet says "aus" large; a step
// turns it on at that level in one call (light.turn_on, brightness_pct).
// A lamp that only switches says a plain "aus"; one HA does not reach
// has neither.
func TestAnOffDimmerSaysAusAsItsLevel(t *testing.T) {
	fake, hc := house(t)
	dimHouse(t, fake)
	fake.Set("light.schalter", "off", map[string]any{"friendly_name": "Stehlampe", "supported_color_modes": []any{"onoff"}})
	_, body, _ := familyPage(t, Raeume, StartConfig{HA: hc}, "GET", "/raeume", "family")
	ess := section(t, body, "karte-esszimmer")
	mustContain(t, ess,
		// the dimmer: its switch, and "aus" the button that opens its sheet
		`role=switch aria-checked=false aria-label="Z-Wave-Dimmer"`,
		`Z-Wave-Dimmer</a> <span class=k><button type=button class=stufe popovertarget=regler-light-dimmer aria-haspopup=dialog aria-label="Z-Wave-Dimmer Helligkeit aus, einstellen">aus</button></span>`,
		`<div popover id=regler-light-dimmer class=blatt role=dialog aria-labelledby=regler-light-dimmer-titel><p class=blatt-titel id=regler-light-dimmer-titel>Z-Wave-Dimmer</p>`,
		`<span class=k>Helligkeit</span> <output>aus</output></p><input type=range name=wert min=0 max=100 step=1 value=0 aria-label="Z-Wave-Dimmer Helligkeit"`,
		`name=geraet value="light.dimmer"><input type=hidden name=tun value="light.turn_on"><input type=hidden name=zurueck value="/raeume#karte-esszimmer"><input type=hidden name=wert value=25><button data-laeuft="…" aria-label="Helligkeit 25 %">25 %</button>`,
		// the lamp that only switches: a plain "aus"
		`Stehlampe</a> <span class=k>aus</span>`)
	mustNotContain(t, ess, `popovertarget=regler-light-schalter`, `id=regler-light-schalter`, `aria-label="Stehlampe Helligkeit`)

	// a thing's own page: the same
	_, own, _ := familyPage(t, Ding, StartConfig{HA: hc}, "GET", "/raeume/light.dimmer", "family")
	mustContain(t, own, `<button type=button class=stufe popovertarget=regler-light-dimmer aria-haspopup=dialog aria-label="Z-Wave-Dimmer Helligkeit aus, einstellen">aus</button>`,
		`<div popover id=regler-light-dimmer class=blatt`, `<output>aus</output>`)
	_, plain, _ := familyPage(t, Ding, StartConfig{HA: hc}, "GET", "/raeume/light.schalter", "family")
	mustNotContain(t, plain, "<input type=range", "<div popover", "class=stufe")

	// a quick step turns it on at its level: exactly one call
	h := Schalten(StartConfig{HA: hc})
	before := len(fake.Calls())
	if rec := post(t, h, "/schalten", url.Values{"geraet": {"light.dimmer"}, "tun": {"light.turn_on"}, "wert": {"25"}, "zurueck": {"/raeume#karte-esszimmer"}}, "anna", nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("step → %d", rec.Code)
	}
	if calls := fake.Calls()[before:]; len(calls) != 1 || calls[0].Domain+"."+calls[0].Service != "light.turn_on" || fmtData(calls[0].Data) != "brightness_pct:25 entity_id:light.dimmer" {
		t.Fatalf("a step on an off lamp called %v, want one light.turn_on with brightness_pct", calls)
	}

	// switched on, the level is a percentage again (the live update fetches this page)
	fake.Set("light.dimmer", "on", map[string]any{"friendly_name": "Z-Wave-Dimmer", "brightness": 64.0, "supported_color_modes": []any{"brightness"}})
	_, body, _ = familyPage(t, Raeume, StartConfig{HA: hc}, "GET", "/raeume", "family")
	mustContain(t, section(t, body, "karte-esszimmer"), `Z-Wave-Dimmer</a> <span class=k>an · <button type=button class=stufe popovertarget=regler-light-dimmer aria-haspopup=dialog aria-label="Z-Wave-Dimmer Helligkeit 25 %, einstellen">25 %</button>`)

	// "nicht erreichbar" is never a button, even for a lamp HA can dim
	fake.Set("light.dimmer", "unavailable", map[string]any{"friendly_name": "Z-Wave-Dimmer", "supported_color_modes": []any{"brightness"}})
	_, body, _ = familyPage(t, Raeume, StartConfig{HA: hc}, "GET", "/raeume", "family")
	ess = section(t, body, "karte-esszimmer")
	mustContain(t, ess, `Z-Wave-Dimmer</a> <span class=k>nicht erreichbar</span>`)
	mustNotContain(t, ess, `popovertarget=regler-light-dimmer`, `id=regler-light-dimmer`, `aria-label="Z-Wave-Dimmer Helligkeit`)
	_, gone, _ := familyPage(t, Ding, StartConfig{HA: hc}, "GET", "/raeume/light.dimmer", "family")
	mustNotContain(t, gone, "<input type=range", "<div popover", "class=stufe")
}
