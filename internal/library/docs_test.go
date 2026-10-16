package library

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/haus/internal/paperless"
	"github.com/mdopp/haus/internal/paperless/paperlessfake"
	"github.com/mdopp/haus/internal/qmd"
	"github.com/mdopp/haus/internal/qmd/qmdfake"
)

// the household's documents: in Paperless, and qmd's copy of them, the
// collection "dokumente" of <id>.md as `haus export-documents` writes it
func archive(t *testing.T, f *qmdfake.Server) *paperlessfake.Server {
	t.Helper()
	p := paperlessfake.New("pl-token")
	t.Cleanup(p.Close)
	p.Correspondent(3, "Stadtwerke Musterstadt")
	p.Add(
		paperlessfake.Doc{ID: 7, Title: "Stromvertrag", Content: "Vertragslaufzeit bis 31.12.2027.", Correspondent: 3, Created: "2025-03-14"},
		paperlessfake.Doc{ID: 9, Title: "Kfz-Versicherung", Content: "Die Versicherung läuft bis 30.11.2026.", Created: "2024-11-30"},
	)
	f.Add(
		qmdfake.Doc{Collection: "dokumente", Path: "7.md", Title: "Stromvertrag (alter Titel)", Body: "# Stromvertrag\n\nDatum: 14.03.2025\nVon: Stadtwerke Musterstadt\n\nVertragslaufzeit bis 31.12.2027.",
			Means: []string{"strom", "laufzeit"}, Vec: 0.52},
		qmdfake.Doc{Collection: "dokumente", Path: "9.md", Title: "Kfz-Versicherung", Body: "# Kfz-Versicherung\n\nDie Versicherung läuft bis 30.11.2026.",
			Means: []string{"auto", "laufzeit"}, Vec: 0.48},
	)
	return p
}

func docIDs(ds []Doc) []int {
	var out []int
	for _, d := range ds {
		out = append(out, d.ID)
	}
	return out
}

