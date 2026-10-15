// Package qmdfake is a fake qmd 2.8.3 HTTP server for tests (haus#13),
// answering in the shapes captured from a real `qmd mcp --http` (the
// captures are internal/qmd/testdata): GET /health, POST /query with typed
// searches, and POST /mcp, where only the status tool is served, as
// stateless streamable HTTP (an SSE "event: message" frame). Like qmd it
// refuses a Host that is not loopback (403), a body without searches (400),
// a body that is not JSON (500), and an /mcp call that does not accept both
// JSON and SSE (406).
//
// A document is found when a word of the query is in its body or in its
// Means words: the fake's stand-in for search by meaning, so a test can
// find a note by a word it does not contain. The order is the order the
// documents were added; the score is qmd's fused rank, 1/rank.
package qmdfake

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Doc is one indexed document.
type Doc struct {
	Path  string   // relative to the collection, as the share has it
	Title string   // qmd's title: the first heading, else the file name
	Body  string   // the file's text
	Means []string // words it is found by without containing them
}

// Request is one request the fake saw.
type Request struct {
	Method, Path, Host, Accept string
	Body                       []byte
}

// Server is the fake.
type Server struct {
	*httptest.Server
	Collection string // the notes' collection name

	mu   sync.Mutex
	docs []Doc
	seen []Request

	// Indexing answers status as qmd does before the first `qmd embed`:
	// documents known, no vector index.
	Indexing bool
	// Status, when set, is every answer's status, with qmd's plain body.
	Status int
	// Results, when set, is /query's answer verbatim.
	Results string
	// Hang, when set, holds every /query and /mcp answer that long.
	Hang time.Duration
}

// New starts a fake whose index holds the collection.
func New(collection string) *Server {
	s := &Server{Collection: collection}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	return s
}

// Add indexes documents.
func (s *Server) Add(docs ...Doc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.docs = append(s.docs, docs...)
}

// Seen returns the requests so far.
func (s *Server) Seen() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Request(nil), s.seen...)
}

func loopback(hostport string) bool {
	h, _, err := net.SplitHostPort(hostport)
	if err != nil {
		h = hostport
	}
	h = strings.Trim(h, "[]")
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	s.seen = append(s.seen, Request{Method: r.Method, Path: r.URL.Path, Host: r.Host, Accept: r.Header.Get("Accept"), Body: body})
	hang, status := s.Hang, s.Status
	s.mu.Unlock()
	if !loopback(r.Host) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprintf(w, `{"jsonrpc":"2.0","error":{"code":-32003,"message":"Forbidden: Host not allowed: %s"},"id":null}`, r.Host)
		return
	}
	if r.URL.Path == "/health" && r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok","uptime":42}`))
		return
	}
	if hang > 0 {
		select {
		case <-time.After(hang):
		case <-r.Context().Done():
			return
		}
	}
	if status != 0 {
		w.WriteHeader(status)
		w.Write([]byte("Internal Server Error"))
		return
	}
	switch {
	case (r.URL.Path == "/query" || r.URL.Path == "/search") && r.Method == http.MethodPost:
		s.query(w, body)
	case r.URL.Path == "/mcp":
		s.mcp(w, r, body)
	default:
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("Not Found"))
	}
}

type search struct {
	Type  string `json:"type"`
	Query string `json:"query"`
}

type result struct {
	Docid   string  `json:"docid"`
	File    string  `json:"file"`
	Title   string  `json:"title"`
	Score   float64 `json:"score"`
	Context *string `json:"context"`
	Line    int     `json:"line"`
	Snippet string  `json:"snippet"`
}

// encode is qmd's encodeQmdPath: each segment percent-encoded as
// JavaScript's encodeURIComponent does, the slashes kept.
func encode(p string) string {
	parts := strings.Split(p, "/")
	for i, seg := range parts {
		e := url.QueryEscape(seg)
		e = strings.ReplaceAll(e, "+", "%20")
		for _, keep := range []string{"!", "'", "(", ")", "*"} {
			e = strings.ReplaceAll(e, url.QueryEscape(keep), keep)
		}
		parts[i] = e
	}
	return strings.Join(parts, "/")
}

