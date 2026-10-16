package library

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
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
		qmdfake.Doc{Path: "kaminkehrer.md", Title: "qmds Titel", Body: files["kaminkehrer.md"], Means: []string{"schornsteinfeger"}, Vec: 0.54},
		qmdfake.Doc{Path: "heizung.md", Title: "Gastherme", Body: files["heizung.md"], Means: []string{"schornsteinfeger", "heizung"}, Vec: 0.45},
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
	got, err := Find(bg, c, Tidy{}, d, nil, "Schornsteinfeger", 10)
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
	if got, _ := Find(bg, c, Tidy{}, d, nil, "Teig", 10); names(got.Hits) != "rezepte/pfannkuchen.md" || got.Hits[0].Title != "Pfannkuchen" {
		t.Fatalf("folder: %+v", got)
	}
	if got, _ := Find(bg, c, Tidy{}, d, nil, "Kleidung", 10); names(got.Hits) != "Größen Kinder.md" || got.Hits[0].Title != "Größenliste Kinder" {
		t.Fatalf("umlaut: %+v", got)
	}
	// at most max, asked of qmd as its limit
	if got, _ := Find(bg, c, Tidy{}, d, nil, "Schornsteinfeger", 1); len(got.Hits) != 1 {
		t.Fatalf("max: %+v", got)
	}
	last := f.Seen()[len(f.Seen())-1]
	if !strings.Contains(string(last.Body), `"limit":1`) {
		t.Fatalf("limit not asked of qmd: %s", last.Body)
	}
	// a qmd that answers more than it was asked for is cut at max
	f.Results = `{"results":[` + strings.Repeat(`{"docid":"#a","file":"qmd://notizen/kaminkehrer.md","title":"K","score":1,"context":null,"line":1,"snippet":"1: x","vec":0.5},`, 4) +
		`{"docid":"#b","file":"qmd://notizen/heizung.md","title":"H","score":1,"context":null,"line":1,"snippet":"1: y","vec":0.5}]}`
	if got, _ := Find(bg, c, Tidy{}, d, nil, "Schornsteinfeger", 2); len(got.Hits) != 2 || got.Total != 2 {
		t.Fatalf("cut at max: %+v", got)
	}
	f.Results = ""
}

// What qmd names is checked against the folder: a path out of it, a link
// out of it, a hidden file, a note deleted since the last refresh — none
// reaches an answer, and nothing of their text either. Nor do they set
// the bar of the cut: they are closer to the question than the note, and
// the note is still shown.
func TestOnlyNotesInsideTheFolder(t *testing.T) {
	_, d, f := shelf(t)
	snip := func(s string) string { return `"1: @@ -1,2 @@ (0 before, 0 after)\n2: ` + s + `"` }
	f.Results = `{"results":[` +
		`{"docid":"#a","vec":0.9,"file":"qmd://notizen/../draussen.md","title":"Draußen","score":1,"context":null,"line":1,"snippet":` + snip("GEHEIM-DRAUSSEN") + `},` +
		`{"docid":"#b","vec":0.9,"file":"qmd://notizen/link.md","title":"Link","score":0.5,"context":null,"line":1,"snippet":` + snip("GEHEIM-LINK") + `},` +
		`{"docid":"#c","vec":0.9,"file":"qmd://notizen/.stversions/alt.md","title":"Alt","score":0.33,"context":null,"line":1,"snippet":` + snip("GEHEIM-VERSION") + `},` +
		`{"docid":"#d","vec":0.9,"file":"qmd://notizen/geloescht.md","title":"Gelöscht","score":0.25,"context":null,"line":1,"snippet":` + snip("GEHEIM-GELOESCHT") + `},` +
		`{"docid":"#e","vec":0.9,"file":"qmd://andere/kaminkehrer.md","title":"Andere","score":0.2,"context":null,"line":1,"snippet":` + snip("GEHEIM-ANDERE") + `},` +
		`{"docid":"#f","vec":0.5,"file":"qmd://notizen/kaminkehrer.md","title":"Kaminkehrer","score":0.17,"context":null,"line":1,"snippet":` + snip("Herr Brandl") + `}]}`
	got, err := Find(bg, &qmd.Client{URL: f.URL, Collection: "notizen"}, Tidy{}, d, nil, "Schornstein", 10)
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
		got, err := Find(bg, cl, Tidy{}, d, nil, "Brandl", 10)
		if err != nil || !got.Reduced || got.NoMatch {
			t.Fatalf("%s: %+v %v", c.name, got, err)
		}
		if names(got.Hits) != "kaminkehrer.md" || got.Hits[0].Title != "Kaminkehrer" {
			t.Fatalf("%s: the word search's hits: %+v", c.name, got)
		}
		if d := time.Since(start); d > time.Second {
			t.Fatalf("%s: took %v; qmd gets Wait, not the page's whole budget", c.name, d)
		}
		// meaning is not there: a word that is not written finds nothing, said plainly
		if got, err := Find(bg, cl, Tidy{}, d, nil, "Schornsteinfeger", 10); err != nil || !got.Reduced || got.Total != 0 {
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
	got, err := Find(ctx, &qmd.Client{URL: f.URL, Collection: "notizen"}, Tidy{}, d, nil, "Brandl", 10)
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
		if got, err := Find(bg, &qmd.Client{URL: f.URL, Collection: "notizen"}, Tidy{}, d, nil, "Schornsteinfeger", 10); err == nil {
			t.Fatalf("qmd down %v: %+v, want an error", down, got)
		}
	}
	_, d, f := shelf(t)
	if _, err := Find(bg, &qmd.Client{URL: f.URL, Collection: "notizen"}, Tidy{}, d, nil, "  ", 10); err == nil {
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
	got, err := Find(bg, &qmd.Client{URL: f.URL, Collection: "notizen"}, Tidy{}, d, nil, "Monteur", 10)
	if err != nil || got.Reduced || len(got.Hits) != 2 {
		t.Fatalf("%+v %v", got, err)
	}
	if l := strings.Join(got.Hits[0].Lines, "|"); l != "Gewerk Heizung · Firma Huber GmbH · Telefon 089 111 · Notiz Wartung" {
		t.Fatalf("table: %q", l)
	}
	// the fake's passage is heading, blank, first item: one line
	if l := strings.Join(got.Hits[1].Lines, "|"); l != "Skischuhe" {
		t.Fatalf("list: %q", l)
	}
}

// verbatim serves body for every request.
func verbatim(t *testing.T, body string) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}))
	t.Cleanup(s.Close)
	return s
}

