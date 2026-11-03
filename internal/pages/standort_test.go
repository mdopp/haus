package pages

import (
	"bytes"
	"html"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/homes/internal/reitti"
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
	return StartConfig{Reitti: r, PublicURL: "https://homes.box.test", Loc: time.UTC}, srv
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
	mustContain(t, page, html.EscapeString(saysStandortVerbunden), "Verbunden seit 2. November 2026", "Trennen", `data-frage=`)
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
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "noch nicht eingerichtet") || strings.Contains(rec.Body.String(), "<form") {
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
