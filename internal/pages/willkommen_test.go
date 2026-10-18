package pages

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mdopp/haus/internal/samba"
	"github.com/mdopp/haus/internal/samba/sambafake"
)

func welcomePage(t *testing.T, cfg StartConfig) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", "/willkommen", nil)
	req.Host = "haus.box.test"
	req.Header.Set("Remote-User", "anna")
	req.Header.Set("Remote-Name", "Anna Müller")
	rec := httptest.NewRecorder()
	Willkommen(cfg).ServeHTTP(rec, req)
	return rec
}

// haus#56: the welcome page shows only the apps this box has, each with
// its steps and its server address, in the family's words.
func TestWillkommenShowsOnlyTheInstalledApps(t *testing.T) {
	rows := []map[string]any{{"name": "immich", "state": "grün"}, {"name": "vaultwarden", "state": "grün"}, {"name": "auth", "state": "grün"}}
	rec := welcomePage(t, StartConfig{Laib: fakeLaib(t, allGreen(t), rows), PublicURL: "https://haus.dopp.example"})
	page := rec.Body.String()
	for _, w := range []string{"<h1>Willkommen, Anna</h1>", saysWelcomeLead, ">anna<", "<h2>Fotos</h2>", "„Immich“", "https://photos.dopp.example", "Mit Familienkonto anmelden",
		"<h2>Passwörter</h2>", "„Bitwarden“", "https://vault.dopp.example", "Master-Passwort", "auch kein Admin", "Apps einrichten", "href=/start"} {
		if !strings.Contains(page, w) {
			t.Errorf("the page lacks %q", w)
		}
	}
	for _, w := range []string{"Jellyfin", "DAVx", "caldav.", "Netzlaufwerk", "die Anmeldung", "auth"} {
		if strings.Contains(page, w) {
			t.Errorf("the page shows %q, which this box does not have", w)
		}
	}
	if c := rec.Header().Get("Set-Cookie"); !strings.Contains(c, welcomeCookie+"=gesehen") {
		t.Errorf("the device is not marked: %q", c)
	}
	if rec := httptest.NewRecorder(); true {
		Willkommen(StartConfig{}).ServeHTTP(rec, httptest.NewRequest("POST", "/willkommen", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("POST: %d", rec.Code)
		}
	}
}

func TestWillkommenAllAppsAndTheDrive(t *testing.T) {
	fs := sambafake.New(t, map[string]int64{"anna": 0}, true)
	rows := []map[string]any{{"name": "immich"}, {"name": "media"}, {"name": "radicale"}, {"name": "vaultwarden"}}
	// no public address: the domain is the host's, after haus.
	page := welcomePage(t, StartConfig{Laib: fakeLaib(t, allGreen(t), rows), Samba: &samba.Dir{Path: fs.Dir}}).Body.String()
	for _, w := range []string{"Fotos", "Filme und Musik", "„Jellyfin“", "https://media.box.test", "Kalender und Kontakte", "„DAVx⁵“", "CalDAV-Account", "https://caldav.box.test",
		"Passwörter", "https://vault.box.test", "Dateien am Computer", "href=/netzlaufwerk"} {
		if !strings.Contains(page, w) {
			t.Errorf("the page lacks %q", w)
		}
	}
	if strings.Index(page, "Fotos") > strings.Index(page, "Passwörter") {
		t.Errorf("Fotos come first")
	}
}

func TestWillkommenSaysWhenItCannotLook(t *testing.T) {
	page := welcomePage(t, StartConfig{Laib: brokenLaib(t)}).Body.String()
	if !strings.Contains(page, saysAppsUnknown) || strings.Contains(page, "500") || strings.Contains(page, saysNoAppsYet) {
		t.Fatalf("%s", page)
	}
	page = welcomePage(t, StartConfig{Laib: fakeLaib(t, allGreen(t), nil)}).Body.String()
	if !strings.Contains(page, saysNoAppsYet) {
		t.Fatalf("nothing installed:\n%s", page)
	}
}

// Start sends a device's first visit to the welcome page, once; Start
// itself leads there quietly.
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
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "href=/willkommen>Apps einrichten") {
		t.Fatalf("seen: %d\n%s", rec.Code, rec.Body.String())
	}
}
