package pages

import (
	"regexp"
	"strings"
	"testing"
)

// owner, 2026-11-03 (a screenshot of /raeume on the phone): a finger
// scrolling the page moved the sliders. A slider is folded behind a pill
// that says its level ("Helligkeit 60 %") and opens on a tap; a lamp that
// is off has no brightness at all, its switch turns it on. The same on a
// thing's own page.
func TestSlidersAreFoldedAndAnOffLampHasNone(t *testing.T) {
	fake, hc := house(t)
	dimHouse(t, fake)
	_, body, _ := familyPage(t, Raeume, StartConfig{HA: hc}, "GET", "/raeume", "family")
	ess := section(t, body, "karte-esszimmer")
	mustContain(t, ess,
		`<details class=regelung><summary><span class=nur-vorlesen>Bambi </span><span class=was>Helligkeit</span> <span class=wert>60 %</span><span class=aendern aria-hidden=true></span></summary><form method=post action=/schalten class=regler><input type=hidden name=geraet value="light.bambi">`,
		`<details class=regelung><summary><span class=nur-vorlesen>Rollo </span><span class=was>Position</span> <span class=wert>40 %</span>`,
		`aria-label="Bambi Helligkeit"`, `aria-label="Rollo Position"`)
	if strings.Contains(ess, "<details class=regelung open") || strings.Contains(ess, " open>") {
		t.Fatalf("a slider is open by default:\n%s", ess)
	}
	// every slider is inside its fold, none bare on the row
	if n, m := strings.Count(ess, "<details class=regelung>"), strings.Count(ess, "type=range"); n != m || n == 0 {
		t.Fatalf("%d folds for %d sliders:\n%s", n, m, ess)
	}
	if !regexp.MustCompile(`<details class=regelung><summary>.*?</summary><form method=post action=/schalten class=regler>.*?type=range.*?</form></details>`).MatchString(ess) {
		t.Fatalf("the slider is not inside its fold:\n%s", ess)
	}
	// the Z-Wave-Dimmer is off: its switch, no brightness
	mustContain(t, ess, `role=switch aria-checked=false aria-label="Z-Wave-Dimmer"`)
	mustNotContain(t, ess, `<span class=nur-vorlesen>Z-Wave-Dimmer </span>`, `aria-label="Z-Wave-Dimmer Helligkeit"`)

	// a thing's own page: the same fold; off, none
	_, own, _ := familyPage(t, Ding, StartConfig{HA: hc}, "GET", "/raeume/light.bambi", "family")
	mustContain(t, own, `<details class=regelung><summary><span class=nur-vorlesen>Bambi </span><span class=was>Helligkeit</span> <span class=wert>60 %</span>`)
	_, off, _ := familyPage(t, Ding, StartConfig{HA: hc}, "GET", "/raeume/light.dimmer", "family")
	mustNotContain(t, off, "<input type=range", "<details class=regelung>")

	// switched on, the brightness is back (the live update fetches this page)
	fake.Set("light.dimmer", "on", map[string]any{"friendly_name": "Z-Wave-Dimmer", "brightness": 51.0, "supported_color_modes": []any{"brightness"}})
	_, body, _ = familyPage(t, Raeume, StartConfig{HA: hc}, "GET", "/raeume", "family")
	mustContain(t, section(t, body, "karte-esszimmer"), `<span class=nur-vorlesen>Z-Wave-Dimmer </span><span class=was>Helligkeit</span> <span class=wert>20 %</span>`)
}
