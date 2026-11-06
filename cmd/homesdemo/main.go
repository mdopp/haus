// Command homesdemo serves Homes' family pages against fakes: for the
// browser tests (e2e/) and to look at a page without a box. Home
// Assistant is hafake with a few lamps, a switch, a cover and a player,
// whose calls change their state as HA's do; the notes and the
// favourites are a temp folder. Every request is anna, in family and
// admins. Never for a box: there is no login and nothing is real.
//
// /demo/fehler?status=503 makes every switch fail (0: work again);
// /demo/setze?id=light.flur&state=on changes a state as if someone
// switched it elsewhere (&dimmbar=1: a lamp HA can dim); /demo/laib?ms=800&fehler=0 makes Mehr's Laib
// answer its task list that slowly (or fail), and forgets the kept list.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/mdopp/homes/internal/abruf"
	"github.com/mdopp/homes/internal/devices"
	"github.com/mdopp/homes/internal/discogs"
	"github.com/mdopp/homes/internal/namen"
	"github.com/mdopp/homes/internal/napi"

	"github.com/mdopp/homes/internal/abs"
	"github.com/mdopp/homes/internal/abs/absfake"
	"github.com/mdopp/homes/internal/ansicht"
	"github.com/mdopp/homes/internal/caldav"
	"github.com/mdopp/homes/internal/caldav/davfake"
	"github.com/mdopp/homes/internal/cards"
	"github.com/mdopp/homes/internal/einkauf"
	"github.com/mdopp/homes/internal/einrichten"
	"github.com/mdopp/homes/internal/favoriten"
	"github.com/mdopp/homes/internal/gesehen"
	"github.com/mdopp/homes/internal/gesichter"
	"github.com/mdopp/homes/internal/ha"
	"github.com/mdopp/homes/internal/ha/hafake"
	"github.com/mdopp/homes/internal/jellyfin"
	"github.com/mdopp/homes/internal/laib"
	"github.com/mdopp/homes/internal/laib/laibfake"
	"github.com/mdopp/homes/internal/merken"
	"github.com/mdopp/homes/internal/modul"
	"github.com/mdopp/homes/internal/notes"
	"github.com/mdopp/homes/internal/pages"
	"github.com/mdopp/homes/internal/podcasts"
	"github.com/mdopp/homes/internal/reitti"
	"github.com/mdopp/homes/internal/samba"
	"github.com/mdopp/homes/internal/texte"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8651", "where to listen")
	flag.Parse()

	fake := hafake.New("demo")
	defer fake.Close()
	fake.ApplyCalls(true)
	fake.SetRegistries(
		[]map[string]any{{"area_id": "flur", "name": "Flur"}, {"area_id": "kueche", "name": "Küche"}, {"area_id": "wohnzimmer", "name": "Wohnzimmer"},
			{"area_id": "buero", "name": "Büro"}, {"area_id": "esszimmer", "name": "Esszimmer"}, {"area_id": "garage", "name": "Garage"}},
		nil,
		[]map[string]any{
			{"entity_id": "light.flur", "area_id": "flur"},
			{"entity_id": "light.kueche", "area_id": "kueche"},
			{"entity_id": "switch.kaffee", "area_id": "kueche"},
			{"entity_id": "cover.rollo", "area_id": "wohnzimmer"},
			{"entity_id": "media_player.wz", "area_id": "wohnzimmer"},
			// Anna's favourites on Start, the app's preview of the tiles (homes-android#245)
			{"entity_id": "light.buero", "area_id": "buero"}, {"entity_id": "cover.esszimmer", "area_id": "esszimmer"},
			{"entity_id": "cover.wohnzimmer", "area_id": "wohnzimmer"}, {"entity_id": "cover.garage", "area_id": "garage"}, {"entity_id": "lock.haustuer", "area_id": "flur"},
			// a Voice PE still on its factory word: the Aktivierungswort page says Laib selects „Hey Mycroft“ (owner 2026-10-30)
			{"entity_id": "select.voice_pe_aktivierungswort", "area_id": "kueche", "translation_key": "wake_word", "entity_category": "config"},
		})
	fake.Set("select.voice_pe_aktivierungswort", "Okay Nabu", map[string]any{"friendly_name": "Voice PE Aktivierungswort", "options": []string{"Okay Nabu", "Hey Jarvis", "Hey Mycroft"}})
	fake.Set("light.flur", "off", map[string]any{"friendly_name": "Flurlicht"})
	fake.Set("light.kueche", "on", map[string]any{"friendly_name": "Küchenlicht", "brightness": 153, "supported_color_modes": []string{"brightness"}, "color_mode": "brightness"})
	fake.Set("switch.kaffee", "off", map[string]any{"friendly_name": "Kaffeemaschine"})
	fake.Set("cover.rollo", "open", map[string]any{"friendly_name": "Rollo", "device_class": "shutter", "current_position": 60, "supported_features": 15.0})
	fake.Set("light.buero", "off", map[string]any{"friendly_name": "Bürolicht", "supported_color_modes": []string{"brightness"}})
	fake.Set("cover.esszimmer", "open", map[string]any{"friendly_name": "Esszimmerjalousien", "device_class": "blind", "current_position": 32, "supported_features": 15.0})
	fake.Set("cover.wohnzimmer", "open", map[string]any{"friendly_name": "Wohnzimmerjalousien", "device_class": "blind", "current_position": 15, "supported_features": 15.0})
	fake.Set("cover.garage", "closed", map[string]any{"friendly_name": "Garagentor Garage", "device_class": "garage", "supported_features": 3.0})
	fake.Set("lock.haustuer", "locked", map[string]any{"friendly_name": "Haustür", "supported_features": 1.0})
	power := func(name string) map[string]any {
		return map[string]any{"friendly_name": name, "device_class": "power", "unit_of_measurement": "W"}
	}
	fake.Set("sensor.pv", "3200", power("Aktuell erzeugter PV-Strom"))
	fake.Set("sensor.haus", "800", power("Aktueller Hausverbrauch"))
	fake.Set("sensor.netz", "-1600", power("Aktuelle Netz Leistung"))
	fake.Set("sensor.akku", "800", power("Aktuelle Akku Leistung"))
	fake.Set("sensor.akku_soc", "74", map[string]any{"friendly_name": "Akku-Füllstand in Prozent", "device_class": "battery", "unit_of_measurement": "%"})
	demoEnergyCounters(fake, time.Now())
	fake.Set("media_player.wz", "playing", map[string]any{"friendly_name": "Wohnzimmer", "media_title": "Abbey Road", "media_artist": "The Beatles",
		"volume_level": 0.3, "supported_features": 1.0 + 4 + 1024 + 16384})

	dir, err := os.MkdirTemp("", "homesdemo-")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)
	os.WriteFile(filepath.Join(dir, favoriten.File), []byte(`{"anna": ["light.buero", "cover.esszimmer", "cover.wohnzimmer", "cover.garage", "light.kueche", "lock.haustuer"]}`), 0o600)
	notesDir := filepath.Join(dir, "notes")
	os.MkdirAll(notesDir, 0o755)
	os.WriteFile(filepath.Join(notesDir, "einkaufsliste.md"), []byte("# Einkaufsliste\n\nMilch\nBrot\n"), 0o644)
	// haus#182: a note with every element the renderer knows, for the phone layout gate
	os.WriteFile(filepath.Join(notesDir, "markdown.md"), []byte("# Markdown\n\nEin Absatz mit **fett**, *kursiv*, `code` und einem [Link](https://example.org/eine/sehr/lange/adresse/die/umbrechen/muss).\n\n"+
		"## Liste\n\n- eins\n  - eins a\n- zwei\n\n1. erst\n2. dann\n\n- [ ] offen\n- [x] erledigt\n\n> ein Zitat\n\n---\n\n```\nein Codeblock mit einer langen Zeile, die auf dem Handy nicht über den Rand darf\n```\n"), 0o644)
	os.MkdirAll(filepath.Join(notesDir, "rezepte"), 0o755)
	os.WriteFile(filepath.Join(notesDir, "rezepte", "lasagne.md"), []byte("---\nart: rezept\ntags: [kochen]\n---\n# Lasagne\n\n*Für 4 Portionen · 1 Std.*\n\n"+
		"## Zutaten\n\n- 500 g Hackfleisch\n- 2 Zwiebeln\n- 1 Dose Tomaten\n- 250 g Lasagneplatten\n\n## Zubereitung\n\n1. Soße kochen.\n2. Schichten und backen.\n"), 0o644)
	nd, err := notes.Open(notesDir)
	if err != nil {
		log.Fatal(err)
	}

	inbox := filepath.Join(dir, "musik-eingang")
	dav := davfake.New("homes", "demo")
	defer dav.Close()
	dav.AddCalendar("/homes/haushalt/", "Haushalt")
	dav.AddPrincipal("anna")
	absFake := absfake.New("demo")
	defer absFake.Close()
	pods := podcasts.Open(&abs.Client{URL: absFake.URL, Token: "demo"}, dir)
	pods.Fetch = abruf.Fetcher{AllowPrivate: true}
	cal := &caldav.Client{URL: dav.URL, User: "homes", Password: "demo", Loc: time.Local}
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
	discogsKonto := func(an bool) {
		os.WriteFile(filepath.Join(dir, "discogs-konten.json"), []byte(map[bool]string{true: `{"anna":{"name":"anna","token":"t","secret":"s","seit":"2026-10-26T10:00:00Z"}}`, false: `{}`}[an]), 0o600)
	}
	discogsKonto(true)
	shelves := &discogs.Regale{App: &discogs.App{Key: "demo", Secret: "demo", Base: "http://127.0.0.1:1", Web: "https://www.discogs.test"}, Dir: dir}
	// haus#152: what Anna watched and heard, and the film and book wish list
	absFake.Hoert("anna", "Krabat", 0.4, false, time.Now().Add(-time.Hour))
	absFake.Hoert("anna", "Momo", 1, true, time.Now().Add(-50*time.Hour))
	seen := &gesehen.Service{Filme: demoFilme{}, Buecher: &abs.Client{URL: absFake.URL, Token: "demo"}, DAV: cal}
	live := &pages.Live{}
	cfg := pages.StartConfig{Gesehen: seen, Einkauf: list, Wuensche: wishes, Discogs: shelves, Podcasts: pods, HA: &ha.Client{URL: fake.URL, Token: "demo"}, Notes: nd, Favorites: favoriten.Open(dir), MusicInbox: inbox,
		Calendar: cal, Views: ansicht.Open(dir, "homes"),
		RadicalePublicURL: "https://caldav.example", Fetcher: abruf.Fetcher{AllowPrivate: true}}
	// what the box lacks (ADR 0001 §10): Standortverlauf missing, Netzlaufwerk switched off, a wish to Laib
	cfg.Module = demoModule(false)
	// each module's setup a task for Anna (owner 2026-11-04): the Homes app, Audiobookshelf, Immich, Jellyfin; none ticked yet
	cfg.Einrichtung = &einrichten.Quellen{Module: cfg.Module, Haken: einrichten.Open(dir), HomesApp: einrichten.App("Homes", "cloud.dopp.homes", "https://homes.example",
		"https://github.com/mdopp/homes-android#readme", "schritt.homes", "schritt.homes-iphone"), Adresse: "https://homes.example"}
	mux := http.NewServeMux()
	mux.HandleFunc(pages.EinrichtenPages, pages.Einrichten(cfg))
	mux.HandleFunc("/start", pages.Start(cfg))
	mux.HandleFunc("/schrift/", pages.Font()) // Geist, as on the box: the pictures and the layout gate in the family's font
	mux.HandleFunc("/raeume", pages.Raeume(cfg))
	mux.HandleFunc(cards.ThingPages, pages.Ding(cfg))
	mux.HandleFunc(cards.SwitchPath, pages.Schalten(cfg))
	mux.HandleFunc("/medien", pages.Medien(cfg))
	mux.HandleFunc("/energie", pages.Energie(cfg))
	mux.HandleFunc("/kalender", pages.Kalender(cfg))
	mux.HandleFunc("/kalender/verwalten", pages.KalenderVerwalten(cfg))
	mux.HandleFunc(pages.PlattePath, pages.Platte(cfg))
	mux.HandleFunc(pages.AktivierungswortPath, pages.Aktivierungswort(cfg))
	mux.HandleFunc("/aufgaben", pages.DeineAufgaben(cfg))
	// haus#176: the phone layout gate covers these too
	upload := filepath.Join(dir, "upload")
	os.MkdirAll(upload, 0o755)
	doku := cfg
	doku.Ablage, doku.DocumentsMail, doku.Module = napi.NewUploads(upload), "dokumente@example.org", demoModule(true)
	doku.PaperlessPublicURL, doku.PaperlessLANOnly = "https://paperless.example", true // the archive's way in, at home only
	doku.Docs = demoPaperless()                                                        // the search, the Bereiche, Zuletzt abgelegt (owner 2026-11-06)
	mux.HandleFunc("/dokumente", pages.Dokumente(doku))
	mux.HandleFunc(pages.DokumenteSuchePath, pages.DokumenteSuche(doku))
	mux.HandleFunc("/willkommen", pages.Willkommen(cfg))
	mux.HandleFunc("/sprechen", pages.Sprechen(cfg))
	store, err := devices.Open(dir)
	if err == nil { // haus#172: pairing and removing a phone, end to end
		mux.HandleFunc("/box/geraete", pages.Geraete(store, "https://homes.example", cfg))
		mux.HandleFunc("/pair-device", pages.PairDevice(store, cfg))
	}
	mux.HandleFunc(pages.DiscogsPath, pages.Discogs(cfg))
	// Standortverlauf, Anna connected (owner 2026-11-04): her newest point came 20 minutes ago;
	// /demo/standort?punkte=0: none has come yet. The demo's Reitti takes nothing.
	reittiKonto := func(punkte bool) {
		k := reitti.Konto{Token: "demo", Seit: time.Now().Add(-26 * time.Hour)}
		if punkte {
			k.Zuletzt = time.Now().Add(-20 * time.Minute)
		}
		b, _ := json.Marshal(map[string]reitti.Konto{"anna": k})
		os.WriteFile(filepath.Join(dir, "reitti-konten.json"), b, 0o600)
	}
	reittiKonto(true)
	standort := cfg
	standort.Reitti = &reitti.Reitti{URL: "http://127.0.0.1:1", PublicURL: "https://reitti.example", Dir: dir}
	mux.HandleFunc(pages.StandortPath, pages.Standort(standort))
	mux.HandleFunc("/demo/standort", func(w http.ResponseWriter, r *http.Request) { reittiKonto(r.URL.Query().Get("punkte") != "0") })
	mux.HandleFunc("/notizen", pages.Notizen(cfg))
	mux.HandleFunc(cards.NotePages, pages.Notizen(cfg))
	// Mehr with a Laib whose task list takes a while: the Einrichtung card
	// fills itself after the page (owner 2026-11-03)
	demoLaib := &laibFake{wishes: laibfake.New()}
	mux.Handle("/demo/laib/mcp", demoLaib)
	mehr := cfg
	mehr.Laib = &laib.Client{URL: "http://" + *addr + "/demo/laib", Token: "demo"}
	mux.HandleFunc("/mehr", pages.Mehr(mehr))
	mux.HandleFunc(pages.MehrAufgabenPath, pages.Mehr(mehr))
	mux.HandleFunc(pages.SpurenPath, pages.Mehr(mehr))
	mux.HandleFunc(pages.AnfordernPath, pages.Anfordern(mehr))
	mux.HandleFunc(pages.ModulPages, pages.ModulSeite(mehr))
	// „So hängt alles zusammen“ on a box with all its threads (owner 2026-11-05: „auch im Bild,
	// genau wie der Standort“): Standortverlauf on, and Immich for the contacts (no key: Fotos searches)
	zusammen := mehr
	zusammen.Module, zusammen.Gesichter = demoModule(false, "standort"), &gesichter.Gesichter{Web: "https://photos.example", Dir: dir}
	mux.HandleFunc(pages.ZusammenPath, pages.Zusammen(zusammen))
	// Musik (owner 2026-11-04: „nur Dateien darstellen“): a file being sorted in, one done, one beets
	// kept back; /demo/musik?leer=1 empties the inbox and beets' status, ?discogs=0 is Anna without Discogs
	musikCfg := mehr
	musikCfg.Samba = &samba.Dir{Path: dir}
	mux.HandleFunc(pages.MusikPath, pages.Musik(musikCfg))
	mux.HandleFunc("/demo/musik", func(w http.ResponseWriter, r *http.Request) {
		demoLaib.wishes.Status("beets", laibfake.DemoBeets(inbox, time.Now(), r.URL.Query().Get("leer") == "1"))
		discogsKonto(r.URL.Query().Get("discogs") != "0")
	})
	demoLaib.wishes.Status("beets", laibfake.DemoBeets(inbox, time.Now(), false))
	seiten := mehr // Daten sichern, Netzlaufwerk, Box, Adresse (the Mehr audit)
	seiten.Reitti = standort.Reitti
	mehrSeiten(mux, seiten, dav, dir, store)
	mux.HandleFunc("/demo/wunsch", func(w http.ResponseWriter, r *http.Request) { // a wish moved on in Laib: wartet, abgelehnt, …
		demoLaib.wishes.Setze(r.URL.Query().Get("modul"), r.URL.Query().Get("stand"), "")
		mehr.Laib.ForgetWuensche()
	})
	mux.HandleFunc("/demo/laib", func(w http.ResponseWriter, r *http.Request) {
		ms, _ := strconv.Atoi(r.URL.Query().Get("ms"))
		demoLaib.set(time.Duration(ms)*time.Millisecond, r.URL.Query().Get("fehler") == "1")
		mehr.Laib.ForgetAufgaben()
		mehr.Laib.ForgetWuensche()
	})
	mux.HandleFunc(pages.PodcastsPath, pages.Podcasts(cfg))
	mux.HandleFunc(pages.GesehenPath, pages.Gesehen(cfg))
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
		attrs := map[string]any{"friendly_name": q.Get("name")}
		if q.Get("dimmbar") != "" {
			attrs["supported_color_modes"] = []any{"brightness"}
		}
		fake.Change(q.Get("id"), q.Get("state"), attrs)
	})
	anna := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("Remote-User", "anna")
		r.Header.Set("Remote-Email", "anna@example.org")
		r.Header.Set("Remote-Groups", "family,admins")
		if c, err := r.Cookie("wer"); err == nil && c.Value != "" { // another person, in the family only
			r.Header.Set("Remote-User", c.Value)
			r.Header.Set("Remote-Email", c.Value+"@example.org")
			r.Header.Set("Remote-Groups", "family")
		}
		mux.ServeHTTP(w, r)
	})
	absFake.AddFeed("http://"+*addr+"/demo/feed.xml", "Demo-Podcast")
	srv := &http.Server{Addr: *addr, Handler: anna, ReadHeaderTimeout: 5 * time.Second}
	log.Printf("homesdemo: http://%s/start (fakes only, not for a box)", *addr)
	log.Fatal(srv.ListenAndServe())
}

