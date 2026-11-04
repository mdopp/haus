package pages

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mdopp/homes/internal/einrichten"
)

// tick is the person's „Ist eingerichtet“ on a module's block, from Homes' own page.
func tick(t *testing.T, cfg StartConfig, name, from string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", EinrichtenPages+name, strings.NewReader(url.Values{"zurueck": {from}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Remote-User", "anna")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	rec := httptest.NewRecorder()
	Einrichten(cfg).ServeHTTP(rec, req)
	return rec
}

// owner 2026-11-04: a module with its own app is a task, on Deine
// Aufgaben with the counter beside its heading, beside Mehr in the bar
// and beside Aufgaben in the list; the counter is the open rows' number.
func TestTheCounterIsTheOpenTasks(t *testing.T) {
	cfg := setupBox(t)
	_, body, _ := familyPage(t, DeineAufgaben, cfg, "GET", "/aufgaben", "family")
	mustContain(t, body, `<h1>Deine Aufgaben <span class=anzahl aria-label="2 offen">2</span></h1>`,
		`<a href="/einrichten/fotos">Fotos auf dem Handy einrichten</a>`, `<a href="/kalender#einrichten-kalender">Kalender auf dem Handy einrichten</a>`)
	open := body[strings.Index(body, "karte-offen"):]
	if n := strings.Count(open[:strings.Index(open, "</section>")], "<li>"); n != 2 {
		t.Errorf("%d open rows, the counter says 2", n)
	}
	nav := familyNav(httptest.NewRequest("GET", "/start", nil), cfg, "Start")
	mustNotContain(t, nav, "anzahl") // no one signed in: no counter
	req := httptest.NewRequest("GET", "/start", nil)
	req.Header.Set("Remote-User", "anna")
	nav = familyNav(req, cfg, "Start")
	mustContain(t, nav, `Mehr <span class=anzahl aria-label="2 offen">2</span></a>`, `Aufgaben <span class=anzahl aria-label="2 offen">2</span></a>`)
	_, body, _ = familyPage(t, Mehr, cfg, "GET", "/mehr", "family")
	mustContain(t, body, "2 offen · "+mehrSays["Aufgaben"])

	if rec := tick(t, cfg, "fotos", "/aufgaben"); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/aufgaben" {
		t.Fatalf("tick: %d %q %s", rec.Code, rec.Header().Get("Location"), rec.Body)
	}
	_, body, _ = familyPage(t, DeineAufgaben, cfg, "GET", "/aufgaben", "family")
	mustContain(t, body, `<span class=anzahl aria-label="1 offen">1</span></h1>`, "Noch einmal: Mit der App „Immich“.")
	nav = familyNav(req, cfg, "Start")
	mustContain(t, nav, `Mehr <span class=anzahl aria-label="1 offen">1</span></a>`)

	tick(t, cfg, "kalender", "/kalender")
	nav = familyNav(req, cfg, "Start")
	mustNotContain(t, nav, "anzahl")
}

// The person's tick: from Homes' own page only, back where it was
// pressed (a local path), and it holds; a module that is off is no task.
func TestTheTickByHand(t *testing.T) {
	cfg := setupBox(t)
	req := httptest.NewRequest("POST", EinrichtenPages+"fotos", nil)
	req.Header.Set("Remote-User", "anna")
	rec := httptest.NewRecorder()
	Einrichten(cfg).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("a POST from elsewhere: %d", rec.Code)
	}
	if rec := tick(t, cfg, "fotos", "//elsewhere.example"); rec.Header().Get("Location") != "/einrichten/fotos" {
		t.Errorf("back to another site: %q", rec.Header().Get("Location"))
	}
	if s := cfg.Einrichtung.Haken.Von("anna")["fotos"]; s.Wie != einrichten.Hand {
		t.Errorf("kept: %+v", s)
	}
	if rec := tick(t, cfg, "notizen", "/"); rec.Code != http.StatusNotFound {
		t.Errorf("a module without an app: %d", rec.Code)
	}
	cfg.Einrichtung.Haken = einrichten.Open(t.TempDir() + "/gibtsnicht")
	if rec := tick(t, cfg, "kalender", "/kalender"); rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "ließ sich gerade nicht abhaken") {
		t.Errorf("a tick that cannot be kept: %d %s", rec.Code, rec.Body)
	}
}

// owner 2026-11-04: on a module's own page its setup leads while it is
// open (the steps, the store, the address, the guide, the tick); once done
// it shrinks to one quiet line at the bottom, to set up another device.
func TestAModulePageBeforeAndAfterItsSetup(t *testing.T) {
	_, cal := calendar(t)
	cfg := setupBox(t)
	cfg.Calendar = cal
	_, body, _ := familyPage(t, Kalender, cfg, "GET", "/kalender", "")
	mustContain(t, body, `<section class="karte einrichten" id=einrichten-kalender><h2>Kalender auf dem Handy einrichten</h2>`,
		"Lade die App „DAVx⁵“ aufs Handy.", `href="https://play.google.com/store/apps/details?id=at.bitfire.davdroid">App laden</a>`,
		"<span class=adresse>https://caldav.box.test</span>", `href="https://manual.davx5.com/">Ausführliche Anleitung</a>`,
		`action=/einrichten/kalender><input type=hidden name=zurueck value="/kalender">`, `data-laeuft="Hakt ab …">Ist eingerichtet</button>`)
	before(t, body, "einrichten-kalender", "Heute")
	mustNotContain(t, body, "Auf einem weiteren Gerät einrichten")

	tick(t, cfg, "kalender", "/kalender")
	_, body, _ = familyPage(t, Kalender, cfg, "GET", "/kalender", "")
	mustContain(t, body, `<p class=leise-weg id=einrichten-kalender><a href=/einrichten/kalender>Auf einem weiteren Gerät einrichten</a></p>`)
	mustNotContain(t, body, "karte einrichten", "Ist eingerichtet")
	before(t, body, "Heute", "Auf einem weiteren Gerät")

	// its own page has the steps for the next device, no tick
	_, body, _ = familyPage(t, Einrichten, cfg, "GET", "/einrichten/kalender", "")
	mustContain(t, body, "<h1>Kalender auf dem Handy einrichten</h1>", "Lade die App „DAVx⁵“")
	mustNotContain(t, body, "Ist eingerichtet")
	if code, _, _ := familyPage(t, Einrichten, cfg, "GET", "/einrichten/gibtsnicht", ""); code != http.StatusNotFound {
		t.Errorf("/einrichten/gibtsnicht → %d", code)
	}
}

// A module that leaves the box takes its task along (owner 2026-11-04: its
// steps are gone with it); the others stay.
func TestARemovedModuleDropsItsTask(t *testing.T) {
	cfg := setupBox(t, "kalender", "notizen")
	_, body, _ := familyPage(t, DeineAufgaben, cfg, "GET", "/aufgaben", "family")
	mustContain(t, body, "Kalender auf dem Handy einrichten")
	mustNotContain(t, body, "Fotos", "Immich")
}
