package main

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mdopp/homes/internal/einrichten"
	"github.com/mdopp/homes/internal/laib"
	"github.com/mdopp/homes/internal/modul"
	"github.com/mdopp/homes/internal/podcasts"
	"github.com/mdopp/homes/internal/texte"
)

// TestEveryVariableHasAnOwner (ADR 0001 §8): every HOMES_* the production
// code reads is a module's or the core's, and every one a module names is
// read somewhere, so the catalog cannot go stale.
func TestEveryVariableHasAnOwner(t *testing.T) {
	s, _, _ := fullSite(t)
	owned := slices.Clone(kern)
	for _, m := range katalog(s) {
		owned = append(owned, m.Env...)
	}
	read := map[string]string{}
	re := regexp.MustCompile(`"((?:HOMES|LAIB)_[A-Z0-9_]+)`)
	for _, root := range []string{"../../cmd", "../../internal"} {
		filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") || strings.Contains(p, "fake") {
				return err
			}
			b, _ := os.ReadFile(p)
			for _, m := range re.FindAllStringSubmatch(string(b), -1) {
				read[m[1]] = p
			}
			return nil
		})
	}
	if len(read) < 40 {
		t.Fatalf("found only %d variables: the walk missed the code", len(read))
	}
	for v, file := range read {
		if !slices.Contains(owned, v) {
			t.Errorf("%s (read in %s) belongs to no module's Env and is not in kern (cmd/homes/module.go)", v, file)
		}
	}
	for _, v := range owned {
		if _, ok := read[v]; !ok {
			t.Errorf("%s is named in the catalog but read nowhere", v)
		}
	}
}

func TestTheCatalogBuilds(t *testing.T) {
	full, _, _ := fullSite(t)
	full.podcasts, full.gesundheit, full.musik = nil, "", "/musik-eingang"
	l, err := modul.Bauen(katalog(full))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"geraete", "energie", "kalender", "kontakte", "konto", "notizen", "dokumente", "musik"} {
		if !l.An(name) {
			t.Errorf("%s is off with its backend set", name)
		}
	}
	for _, name := range []string{"podcasts", "gesundheit", "standort", "sicherung", "sprechen"} {
		if l.An(name) {
			t.Errorf("%s is on without its backend", name)
		}
	}
	log := l.String()
	for _, want := range []string{"geraete (home-assistant)", "kalender (radicale)", "standort (fehlt: Auf dieser Box läuft kein Reitti.)"} {
		if !strings.Contains(log, want) {
			t.Errorf("the log line lacks %q: %s", want, log)
		}
	}
	empty, err := modul.Bauen(katalog(site{}))
	if err != nil {
		t.Fatalf("a box with nothing: %v", err)
	}
	if strings.Contains(empty.String(), "Module an: geraete") || !strings.Contains(empty.String(), "energie (fehlt") {
		t.Errorf("a box with nothing: %s", empty)
	}
	names := map[string]bool{}
	for _, m := range katalog(site{dienste: map[string]bool{}}) {
		names[m.Name] = true
	}
	if len(names) != 22 {
		t.Errorf("the catalog has %d modules, ADR 0001 §7 lists 22", len(names))
	}
	if n := len(katalog(site{})); n != 18 { // Laib silent: the four link modules wait (dienstePflegen)
		t.Errorf("Laib silent, %d modules", n)
	}
}

