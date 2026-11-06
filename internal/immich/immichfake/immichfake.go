// Package immichfake is a fake Immich people API for tests: "x-api-key"
// with its permissions, GET /api/people (withHidden, page, size,
// hasNextPage; as Immich, only people with a face), GET
// /api/people/<id>/thumbnail, which it counts, POST /api/people, PUT
// /api/people/<id>, GET /api/search/person (faceless and hidden ones too)
// and GET /api/users/me.
package immichfake

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
)

type person struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Birth     string `json:"birthDate,omitempty"`
	Hidden    bool   `json:"isHidden"`
	Updated   string `json:"updatedAt"`
	gesicht   []byte
	abgerufen int
	ohneFoto  bool // made by POST: no face yet
}

// Server is the fake.
type Server struct {
	*httptest.Server
	key string
	// Email is the key's account; Ohne are the permissions it lacks.
	Email  string
	Ohne   []string
	mu     sync.Mutex
	people []*person
	writes int
}

// New is a fake taking key, with every permission.
func New(key string) *Server {
	s := &Server{key: key, Email: "anna@box.test"}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	return s
}

// Person adds or changes one with its face; a nil face answers 404.
func (s *Server) Person(id, name, updated string, hidden bool, gesicht []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p := s.find(id); p != nil {
		p.Name, p.Updated, p.Hidden, p.gesicht, p.ohneFoto = name, updated, hidden, gesicht, false
		return
	}
	s.people = append(s.people, &person{ID: id, Name: name, Updated: updated, Hidden: hidden, gesicht: gesicht})
}

// Alle are every person as "name|birthday", faceless ones too, in order.
func (s *Server) Alle() (out []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.people {
		out = append(out, p.Name+"|"+p.Birth)
	}
	return out
}

// ID is the id of the person named name; "" for none.
func (s *Server) ID(name string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.people {
		if p.Name == name {
			return p.ID
		}
	}
	return ""
}

// Writes is how many POSTs and PUTs were taken.
func (s *Server) Writes() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writes
}

// Abgerufen is how often id's face was fetched.
func (s *Server) Abgerufen(id string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p := s.find(id); p != nil {
		return p.abgerufen
	}
	return 0
}

func (s *Server) find(id string) *person {
	for _, p := range s.people {
		if p.ID == id {
			return p
		}
	}
	return nil
}

// darf answers 403 as Immich's guard does when the key lacks recht.
func (s *Server) darf(w http.ResponseWriter, recht string) bool {
	if !slices.Contains(s.Ohne, recht) {
		return true
	}
	w.WriteHeader(http.StatusForbidden)
	json.NewEncoder(w).Encode(map[string]any{"message": "Missing required permission: " + recht})
	return false
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("x-api-key") != s.key {
		http.Error(w, `{"message":"Invalid API key"}`, http.StatusUnauthorized)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id, isPerson := strings.CutPrefix(r.URL.Path, "/api/people/")
	switch {
	case r.URL.Path == "/api/users/me":
		if s.darf(w, "user.read") {
			json.NewEncoder(w).Encode(map[string]any{"id": "u1", "email": s.Email})
		}
	case r.URL.Path == "/api/search/person":
		if s.darf(w, "person.read") {
			s.search(w, r)
		}
	case r.URL.Path == "/api/people" && r.Method == http.MethodPost:
		if s.darf(w, "person.create") {
			p := &person{ID: fmt.Sprint("neu-", len(s.people)), Updated: "1", ohneFoto: true}
			s.people = append(s.people, p)
			s.write(w, r, p, http.StatusCreated)
		}
	case isPerson && r.Method == http.MethodPut:
		if p := s.find(id); p == nil {
			http.Error(w, `{"message":"Not found or no person.update access"}`, http.StatusBadRequest)
		} else if s.darf(w, "person.update") {
			s.write(w, r, p, http.StatusOK)
		}
	case strings.HasSuffix(id, "/thumbnail"):
		if p := s.find(strings.TrimSuffix(id, "/thumbnail")); p != nil && p.gesicht != nil {
			p.abgerufen++
			w.Header().Set("Content-Type", "image/jpeg")
			w.Write(p.gesicht)
			return
		}
		http.NotFound(w, r)
	case r.URL.Path == "/api/people":
		s.list(w, r)
	default:
		http.NotFound(w, r)
	}
}

// write takes name and birthDate (null: none) as Immich's PersonCreate/UpdateDto.
func (s *Server) write(w http.ResponseWriter, r *http.Request, p *person, code int) {
	var body struct {
		Name  *string `json:"name"`
		Birth *string `json:"birthDate"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	if body.Name != nil {
		p.Name = *body.Name
	}
	p.Birth = ""
	if body.Birth != nil {
		p.Birth = *body.Birth
	}
	s.writes++
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(p)
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	q := strings.ToLower(r.URL.Query().Get("name"))
	out := []*person{}
	for _, p := range s.people {
		if q != "" && strings.Contains(strings.ToLower(p.Name), q) && (!p.Hidden || r.URL.Query().Get("withHidden") == "true") {
			out = append(out, p)
		}
	}
	json.NewEncoder(w).Encode(out)
}

func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	size, _ := strconv.Atoi(q.Get("size"))
	page, size = max(page, 1), max(size, 1)
	var shown []*person
	for _, p := range s.people {
		if !p.ohneFoto && (!p.Hidden || q.Get("withHidden") == "true") {
			shown = append(shown, p)
		}
	}
	from, to := min((page-1)*size, len(shown)), min(page*size, len(shown))
	json.NewEncoder(w).Encode(map[string]any{"total": len(shown), "hidden": 0, "people": shown[from:to], "hasNextPage": to < len(shown)})
}
