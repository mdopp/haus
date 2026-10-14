// Package paperlessfake is a fake Paperless-ngx REST API for tests:
// "Authorization: Token …", GET /api/documents/ with query, page_size and
// fields, GET /api/correspondents/ with id__in. A query matches title and
// content, case-insensitively, the way a full-text hit would look.
package paperlessfake

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Doc is one stored document.
type Doc struct {
	ID            int
	Title         string
	Content       string
	Correspondent int    // 0: none
	Created       string // as the API answers it: "2025-03-14" or an RFC 3339 time
}

// Server is the fake.
type Server struct {
	*httptest.Server
	Token string

	mu             sync.Mutex
	docs           []Doc
	correspondents map[int]string
	Requests       []*http.Request // every request, in order (headers and URL)

	// Status, when set, is every answer's status, with a body that echoes
	// the request's Authorization header, as a careless proxy might.
	Status int
	// Redirect, when set, answers every request with a 302 to it.
	Redirect string
	// IgnorePageSize answers with every hit, as a server that does not
	// honour page_size would.
	IgnorePageSize bool
}

// New starts a fake that accepts token.
func New(token string) *Server {
	s := &Server{Token: token, correspondents: map[int]string{}}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	return s
}

// Add stores documents.
func (s *Server) Add(docs ...Doc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.docs = append(s.docs, docs...)
}

// Correspondent names a correspondent.
func (s *Server) Correspondent(id int, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.correspondents[id] = name
}

// Seen returns the requests so far.
func (s *Server) Seen() []*http.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*http.Request(nil), s.Requests...)
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Requests = append(s.Requests, r.Clone(r.Context()))
	if s.Redirect != "" {
		http.Redirect(w, r, s.Redirect, http.StatusFound)
		return
	}
	if s.Status != 0 {
		w.WriteHeader(s.Status)
		w.Write([]byte(`{"detail":"broken, you sent ` + r.Header.Get("Authorization") + `"}`))
		return
	}
	if r.Header.Get("Authorization") != "Token "+s.Token {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"detail":"Ungültiges Token."}`))
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query()
	size, _ := strconv.Atoi(q.Get("page_size"))
	if size <= 0 {
		size = 25
	}
	if s.IgnorePageSize {
		size = 1 << 30
	}
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/api/documents/":
		needle := strings.ToLower(q.Get("query"))
		var hits []Doc
		for _, d := range s.docs {
			if needle == "" || strings.Contains(strings.ToLower(d.Title+" "+d.Content), needle) {
				hits = append(hits, d)
			}
		}
		sort.SliceStable(hits, func(i, j int) bool { return hits[i].ID < hits[j].ID })
		fields := map[string]bool{}
		for _, f := range strings.Split(q.Get("fields"), ",") {
			if f != "" {
				fields[f] = true
			}
		}
		results := []map[string]any{}
		for i, d := range hits {
			if i == size {
				break
			}
			m := map[string]any{"id": d.ID, "title": d.Title, "content": d.Content, "created": d.Created, "correspondent": nil}
			if d.Correspondent != 0 {
				m["correspondent"] = d.Correspondent
			}
			if len(fields) > 0 {
				for k := range m {
					if !fields[k] {
						delete(m, k)
					}
				}
			}
			results = append(results, m)
		}
		json.NewEncoder(w).Encode(map[string]any{"count": len(hits), "next": nil, "previous": nil, "results": results})
	case "/api/correspondents/":
		results := []map[string]any{}
		for _, x := range strings.Split(q.Get("id__in"), ",") {
			id, err := strconv.Atoi(x)
			if name, ok := s.correspondents[id]; err == nil && ok {
				results = append(results, map[string]any{"id": id, "name": name})
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"count": len(results), "next": nil, "previous": nil, "results": results})
	default:
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"detail":"Nicht gefunden."}`))
	}
}
