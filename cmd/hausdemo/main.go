// Command hausdemo serves Haus's family pages against fakes: for the
// browser tests (e2e/) and to look at a page without a box. Home
// Assistant is hafake with a few lamps, a switch, a cover and a player,
// whose calls change their state as HA's do; the notes and the
// favourites are a temp folder. Every request is anna, in family and
// admins. Never for a box: there is no login and nothing is real.
//
// /demo/fehler?status=503 makes every switch fail (0: work again);
// /demo/setze?id=light.flur&state=on changes a state as if someone
// switched it elsewhere.
package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/mdopp/haus/internal/cards"
	"github.com/mdopp/haus/internal/favoriten"
	"github.com/mdopp/haus/internal/ha"
	"github.com/mdopp/haus/internal/ha/hafake"
	"github.com/mdopp/haus/internal/notes"
	"github.com/mdopp/haus/internal/pages"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8651", "where to listen")
	flag.Parse()

	fake := hafake.New("demo")
	defer fake.Close()
	fake.ApplyCalls(true)
	fake.SetRegistries(
		[]map[string]any{{"area_id": "flur", "name": "Flur"}, {"area_id": "kueche", "name": "Küche"}, {"area_id": "wohnzimmer", "name": "Wohnzimmer"}},
		nil,
		[]map[string]any{
			{"entity_id": "light.flur", "area_id": "flur"},
			{"entity_id": "light.kueche", "area_id": "kueche"},
			{"entity_id": "switch.kaffee", "area_id": "kueche"},
			{"entity_id": "cover.rollo", "area_id": "wohnzimmer"},
			{"entity_id": "media_player.wz", "area_id": "wohnzimmer"},
		})
	fake.Set("light.flur", "off", map[string]any{"friendly_name": "Flurlicht"})
	fake.Set("light.kueche", "on", map[string]any{"friendly_name": "Küchenlicht", "brightness": 153, "supported_color_modes": []string{"brightness"}, "color_mode": "brightness"})
	fake.Set("switch.kaffee", "off", map[string]any{"friendly_name": "Kaffeemaschine"})
	fake.Set("cover.rollo", "open", map[string]any{"friendly_name": "Rollo", "device_class": "shutter", "current_position": 60, "supported_features": 15.0})
	fake.Set("media_player.wz", "playing", map[string]any{"friendly_name": "Wohnzimmer", "media_title": "Abbey Road", "media_artist": "The Beatles",
		"volume_level": 0.3, "supported_features": 1.0 + 4 + 1024 + 16384})

	dir, err := os.MkdirTemp("", "hausdemo-")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)
	notesDir := filepath.Join(dir, "notes")
	os.MkdirAll(notesDir, 0o755)
	os.WriteFile(filepath.Join(notesDir, "einkaufsliste.md"), []byte("# Einkaufsliste\n\nMilch\nBrot\n"), 0o644)
	nd, err := notes.Open(notesDir)
	if err != nil {
		log.Fatal(err)
	}

	cfg := pages.StartConfig{HA: &ha.Client{URL: fake.URL, Token: "demo"}, Notes: nd, Favorites: favoriten.Open(dir)}
	mux := http.NewServeMux()
	mux.HandleFunc("/start", pages.Start(cfg))
	mux.HandleFunc("/raeume", pages.Raeume(cfg))
	mux.HandleFunc(cards.ThingPages, pages.Ding(cfg))
	mux.HandleFunc(cards.SwitchPath, pages.Schalten(cfg))
	mux.HandleFunc("/medien", pages.Medien(cfg))
	mux.HandleFunc("/notizen", pages.Notizen(cfg))
	mux.HandleFunc(cards.NotePages, pages.Notizen(cfg))
	mux.HandleFunc("/mehr", pages.Mehr(cfg))
	mux.Handle(pages.LivePath, &pages.Live{HA: cfg.HA})
	mux.HandleFunc("/demo/fehler", func(w http.ResponseWriter, r *http.Request) {
		n, _ := strconv.Atoi(r.URL.Query().Get("status"))
		fake.FailCalls(n)
	})
	mux.HandleFunc("/demo/setze", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		fake.Change(q.Get("id"), q.Get("state"), map[string]any{"friendly_name": q.Get("name")})
	})
	anna := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("Remote-User", "anna")
		r.Header.Set("Remote-Groups", "family,admins")
		mux.ServeHTTP(w, r)
	})
	srv := &http.Server{Addr: *addr, Handler: anna, ReadHeaderTimeout: 5 * time.Second}
	log.Printf("hausdemo: http://%s/start (fakes only, not for a box)", *addr)
	log.Fatal(srv.ListenAndServe())
}
