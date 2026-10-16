package qmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/mdopp/haus/internal/qmd/qmdfake"
)

// testdata holds what the Vermittler's prototype (laib#198: qmd 2.8.3's
// SDK, embeddinggemma on the CPU) answered on the eval's German notes
// (haus#13); the fake answers in the same shapes.
func captured(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// verbatim serves body for every request, as content type ct.
func verbatim(t *testing.T, ct, body string) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ct)
		w.Write([]byte(body))
	}))
	t.Cleanup(s.Close)
	return s
}

func paths(hits []Hit) []string {
	var out []string
	for _, h := range hits {
		out = append(out, h.Path)
	}
	return out
}

// The defaults are the slow path: a plain query pulls in the 1.7B query
// expansion model and the reranker, 30–140 s a query on the CPU (haus#13).
// Haus always sends the two typed searches, words (lex) and meaning (vec),
// with rerank off, scoped to the notes' collection — and nothing else.
func TestSendsTypedSearchesNeverThePlainQuery(t *testing.T) {
	f := qmdfake.New("notizen")
	defer f.Close()
	c := &Client{URL: f.URL, Collection: "notizen"}
	if _, err := c.Search(context.Background(), "wann kommt der Schornsteinfeger", 7); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	var queries int
	for _, r := range f.Seen() {
		switch r.Path {
		case "/query":
			queries++
			var body map[string]any
			if err := json.Unmarshal(r.Body, &body); err != nil {
				t.Fatalf("not JSON: %s", r.Body)
			}
			keys := []string{}
			for k := range body {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			// exactly what the Vermittler takes (laib#198, qmd/vermittler.mjs):
			// any other field, minScore too, is its 400
			if strings.Join(keys, ",") != "collections,limit,rerank,searches" {
				t.Fatalf("the body says %v, want only collections, limit, rerank, searches: %s", keys, r.Body)
			}
			if rr, ok := body["rerank"].(bool); !ok || rr {
				t.Fatalf("rerank must be sent, and false: %s", r.Body)
			}
			want := []any{map[string]any{"type": "lex", "query": "wann kommt der Schornsteinfeger"},
				map[string]any{"type": "vec", "query": "wann kommt der Schornsteinfeger"}}
			if !reflect.DeepEqual(body["searches"], want) {
				t.Fatalf("searches: %v", body["searches"])
			}
			if !reflect.DeepEqual(body["collections"], []any{"notizen"}) || body["limit"] != float64(7) {
				t.Fatalf("scope: %s", r.Body)
			}
		case "/status":
			if r.Method != http.MethodGet || len(r.Body) != 0 {
				t.Fatalf("status is a GET: %s %s", r.Method, r.Body)
			}
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.Path)
		}
	}
	if queries != 1 {
		t.Fatalf("%d queries, want 1", queries)
	}
}

