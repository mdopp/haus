package pages

import (
	"errors"
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
		{"geraete", func() string {
			return getAt(Geraete(store, "https://haus.box.test", StartConfig{}), "/box/geraete", "admins")
		}},
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

// haus#176: an error a person reads is a sentence; anything technical goes to the log
func TestFamilySentence(t *testing.T) {
	for in, want := range map[string]string{
		"Den Benutzer anna gibt es schon.":                                     "Den Benutzer anna gibt es schon.",
		`laib: Post "http://127.0.0.1:5900/mcp": dial tcp: connection refused`: "F",
		"store: write devices.json: no space left on device":                   "F",
		"open /data/x: permission denied":                                      "F",
		"Discogs kennt „Amore“ von Wanda nicht als Platte.":                    "Discogs kennt „Amore“ von Wanda nicht als Platte.",
	} {
		if got := familySentence("test", errors.New(in), "F"); got != want {
			t.Errorf("%q → %q", in, got)
		}
	}
	if s := hausScript; strings.Contains(s, "HTMLFormElement.prototype.submit") || strings.Contains(s, "7*sec") || !strings.Contains(s, "AbortController") {
		t.Error("the script resubmits natively, hides its sentence by itself, or has no limit")
	}
}
