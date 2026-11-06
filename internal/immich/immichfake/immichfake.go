// Package immichfake is a fake Immich people API for tests: "x-api-key"
// with its permissions, GET /api/people (withHidden, page, size,
// hasNextPage, closestPersonId; as Immich, only people with a face, the
// favourites first, then by closeness, then the named by photos), GET
// /api/people/<id>/thumbnail, which it counts, …/statistics, POST
// /api/people, PUT /api/people/<id>, POST /api/people/merge, GET
// /api/search/person (faceless and hidden ones too), GET /api/users/me,
// and the photos Homes uploads: POST /api/assets (multipart, a duplicate
// for bytes it has), GET /api/faces?id= (the faces Erkennen gives the
// photo) and DELETE /api/assets.
package immichfake

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"math"
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
	Favorit   bool   `json:"isFavorite"`
	gesicht   []byte
	abgerufen int
	ohneFoto  bool       // made by POST: no face yet
	fotos     int        // photos it is in
	lage      [2]float64 // where its face is: closeness is the distance
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
	merges [][]string
	// Erkennen are the faces Immich finds in an uploaded photo: each the
	// id of the person it gives the face, "" none; nil finds no face.
	Erkennen func(name string, jpeg []byte) []string
	assets   []*asset
	hoch     int // uploads taken
}

// asset is a photo Homes uploaded.
type asset struct {
	ID, Name, Visibility string
	jpeg                 []byte
}

// Fotos are the uploaded photos still there, as "name|visibility".
func (s *Server) Fotos() (out []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range s.assets {
		out = append(out, a.Name+"|"+a.Visibility)
	}
	return out
}

// Lage sets id's photos, its face's place (the closest by distance) and
// whether it is a favourite.
func (s *Server) Lage(id string, fotos int, x, y float64, favorit bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p := s.find(id); p != nil {
		p.fotos, p.lage, p.Favorit = fotos, [2]float64{x, y}, favorit
	}
}