// The Vermittler's answer, as captured: the document path relative to
// the collection, decoded; the title; the snippet as the note's own lines,
// without qmd's diff header and the line numbers it puts in front, and the
// note's line it starts at (the header's, not qmd's numbers, which count
// the header); the cosine. Making the lines plain (Markdown, tables,
// length) is the notes package's: it knows the note (haus#13).
func TestReadsRealAnswers(t *testing.T) {
	ctx := context.Background()
	c := &Client{URL: verbatim(t, "application/json", captured(t, "query-meaning.json")).URL, Collection: "notizen"}
	hits, err := c.Search(ctx, "wann muss der Schornsteinfeger kommen", 5)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(paths(hits), ","); got != "kaminkehrer.md,heizung-wartung.md,dachrinne.md,rauchmelder.md,handwerker-kontakte.md" {
		t.Fatalf("paths: %s", got)
	}
	k := hits[0]
	if k.Title != "Kaminkehrer" || len(k.Lines) != 4 || k.Lines[0] != "- Kommt einmal im Jahr, meist zweite Oktoberwoche, kündigt sich per Zettel im Briefkasten an." {
		t.Fatalf("first hit: %+v", k)
	}
	var got []string
	for _, h := range hits {
		got = append(got, fmt.Sprintf("%d %.3f", h.Start, h.Vec))
	}
	if strings.Join(got, ",") != "5 0.538,7 0.318,2 0.311,1 0.305,1 0.276" {
		t.Fatalf("start lines and cosines: %s", strings.Join(got, ","))
	}
	for _, h := range hits {
		for _, l := range h.Lines {
			if strings.Contains(l, "@@") || l == "" || l[0] >= '0' && l[0] <= '9' && strings.Contains(l[:min(4, len(l))], ":") {
				t.Fatalf("%s: a line still carries qmd's markup: %q", h.Path, l)
			}
		}
	}
	// the rauchmelder snippet is its heading and the first line; the
	// handwerker snippet its heading and a table's header row only
	if r := hits[3]; strings.Join(r.Lines, "|") != "# Rauchwarnmelder|Wir haben 7 Stück: alle Schlafzimmer, Kinderzimmer, Flur oben + unten, Wohnzimmer." {
		t.Fatalf("rauchmelder: %q", r.Lines)
	}
	if h := hits[4]; strings.Join(h.Lines, "\n") != "# Handwerker\n| Gewerk | Firma | Telefon | Notiz |" {
		t.Fatalf("handwerker: %q", h.Lines)
	}
	// qmd's " ..." at a cut snippet is the page's ellipsis
	if last := k.Lines[len(k.Lines)-1]; strings.HasSuffix(last, " ...") {
		t.Fatalf("qmd's ellipsis: %q", last)
	}

	// folders, umlauts, spaces, & and parentheses: qmd encodes each segment
	for file, want := range map[string]string{
		"query-subfolder.json": "rezepte/rezept-pfannkuchen.md",
		"query-umlaut.json":    "Größen Kinder.md",
		"query-ampersand.json": "schlüssel & keller (2).md",
	} {
		c := &Client{URL: verbatim(t, "application/json", captured(t, file)).URL, Collection: "notizen"}
		hits, err := c.Search(ctx, "x", 5)
		if err != nil || len(hits) == 0 || hits[0].Path != want {
			t.Fatalf("%s: %v %v", file, paths(hits), err)
		}
	}
}

// A path that is not a plain name inside the notes' collection is dropped,
// never passed on: another collection, a step out, a hidden part, an
// encoded slash or backslash, another scheme.
func TestDropsWhatIsNotANoteOfTheCollection(t *testing.T) {
	item := func(file string) string {
		return `{"docid":"#000001","file":` + strconvQuote(file) + `,"title":"T","score":1,"context":null,"line":1,"snippet":"1: @@ -1,1 @@ (0 before, 0 after)\n2: Text"}`
	}
	bad := []string{
		"qmd://andere/kaminkehrer.md",
		"qmd://notizen-alt/kaminkehrer.md",
		"qmd://notizen/../geheim.md",
		"qmd://notizen/%2E%2E/geheim.md",
		"qmd://notizen/rezepte/../../geheim.md",
		"qmd://notizen/a/%2E%2E/%2E%2E/geheim.md",
		"qmd://notizen/..%2Fgeheim.md",
		"qmd://notizen/..%5Cgeheim.md",
		"qmd://notizen/%2Fetc%2Fpasswd",
		"qmd://notizen//etc/passwd",
		"qmd://notizen/.stversions/alt.md",
		"qmd://notizen/rezepte/.versteckt.md",
		"qmd://notizen/nul%00.md",
		"qmd://notizen/kaputt%zz.md",
		"qmd://notizen/",
		"qmd://notizen",
		"file:///etc/passwd",
		"/etc/passwd",
		"notizen/kaminkehrer.md",
		"QMD://notizen/x.md",
	}
	var items []string
	for _, b := range bad {
		items = append(items, item(b))
	}
	items = append(items, item("qmd://notizen/rezepte/linsensuppe.md"))
	c := &Client{URL: verbatim(t, "application/json", `{"results":[`+strings.Join(items, ",")+`]}`).URL, Collection: "notizen"}
	hits, err := c.Search(context.Background(), "x", 50)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(paths(hits), ","); got != "rezepte/linsensuppe.md" {
		t.Fatalf("kept %q, want only rezepte/linsensuppe.md", got)
	}
}