func same(a []int, b ...int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// haus#13: with Paperless and the documents' collection set, one search
// covers notes and documents. A document is found by meaning, shown as
// Paperless has it now (title, date, sender, its page), with the passage
// that matched — the lines the export put above its text left out — and
// the notes are found as before.
func TestFindsDocumentsByMeaning(t *testing.T) {
	_, d, f := shelf(t)
	p := archive(t, f)
	c := &qmd.Client{URL: f.URL, Collection: "notizen", Documents: "dokumente"}
	pl := &paperless.Client{URL: p.URL, Token: "pl-token", PublicURL: "https://paperless.example"}
	got, err := Find(bg, c, Tidy{}, d, pl, "Laufzeit", 10)
	if err != nil || got.Reduced || got.NoMatch || got.DocsDown {
		t.Fatalf("%+v %v", got, err)
	}
	if !same(docIDs(got.Docs), 7, 9) || len(got.Hits) != 0 {
		t.Fatalf("docs %v, notes %s", docIDs(got.Docs), names(got.Hits))
	}
	s := got.Docs[0]
	if s.Title != "Stromvertrag" || s.Correspondent != "Stadtwerke Musterstadt" || s.Created.Format("2006-01-02") != "2025-03-14" ||
		s.Link != "https://paperless.example/documents/7/details" {
		t.Fatalf("Paperless's own data: %+v", s)
	}
	// the fake's passage is the file's first lines: the export's own,
	// which the page says from Paperless — none of them shows, the start
	// of the document's text stands in
	if strings.Join(s.Lines, "|") != "Vertragslaufzeit bis 31.12.2027." {
		t.Fatalf("passage: %q", s.Lines)
	}
	// the search asked qmd for both collections, and Paperless only for the ids it found
	var body string
	for _, r := range f.Seen() {
		if r.Path == "/query" {
			body = string(r.Body)
		}
	}
	if !strings.Contains(body, `"collections":["notizen","dokumente"]`) {
		t.Fatalf("query: %s", body)
	}
	seen := p.Seen()
	if len(seen) == 0 || seen[0].URL.Query().Get("id__in") != "7,9" {
		t.Fatalf("Paperless asked: %v", seen)
	}
	// notes and documents in one search, max in all
	got, err = Find(bg, c, Tidy{}, d, pl, "Schornsteinfeger Strom", 10)
	if err != nil || names(got.Hits) != "kaminkehrer.md,heizung.md" || !same(docIDs(got.Docs), 7) {
		t.Fatalf("both: %s %v %v", names(got.Hits), docIDs(got.Docs), err)
	}
	if got, _ := Find(bg, c, Tidy{}, d, pl, "Schornsteinfeger Strom", 2); len(got.Hits)+len(got.Docs) != 2 {
		t.Fatalf("max in all: %s %v", names(got.Hits), docIDs(got.Docs))
	}
}

// A document deleted in Paperless since qmd's last refresh, or one Haus's
// user may not see, is not shown — and does not set the cut's bar: it is
// closer to the question than the note, and the note is still shown.
func TestDocumentsGoneFromPaperlessAreLeftOut(t *testing.T) {
	_, d, f := shelf(t)
	p := archive(t, f)
	f.Add(qmdfake.Doc{Collection: "dokumente", Path: "99.md", Title: "Gelöscht", Body: "# Gelöscht\n\nGEHEIM", Means: []string{"schornsteinfeger"}, Vec: 0.95})
	c := &qmd.Client{URL: f.URL, Collection: "notizen", Documents: "dokumente"}
	got, err := Find(bg, c, Tidy{}, d, &paperless.Client{URL: p.URL, Token: "pl-token"}, "Schornsteinfeger", 10)
	if err != nil || got.NoMatch || len(got.Docs) != 0 || names(got.Hits) != "kaminkehrer.md,heizung.md" {
		t.Fatalf("%+v %v", got, err)
	}
	// what qmd names in the documents' collection that is no <id>.md is none
	f.Results = `{"results":[` +
		`{"docid":"#a","vec":0.6,"file":"qmd://dokumente/..%2F7.md","title":"x","score":1,"context":null,"line":1,"snippet":"1: GEHEIM"},` +
		`{"docid":"#b","vec":0.6,"file":"qmd://dokumente/sub/7.md","title":"x","score":1,"context":null,"line":1,"snippet":"1: GEHEIM"},` +
		`{"docid":"#c","vec":0.6,"file":"qmd://dokumente/007.md","title":"x","score":1,"context":null,"line":1,"snippet":"1: GEHEIM"},` +
		`{"docid":"#d","vec":0.6,"file":"qmd://dokumente/9.md","title":"x","score":1,"context":null,"line":1,"snippet":"@@ -1,3 @@\n2: # Kfz-Versicherung\n3: Datum: 30.11.2024\n4: Die Versicherung läuft bis 30.11.2026."}]}`
	got, err = Find(bg, c, Tidy{}, d, &paperless.Client{URL: p.URL, Token: "pl-token"}, "Auto", 10)
	if err != nil || !same(docIDs(got.Docs), 9) || strings.Join(got.Docs[0].Lines, "|") != "Die Versicherung läuft bis 30.11.2026." {
		t.Fatalf("%+v %v", got, err)
	}
}

// Paperless not answering: the documents are left out and that is said
// (DocsDown); the notes' answer stands as it would without documents.
func TestPaperlessDownKeepsTheNotes(t *testing.T) {
	for _, c := range []struct {
		name  string
		spoil func(*paperlessfake.Server)
	}{
		{"down", func(p *paperlessfake.Server) { p.Close() }},
		{"500", func(p *paperlessfake.Server) { p.Status = http.StatusInternalServerError }},
	} {
		_, d, f := shelf(t)
		p := archive(t, f)
		c.spoil(p)
		cl := &qmd.Client{URL: f.URL, Collection: "notizen", Documents: "dokumente"}
		pl := &paperless.Client{URL: p.URL, Token: "pl-token"}
		got, err := Find(bg, cl, Tidy{}, d, pl, "Schornsteinfeger Strom", 10)
		if err != nil || !got.DocsDown || got.Reduced || got.NoMatch || len(got.Docs) != 0 || names(got.Hits) != "kaminkehrer.md,heizung.md" {
			t.Fatalf("%s: %+v %v", c.name, got, err)
		}
		// a question only documents answer: the words, and still said
		got, err = Find(bg, cl, Tidy{}, d, pl, "Laufzeit", 10)
		if err != nil || !got.DocsDown || len(got.Docs) != 0 {
			t.Fatalf("%s, documents only: %+v %v", c.name, got, err)
		}
	}
}

// A Paperless that hangs costs the search at most its share of the
// caller's time: the notes come in time.
func TestPaperlessSlowKeepsTheBudget(t *testing.T) {
	old := DocsWait
	DocsWait = 100 * time.Millisecond
	defer func() { DocsWait = old }()
	_, d, f := shelf(t)
	p := archive(t, f)
	hang := make(chan struct{})
	defer close(hang)
	slow := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		select {
		case <-hang:
		case <-r.Context().Done():
		}
		return nil, r.Context().Err()
	})}
	cl := &qmd.Client{URL: f.URL, Collection: "notizen", Documents: "dokumente"}
	start := time.Now()
	got, err := Find(bg, cl, Tidy{}, d, &paperless.Client{URL: p.URL, Token: "pl-token", HTTP: slow}, "Schornsteinfeger Strom", 10)
	if err != nil || !got.DocsDown || names(got.Hits) != "kaminkehrer.md,heizung.md" {
		t.Fatalf("%+v %v", got, err)
	}
	if took := time.Since(start); took > time.Second {
		t.Fatalf("took %v", took)
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// When the search by meaning cannot answer, the documents come from
// Paperless's own full text search, any meaningful word of the question;
// when it answers and nothing is close, the same, as words only.
func TestDocumentsByWordsWhenReduced(t *testing.T) {
	old := Wait
	Wait = 150 * time.Millisecond
	defer func() { Wait = old }()
	_, d, f := shelf(t)
	p := archive(t, f)
	pl := &paperless.Client{URL: p.URL, Token: "pl-token"}
	f.Status = http.StatusInternalServerError
	cl := &qmd.Client{URL: f.URL, Collection: "notizen", Documents: "dokumente"}
	got, err := Find(bg, cl, Tidy{}, d, pl, "Wann läuft die Versicherung ab?", 10)
	if err != nil || !got.Reduced || got.DocsDown || !same(docIDs(got.Docs), 9) || got.Docs[0].Title != "Kfz-Versicherung" {
		t.Fatalf("%+v %v", got, err)
	}
	last := p.Seen()[0].URL.Query().Get("query")
	if last != "läuft OR versicherung" {
		t.Fatalf("asked Paperless %q", last)
	}
	// nothing close: the words of both, said as such
	f.Status = 0
	f.Results = `{"results":[{"docid":"#a","vec":0.1,"file":"qmd://dokumente/7.md","title":"x","score":1,"context":null,"line":1,"snippet":"1: x"}]}`
	got, err = Find(bg, cl, Tidy{}, d, pl, "Stromvertrag Brandl", 10)
	if err != nil || got.Reduced || !got.NoMatch || !same(docIDs(got.Docs), 7) || names(got.Hits) != "kaminkehrer.md" {
		t.Fatalf("nothing close: %+v %v", got, err)
	}
}

// Without the documents' collection, or without Paperless, the library is
// the notes as before: qmd is asked for the notes only, Paperless never.
func TestNoDocumentsWithoutTheirCollection(t *testing.T) {
	_, d, f := shelf(t)
	p := archive(t, f)
	for _, c := range []struct {
		cl *qmd.Client
		pl *paperless.Client
	}{
		{&qmd.Client{URL: f.URL, Collection: "notizen"}, &paperless.Client{URL: p.URL, Token: "pl-token"}},
		{&qmd.Client{URL: f.URL, Collection: "notizen", Documents: "dokumente"}, nil},
	} {
		got, err := Find(bg, c.cl, Tidy{}, d, c.pl, "Schornsteinfeger Strom", 10)
		if err != nil || got.Docs != nil || got.DocsDown || names(got.Hits) != "kaminkehrer.md,heizung.md" {
			t.Fatalf("%+v %v", got, err)
		}
	}
	if n := len(p.Seen()); n != 0 {
		t.Fatalf("Paperless asked %d times", n)
	}
}

// The words' answer says how many more documents Paperless counted than
// it shows; a question's words go to Paperless in lower case, so a word
// that is one of its search operators (NOT, AND) is a word.
func TestDocumentsByWordsCountsTheRest(t *testing.T) {
	old := Wait
	Wait = 150 * time.Millisecond
	defer func() { Wait = old }()
	_, d, f := shelf(t)
	p := archive(t, f)
	for i := 20; i < 30; i++ {
		p.Add(paperlessfake.Doc{ID: i, Title: "Versicherung", Content: "Versicherung"})
	}
	f.Status = http.StatusInternalServerError
	cl := &qmd.Client{URL: f.URL, Collection: "notizen", Documents: "dokumente"}
	got, err := Find(bg, cl, Tidy{}, d, &paperless.Client{URL: p.URL, Token: "pl-token"}, "Versicherung", 4)
	if err != nil || len(got.Docs) != 2 || got.DocsMore != 9 {
		t.Fatalf("%d docs, %d more: %+v %v", len(got.Docs), got.DocsMore, got, err)
	}
	if q := AnyWord("NOT AND Versicherung"); q != "not OR and OR versicherung" {
		t.Fatalf("AnyWord: %q", q)
	}
}

// What qmd names in the documents' collection is never read as a note,
// also when the notes' folder holds a file of that name and Paperless is
// not there to check the document against.
func TestADocumentHitIsNoNote(t *testing.T) {
	dir, d, f := shelf(t)
	os.WriteFile(filepath.Join(dir, "7.md"), []byte("# Sieben\n\nGEHEIM-NOTIZ\n"), 0o644)
	archive(t, f)
	cl := &qmd.Client{URL: f.URL, Collection: "notizen", Documents: "dokumente"}
	got, err := Find(bg, cl, Tidy{}, d, nil, "Laufzeit", 10)
	if err == nil {
		for _, h := range got.Hits {
			if h.Name == "7.md" {
				t.Fatalf("a document's hit read as the note 7.md: %+v", got)
			}
		}
	}
}

// Paperless that hangs gets at most half the time the caller has left,
// so the words still have theirs: a question only documents answer, with
// Paperless hanging, ends in the notes' word search, not in nothing.
func TestPaperlessSlowLeavesTheWordsTheirTime(t *testing.T) {
	dir, d, f := shelf(t)
	os.WriteFile(filepath.Join(dir, "vertrag.md"), []byte("# Vertrag\n\nDie Laufzeit steht im Brief.\n"), 0o644)
	p := archive(t, f)
	hang := make(chan struct{})
	defer close(hang)
	slow := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		select {
		case <-hang:
		case <-r.Context().Done():
		}
		return nil, r.Context().Err()
	})}
	cl := &qmd.Client{URL: f.URL, Collection: "notizen", Documents: "dokumente"}
	ctx, cancel := context.WithTimeout(bg, 1200*time.Millisecond)
	defer cancel()
	got, err := Find(ctx, cl, Tidy{}, d, &paperless.Client{URL: p.URL, Token: "pl-token", HTTP: slow}, "Laufzeit", 10)
	if err != nil || !got.DocsDown || got.Partial || names(got.Hits) != "vertrag.md" {
		t.Fatalf("%+v %v", got, err)
	}
}
