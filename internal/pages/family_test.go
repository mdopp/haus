package pages

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/haus/internal/caldav"
	"github.com/mdopp/haus/internal/notes"
	"github.com/mdopp/haus/internal/persona"
)

// familyPage asks one of the family's pages, with Start's test clock.
func familyPage(t *testing.T, page func(StartConfig) http.HandlerFunc, cfg StartConfig, method, target, groups string) (int, string, http.Header) {
	t.Helper()
	if cfg.Loc == nil {
		cfg.Loc = berlin(t)
	}
	if cfg.Now == nil {
		now := startNow(t)
		cfg.Now = func() time.Time { return now }
	}
	req := httptest.NewRequest(method, target, nil)
	req.Header.Set("Remote-User", "anna")
	if groups != "" {
		req.Header.Set("Remote-Groups", groups)
	}
	rec := httptest.NewRecorder()
	page(cfg).ServeHTTP(rec, req)
	return rec.Code, rec.Body.String(), rec.Header()
}

// before says a comes before b in body, both present.
func before(t *testing.T, body, a, b string) {
	t.Helper()
	i, j := strings.Index(body, a), strings.Index(body, b)
	if i < 0 || j < 0 || i > j {
		t.Errorf("%q (%d) should come before %q (%d):\n%s", a, i, b, j, body)
	}
}

// haus#12, haus#35: one tab bar on every family page, the item pages too
// — Start · Räume · Kalender · Notizen, each with its icon and only when
// its backend is set, Box for admins — with the page itself marked.
func TestFamilyTabBar(t *testing.T) {
	fake, hc := house(t)
	fake.Set("light.flur", "on", map[string]any{"friendly_name": "Flurlicht"})
	_, cal := calendar(t)
	nd := notesDir(t, map[string]time.Duration{"Einkaufsliste": time.Hour})
	all := StartConfig{HA: hc, Calendar: cal, Notes: nd}
	for _, p := range []struct {
		page func(StartConfig) http.HandlerFunc
		path string
		self string
	}{{Start, "/start", "/start"}, {Raeume, "/raeume", "/raeume"}, {Kalender, "/kalender", "/kalender"}, {Notizen, "/notizen", "/notizen"},
		{Ding, "/raeume/light.flur", "/raeume"}, {Ding, "/raeume/light.gibtsnicht", "/raeume"}, {Notizen, "/notizen/einkaufsliste.md", "/notizen"}} {
		code, body, _ := familyPage(t, p.page, all, "GET", p.path, "family")
		if code != 200 && code != 404 {
			t.Fatalf("%s → %d", p.path, code)
		}
		mustContain(t, body, "<nav class=tabs aria-label=Seiten>", "href=/start", "href=/raeume", "href=/kalender", "href=/notizen",
			"href="+p.self+" aria-current=page>", "<svg")
		if n := strings.Count(body, "aria-current=page"); n != 1 {
			t.Errorf("%s: %d tabs marked", p.path, n)
		}
		mustNotContain(t, body, "href=/box")
		before(t, body, "href=/start", "href=/raeume")
		before(t, body, "href=/raeume", "href=/kalender")
		before(t, body, "href=/kalender", "href=/notizen")
		_, body, _ = familyPage(t, p.page, all, "GET", p.path, "family,admins")
		mustContain(t, body, "href=/box")
	}
	// nothing set: Start alone, and no word about the rest
	_, body, _ := familyPage(t, Start, StartConfig{}, "GET", "/start", "family")
	mustContain(t, body, "href=/start aria-current=page>")
	mustNotContain(t, body, "href=/raeume", "href=/kalender", "href=/notizen", "Räume", "Kalender", "Notizen")
}

// A page whose backend is not set is not served: it lands on Start, like
// every path Haus does not serve.
func TestFamilyPagesWithoutBackendLandOnStart(t *testing.T) {
	for _, p := range []struct {
		page func(StartConfig) http.HandlerFunc
		path string
	}{{Raeume, "/raeume"}, {Kalender, "/kalender"}, {Notizen, "/notizen"}, {Notizen, "/notizen/einkaufsliste.md"},
		{Ding, "/raeume/light.flur"}, {Erinnerung, "/kalender/erinnerung/a"}, {Kontakt, "/kontakte/a"}, {Dokument, "/dokumente/2"}} {
		code, _, h := familyPage(t, p.page, StartConfig{}, "GET", p.path, "family")
		if code != http.StatusFound || h.Get("Location") != "/start" {
			t.Fatalf("%s without its backend → %d %q, want 302 /start", p.path, code, h.Get("Location"))
		}
	}
}