// snippet is qmd's: a diff-like header naming the lines shown, then each
// line numbered, the header's number one less than the first line's.
func snippet(body string) (int, string) {
	lines := strings.Split(body, "\n")
	n := min(len(lines), 3)
	var b strings.Builder
	fmt.Fprintf(&b, "1: @@ -1,%d @@ (0 before, %d after)", n, len(lines)-n)
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "\n%d: %s", i+2, lines[i])
	}
	return 1, b.String()
}

func (s *Server) query(w http.ResponseWriter, body []byte) {
	var p struct {
		Searches    []search `json:"searches"`
		Limit       *float64 `json:"limit"`
		Collections []string `json:"collections"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Internal Server Error"))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if p.Searches == nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"Missing required field: searches (array)"}`))
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Results != "" {
		w.Write([]byte(s.Results))
		return
	}
	limit := 10
	if p.Limit != nil {
		limit = int(*p.Limit)
	}
	inColl := p.Collections == nil
	for _, c := range p.Collections {
		inColl = inColl || c == s.Collection
	}
	out := []result{}
	for _, d := range s.docs {
		if !inColl || len(out) >= limit || !matches(d, p.Searches) {
			continue
		}
		sum := sha256.Sum256([]byte(d.Body))
		line, snip := snippet(d.Body)
		out = append(out, result{Docid: "#" + hex.EncodeToString(sum[:3]), File: "qmd://" + s.Collection + "/" + encode(d.Path),
			Title: d.Title, Score: math.Round(100/float64(len(out)+1)) / 100, Line: line, Snippet: snip})
	}
	json.NewEncoder(w).Encode(map[string]any{"results": out})
}

func matches(d Doc, searches []search) bool {
	hay := strings.ToLower(d.Body + " " + strings.Join(d.Means, " "))
	for _, q := range searches {
		for _, word := range strings.Fields(strings.ToLower(q.Query)) {
			if strings.Contains(hay, word) {
				return true
			}
		}
	}
	return false
}

func (s *Server) mcp(w http.ResponseWriter, r *http.Request, body []byte) {
	acc := r.Header.Get("Accept")
	if r.Method != http.MethodPost || !strings.Contains(acc, "application/json") || !strings.Contains(acc, "text/event-stream") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotAcceptable)
		w.Write([]byte(`{"jsonrpc":"2.0","error":{"code":-32000,"message":"Not Acceptable: Client must accept both application/json and text/event-stream"},"id":null}`))
		return
	}
	var req struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params struct {
			Name string `json:"name"`
		} `json:"params"`
	}
	json.Unmarshal(body, &req)
	msg := map[string]any{"jsonrpc": "2.0", "id": req.ID}
	if req.Method == "tools/call" && req.Params.Name == "status" {
		s.mu.Lock()
		n, need := len(s.docs), 0
		if s.Indexing {
			need = n
		}
		msg["result"] = map[string]any{
			"content": []map[string]any{{"type": "text", "text": fmt.Sprintf("QMD Index Status:\n  Total documents: %d\n  Needs embedding: %d", n, need)}},
			"structuredContent": map[string]any{"totalDocuments": n, "needsEmbedding": need, "hasVectorIndex": !s.Indexing,
				"collections": []map[string]any{{"name": s.Collection, "path": "/notes", "pattern": "**/*.md", "documents": n,
					"lastUpdated": "2026-10-15T09:05:23.566Z"}}},
		}
		s.mu.Unlock()
	} else {
		msg["error"] = map[string]any{"code": -32601, "message": "the fake serves only the status tool"}
	}
	b, _ := json.Marshal(msg)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	fmt.Fprintf(w, "event: message\ndata: %s\n\n", b)
}
