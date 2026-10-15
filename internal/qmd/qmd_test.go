package qmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/mdopp/haus/internal/qmd/qmdfake"
)

// testdata holds what a real qmd 2.8.3 (`qmd mcp --http`, embeddinggemma
// on the CPU) answered on German notes (haus#13), the eval folder's path
// replaced by /notes; the fake answers in the same shapes.
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
			if strings.Join(keys, ",") != "collections,limit,minScore,rerank,searches" {
				t.Fatalf("the body says %v, want only collections, limit, minScore, rerank, searches: %s", keys, r.Body)
			}
			if rr, ok := body["rerank"].(bool); !ok || rr {
				t.Fatalf("rerank must be sent, and false: %s", r.Body)
			}
			want := []any{map[string]any{"type": "lex", "query": "wann kommt der Schornsteinfeger"},
				map[string]any{"type": "vec", "query": "wann kommt der Schornsteinfeger"}}
			if !reflect.DeepEqual(body["searches"], want) {
				t.Fatalf("searches: %v", body["searches"])
			}
			if !reflect.DeepEqual(body["collections"], []any{"notizen"}) || body["limit"] != float64(7) || body["minScore"] != float64(0) {
				t.Fatalf("scope: %s", r.Body)
			}
		case "/mcp":
			var rpc struct {
				Method string `json:"method"`
				Params struct {
					Name string `json:"name"`
				} `json:"params"`
			}
			json.Unmarshal(r.Body, &rpc)
			if rpc.Method != "tools/call" || rpc.Params.Name != "status" {
				t.Fatalf("the only MCP call is the status tool, not %s %s", rpc.Method, rpc.Params.Name)
			}
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.Path)
		}
	}
	if queries != 1 {
		t.Fatalf("%d queries, want 1", queries)
	}
}

// qmd's own answer, as captured: the document path relative to the
// collection, decoded; the title; the snippet as lines, without qmd's
// diff header, its line numbers or the note's heading.
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
	if k.Title != "Kaminkehrer" || len(k.Lines) == 0 || k.Lines[0] != "- Kommt einmal im Jahr, meist zweite Oktoberwoche, kündigt sich per Zettel im Briefkasten an." {
		t.Fatalf("first hit: %+v", k)
	}
	if len(k.Lines) > 3 {
		t.Fatalf("at most three lines: %q", k.Lines)
	}
	for _, h := range hits {
		for _, l := range h.Lines {
			if strings.Contains(l, "@@") || strings.HasPrefix(l, "# ") || l == "" || l[0] >= '0' && l[0] <= '9' && strings.Contains(l[:min(4, len(l))], ":") {
				t.Fatalf("%s: a line still carries qmd's markup: %q", h.Path, l)
			}
			if len([]rune(l)) > 121 {
				t.Fatalf("%s: a line is not cut: %q", h.Path, l)
			}
		}
	}
	// the rauchmelder snippet is its heading and the first line
	if r := hits[3]; len(r.Lines) != 1 || r.Lines[0] != "Wir haben 7 Stück: alle Schlafzimmer, Kinderzimmer, Flur oben + unten, Wohnzimmer." {
		t.Fatalf("heading left out: %+v", r)
	}
	// qmd's " ..." at a cut snippet is the page's ellipsis
	if last := k.Lines[len(k.Lines)-1]; strings.HasSuffix(last, " ...") {
		t.Fatalf("qmd's ellipsis: %q", last)
	}

	// folders, umlauts, spaces, & and parentheses: qmd encodes each segment
	for file, want := range map[string]string{
		"query-subfolder.json":    "rezepte/rezept-pfannkuchen.md",
		"query-umlaut.json":       "Größen Kinder.md",
		"query-before-embed.json": "schlüssel & keller (2).md",
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

// qmd's status tool, over stateless streamable HTTP (an SSE frame): ready
// only once the notes' collection has documents and vectors. Before the
// first `qmd embed` it answers words only, which is the reduced search.
func TestStatusFromRealAnswers(t *testing.T) {
	ctx := context.Background()
	for file, ready := range map[string]bool{"status-embedded.sse": true, "status-before-embed.sse": false} {
		c := &Client{URL: verbatim(t, "text/event-stream", captured(t, file)).URL, Collection: "notizen"}
		st, err := c.Status(ctx)
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		if st.Ready() != ready || st.Documents != 46 {
			t.Fatalf("%s: %+v ready %v", file, st, st.Ready())
		}
		// the same index, but Haus's collection is not in it
		c.Collection = "andere"
		if st, err := c.Status(ctx); err != nil || st.Ready() {
			t.Fatalf("%s: a collection qmd does not have is not ready: %+v %v", file, st, err)
		}
	}
	// the MCP transport may answer plain JSON too
	f := qmdfake.New("notizen")
	defer f.Close()
	f.Add(qmdfake.Doc{Path: "a.md", Title: "A", Body: "# A\n\nText"})
	c := &Client{URL: f.URL, Collection: "notizen"}
	if st, err := c.Status(ctx); err != nil || !st.Ready() {
		t.Fatalf("fake, embedded: %+v %v", st, err)
	}
	f.Indexing = true
	if st, err := c.Status(ctx); err != nil || st.Ready() {
		t.Fatalf("fake, indexing: %+v %v", st, err)
	}
	j := verbatim(t, "application/json", `{"result":{"content":[],"structuredContent":{"totalDocuments":3,"needsEmbedding":0,"hasVectorIndex":true,"collections":[{"name":"notizen","documents":3}]}},"jsonrpc":"2.0","id":1}`)
	if st, err := (&Client{URL: j.URL, Collection: "notizen"}).Status(ctx); err != nil || !st.Ready() {
		t.Fatalf("plain JSON: %+v %v", st, err)
	}
	// nothing embedded yet, though an index of vectors exists (a new model)
	n := verbatim(t, "application/json", `{"result":{"structuredContent":{"totalDocuments":3,"needsEmbedding":3,"hasVectorIndex":true,"collections":[{"name":"notizen","documents":3}]}},"jsonrpc":"2.0","id":1}`)
	if st, _ := (&Client{URL: n.URL, Collection: "notizen"}).Status(ctx); st.Ready() {
		t.Fatalf("nothing embedded is not ready: %+v", st)
	}
	// a JSON-RPC error, or a tool error, is an error
	for _, body := range []string{`{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"nope"}}`,
		`{"jsonrpc":"2.0","id":1,"result":{"isError":true,"content":[{"type":"text","text":"kaputt"}]}}`, `event: message\ndata: nicht json\n\n`} {
		e := verbatim(t, "application/json", body)
		if _, err := (&Client{URL: e.URL, Collection: "notizen"}).Status(ctx); err == nil {
			t.Fatalf("%s: want an error", body)
		}
	}
}

// The fake answers in the captured shapes: the same fields, the same
// path encoding, the same snippet header.
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
	realKeys, real := keys(captured(t, "query-before-embed.json"))
	f := qmdfake.New("notizen")
	defer f.Close()
	f.Add(qmdfake.Doc{Path: "schlüssel & keller (2).md", Title: "schlüssel & keller (2)",
		Body: "Notiz ohne Überschrift über den Kellerschlüssel: hängt am Brett neben der Tür.\n"})
	resp, err := http.Post(f.URL+"/query", "application/json", strings.NewReader(`{"searches":[{"type":"lex","query":"Kellerschlüssel"},{"type":"vec","query":"Kellerschlüssel"}],"limit":3,"minScore":0,"rerank":false,"collections":["notizen"]}`))
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
	// qmd's guard refuses a Host that is not loopback
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
}
