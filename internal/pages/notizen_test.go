package pages

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/haus/internal/notes"
)

// a notes folder as the share has it: notes at the top and in a folder,
// Syncthing's hidden versions, a file next to the folder
func shelf(t *testing.T) (string, *notes.Dir) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "notes")
	files := map[string]string{
		"einkaufsliste.md":         "# Einkaufsliste\n\nMilch\nBrot\n\nKäse <b>fett</b>\n<script>alert(1)</script>\n",
		"packliste.md":             "# Packliste\n\nZahnbürste\n",
		"rezepte/apfelkuchen.md":   "# Apfelkuchen\n\nÄpfel schälen, dann backen.\n",
		".stversions/geheim.md":    "# Geheim\n\nApfel und Passwort\n",
		"WLAN für Gäste.md":        "# WLAN für Gäste\n\nNetz: Gast\n",
		"schraeg.md":               "# <i>Schräger Titel</i>\n\nText\n",
		"../draussen.md":           "# Draußen\n\nApfel von draußen\n",
		"rezepte/.versteckt.md":    "# Versteckt\n\nApfel\n",
		"rezepte/zwiebelkuchen.md": "Zwiebeln, kein Titel, kein Apfel.\n",
	}
	age := time.Hour
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		age += time.Hour
		at := time.Now().Add(-age)
		if name == "einkaufsliste.md" {
			at = time.Now().Add(-time.Minute) // the newest
		}
		os.Chtimes(p, at, at)
	}
	d, err := notes.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return dir, d
}

// haus#12: Notizen — a search field and the newest notes, each leading to
// its own page.
func TestNotizenListsTheNewest(t *testing.T) {
	_, nd := shelf(t)
	code, body, _ := familyPage(t, Notizen, StartConfig{Notes: nd, Now: time.Now}, "GET", "/notizen", "family")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	mustContain(t, body, `<form method=get action=/notizen`, `name=q`, `type=search`,
		"<h2 id=karte-zuletzt-geaendert>Zuletzt geändert</h2>",
		`<a href="/notizen/einkaufsliste.md">Einkaufsliste</a>`,
		`<a href="/notizen/rezepte/apfelkuchen.md">Apfelkuchen</a>`,
		`<a href="/notizen/WLAN%20f%C3%BCr%20G%C3%A4ste.md">WLAN für Gäste</a>`,
		"heute")
	mustNotContain(t, body, "Geheim", "Versteckt", "Draußen", "<i>", "<script")
	before(t, body, "Einkaufsliste", "Packliste")
}

// The search is the notes package's: titles before text, any case,
// hidden files never; the query stays in the field as text.
func TestNotizenSearch(t *testing.T) {
	_, nd := shelf(t)
	_, body, _ := familyPage(t, Notizen, StartConfig{Notes: nd}, "GET", "/notizen?q=APFEL", "")
	mustContain(t, body, `value="APFEL"`, "Treffer für „APFEL“",
		`<a href="/notizen/rezepte/apfelkuchen.md">Apfelkuchen</a>`, "Äpfel schälen, dann backen.",
		`<a href="/notizen/rezepte/zwiebelkuchen.md">zwiebelkuchen</a>`)
	before(t, body, "Apfelkuchen", "zwiebelkuchen")
	mustNotContain(t, body, "Geheim", "Versteckt", "Draußen", "Einkaufsliste", "karte-zuletzt")

	_, body, _ = familyPage(t, Notizen, StartConfig{Notes: nd}, "GET", "/notizen?q="+url.QueryEscape(`"><script>alert(1)</script>`), "")
	mustContain(t, body, "Nichts gefunden", `value="&#34;&gt;&lt;script&gt;alert(1)&lt;/script&gt;"`)
	mustNotContain(t, body, "<script")

	// a blank query is no search
	_, body, _ = familyPage(t, Notizen, StartConfig{Notes: nd}, "GET", "/notizen?q=++", "")
	mustContain(t, body, "karte-zuletzt-geaendert")
	mustNotContain(t, body, "Treffer", "Nichts gefunden")
}

// Many hits: the first twenty, and how many more.
func TestNotizenSearchIsCapped(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 25; i++ {
		os.WriteFile(filepath.Join(dir, fmt.Sprintf("liste-%02d.md", i)), []byte(fmt.Sprintf("# Liste %02d\n", i)), 0o644)
	}
	nd, _ := notes.Open(dir)
	_, body, _ := familyPage(t, Notizen, StartConfig{Notes: nd}, "GET", "/notizen?q=liste", "")
	if n := strings.Count(body, `<a href="/notizen/liste-`); n != 20 {
		t.Fatalf("%d hits shown, want 20", n)
	}
	mustContain(t, body, "und 5 weitere")
}

// A note's own page: its Markdown as text — paragraphs and line breaks,
// never markup from the file.
func TestNotizShowsItsTextEscaped(t *testing.T) {
	_, nd := shelf(t)
	for _, path := range []string{"/notizen/einkaufsliste.md", "/notizen/einkaufsliste", "/notizen/Einkaufsliste"} {
		code, body, _ := familyPage(t, Notizen, StartConfig{Notes: nd}, "GET", path, "family")
		if code != 200 {
			t.Fatalf("%s → %d", path, code)
		}
		mustContain(t, body, "<title>Einkaufsliste · Haus</title>", ">Einkaufsliste</h1>",
			"Milch\nBrot", "Käse &lt;b&gt;fett&lt;/b&gt;", "&lt;script&gt;alert(1)&lt;/script&gt;",
			`href=/notizen`, "aria-current=page>Notizen</a>")
		mustNotContain(t, body, "<b>fett", "<script", "# Einkaufsliste")
		// two paragraphs, the line break inside the first kept
		if strings.Count(body, "<p class=notiz>") != 2 {
			t.Fatalf("%s: want two paragraphs:\n%s", path, body)
		}
	}
	_, body, _ := familyPage(t, Notizen, StartConfig{Notes: nd}, "GET", "/notizen/rezepte/apfelkuchen.md", "")
	mustContain(t, body, ">Apfelkuchen</h1>", "Äpfel schälen")
	_, body, _ = familyPage(t, Notizen, StartConfig{Notes: nd}, "GET", "/notizen/"+"schraeg.md", "")
	mustContain(t, body, "&lt;i&gt;Schräger Titel&lt;/i&gt;")
	mustNotContain(t, body, "<i>")
}

// No name leaves the folder, hidden notes are not notes, and a note that
// is not there is said plainly — a 404, nothing from outside.
func TestNotizRefusesWhatIsNotANote(t *testing.T) {
	dir, nd := shelf(t)
	os.Symlink(filepath.Join(dir, "..", "draussen.md"), filepath.Join(dir, "link.md"))
	for _, path := range []string{"/notizen/../draussen.md", "/notizen/..%2fdraussen.md", "/notizen/.stversions/geheim.md",
		"/notizen/rezepte/.versteckt.md", "/notizen/link.md", "/notizen/gibtsnicht", "/notizen/" + url.PathEscape(filepath.Join(dir, "einkaufsliste.md"))} {
		code, body, _ := familyPage(t, Notizen, StartConfig{Notes: nd}, "GET", path, "")
		if code != http.StatusNotFound {
			t.Fatalf("%s → %d, want 404", path, code)
		}
		mustContain(t, body, "Diese Notiz gibt es nicht", `href=/notizen`)
		mustNotContain(t, body, "Draußen", "Geheim", "Versteckt", "Passwort", "außerhalb", "versteckte")
	}
}