func strconvQuote(s string) string { b, _ := json.Marshal(s); return string(b) }

// The Vermittler's GET /status: ready only once the notes' collection
// has documents and some of them their vectors. Before the first `qmd
// embed` it answers words only, which is the reduced search.
func TestStatusFromRealAnswers(t *testing.T) {
	ctx := context.Background()
	c := &Client{URL: verbatim(t, "application/json", captured(t, "status.json")).URL, Collection: "notizen"}
	st, err := c.Status(ctx)
	if err != nil || !st.Ready() || st.Documents != 46 || st.Embedded != 46 {
		t.Fatalf("captured: %+v %v", st, err)
	}
	// the same index, but Haus's collection is not in it
	c.Collection = "andere"
	if st, err := c.Status(ctx); err != nil || st.Ready() {
		t.Fatalf("a collection the index does not have is not ready: %+v %v", st, err)
	}
	f := qmdfake.New("notizen")
	defer f.Close()
	f.Add(qmdfake.Doc{Path: "a.md", Title: "A", Body: "# A\n\nText"})
	c = &Client{URL: f.URL, Collection: "notizen"}
	if st, err := c.Status(ctx); err != nil || !st.Ready() {
		t.Fatalf("fake, embedded: %+v %v", st, err)
	}
	f.Indexing = true
	if st, err := c.Status(ctx); err != nil || st.Ready() {
		t.Fatalf("fake, indexing: %+v %v", st, err)
	}
	for body, ready := range map[string]bool{
		`{"collections":[{"name":"notizen","documents":3,"embedded":1}]}`:                                              true,
		`{"collections":[{"name":"notizen","documents":3,"embedded":0}]}`:                                              false,
		`{"collections":[{"name":"notizen","documents":0,"embedded":0}]}`:                                              false,
		`{"collections":[{"name":"andere","documents":3,"embedded":3},{"name":"notizen","documents":3,"embedded":0}]}`: false,
		`{"collections":[]}`: false,
	} {
		if st, err := (&Client{URL: verbatim(t, "application/json", body).URL, Collection: "notizen"}).Status(ctx); err != nil || st.Ready() != ready {
			t.Fatalf("%s: %+v %v, want ready %v", body, st, err, ready)
		}
	}
	// an answer without collections, or not JSON, is an error
	for _, body := range []string{`{}`, `{"collections":null}`, `nicht json`, `{"collections":"notizen"}`} {
		if _, err := (&Client{URL: verbatim(t, "application/json", body).URL, Collection: "notizen"}).Status(ctx); err == nil {
			t.Fatalf("%s: want an error", body)
		}
	}
}

