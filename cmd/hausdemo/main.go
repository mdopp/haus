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
	"context"
	"encoding/json"
	"flag"
	"github.com/mdopp/haus/internal/discogs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/mdopp/haus/internal/abs"
	"github.com/mdopp/haus/internal/abs/absfake"
	"github.com/mdopp/haus/internal/ansicht"
	"github.com/mdopp/haus/internal/caldav"
	"github.com/mdopp/haus/internal/caldav/davfake"
	"github.com/mdopp/haus/internal/cards"
	"github.com/mdopp/haus/internal/einkauf"
	"github.com/mdopp/haus/internal/favoriten"
	"github.com/mdopp/haus/internal/ha"
	"github.com/mdopp/haus/internal/ha/hafake"
	"github.com/mdopp/haus/internal/merken"
	"github.com/mdopp/haus/internal/notes"
	"github.com/mdopp/haus/internal/pages"
	"github.com/mdopp/haus/internal/podcasts"
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
	power := func(name string) map[string]any {
		return map[string]any{"friendly_name": name, "device_class": "power", "unit_of_measurement": "W"}
	}
	fake.Set("sensor.pv", "3200", power("Aktuell erzeugter PV-Strom"))
	fake.Set("sensor.haus", "800", power("Aktueller Hausverbrauch"))
	fake.Set("sensor.netz", "-1600", power("Aktuelle Netz Leistung"))
	fake.Set("sensor.akku", "800", power("Aktuelle Akku Leistung"))
	fake.Set("sensor.akku_soc", "74", map[string]any{"friendly_name": "Akku-Füllstand in Prozent", "device_class": "battery", "unit_of_measurement": "%"})
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
	os.MkdirAll(filepath.Join(notesDir, "rezepte"), 0o755)
	os.WriteFile(filepath.Join(notesDir, "rezepte", "lasagne.md"), []byte("---\nart: rezept\ntags: [kochen]\n---\n# Lasagne\n\n*Für 4 Portionen · 1 Std.*\n\n"+
		"## Zutaten\n\n- 500 g Hackfleisch\n- 2 Zwiebeln\n- 1 Dose Tomaten\n- 250 g Lasagneplatten\n\n## Zubereitung\n\n1. Soße kochen.\n2. Schichten und backen.\n"), 0o644)
	nd, err := notes.Open(notesDir)
	if err != nil {
		log.Fatal(err)
	}

	inbox := filepath.Join(dir, "musik-eingang")
	os.MkdirAll(inbox, 0o755)
	dav := davfake.New("haus", "demo")
	defer dav.Close()
	dav.AddCalendar("/haus/haushalt/", "Haushalt")
	dav.AddPrincipal("anna")
	absFake := absfake.New("demo")
	defer absFake.Close()
	pods := podcasts.Open(&abs.Client{URL: absFake.URL, Token: "demo"}, dir)
	pods.Fetch = merken.Fetcher{AllowPrivate: true}
	cal := &caldav.Client{URL: dav.URL, User: "haus", Password: "demo", Loc: time.Local}
	list := einkauf.Open(cal)
	if err := list.Ensure(context.Background()); err != nil {
		log.Fatal(err)
	}
	if _, err := cal.MakeList(context.Background(), "", "baumarkt", "Baumarkt"); err != nil { // a named list beside it (haus#119)
		log.Fatal(err)
	}
	wishes := &einkauf.Wuensche{Listen: list, Dir: dir} // the music wish list with one wish, its stages kept
	if _, err := wishes.Wuenschen(context.Background(), merken.Lied{Kuenstler: "Wanda", Titel: "Salbei", Link: "https://youtube.com/watch?v=Ca7RfL4m744"}, "anna"); err != nil {
		log.Fatal(err)
	}
	// Anna's Discogs is connected (owner 2026-10-26: Platte erfassen on a phone is checked too); the app reaches no Discogs
	os.WriteFile(filepath.Join(dir, "discogs-konten.json"), []byte(`{"anna":{"name":"anna","token":"t","secret":"s"}}`), 0o600)
	shelves := &discogs.Regale{App: &discogs.App{Key: "demo", Secret: "demo", Base: "http://127.0.0.1:1", Web: "https://www.discogs.test"}, Dir: dir}
	wake := filepath.Join(dir, "stimme")
	os.MkdirAll(wake, 0o700)
	pages.House.Phrase, pages.House.Wakeword = "Hey Joshua", "hey_joshua" // the Stimme page needs a wake phrase
	live := &pages.Live{}
	cfg := pages.StartConfig{WakeRecordings: wake, Einkauf: list, Wuensche: wishes, Discogs: shelves, Podcasts: pods, HA: &ha.Client{URL: fake.URL, Token: "demo"}, Notes: nd, Favorites: favoriten.Open(dir), MusicInbox: inbox,
		Calendar: cal, Views: ansicht.Open(dir),
		RadicalePublicURL: "https://caldav.example", Fetcher: merken.Fetcher{AllowPrivate: true}}
	mux := http.NewServeMux()
	mux.HandleFunc("/start", pages.Start(cfg))
	mux.HandleFunc("/raeume", pages.Raeume(cfg))
	mux.HandleFunc(cards.ThingPages, pages.Ding(cfg))
	mux.HandleFunc(cards.SwitchPath, pages.Schalten(cfg))
	mux.HandleFunc("/medien", pages.Medien(cfg))
	mux.HandleFunc("/energie", pages.Energie(cfg))
	mux.HandleFunc("/kalender", pages.Kalender(cfg))
	mux.HandleFunc("/kalender/verwalten", pages.KalenderVerwalten(cfg))
	mux.HandleFunc(pages.MusikPath, pages.Musik(cfg))
	mux.HandleFunc(pages.PlattePath, pages.Platte(cfg))
	mux.HandleFunc(pages.StimmePath, pages.Stimme(cfg))
	mux.HandleFunc(pages.DiscogsPath, pages.Discogs(cfg))
	mux.HandleFunc(pages.MusikTeilPath, pages.Musik(cfg))
	mux.HandleFunc(pages.MusikStandPath, pages.Musik(cfg))                       // haus#113: the import's line
	mux.HandleFunc("/demo/musik", func(w http.ResponseWriter, r *http.Request) { // what arrived in the inbox, name → size
		got := map[string]int64{}
		filepath.WalkDir(inbox, func(p string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				if fi, err := d.Info(); err == nil {
					rel, _ := filepath.Rel(inbox, p)
					got[rel] = fi.Size()
				}
			}
			return nil
		})
		json.NewEncoder(w).Encode(got)
	})
	mux.HandleFunc("/notizen", pages.Notizen(cfg))
	mux.HandleFunc(cards.NotePages, pages.Notizen(cfg))
	mux.HandleFunc("/mehr", pages.Mehr(cfg))
	mux.HandleFunc(pages.PodcastsPath, pages.Podcasts(cfg))
	mux.HandleFunc("/demo/feed.xml", func(w http.ResponseWriter, r *http.Request) { // a podcast's feed; the fake ABS knows it by this address
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Write([]byte(`<?xml version="1.0"?><rss version="2.0"><channel><title>Demo-Podcast</title></channel></rss>`))
	})
	mux.HandleFunc("/demo/rezept", func(w http.ResponseWriter, r *http.Request) { // a recipe page to keep, as Chefkoch marks one up
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<html><head><title>Kürbissuppe</title><script type="application/ld+json">{"@type":"Recipe","name":"Kürbissuppe",` +
			`"recipeYield":"4","totalTime":"PT40M","recipeIngredient":["1 Hokkaido","1 l Gemüsebrühe","200 ml Kokosmilch"],` +
			`"recipeInstructions":[{"@type":"HowToStep","text":"Kürbis würfeln."},{"@type":"HowToStep","text":"In Brühe kochen und pürieren."}]}</script></head><body></body></html>`))
	})
	live.HA = cfg.HA
	list.Changed = func(r caldav.ListRef) { live.Tell(pages.ListeLive(r)) }
	mux.Handle(pages.LivePath, live)
	mux.HandleFunc(pages.EinkaufPath, pages.Einkauf(cfg))
	mux.HandleFunc(pages.EinkaufRezeptePath, pages.Einkauf(cfg))
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
	absFake.AddFeed("http://"+*addr+"/demo/feed.xml", "Demo-Podcast")
	srv := &http.Server{Addr: *addr, Handler: anna, ReadHeaderTimeout: 5 * time.Second}
	log.Printf("hausdemo: http://%s/start (fakes only, not for a box)", *addr)
	log.Fatal(srv.ListenAndServe())
}
