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

	"github.com/mdopp/haus/internal/judge"
	"github.com/mdopp/haus/internal/judge/proxyfake"
	"github.com/mdopp/haus/internal/library"
	"github.com/mdopp/haus/internal/notes"
	"github.com/mdopp/haus/internal/qmd"
	"github.com/mdopp/haus/internal/qmd/qmdfake"
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
		"steuer.md":                "# Steuer\n\na\x1bb\x00c\x07\tdef\n",
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

	// any word of the query finds a note now (haus#13): the shopping list
	// holds a <script> line, which stays text
	_, body, _ = familyPage(t, Notizen, StartConfig{Notes: nd}, "GET", "/notizen?q="+url.QueryEscape(`"><script>alert(1)</script>`), "")
	mustContain(t, body, `value="&#34;&gt;&lt;script&gt;alert(1)&lt;/script&gt;"`, `<a href="/notizen/einkaufsliste.md">Einkaufsliste</a>`)
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
		mustContain(t, body, "<title>Einkaufsliste · Zuhause</title>", ">Einkaufsliste</h1>",
			"Milch\nBrot", "Käse &lt;b&gt;fett&lt;/b&gt;", "&lt;script&gt;alert(1)&lt;/script&gt;",
			`href=/notizen`, "href=/notizen aria-current=page>")
		mustNotContain(t, body, "<b>fett", "<script", "# Einkaufsliste")
		// two paragraphs, the line break inside the first kept
		if strings.Count(body, "<p class=notiz>") != 2 {
			t.Fatalf("%s: want two paragraphs:\n%s", path, body)
		}
	}
	_, body, _ := familyPage(t, Notizen, StartConfig{Notes: nd}, "GET", "/notizen/steuer.md", "")
	mustContain(t, body, ">Steuer</h1>", "abc\tdef")
	mustNotContain(t, body, "\x1b", "\x00", "\x07")
	_, body, _ = familyPage(t, Notizen, StartConfig{Notes: nd}, "GET", "/notizen/rezepte/apfelkuchen.md", "")
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

// A notes folder that cannot be read is said in Start's plain line, where
// the notes would be, for the newest and for a search alike; the search
// field stays, the folder's path and the error never show (haus#33).
func TestNotizenSaysPlainlyWhatDoesNotAnswer(t *testing.T) {
	gone := t.TempDir()
	nd, err := notes.Open(gone)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/notizen", "/notizen?q=apfel"} {
		code, body, _ := familyPage(t, Notizen, StartConfig{Notes: nd}, "GET", path, "")
		if code != 200 {
			t.Fatalf("%s → %d", path, code)
		}
		mustContain(t, body, `<form method=get action=/notizen`)
		if n := strings.Count(body, "<p class=k>Die Notizen lassen sich gerade nicht lesen.</p>"); n != 1 {
			t.Fatalf("%s: the notes said %d times, want once:\n%s", path, n, body)
		}
		mustNotContain(t, body, "karte-", "Noch keine Notizen", "Nichts gefunden", "Treffer")
		mustNotLeak(t, body, gone)
		before(t, body, "</form>", "Die Notizen lassen sich gerade nicht lesen.")
	}
}

// The memory library's index of the shelf (haus#13): qmd finds a note by
// what it means, and names what it should not — a hidden version, a note
// next to the folder.
func library13(t *testing.T, dir string) *qmdfake.Server {
	t.Helper()
	f := qmdfake.New("notizen")
	t.Cleanup(f.Close)
	read := func(name string) string { b, _ := os.ReadFile(filepath.Join(dir, name)); return string(b) }
	f.Add(
		qmdfake.Doc{Path: "einkaufsliste.md", Title: "Einkaufsliste", Body: read("einkaufsliste.md"), Means: []string{"lebensmittel"}},
		qmdfake.Doc{Path: ".stversions/geheim.md", Title: "Geheim", Body: read(".stversions/geheim.md"), Means: []string{"lebensmittel"}},
		qmdfake.Doc{Path: "../draussen.md", Title: "Draußen", Body: "# Draußen\n\nApfel von draußen\n", Means: []string{"lebensmittel"}},
		qmdfake.Doc{Path: "schraeg.md", Title: "Schräg", Body: read("schraeg.md"), Means: []string{"lebensmittel"}},
		qmdfake.Doc{Path: "rezepte/apfelkuchen.md", Title: "Apfelkuchen", Body: read("rezepte/apfelkuchen.md"), Means: []string{"lebensmittel"}},
	)
	return f
}

