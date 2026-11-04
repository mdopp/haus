package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mdopp/homes/internal/laib"
	"github.com/mdopp/homes/internal/laib/laibfake"
)

// moduleSite is leafSite with podcasts switched off and the wish fake as
// its Laib, which also takes laib_module (laib#1036).
func moduleSite(t *testing.T) (func() http.Handler, *laibfake.Laib) {
	t.Helper()
	f := laibfake.New()
	srv := f.Server()
	t.Cleanup(srv.Close)
	base := leafSite(t)
	base.napi.Reitti = nil // standort fehlt
	s, _ := switched(t, base, "podcasts")
	return func() http.Handler {
		s.ops = &laib.Client{URL: srv.URL, Token: "homes"}
		s.napi.Laib = s.ops
		return routes(s)
	}, f
}

// H7: Box → Module lists every module, its backend, on or off with why,
// and its wish; each row leads to the module's page. Admins only.
func TestBoxModuleListsEveryModule(t *testing.T) {
	fresh, f := moduleSite(t)
	f.Setze("standort", "wartet", "")
	h := fresh()
	rec := as(h, "GET", "/box/teil?was=module", "anna", "family,admins", nil)
	var parts map[string]string
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &parts) != nil {
		t.Fatalf("the part → %d %s", rec.Code, rec.Body)
	}
	card := parts["box-module"]
	for _, want := range []string{"Was Homes kann", `href="/module/geraete"`, "home-assistant · an", "Podcasts", "ausgeschaltet: Auf dieser Box ausgeschaltet.",
		"Standortverlauf", "auf dieser Box nicht eingerichtet: Auf dieser Box läuft kein Reitti.", "wartet auf Freigabe, gewünscht von anna seit", "Werkstatt", "Was wird genutzt"} {
		if !strings.Contains(card, want) {
			t.Errorf("the card lacks %q:\n%s", want, card)
		}
	}
	if page := as(h, "GET", "/box/module", "anna", "family,admins", nil); page.Code != 200 || !strings.Contains(page.Body.String(), "box-module") {
		t.Errorf("/box/module → %d", page.Code)
	}
	if rec := as(h, "GET", "/box/module", "anna", "family", nil); rec.Code == 200 {
		t.Errorf("a family member sees /box/module")
	}
}

// H7: the module's page proposes the switch to Laib (laib_module, the
// door verb whose proposal waits for a human): off for one that is on,
// on again for one switched off, none for one whose backend is missing.
// The answer ends visibly: the page says it waits in Laib's Inbox, or one
// sentence why not.
func TestModuleSwitchProposes(t *testing.T) {
	fresh, f := moduleSite(t)
	h := fresh()
	admin := func(method, path string, form url.Values) *httptest.ResponseRecorder {
		return as(h, method, path, "anna", "family,admins", form)
	}
	if b := admin("GET", "/module/gesehen", nil).Body.String(); !strings.Contains(b, ">Ausschalten vorschlagen</button>") {
		t.Errorf("gesehen (on) offers no switch")
	}
	if b := admin("GET", "/module/podcasts", nil).Body.String(); !strings.Contains(b, ">Wieder einschalten vorschlagen</button>") {
		t.Errorf("podcasts (aus) offers no way back")
	}
	if b := admin("GET", "/module/standort", nil).Body.String(); strings.Contains(b, "vorschlagen</button>") {
		t.Errorf("standort (fehlt) offers a switch")
	}
	rec := admin("POST", "/module/gesehen", url.Values{"tun": {"aus"}})
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/module/gesehen?vorgeschlagen=17") {
		t.Fatalf("switching gesehen off → %d %q %s", rec.Code, rec.Header().Get("Location"), rec.Body)
	}
	if len(f.Modul) != 1 || f.Modul[0] != "podcasts,gesehen|Gesehen & Gehört verschwindet aus Homes und der App." {
		t.Errorf("the proposal: %v", f.Modul)
	}
	if b := admin("GET", rec.Header().Get("Location"), nil).Body.String(); !strings.Contains(b, "wartet in Laibs Inbox") || !strings.Contains(b, "https://laib.example/inbox") {
		t.Errorf("the page does not say it waits:\n%s", b)
	}
	if rec := admin("POST", "/module/podcasts", url.Values{"tun": {"an"}}); rec.Code != http.StatusSeeOther || f.Modul[1] != "|Podcasts kommt in Homes und die App zurück." {
		t.Errorf("podcasts on again → %d, %v", rec.Code, f.Modul)
	}
	for _, c := range []struct {
		path, tun string
		code      int
	}{{"/module/standort", "aus", 409}, {"/module/podcasts", "aus", 409}, {"/module/gesehen", "an", 409}, {"/module/geraete", "aus", 409}} {
		if rec := admin("POST", c.path, url.Values{"tun": {c.tun}}); rec.Code != c.code || !strings.Contains(rec.Body.String(), "lässt sich noch nicht") {
			t.Errorf("%s %s → %d %s", c.path, c.tun, rec.Code, rec.Body)
		}
	}
	if rec := as(h, "POST", "/module/gesehen", "anna", "family", url.Values{"tun": {"aus"}}); rec.Code != 404 {
		t.Errorf("a family member's POST → %d", rec.Code)
	}
	f.Kaputt = true
	if rec := admin("POST", "/module/gesehen", url.Values{"tun": {"aus"}}); rec.Code != 502 || !strings.Contains(rec.Body.String(), "nicht angenommen") {
		t.Errorf("Laib failing → %d %s", rec.Code, rec.Body)
	}
	f.Kaputt, f.Ohne = false, true
	if rec := as(fresh(), "POST", "/module/gesehen", "anna", "family,admins", url.Values{"tun": {"aus"}}); rec.Code != 503 || !strings.Contains(rec.Body.String(), "Update") {
		t.Errorf("an older Laib → %d %s", rec.Code, rec.Body)
	}
}
