package pages

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/homes/internal/einrichten"
	"github.com/mdopp/homes/internal/modul"
	"github.com/mdopp/homes/internal/samba"
	"github.com/mdopp/homes/internal/samba/sambafake"
)

// setupBox is a box with Fotos (Immich) and Kalender (DAVx⁵), both with an
// app, and Notizen without one; the Homes app is paired by nobody.
func setupBox(t *testing.T, mods ...string) StartConfig {
	t.Helper()
	all := map[string]modul.Modul{
		"fotos":    {Name: "fotos", Titel: "Fotos", Einrichtung: einrichten.App("Immich", "app.alextran.immich", "https://photos.box.test", "https://immich.app/docs", "schritt.fotos", "schritt.fotos-sichern")},
		"kalender": {Name: "kalender", Titel: "Kalender", Einrichtung: einrichten.App("DAVx⁵", "at.bitfire.davdroid", "https://caldav.box.test", "https://manual.davx5.com/", "schritt.davx")},
		"notizen":  {Name: "notizen", Titel: "Notizen"},
	}
	if len(mods) == 0 {
		mods = []string{"fotos", "kalender", "notizen"}
	}
	var ms []modul.Modul
	for _, m := range mods {
		ms = append(ms, all[m])
	}
	l, err := modul.Bauen(ms)
	if err != nil {
		t.Fatal(err)
	}
	return StartConfig{Module: l, Einrichtung: &einrichten.Quellen{Module: l, Haken: einrichten.Open(t.TempDir())}}
}

func welcomePage(t *testing.T, cfg StartConfig) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", "/willkommen", nil)
	req.Header.Set("Remote-User", "anna")
	req.Header.Set("Remote-Name", "Anna Müller")
	rec := httptest.NewRecorder()
	Willkommen(cfg).ServeHTTP(rec, req)
	return rec
}

// owner 2026-11-04: Willkommen is „Hallo <Vorname>“, the username and the
// next steps, the person's open setup tasks as links, only the open ones;
// the long guides are gone (they are each module's now).
func TestWillkommenListsOnlyTheOpenTasks(t *testing.T) {
	cfg := setupBox(t)
	fs := sambafake.New(t, map[string]int64{"anna": 0}, true)
	cfg.Samba = &samba.Dir{Path: fs.Dir}
	cfg.Einrichtung.Haken.Setzen("anna", "kalender", einrichten.Hand, time.Now())
	rec := welcomePage(t, cfg)
	page := rec.Body.String()
	mustContain(t, page, "<h1>Hallo Anna</h1>", "Dein Benutzername:", ">anna</span>", "Deine nächsten Schritte",
		`<a href="/einrichten/fotos">Fotos auf dem Handy einrichten</a>`, "Mit der App „Immich“.",
		`<a href="/netzlaufwerk">Netzlaufwerk-Passwort festlegen</a>`, "href=/start>Weiter zur Startseite")
	mustNotContain(t, page, "Kalender auf dem Handy", "Notizen auf dem Handy", "Bitwarden", "drucken", "Apps einrichten")
	before(t, page, "Netzlaufwerk-Passwort", "Fotos auf dem Handy") // the important first
	if c := rec.Header().Get("Set-Cookie"); !strings.Contains(c, welcomeCookie+"=gesehen") {
		t.Errorf("the device is not marked: %q", c)
	}
	cfg.Samba = nil
	cfg.Einrichtung.Haken.Setzen("anna", "fotos", einrichten.Handy, time.Now())
	page = welcomePage(t, cfg).Body.String()
	mustContain(t, page, "Alles eingerichtet.")
	mustNotContain(t, page, "Deine nächsten Schritte", "auf dem Handy einrichten")
	rec = httptest.NewRecorder()
	Willkommen(StartConfig{}).ServeHTTP(rec, httptest.NewRequest("POST", "/willkommen", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST: %d", rec.Code)
	}
}

// Start sends a device's first visit to the welcome page, once.
func TestStartSendsTheFirstVisitToWillkommen(t *testing.T) {
	cfg := StartConfig{Laib: fakeLaib(t, allGreen(t), nil), Welcome: true}
	req := httptest.NewRequest("GET", "/start", nil)
	req.Header.Set("Remote-User", "anna")
	rec := httptest.NewRecorder()
	Start(cfg).ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/willkommen" {
		t.Fatalf("first visit: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	req.AddCookie(&http.Cookie{Name: welcomeCookie, Value: "gesehen"})
	rec = httptest.NewRecorder()
	Start(cfg).ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("seen: %d\n%s", rec.Code, rec.Body.String())
	}
}