// owner 2026-11-04: each module's setup is whole (every step it names is
// in German, an app has its package, guide and address), and a service
// Homes only leads to is on with it: fotos and filme here, passwoerter
// not; while Laib has not said what runs, they are not in the list at all
// (unknown is not missing: no „Hier anfordern“).
func TestTheSetupsAreWhole(t *testing.T) {
	s, _, _ := fullSite(t)
	s.radicale, s.boteSignal, s.publicURL = "https://caldav.example", "+491701234567", "https://homes.example"
	steps := 0
	check := func(name string, e *modul.Einrichtung) {
		for _, id := range e.Schritte {
			if steps++; texte.T(texte.ID(id)) == id {
				t.Errorf("%s: step %s has no German", name, id)
			}
		}
		if e.App != "" && (e.Paket == "" || e.Anleitung == "" || e.Web == "" || !strings.HasPrefix(e.Store, "https://")) {
			t.Errorf("%s: %+v", name, e)
		}
	}
	apps := 0
	for _, m := range katalog(s) {
		if m.Einrichtung != nil {
			apps++
			check(m.Name, m.Einrichtung)
		}
	}
	l, err := modul.Bauen(katalog(s))
	if err != nil {
		t.Fatal(err)
	}
	s.napi.Module = l
	check("homes", einrichtung(s, nil, nil).HomesApp)
	if m, _ := l.Eins("kalender"); m.Einrichtung != nil { // owner 2026-11-04: no calendar task, iPhone to be thought anew
		t.Errorf("kalender has a setup: %+v", m.Einrichtung)
	}
	if apps != 5 || steps < 9 {
		t.Errorf("%d modules with a setup, %d steps", apps, steps)
	}
	if !l.An("fotos") || !l.An("filme") || l.An("passwoerter") {
		t.Errorf("link modules: %s", l)
	}
	if m, _ := l.Eins("passwoerter"); m.Grund != "Auf dieser Box läuft Vaultwarden nicht." {
		t.Errorf("passwoerter: %q", m.Grund)
	}
	s.dienste = nil
	l, _ = modul.Bauen(katalog(s))
	for _, name := range []string{"fotos", "passwoerter", "filme", "hoerbuecher"} {
		if m, ok := l.Eins(name); ok {
			t.Errorf("Laib silent: %+v", m)
		}
	}
}

// owner 2026-11-04: „Ich sehe keine Beschreibung für Audiobookshelf.“ Audiobookshelf's app is
// set up once, as Hörbücher und Podcasts, whenever Laib runs it — with Homes' own ABS token
// or without; the phone saying it has the app ticks the task; Podcasts adds no second one.
func TestHoerbuecherIsSetUpOnce(t *testing.T) {
	s, _, _ := fullSite(t)
	s.dataDir, s.publicURL = t.TempDir(), "https://homes.example"
	s.dienste["audiobookshelf"] = true
	for _, pods := range []*podcasts.Service{nil, {}} { // no ABS token, then Podcasts on
		s.podcasts = pods
		l, err := modul.Bauen(katalog(s))
		if err != nil {
			t.Fatal(err)
		}
		if m, _ := l.Eins("podcasts"); m.An() != (pods != nil) || m.Einrichtung != nil {
			t.Errorf("podcasts: on %v, setup %+v", m.An(), m.Einrichtung)
		}
		s.napi.Module = l
		q := einrichtung(s, nil, nil)
		var abs []einrichten.Aufgabe
		for _, a := range q.Fuer("anna") {
			if a.Einrichtung.Paket == "com.audiobookshelf.app" {
				abs = append(abs, a)
			}
		}
		if len(abs) != 1 || abs[0].Modul != "hoerbuecher" || abs[0].Titel != "Hörbücher und Podcasts auf dem Handy einrichten" ||
			abs[0].Einrichtung.Web != "https://hoerbuecher.example" || abs[0].Erledigt != nil {
			t.Fatalf("podcasts %v: %+v", pods != nil, abs)
		}
	}
	q := einrichtung(s, nil, nil)
	if n, err := q.Installiert("anna", []string{"com.audiobookshelf.app"}); err != nil || n != 1 {
		t.Fatalf("ticked %d, %v", n, err)
	}
	if a, _ := q.Eine("anna", "hoerbuecher"); a.Erledigt == nil || a.Erledigt.Wie != einrichten.Handy {
		t.Errorf("not ticked by the phone: %+v", a.Erledigt)
	}
	delete(s.dienste, "audiobookshelf")
	if l, _ := modul.Bauen(katalog(s)); l.An("hoerbuecher") {
		t.Error("on without Audiobookshelf")
	}
}

