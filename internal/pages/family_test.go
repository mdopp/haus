package pages

import (
	"encoding/json"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/homes/internal/assistent"
	"github.com/mdopp/homes/internal/caldav"
	"github.com/mdopp/homes/internal/modul"
	"github.com/mdopp/homes/internal/notes"
)

// modulListe is a box's module list with these modules on and their
// entries in the family's selection (ADR 0001 H6), as the catalog gives
// them: "musik" with Musik, a box with a music inbox; "musik:ohne-eingang"
// with Platte erfassen and Discogs on their own.
func modulListe(t *testing.T, names ...string) *modul.Liste {
	t.Helper()
	var ms []modul.Modul
	for _, n := range names {
		name, wie, _ := strings.Cut(n, ":")
		nav := slices.DeleteFunc(Nav(name), func(e modul.Eintrag) bool {
			return name == "musik" && (e.Pfad == MusikPath) == (wie == "ohne-eingang")
		})
		ms = append(ms, modul.Modul{Name: name, Nav: nav})
	}
	l, err := modul.Bauen(ms)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

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

// barOf is the nav's bar entries, without what Mehr holds.
func barOf(t *testing.T, body string) string {
	t.Helper()
	i, j := strings.Index(body, "<nav class=tabs aria-label=Seiten>"), strings.Index(body, "<div class=mehr>")
	if i < 0 || j < i {
		t.Fatalf("no selection with Mehr:\n%s", body)
	}
	return body[i:j]
}

// mehrOf is what Mehr holds in the nav (a wide window shows it open).
func mehrOf(t *testing.T, body string) string {
	t.Helper()
	i := strings.Index(body, "<div class=mehr>")
	j := strings.Index(body[i:], "</nav>")
	return body[i : i+j]
}

// haus#12, haus#35, haus#67, owner 2026-10-24: one selection on every
// family page, the item pages too. The bar is Start · Geräte · Haushalt ·
// Energie · Mehr, each with its icon and only when its backend is set;
// every page is in exactly one: Räume, things and Medien are Geräte;
// Kalender, Einkauf and Notizen Haushalt, with their pills on top; Mehr
// holds Dokumente (owner 2026-10-27: Dokumente unter Mehr), the settings
// and, for admins, Box, then who is signed in and Abmelden.
func TestFamilyTabBar(t *testing.T) {
	fake, hc := house(t)
	fake.Set("light.flur", "on", map[string]any{"friendly_name": "Flurlicht"})
	_, cal := calendar(t)
	nd := notesDir(t, map[string]time.Duration{"Einkaufsliste": time.Hour})
	all := StartConfig{HA: hc, Calendar: cal, Notes: nd, Module: modulListe(t, "geraete", "energie", "kalender", "notizen")}
	for _, p := range []struct {
		page func(StartConfig) http.HandlerFunc
		path string
		bar  string
		pill string // the Haushalt pill marked; "": no pills
	}{{Start, "/start", "/start", ""}, {Raeume, "/raeume", "/raeume", ""}, {Kalender, "/kalender", "/kalender", "/kalender"},
		{Energie, "/energie", "/energie", ""}, {Medien, "/medien", "/raeume", ""}, {Notizen, "/notizen", "/kalender", "/notizen"},
		{Mehr, "/mehr", "/mehr", ""}, {Ding, "/raeume/light.flur", "/raeume", ""}, {Ding, "/raeume/light.gibtsnicht", "/raeume", ""},
		{Notizen, "/notizen/einkaufsliste.md", "/kalender", "/notizen"}} {
		code, body, _ := familyPage(t, p.page, all, "GET", p.path, "family")
		if code != 200 && code != 404 {
			t.Fatalf("%s → %d", p.path, code)
		}
		bar, more := barOf(t, body), mehrOf(t, body)
		if n := strings.Count(bar, "<a "); n != 5 {
			t.Errorf("%s: %d entries in the bar, want 5", p.path, n)
		}
		mustContain(t, bar, ">Start</a>", ">Geräte</a>", ">Haushalt</a>", ">Energie</a>", ">Mehr</a>", "<svg")
		before(t, bar, "href=/start", "href=/raeume")
		before(t, bar, "href=/raeume", "href=/kalender")
		before(t, bar, "href=/kalender", "href=/energie")
		before(t, bar, "href=/energie", "href=/mehr")
		mustNotContain(t, bar, "href=/medien", "href=/notizen", "href=/box", "Abmelden")
		if !strings.Contains(bar, "href="+p.bar+" aria-current=page>") || strings.Count(bar, "aria-current=page") != 1 {
			t.Errorf("%s: the bar marks not just %s:\n%s", p.path, p.bar, bar)
		}
		mustContain(t, more, ">Mehr</p>", "Angemeldet als <span class=wer>anna</span>", "href=/abmelden>")
		mustNotContain(t, more, "href=/box", "href=/energie", "href=/medien", "href=/notizen", "href=/dokumente")
		if p.pill != "" {
			pills := between(t, body, "<nav class=unterseiten aria-label=Haushalt>", "</nav>")
			mustContain(t, pills, "<a href=/kalender", ">Kalender</a>", "<a href=/notizen", ">Notizen</a>", "href="+p.pill+" aria-current=page>")
			before(t, pills, "Kalender", "Notizen")
		} else {
			mustNotContain(t, body, "class=unterseiten")
		}
		_, body, _ = familyPage(t, p.page, all, "GET", p.path, "family,admins")
		mustContain(t, mehrOf(t, body), "href=/box")
		mustNotContain(t, barOf(t, body), "href=/box")
	}
	// Box is under Mehr, for admins
	_, body, _ := familyPage(t, func(c StartConfig) http.HandlerFunc {
		return BoxPage(BoxConfig{Laib: fakeLaib(t, allGreen(t), nil), Family: c})
	}, all, "GET", "/box", "admins")
	mustContain(t, barOf(t, body), "href=/mehr aria-current=page>")
	mustContain(t, mehrOf(t, body), "href=/box aria-current=page>")
	// Haushalt with notes only: the tab leads there, no pills for one page
	_, body, _ = familyPage(t, Notizen, StartConfig{Notes: nd, Module: modulListe(t, "notizen")}, "GET", "/notizen", "family")
	mustContain(t, barOf(t, body), "href=/notizen aria-current=page>", "</svg>Haushalt</a>")
	mustNotContain(t, body, "class=unterseiten")
	// nothing set: Start and Mehr, and no word about the rest
	_, body, _ = familyPage(t, Start, StartConfig{}, "GET", "/start", "family")
	mustContain(t, body, "href=/start aria-current=page>", "href=/mehr>")
	mustNotContain(t, body, "href=/raeume", "href=/energie", "href=/medien", "href=/kalender", "href=/notizen", "Räume", "Geräte", "Haushalt", "Energie", "Medien", "Kalender", "Notizen")
}

// haus#159, owner 2026-10-27: Dokumente is under Mehr, with its icon and
// only when the box has documents; the Haushalt pills are Kalender, Einkauf
// and Notizen, and a box with documents only has no Haushalt tab.
func TestDokumenteUnderMehr(t *testing.T) {
	if got := tabOf("Dokumente"); got != "Mehr" {
		t.Errorf("tabOf(Dokumente) = %q, want Mehr", got)
	}
	_, cal := calendar(t)
	nd := notesDir(t, nil)
	_, docs := dokumente(t)
	docs.Module = modulListe(t, "dokumente")
	all := docs
	all.Calendar, all.Notes, all.Module = cal, nd, modulListe(t, "kalender", "notizen", "dokumente")
	// Dokumente marks Mehr and its own entry there, no pills
	_, body, _ := familyPage(t, Dokumente, all, "GET", "/dokumente", "family")
	mustContain(t, barOf(t, body), "href=/mehr aria-current=page>")
	mustNotContain(t, barOf(t, body), "href=/kalender aria-current", "href=/dokumente")
	more := mehrOf(t, body)
	mustContain(t, more, "href=/dokumente aria-current=page><svg viewBox=\"0 0 24 24\" aria-hidden=true><", "</svg>Dokumente</a>")
	mustNotContain(t, more, "aria-hidden=true></svg>Dokumente")
	mustNotContain(t, body, "class=unterseiten")
	// Kalender's pills no longer hold Dokumente
	_, body, _ = familyPage(t, Kalender, all, "GET", "/kalender", "family")
	pills := between(t, body, "<nav class=unterseiten aria-label=Haushalt>", "</nav>")
	mustContain(t, pills, ">Kalender</a>", ">Notizen</a>")
	mustNotContain(t, pills, "Dokumente")
	mustContain(t, mehrOf(t, body), "href=/dokumente>")
	// Mehr's page lists it, with its line
	_, body, _ = familyPage(t, Mehr, all, "GET", "/mehr", "family")
	mustContain(t, body, mehrSays["Dokumente"])
	if mehrSays["Dokumente"] == "" {
		t.Error("Mehr says nothing of Dokumente")
	}
	// documents only: no Haushalt tab, Dokumente under Mehr
	_, body, _ = familyPage(t, Dokumente, docs, "GET", "/dokumente", "family")
	mustNotContain(t, barOf(t, body), "Haushalt")
	mustContain(t, mehrOf(t, body), "href=/dokumente aria-current=page>")
	// no documents: no entry
	_, body, _ = familyPage(t, Mehr, StartConfig{Calendar: cal}, "GET", "/mehr", "family")
	mustNotContain(t, body, "/dokumente")
}

// haus#67: on the phone the bar is at the bottom; from 900 px it is a list
// on the right, icon and name, the page marked, Mehr open in it (its own
// entry the phone's only), and the cards in two columns beside it.
func TestFamilySelectionOnAWideWindow(t *testing.T) {
	_, body, _ := familyPage(t, Start, StartConfig{}, "GET", "/start", "family")
	mustContain(t, body, ".tabs{position:fixed;left:0;right:0;bottom:0;", ".tabs .mehr{display:none}", "@media (min-width:900px){",
		".tabs{top:0;left:auto;right:0;bottom:0;width:15rem;flex-direction:column;", ".tabs .zu-mehr{display:none}",
		".tabs .mehr{display:flex;", "grid-template-columns:repeat(2,minmax(0,32rem))", "body>.karte{grid-column:auto}")
	before(t, body, ".tabs .mehr{display:none}", "@media (min-width:900px){")
}

// laib#418: a page whose backend this box does not have says so, in one
// sentence, with the way to Start — never a silent redirect to Start (the
// family tapped Kalender and landed on Start without a word). An admin also
// reads what to set up; the family never reads a backend's name.
func TestFamilyPagesWithoutBackendSayWhatIsMissing(t *testing.T) {
	for _, p := range []struct {
		page       func(StartConfig) http.HandlerFunc
		path, says string
	}{{Raeume, "/raeume", "Licht und Geräte"}, {Energie, "/energie", "Licht und Geräte"}, {Medien, "/medien", "Licht und Geräte"},
		{Kalender, "/kalender", "Der Kalender ist hier noch nicht eingerichtet."}, {Notizen, "/notizen", "Die Notizen"}, {Notizen, "/notizen/einkaufsliste.md", "Die Notizen"},
		{Ding, "/raeume/light.flur", "Licht und Geräte"}, {Erinnerung, "/kalender/erinnerung/a", "Der Kalender"}, {Kontakt, "/kontakte/a", "Die Kontakte"},
		{Dokument, "/dokumente/2", "Die Dokumente"}} {
		code, body, h := familyPage(t, p.page, StartConfig{}, "GET", p.path, "family")
		if code != http.StatusOK || h.Get("Location") != "" {
			t.Fatalf("%s without its backend → %d %q, want the page saying what is missing", p.path, code, h.Get("Location"))
		}
		mustContain(t, body, p.says, "noch nicht eingerichtet", "href=/start")
		mustNotLeak(t, body, "Radicale", "Home Assistant", "Paperless", "HOMES_", "RADICALE_", "file-share", "laib add")
		_, admin, _ := familyPage(t, p.page, StartConfig{}, "GET", p.path, "family,admins")
		mustContain(t, admin, p.says, "Für Admins:")
	}
	_, admin, _ := familyPage(t, Kalender, StartConfig{}, "GET", "/kalender", "admins")
	mustContain(t, admin, "RADICALE_USER", "HOMES_RADICALE_PASSWORD")
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
	}{{Raeume, "/raeume"}, {Energie, "/energie"}, {Medien, "/medien"}, {Kalender, "/kalender"}, {Notizen, "/notizen"}, {Notizen, "/notizen/einkaufsliste.md"}} {
		for _, m := range []string{"POST", "PUT", "DELETE"} {
			if m == "POST" && strings.HasPrefix(p.path, "/notizen") {
				continue // a note is written, not a verdict (owner, 2026-10-24): TestNotizenWrite
			}
			if code, _, _ := familyPage(t, p.page, all, m, p.path, "admins"); code != http.StatusMethodNotAllowed {
				t.Fatalf("%s %s → %d, want 405", m, p.path, code)
			}
		}
	}
}

