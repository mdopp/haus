package pages

import (
	"context"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/haus/internal/caldav"
	"github.com/mdopp/haus/internal/ha"
	"github.com/mdopp/haus/internal/ha/hafake"
	"github.com/mdopp/haus/internal/usage"
	"github.com/mdopp/haus/internal/usage/usagefake"
)

// #50: Haus counts what the family uses — per event and UTC day, never
// who — and the Box page shows it to admins.

// regexpTags strips tags, so a test reads the page's text.
var regexpTags = regexp.MustCompile(`<[^>]*>`)

func metrics(t *testing.T) (*usagefake.Fake, *usage.Counter) {
	t.Helper()
	fake := usagefake.New("um-token")
	t.Cleanup(fake.Close)
	return fake, usage.New(fake.URL, "um-token")
}

// events flushes c and returns haus's counts by event.
func events(t *testing.T, fake *usagefake.Fake, c *usage.Counter) map[string]int64 {
	t.Helper()
	c.Flush(context.Background())
	out := map[string]int64{}
	for k, n := range fake.Counts() {
		if k.App == "haus" {
			out[k.Event] += n
		}
	}
	return out
}

func serve(h http.Handler, method, target, user string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	req.RemoteAddr = "192.0.2.77:51515"
	if user != "" {
		req.Header.Set("Remote-User", user)
		req.Header.Set("Remote-Name", "Anna Beispiel")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// a stand-in for Haus's pages: each path answers what a page would
func pagesLike(status map[string]int, body map[string]string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code := status[r.URL.Path]
		if code == 0 {
			code = http.StatusOK
		}
		if code == http.StatusFound {
			http.Redirect(w, r, "/start", code)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(code)
		w.Write([]byte("<!doctype html><h1>Seite</h1>" + body[r.URL.Path]))
	})
}

// A page view per page the family opens: 200 only, GET only; a 404, a
// redirect, the font and the icon are no page view.
func TestPageViewsPerPage(t *testing.T) {
	fake, c := metrics(t)
	h := Counted(c, pagesLike(map[string]int{"/gibtsnicht": 404, "/notizen": 302}, nil))
	for _, p := range []string{"/start", "/start", "/raeume", "/raeume/light.flur", "/energie", "/medien", "/kalender",
		"/kalender/erinnerung/abc", "/kontakte/x", "/dokumente/12", "/notizen/einkauf.md", "/sprechen", "/box", "/box/geraete", "/pair-device",
		"/gibtsnicht", "/notizen", "/schrift/geist-1b5ebf.woff2", "/persona/icon.svg", "/healthz"} {
		serve(h, "GET", p, "anna")
	}
	serve(h, "HEAD", "/start", "anna")
	want := map[string]int64{"seite.start": 2, "seite.raeume": 1, "seite.ding": 1, "seite.energie": 1, "seite.medien": 1, "seite.kalender": 1,
		"seite.erinnerung": 1, "seite.kontakt": 1, "seite.dokument": 1, "seite.notiz": 1, "seite.sprechen": 1, "seite.box": 1,
		"seite.geraete": 1, "seite.koppeln": 1}
	if got := events(t, fake, c); !reflect.DeepEqual(got, want) {
		t.Errorf("page views %v,\nwant %v", got, want)
	}
}

// The plain line a page showed because something did not answer, by page
// and by line, once per page shown; the longer line wins over the one it
// contains (Schalten's names Licht und Geräte).
func TestDegradedLinesShownAreCounted(t *testing.T) {
	fake, c := metrics(t)
	House.Display = "Joshua & Co"
	t.Cleanup(func() { House.Display = "Haus" })
	h := Counted(c, pagesLike(nil, map[string]string{
		"/start":    "<p>" + saysNoHouse + " " + saysNoSwitching + "</p><p>" + saysNoCalendar + "</p><p>" + saysNoHouse + "</p>",
		"/schalten": "<p>" + saysSwitchFailed + "</p>",
		"/sprechen": "<p>" + html.EscapeString(saysDown(House.Display)) + "</p>",
		"/notizen":  "<p class=k>" + saysWordsOnly + "</p>",
		"/energie":  "<p class=k>" + saysNoEnergy + "</p>",
		"/raeume":   "<p>Alles da.</p>",
	}))
	for _, p := range []string{"/start", "/start", "/sprechen", "/notizen", "/energie", "/raeume"} {
		serve(h, "GET", p, "anna")
	}
	serve(h, "POST", "/schalten", "anna")
	got := events(t, fake, c)
	for e, n := range map[string]int64{"gestoert.start.licht": 2, "gestoert.start.kalender": 2, "gestoert.schalten.schalten": 1,
		"gestoert.sprechen.bote": 1, "gestoert.notizen.bedeutung": 1, "gestoert.energie.strom": 1} {
		if got[e] != n {
			t.Errorf("%s = %d, want %d (all: %v)", e, got[e], n, got)
		}
	}
	for e := range got {
		if strings.HasPrefix(e, "gestoert.raeume") || e == "gestoert.schalten.licht" {
			t.Errorf("counted %s", e)
		}
	}
}

// A tap from a widget arrives with ?von=widget.<kind>: counted by kind,
// only for the app's nine widgets (haus-android#181); anything else is no
// source tag, never stored raw.
func TestWidgetTapsBySource(t *testing.T) {
	fake, c := metrics(t)
	h := Counted(c, pagesLike(map[string]int{"/gibtsnicht": 404}, nil))
	for _, q := range []string{"widget.device", "widget.device", "widget.energy-stats", "widget.geraet", "widget.Device", "widget.", "../etc", "src",
		"widget." + strings.Repeat("a", 33), "widget.a b", "anna", "device", "widget.device.x"} {
		serve(h, "GET", "/start?von="+url.QueryEscape(q), "anna")
	}
	serve(h, "GET", "/gibtsnicht?von=widget.device", "anna")
	got := events(t, fake, c)
	delete(got, "seite.start")
	if want := map[string]int64{"von.widget.device": 2, "von.widget.energy-stats": 1}; !reflect.DeepEqual(got, want) {
		t.Errorf("taps %v, want %v", got, want)
	}
}

// A search on Notizen is counted; the words searched for never are.
func TestNotesSearchIsCounted(t *testing.T) {
	fake, c := metrics(t)
	h := Counted(c, pagesLike(nil, nil))
	serve(h, "GET", "/notizen?q=Geheimes+Rezept", "anna")
	serve(h, "GET", "/notizen?q=", "anna")
	serve(h, "GET", "/notizen", "anna")
	got := events(t, fake, c)
	if got["suche.notizen"] != 1 || got["seite.notizen"] != 3 {
		t.Errorf("counted %v, want one search and three views", got)
	}
	for _, b := range fake.Bodies() {
		if strings.Contains(strings.ToLower(b), "rezept") {
			t.Errorf("the search words left Haus: %s", b)
		}
	}
}

// Schalten, ☆ and Sprechen count once they passed their guards; a refused
// one counts nothing.
func TestActionsCountAfterTheirGuards(t *testing.T) {
	fake, c := metrics(t)
	_, cfg := switchHouse(t)
	cfg.Usage = c
	cfg.Favorites = favorites(t, "{}")
	_, bc := fakeBote(t)
	cfg.Bote = bc
	sw := Schalten(cfg)
	post(t, sw, "/schalten", url.Values{"geraet": {"light.buero"}, "tun": {"light.turn_off"}}, "anna", nil)
	post(t, sw, "/schalten", url.Values{"tun": {"alle-lichter-aus"}}, "anna", nil)
	post(t, sw, "/schalten", url.Values{"geraet": {"light.buero"}, "tun": {"light.turn_off"}}, "anna", map[string]string{"Sec-Fetch-Site": "cross-site"})
	post(t, sw, "/schalten", url.Values{"geraet": {"light.buero"}, "tun": {"lock.unlock"}}, "anna", nil)
	post(t, sw, "/schalten", url.Values{"geraet": {"light.buero"}, "tun": {"light.turn_off"}}, "", nil)
	ding := Ding(cfg)
	post(t, ding, "/raeume/light.buero", url.Values{"stern": {"an"}}, "anna", nil)
	post(t, ding, "/raeume/light.buero", url.Values{"stern": {"aus"}}, "anna", nil)
	post(t, ding, "/raeume/light.buero", url.Values{"stern": {"an"}}, "anna", map[string]string{"Sec-Fetch-Site": "cross-site"})
	sp := Sprechen(cfg)
	post(t, sp, "/sprechen", url.Values{"nachricht": {"Wann kommt der Müll?"}}, "anna", nil)
	post(t, sp, "/sprechen", url.Values{"nachricht": {"Wann kommt der Müll?"}}, "anna", map[string]string{"Sec-Fetch-Site": "cross-site"})
	post(t, sp, "/sprechen", url.Values{"nachricht": {""}}, "anna", nil)
	got := events(t, fake, c)
	want := map[string]int64{"schalten.light.turn_off": 1, "schalten.alle-lichter-aus": 1, "stern.an": 1, "stern.aus": 1, "sprechen.senden": 1}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("actions %v,\nwant %v", got, want)
	}
	for _, b := range fake.Bodies() {
		for _, leak := range []string{"anna", "Anna", "buero", "Müll", "192.0.2"} {
			if strings.Contains(b, leak) {
				t.Errorf("%q left Haus: %s", leak, b)
			}
		}
	}
}