// 2026-11-05 on the box: whoever restarts Homes holds Laib's lock until
// Homes is healthy, so Laib's status times out at start and Fotos,
// Passwörter, Filme und Musik, Hörbücher und Podcasts stayed under „Gibt
// es hier noch nicht“ until the next restart. Laib answers here only from
// the third ask: until then no link module is in the list (none offered
// as „Hier anfordern“), then they follow what runs, in the one list the
// pages, /napi and the setup tasks hold, and again when a service goes.
func TestLinkModulesFollowLaibOnceItAnswers(t *testing.T) {
	var mu sync.Mutex
	asked, services := 0, `[{"name":"immich"},{"name":"media"}]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Params struct{ Name string } `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		mu.Lock()
		defer mu.Unlock()
		if req.Params.Name != "laib_status" {
			http.Error(w, "nur status", http.StatusNotFound)
			return
		}
		if asked++; asked < 3 {
			http.Error(w, "busy", http.StatusServiceUnavailable)
			return
		}
		text := `{"ready":{"state":"bereit"},"services":` + services + `}`
		json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"content": []map[string]string{{"text": text}}}})
	}))
	defer srv.Close()
	s, _, _ := fullSite(t)
	s.dataDir, s.domain, s.dienste, s.ops = t.TempDir(), "example", nil, &laib.Client{URL: srv.URL, Token: "t"}
	start := s
	s.napi.Module = logModule(&s)
	q := einrichtung(s, nil, nil)
	kann := s.napi.Module.Kann(modul.Wuensche{Geht: true})
	if slices.ContainsFunc(kann.Module, func(e modul.KannEins) bool { return e.Name == "fotos" || e.Anfordern && e.Backend == "immich" }) {
		t.Fatalf("Laib silent, fotos is listed: %+v", kann.Module)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go dienstePflegen(ctx, start, s.napi.Module, 5*time.Millisecond, 40*time.Millisecond)
	warte := func(what string, ok func() bool) {
		t.Helper()
		for end := time.Now().Add(5 * time.Second); !ok(); time.Sleep(5 * time.Millisecond) {
			if time.Now().After(end) {
				t.Fatalf("%s: %s", what, s.napi.Module)
			}
		}
	}
	warte("Laib answered", func() bool { return s.napi.Module.An("fotos") && s.napi.Module.An("filme") })
	if m, ok := s.napi.Module.Eins("passwoerter"); !ok || m.An() || m.Grund != "Auf dieser Box läuft Vaultwarden nicht." {
		t.Errorf("passwoerter: %+v", m)
	}
	if _, ok := q.Eine("anna", "fotos"); !ok {
		t.Error("the setup tasks did not follow the list")
	}
	mu.Lock()
	if asked < 3 {
		t.Errorf("on after %d asks", asked)
	}
	services = `[{"name":"immich"}]`
	mu.Unlock()
	warte("media gone", func() bool { return !s.napi.Module.An("filme") && s.napi.Module.An("fotos") })
}

// Gesundheit retired (2026-11-06): its module follows the service Laib runs, not only
// its address; before Laib has answered, the address alone decides.
func TestGesundheitFollowsItsService(t *testing.T) {
	s, _, _ := fullSite(t)
	s.gesundheit = "https://gesundheit.example"
	an := func() bool {
		l, err := modul.Bauen(katalog(s))
		if err != nil {
			t.Fatal(err)
		}
		return l.An("gesundheit")
	}
	s.dienste["gesundheit"] = true
	if !an() {
		t.Error("off while the service runs")
	}
	delete(s.dienste, "gesundheit")
	if an() {
		t.Error("on while the service is gone")
	}
	s.dienste = nil
	if !an() {
		t.Error("off before Laib has said what runs")
	}
}
