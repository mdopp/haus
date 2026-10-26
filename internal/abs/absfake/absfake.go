// Package absfake is a fake Audiobookshelf for the tests and the demo:
// one podcast library, feeds it knows by address, subscriptions it keeps,
// and episodes a test adds as if downloaded.
package absfake

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"
)

// Server is the fake.
type Server struct {
	*httptest.Server
	Token string

	mu       sync.Mutex
	feeds    map[string]map[string]any // feed address → its podcast's metadata
	items    []map[string]any
	episodes []map[string]any
	Patches  []map[string]any // what PATCH …/media was sent
	Removed  []string
	Loaded   []string // the episodes asked to download, by title
	n        int
}

// New starts the fake with a podcast library at /podcasts.
func New(token string) *Server {
	s := &Server{Token: token, feeds: map[string]map[string]any{}}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	return s
}

// AddFeed makes a feed address readable, as its podcast.
func (s *Server) AddFeed(feed, title string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.feeds[feed] = map[string]any{"title": title, "author": "Autor", "feedUrl": feed, "description": "Über " + title}
}

// feedEpisodes are a feed's five episodes, newest last as feeds often
// have them: "Folge 1" (oldest) … "Folge 5".
func feedEpisodes() []any {
	var out []any
	for i := 1; i <= 5; i++ {
		out = append(out, map[string]any{"title": "Folge " + itoa(i), "publishedAt": int64(1_700_000_000_000 + i*86_400_000),
			"enclosure": map[string]string{"url": "https://example.org/" + itoa(i) + ".mp3", "type": "audio/mpeg"}})
	}
	return out
}

// Items are the subscribed podcasts' ids and titles.
func (s *Server) Items() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]string{}
	for _, it := range s.items {
		out[it["id"].(string)] = it["title"].(string)
	}
	return out
}

// Episode adds a downloaded episode to the podcast with that title.
func (s *Server) Episode(podcast, title, description string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, it := range s.items {
		if it["title"] == podcast {
			s.n++
			s.episodes = append([]map[string]any{{"id": "ep" + itoa(s.n), "libraryItemId": it["id"], "title": title, "description": description,
				"publishedAt": time.Now().UnixMilli(), "podcast": map[string]any{"metadata": map[string]any{"title": podcast}}}}, s.episodes...)
		}
	}
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+s.Token {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	answer := func(v any) { w.Header().Set("Content-Type", "application/json"); json.NewEncoder(w).Encode(v) }
	var in map[string]any
	var raw []byte
	if r.Body != nil {
		raw, _ = io.ReadAll(r.Body)
		json.Unmarshal(raw, &in)
	}
	switch {
	case r.Method == "GET" && r.URL.Path == "/api/libraries":
		answer(map[string]any{"libraries": []any{
			map[string]any{"id": "lib-books", "mediaType": "book", "folders": []any{map[string]any{"id": "f-books", "fullPath": "/audiobooks"}}},
			map[string]any{"id": "lib-pod", "mediaType": "podcast", "folders": []any{map[string]any{"id": "f-pod", "fullPath": "/podcasts"}}}}})
	case r.Method == "POST" && r.URL.Path == "/api/podcasts/feed":
		feed, _ := in["rssFeed"].(string)
		md, ok := s.feeds[feed]
		if !ok {
			http.Error(w, "Invalid podcast RSS feed", http.StatusNotFound)
			return
		}
		answer(map[string]any{"podcast": map[string]any{"metadata": md, "episodes": feedEpisodes()}})
	case r.Method == "POST" && r.URL.Path == "/api/podcasts":
		media, _ := in["media"].(map[string]any)
		md, _ := media["metadata"].(map[string]any)
		if in["libraryId"] != "lib-pod" || in["folderId"] != "f-pod" || !strings.HasPrefix(in["path"].(string), "/podcasts/") || media["autoDownloadEpisodes"] != true {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		s.n++
		id := "item" + itoa(s.n)
		s.items = append(s.items, map[string]any{"id": id, "title": md["title"], "feedUrl": md["feedUrl"], "path": in["path"]})
		answer(map[string]any{"id": id})
	case r.Method == "POST" && strings.HasPrefix(r.URL.Path, "/api/podcasts/") && strings.HasSuffix(r.URL.Path, "/download-episodes"):
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/podcasts/"), "/download-episodes")
		var eps []map[string]any
		if json.Unmarshal(raw, &eps) != nil || len(eps) == 0 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		known := false
		for _, it := range s.items {
			if it["id"] != id {
				continue
			}
			known = true
			for _, e := range eps {
				s.Loaded = append(s.Loaded, e["title"].(string))
				s.n++
				s.episodes = append([]map[string]any{{"id": "ep" + itoa(s.n), "libraryItemId": id, "title": e["title"], "publishedAt": e["publishedAt"],
					"podcast": map[string]any{"metadata": map[string]any{"title": it["title"]}}}}, s.episodes...)
			}
		}
		if !known {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/api/items/"):
		id := strings.TrimPrefix(r.URL.Path, "/api/items/")
		var eps []any
		for _, e := range s.episodes {
			if e["libraryItemId"] == id {
				eps = append(eps, map[string]any{"id": e["id"], "title": e["title"]})
			}
		}
		answer(map[string]any{"id": id, "media": map[string]any{"episodes": eps}})
	case r.Method == "PATCH" && strings.HasPrefix(r.URL.Path, "/api/items/") && strings.HasSuffix(r.URL.Path, "/media"):
		in["item"] = strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/items/"), "/media")
		s.Patches = append(s.Patches, in)
		answer(map[string]any{"updated": true})
	case r.Method == "DELETE" && strings.HasPrefix(r.URL.Path, "/api/items/"):
		id := strings.TrimPrefix(r.URL.Path, "/api/items/")
		s.Removed = append(s.Removed, id)
		var keep []map[string]any
		for _, it := range s.items {
			if it["id"] != id {
				keep = append(keep, it)
			}
		}
		s.items = keep
		answer(map[string]any{})
	case r.Method == "GET" && r.URL.Path == "/api/libraries/lib-pod/items":
		var res []any
		for _, it := range s.items {
			res = append(res, map[string]any{"id": it["id"], "media": map[string]any{"metadata": map[string]any{"title": it["title"], "feedUrl": it["feedUrl"]}}})
		}
		answer(map[string]any{"results": res})
	case r.Method == "GET" && r.URL.Path == "/api/libraries/lib-pod/recent-episodes":
		answer(map[string]any{"episodes": s.episodes})
	default:
		http.NotFound(w, r)
	}
}
