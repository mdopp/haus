package library

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/haus/internal/notes"
	"github.com/mdopp/haus/internal/qmd"
	"github.com/mdopp/haus/internal/qmd/qmdfake"
)

var bg = context.Background()

// a notes folder as the share has it, and qmd's index of it: a note in a
// folder, umlauts and spaces in a name, Syncthing's hidden versions, a
// note next to the folder and a link to it
func shelf(t *testing.T) (string, *notes.Dir, *qmdfake.Server) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "notes")
	files := map[string]string{
		"kaminkehrer.md":         "# Kaminkehrer\n\nHerr Brandl kommt einmal im Jahr im Oktober.\nRechnung für die Steuer aufheben.\n",
		"heizung.md":             "# Gastherme\n\nWartung im September, Firma Huber.\n",
		"rezepte/pfannkuchen.md": "# Pfannkuchen\n\n250 g Mehl, 500 ml Milch, 3 Eier.\n",
		"Größen Kinder.md":       "# Größenliste Kinder\n\nLena trägt Schuhgröße 31.\n",
		".stversions/alt.md":     "# Alt\n\nGEHEIM-VERSION Schornstein\n",
		"../draussen.md":         "# Draußen\n\nGEHEIM-DRAUSSEN Schornstein\n",
	}
	for name, body := range files {
		p := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	os.Symlink(filepath.Join(dir, "..", "draussen.md"), filepath.Join(dir, "link.md"))
	d, err := notes.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	f := qmdfake.New("notizen")
	t.Cleanup(f.Close)
	f.Add(
		qmdfake.Doc{Path: "kaminkehrer.md", Title: "qmds Titel", Body: files["kaminkehrer.md"], Means: []string{"schornsteinfeger"}},
		qmdfake.Doc{Path: "heizung.md", Title: "Gastherme", Body: files["heizung.md"], Means: []string{"schornsteinfeger", "heizung"}},
		qmdfake.Doc{Path: "rezepte/pfannkuchen.md", Title: "Pfannkuchen", Body: files["rezepte/pfannkuchen.md"], Means: []string{"teig"}},
		qmdfake.Doc{Path: "Größen Kinder.md", Title: "Größenliste Kinder", Body: files["Größen Kinder.md"], Means: []string{"kleidung"}},
	)
	return dir, d, f
}

func names(hs []notes.Hit) string {
	var out []string
	for _, h := range hs {
		out = append(out, h.Name)
	}
	return strings.Join(out, ",")
}

// Search by meaning: qmd finds the note by a word it does not contain,
// which the word search cannot; the title is Haus's own (the note's
// heading, as its page shows it), the lines qmd's best passage.
func TestFindsByMeaning(t *testing.T) {
	_, d, f := shelf(t)
	if r, _ := d.Search(bg, "Schornsteinfeger", 10); r.Total != 0 {
		t.Fatalf("the word search finds it too; the test proves nothing: %+v", r)
	}
	c := &qmd.Client{URL: f.URL, Collection: "notizen"}
	got, err := Find(bg, c, d, "Schornsteinfeger", 10)
	if err != nil || got.Reduced {
		t.Fatalf("%+v %v", got, err)
	}
	if names(got.Hits) != "kaminkehrer.md,heizung.md" || got.Total != 2 {
		t.Fatalf("hits: %s (%d)", names(got.Hits), got.Total)
	}
	k := got.Hits[0]
	if k.Title != "Kaminkehrer" || len(k.Lines) == 0 || k.Lines[0] != "Herr Brandl kommt einmal im Jahr im Oktober." {
		t.Fatalf("first hit: %+v", k)
	}
	// folders and umlauts reach their file
	if got, _ := Find(bg, c, d, "Teig", 10); names(got.Hits) != "rezepte/pfannkuchen.md" || got.Hits[0].Title != "Pfannkuchen" {
		t.Fatalf("folder: %+v", got)
	}
	if got, _ := Find(bg, c, d, "Kleidung", 10); names(got.Hits) != "Größen Kinder.md" || got.Hits[0].Title != "Größenliste Kinder" {
		t.Fatalf("umlaut: %+v", got)
	}
	// at most max, asked of qmd as its limit
	if got, _ := Find(bg, c, d, "Schornsteinfeger", 1); len(got.Hits) != 1 {
		t.Fatalf("max: %+v", got)
	}
	last := f.Seen()[len(f.Seen())-1]
	if !strings.Contains(string(last.Body), `"limit":1`) {
		t.Fatalf("limit not asked of qmd: %s", last.Body)
	}
	// a qmd that answers more than it was asked for is cut at max
	f.Results = `{"results":[` + strings.Repeat(`{"docid":"#a","file":"qmd://notizen/kaminkehrer.md","title":"K","score":1,"context":null,"line":1,"snippet":"1: x"},`, 4) +
		`{"docid":"#b","file":"qmd://notizen/heizung.md","title":"H","score":1,"context":null,"line":1,"snippet":"1: y"}]}`
	if got, _ := Find(bg, c, d, "Schornsteinfeger", 2); len(got.Hits) != 2 || got.Total != 2 {
		t.Fatalf("cut at max: %+v", got)
	}
	f.Results = ""
}