// demoFilme is Jellyfin's watch history for the demo: a series and a film,
// and Dune in the library.
type demoFilme struct{}

func (demoFilme) Geschaut(context.Context, string, int) ([]jellyfin.Gesehen, error) {
	return []jellyfin.Gesehen{{Name: "Dark", Serie: true, Folge: "S2E3 · Der Plan", Fertig: true, Zuletzt: time.Now().Add(-26 * time.Hour)},
		{Name: "Dune", Jahr: 2021, Fertig: true, Zuletzt: time.Now().Add(-75 * time.Hour)}}, nil
}

func (demoFilme) HatFilm(_ context.Context, name string) (string, bool, error) {
	return "Dune", namen.Plain(name) == "dune", nil
}

// demoEnergyCounters gives Energie its counters, as the fallback finds
// them by friendly name: what each added since local midnight (Heute),
// and HA's long-term statistics from last January on (the history), the
// sun by the season.
func demoEnergyCounters(fake *hafake.HA, now time.Time) {
	kwh := func(name string) map[string]any {
		return map[string]any{"friendly_name": name, "device_class": "energy", "unit_of_measurement": "kWh", "state_class": "total_increasing"}
	}
	y, m, d := now.Date()
	midnight := time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	for _, c := range []struct {
		id, name     string
		total, today float64
		perDay       func(time.Time) float64
	}{
		{"sensor.pv_total", "PV Erzeugung", 5012.4, 8.2, func(t time.Time) float64 { return 2 + 14*sunOf(t) }},
		{"sensor.einsp_total", "Netzeinspeisung", 2306, 0.3, func(t time.Time) float64 { return 9 * sunOf(t) }},
		{"sensor.bezug_total", "Netzbezug", 1803.1, 2.0, func(t time.Time) float64 { return 5 - 4*sunOf(t) }},
		{"sensor.akku_laden", "Batterie laden", 700.5, 3.1, nil},
		{"sensor.akku_entladen", "Batterie entladen", 650.2, 2.4, nil},
	} {
		fake.Set(c.id, strconv.FormatFloat(c.total, 'f', 1, 64), kwh(c.name))
		fake.SetHistory(c.id, []map[string]any{{"state": strconv.FormatFloat(c.total-c.today, 'f', 1, 64), "last_changed": midnight.UTC().Format(time.RFC3339)}})
		if c.perDay == nil {
			continue
		}
		var pts []hafake.Stat
		for t := time.Date(y-1, time.January, 1, 0, 0, 0, 0, now.Location()); t.Before(midnight); t = t.AddDate(0, 0, 1) {
			pts = append(pts, hafake.Stat{Start: t.Add(12 * time.Hour), Change: c.perDay(t)})
		}
		for h := 0; h < now.Hour(); h++ {
			pts = append(pts, hafake.Stat{Start: midnight.Add(time.Duration(h) * time.Hour), Change: c.today / float64(max(1, now.Hour()))})
		}
		fake.SetStatistics(c.id, pts)
	}
}

