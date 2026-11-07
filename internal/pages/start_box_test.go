package pages

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mdopp/homes/internal/laib"
)

// slowLaib answers laib_status after delay, every time, and counts those
// asks; anything else it refuses at once.
func slowLaib(t *testing.T, delay time.Duration, ready map[string]any, asked *atomic.Int32) *laib.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"laib_status"`) {
			http.Error(w, "nein", http.StatusInternalServerError)
			return
		}
		asked.Add(1)
		select {
		case <-time.After(delay):
		case <-r.Context().Done():
			return
		}
		b, _ := json.Marshal(map[string]any{"ready": ready, "services": []map[string]any{{"name": "immich", "state": "grün"}}})
		json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"content": []map[string]string{{"text": string(b)}}}})
	}))
	t.Cleanup(srv.Close)
	return &laib.Client{URL: srv.URL, Token: "t"}
}

// ask is one request to the one Start handler h, as anna of groups.
func ask(h http.HandlerFunc, target string, groups ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", target, nil)
	req.Header.Set("Remote-User", "anna")
	if len(groups) > 0 {
		req.Header.Set("Remote-Groups", groups[0])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// haus#67: Start opens as fast as the other tabs. laib's status takes a
// second; Start does not wait for it: the page comes at once with Braucht
// dich's place, which fills from /start/box once laib has answered. The
// status is kept a while, so the next Start has it at once and laib is not
// asked again.
func TestStartDoesNotWaitForLaib(t *testing.T) {
	var asked atomic.Int32
	fake, hc := house(t)
	fake.Set("light.flur", "on", map[string]any{"friendly_name": "Flurlicht"})
	down := readyAt(t, time.Time{}, "fehlt") // never backed up: the family's banner
	cfg := StartConfig{Laib: slowLaib(t, 800*time.Millisecond, down, &asked), HA: hc, Loc: berlin(t)}
	now := startNow(t)
	cfg.Now = func() time.Time { return now }
	h := Start(cfg)

	start := time.Now()
	rec := ask(h, "/start")
	if d := time.Since(start); d > 150*time.Millisecond { // owner 2026-11-03: not even briefly
		t.Fatalf("Start waited %s for laib", d)
	}
	body := rec.Body.String()
	if rec.Code != 200 {
		t.Fatalf("→ %d", rec.Code)
	}
	mustContain(t, body, "karte-im-haus", "Flurlicht", `<div data-nachladen="/start/box"`)
	mustNotContain(t, body, "Eure Daten sind noch nicht gesichert.")
	before(t, body, "data-nachladen", "karte-im-haus")

	// the place's fetch: the card once laib has answered — the same read
	rec = ask(h, StartBoxPath)
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("/start/box → %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	mustContain(t, rec.Body.String(), `<section class="achtung hilfe"`, "Eure Daten sind noch nicht gesichert.")
	if n := asked.Load(); n != 1 {
		t.Fatalf("laib was asked %d times, want once for the page and its card", n)
	}

	// the next Start has the kept status at once and asks nobody
	start = time.Now()
	body = ask(h, "/start").Body.String()
	if d := time.Since(start); d > 300*time.Millisecond {
		t.Fatalf("the second Start took %s", d)
	}
	mustContain(t, body, "Eure Daten sind noch nicht gesichert.")
	if n := asked.Load(); n != 1 {
		t.Fatalf("laib was asked %d times", n)
	}

	// fresh only statusFresh: then the kept status is shown at once and
	// laib is asked again behind the page
	fresh := statusFresh
	statusFresh = 0
	defer func() { statusFresh = fresh }()
	start = time.Now()
	body = ask(h, "/start").Body.String()
	if d := time.Since(start); d > 150*time.Millisecond {
		t.Fatalf("Start with a stale status took %s", d)
	}
	mustContain(t, body, "Eure Daten sind noch nicht gesichert.")
	for deadline := time.Now().Add(2 * time.Second); asked.Load() < 2 && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
	}
	if n := asked.Load(); n != 2 {
		t.Fatalf("laib was asked %d times after the status went stale", n)
	}
}

// Laib not answering: the family is told nothing (nothing for them to do),
// an admin one plain row without the error; nothing configured, nothing
// to fetch. Anything else under /start is a 404 in the family's frame.
func TestStartBoxLineWhenLaibDoesNotAnswer(t *testing.T) {
	h := Start(StartConfig{Laib: brokenLaib(t), Loc: berlin(t)})
	if rec := ask(h, StartBoxPath); rec.Code != 200 || rec.Body.String() != "" {
		t.Fatalf("family → %d %q", rec.Code, rec.Body.String())
	}
	rec := ask(h, StartBoxPath, "admins")
	mustContain(t, rec.Body.String(), "Braucht dich", saysBoxSilent)
	mustNotLeak(t, rec.Body.String(), "500", "laib", "boom")
	if rec := ask(Start(StartConfig{}), StartBoxPath); rec.Code != http.StatusNotFound {
		t.Fatalf("no laib: /start/box → %d", rec.Code)
	}
	if rec := ask(h, "/start/gibtsnicht"); rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), saysNoSuchPage) {
		t.Fatalf("/start/gibtsnicht → %d", rec.Code)
	}
}

// owner 2026-11-07: Braucht dich is one card, only when something is open —
// the person's own Box-Aufgaben, for an admin the box's red and yellow (a
// service and a finding beyond it), the Box-Aufgaben due and the decisions
// waiting on Laib's page; never an optional app; all well, no card.
func TestStartBrauchtDich(t *testing.T) {
	immichRot := []map[string]any{{"name": "immich", "title": "Immich", "beschreibung": "Fotos", "state": "rot"}, {"name": "paperless", "state": "grün"}}
	ready := allGreen(t)
	ready["aufgabenFaellig"] = 1
	more := map[string]string{
		"laib_befunde": `{"at":"2026-11-06T10:00:00Z","findings":[{"severity":"gelb","probe":"cert","say":"Über die IP-Adresse warnt der Browser."},{"severity":"rot","probe":"immich","service":"immich"},{"severity":"info","probe":"watchdog"}]}`,
		"laib_inbox":   `[{"id":412,"reason":"Immich: neue Version","status":"open"}]`,
		"laib_aufgabe": `{"offen":2,"aufgaben":[{"id":7,"titel":"Signal-Nummer eintragen","fuer":"person:anna","weg":{"seite":"/mehr"}},{"id":8,"titel":"Für alle","fuer":"alle"}]}`,
	}
	cfg := StartConfig{Laib: fakeLaibWith(t, ready, immichRot, more), LaibURL: "https://laib.box.test/", Loc: berlin(t)}
	now := startNow(t)
	cfg.Now = func() time.Time { return now }
	h := Start(cfg)

	admin := ask(h, StartBoxPath, "family,admins").Body.String()
	c := section(t, admin, "karte-braucht-dich")
	mustContain(t, c, `<a href="/mehr">Signal-Nummer eintragen</a>`, `<a href="/box">Box: 1 rot · 1 gelb · 1 Box-Aufgabe fällig</a>`,
		`<a href="https://laib.box.test/inbox">1 Entscheidung wartet</a>`)
	mustNotContain(t, admin, "Für alle", "watchdog", "achtung", "Über die IP-Adresse")
	before(t, c, "Signal-Nummer", "Box:")
	before(t, c, "Box:", "Entscheidung")

	// the family: their own task, and the service down as the box's banner
	fam := ask(h, StartBoxPath, "family").Body.String()
	mustContain(t, fam, "Signal-Nummer eintragen", "Gerade nicht erreichbar: Immich (Fotos).")
	mustNotContain(t, fam, "Box:", "Entscheidung", "Box-Aufgabe")

	// all well, nothing of one's own: nothing at all
	calm := Start(StartConfig{Laib: fakeLaib(t, allGreen(t), nil), Loc: berlin(t), Now: cfg.Now})
	for _, g := range []string{"family", "family,admins"} {
		if b := ask(calm, StartBoxPath, g).Body.String(); b != "" {
			t.Errorf("%s, all well: %q", g, b)
		}
	}
}

// haus#67, owner 2026-10-24: Mehr holds the settings — Aufgaben (the
// guides of Apps einrichten went to the modules, 2026-11-04), the Netzlaufwerk password, for admins Box and Adresse — each only when
// this box has it, and the account with Abmelden; nothing used day to day.
func TestMehrListsWhatItHolds(t *testing.T) {
	_, hc := house(t)
	nd := notesDir(t, map[string]time.Duration{"Einkaufsliste": time.Hour})
	_, body, _ := familyPage(t, Mehr, StartConfig{HA: hc, Notes: nd, Laib: fakeLaib(t, allGreen(t), nil)}, "GET", "/mehr", "family")
	page := body[strings.Index(body, "<h1>Mehr</h1>"):]
	mustContain(t, page, `<a href="/aufgaben">Deine Aufgaben</a>`, mehrSays["Deine Aufgaben"],
		"<h2 id=karte-konto>Konto</h2>", "Angemeldet als anna", `<a href="/abmelden">Abmelden</a>`)
	mustNotContain(t, page, "/box", "/netzlaufwerk", "/energie", "/medien", "/notizen", "/dokumente", "/adresse", "/willkommen")
	before(t, page, "/aufgaben", "karte-konto")
	_, body, _ = familyPage(t, Mehr, StartConfig{}, "GET", "/mehr", "family,admins")
	page = body[strings.Index(body, "<h1>Mehr</h1>"):]
	mustContain(t, page, `<a href="/box">Box</a>`, "/abmelden")
	mustNotContain(t, page, "/energie", "/medien", "/notizen")
	if code, _, _ := familyPage(t, Mehr, StartConfig{}, "POST", "/mehr", "family"); code != http.StatusMethodNotAllowed {
		t.Errorf("POST /mehr → %d", code)
	}
	if code, _, _ := familyPage(t, Mehr, StartConfig{}, "GET", "/mehr/x", "family"); code != http.StatusNotFound {
		t.Errorf("/mehr/x → %d", code)
	}
}
