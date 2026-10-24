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
	"github.com/mdopp/haus/internal/persona"
)

// Notizen says where notes come from, and a person writes them here
// (owner, 2026-10-24): Neue Notiz makes one and shows it with „Notiz
// angelegt.“; Bearbeiten puts its whole file in a field and saves it,
// unless someone else changed it since — then both texts are there and
// nothing is overwritten. Only from Haus's own page; what does not work is
// said, the text kept.
func TestNotizenWrite(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "einkaufsliste.md"), []byte("# Einkaufsliste\n\nMilch\n"), 0o640)
	nd, err := notes.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg := StartConfig{Notes: nd}
	h := Notizen(cfg)

	_, body, _ := familyPage(t, Notizen, cfg, "GET", "/notizen", "family")
	mustContain(t, body, "Notizen entstehen, wenn du Haus sagst „Merk dir …“", "Netzlaufwerk",
		`<details class=neu><summary>Neue Notiz</summary><form class=notizform method=post action=/notizen>`, `name=titel required`, `<textarea id=text name=text>`)
	mustContain(t, body, `<button data-laeuft="Legt an …">Anlegen</button>`, hausScript)
	// the persona by its name where the page says how to address it
	House.Display = "Joshua"
	_, body, _ = familyPage(t, Notizen, cfg, "GET", "/notizen", "family")
	House = persona.Default()
	mustContain(t, body, "wenn du Joshua sagst „Merk dir …“", "Joshua findet sie wieder")

	// a new note
	rec := post(t, h, "/notizen", url.Values{"titel": {"Wasserzähler"}, "text": {"Nummer 1234\nim Keller"}}, "anna", nil)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/notizen/wasserzaehler.md?gesagt=angelegt" {
		t.Fatalf("%d %q", rec.Code, rec.Header().Get("Location"))
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "wasserzaehler.md")); string(got) != "# Wasserzähler\n\nNummer 1234\nim Keller\n" {
		t.Fatalf("file %q", got)
	}
	_, body, _ = familyPage(t, Notizen, cfg, "GET", "/notizen/wasserzaehler.md?gesagt=angelegt", "family")
	mustContain(t, body, "<div class=gut role=status><span class=punkt></span><p>Notiz angelegt.</p></div>", "Nummer 1234",
		`<a class=knopf href="/notizen/wasserzaehler.md?bearbeiten=1">Bearbeiten</a>`)

	// no title: said, the text kept, nothing written
	rec = post(t, h, "/notizen", url.Values{"titel": {"  "}, "text": {"nur Text <b>"}}, "anna", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("no title → %d", rec.Code)
	}
	mustContain(t, rec.Body.String(), saysNoteNoTitle, "<details class=neu open>", "nur Text &lt;b&gt;")

	// from elsewhere, or nobody: nothing
	if rec := post(t, h, "/notizen", url.Values{"titel": {"X"}, "text": {"y"}}, "anna", map[string]string{"Sec-Fetch-Site": "cross-site"}); rec.Code != http.StatusForbidden {
		t.Errorf("cross-site → %d", rec.Code)
	}
	if rec := post(t, h, "/notizen", url.Values{"titel": {"X"}, "text": {"y"}}, "", nil); rec.Code != http.StatusForbidden {
		t.Errorf("no person → %d", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(dir, "x.md")); err == nil {
		t.Error("a refused post wrote a note")
	}

	// Bearbeiten: the whole file and the time it was read
	_, body, _ = familyPage(t, Notizen, cfg, "GET", "/notizen/einkaufsliste.md?bearbeiten=1", "family")
	mustContain(t, body, "<h1>Einkaufsliste bearbeiten</h1>", `<form class=notizform method=post action="/notizen/einkaufsliste.md">`,
		"<textarea id=text name=text># Einkaufsliste\n\nMilch\n</textarea>", ">Speichern</button>", `href="/notizen/einkaufsliste.md">Abbrechen</a>`)
	stand := regexp.MustCompile(`name=stand value=(\d+)>`).FindStringSubmatch(body)
	if stand == nil {
		t.Fatalf("no stand:\n%s", body)
	}
	rec = post(t, h, "/notizen/einkaufsliste.md", url.Values{"stand": {stand[1]}, "text": {"# Einkaufsliste\n\nMilch\nBrot"}}, "anna", nil)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/notizen/einkaufsliste.md?gesagt=gespeichert" {
		t.Fatalf("save → %d %q\n%s", rec.Code, rec.Header().Get("Location"), rec.Body)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "einkaufsliste.md"))
	if string(got) != "# Einkaufsliste\n\nMilch\nBrot\n" {
		t.Fatalf("saved %q", got)
	}
	if st, _ := os.Stat(filepath.Join(dir, "einkaufsliste.md")); st.Mode().Perm() != 0o640 {
		t.Errorf("mode %v, want the file's own 0640", st.Mode().Perm())
	}

	// the same old stand again: someone (here: that save) changed it — both texts, nothing overwritten
	rec = post(t, h, "/notizen/einkaufsliste.md", url.Values{"stand": {stand[1]}, "text": {"# Einkaufsliste\n\nKäse"}}, "ben", nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("stale → %d", rec.Code)
	}
	mustContain(t, rec.Body.String(), saysNoteChanged, "<div class=vorher># Einkaufsliste\n\nMilch\nBrot\n</div>", "<textarea id=text name=text># Einkaufsliste\n\nKäse</textarea>")
	if got, _ := os.ReadFile(filepath.Join(dir, "einkaufsliste.md")); !strings.Contains(string(got), "Brot") {
		t.Fatalf("overwritten: %q", got)
	}

	// what is no note here
	for _, bad := range []string{"/notizen/../geheim.md", "/notizen/.versteckt.md", "/notizen/gibtsnicht.md"} {
		rec := post(t, h, bad, url.Values{"stand": {"1"}, "text": {"x"}}, "anna", nil)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s → %d", bad, rec.Code)
		}
	}
	// too long: said, kept, not written
	long := strings.Repeat("a", notes.MaxText+1)
	rec = post(t, h, "/notizen/einkaufsliste.md", url.Values{"stand": {stand[1]}, "text": {long}}, "anna", nil)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), saysNoteTooLong) {
		t.Errorf("too long → %d", rec.Code)
	}
}