// sunOf is how much the sun gives on a day, 0 in midwinter to 1 in midsummer.
func sunOf(t time.Time) float64 {
	return (1 - math.Cos(2*math.Pi*float64(t.YearDay()-10)/365)) / 2
}

// laibFake is Laib's MCP for Mehr: one task for everyone, after delay,
// and the modules' wishes (laibfake).
type laibFake struct {
	mu     sync.Mutex
	delay  time.Duration
	fail   bool
	wishes *laibfake.Laib
}

// demoModule is the demo's box: the modules its pages have, with their
// entries in the family's selection as the catalog gives them on a box
// with a music inbox (Musik, not Platte erfassen or Discogs on their own);
// no Reitti, and Netzlaufwerk switched off; Dokumente on its own page only,
// which has a way into the archive. The modules named in an are on all the same.
func demoModule(docs bool, an ...string) *modul.Liste {
	m := func(name, backend, warum, grund string) modul.Modul {
		if slices.Contains(an, name) {
			warum, grund = "", ""
		}
		nav := slices.DeleteFunc(pages.Nav(name), func(e modul.Eintrag) bool {
			return e.Pfad == pages.PlattePath || e.Pfad == pages.DiscogsPath || (e.Pfad == pages.DokumentePath && !docs)
		})
		e := map[string]*modul.Einrichtung{"podcasts": einrichten.App("Audiobookshelf", "com.audiobookshelf.app", "https://hoerbuecher.example", "https://www.audiobookshelf.org/guides/", "schritt.anmelden"),
			"fotos": einrichten.App("Immich", "app.alextran.immich", "https://photos.example", "https://immich.app/docs/features/mobile-app", "schritt.fotos", "schritt.fotos-sichern"),
			"filme": einrichten.App("Jellyfin", "org.jellyfin.mobile", "https://media.example", "https://jellyfin.org/docs/general/clients/", "schritt.anmelden")}[name]
		return modul.Modul{Name: name, Titel: texte.Titel(name), Backend: backend, Warum: warum, Grund: grund, Nav: nav, Einrichtung: e}
	}
	l, err := modul.Bauen([]modul.Modul{m("geraete", "home-assistant", "", ""), m("energie", "home-assistant", "", ""),
		m("kalender", "radicale", "", ""), m("kontakte", "radicale", "", ""), m("einkauf", "radicale", "", ""), m("notizen", "file-share", "", ""),
		m("musik", "beets", "", ""), m("gesehen", "media", "", ""), m("dokumente", "paperless", "", ""),
		m("standort", "reitti", modul.Fehlt, "Auf dieser Box läuft kein Reitti."),
		m("podcasts", "audiobookshelf", "", ""), m("netzlaufwerk", "file-share", modul.Aus, "Auf dieser Box ausgeschaltet."),
		m("gesundheit", "gesundheit", modul.Fehlt, "Auf dieser Box läuft Gesundheit nicht."), m("fotos", "immich", "", ""), m("filme", "media", "", "")})
	if err != nil {
		log.Fatal(err)
	}
	return l
}