// haus#13: the search field searches by meaning when the library answers:
// a note found by a word it does not contain, with its title and qmd's
// passage, leading to its page; nothing hidden or outside, nothing as
// markup, and no line that the search is reduced.
func TestNotizenSearchByMeaning(t *testing.T) {
	dir, nd := shelf(t)
	f := library13(t, dir)
	cfg := StartConfig{Notes: nd, Library: &qmd.Client{URL: f.URL, Collection: "notizen"}}
	code, body, _ := familyPage(t, Notizen, cfg, "GET", "/notizen?q=Lebensmittel", "")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	mustContain(t, body, "Treffer für „Lebensmittel“", `<a href="/notizen/einkaufsliste.md">Einkaufsliste</a>`, "Milch",
		`<a href="/notizen/rezepte/apfelkuchen.md">Apfelkuchen</a>`, "&lt;i&gt;Schräger Titel&lt;/i&gt;")
	mustNotContain(t, body, "Geheim", "Draußen", "draußen", "<i>", "<script", saysWordsOnly, "Nichts gefunden")
	before(t, body, "Einkaufsliste", "Apfelkuchen")
	mustNotLeak(t, body, f.URL, "qmd")
}

// When the library does not answer, is not set, or is still indexing, the
// field still searches — by words — and says so in one plain line above
// the hits, once; the page is a 200 and names nothing technical
// (haus#13, like #34's lines).
func TestNotizenSearchReducedSaysSo(t *testing.T) {
	old := library.Wait
	library.Wait = 150 * time.Millisecond
	defer func() { library.Wait = old }()
	for _, c := range []struct {
		name string
		set  func(*qmdfake.Server) *qmd.Client
	}{
		{"not set", func(*qmdfake.Server) *qmd.Client { return nil }},
		{"down", func(f *qmdfake.Server) *qmd.Client { f.Close(); return &qmd.Client{URL: f.URL, Collection: "notizen"} }},
		{"500", func(f *qmdfake.Server) *qmd.Client {
			f.Status = 500
			return &qmd.Client{URL: f.URL, Collection: "notizen"}
		}},
		{"indexing", func(f *qmdfake.Server) *qmd.Client {
			f.Indexing = true
			return &qmd.Client{URL: f.URL, Collection: "notizen"}
		}},
		{"slow", func(f *qmdfake.Server) *qmd.Client {
			f.Hang = 3 * time.Second
			return &qmd.Client{URL: f.URL, Collection: "notizen"}
		}},
	} {
		dir, nd := shelf(t)
		f := library13(t, dir)
		cfg := StartConfig{Notes: nd, Library: c.set(f)}
		code, body, _ := familyPage(t, Notizen, cfg, "GET", "/notizen?q=APFEL", "")
		if code != 200 {
			t.Fatalf("%s: status %d", c.name, code)
		}
		if n := strings.Count(body, "<p class=k>"+saysWordsOnly+"</p>"); n != 1 {
			t.Fatalf("%s: the reduced search said %d times, want once:\n%s", c.name, n, body)
		}
		mustContain(t, body, `value="APFEL"`, "Treffer für „APFEL“", `<a href="/notizen/rezepte/apfelkuchen.md">Apfelkuchen</a>`)
		mustNotContain(t, body, "Geheim", "Draußen", saysNoNotes)
		mustNotLeak(t, body, f.URL, "qmd", "Vektor", "embed", "Index")
		before(t, body, "</form>", saysWordsOnly)
		before(t, body, saysWordsOnly, "Treffer für")
		// nothing found by words is said too, under the line
		_, body, _ = familyPage(t, Notizen, cfg, "GET", "/notizen?q=Lebensmittel", "")
		mustContain(t, body, saysWordsOnly, "Nichts gefunden")
		mustNotContain(t, body, "Einkaufsliste")
		// the newest notes are no search: no line
		_, body, _ = familyPage(t, Notizen, cfg, "GET", "/notizen", "")
		mustNotContain(t, body, saysWordsOnly)
	}
}

