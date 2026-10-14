package pages

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mdopp/haus/internal/werkstatt"
)

// haus#15: the Box page lists the Werkstatt's open tickets — what pi works
// (pi:ready) and what waits for Claude (needs-claude) — read-only, linked.
func TestBoxShowsTheWerkstattTickets(t *testing.T) {
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/mdopp/laib/issues" {
			http.NotFound(w, r)
			return
		}
		var out []map[string]any
		switch r.URL.Query().Get("labels") {
		case "pi:ready":
			out = []map[string]any{{"number": 7, "title": "Tippfehler im README", "html_url": "https://github.com/mdopp/laib/issues/7"}}
		case "needs-claude":
			out = []map[string]any{{"number": 9, "title": "Lease <hängt>", "html_url": "https://github.com/mdopp/laib/issues/9"}}
		}
		json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(gh.Close)
	tk := &werkstatt.Tickets{API: gh.URL, Repos: []string{"mdopp/laib"}}
	page := get(BoxWith(fakeLaib(t, map[string]any{"state": "bereit"}, nil), "https://laib.box.test", tk), "admins")
	for _, want := range []string{"Werkstatt", "Tippfehler im README", "https://github.com/mdopp/laib/issues/7", "Lease &lt;hängt&gt;", "wartet auf Claude"} {
		if !strings.Contains(page, want) {
			t.Fatalf("the Werkstatt card lacks %q:\n%s", want, page)
		}
	}
}

// No Werkstatt configured: no card, and the page does not break.
func TestBoxWithoutAWerkstattHasNoCard(t *testing.T) {
	page := get(BoxWith(fakeLaib(t, map[string]any{"state": "bereit"}, nil), "https://laib.box.test", nil), "admins")
	if strings.Contains(page, "Werkstatt") {
		t.Fatalf("a card without a Werkstatt:\n%s", page)
	}
}

// GitHub down: the card says so in plain words; the rest of the page stands.
func TestBoxSaysWhenGitHubDoesNotAnswer(t *testing.T) {
	tk := &werkstatt.Tickets{API: "http://127.0.0.1:1", Repos: []string{"mdopp/laib"}}
	page := get(BoxWith(fakeLaib(t, map[string]any{"state": "bereit"}, nil), "https://laib.box.test", tk), "admins")
	if !strings.Contains(page, "Werkstatt") || !strings.Contains(page, "nicht erreichbar") || !strings.Contains(page, "Was läuft") {
		t.Fatalf("GitHub down must be said plainly and not break the page:\n%s", page)
	}
}