// Nothing configured: the pages count nothing and still answer.
func TestNoCountingWithoutUsage(t *testing.T) {
	h := Counted(nil, pagesLike(nil, map[string]string{"/start": saysNoHouse}))
	if rec := serve(h, "GET", "/start?von=widget.geraet", "anna"); rec.Code != 200 || !strings.Contains(rec.Body.String(), saysNoHouse) {
		t.Errorf("the page changed without counting: %d %s", rec.Code, rec.Body.String())
	}
}

// Counting never slows a page: usage-metrics down, the page answers at once.
func TestPagesDoNotWaitForUsageMetrics(t *testing.T) {
	fake, c := metrics(t)
	fake.Down(true)
	h := Counted(c, pagesLike(nil, nil))
	start := time.Now()
	for i := 0; i < 200; i++ {
		serve(h, "GET", "/start", "anna")
	}
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Errorf("200 pages took %v with usage-metrics down", d)
	}
}

// what a month of use looks like to usage-metrics
func aMonth(fake *usagefake.Fake) {
	fake.SetSummary(map[string]any{"days": 30, "from": "2026-09-17", "total": 300, "knownApps": []string{"bote", "haus", "stimme"},
		"apps": []any{
			map[string]any{"app": "bote", "total": 61, "events": []any{
				map[string]any{"event": "unbeantwortet", "count": 3}, map[string]any{"event": "werkzeug.haushalt.termine", "count": 12},
				map[string]any{"event": "werkzeug.ha.hassturnoff", "count": 6}, map[string]any{"event": "werkzeug.memory", "count": 5},
				map[string]any{"event": "werkzeug.laib.status", "count": 1}, map[string]any{"event": "zug", "count": 40}}},
			map[string]any{"app": "haus", "total": 200, "events": []any{
				map[string]any{"event": "gestoert.start.licht", "count": 4}, map[string]any{"event": "napi.portal.state", "count": 31},
				map[string]any{"event": "schalten.light.turn_off", "count": 17}, map[string]any{"event": "seite.raeume", "count": 22},
				map[string]any{"event": "seite.start", "count": 120}, map[string]any{"event": "von.widget.device", "count": 6}}},
			map[string]any{"app": "stimme", "total": 39, "events": []any{
				map[string]any{"event": "erkannt", "count": 30}, map[string]any{"event": "nicht-erkannt", "count": 4}, map[string]any{"event": "weitergereicht", "count": 5}}},
		}})
	fake.SetMisses(map[string]any{"days": 30, "from": "2026-09-17", "total": 13, "misses": []any{
		map[string]any{"source": "bote", "text": "Wann kommt der Müll?", "count": 9, "lastDay": "2026-10-16"},
		map[string]any{"source": "stimme", "text": "Mach die <Musik> lauter", "count": 4, "lastDay": "2026-10-15"}}})
}