// What qmd names is checked against the folder: a path out of it, a link
// out of it, a hidden file, a note deleted since the last refresh — none
// reaches an answer, and nothing of their text either.
func TestOnlyNotesInsideTheFolder(t *testing.T) {
	_, d, f := shelf(t)
	snip := func(s string) string { return `"1: @@ -1,2 @@ (0 before, 0 after)\n2: ` + s + `"` }
	f.Results = `{"results":[` +
		`{"docid":"#a","file":"qmd://notizen/../draussen.md","title":"Draußen","score":1,"context":null,"line":1,"snippet":` + snip("GEHEIM-DRAUSSEN") + `},` +
		`{"docid":"#b","file":"qmd://notizen/link.md","title":"Link","score":0.5,"context":null,"line":1,"snippet":` + snip("GEHEIM-LINK") + `},` +
		`{"docid":"#c","file":"qmd://notizen/.stversions/alt.md","title":"Alt","score":0.33,"context":null,"line":1,"snippet":` + snip("GEHEIM-VERSION") + `},` +
		`{"docid":"#d","file":"qmd://notizen/geloescht.md","title":"Gelöscht","score":0.25,"context":null,"line":1,"snippet":` + snip("GEHEIM-GELOESCHT") + `},` +
		`{"docid":"#e","file":"qmd://andere/kaminkehrer.md","title":"Andere","score":0.2,"context":null,"line":1,"snippet":` + snip("GEHEIM-ANDERE") + `},` +
		`{"docid":"#f","file":"qmd://notizen/kaminkehrer.md","title":"Kaminkehrer","score":0.17,"context":null,"line":1,"snippet":` + snip("Herr Brandl") + `}]}`
	got, err := Find(bg, &qmd.Client{URL: f.URL, Collection: "notizen"}, d, "Schornstein", 10)
	if err != nil || got.Reduced {
		t.Fatalf("%+v %v", got, err)
	}
	if names(got.Hits) != "kaminkehrer.md" {
		t.Fatalf("hits: %s", names(got.Hits))
	}
	for _, h := range got.Hits {
		all := h.Name + h.Title + strings.Join(h.Lines, "\n")
		if strings.Contains(all, "GEHEIM") || strings.Contains(all, "Draußen") {
			t.Fatalf("leaked: %+v", h)
		}
	}
}