// haus#1: the family's pages show, they change nothing.
func TestFamilyPagesDecideNothing(t *testing.T) {
	_, hc := house(t)
	_, cal := calendar(t)
	nd := notesDir(t, map[string]time.Duration{"Einkaufsliste": time.Hour})
	all := StartConfig{HA: hc, Calendar: cal, Notes: nd}
	for _, p := range []struct {
		page func(StartConfig) http.HandlerFunc
		path string
	}{{Raeume, "/raeume"}, {Kalender, "/kalender"}, {Notizen, "/notizen"}, {Notizen, "/notizen/einkaufsliste.md"}} {
		for _, m := range []string{"POST", "PUT", "DELETE"} {
			if code, _, _ := familyPage(t, p.page, all, m, p.path, "admins"); code != http.StatusMethodNotAllowed {
				t.Fatalf("%s %s → %d, want 405", m, p.path, code)
			}
		}
	}
}

// haus#35: the persona speaks where you talk to it, not on the pages: no
// greeting, name, icon or colours on any family page. The pages wear the
// house palette (the chat's tokens, haus-android's colors.xml), light
// or dark as the phone is, and Haus's own Geist — never a font from
// Google.
func TestFamilyPagesWearTheHousePaletteNotThePersona(t *testing.T) {
	House = persona.Persona{Display: "Joshua", Greeting: map[string]string{"de": "Wollen wir ein Spiel spielen?"},
		Colors: map[string]string{"accent": "#3ddc84", "ground": "#0c120e"}, Icon: []byte("<svg/>")}
	defer func() { House = persona.Default() }()
	fake, hc := house(t)
	fake.Set("light.flur", "on", map[string]any{"friendly_name": "Flurlicht"})
	_, cal := calendar(t)
	nd := notesDir(t, map[string]time.Duration{"Einkaufsliste": time.Hour})
	all := StartConfig{HA: hc, Calendar: cal, Notes: nd}
	for _, p := range []struct {
		page  func(StartConfig) http.HandlerFunc
		path  string
		title string
	}{{Start, "/start", "Zuhause"}, {Raeume, "/raeume", "Räume · Zuhause"}, {Kalender, "/kalender", "Kalender · Zuhause"},
		{Notizen, "/notizen", "Notizen · Zuhause"}, {Ding, "/raeume/light.flur", "Flurlicht · Zuhause"}} {
		_, body, h := familyPage(t, p.page, all, "GET", p.path, "")
		mustContain(t, body, "<title>"+p.title+"</title>", "lang=de", "<h1>",
			"#070709", "#14141a", "#f97316", "#ffc107", "prefers-color-scheme:light", "color-scheme",
			"@font-face", FontPath, "system-ui")
		mustNotContain(t, body, "Joshua", "Wollen wir", "#3ddc84", "#0c120e", "/persona/icon.svg", "<header>", "class=sr>",
			"<script", "googleapis", "gstatic", "#faf9f6")
		if h.Get("Content-Type") != "text/html; charset=utf-8" {
			t.Fatalf("%s: %q", p.path, h.Get("Content-Type"))
		}
	}
}