// haus#35: the assistant's name appears where you talk to it, not on the
// pages: no name on top of any family page. The pages wear the
// house palette (the chat's tokens, homes-android's colors.xml), light
// or dark as the phone is, and Homes' own Geist — never a font from
// Google.
func TestFamilyPagesWearTheHousePalette(t *testing.T) {
	named(t, "Ada")
	fake, hc := house(t)
	fake.Set("light.flur", "on", map[string]any{"friendly_name": "Flurlicht"})
	_, cal := calendar(t)
	nd := notesDir(t, map[string]time.Duration{"Einkaufsliste": time.Hour})
	all := StartConfig{HA: hc, Calendar: cal, Notes: nd}
	for _, p := range []struct {
		page  func(StartConfig) http.HandlerFunc
		path  string
		title string
	}{{Start, "/start", "Homes"}, {Raeume, "/raeume", "Geräte · Homes"}, {Kalender, "/kalender", "Kalender · Homes"},
		{Notizen, "/notizen", "Notizen · Homes"}, {Ding, "/raeume/light.flur", "Flurlicht · Homes"}} {
		_, body, h := familyPage(t, p.page, all, "GET", p.path, "")
		mustContain(t, body, "<title>"+p.title+"</title>", "lang=de", "<h1>",
			"#070709", "#14141a", "#f97316", "#ffc107", "prefers-color-scheme:light", "color-scheme",
			"@font-face", FontPath, "system-ui",
			"rel=icon href=/symbol/favicon.svg", "rel=apple-touch-icon href=/symbol/apple-touch-icon.png",
			"rel=manifest href=/symbol/homes.webmanifest crossorigin=use-credentials")
		mustNotContain(t, body, "<header>", "class=sr>",
			"<script src", "googleapis", "gstatic", "#faf9f6") // Homes' own small scripts only, never one from elsewhere
		if h.Get("Content-Type") != "text/html; charset=utf-8" {
			t.Fatalf("%s: %q", p.path, h.Get("Content-Type"))
		}
	}
}

