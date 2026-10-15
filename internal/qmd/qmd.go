// Package qmd is Haus's client for qmd (github.com/tobi/qmd), the memory
// library's search (haus#13): keyword and vector search over the notes,
// run by Laib in Haus's pod on loopback (laib#195), behind the Vermittler
// (laib#198), a small server on qmd's SDK that adds each hit's cosine. It
// has no auth, so its address is 127.0.0.1 or localhost, never a name
// another container could reach.
//
// Haus asks only two things of it: POST /query with typed searches, and
// GET /status. A search is always words (lex) plus meaning (vec), rerank
// off, scoped to the notes' collection — never a plain query: that pulls
// in the 1.7B query expansion model and the reranker, 30–140 s a query on
// the CPU, and found nothing more in the eval.
package qmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Client is one qmd, and the collection that holds the notes.
type Client struct {
	URL        string // http://127.0.0.1:<port>, no trailing slash
	Collection string // the notes' collection in qmd's index.yml
	HTTP       *http.Client
}

// maxAnswer caps what Haus reads of one answer.
const maxAnswer = 4 << 20

// Hit is one document qmd found: its path inside the collection, decoded
// and plain (no step out, nothing hidden), qmd's title, the lines of its
// best passage and the note's line that passage starts at (0: not said),
// and its cosine to the question (0: outside qmd's list by meaning).
type Hit struct {
	Path  string
	Title string
	Lines []string
	Start int
	Vec   float64
}

var noRedirects = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

// call sends body (nil: a GET) to p and returns the answer.
func (c *Client) call(ctx context.Context, p string, body any) ([]byte, error) {
	method, rd := http.MethodGet, io.Reader(nil)
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		method, rd = http.MethodPost, bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.URL, "/")+p, rd)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	hc := c.HTTP
	if hc == nil {
		hc = noRedirects
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(resp.Body, maxAnswer))
		return nil, fmt.Errorf("qmd %s: HTTP %d", p, resp.StatusCode)
	}
	out, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer+1))
	if err != nil {
		return nil, err
	}
	if len(out) > maxAnswer {
		return nil, fmt.Errorf("qmd %s: the answer is over %d bytes", p, maxAnswer)
	}
	return out, nil
}

type search struct {
	Type  string `json:"type"`
	Query string `json:"query"`
}

// query is the whole body Haus sends: nothing qmd would fill with its
// defaults (rerank on, a plain query expanded by the LLM, an intent).
type query struct {
	Searches    []search `json:"searches"`
	Limit       int      `json:"limit"`
	MinScore    float64  `json:"minScore"`
	Rerank      bool     `json:"rerank"`
	Collections []string `json:"collections"`
}

// Search asks qmd for the notes that fit text, by words and by meaning,
// at most limit of them, best first. What qmd names outside the notes'
// collection, or not as a plain path inside it, is left out.
func (c *Client) Search(ctx context.Context, text string, limit int) ([]Hit, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("qmd: an empty search")
	}
	body := query{
		Searches:    []search{{Type: "lex", Query: text}, {Type: "vec", Query: text}},
		Limit:       limit,
		Rerank:      false,
		Collections: []string{c.Collection},
	}
	raw, err := c.call(ctx, "/query", body)
	if err != nil {
		return nil, err
	}
	var ans struct {
		Results []struct {
			File    string  `json:"file"`
			Title   string  `json:"title"`
			Snippet string  `json:"snippet"`
			Vec     float64 `json:"vec"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &ans); err != nil {
		return nil, fmt.Errorf("qmd /query: %w", err)
	}
	hits := []Hit{}
	for _, r := range ans.Results {
		p, ok := c.local(r.File)
		if !ok {
			continue
		}
		hits = append(hits, Hit{Path: p, Title: r.Title, Lines: Lines(r.Snippet), Start: start(r.Snippet), Vec: r.Vec})
	}
	return hits, nil
}

// local turns qmd://<collection>/<segments, each percent-encoded> into
// the plain path inside the collection, or says it is none: another
// collection, an empty or hidden segment (".." included: a step out), a
// segment that decodes to a separator or a NUL.
func (c *Client) local(file string) (string, bool) {
	rest, ok := strings.CutPrefix(file, "qmd://"+c.Collection+"/")
	if !ok || rest == "" {
		return "", false
	}
	segs := strings.Split(rest, "/")
	for i, s := range segs {
		d, err := url.PathUnescape(s)
		if err != nil || d == "" || strings.HasPrefix(d, ".") || strings.ContainsAny(d, "/\\\x00") {
			return "", false
		}
		segs[i] = d
	}
	return strings.Join(segs, "/"), true
}

// maxLines bounds a passage: qmd's snippets are a few lines.
const maxLines = 12

// Lines is qmd's snippet as the note's own lines: without its diff-like
// header ("@@ -5,4 @@ (4 before, 1 after)"), the line numbers it puts in
// front ("7: ") and blank lines; qmd's " ..." at a cut is "…". Making
// them plain — Markdown, tables, length — is notes.Passage's, which knows
// the note.
func Lines(snippet string) []string {
	var out []string
	for _, l := range strings.Split(snippet, "\n") {
		if i := strings.Index(l, ": "); i > 0 && allDigits(l[:i]) {
			l = l[i+2:]
		}
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "@@ ") {
			continue
		}
		if s, ok := strings.CutSuffix(l, " ..."); ok {
			l = s + " …"
		}
		out = append(out, l)
		if len(out) == maxLines {
			break
		}
	}
	return out
}

// start is the note's line a snippet's passage starts at, from its header
// ("@@ -5,4 @@": line 5), or 0. qmd's own line numbers in front of each
// line, and its "line" field, count the header too: they are one more.
func start(snippet string) int {
	i := strings.Index(snippet, "@@ -")
	if i < 0 {
		return 0
	}
	n := 0
	for _, c := range snippet[i+4:] {
		if c < '0' || c > '9' || n > 1e6 {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func allDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return s != ""
}

// Status is what qmd's index holds of the notes' collection.
type Status struct {
	Documents int // the collection's documents
	Embedded  int // how many of them have their vectors
}

// Ready says the search by meaning can answer: the collection is indexed
// and something in it has its vectors. Until then (before the first `qmd
// embed`), qmd answers words only.
func (s Status) Ready() bool {
	return s.Documents > 0 && s.Embedded > 0
}

// Status asks the Vermittler's GET /status.
func (c *Client) Status(ctx context.Context) (Status, error) {
	raw, err := c.call(ctx, "/status", nil)
	if err != nil {
		return Status{}, err
	}
	var ans struct {
		Collections *[]struct {
			Name      string `json:"name"`
			Documents int    `json:"documents"`
			Embedded  int    `json:"embedded"`
		} `json:"collections"`
	}
	if err := json.Unmarshal(raw, &ans); err != nil {
		return Status{}, fmt.Errorf("qmd status: %w", err)
	}
	if ans.Collections == nil {
		return Status{}, errors.New("qmd status: no collections in the answer")
	}
	for _, col := range *ans.Collections {
		if col.Name == c.Collection {
			return Status{Documents: col.Documents, Embedded: col.Embedded}, nil
		}
	}
	return Status{}, nil
}