func boxWithUsage(t *testing.T, c *usage.Counter, v *usage.Voice) http.HandlerFunc {
	t.Helper()
	family := StartConfig{HA: &ha.Client{URL: "http://127.0.0.1:1", Token: "t"}, Calendar: &caldav.Client{URL: "http://127.0.0.1:1"}, Usage: c}
	return BoxPage(BoxConfig{Laib: fakeLaib(t, map[string]any{"state": "bereit"}, nil), LaibURL: "https://laib.box.test", Family: family, Voice: v})
}

// The card says, in the admin's words: what is used most, what hardly or
// never, which plain lines the family saw, what nobody could answer — and
// never an event id.
func TestBoxCardWhatIsUsed(t *testing.T) {
	fake, c := metrics(t)
	aMonth(fake)
	page := getAt(boxWithUsage(t, c, nil), "/box/nutzung?ganz=1", "admins")
	for _, want := range []string{"Was wird genutzt", "letzten 30 Tage", "nie, wer",
		"Am meisten genutzt", "Start geöffnet", "120", "Räume geöffnet", "Licht ausgeschaltet", "17", "Vom Widget „Gerät“ aus geöffnet",
		"Kaum oder nie genutzt", "Kalender geöffnet", "Energie geöffnet",
		"Was nicht antwortete", saysNoHouse, "auf Start", "4×",
		"Im Chat", "40 Anfragen", "3 davon nicht beantwortet", "Termine nachgesehen (12)", "Geräte ausgeschaltet (6)", "Gedächtnis (5)",
		"Per Stimme", "30 hat Home Assistant selbst verstanden", "5 an die KI weitergereicht", "4 hat niemand verstanden",
		"Was niemand beantworten konnte", "Wann kommt der Müll?", "9×", "im Chat", "Mach die &lt;Musik&gt; lauter", "per Stimme", "30 Tage", "ohne Namen",
		`aria-current=page>Nutzung</a>`} {
		if !strings.Contains(page, want) {
			t.Errorf("the card lacks %q", want)
		}
	}
	text := regexpTags.ReplaceAllString(page, " ")
	if strings.Contains(text, "hassturnoff") || strings.Contains(text, "memory") {
		t.Error("a Bote tool is shown by its raw name")
	}
	for _, raw := range []string{"seite.start", "gestoert.", "napi.", "werkzeug.", "von.widget", "nicht-erkannt", "mcp_"} {
		if strings.Contains(text, raw) {
			t.Errorf("the card shows the raw id %q", raw)
		}
	}
	if strings.Contains(page, "um-token") {
		t.Error("the token is on the page")
	}
}