// The fake answers in the captured shapes: the same fields (vec too),
// the same path encoding, the same snippet header.
func TestFakeAnswersLikeQmd(t *testing.T) {
	keys := func(raw string) (string, map[string]any) {
		var r struct {
			Results []map[string]any `json:"results"`
		}
		if err := json.Unmarshal([]byte(raw), &r); err != nil || len(r.Results) == 0 {
			t.Fatalf("%v: %s", err, raw)
		}
		var ks []string
		for k := range r.Results[0] {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		return strings.Join(ks, ","), r.Results[0]
	}
	realKeys, real := keys(captured(t, "query-ampersand.json"))
	f := qmdfake.New("notizen")
	defer f.Close()
	f.Add(qmdfake.Doc{Path: "schlüssel & keller (2).md", Title: "schlüssel & keller (2)",
		Body: "Notiz ohne Überschrift über den Kellerschlüssel: hängt am Brett neben der Tür.\n"})
	resp, err := http.Post(f.URL+"/query", "application/json", strings.NewReader(`{"searches":[{"type":"lex","query":"Kellerschlüssel"},{"type":"vec","query":"Kellerschlüssel"}],"limit":3,"rerank":false,"collections":["notizen"]}`))
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		b.Write(buf[:n])
		if err != nil {
			break
		}
	}
	resp.Body.Close()
	fakeKeys, fake := keys(b.String())
	if fakeKeys != realKeys {
		t.Fatalf("fields: fake %s, qmd %s", fakeKeys, realKeys)
	}
	for _, k := range []string{"docid", "file", "title", "snippet", "score", "line", "context"} {
		if !reflect.DeepEqual(fake[k], real[k]) {
			t.Fatalf("%s: fake %#v, qmd %#v", k, fake[k], real[k])
		}
	}
}

// Haus reads qmd's answer and nothing else: a redirect is not followed,
// a status other than 200 is an error that names it, never qmd's body.
func TestErrorsArePlain(t *testing.T) {
	ctx := context.Background()
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("followed a redirect to %s", r.URL)
	}))
	defer elsewhere.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	if _, err := (&Client{URL: redirect.URL, Collection: "notizen"}).Search(ctx, "x", 3); err == nil {
		t.Fatal("a redirect is an error")
	}
	f := qmdfake.New("notizen")
	defer f.Close()
	f.Status = http.StatusInternalServerError
	c := &Client{URL: f.URL, Collection: "notizen"}
	if _, err := c.Search(ctx, "x", 3); err == nil || !strings.Contains(err.Error(), "500") || strings.Contains(err.Error(), "Internal Server Error") {
		t.Fatalf("500: %v", err)
	}
	if _, err := c.Status(ctx); err == nil {
		t.Fatal("status 500 is an error")
	}
	f.Status = 0
	f.Results = `{"results":"kaputt"}`
	if _, err := c.Search(ctx, "x", 3); err == nil {
		t.Fatal("a shape Haus does not know is an error")
	}
	f.Results = ""
	// qmd's guard lets a loopback Host in
	if _, err := (&Client{URL: strings.Replace(f.URL, "127.0.0.1", "localhost", 1), Collection: "notizen"}).Search(ctx, "x", 3); err != nil {
		t.Fatalf("localhost is loopback: %v", err)
	}
	f.Close()
	if _, err := c.Search(ctx, "x", 3); err == nil {
		t.Fatal("qmd down is an error")
	}
	// a blank search asks nothing
	if hits, err := (&Client{URL: "http://127.0.0.1:1", Collection: "notizen"}).Search(ctx, "  ", 3); err == nil || hits != nil {
		t.Fatalf("blank: %v %v", hits, err)
	}
}

// An answer is read through a limit: a qmd gone wrong cannot fill Haus's
// memory.
func TestAnswerIsBounded(t *testing.T) {
	big := `{"results":[` + strings.Repeat(`{"docid":"#1","file":"qmd://notizen/a.md","title":"`+strings.Repeat("x", 1<<16)+`","score":1,"context":null,"line":1,"snippet":""},`, 200) + `{}]}`
	c := &Client{URL: verbatim(t, "application/json", big).URL, Collection: "notizen"}
	if _, err := c.Search(context.Background(), "x", 3); err == nil {
		t.Fatal("an answer over the cap is an error")
	}
	// even when what fits under the cap would read as an answer
	padded := `{"results":[{"docid":"#1","file":"qmd://notizen/a.md","title":"A","score":1,"context":null,"line":1,"snippet":""}]}` + strings.Repeat(" ", 5<<20)
	c = &Client{URL: verbatim(t, "application/json", padded).URL, Collection: "notizen"}
	if hits, err := c.Search(context.Background(), "x", 3); err == nil {
		t.Fatalf("an answer over the cap is an error, not %v", hits)
	}
}