// A folder that cannot be read is the notes' one line, whether the
// library answers or not; never the reduced line on top of it, never an
// empty "Nichts gefunden".
func TestNotizenSearchFolderGone(t *testing.T) {
	for _, down := range []bool{false, true} {
		dir, nd := shelf(t)
		f := library13(t, dir)
		if down {
			f.Close()
		}
		os.RemoveAll(dir)
		code, body, _ := familyPage(t, Notizen, StartConfig{Notes: nd, Library: &qmd.Client{URL: f.URL, Collection: "notizen"}}, "GET", "/notizen?q=Lebensmittel", "")
		if code != 200 {
			t.Fatalf("down %v: status %d", down, code)
		}
		if n := strings.Count(body, "<p class=k>"+saysNoNotes+"</p>"); n != 1 {
			t.Fatalf("down %v: said %d times:\n%s", down, n, body)
		}
		mustNotContain(t, body, saysWordsOnly, "Nichts gefunden", "Treffer")
		mustNotLeak(t, body, f.URL, dir, "qmd")
	}
}

// A hit's line is plain text: a table row as its cells, no pipes, no
// table header, no list dash (haus#13).
func TestNotizenLinesArePlain(t *testing.T) {
	dir := t.TempDir()
	table := "# Handwerker\n\n| Gewerk | Firma | Telefon | Notiz |\n|---|---|---|---|\n| Heizung | Huber GmbH | 089 111 | Wartung |\n"
	list := "# Putzplan\n\n- Bad: Anna, samstags\n- Küche: Ben\n"
	os.WriteFile(filepath.Join(dir, "handwerker.md"), []byte(table), 0o644)
	os.WriteFile(filepath.Join(dir, "putzplan.md"), []byte(list), 0o644)
	nd, _ := notes.Open(dir)
	f := qmdfake.New("notizen")
	defer f.Close()
	f.Add(qmdfake.Doc{Path: "handwerker.md", Title: "Handwerker", Body: table, Means: []string{"haus"}},
		qmdfake.Doc{Path: "putzplan.md", Title: "Putzplan", Body: list, Means: []string{"haus"}})
	for _, cfg := range []StartConfig{{Notes: nd, Library: &qmd.Client{URL: f.URL, Collection: "notizen"}}, {Notes: nd}} {
		q := "/notizen?q=Haus"
		if cfg.Library == nil {
			q = "/notizen?q=Huber+Anna" // by words
		}
		_, body, _ := familyPage(t, Notizen, cfg, "GET", q, "")
		mustContain(t, body, "Gewerk Heizung · Firma Huber GmbH · Telefon 089 111 · Notiz Wartung", "Bad: Anna, samstags")
		mustNotContain(t, body, "|", "Gewerk · Firma", "Gewerk Firma", "- Bad", "---")
	}
}

// When only words are searched, a sentence needs none of its common
// words: any meaningful word of it finds a note. When none does, the page
// says what to try, without suggesting a word of the query: each of them
// has just failed (haus#13).
func TestNotizenWordsFromASentence(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "kaminkehrer.md"), []byte("# Kaminkehrer\n\nHerr Brandl kommt im Oktober.\n"), 0o644)
	nd, _ := notes.Open(dir)
	_, body, _ := familyPage(t, Notizen, StartConfig{Notes: nd}, "GET", "/notizen?q="+url.QueryEscape("Wann kommt der Kaminkehrer?"), "")
	mustContain(t, body, saysWordsOnly, `<a href="/notizen/kaminkehrer.md">Kaminkehrer</a>`)
	mustNotContain(t, body, "Nichts gefunden")
	// no example word: every word of the query has just failed
	for _, q := range []string{"Wann kommt der Schornsteinfeger?", "wo liegt der Reisepass", "wo ist das"} {
		_, body, _ := familyPage(t, Notizen, StartConfig{Notes: nd}, "GET", "/notizen?q="+url.QueryEscape(q), "")
		mustContain(t, body, "Nichts gefunden.", "Versuch es mit einem anderen Wort, das in der Notiz stehen könnte.")
		mustNotContain(t, body, "z. B.", "„Schornsteinfeger“.", "„Reisepass“", "einzelnen Wort")
	}
}