// Nothing counted yet: said plainly, with since when Haus counts.
func TestBoxCardEmpty(t *testing.T) {
	_, c := metrics(t)
	c.Started = time.Date(2026, 10, 16, 21, 30, 0, 0, time.Local)
	page := getAt(boxWithUsage(t, c, nil), "/box/nutzung?ganz=1", "admins")
	if !strings.Contains(page, "Was wird genutzt") || !strings.Contains(page, "Noch nichts gezählt") || !strings.Contains(page, "16.10.") {
		t.Errorf("the empty card does not say so:\n%s", page)
	}
}

// usage-metrics not answering: one plain line, never its error; the rest
// of the Box page stands.
func TestBoxCardWhenUsageMetricsIsDown(t *testing.T) {
	fake, c := metrics(t)
	fake.Down(true)
	page := getAt(boxWithUsage(t, c, nil), "/box/nutzung?ganz=1", "admins")
	if !strings.Contains(page, "Die Nutzungszahlen lassen sich gerade nicht lesen.") || !strings.Contains(page, "<h1>Nutzung</h1>") {
		t.Errorf("down must be one plain line:\n%s", page)
	}
	for _, leak := range []string{"503", "127.0.0.1", fake.URL} {
		if strings.Contains(page, leak) {
			t.Errorf("the page shows %q", leak)
		}
	}
}

