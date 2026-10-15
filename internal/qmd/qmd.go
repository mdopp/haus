// Package qmd is Haus's client for qmd (github.com/tobi/qmd), the memory
// library's search (haus#13): keyword and vector search over the notes,
// run by Laib in Haus's pod on loopback (laib#195). qmd has no auth, and
// its guard refuses a Host that is not loopback, so its address is
// 127.0.0.1 or localhost, never a name another container could reach.
//
// Haus asks only two things of it: POST /query with typed searches, and
// the status tool over MCP. A search is always words (lex) plus meaning
// (vec), rerank off, scoped to the notes' collection — never a plain
// query: that pulls in the 1.7B query expansion model and the reranker,
// 30–140 s a query on the CPU, and found nothing more in the eval.
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
// and plain (no step out, nothing hidden), qmd's title and the lines of
// its best passage.
type Hit struct {
	Path  string
	Title string
	Lines []string
}

var noRedirects = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

func (c *Client) post(ctx context.Context, p string, body any, accept string) ([]byte, string, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.URL, "/")+p, bytes.NewReader(b))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", accept)
	hc := c.HTTP
	if hc == nil {
		hc = noRedirects
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(resp.Body, maxAnswer))
		return nil, "", fmt.Errorf("qmd %s: HTTP %d", p, resp.StatusCode)
	}
	out, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer+1))
	if err != nil {
		return nil, "", err
	}
	if len(out) > maxAnswer {
		return nil, "", fmt.Errorf("qmd %s: the answer is over %d bytes", p, maxAnswer)
	}
	return out, resp.Header.Get("Content-Type"), nil
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
	raw, _, err := c.post(ctx, "/query", body, "application/json")
	if err != nil {
		return nil, err
	}
	var ans struct {
		Results []struct {
			File    string `json:"file"`
			Title   string `json:"title"`
			Snippet string `json:"snippet"`
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
		hits = append(hits, Hit{Path: p, Title: r.Title, Lines: Lines(r.Snippet)})
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
	Documents      int  // the collection's documents
	Total          int  // every collection's
	NeedsEmbedding int  // documents without vectors yet, every collection's
	Vectors        bool // the vector index exists
}

// Ready says the search by meaning can answer: the collection is
// indexed, there is a vector index, and something has its vectors. Until
// then (before the first `qmd embed`), qmd answers words only.
func (s Status) Ready() bool {
	return s.Documents > 0 && s.Vectors && s.NeedsEmbedding < s.Total
}

// Status asks qmd's status tool (MCP over HTTP, stateless: no session),
// whose answer comes as JSON or as one SSE frame.
func (c *Client) Status(ctx context.Context) (Status, error) {
	raw, ct, err := c.post(ctx, "/mcp", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": "status", "arguments": map[string]any{}}}, "application/json, text/event-stream")
	if err != nil {
		return Status{}, err
	}
	if strings.HasPrefix(ct, "text/event-stream") || bytes.HasPrefix(raw, []byte("event:")) || bytes.HasPrefix(raw, []byte("data:")) {
		var data []byte
		for _, l := range bytes.Split(raw, []byte("\n")) {
			if d, ok := bytes.CutPrefix(bytes.TrimRight(l, "\r"), []byte("data:")); ok {
				data = bytes.TrimSpace(d)
			}
		}
		raw = data
	}
	var ans struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
		Result *struct {
			IsError bool `json:"isError"`
			Status  *struct {
				Total          int  `json:"totalDocuments"`
				NeedsEmbedding int  `json:"needsEmbedding"`
				Vectors        bool `json:"hasVectorIndex"`
				Collections    []struct {
					Name      string `json:"name"`
					Documents int    `json:"documents"`
				} `json:"collections"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &ans); err != nil {
		return Status{}, fmt.Errorf("qmd status: %w", err)
	}
	if ans.Error != nil {
		return Status{}, fmt.Errorf("qmd status: %s", ans.Error.Message)
	}
	if ans.Result == nil || ans.Result.IsError || ans.Result.Status == nil {
		return Status{}, errors.New("qmd status: no status in the answer")
	}
	st := ans.Result.Status
	out := Status{Total: st.Total, NeedsEmbedding: st.NeedsEmbedding, Vectors: st.Vectors}
	for _, col := range st.Collections {
		if col.Name == c.Collection {
			out.Documents = col.Documents
		}
	}
	return out, nil
}