// The cut on real scores (haus#13): what the Vermittler answered for
// these questions over the eval's 44 notes. A hit stays when its cosine
// is at least 0.30 and at least 0.75 of the best one's, in qmd's order.
func TestCutOnRealScores(t *testing.T) {
	for file, want := range map[string]string{
		// one clear note: the others are far below the best
		"query-meaning.json": "kaminkehrer.md",
		// a paraphrase: three contracts close together, the right one first
		"cut-auto-vertrag.json": "kfz-versicherung.md,tuev-hauptuntersuchung.md,internetvertrag.md",
		// five near the bar, in qmd's order (kita ranks first, by words)
		"cut-vorsorge.json": "kita.md,kinderaerztin.md,impfpass.md,tuev-hauptuntersuchung.md,hausarzt.md",
		// handwerker-kontakte at 0.357 is just under 0.75 × 0.477
		"cut-heizkessel.json": "kaminkehrer.md,heizung-wartung.md",
		// nonsense near the recipes: two just over the floor (the eval's one leak)
		"cut-sushi.json": "rezept-linsensuppe.md,rezept-bananenbrot.md",
		// nonsense: nothing reaches the floor
		"cut-everest.json": "",
	} {
		raw, err := os.ReadFile(filepath.Join("..", "qmd", "testdata", file))
		if err != nil {
			t.Fatal(err)
		}
		hits, err := (&qmd.Client{URL: verbatim(t, string(raw)).URL, Collection: "notizen"}).Search(bg, "x", 20)
		if err != nil || len(hits) == 0 {
			t.Fatalf("%s: %v %v", file, hits, err)
		}
		var got []string
		for _, h := range Cut(hits) {
			got = append(got, path.Base(h.Path))
		}
		if strings.Join(got, ",") != want {
			t.Errorf("%s: kept %s, want %s", file, strings.Join(got, ","), want)
		}
	}
	// the edges: exactly the floor and exactly the share stay; a hit
	// outside the list by meaning (0) never does
	h := func(p string, v float64) qmd.Hit { return qmd.Hit{Path: p, Vec: v} }
	cut := func(hs ...qmd.Hit) string {
		var out []string
		for _, x := range Cut(hs) {
			out = append(out, x.Path)
		}
		return strings.Join(out, ",")
	}
	if got := cut(h("a", 0.40), h("b", 0.30), h("c", 0.2999)); got != "a,b" {
		t.Errorf("floor: %s", got)
	}
	if got := cut(h("a", 0.80), h("b", 0.60), h("c", 0.5999)); got != "a,b" {
		t.Errorf("share: %s", got)
	}
	if got := cut(h("a", 0), h("b", 0.31), h("c", 0)); got != "b" {
		t.Errorf("outside the list: %s", got)
	}
	if got := cut(); got != "" {
		t.Errorf("none: %s", got)
	}
}

// When nothing qmd found is close enough, the meaning hits are not shown,
// and the search says so apart from a library that cannot answer: NoMatch,
// not Reduced. The hits, if any, are the word search's.
func TestNothingCloseIsTheWordSearch(t *testing.T) {
	dir, d, f := shelf(t)
	os.WriteFile(filepath.Join(dir, "everest.md"), []byte("# Berge\n\nDer Mount Everest ist hoch.\n"), 0o644)
	f.Add(qmdfake.Doc{Path: "everest.md", Title: "Berge", Body: "# Berge\n\nDer Mount Everest ist hoch.\n", Vec: 0.29})
	c := &qmd.Client{URL: f.URL, Collection: "notizen"}
	got, err := Find(bg, c, Tidy{}, d, nil, "Everest", 10)
	if err != nil || got.Reduced || !got.NoMatch || names(got.Hits) != "everest.md" {
		t.Fatalf("%+v %v", got, err)
	}
	// the real nonsense answer (its best, "Größen Kinder.md", is in the
	// folder at 0.175): words
	f.Results = string(mustRead(t, filepath.Join("..", "qmd", "testdata", "cut-everest.json")))
	got, err = Find(bg, c, Tidy{}, d, nil, "Wie hoch ist der Gipfel?", 10)
	if err != nil || got.Reduced || !got.NoMatch || names(got.Hits) != "everest.md" {
		t.Fatalf("real nonsense scores: %+v %v", got, err)
	}
	// and nothing by words either: said as nothing, not as an error
	if got, err := Find(bg, c, Tidy{}, d, nil, "Quantenchromodynamik", 10); err != nil || got.Reduced || !got.NoMatch || got.Total != 0 {
		t.Fatalf("nothing at all: %+v %v", got, err)
	}
	// a library that answers with close notes is neither
	f.Results = ""
	if got, err := Find(bg, c, Tidy{}, d, nil, "Schornsteinfeger", 10); err != nil || got.Reduced || got.NoMatch || got.Total != 2 {
		t.Fatalf("close: %+v %v", got, err)
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
