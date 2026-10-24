package pages

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mdopp/haus/internal/laib"
)

// slowLaib answers laib_status after delay, every time, and counts the asks.
func slowLaib(t *testing.T, delay time.Duration, ready map[string]any, asked *atomic.Int32) *laib.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

// ask is one request to the one Start handler h.
func ask(h http.HandlerFunc, target string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", target, nil)
	req.Header.Set("Remote-User", "anna")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// haus#67: Start opens as fast as the other tabs. laib's status takes a
// second; Start does not wait for it: the page comes at once with a place
// for the box line, which its script fetches from /start/box once laib has
// answered. The status is kept a while, so the next Start has it inline
// and laib is not asked again.
func TestStartDoesNotWaitForLaib(t *testing.T) {
	var asked atomic.Int32
	fake, hc := house(t)
	fake.Set("light.flur", "on", map[string]any{"friendly_name": "Flurlicht"})
	cfg := StartConfig{Laib: slowLaib(t, 800*time.Millisecond, allGreen(t), &asked), HA: hc, Loc: berlin(t)}
	now := startNow(t)
	cfg.Now = func() time.Time { return now }
	h := Start(cfg)

	start := time.Now()
	rec := ask(h, "/start")
	if d := time.Since(start); d > 600*time.Millisecond {
		t.Fatalf("Start waited %s for laib", d)
	}
	body := rec.Body.String()
	if rec.Code != 200 {
		t.Fatalf("→ %d", rec.Code)
	}
	mustContain(t, body, "karte-im-haus", "Flurlicht", "<div id=box-oben></div>", "<div id=box-unten>", saysBoxLooking,
		"fetch('/start/box?ruhig=1'", "Wie es der Box geht, lässt sich gerade nicht sagen.")
	mustNotContain(t, body, "Alles in Ordnung")
	before(t, body, "<div id=box-oben>", "karte-im-haus")
	before(t, body, "karte-im-haus", "<div id=box-unten>")

	// the script's fetch: the line once laib has answered — the same read
	rec = ask(h, StartBoxPath+"?ruhig=1")
	var line map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &line); err != nil || rec.Code != 200 || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("/start/box → %d %q: %v", rec.Code, rec.Body.String(), err)
	}
	if line["oben"] != "" || !strings.Contains(line["unten"], "Alles in Ordnung · eure Daten sind von gestern Abend gesichert") {
		t.Fatalf("/start/box: %v", line)
	}
	if n := asked.Load(); n != 1 {
		t.Fatalf("laib was asked %d times, want once for the page and its line", n)
	}
	// a part of the page that did not answer: no "Alles in Ordnung"
	rec = ask(h, StartBoxPath+"?ruhig=0")
	json.Unmarshal(rec.Body.Bytes(), &line)
	mustContain(t, line["unten"], "Eure Daten sind von gestern Abend gesichert")

	// the next Start has the kept status inline, at once, and asks nobody
	start = time.Now()
	body = ask(h, "/start").Body.String()
	if d := time.Since(start); d > 300*time.Millisecond {
		t.Fatalf("the second Start took %s", d)
	}
	mustContain(t, body, "Alles in Ordnung · eure Daten sind von gestern Abend gesichert")
	mustNotContain(t, body, "box-unten", StartBoxPath)
	if n := asked.Load(); n != 1 {
		t.Fatalf("laib was asked %d times", n)
	}

	// kept only statusFresh: then laib is asked again
	fresh := statusFresh
	statusFresh = 0
	defer func() { statusFresh = fresh }()
	ask(h, "/start")
	if n := asked.Load(); n != 2 {
		t.Fatalf("laib was asked %d times after the status went stale", n)
	}
}

// The box line fetched later says plainly what laib not answering means,
// like the page would have (#34); nothing configured, nothing to fetch.
// Anything else under /start is a 404 in the family's frame.
func TestStartBoxLineWhenLaibDoesNotAnswer(t *testing.T) {
	h := Start(StartConfig{Laib: brokenLaib(t), Loc: berlin(t)})
	rec := ask(h, StartBoxPath+"?ruhig=1")
	var line map[string]string
	json.Unmarshal(rec.Body.Bytes(), &line)
	if rec.Code != 200 || !strings.Contains(line["unten"], "Wie es der Box geht, lässt sich gerade nicht sagen.") {
		t.Fatalf("→ %d %v", rec.Code, line)
	}
	mustNotLeak(t, rec.Body.String(), "500", "laib")
	if rec := ask(Start(StartConfig{}), StartBoxPath); rec.Code != http.StatusNotFound {
		t.Fatalf("no laib: /start/box → %d", rec.Code)
	}
	if rec := ask(h, "/start/gibtsnicht"); rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), saysNoSuchPage) {
		t.Fatalf("/start/gibtsnicht → %d", rec.Code)
	}
}

// haus#67: Mehr holds the pages without a bar entry — each only when this
// box has it, Box for admins — and the account with Abmelden.
func TestMehrListsWhatItHolds(t *testing.T) {
	_, hc := house(t)
	nd := notesDir(t, map[string]time.Duration{"Einkaufsliste": time.Hour})
	_, body, _ := familyPage(t, Mehr, StartConfig{HA: hc, Notes: nd}, "GET", "/mehr", "family")
	page := body[strings.Index(body, "<h1>Mehr</h1>"):]
	mustContain(t, page, `<a href="/energie">Energie</a>`, `<a href="/medien">Medien</a>`, mehrSays["Energie"],
		"<h2 id=karte-konto>Konto</h2>", "Angemeldet als anna", `<a href="/abmelden">Abmelden</a>`)
	mustNotContain(t, page, "/box", "/netzlaufwerk", "/notizen")
	before(t, page, "/energie", "/medien")
	before(t, page, "/medien", "karte-konto")
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