// Without usage-metrics configured there is no card and no word about it;
// the family never sees it (Box sends them to Start).
func TestBoxCardOnlyWhenCountingAndOnlyForAdmins(t *testing.T) {
	if page := getAt(boxWithUsage(t, nil, nil), "/box/nutzung?ganz=1", "admins"); strings.Contains(page, "genutzt") {
		t.Errorf("a card without usage-metrics:\n%s", page)
	}
	fake, c := metrics(t)
	aMonth(fake)
	req := httptest.NewRequest("GET", "/box/nutzung?ganz=1", nil)
	req.Header.Set("Remote-User", "ben")
	req.Header.Set("Remote-Groups", "family")
	rec := httptest.NewRecorder()
	boxWithUsage(t, c, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || strings.Contains(rec.Body.String(), "genutzt") || strings.Contains(rec.Body.String(), "Müll") {
		t.Errorf("the family got %d %s", rec.Code, rec.Body.String())
	}
	if len(fake.Asked()) != 0 {
		t.Errorf("the family's request read usage-metrics: %v", fake.Asked())
	}
}

// HA refuses Haus the Assist runs: the card says the voice is not counted
// and what it takes.
func TestBoxCardSaysWhyVoiceIsNotCounted(t *testing.T) {
	fake, c := metrics(t)
	aMonth(fake)
	fakeHA := hafake.New("ha-token")
	t.Cleanup(fakeHA.Close)
	fakeHA.SetAssist([]hafake.Pipeline{{ID: "p"}})
	fakeHA.AssistAdmin(false)
	v := &usage.Voice{HA: &ha.Client{URL: fakeHA.URL, Token: "ha-token"}, Counter: c, Since: time.Now(), Log: func(string, ...any) {}}
	v.Poll(context.Background())
	page := getAt(boxWithUsage(t, c, v), "/box/nutzung?ganz=1", "admins")
	if !strings.Contains(page, "Sprachbefehle werden nicht gezählt") || !strings.Contains(page, "Administrator") {
		t.Errorf("the refusal is not said:\n%s", page)
	}
}

// The Bote's tools as laib's plugin counts them (laib#275):
// werkzeug.<server>.<tool> from Hermes' mcp__<server>__<tool>, Hermes' own
// without a server; the card says the server in words.
func TestBoteToolsInWords(t *testing.T) {
	for event, want := range map[string]string{
		"werkzeug.haushalt.termine":             "Termine nachgesehen",
		"werkzeug.haushalt.termin_anlegen":      "Termin eingetragen",
		"werkzeug.haushalt.notiz_anlegen":       "Notiz angelegt",
		"werkzeug.haushalt.notizen_suchen":      "In den Notizen gesucht",
		"werkzeug.haushalt.dokumente_suchen":    "Dokumente gesucht",
		"werkzeug.haushalt.kontakte_suchen":     "Kontakte gesucht",
		"werkzeug.ha.hassturnoff":               "Geräte ausgeschaltet",
		"werkzeug.ha.hassturnon":                "Geräte eingeschaltet",
		"werkzeug.ha.getlivecontext":            "Nachgesehen, was im Haus an ist",
		"werkzeug.laib.laib_status":             "Wie es der Box geht",
		"werkzeug.laib.laib_propose":            "Der Box etwas vorgeschlagen",
		"werkzeug.frage.frage":                  "Im Netz gesucht",
		"werkzeug.automat.automation_schreiben": "Automation geschrieben",
		"werkzeug.memory":                       "Gedächtnis",
		"werkzeug.todo":                         "Aufgabenliste",
		"werkzeug.session_search":               "Frühere Gespräche durchsucht",
		"werkzeug.clarify":                      "Zurückgefragt",
		"werkzeug.haushalt.neu_dazu":            "Sonst etwas im Haushalt",
		"werkzeug.ha.hassirgendwas":             "Sonst etwas an den Geräten",
		"werkzeug.neu.etwas_tun":                "Ein anderes Werkzeug",
		"werkzeug.eigenes_werkzeug":             "Ein anderes Werkzeug",
	} {
		got := toolSays(strings.TrimPrefix(event, "werkzeug."))
		if got != want {
			t.Errorf("%s → %q, want %q", event, got, want)
		}
		_, name, _ := strings.Cut(strings.TrimPrefix(event, "werkzeug."), ".")
		if strings.ContainsAny(got, "_:") || name != "" && strings.EqualFold(got, strings.ReplaceAll(name, "_", " ")) {
			t.Errorf("%s shows the tool's name, not what was done: %q", event, got)
		}
	}
}

// haus#67: Start's box line, fetched after the page, is no page view of
// its own; the plain line it shows counts for Start.
func TestStartBoxLineCountsForStart(t *testing.T) {
	fake, c := metrics(t)
	h := Counted(c, pagesLike(nil, map[string]string{StartBoxPath: `{"unten":"<p>Wie es der Box geht, lässt sich gerade nicht sagen.</p>"}`}))
	serve(h, "GET", StartBoxPath+"?ruhig=1", "anna")
	got := events(t, fake, c)
	if got["gestoert.start.box"] != 1 || got["seite.start"] != 0 {
		t.Fatalf("counted %v", got)
	}
}

// haus#63: the streamed answer flushes through the counting: each piece
// reaches the page when it is written, not when the turn is over.
func TestCountedLetsAStreamFlush(t *testing.T) {
	_, c := metrics(t)
	h := Counted(c, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("data: {}\n\n"))
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Errorf("flush: %v", err)
		}
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/sprechen", nil))
	if !rec.Flushed {
		t.Fatal("not flushed")
	}
}
