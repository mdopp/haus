// Package immichfake is a fake Immich people API for tests: "x-api-key",
// GET /api/people (withHidden, page, size, hasNextPage) and GET
// /api/people/<id>/thumbnail, which it counts.
package immichfake

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
)

type person struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Hidden    bool   `json:"isHidden"`
	Updated   string `json:"updatedAt"`
	gesicht   []byte
	abgerufen int
}

// Server is the fake.
type Server struct {
	*httptest.Server
	key    string
	mu     sync.Mutex
	people []*person
}

// New is a fake taking key.
func New(key string) *Server {
	s := &Server{key: key}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	return s
}

// Person adds or changes one; a nil face answers 404.
func (s *Server) Person(id, name, updated string, hidden bool, gesicht []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.people {
		if p.ID == id {
			p.Name, p.Updated, p.Hidden, p.gesicht = name, updated, hidden, gesicht
			return
		}
	}
	s.people = append(s.people, &person{ID: id, Name: name, Updated: updated, Hidden: hidden, gesicht: gesicht})
}

// Abgerufen is how often id's face was fetched.
func (s *Server) Abgerufen(id string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.people {
		if p.ID == id {
			return p.abgerufen
		}
	}
	return 0
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("x-api-key") != s.key {
		http.Error(w, `{"message":"Invalid API key"}`, http.StatusUnauthorized)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if id, ok := strings.CutSuffix(strings.TrimPrefix(r.URL.Path, "/api/people/"), "/thumbnail"); ok {
		for _, p := range s.people {
			if p.ID == id && p.gesicht != nil {
				p.abgerufen++
				w.Header().Set("Content-Type", "image/jpeg")
				w.Write(p.gesicht)
				return
			}
		}
		http.NotFound(w, r)
		return
	}
	if r.URL.Path != "/api/people" {
		http.NotFound(w, r)
		return
	}
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	size, _ := strconv.Atoi(q.Get("size"))
	page, size = max(page, 1), max(size, 1)
	var shown []*person
	for _, p := range s.people {
		if !p.Hidden || q.Get("withHidden") == "true" {
			shown = append(shown, p)
		}
	}
	from, to := min((page-1)*size, len(shown)), min(page*size, len(shown))
	json.NewEncoder(w).Encode(map[string]any{"total": len(shown), "hidden": 0, "people": shown[from:to], "hasNextPage": to < len(shown)})
}