// Merges are the merges taken, each its ids in order.
func (s *Server) Merges() [][]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.merges)
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
	case r.URL.Path == "/api/api-keys/me":
		rechte := []string{"all"}
		if len(s.Ohne) > 0 {
			rechte = slices.DeleteFunc([]string{"person.read", "person.create", "person.update", "person.merge", "person.statistics", "user.read", "asset.upload", "face.read", "asset.delete"}, func(r string) bool { return slices.Contains(s.Ohne, r) })
		}
		json.NewEncoder(w).Encode(map[string]any{"id": "k1", "name": "Homes", "permissions": rechte})
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
	case r.URL.Path == "/api/people/merge" && r.Method == http.MethodPost:
		if s.darf(w, "person.merge") {
			s.merge(w, r)
		}
	case strings.HasSuffix(id, "/statistics"):
		if p := s.find(strings.TrimSuffix(id, "/statistics")); p == nil {
			http.Error(w, `{"message":"Not found"}`, http.StatusBadRequest)
		} else if s.darf(w, "person.statistics") {
			json.NewEncoder(w).Encode(map[string]int{"assets": p.fotos})
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
	case r.URL.Path == "/api/assets" && r.Method == http.MethodPost:
		if s.darf(w, "asset.upload") {
			s.upload(w, r)
		}
	case r.URL.Path == "/api/assets" && r.Method == http.MethodDelete:
		if s.darf(w, "asset.delete") {
			var body struct{ IDs []string }
			json.NewDecoder(r.Body).Decode(&body)
			s.assets = slices.DeleteFunc(s.assets, func(a *asset) bool { return slices.Contains(body.IDs, a.ID) })
			w.WriteHeader(http.StatusNoContent)
		}
	case r.URL.Path == "/api/faces" && r.Method == http.MethodGet:
		if s.darf(w, "face.read") {
			s.faces(w, r.URL.Query().Get("id"))
		}
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

// merge is Immich's mergePeople: the first stays, takes the others'
// photos, and their name and birthday where it has none; they go.
func (s *Server) merge(w http.ResponseWriter, r *http.Request) {
	var body struct{ IDs []string }
	json.NewDecoder(r.Body).Decode(&body)
	if len(body.IDs) < 2 || s.find(body.IDs[0]) == nil {
		http.Error(w, `{"message":"At least two people are required for merging"}`, http.StatusBadRequest)
		return
	}
	s.merges = append(s.merges, body.IDs)
	ziel, out := s.find(body.IDs[0]), []map[string]any{{"id": body.IDs[0], "success": true}}
	for _, id := range body.IDs[1:] {
		p := s.find(id)
		if p == nil {
			out = append(out, map[string]any{"id": id, "success": false, "error": "not_found"})
			continue
		}
		if ziel.Name == "" {
			ziel.Name = p.Name
		}
		if ziel.Birth == "" {
			ziel.Birth = p.Birth
		}
		ziel.fotos += p.fotos
		if ziel.ohneFoto && !p.ohneFoto {
			ziel.gesicht, ziel.ohneFoto, ziel.lage = p.gesicht, false, p.lage
		}
		s.people = slices.DeleteFunc(s.people, func(q *person) bool { return q == p })
		out = append(out, map[string]any{"id": id, "success": true})
	}
	json.NewEncoder(w).Encode(out)
}

// upload takes a photo as Immich's POST /api/assets: 201 created, or 200
// duplicate with the id it has for those bytes.
func (s *Server) upload(w http.ResponseWriter, r *http.Request) {
	f, _, err := r.FormFile("assetData")
	if err != nil || r.FormValue("fileCreatedAt") == "" {
		http.Error(w, `{"message":"assetData and fileCreatedAt are required"}`, http.StatusBadRequest)
		return
	}
	var b bytes.Buffer
	b.ReadFrom(f)
	for _, a := range s.assets {
		if bytes.Equal(a.jpeg, b.Bytes()) {
			json.NewEncoder(w).Encode(map[string]string{"id": a.ID, "status": "duplicate"})
			return
		}
	}
	s.hoch++
	a := &asset{ID: fmt.Sprint("a-", s.hoch), Name: r.FormValue("filename"), Visibility: cmp.Or(r.FormValue("visibility"), "timeline"), jpeg: b.Bytes()}
	s.assets, s.writes = append(s.assets, a), s.writes+1
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"id": a.ID, "status": "created"})
}

// faces are the faces Erkennen finds in the photo, each with its person.
func (s *Server) faces(w http.ResponseWriter, id string) {
	i := slices.IndexFunc(s.assets, func(a *asset) bool { return a.ID == id })
	if i < 0 {
		http.Error(w, `{"message":"Not found or no asset.read access"}`, http.StatusBadRequest)
		return
	}
	out := []map[string]any{}
	if s.Erkennen != nil {
		for n, pid := range s.Erkennen(s.assets[i].Name, s.assets[i].jpeg) {
			var p any
			if q := s.find(pid); q != nil {
				p = q
			}
			out = append(out, map[string]any{"id": fmt.Sprint(id, "-f", n), "person": p})
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
	nah := func(p *person) float64 { return 0 }
	if c := s.find(q.Get("closestPersonId")); c != nil {
		nah = func(p *person) float64 { return math.Hypot(p.lage[0]-c.lage[0], p.lage[1]-c.lage[1]) }
	}
	slices.SortStableFunc(shown, func(a, b *person) int {
		return cmp.Or(-cmp.Compare(btoi(a.Favorit), btoi(b.Favorit)), cmp.Compare(nah(a), nah(b)),
			cmp.Compare(btoi(a.Name == ""), btoi(b.Name == "")), -cmp.Compare(a.fotos, b.fotos))
	})
	from, to := min((page-1)*size, len(shown)), min(page*size, len(shown))
	json.NewEncoder(w).Encode(map[string]any{"total": len(shown), "hidden": 0, "people": shown[from:to], "hasNextPage": to < len(shown)})
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}
