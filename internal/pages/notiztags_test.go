package pages

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mdopp/haus/internal/notes"
)

// haus#116: a note gets its kind and tags when it is made — offered tags
// ticked, others typed — the list filters by tag (a tap adds or takes a
// tag out), the note's page shows them with their links, and Bearbeiten
// changes them in fields while every other line of the head stays.
func TestNotizenArtUndTags(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "alt.md"), []byte("---\nid: 4cfd\ntags: [reisen]\n---\n# Alt\n\nText\n"), 0o644)
	nd, _ := notes.Open(dir)
	cfg := StartConfig{Notes: nd}
	h := Notizen(cfg)

	_, body, _ := familyPage(t, Notizen, cfg, "GET", "/notizen", "family")
	mustContain(t, body, `<select id=art name=art>`, `<option value=rezept>Rezept</option>`, `<input type=checkbox name=tag value=kochen> #kochen`,
		`name=tags`, `<nav class=tagfilter`, `>#reisen <span class=k>1</span></a>`)

	rec := post(t, h, "/notizen", url.Values{"titel": {"Lasagne"}, "text": {"Nudeln"}, "art": {"rezept"}, "tag": {"kochen"}, "tags": {"Italienisch, vegetarisch"}}, "anna", nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "lasagne.md")); !strings.HasPrefix(string(b), "---\nart: rezept\ntags: [kochen, italienisch, vegetarisch]\n---\n\n# Lasagne\n") {
		t.Fatalf("file %q", b)
	}

	_, body, _ = familyPage(t, Notizen, cfg, "GET", "/notizen?tag=kochen", "family")
	mustContain(t, body, "<h2 id=karte-notizen-mit-kochen>Notizen mit #kochen</h2>", ">Lasagne</a>", "Rezept · #kochen #italienisch #vegetarisch",
		`href="/notizen" aria-current=true>#kochen`, `href=/notizen class=alle>alle</a>`, `<input type=checkbox name=tag value=kochen checked>`)
	mustNotContain(t, between(t, body, "karte-notizen-mit-kochen", "</section>"), ">Alt</a>")
	_, body, _ = familyPage(t, Notizen, cfg, "GET", "/notizen?tag=kochen&tag=reisen", "family")
	mustContain(t, body, "Keine Notiz trägt diese Tags.")

	_, body, _ = familyPage(t, Notizen, cfg, "GET", "/notizen/lasagne.md", "family")
	mustContain(t, body, `<p class="k kopf">Rezept <a href="/notizen?tag=kochen">#kochen</a>`)
	mustNotContain(t, body, "art: rezept", "---")

	// Bearbeiten: the fields, the body without its head; the old note's id kept
	_, body, _ = familyPage(t, Notizen, cfg, "GET", "/notizen/alt.md?bearbeiten=1", "family")
	mustContain(t, body, "<textarea id=text name=text># Alt\n\nText\n</textarea>", `<input type=checkbox name=tag value=reisen checked>`)
	mustNotContain(t, body, "id: 4cfd")
	stand := regexp.MustCompile(`name=stand value=(\d+)>`).FindStringSubmatch(body)
	rec = post(t, h, "/notizen/alt.md", url.Values{"stand": {stand[1]}, "text": {"# Alt\n\nNeu"}, "art": {"reise"}, "tag": {"ideen"}, "tags": {"reisen"}}, "anna", nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("save %d %s", rec.Code, rec.Body)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "alt.md")); string(b) != "---\nart: reise\ntags: [ideen, reisen]\nid: 4cfd\n---\n\n# Alt\n\nNeu\n" {
		t.Fatalf("saved %q", b)
	}
}
