package pages

import (
	"bytes"
	"context"
	"html"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/homes/internal/napi"
	"github.com/mdopp/homes/internal/reitti"
	"github.com/mdopp/homes/internal/texte"
)

// Standortverlauf (owner 2026-11-02): each person pastes their Reitti
// token once; Homes checks it with Reitti, keeps it, never shows it back.
// A fake Reitti answers by the token.
func standortCfg(t *testing.T) (StartConfig, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("Authorization") {
		case "Bearer gut-0123":
			w.Write([]byte(`{"result":"ok"}`))
		case "Bearer ohne-geraet":
			w.WriteHeader(500)
		default:
			w.WriteHeader(401)
		}
	}))
	t.Cleanup(srv.Close)
	r := &reitti.Reitti{URL: srv.URL, PublicURL: "https://reitti.", Dir: t.TempDir(),
		Now: func() time.Time { return time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC) }}
	return StartConfig{Reitti: r, PublicURL: "https://homes.box.test", Loc: time.UTC, Module: modulListe(t, "standort")}, srv
}

func getStandort(cfg StartConfig, user, query string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", StandortPath+query, nil)
	req.Header.Set("Remote-User", user)
	rec := httptest.NewRecorder()
	Standort(cfg).ServeHTTP(rec, req)
	return rec
}

