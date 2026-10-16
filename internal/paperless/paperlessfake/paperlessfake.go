// Package paperlessfake is a fake Paperless-ngx REST API for tests:
// "Authorization: Token …", GET /api/documents/ with query, id__in,
// page_size, page (and next), fields and ordering (-created, id),
// GET /api/correspondents/ and GET /api/tags/ with id__in. A query matches title and content, case-insensitively, the way
// a full-text hit would look.
package paperlessfake

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

// Doc is one stored document.
type Doc struct {
	ID            int
	Title         string
	Content       string
	Correspondent int    // 0: none
	Created       string // as the API answers it: "2025-03-14" or an RFC 3339 time
	Tags          []int  // tag ids, as the API lists them
	Modified      string // the last change, RFC 3339 as the API answers it; "": not said
}

// Server is the fake.
type Server struct {
	*httptest.Server
	Token string

	mu             sync.Mutex
	docs           []Doc
	correspondents map[int]string
	tags           map[int]string
	Requests       []*http.Request // every request, in order (headers and URL)

	// Status, when set, is every answer's status, with a body that echoes
	// the request's Authorization header, as a careless proxy might.
	Status int
	// Redirect, when set, answers every request with a 302 to it.
	Redirect string
	// FailPath, when set, answers that path (e.g. /api/tags/) with a 500.
	FailPath string
	// IgnorePageSize answers with every hit, as a server that does not
	// honour page_size would.
	IgnorePageSize bool
	// CountMore is added to the count a listing gives, as a listing that
	// loses documents on the way would.
	CountMore int
}

// New starts a fake that accepts token.
func New(token string) *Server {
	s := &Server{Token: token, correspondents: map[int]string{}, tags: map[int]string{}}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	return s
}

// Add stores documents.
func (s *Server) Add(docs ...Doc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.docs = append(s.docs, docs...)
}

// Remove deletes a document.
func (s *Server) Remove(id int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, d := range s.docs {
		if d.ID == id {
			s.docs = append(s.docs[:i], s.docs[i+1:]...)
			return
		}
	}
}

// Change replaces a stored document (by its ID), as an edit in Paperless.
func (s *Server) Change(d Doc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.docs {
		if s.docs[i].ID == d.ID {
			s.docs[i] = d
		}
	}
}

// Correspondent names a correspondent.
func (s *Server) Correspondent(id int, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.correspondents[id] = name
}

// Tag names a tag.
func (s *Server) Tag(id int, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tags[id] = name
}

// Seen returns the requests so far.
func (s *Server) Seen() []*http.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*http.Request(nil), s.Requests...)
}

// found says whether a full-text query finds d: its words joined by OR
// (Whoosh's syntax, which Paperless's search takes) find a document that
// holds any of them, anything else the whole query.
func found(d Doc, needle string) bool {
	hay := strings.ToLower(d.Title + " " + d.Content)
	for _, w := range strings.Split(needle, " or ") {
		if strings.Contains(hay, strings.TrimSpace(w)) {
			return true
		}
	}
	return false
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
	if s.FailPath != "" && r.URL.Path == s.FailPath {
		w.WriteHeader(http.StatusInternalServerError)
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
		var only map[int]bool
		if v := q.Get("id__in"); v != "" {
			only = map[int]bool{}
			for _, x := range strings.Split(v, ",") {
				id, _ := strconv.Atoi(x)
				only[id] = true
			}
		}
		var hits []Doc
		for _, d := range s.docs {
			if only != nil && !only[d.ID] {
				continue
			}
			if needle == "" || found(d, needle) {
				hits = append(hits, d)
			}
		}
		sort.SliceStable(hits, func(i, j int) bool { return hits[i].ID < hits[j].ID })
		if q.Get("ordering") == "-created" {
			sort.SliceStable(hits, func(i, j int) bool { return hits[i].Created > hits[j].Created })
		}
		fields := map[string]bool{}
		for _, f := range strings.Split(q.Get("fields"), ",") {
			if f != "" {
				fields[f] = true
			}
		}
		page, _ := strconv.Atoi(q.Get("page"))
		if page <= 0 {
			page = 1
		}
		from := (page - 1) * size
		if from > len(hits) || (page > 1 && from == len(hits)) {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"detail":"Ungültige Seite."}`))
			return
		}
		var next any
		if from+size < len(hits) {
			nq := r.URL.Query()
			nq.Set("page", strconv.Itoa(page+1))
			next = "http://" + r.Host + r.URL.Path + "?" + nq.Encode()
		}
		results := []map[string]any{}
		for i, d := range hits[from:] {
			if i == size {
				break
			}
			tags := append([]int{}, d.Tags...)
			content := d.Content
			if q.Get("truncate_content") == "true" && utf8.RuneCountInString(content) > 550 {
				content = string([]rune(content)[:550]) // as Paperless's serializer cuts it
			}
			m := map[string]any{"id": d.ID, "title": d.Title, "content": content, "created": d.Created, "correspondent": nil, "tags": tags, "modified": d.Modified}
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
		json.NewEncoder(w).Encode(map[string]any{"count": len(hits) + s.CountMore, "next": next, "previous": nil, "results": results})
	case "/api/correspondents/", "/api/tags/":
		names := s.correspondents
		if r.URL.Path == "/api/tags/" {
			names = s.tags
		}
		results := []map[string]any{}
		for _, x := range strings.Split(q.Get("id__in"), ",") {
			id, err := strconv.Atoi(x)
			if name, ok := names[id]; err == nil && ok {
				results = append(results, map[string]any{"id": id, "name": name})
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"count": len(results), "next": nil, "previous": nil, "results": results})
	default:
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"detail":"Nicht gefunden."}`))
	}
}
