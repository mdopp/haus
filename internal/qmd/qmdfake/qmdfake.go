// Package qmdfake is a fake of the Vermittler (laib#198), the small
// server in front of qmd 2.8.3, for tests (haus#13). It answers in the
// shapes captured from the prototype (internal/qmd/testdata): GET /health,
// GET /status with each collection's documents and how many have their
// vectors, and POST /query with typed searches — qmd's answer, plus vec,
// each hit's cosine to the question. Like qmd it refuses a Host that is
// not loopback (403), a body without searches (400) and a body that is not
// JSON (500).
//
// A document is found when a word of the query is in its body or in its
// Means words: the fake's stand-in for search by meaning, so a test can
// find a note by a word it does not contain. The order is the order the
// documents were added; the score is qmd's fused rank, 1/rank; vec is the
// document's Vec. Like the Vermittler it refuses with 400 a field it does
// not take (minScore, intent, a plain query), rerank on, and a search that
// is not lex or vec.
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
	"unicode"
)

// Doc is one indexed document.
type Doc struct {
	// Collection is the document's collection; "": the server's (the notes').
	Collection string
	Path       string   // relative to the collection, as the share has it
	Title      string   // qmd's title: the first heading, else the file name
	Body       string   // the file's text
	Means      []string // words it is found by without containing them
	// Vec is the cosine the fake answers for it; 0 is DefaultVec, a
	// clear match. A hit outside the vector list (vec 0) is a Results test.
	Vec float64
}

// DefaultVec is a Doc's cosine when it sets none: well above the cut
// (haus#13), as a note that answers the question has.
const DefaultVec = 0.5

// Request is one request the fake saw.
type Request struct {
	Method, Path, Host, Accept string
	Body                       []byte
}

// Server is the fake.
type Server struct {
	*httptest.Server
	Collection string // the notes' collection name; a Doc may name another

	mu   sync.Mutex
	docs []Doc
	seen []Request

	// Indexing answers status as before the first `qmd embed`: documents
	// known, none with its vectors.
	Indexing bool
	// Status, when set, is every answer's status, with qmd's plain body.
	Status int
	// Results, when set, is /query's answer verbatim.
	Results string
	// Hang, when set, holds every /query and /status answer that long.
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
	case r.URL.Path == "/query" && r.Method == http.MethodPost:
		s.query(w, body)
	case r.URL.Path == "/status" && r.Method == http.MethodGet:
		s.status(w)
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
	Vec     float64 `json:"vec"`
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

// taken is what the Vermittler's POST /query takes (laib qmd/vermittler.mjs).
var taken = map[string]bool{"searches": true, "limit": true, "collections": true, "rerank": true}

// refused says why the Vermittler would answer 400, or "".
func refused(body []byte) string {
	var raw map[string]json.RawMessage
	if json.Unmarshal(body, &raw) != nil {
		return ""
	}
	for k := range raw {
		if !taken[k] {
			return k + " is not taken: typed lex/vec searches only, no plain or expanded query"
		}
	}
	if r, ok := raw["rerank"]; ok && string(r) != "false" {
		return "rerank is always off"
	}
	var p struct {
		Searches []search `json:"searches"`
	}
	json.Unmarshal(body, &p)
	for _, q := range p.Searches {
		if (q.Type != "lex" && q.Type != "vec") || strings.TrimSpace(q.Query) == "" {
			return `each search is {type: "lex"|"vec", query}`
		}
	}
	return ""
}

func (s *Server) query(w http.ResponseWriter, body []byte) {
	if why := refused(body); why != "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": why})
		return
	}
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
	asked := map[string]bool{}
	for _, c := range p.Collections {
		asked[c] = true
	}
	out := []result{}
	for _, d := range s.docs {
		coll := s.coll(d)
		if (p.Collections != nil && !asked[coll]) || len(out) >= limit || !matches(d, p.Searches) {
			continue
		}
		sum := sha256.Sum256([]byte(d.Body))
		line, snip := snippet(d.Body)
		vec := d.Vec
		if vec == 0 {
			vec = DefaultVec
		}
		out = append(out, result{Docid: "#" + hex.EncodeToString(sum[:3]), File: "qmd://" + coll + "/" + encode(d.Path),
			Title: d.Title, Score: math.Round(100/float64(len(out)+1)) / 100, Line: line, Snippet: snip, Vec: vec})
	}
	json.NewEncoder(w).Encode(map[string]any{"results": out})
}

func matches(d Doc, searches []search) bool {
	hay := strings.ToLower(d.Body + " " + strings.Join(d.Means, " "))
	for _, q := range searches {
		words := strings.FieldsFunc(strings.ToLower(q.Query), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
		for _, word := range words {
			if strings.Contains(hay, word) {
				return true
			}
		}
	}
	return false
}

func (s *Server) coll(d Doc) string {
	if d.Collection != "" {
		return d.Collection
	}
	return s.Collection
}

// status is the Vermittler's GET /status: each collection, its documents
// and how many of them have their vectors.
func (s *Server) status(w http.ResponseWriter) {
	s.mu.Lock()
	names := []string{s.Collection}
	count := map[string]int{s.Collection: 0}
	for _, d := range s.docs {
		c := s.coll(d)
		if _, ok := count[c]; !ok {
			names = append(names, c)
		}
		count[c]++
	}
	cols := []map[string]any{}
	for _, n := range names {
		embedded := count[n]
		if s.Indexing {
			embedded = 0
		}
		cols = append(cols, map[string]any{"name": n, "documents": count[n], "embedded": embedded})
	}
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"collections": cols})
}