func TestStandortConnectsWithATokenReittiTakes(t *testing.T) {
	var logs bytes.Buffer
	log.SetOutput(&logs)
	defer log.SetOutput(os.Stderr)
	cfg, _ := standortCfg(t)
	h := Standort(cfg)

	// idle: what it is, how to get a token, the field, a way to Reitti
	rec := getStandort(cfg, "anna", "")
	page := rec.Body.String()
	mustContain(t, page, "<h1>Standortverlauf</h1>", "Einstellungen → API-Token", "Mit Gerät verbinden", `name=token type=password`,
		`data-laeuft="Prüft bei Reitti …"`, `href="https://reitti.box.test"`, "Homes-App")
	if rec.Code != 200 || rec.Header().Get("Cache-Control") != "no-store" || strings.Contains(page, "Verbunden seit") {
		t.Fatalf("%d %q", rec.Code, rec.Header().Get("Cache-Control"))
	}

	// failed, each with its reason and the next step; nothing kept
	for _, c := range []struct {
		token string
		code  int
		say   string
	}{
		{"   ", 400, saysStandortLeer},
		{"falsch", 400, saysStandortFalsch},
		{"ohne-geraet", 502, saysStandortGeraet},
	} {
		rec := post(t, h, StandortPath, url.Values{"tun": {"verbinden"}, "token": {c.token}}, "anna", nil)
		if rec.Code != c.code || !strings.Contains(rec.Body.String(), html.EscapeString(c.say)) || !strings.Contains(rec.Body.String(), "role=alert") {
			t.Errorf("%q → %d\n%s", c.token, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "value=\""+c.token) {
			t.Errorf("%q is written back into the page", c.token)
		}
	}
	if _, ok := cfg.Reitti.Konto("anna"); ok {
		t.Fatal("a refused token was kept")
	}

	// done: 303 back to the page, which says since when and offers Trennen — never the token
	rec = post(t, h, StandortPath, url.Values{"tun": {"verbinden"}, "token": {" gut-0123 "}}, "anna", nil)
	if rec.Code != 303 || rec.Header().Get("Location") != StandortPath+"?ok=verbunden" {
		t.Fatalf("connected → %d %q\n%s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	page = getStandort(cfg, "anna", "?ok=verbunden").Body.String()
	mustContain(t, page, html.EscapeString(saysStandortVerbunden), "Verbunden seit 2. November 2026", "Trennen", `data-frage=`, `href="https://reitti.box.test"`)
	if strings.Contains(page, "gut-0123") || strings.Contains(page, `name=token`) {
		t.Fatalf("the token or its field is on the page:\n%s", page)
	}
	if strings.Contains(getStandort(cfg, "ben", "").Body.String(), "Verbunden seit") {
		t.Fatal("ben sees anna's connection")
	}
	if strings.Contains(logs.String(), "gut-0123") || strings.Contains(logs.String(), "ohne-geraet") {
		t.Fatalf("a token in the log:\n%s", logs.String())
	}

	// only from Homes' own page
	if rec := post(t, h, StandortPath, url.Values{"tun": {"trennen"}}, "anna", map[string]string{"Sec-Fetch-Site": "cross-site"}); rec.Code != 403 {
		t.Fatalf("a cross-site Trennen → %d", rec.Code)
	}
	rec = post(t, h, StandortPath, url.Values{"tun": {"trennen"}}, "anna", nil)
	if rec.Code != 303 || rec.Header().Get("Location") != StandortPath+"?ok=getrennt" {
		t.Fatalf("Trennen → %d", rec.Code)
	}
	if _, ok := cfg.Reitti.Konto("anna"); ok {
		t.Fatal("still connected")
	}
	mustContain(t, getStandort(cfg, "anna", "?ok=getrennt").Body.String(), html.EscapeString(saysStandortGetrennt), `name=token`)
}

// Reitti not answering is said as such, not as a wrong token.
func TestStandortSaysWhenReittiIsDown(t *testing.T) {
	cfg, srv := standortCfg(t)
	srv.Close()
	rec := post(t, Standort(cfg), StandortPath, url.Values{"tun": {"verbinden"}, "token": {"gut-0123"}}, "anna", nil)
	if rec.Code != 502 || !strings.Contains(rec.Body.String(), html.EscapeString(saysStandortWeg)) {
		t.Fatalf("→ %d\n%s", rec.Code, rec.Body.String())
	}
}

// Without Reitti the page says so, and Mehr does not list it; with it, Mehr does.
func TestStandortOnlyWhereTheBoxHasReitti(t *testing.T) {
	rec := getStandort(StartConfig{}, "anna", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "nicht eingerichtet") || strings.Contains(rec.Body.String(), "<form") {
		t.Fatalf("%d\n%s", rec.Code, rec.Body.String())
	}
	mehr := func(cfg StartConfig) string {
		req := httptest.NewRequest("GET", "/mehr", nil)
		req.Header.Set("Remote-User", "anna")
		rec := httptest.NewRecorder()
		Mehr(cfg).ServeHTTP(rec, req)
		return rec.Body.String()
	}
	if strings.Contains(mehr(StartConfig{}), "Standortverlauf") {
		t.Error("Mehr lists Standortverlauf on a box without Reitti")
	}
	cfg, _ := standortCfg(t)
	if !strings.Contains(mehr(cfg), "Standortverlauf") {
		t.Error("Mehr lacks Standortverlauf")
	}
}

// Once connected the page leads with what the person came for (owner
// 2026-11-04: „Ab dem Moment der Koppelung ist es aber das wichtigste“):
// the timeline first and big, then the state; the step in the app only
// while no point has come; Trennen last, quiet and asked.
func TestStandortConnectedLeadsWithTheTimeline(t *testing.T) {
	cfg, _ := standortCfg(t)
	if err := cfg.Reitti.Verbinden(t.Context(), "anna", "gut-0123"); err != nil {
		t.Fatal(err)
	}
	cfg.Now = func() time.Time { return time.Date(2026, 11, 2, 18, 0, 0, 0, time.UTC) }
	open := `<a class=vorne href="https://reitti.box.test">` + texte.T(texte.StandortOeffnen) + `</a>`
	step := html.EscapeString(texte.T(texte.StandortEinschalten))

	// connected, no point yet: the button, the state, the step, Trennen
	page := getStandort(cfg, "anna", "").Body.String()
	mustContain(t, page, html.EscapeString(texte.T(texte.StandortKurz)), `<span class="punkt gelb"></span>Verbunden seit 2. November 2026`, `data-frage=`)
	mustNotContain(t, page, html.EscapeString(saysStandortWas), html.EscapeString(saysStandortApp), "Reitti öffnen")
	before(t, page, "<h1>Standortverlauf</h1>", open)
	before(t, page, open, "class=zustand")
	before(t, page, "class=zustand", step)
	before(t, page, step, "class=leise-weg")
	trennenLast(t, page)

	// a point came: the state says when, the step is gone
	if err := cfg.Reitti.Angekommen("anna", time.Date(2026, 11, 2, 8, 40, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	page = getStandort(cfg, "anna", "").Body.String()
	mustContain(t, page, "Verbunden · zuletzt aufgezeichnet heute 08:40")
	mustNotContain(t, page, step, "Verbunden seit")
	before(t, page, open, "zuletzt aufgezeichnet")
	trennenLast(t, page)

	// without Reitti's own address there is no button; the state leads
	cfg.Reitti.PublicURL = ""
	page = getStandort(cfg, "anna", "").Body.String()
	mustNotContain(t, page, "class=vorne")
	before(t, page, "zuletzt aufgezeichnet", "class=leise-weg")
}

// trennenLast says the quiet Trennen ends the page: nothing to do after it.
func trennenLast(t *testing.T, page string) {
	t.Helper()
	i := strings.Index(page, "<form class=leise-weg")
	if i < 0 || !strings.Contains(page[i:], `<button data-laeuft="Trennt …">Trennen</button></form>`) {
		t.Fatalf("no quiet Trennen:\n%s", page)
	}
	for _, after := range []string{"<form", "class=vorne", "class=zustand", "class=schritt", "<section", "role=alert"} {
		if strings.Contains(page[i+len("<form"):], after) {
			t.Errorf("%q after Trennen:\n%s", after, page[i:])
		}
	}
}

// The Mehr audit (owner 2026-11-04): Mehr lists what a person uses day to day
// first; what is set up once comes lower, quieter, each row saying where it
// stands from what Homes keeps itself; the account is one row.
func TestMehrSaysWhereEachStands(t *testing.T) {
	cfg, _ := standortCfg(t)
	_, net := netzlaufwerk(t, true)
	cfg.Samba, cfg.SambaHost = net.Samba, net.SambaHost
	cfg.Module = modulListe(t, "dokumente", "standort", "netzlaufwerk")
	cfg.Ablage = napi.NewUploads(t.TempDir())
	page := func(user string) string {
		req := httptest.NewRequest("GET", "/mehr", nil)
		req.Header.Set("Remote-User", user)
		rec := httptest.NewRecorder()
		Mehr(cfg).ServeHTTP(rec, req)
		return rec.Body.String()
	}
	anna := page("anna")
	mustContain(t, anna, `<section class="karte leise" aria-labelledby=karte-einrichten>`,
		`<a href="/standort">Standortverlauf</a> <span class=k>Noch nicht verbunden</span>`,
		`<a href="/netzlaufwerk">Netzlaufwerk</a> <span class=k>Noch kein Passwort</span>`,
		`<a href="/abmelden">Abmelden</a> <span class=k>Angemeldet als anna</span>`)
	mustNotContain(t, anna, "Danach meldet sich jemand anderes an")
	before(t, anna, `href="/dokumente"`, "karte-einrichten")
	before(t, anna, "karte-einrichten", "karte-konto")
	cfg.Reitti.Verbinden(context.Background(), "ben", "gut-0123")
	mustContain(t, page("ben"), "Verbunden, noch kein Punkt angekommen", "Passwort seit 3. Mai 2026")
}