// haus#13: a hit by meaning is shown only when it is close to the
// question. When nothing is, the field falls to the word search with its
// one line, as when the library cannot answer: nonsense shows no notes by
// meaning.
func TestNotizenNothingCloseIsWords(t *testing.T) {
	dir, nd := shelf(t)
	f := qmdfake.New("notizen")
	defer f.Close()
	read := func(name string) string { b, _ := os.ReadFile(filepath.Join(dir, name)); return string(b) }
	f.Add(qmdfake.Doc{Path: "einkaufsliste.md", Title: "Einkaufsliste", Body: read("einkaufsliste.md"), Means: []string{"lebensmittel", "apfel"}, Vec: 0.29},
		qmdfake.Doc{Path: "packliste.md", Title: "Packliste", Body: read("packliste.md"), Means: []string{"lebensmittel", "apfel"}, Vec: 0.2})
	cfg := StartConfig{Notes: nd, Library: &qmd.Client{URL: f.URL, Collection: "notizen"}}
	_, body, _ := familyPage(t, Notizen, cfg, "GET", "/notizen?q=Lebensmittel", "")
	if n := strings.Count(body, "<p class=k>"+saysWordsOnly+"</p>"); n != 1 {
		t.Fatalf("the word search's line %d times:\n%s", n, body)
	}
	mustContain(t, body, "Nichts gefunden.", saysTryAnother)
	mustNotContain(t, body, "Einkaufsliste", "Packliste")
	// the words still find what they find
	_, body, _ = familyPage(t, Notizen, cfg, "GET", "/notizen?q=Apfel", "")
	mustContain(t, body, saysWordsOnly, `<a href="/notizen/rezepte/apfelkuchen.md">Apfelkuchen</a>`)
	mustNotContain(t, body, "Einkaufsliste", "Packliste", "Geheim")
}

// The house model tidies the page's hits (haus#13) when it is loaded: the
// page shows only those it keeps. It never waits for it longer than its
// budget, and nothing about it is on the page.
func TestNotizenTidyUp(t *testing.T) {
	dir, nd := shelf(t)
	f := library13(t, dir)
	p := proxyfake.New("gemma-4-e4b")
	defer p.Close()
	cfg := StartConfig{Notes: nd, Library: &qmd.Client{URL: f.URL, Collection: "notizen"},
		Judge: &judge.Client{URL: p.Base(), Model: "gemma-4-e4b", Key: "model-key"}}
	p.Content = `{"ids":[1]}`
	_, body, _ := familyPage(t, Notizen, cfg, "GET", "/notizen?q=Lebensmittel", "")
	mustContain(t, body, `<a href="/notizen/einkaufsliste.md">Einkaufsliste</a>`)
	mustNotContain(t, body, "Apfelkuchen", "Schräger Titel", saysWordsOnly, "Nichts gefunden")
	mustNotLeak(t, body, p.URL, "gemma", "Modell", "KI", "aufgeräumt", "sortiert")
	if n := len(p.Chats()); n != 1 {
		t.Fatalf("%d chats", n)
	}
	// a house model too slow for the page: the cut's hits, within the budget
	p.Hang = 5 * time.Second
	start := time.Now()
	code, body, _ := familyPage(t, Notizen, cfg, "GET", "/notizen?q=Lebensmittel", "")
	if took := time.Since(start); code != 200 || took > library.PageTidy+800*time.Millisecond {
		t.Fatalf("status %d after %v; the page waits at most %v for the tidy-up", code, took, library.PageTidy)
	}
	mustContain(t, body, `<a href="/notizen/einkaufsliste.md">Einkaufsliste</a>`, `<a href="/notizen/rezepte/apfelkuchen.md">Apfelkuchen</a>`)
	mustNotContain(t, body, saysWordsOnly, "Nichts gefunden")
	mustNotLeak(t, body, p.URL, "gemma", "Modell", "KI")
	for _, r := range p.Seen() {
		if r.Path == "/api/model-lease" && r.Method != "GET" {
			t.Fatalf("the page took the lease: %s", r.Method)
		}
	}
}