// haus#35: Homes serves Geist itself (OFL, vendored), cached for good: the
// family's box asks no one else for a font.
func TestHomesServesItsFont(t *testing.T) {
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

// Homes' own icon (owner, 2026-11-02): each size rendered from the master,
// served with its type, and the manifest that puts Homes on a home screen;
// nothing else under /symbol/.
func TestHomesServesItsIcon(t *testing.T) {
	get := func(method, p string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		Symbol().ServeHTTP(rec, httptest.NewRequest(method, p, nil))
		return rec
	}
	for name, side := range map[string]int{"favicon-32.png": 32, "apple-touch-icon.png": 180, "icon-192.png": 192, "icon-512.png": 512, "icon-maskable-512.png": 512} {
		rec := get("GET", SymbolPath+name)
		if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/png" {
			t.Fatalf("%s: %d %q", name, rec.Code, rec.Header().Get("Content-Type"))
		}
		cfg, err := png.DecodeConfig(rec.Body)
		if err != nil || cfg.Width != side || cfg.Height != side {
			t.Fatalf("%s: %v %dx%d, want %d", name, err, cfg.Width, cfg.Height, side)
		}
	}
	rec := get("GET", SymbolPath+"favicon.svg")
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/svg+xml" || !strings.Contains(rec.Body.String(), "#3ddc84") ||
		!strings.Contains(rec.Body.String(), "#0c120e") || strings.Contains(rec.Body.String(), "<script") {
		t.Fatalf("favicon.svg: %d %q %s", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}
	rec = get("GET", SymbolPath+"homes.webmanifest")
	var m struct {
		Name     string
		StartURL string `json:"start_url"`
		Icons    []struct{ Src, Purpose string }
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil || rec.Header().Get("Content-Type") != "application/manifest+json" || m.StartURL != "/start" {
		t.Fatalf("manifest: %v %q %s", err, rec.Header().Get("Content-Type"), rec.Body.String())
	}
	for _, i := range m.Icons {
		if c := get("GET", i.Src).Code; c != 200 {
			t.Fatalf("the manifest names %s, which answers %d", i.Src, c)
		}
	}
	if !strings.Contains(rec.Body.String(), `"purpose":"maskable"`) {
		t.Fatalf("no maskable icon: %s", rec.Body.String())
	}
	for _, p := range []string{SymbolPath + "icon.svg", SymbolPath + "andere.png", SymbolPath + "sub/favicon.svg", SymbolPath} {
		if c := get("GET", p).Code; c != http.StatusNotFound {
			t.Fatalf("%s → %d", p, c)
		}
	}
	if c := get("POST", SymbolPath+"favicon.svg").Code; c != http.StatusMethodNotAllowed {
		t.Fatalf("POST → %d", c)
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
	cal := &caldav.Client{URL: hang.URL, User: "homes", Password: "pw", Loc: berlin(t)}
	nd := notesDir(t, map[string]time.Duration{"Einkaufsliste": time.Hour})
	for _, p := range []struct {
		page func(StartConfig) http.HandlerFunc
		path string
		want string
	}{{Start, "/start", "<h1>Homes</h1>"}, {Kalender, "/kalender", "<h1>Kalender</h1>"}} {
		began := time.Now()
		code, body, _ := familyPage(t, p.page, StartConfig{Calendar: cal, Notes: nd}, "GET", p.path, "")
		if took := time.Since(began); took > 3*time.Second || code != 200 {
			t.Fatalf("%s: %d after %v", p.path, code, took)
		}
		mustContain(t, body, p.want)
		mustNotContain(t, body, "keine Termine", "Keine Termine", "Erinnerungen", saysNichtsHeute)
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
		{Kontakt, StartConfig{Contacts: contacts}, "/kontakte/a", "Die Kontakte lassen sich gerade nicht lesen.", "href=/kontakte", []string{host(contacts.URL)}},
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

// named sets the assistant's name for one test, as Laib's HOMES_ASSISTANT would.
func named(t *testing.T, name string) {
	t.Helper()
	assistent.Set(name)
	t.Cleanup(func() { assistent.Set("") })
}