// The fake refuses what the Vermittler refuses (laib#198): a field it does
// not take — minScore, intent, a plain query — rerank on, a search that is
// not lex or vec. So a Haus that sends one fails here, not on the box,
// where every search would fall to words (haus#13).
func TestFakeRefusesLikeTheVermittler(t *testing.T) {
	f := qmdfake.New("notizen")
	defer f.Close()
	f.Add(qmdfake.Doc{Path: "a.md", Title: "A", Body: "Kellerschlüssel"})
	for _, body := range []string{
		`{"searches":[{"type":"lex","query":"x"}],"minScore":0}`,
		`{"searches":[{"type":"lex","query":"x"}],"intent":"y"}`,
		`{"query":"x"}`,
		`{"searches":[{"type":"lex","query":"x"}],"rerank":true}`,
		`{"searches":[{"type":"hyde","query":"x"}]}`,
		`{"searches":[{"type":"lex","query":" "}]}`,
	} {
		resp, err := http.Post(f.URL+"/query", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s: %d, want 400", body, resp.StatusCode)
		}
	}
	// and Haus's own body is taken
	c := &Client{URL: f.URL, Collection: "notizen"}
	if hits, err := c.Search(context.Background(), "Kellerschlüssel", 3); err != nil || len(hits) != 1 {
		t.Fatalf("%v %v", hits, err)
	}
}

// haus#13: with the documents' collection set, a search covers both
// collections and says of each hit which one it is from; another
// collection's hits stay out, and a status names each collection's counts.
func TestDocumentsCollection(t *testing.T) {
	var asked map[string]any
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/status" {
			w.Write([]byte(`{"collections":[{"name":"notizen","documents":4,"embedded":4},{"name":"dokumente","documents":9,"embedded":3},{"name":"fremd","documents":1,"embedded":1}]}`))
			return
		}
		json.NewDecoder(r.Body).Decode(&asked)
		w.Write([]byte(`{"results":[` +
			`{"file":"qmd://dokumente/7.md","title":"Stromvertrag","snippet":"1: a","vec":0.5},` +
			`{"file":"qmd://notizen/a.md","title":"A","snippet":"1: b","vec":0.4},` +
			`{"file":"qmd://fremd/7.md","title":"Fremd","snippet":"1: c","vec":0.9},` +
			`{"file":"qmd://dokumente/..%2F7.md","title":"Raus","snippet":"1: d","vec":0.9}]}`))
	}))
	defer s.Close()
	c := &Client{URL: s.URL, Collection: "notizen", Documents: "dokumente"}
	hits, err := c.Search(context.Background(), "Laufzeit", 5)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(asked["collections"], []any{"notizen", "dokumente"}) {
		t.Fatalf("asked %v", asked["collections"])
	}
	got := fmt.Sprintf("%s/%s %s/%s", hits[0].Collection, hits[0].Path, hits[1].Collection, hits[1].Path)
	if len(hits) != 2 || got != "dokumente/7.md notizen/a.md" {
		t.Fatalf("%d hits: %s", len(hits), got)
	}
	notes, docs, err := c.Statuses(context.Background())
	if err != nil || notes != (Status{4, 4}) || docs != (Status{9, 3}) {
		t.Fatalf("%+v %+v %v", notes, docs, err)
	}
	// without it: the notes only, as before
	c.Documents = ""
	hits, _ = c.Search(context.Background(), "Laufzeit", 5)
	if !reflect.DeepEqual(asked["collections"], []any{"notizen"}) || len(hits) != 1 || hits[0].Path != "a.md" {
		t.Fatalf("notes only: %v %+v", asked["collections"], hits)
	}
}