// haus#35: Haus serves Geist itself (OFL, vendored), cached for good: the
// family's box asks no one else for a font.
func TestHausServesItsFont(t *testing.T) {
	rec := httptest.NewRecorder()
	Font().ServeHTTP(rec, httptest.NewRequest("GET", FontPath, nil))
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "font/woff2" || !strings.HasPrefix(rec.Body.String(), "wOF2") ||
		!strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("%d %q %q %d bytes", rec.Code, rec.Header().Get("Content-Type"), rec.Header().Get("Cache-Control"), rec.Body.Len())
	}
	if rec.Body.Len() > 64<<10 {
		t.Fatalf("the font is %d bytes: keep it a small subset", rec.Body.Len())
	}
	for _, m := range []string{"POST", "DELETE"} {
		rec = httptest.NewRecorder()
		Font().ServeHTTP(rec, httptest.NewRequest(m, FontPath, nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s → %d", m, rec.Code)
		}
	}
	rec = httptest.NewRecorder()
	Font().ServeHTTP(rec, httptest.NewRequest("GET", "/schrift/andere.woff2", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("another font → %d", rec.Code)
	}
}

// A backend that hangs is waited for backendWait, then left out: the page
// still comes, with what the others had.
func TestFamilyPagesDoNotWaitForAHangingBackend(t *testing.T) {
	old := backendWait
	backendWait = 200 * time.Millisecond
	defer func() { backendWait = old }()
	release := make(chan struct{})
	hang := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer hang.Close()
	defer close(release)
	cal := &caldav.Client{URL: hang.URL, User: "haus", Password: "pw", Loc: berlin(t)}
	nd := notesDir(t, map[string]time.Duration{"Einkaufsliste": time.Hour})
	for _, p := range []struct {
		page func(StartConfig) http.HandlerFunc
		path string
		want string
	}{{Start, "/start", "<h1>Zuhause</h1>"}, {Kalender, "/kalender", "<h1>Kalender</h1>"}} {
		began := time.Now()
		code, body, _ := familyPage(t, p.page, StartConfig{Calendar: cal, Notes: nd}, "GET", p.path, "")
		if took := time.Since(began); took > 3*time.Second || code != 200 {
			t.Fatalf("%s: %d after %v", p.path, code, took)
		}
		mustContain(t, body, p.want)
		mustNotContain(t, body, "id=karte-heute>", "Termin", "Erinnerungen")
		// haus#12, haus#33: each page says it, once, in a plain line
		if n := strings.Count(body, "Der Kalender antwortet gerade nicht."); n != 1 {
			t.Fatalf("%s: the calendar said %d times:\n%s", p.path, n, body)
		}
	}
}

// mustNotLeak: a backend that does not answer is never named on a family
// page, nor is its error or where it lives (laib ADR 0021). The log has
// them.
func mustNotLeak(t *testing.T, body string, extra ...string) {
	t.Helper()
	mustNotContain(t, body, append([]string{"boom", "HA 500", "500", "401", "Anmeldung abgelehnt", "HTTP", "class=rot",
		"connection refused", "no such file", "openat", "Home Assistant", "Radicale", "CalDAV", "CardDAV", "WebSocket",
		"127.0.0.1", "localhost", "registry"}, extra...)...)
}

// haus#33: an item page whose backend does not answer is a page with
// Start's plain line and the way back, a 200 like Start — not a 503, and
// never "gibt es nicht": the thing may well be there. Something that is
// not there stays a 404.
func TestItemPagesSayPlainlyWhatDoesNotAnswer(t *testing.T) {
	haDown, hcDown := house(t)
	haDown.Set("light.flur", "on", map[string]any{"friendly_name": "Flurlicht"})
	haDown.SetRegistries([]map[string]any{{"area_id": "flur", "name": "Flur"}}, nil, []map[string]any{
		{"entity_id": "light.flur", "area_id": "flur"}, {"entity_id": "light.versteckt", "hidden_by": "user"}})
	haDown.Down(true)

	unread, hcUnread := house(t)
	unread.Set("light.flur", "on", map[string]any{"friendly_name": "Flurlicht"})
	unread.RegistriesDown(true)

	_, cal := calendar(t)
	cal.Password = "falsch"
	_, contacts := calendar(t)
	contacts.Password = "falsch"
	gone := t.TempDir()
	nd, err := notes.Open(gone)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		page   func(StartConfig) http.HandlerFunc
		cfg    StartConfig
		path   string
		says   string
		back   string
		hidden []string // nothing of the thing, nor where its backend lives
	}{
		{Ding, StartConfig{HA: hcDown}, "/raeume/light.flur", "Licht und Geräte antworten gerade nicht.", "href=/raeume", []string{"Flurlicht", "Flur<", host(hcDown.URL)}},
		{Ding, StartConfig{HA: hcUnread}, "/raeume/light.flur", "Die Räume werden gerade gelesen — gleich nochmal versuchen.", "href=/raeume", []string{"Flurlicht", "Ohne Raum", host(hcUnread.URL)}},
		{Erinnerung, StartConfig{Calendar: cal}, "/kalender/erinnerung/a", "Der Kalender antwortet gerade nicht.", "href=/kalender", []string{host(cal.URL)}},
		{Kontakt, StartConfig{Contacts: contacts}, "/kontakte/a", "Die Kontakte lassen sich gerade nicht lesen.", "href=/start", []string{host(contacts.URL)}},
		{Notizen, StartConfig{Notes: nd}, "/notizen/einkaufsliste.md", "Die Notizen lassen sich gerade nicht lesen.", "href=/notizen", []string{gone}},
	} {
		code, body, h := familyPage(t, c.page, c.cfg, "GET", c.path, "family")
		if code != http.StatusOK {
			t.Errorf("%s with its backend down → %d, want 200", c.path, code)
		}
		if n := strings.Count(body, c.says); n != 1 {
			t.Errorf("%s says %q %d times, want once:\n%s", c.path, c.says, n, body)
		}
		mustContain(t, body, "<h1", "<nav class=tabs", c.back)
		mustNotContain(t, body, "gibt es nicht")
		mustNotLeak(t, body, c.hidden...)
		if h.Get("Content-Type") != "text/html; charset=utf-8" {
			t.Errorf("%s: %q", c.path, h.Get("Content-Type"))
		}
	}

	// what is not there stays a 404, whatever else does not answer: a
	// thing HA does not know while its rooms are unread, a thing HA hides
	// while its states do not answer
	for _, c := range []struct {
		cfg  StartConfig
		path string
	}{{StartConfig{HA: hcUnread}, "/raeume/light.gibtsnicht"}, {StartConfig{HA: hcDown}, "/raeume/light.versteckt"}} {
		code, body, _ := familyPage(t, Ding, c.cfg, "GET", c.path, "family")
		if code != http.StatusNotFound {
			t.Errorf("%s → %d, want 404:\n%s", c.path, code, body)
		}
		mustContain(t, body, "Dieses Gerät gibt es nicht.", "href=/raeume")
		mustNotContain(t, body, "antworten gerade nicht", "gerade gelesen")
	}
}

// host is a backend's host and port, from its URL.
func host(u string) string { return strings.TrimPrefix(u, "http://") }