func (f *laibFake) set(d time.Duration, fail bool) {
	f.mu.Lock()
	f.delay, f.fail = d, fail
	f.mu.Unlock()
}

func (f *laibFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Method string `json:"method"`
		Params struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		} `json:"params"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.Method == "tools/list" {
		json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"tools": f.wishes.Tools()}})
		return
	}
	f.mu.Lock()
	delay, fail := f.delay, f.fail
	f.mu.Unlock()
	select {
	case <-time.After(delay):
	case <-r.Context().Done():
		return
	}
	if fail {
		http.Error(w, "laib is down", http.StatusBadGateway)
		return
	}
	if text, ok := boxAntwort(req.Params.Name, req.Params.Arguments); ok {
		json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"content": []map[string]string{{"text": text}}}})
		return
	}
	if text, isErr, ok := f.wishes.Antwort(req.Params.Name, req.Params.Arguments); ok {
		json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"content": []map[string]string{{"text": text}}, "isError": isErr}})
		return
	}
	text, isErr := "unknown tool "+req.Params.Name, true
	if req.Params.Name == "laib_aufgabe" {
		text, isErr = `{"offen":1,"faellig":0,"aufgaben":[{"id":8,"titel":"Homes-App koppeln","warum":"Dann hast du das Haus auf dem Handy.","fuer":"alle","weg":{"seite":"/willkommen"}}]}`, false
	}
	json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"content": []map[string]string{{"text": text}}, "isError": isErr}})
}
