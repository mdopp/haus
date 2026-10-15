package pages

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/haus/internal/caldav"
	"github.com/mdopp/haus/internal/caldav/davfake"
	"github.com/mdopp/haus/internal/cards"
	"github.com/mdopp/haus/internal/devices"
	"github.com/mdopp/haus/internal/favoriten"
	"github.com/mdopp/haus/internal/ha"
	"github.com/mdopp/haus/internal/ha/hafake"
	"github.com/mdopp/haus/internal/laib"
	"github.com/mdopp/haus/internal/notes"
	"github.com/mdopp/haus/internal/persona"
)

func berlin(t *testing.T) *time.Location {
	t.Helper()
	l, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// a Wednesday morning in Berlin
func startNow(t *testing.T) time.Time { return time.Date(2026, 10, 14, 10, 0, 0, 0, berlin(t)) }

func allGreen(t *testing.T) map[string]any {
	return map[string]any{"state": "bereit", "services": 1, "green": 1, "operator": "läuft",
		"lastBackup": map[string]any{"at": time.Now().Add(-3 * time.Hour).UTC().Format(time.RFC3339), "repos": 2, "ok": true}}
}

func startPage(t *testing.T, cfg StartConfig, groups string) (int, string) {
	t.Helper()
	return startPageAs(t, cfg, "anna", groups)
}

// startPageAs is Start for one person (Remote-User; "" for nobody).
func startPageAs(t *testing.T, cfg StartConfig, user, groups string) (int, string) {
	t.Helper()
	if cfg.Loc == nil {
		cfg.Loc = berlin(t)
	}
	if cfg.Now == nil {
		now := startNow(t)
		cfg.Now = func() time.Time { return now }
	}
	req := httptest.NewRequest("GET", "/start", nil)
	if user != "" {
		req.Header.Set("Remote-User", user)
	}
	if groups != "" {
		req.Header.Set("Remote-Groups", groups)
	}
	rec := httptest.NewRecorder()
	Start(cfg).ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func house(t *testing.T) (*hafake.HA, *ha.Client) {
	t.Helper()
	fake := hafake.New("ha-token")
	t.Cleanup(fake.Close)
	return fake, &ha.Client{URL: fake.URL, Token: "ha-token"}
}

func calendar(t *testing.T) (*davfake.Server, *caldav.Client) {
	t.Helper()
	fake := davfake.New("haus", "pw")
	t.Cleanup(fake.Close)
	fake.AddCalendar("/haus/familie/", "Haushalt")
	return fake, &caldav.Client{URL: fake.URL, User: "haus", Password: "pw", Loc: berlin(t)}
}

func event(uid, start, end, summary string) string {
	return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:" + uid + "\r\nDTSTART:" + start + "\r\nDTEND:" + end + "\r\nSUMMARY:" + summary + "\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
}

func todo(uid, summary, extra string) string {
	return "BEGIN:VCALENDAR\r\nBEGIN:VTODO\r\nUID:" + uid + "\r\nSUMMARY:" + summary + "\r\n" + extra + "END:VTODO\r\nEND:VCALENDAR\r\n"
}

func notesDir(t *testing.T, ns map[string]time.Duration) *notes.Dir {
	t.Helper()
	dir := t.TempDir()
	for title, age := range ns {
		p := filepath.Join(dir, notes.Slug(title)+".md")
		if err := os.WriteFile(p, []byte("# "+title+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		at := time.Now().Add(-age)
		if err := os.Chtimes(p, at, at); err != nil {
			t.Fatal(err)
		}
	}
	d, err := notes.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func mustContain(t *testing.T, body string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(body, w) {
			t.Errorf("start page lacks %q:\n%s", w, body)
		}
	}
}

func mustNotContain(t *testing.T, body string, nots ...string) {
	t.Helper()
	for _, n := range nots {
		if strings.Contains(body, n) {
			t.Errorf("start page shows %q:\n%s", n, body)
		}
	}
}

// haus#12: Jetzt — who is home, which lights are on, what plays, the next
// appointment — under the persona's header and the three guarantees.
func TestStartShowsJetzt(t *testing.T) {
	House = persona.Persona{Display: "Joshua", Greeting: map[string]string{"de": "Hallo, hier ist Joshua."},
		Colors: map[string]string{"accent": "#3ddc84", "ground": "#0c120e"}}
	defer func() { House = persona.Default() }()
	fake, hc := house(t)
	fake.Set("person.anna", "home", map[string]any{"friendly_name": "Anna"})
	fake.Set("person.ben", "not_home", map[string]any{"friendly_name": "Ben"})
	fake.Set("light.kueche", "on", map[string]any{"friendly_name": "Küche"})
	fake.Set("light.flur", "off", map[string]any{"friendly_name": "Flur"})
	fake.Set("light.bad", "on", map[string]any{"friendly_name": "Bad"})
	fake.Set("switch.kaffee", "on", map[string]any{"friendly_name": "Kaffeemaschine"})
	fake.Set("media_player.wohnzimmer", "playing", map[string]any{"friendly_name": "Wohnzimmer", "media_title": "Abbey Road", "media_artist": "The Beatles"})
	fake.Set("media_player.kueche", "paused", map[string]any{"friendly_name": "Küchenradio", "media_title": "Deutschlandfunk"})
	dav, cal := calendar(t)
	dav.Put("/haus/familie/1.ics", event("1", "20261013T080000Z", "20261013T090000Z", "Gestern vorbei"))
	dav.Put("/haus/familie/2.ics", event("2", "20261015T130000Z", "20261015T140000Z", "Zahnarzt"))
	dav.Put("/haus/familie/3.ics", event("3", "20261017T160000Z", "20261017T180000Z", "Elternabend"))
	code, body := startPage(t, StartConfig{Laib: fakeLaib(t, allGreen(t), []map[string]any{{"name": "immich", "state": "grün"}}), LaibURL: "https://laib.box.test",
		HA: hc, Calendar: cal}, "family")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	mustContain(t, body, "<title>Start · Joshua</title>", "Hallo, hier ist Joshua.", "#3ddc84", `lang=de`,
		"Alles läuft", "Letzte Sicherung",
		"<h2 id=karte-jetzt>Jetzt</h2>",
		"Zu Hause: Anna",
		"2 Lichter an", "Bad, Küche",
		"Wohnzimmer", "Abbey Road", "The Beatles",
		"Zahnarzt", "morgen 15:00")
	mustNotContain(t, body, "Ben", "Flur", "Kaffeemaschine", "Küchenradio", "Gestern vorbei", "Elternabend",
		"<form", "<button", "<script", "Haushalt</h2>")
	// Jetzt's order: who is home, lights, what plays, the next appointment (laib DEKONSTRUKTION)
	i := []int{strings.Index(body, "Zu Hause"), strings.Index(body, "Lichter an"), strings.Index(body, "Abbey Road"), strings.Index(body, "Zahnarzt")}
	for k := 1; k < len(i); k++ {
		if i[k] < i[k-1] {
			t.Fatalf("Jetzt out of order: %v", i)
		}
	}
	// the guarantees come before the cards
	if strings.Index(body, "Alles läuft") > strings.Index(body, "karte-jetzt") {
		t.Fatal("the guarantees belong on top")
	}
}

// Nobody home and every light off are said plainly; nothing playing and no
// person entities say nothing.
func TestStartSaysAllOffPlainly(t *testing.T) {
	fake, hc := house(t)
	fake.Set("person.anna", "not_home", map[string]any{"friendly_name": "Anna"})
	fake.Set("light.flur", "off", map[string]any{"friendly_name": "Flur"})
	fake.Set("media_player.wohnzimmer", "idle", map[string]any{"friendly_name": "Wohnzimmer"})
	_, body := startPage(t, StartConfig{HA: hc}, "")
	mustContain(t, body, "Niemand ist zu Hause", "Alle Lichter sind aus")
	mustNotContain(t, body, "Wohnzimmer", "Läuft")

	fake2, hc2 := house(t)
	fake2.Set("light.flur", "on", map[string]any{"friendly_name": "Flur"})
	_, body = startPage(t, StartConfig{HA: hc2}, "")
	mustContain(t, body, "1 Licht an", "Flur")
	mustNotContain(t, body, "Zu Hause", "Niemand")
}

// Many lights on: the names are cut, the count is not.
func TestStartCutsLongLightLists(t *testing.T) {
	fake, hc := house(t)
	for _, n := range []string{"A", "B", "C", "D", "E", "F", "G"} {
		fake.Set("light."+strings.ToLower(n), "on", map[string]any{"friendly_name": "Licht " + n})
	}
	_, body := startPage(t, StartConfig{HA: hc}, "")
	mustContain(t, body, "7 Lichter an", "Licht A", "Licht E", "und 2 weitere")
	mustNotContain(t, body, "Licht F", "Licht G")
}

// Haushalt: the next three open reminders and the three newest notes.
func TestStartShowsHaushalt(t *testing.T) {
	dav, cal := calendar(t)
	dav.Put("/haus/familie/a.ics", todo("a", "Müll rausbringen", "DUE:20261015T050000Z\r\n"))
	dav.Put("/haus/familie/b.ics", todo("b", "Geschenk für Oma", "DUE:20261016T070000Z\r\n"))
	dav.Put("/haus/familie/c.ics", todo("c", "Reifen wechseln", "DUE:20261017T070000Z\r\n"))
	dav.Put("/haus/familie/d.ics", todo("d", "Steuer", "DUE:20261030T070000Z\r\n"))
	dav.Put("/haus/familie/e.ics", todo("e", "Erledigt schon", "STATUS:COMPLETED\r\nDUE:20261014T070000Z\r\n"))
	nd := notesDir(t, map[string]time.Duration{"Einkaufsliste": time.Hour, "Packliste": 2 * time.Hour, "WLAN für Gäste": 3 * time.Hour, "Alte Notiz": 72 * time.Hour})
	_, body := startPage(t, StartConfig{Calendar: cal, Notes: nd}, "")
	mustContain(t, body, "<h2 id=karte-haushalt>Haushalt</h2>",
		"Müll rausbringen", "morgen 07:00", "Geschenk für Oma", "Reifen wechseln",
		"Einkaufsliste", "Packliste", "WLAN für Gäste")
	mustNotContain(t, body, "Steuer", "Erledigt schon", "Alte Notiz", "<form", "<button")
	if strings.Index(body, "Müll rausbringen") > strings.Index(body, "Geschenk für Oma") || strings.Index(body, "Einkaufsliste") > strings.Index(body, "Packliste") {
		t.Fatal("soonest reminder and newest note first")
	}
}

// What is not configured is not mentioned: no HA, no calendar, no notes —
// no cards, and no word about them.
func TestStartSaysNothingAboutWhatIsNotConfigured(t *testing.T) {
	code, body := startPage(t, StartConfig{Laib: fakeLaib(t, allGreen(t), nil), LaibURL: "https://laib.box.test"}, "")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	mustContain(t, body, "Alles läuft")
	mustNotContain(t, body, "karte-jetzt", "karte-haushalt", "Licht", "Termin", "Erinnerung", "Notiz", "nicht konfiguriert", "HAUS_")
}

// haus#12: a backend in trouble is said in one plain line where its card
// would be — never a blank card, never a 500, never its error or its
// name: the error goes to the log, not to the kitchen table.
func TestStartSaysPlainlyWhatDoesNotAnswer(t *testing.T) {
	fake, hc := house(t)
	fake.Set("light.flur", "on", map[string]any{"friendly_name": "Flur"})
	fake.Down(true)
	_, cal := calendar(t)
	cal.Password = "falsch"
	gone := t.TempDir()
	nd, err := notes.Open(gone)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	fav := favorites(t, `{"anna": ["light.flur"]}`)
	code, body := startPage(t, StartConfig{HA: hc, Calendar: cal, Notes: nd, Favorites: fav, Laib: brokenLaib(t)}, "family")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	mustContain(t, body,
		"Wie es der Box geht, lässt sich gerade nicht sagen.",
		"<h2 id=karte-jetzt>Jetzt</h2>", "Licht und Geräte antworten gerade nicht.", "Der Kalender antwortet gerade nicht.",
		"<h2 id=karte-meine-favoriten>Meine Favoriten</h2>", "Deine Favoriten sind gerade nicht zu sehen.",
		"<h2 id=karte-haushalt>Haushalt</h2>", "Die Notizen lassen sich gerade nicht lesen.")
	mustNotContain(t, body, "boom", "HA 500", "Anmeldung abgelehnt", "HTTP", "Flur", "class=rot", "connection refused",
		"Home Assistant", "Radicale", "CalDAV", "Laib", "favoriten.json", "no such file", "Alles läuft")
	// the calendar is said once, though both Jetzt and Haushalt read it
	if n := strings.Count(body, "Der Kalender antwortet gerade nicht."); n != 1 {
		t.Fatalf("the calendar said %d times", n)
	}
}

// Only the reminders failing (the appointments read) is said in Haushalt.
func TestStartSaysFailingRemindersInHaushalt(t *testing.T) {
	dav, cal := calendar(t)
	dav.Put("/haus/familie/2.ics", event("2", "20261015T130000Z", "20261015T140000Z", "Zahnarzt"))
	// in front of the fake: a reminders query answers 500, the rest passes
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Method == "REPORT" && strings.Contains(string(body), `name="VTODO"`) {
			http.Error(w, "REPORT 500", http.StatusInternalServerError)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		httputil.NewSingleHostReverseProxy(must(url.Parse(dav.URL))).ServeHTTP(w, r)
	}))
	t.Cleanup(front.Close)
	cal.URL = front.URL
	_, body := startPage(t, StartConfig{Calendar: cal}, "")
	mustContain(t, body, "Nächster Termin: Zahnarzt", "<h2 id=karte-haushalt>Haushalt</h2>", "Der Kalender antwortet gerade nicht.")
	mustNotContain(t, body, "REPORT", "500")
	if strings.Index(body, "Der Kalender antwortet") < strings.Index(body, "karte-haushalt") {
		t.Fatal("the reminders' line belongs in Haushalt")
	}
}

// Laib not answering is said without its error text.
func TestStartWithoutLaib(t *testing.T) {
	code, body := startPage(t, StartConfig{Laib: brokenLaib(t)}, "")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	mustContain(t, body, "Wie es der Box geht, lässt sich gerade nicht sagen.")
	mustNotContain(t, body, "Alles läuft", "connection refused", "500")
}

// haus#1: the Start page changes nothing; a POST is refused.
func TestStartDecidesNothing(t *testing.T) {
	rec := httptest.NewRecorder()
	Start(StartConfig{}).ServeHTTP(rec, httptest.NewRequest("POST", "/start", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /start → %d, want 405", rec.Code)
	}
}

// Admins find the Box page from Start; the family does not see the link.
func TestStartLinksTheBoxForAdmins(t *testing.T) {
	_, body := startPage(t, StartConfig{}, "admins,family")
	mustContain(t, body, `href=/box`)
	_, body = startPage(t, StartConfig{}, "family")
	mustNotContain(t, body, `href=/box`)
}

// haus#12: "/" and every path Haus does not serve land on Start, never on
// an error.
func TestEverythingElseLandsOnStart(t *testing.T) {
	for _, p := range []string{"/", "/download", "/p/start", "/unbekannt"} {
		rec := httptest.NewRecorder()
		ToStart().ServeHTTP(rec, httptest.NewRequest("GET", p, nil))
		if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/start" {
			t.Fatalf("%s → %d %q, want 302 /start", p, rec.Code, rec.Header().Get("Location"))
		}
	}
}

// brokenLaib answers every call with a 500.
func brokenLaib(t *testing.T) *laib.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "500: boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	return &laib.Client{URL: srv.URL, Token: "t"}
}

// haus#12: Box is the admins' page (laib DEKONSTRUKTION). Anyone else
// asking for /box or /box/… is sent to Start (303) — who sees the admin
// page, not a verdict on anything. Pairing from the phone is untouched.
func TestBoxIsForAdmins(t *testing.T) {
	store, _ := devices.Open(t.TempDir())
	box := Box(fakeLaib(t, allGreen(t), nil), "https://laib.box.test")
	geraete := Geraete(store, "https://haus.example")
	for _, c := range []struct {
		h      http.HandlerFunc
		method string
		path   string
	}{{box, "GET", "/box"}, {box, "GET", "/box/inbox"}, {geraete, "GET", "/box/geraete"}, {geraete, "POST", "/box/geraete"}} {
		for _, groups := range []string{"", "family", "administrators", "family,admins-light"} {
			req := httptest.NewRequest(c.method, c.path, strings.NewReader("name=Pixel"))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Remote-User", "ben")
			if groups != "" {
				req.Header.Set("Remote-Groups", groups)
			}
			rec := httptest.NewRecorder()
			c.h.ServeHTTP(rec, req)
			if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/start" {
				t.Fatalf("%s %s as %q → %d %q, want 303 /start", c.method, c.path, groups, rec.Code, rec.Header().Get("Location"))
			}
		}
	}
	if ds := store.List(); len(ds) != 0 {
		t.Fatalf("a non-admin minted a device: %v", ds)
	}
	for _, c := range []struct {
		h    http.HandlerFunc
		path string
	}{{box, "/box"}, {geraete, "/box/geraete"}} {
		req := httptest.NewRequest("GET", c.path, nil)
		req.Header.Set("Remote-User", "anna")
		req.Header.Set("Remote-Groups", "family,admins")
		rec := httptest.NewRecorder()
		c.h.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("an admin on %s → %d", c.path, rec.Code)
		}
	}
	// the phone's own pairing path is for everyone
	req := httptest.NewRequest("GET", "/pair-device", nil)
	req.Header.Set("Remote-User", "ben")
	req.Header.Set("Remote-Groups", "family")
	rec := httptest.NewRecorder()
	PairDevice(store).ServeHTTP(rec, req)
	if rec.Code == http.StatusSeeOther || rec.Code == http.StatusFound {
		t.Fatalf("pairing redirected a family member: %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

// A group (an entity carrying an entity_id list, light groups included)
// is not a lamp: only real lamps are counted.
func TestStartCountsLampsNotGroups(t *testing.T) {
	fake, hc := house(t)
	fake.Set("light.kueche", "on", map[string]any{"friendly_name": "Küche"})
	fake.Set("light.erdgeschoss", "on", map[string]any{"friendly_name": "Erdgeschoss", "entity_id": []any{"light.kueche", "light.flur"}})
	fake.Set("light.flur", "off", map[string]any{"friendly_name": "Flur"})
	_, body := startPage(t, StartConfig{HA: hc}, "")
	mustContain(t, body, "1 Licht an", "Küche")
	mustNotContain(t, body, "2 Lichter an", "Erdgeschoss")
}

// haus#12: Start's cards lead to the family's pages — Jetzt to Räume, an
// appointment and a reminder to Kalender, a note to its own page — and
// only to pages that are there.
func TestStartLinksToTheFamilyPages(t *testing.T) {
	fake, hc := house(t)
	fake.Set("light.flur", "on", map[string]any{"friendly_name": "Flur"})
	dav, cal := calendar(t)
	dav.Put("/haus/familie/2.ics", event("2", "20261015T130000Z", "20261015T140000Z", "Zahnarzt"))
	dav.Put("/haus/familie/a.ics", todo("a", "Müll rausbringen", "DUE:20261015T050000Z\r\n"))
	nd := notesDir(t, map[string]time.Duration{"Einkaufsliste": time.Hour})
	_, body := startPage(t, StartConfig{HA: hc, Calendar: cal, Notes: nd}, "")
	mustContain(t, body, `<a href="/raeume">Alle Räume</a>`,
		`<a href="/kalender">Nächster Termin: Zahnarzt</a>`,
		`<a href="/kalender">Müll rausbringen</a>`,
		`<a href="/notizen/einkaufsliste.md">Einkaufsliste</a>`,
		`<a href="/notizen">Alle Notizen</a>`)

	// only HA: Jetzt leads to Räume, nothing leads to a calendar or notes
	_, body = startPage(t, StartConfig{HA: hc}, "")
	mustContain(t, body, `href="/raeume"`)
	mustNotContain(t, body, "/kalender", "/notizen")
}

// What HA hides is not counted or named on Start: a hidden lamp is not on,
// a config LED is no light, a diagnostic player plays nothing.
func TestStartLeavesOutWhatHAHides(t *testing.T) {
	fake, hc := house(t)
	fake.SetRegistries(nil, nil, []map[string]any{
		{"entity_id": "light.alt", "hidden_by": "user"},
		{"entity_id": "light.status_led", "entity_category": "config"},
		{"entity_id": "media_player.diag", "entity_category": "diagnostic"},
	})
	fake.Set("light.kueche", "on", map[string]any{"friendly_name": "Küche"})
	fake.Set("light.alt", "on", map[string]any{"friendly_name": "Alte Lampe"})
	fake.Set("light.status_led", "on", map[string]any{"friendly_name": "Status-LED"})
	fake.Set("media_player.diag", "playing", map[string]any{"friendly_name": "Diagnoseplayer", "media_title": "Test"})
	_, body := startPage(t, StartConfig{HA: hc}, "")
	mustContain(t, body, "1 Licht an", "Küche")
	mustNotContain(t, body, "Alte Lampe", "Status-LED", "Diagnoseplayer", "Lichter an")
}

// favorites is a favorites store holding content ("" for no file).
func favorites(t *testing.T, content string) *favoriten.Store {
	t.Helper()
	dir := t.TempDir()
	if content != "" {
		if err := os.WriteFile(filepath.Join(dir, favoriten.File), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return favoriten.Open(dir)
}

// haus#12, laib ADR 0017: Meine Favoriten — the person's own things, in
// their order, each with its state in words and leading to its own page;
// between Jetzt and Haushalt (laib DEKONSTRUKTION).
func TestStartShowsMyFavorites(t *testing.T) {
	fake, hc := house(t)
	fake.SetRegistries(nil, nil, []map[string]any{{"entity_id": "light.alt", "hidden_by": "user"}})
	fake.Set("cover.wohnzimmer", "open", map[string]any{"friendly_name": "Rollladen Wohnzimmer", "current_position": 40.0})
	fake.Set("light.buero", "on", map[string]any{"friendly_name": "Bürolicht", "brightness": 128.0})
	fake.Set("light.alt", "on", map[string]any{"friendly_name": "Alte Lampe"})
	fake.Set("light.flur", "off", map[string]any{"friendly_name": "Flurlicht"})
	fake.Set("person.ben", "home", map[string]any{"friendly_name": "Ben"})
	fake.Set("climate.bad", "heat", map[string]any{"friendly_name": "Heizung Bad", "current_temperature": 21.5, "temperature": 23.0})
	fav := favorites(t, `{"anna": ["cover.wohnzimmer", "light.buero", "light.weg", "person.ben", "light.alt", "climate.bad"], "ben": ["light.flur"]}`)
	nd := notesDir(t, map[string]time.Duration{"Einkaufsliste": time.Hour})
	_, body := startPage(t, StartConfig{HA: hc, Notes: nd, Favorites: fav}, "family")
	mustContain(t, body, "<h2 id=karte-meine-favoriten>Meine Favoriten</h2>",
		`<a href="/raeume/cover.wohnzimmer">Rollladen Wohnzimmer</a> <span class=k>offen · 40 %</span>`,
		`<a href="/raeume/light.buero">Bürolicht</a> <span class=k>an · 50 %</span>`,
		`<a href="/raeume/climate.bad">Heizung Bad</a> <span class=k>21,5 °C · Soll 23 °C</span>`)
	// what is not there, not a card's thing, or hidden by HA is left out; Ben's are Ben's
	mustNotContain(t, body, "light.weg", "/raeume/person.ben", "Alte Lampe", "Flurlicht")
	fav0 := strings.Index(body, "karte-meine-favoriten")
	favs := body[fav0:] // Jetzt names the lamp that is on, too
	if strings.Index(favs, "Rollladen Wohnzimmer") > strings.Index(favs, "Bürolicht") || strings.Index(favs, "Bürolicht") > strings.Index(favs, "Heizung Bad") {
		t.Fatal("the favorites keep the person's order")
	}
	if !(strings.Index(body, "karte-jetzt") < fav0 && fav0 < strings.Index(body, "karte-haushalt")) {
		t.Fatal("Jetzt, then Meine Favoriten, then Haushalt")
	}

	_, body = startPageAs(t, StartConfig{HA: hc, Favorites: fav}, "ben", "family")
	mustContain(t, body, `<a href="/raeume/light.flur">Flurlicht</a> <span class=k>aus</span>`)
	mustNotContain(t, body, `href="/raeume/light.buero"`, "Rollladen") // Jetzt still names the lamp that is on
}

// Without a person (no Remote-User), without favorites or without a store
// there is no favorites card and no word about it.
func TestStartWithoutFavoritesSaysNothing(t *testing.T) {
	fake, hc := house(t)
	fake.Set("light.buero", "on", map[string]any{"friendly_name": "Bürolicht"})
	for name, c := range map[string]struct {
		fav  *favoriten.Store
		user string
	}{
		"no store":       {nil, "anna"},
		"no file":        {favorites(t, ""), "anna"},
		"an empty list":  {favorites(t, `{"anna": []}`), "anna"},
		"someone else's": {favorites(t, `{"ben": ["light.buero"]}`), "anna"},
		"nobody":         {favorites(t, `{"anna": ["light.buero"]}`), ""},
		"only unknowns":  {favorites(t, `{"anna": ["light.weg"]}`), "anna"},
	} {
		code, body := startPageAs(t, StartConfig{HA: hc, Favorites: c.fav}, c.user, "")
		if code != 200 {
			t.Fatalf("%s: status %d", name, code)
		}
		if strings.Contains(body, "Favoriten") {
			t.Errorf("%s: the page talks about favorites:\n%s", name, body)
		}
	}
	// favorites without the house configured: nothing to draw them from, nothing said
	_, body := startPage(t, StartConfig{Favorites: favorites(t, `{"anna": ["light.buero"]}`)}, "")
	mustNotContain(t, body, "Favoriten")
}

// A favorites file that does not read is said plainly, not as "none".
func TestStartSaysABrokenFavoritesFilePlainly(t *testing.T) {
	fake, hc := house(t)
	fake.Set("light.buero", "on", map[string]any{"friendly_name": "Bürolicht"})
	code, body := startPage(t, StartConfig{HA: hc, Favorites: favorites(t, `{"anna": [`)}, "")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	mustContain(t, body, "<h2 id=karte-meine-favoriten>Meine Favoriten</h2>", "Deine Favoriten lassen sich gerade nicht lesen.")
	mustNotContain(t, body, "unexpected", "JSON", "favoriten.json")
}

// Many favorites: the first twelve, and how many more.
func TestStartCutsLongFavoriteLists(t *testing.T) {
	fake, hc := house(t)
	var ids []string
	for i := 1; i <= 15; i++ {
		id := fmt.Sprintf("light.l%02d", i)
		fake.Set(id, "off", map[string]any{"friendly_name": fmt.Sprintf("Lampe %02d", i)})
		ids = append(ids, `"`+id+`"`)
	}
	_, body := startPage(t, StartConfig{HA: hc, Favorites: favorites(t, `{"anna": [`+strings.Join(ids, ",")+`]}`)}, "")
	mustContain(t, body, "Lampe 01", "Lampe 12", "und 3 weitere")
	mustNotContain(t, body, "Lampe 13", "Lampe 15")
}

// The family reads Start on a phone: the page scales to the screen and a
// long name breaks inside its card instead of pushing the page sideways.
func TestStartFitsAPhone(t *testing.T) {
	fake, hc := house(t)
	fake.Set("light.lang", "on", map[string]any{"friendly_name": strings.Repeat("Wohnzimmerdeckenfluter", 3)})
	_, body := startPage(t, StartConfig{HA: hc}, "")
	mustContain(t, body, "width=device-width", cards.Style)
	mustNotContain(t, body, "<script", "<table")
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
