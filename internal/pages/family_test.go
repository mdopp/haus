package pages

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/haus/internal/caldav"
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

// haus#12: one nav on every family page — Start · Räume · Kalender ·
// Notizen, each only when its backend is set, Box for admins — with the
// page itself marked.
func TestFamilyNav(t *testing.T) {
	_, hc := house(t)
	_, cal := calendar(t)
	nd := notesDir(t, map[string]time.Duration{"Einkaufsliste": time.Hour})
	all := StartConfig{HA: hc, Calendar: cal, Notes: nd}
	for _, p := range []struct {
		page func(StartConfig) http.HandlerFunc
		path string
		self string
	}{{Start, "/start", "Start"}, {Raeume, "/raeume", "Räume"}, {Kalender, "/kalender", "Kalender"}, {Notizen, "/notizen", "Notizen"}} {
		code, body, _ := familyPage(t, p.page, all, "GET", p.path, "family")
		if code != 200 {
			t.Fatalf("%s → %d", p.path, code)
		}
		mustContain(t, body, "<nav>", "href=/start", "href=/raeume", "href=/kalender", "href=/notizen", "aria-current=page>"+p.self+"</a>")
		mustNotContain(t, body, "href=/box")
		before(t, body, "href=/start", "href=/raeume")
		before(t, body, "href=/raeume", "href=/kalender")
		before(t, body, "href=/kalender", "href=/notizen")
		_, body, _ = familyPage(t, p.page, all, "GET", p.path, "family,admins")
		mustContain(t, body, "href=/box")
	}
	// nothing set: Start alone, and no word about the rest
	_, body, _ := familyPage(t, Start, StartConfig{}, "GET", "/start", "family")
	mustContain(t, body, "href=/start")
	mustNotContain(t, body, "href=/raeume", "href=/kalender", "href=/notizen", "Räume", "Kalender", "Notizen")
}

// A page whose backend is not set is not served: it lands on Start, like
// every path Haus does not serve.
func TestFamilyPagesWithoutBackendLandOnStart(t *testing.T) {
	for _, p := range []struct {
		page func(StartConfig) http.HandlerFunc
		path string
	}{{Raeume, "/raeume"}, {Kalender, "/kalender"}, {Notizen, "/notizen"}, {Notizen, "/notizen/einkaufsliste.md"}} {
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

// laib ADR 0020: the family's pages wear the persona's header, German.
func TestFamilyPagesWearThePersona(t *testing.T) {
	_, hc := house(t)
	_, cal := calendar(t)
	nd := notesDir(t, map[string]time.Duration{"Einkaufsliste": time.Hour})
	all := StartConfig{HA: hc, Calendar: cal, Notes: nd}
	for _, p := range []struct {
		page  func(StartConfig) http.HandlerFunc
		path  string
		title string
	}{{Raeume, "/raeume", "Räume"}, {Kalender, "/kalender", "Kalender"}, {Notizen, "/notizen", "Notizen"}} {
		_, body, h := familyPage(t, p.page, all, "GET", p.path, "")
		mustContain(t, body, "<title>"+p.title+" · Haus</title>", "<header>", "lang=de", "<h1")
		mustNotContain(t, body, "<script")
		if h.Get("Content-Type") != "text/html; charset=utf-8" {
			t.Fatalf("%s: %q", p.path, h.Get("Content-Type"))
		}
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
	}{{Start, "/start", "Einkaufsliste"}, {Kalender, "/kalender", "<h1 class=sr>Kalender</h1>"}} {
		began := time.Now()
		code, body, _ := familyPage(t, p.page, StartConfig{Calendar: cal, Notes: nd}, "GET", p.path, "")
		if took := time.Since(began); took > 3*time.Second || code != 200 {
			t.Fatalf("%s: %d after %v", p.path, code, took)
		}
		mustContain(t, body, p.want)
		mustNotContain(t, body, "karte-heute", "Termin", "Erinnerungen")
		if p.path == "/start" { // haus#12: Start says it, once, in a plain line
			if n := strings.Count(body, "Der Kalender antwortet gerade nicht."); n != 1 {
				t.Fatalf("/start: the calendar said %d times:\n%s", n, body)
			}
		}
	}
}
