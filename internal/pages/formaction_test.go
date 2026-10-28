package pages

import (
	"regexp"
	"strings"
	"testing"

	"github.com/mdopp/haus/internal/devices"
)

// postFormRE finds the rendered POST forms' opening tags.
var postFormRE = regexp.MustCompile(`(?i)<form\b[^>]*\bmethod=["']?post["']?[^>]*>`)

// formsWithoutAction are the POST forms in body that name no action.
func formsWithoutAction(body string) []string {
	var out []string
	for _, f := range postFormRE.FindAllString(body, -1) {
		if !strings.Contains(f, "action=") {
			out = append(out, f)
		}
	}
	return out
}

// haus#172: the script posted to fetch(getAttribute('action')), which is /null without one: pairing
// a phone and removing a lost one failed. The script now posts to the page itself then, and the
// forms that pair and remove phones name theirs; pairing goes as a whole page (it ends in the app).
func TestPostFormsGoSomewhere(t *testing.T) {
	if !strings.Contains(hausScript, "fetch(or(f.getAttribute('action'),location.pathname+location.search)") {
		t.Error("the script posts to null without an action")
	}
	store, err := devices.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store.Mint("Pixel von Anna", "anna")
	for _, c := range []struct {
		name string
		h    func() string
	}{
		{"pair", func() string { return getAt(PairDevice(store, StartConfig{}), "/pair-device", "family") }},
		{"geraete", func() string { return getAt(Geraete(store, "https://haus.box.test", StartConfig{}), "/box/geraete", "admins") }},
	} {
		body := c.h()
		if n := len(postFormRE.FindAllString(body, -1)); n == 0 {
			t.Fatalf("%s: no POST form at all:\n%s", c.name, body)
		}
		if bad := formsWithoutAction(body); len(bad) > 0 {
			t.Errorf("%s: forms without an action: %v", c.name, bad)
		}
	}
	mustContain(t, getAt(PairDevice(store, StartConfig{}), "/pair-device", "family"), "action=/pair-device data-ganz>")
}
