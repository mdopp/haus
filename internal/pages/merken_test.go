package pages

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/mdopp/haus/internal/merken"
	"github.com/mdopp/haus/internal/notes"
)

// haus#117: „Aus Link merken“ on Notizen keeps a page as a note and opens
// it with its sentence; the same link again opens the note it made; what
// cannot be had is said with the link still in the field; never from
// another site.
func TestNotizenAusLinkMerken(t *testing.T) {
	page := `<html><head><title>Kürbissuppe</title><script type="application/ld+json">{"@type":"Recipe","name":"Kürbissuppe",
"recipeIngredient":["1 Hokkaido","1 l Brühe"],"recipeInstructions":"Kürbis würfeln.\nKochen und pürieren."}</script></head><body></body></html>`
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(page))
	}))
	defer site.Close()
	dir := t.TempDir()
	nd, _ := notes.Open(dir)
	cfg := StartConfig{Notes: nd, Fetcher: merken.Fetcher{AllowPrivate: true}}
	h := Notizen(cfg)

	_, body, _ := familyPage(t, Notizen, cfg, "GET", "/notizen", "family")
	mustContain(t, body, "<h2 id=k-merken>Aus Link merken</h2>", `<form class=notizform method=post action=/notizen/merken>`, `<input id=link type=url name=link required`,
		`<button data-laeuft="Liest die Seite …">Merken</button>`, "auf Signal schicken")

	rec := post(t, h, "/notizen/merken", url.Values{"link": {site.URL + "/suppe"}, "hinweis": {"#herbst"}}, "anna", nil)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/notizen/kuerbissuppe.md?gesagt=gemerkt" {
		t.Fatalf("%d %q %s", rec.Code, rec.Header().Get("Location"), rec.Body)
	}
	b, _ := os.ReadFile(dir + "/kuerbissuppe.md")
	if !strings.Contains(string(b), "art: rezept\ntags: [herbst, kochen]") || !strings.Contains(string(b), "1. Kürbis würfeln.\n2. Kochen und pürieren.") {
		t.Fatalf("%s", b)
	}
	_, body, _ = familyPage(t, Notizen, cfg, "GET", "/notizen/kuerbissuppe.md?gesagt=gemerkt", "family")
	mustContain(t, body, noteSaid["gemerkt"], "Rezept", "Quelle: 127.0.0.1")

	rec = post(t, h, "/notizen/merken", url.Values{"link": {site.URL + "/suppe"}}, "anna", nil)
	if rec.Header().Get("Location") != "/notizen/kuerbissuppe.md?gesagt=schon" {
		t.Fatalf("twice: %q", rec.Header().Get("Location"))
	}
	rec = post(t, h, "/notizen/merken", url.Values{"link": {"kein link"}, "hinweis": {"dazu"}}, "anna", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("no address: %d", rec.Code)
	}
	mustContain(t, rec.Body.String(), merken.ErrNoAddress.Error(), `value="kein link"`, `value="dazu"`)
	if rec := post(t, h, "/notizen/merken", url.Values{"link": {site.URL}}, "anna", map[string]string{"Sec-Fetch-Site": "cross-site"}); rec.Code != http.StatusForbidden {
		t.Errorf("cross-site → %d", rec.Code)
	}
}