// When qmd is not set, does not answer, answers wrong, is too slow, or
// has no vectors yet (still indexing), the search is the word search, and
// says it is reduced: never empty while words match, never an error.
func TestReducedToWords(t *testing.T) {
	old := Wait
	Wait = 150 * time.Millisecond
	defer func() { Wait = old }()
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
		{"other collection", func(f *qmdfake.Server) *qmd.Client { return &qmd.Client{URL: f.URL, Collection: "andere"} }},
		{"slow", func(f *qmdfake.Server) *qmd.Client {
			f.Hang = 2 * time.Second
			return &qmd.Client{URL: f.URL, Collection: "notizen"}
		}},
	} {
		_, d, f := shelf(t)
		cl := c.set(f)
		start := time.Now()
		got, err := Find(bg, cl, d, "Brandl", 10)
		if err != nil || !got.Reduced {
			t.Fatalf("%s: %+v %v", c.name, got, err)
		}
		if names(got.Hits) != "kaminkehrer.md" || got.Hits[0].Title != "Kaminkehrer" {
			t.Fatalf("%s: the word search's hits: %+v", c.name, got)
		}
		if d := time.Since(start); d > time.Second {
			t.Fatalf("%s: took %v; qmd gets Wait, not the page's whole budget", c.name, d)
		}
		// meaning is not there: a word that is not written finds nothing, said plainly
		if got, err := Find(bg, cl, d, "Schornsteinfeger", 10); err != nil || !got.Reduced || got.Total != 0 {
			t.Fatalf("%s: %+v %v", c.name, got, err)
		}
	}
}

// A slow qmd leaves the word search its time even when the caller's
// budget is short.
func TestSlowQmdLeavesTimeForWords(t *testing.T) {
	_, d, f := shelf(t)
	f.Hang = 5 * time.Second
	ctx, cancel := context.WithTimeout(bg, 600*time.Millisecond)
	defer cancel()
	got, err := Find(ctx, &qmd.Client{URL: f.URL, Collection: "notizen"}, d, "Brandl", 10)
	if err != nil || !got.Reduced || names(got.Hits) != "kaminkehrer.md" {
		t.Fatalf("%+v %v", got, err)
	}
}

// A folder that cannot be read is an error, whichever search ran: never
// an empty answer that looks like "nothing found".
func TestUnreadableFolderIsAnError(t *testing.T) {
	for _, down := range []bool{false, true} {
		dir, d, f := shelf(t)
		os.RemoveAll(dir)
		if down {
			f.Close()
		}
		if got, err := Find(bg, &qmd.Client{URL: f.URL, Collection: "notizen"}, d, "Schornsteinfeger", 10); err == nil {
			t.Fatalf("qmd down %v: %+v, want an error", down, got)
		}
	}
	_, d, f := shelf(t)
	if _, err := Find(bg, &qmd.Client{URL: f.URL, Collection: "notizen"}, d, "  ", 10); err == nil {
		t.Fatal("a blank search is an error, as the word search's")
	}
}

// qmd's passage is shown plain (haus#13): a table row as its cells, no
// list dash; a passage that is only a table's header is the note's first
// real lines, read from the note itself.
func TestPassagesArePlain(t *testing.T) {
	dir, d, f := shelf(t)
	table := "# Handwerker\n\n| Gewerk | Firma | Telefon | Notiz |\n|---|---|---|---|\n| Heizung | Huber GmbH | 089 111 | Wartung |\n"
	list := "# Packliste\n\n- Skischuhe\n- Helm\n"
	os.WriteFile(filepath.Join(dir, "handwerker.md"), []byte(table), 0o644)
	os.WriteFile(filepath.Join(dir, "packliste.md"), []byte(list), 0o644)
	// the fake's passage is the note's first three lines: heading, blank, the table's header row
	f.Add(qmdfake.Doc{Path: "handwerker.md", Title: "Handwerker", Body: table, Means: []string{"monteur"}},
		qmdfake.Doc{Path: "packliste.md", Title: "Packliste", Body: list, Means: []string{"monteur"}})
	got, err := Find(bg, &qmd.Client{URL: f.URL, Collection: "notizen"}, d, "Monteur", 10)
	if err != nil || got.Reduced || len(got.Hits) != 2 {
		t.Fatalf("%+v %v", got, err)
	}
	if l := strings.Join(got.Hits[0].Lines, "|"); l != "Heizung · Huber GmbH · 089 111 · Wartung" {
		t.Fatalf("table: %q", l)
	}
	// the fake's passage is heading, blank, first item: one line
	if l := strings.Join(got.Hits[1].Lines, "|"); l != "Skischuhe" {
		t.Fatalf("list: %q", l)
	}
}
