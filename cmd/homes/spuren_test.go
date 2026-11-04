package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/homes/internal/laib"
	"github.com/mdopp/homes/internal/laib/laibfake"
	"github.com/mdopp/homes/internal/modul"
)

// spurSite is fullSite (no Reitti, no podcasts, …) whose Laib is the wish
// fake; fresh gives a new site, so a test sees a wish moved in the fake
// without waiting out the client's minute.
func spurSite(t *testing.T) (fresh func() http.Handler, f *laibfake.Laib) {
	t.Helper()
	f = laibfake.New()
	srv := f.Server()
	t.Cleanup(srv.Close)
	s := kannSite(t)
	return func() http.Handler {
		s.ops = &laib.Client{URL: srv.URL, Token: "homes"}
		s.napi.Laib = s.ops
		return routes(s)
	}, f
}

func as(h http.Handler, method, path, user, groups string, form url.Values) *httptest.ResponseRecorder {
	var req *http.Request
	if form != nil {
		req = httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req.Header.Set("X-Homes-Proxy", "geheim")
	if user != "" {
		req.Header.Set("Remote-User", user)
		req.Header.Set("Remote-Groups", groups)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// Mehr ends with what the box lacks (ADR 0001 §10): each off module once,
// quiet, with „Hier anfordern“ for a signed-in person; the admins'
// modules are not there; a module that is on has no trace.
func TestMehrShowsWhatTheBoxLacks(t *testing.T) {
	fresh, _ := spurSite(t)
	h := fresh()
	frag := as(h, "GET", "/mehr/spuren", "anna", "family", nil).Body.String()
	for _, want := range []string{"Gibt es hier noch nicht", `id="spur-standort"`, "Standortverlauf</span> <span class=k>auf dieser Box nicht eingerichtet",
		`name=modul value="standort"`, ">Hier anfordern</button>", "Podcasts"} {
		if !strings.Contains(frag, want) {
			t.Errorf("the traces lack %q:\n%s", want, frag)
		}
	}
	for _, not := range []string{"Werkstatt", "Was wird genutzt", "spur-geraete", "spur-kalender", "rot", "Fehler", "kaputt"} {
		if strings.Contains(frag, not) {
			t.Errorf("the traces show %q:\n%s", not, frag)
		}
	}
	if anon := as(h, "GET", "/mehr/spuren", "", "", nil).Body.String(); strings.Contains(anon, "Hier anfordern") || !strings.Contains(anon, "spur-standort") {
		t.Errorf("signed out: the trace without the button, got:\n%s", anon)
	}
	// the page itself never waits for Laib: the card is a place that fills itself
	if mehr := as(fresh(), "GET", "/mehr", "anna", "family", nil).Body.String(); !strings.Contains(mehr, `data-nachladen="/mehr/spuren"`) || !strings.Contains(mehr, "Standortverlauf") {
		t.Errorf("Mehr lacks the traces' place")
	}
}

// „Hier anfordern“ files the wish for the person and leads back to Mehr,
// where the trace says angefordert; Laib's states follow; abgelehnt
// offers it again.
func TestHierAnfordern(t *testing.T) {
	fresh, f := spurSite(t)
	h := fresh()
	rec := as(h, "POST", "/mehr/anfordern", "anna", "family", url.Values{"modul": {"standort"}})
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/mehr") {
		t.Fatalf("anfordern → %d %q %s", rec.Code, rec.Header().Get("Location"), rec.Body)
	}
	if f.Stand("standort") != "angefordert" {
		t.Fatalf("the fake has no wish: %q", f.Stand("standort"))
	}
	frag := as(h, "GET", "/mehr/spuren", "anna", "family", nil).Body.String()
	if !strings.Contains(frag, "Standortverlauf</span> <span class=k>angefordert") || strings.Contains(frag, `value="standort"`) {
		t.Errorf("after the wish:\n%s", frag)
	}
	for stand, want := range map[string]string{"wartet": "wartet auf Freigabe", "eingerichtet": "wird eingerichtet", "abgelehnt": "abgelehnt"} {
		f.Setze("standort", stand, "")
		frag := as(fresh(), "GET", "/mehr/spuren", "anna", "family", nil).Body.String()
		if !strings.Contains(frag, "Standortverlauf</span> <span class=k>"+want) {
			t.Errorf("%s: the trace does not say %q:\n%s", stand, want, frag)
		}
		if offers := strings.Contains(frag, `value="standort"`); offers != (stand == "abgelehnt") {
			t.Errorf("%s: button %v", stand, offers)
		}
	}
}

func TestHierAnfordernRefuses(t *testing.T) {
	fresh, f := spurSite(t)
	h := fresh()
	for _, c := range []struct {
		name, user, modul string
		code              int
	}{{"signed out", "", "standort", 404}, {"a module that is on", "anna", "geraete", 404}, {"unknown", "anna", "gibtsnicht", 404}} {
		rec := as(h, "POST", "/mehr/anfordern", c.user, "family", url.Values{"modul": {c.modul}})
		if rec.Code != c.code || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain") || rec.Body.Len() < 10 {
			t.Errorf("%s → %d %q %q", c.name, rec.Code, rec.Header().Get("Content-Type"), rec.Body)
		}
	}
	// another site's form: no Sec-Fetch-Site same-origin
	req := httptest.NewRequest("POST", "/mehr/anfordern", strings.NewReader("modul=standort"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Homes-Proxy", "geheim")
	req.Header.Set("Remote-User", "anna")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 404 || f.Stand("standort") != "" {
		t.Errorf("a cross-site POST → %d, wish %q", rec.Code, f.Stand("standort"))
	}
	f.Kaputt = true
	if rec := as(h, "POST", "/mehr/anfordern", "anna", "family", url.Values{"modul": {"standort"}}); rec.Code != 502 || !strings.Contains(rec.Body.String(), "nicht aufschreiben") {
		t.Errorf("Laib failing → %d %s", rec.Code, rec.Body)
	}
	f.Kaputt = false
	f.Ohne = true
	if rec := as(fresh(), "POST", "/mehr/anfordern", "anna", "family", url.Values{"modul": {"standort"}}); rec.Code != 503 || !strings.Contains(rec.Body.String(), "keine Wünsche") {
		t.Errorf("an older Laib → %d %s", rec.Code, rec.Body)
	}
	if frag := as(fresh(), "GET", "/mehr/spuren", "anna", "family", nil).Body.String(); strings.Contains(frag, "Hier anfordern") || !strings.Contains(frag, "nicht eingerichtet") {
		t.Errorf("an older Laib: no button, the plain words:\n%s", frag)
	}
}

// Laib not answering: the traces in their own words, no button, and a
// line that the wishes cannot be read.
func TestSpurenWithoutLaib(t *testing.T) {
	s := kannSite(t) // its Laib is 127.0.0.1:1
	frag := as(routes(s), "GET", "/mehr/spuren", "anna", "family", nil).Body.String()
	if !strings.Contains(frag, "auf dieser Box nicht eingerichtet") || strings.Contains(frag, "Hier anfordern") || !strings.Contains(frag, "lässt sich gerade nicht lesen") {
		t.Errorf("Laib down:\n%s", frag)
	}
}

func TestModulSeiteIsTheAdmins(t *testing.T) {
	fresh, f := spurSite(t)
	f.Setze("standort", "wartet", "")
	h := fresh()
	if rec := as(h, "GET", "/module/standort", "anna", "family", nil); rec.Code != 404 {
		t.Errorf("a family member → %d, want 404", rec.Code)
	}
	body := as(h, "GET", "/module/standort", "anna", "family,admins", nil).Body.String()
	for _, want := range []string{"<h1>Standortverlauf</h1>", "reitti", "Auf dieser Box läuft kein Reitti.", "wartet auf Freigabe", "/box/aufgaben"} {
		if !strings.Contains(body, want) {
			t.Errorf("/module/standort lacks %q", want)
		}
	}
	if rec := as(h, "GET", "/module/gibtsnicht", "anna", "family,admins", nil); rec.Code != 404 {
		t.Errorf("an unknown module → %d", rec.Code)
	}
}

// Homes ticks off the wish of a module that is on as the homes hand, and
// leaves an off module's alone.
func TestSchliesseWuensche(t *testing.T) {
	f := laibfake.New()
	srv := f.Server()
	defer srv.Close()
	f.Setze("geraete", "eingerichtet", "")
	f.Setze("standort", "angefordert", "")
	l, err := modul.Bauen(katalog(kannSite(t)))
	if err != nil {
		t.Fatal(err)
	}
	schliesseWuensche(context.Background(), &laib.Client{URL: srv.URL, Token: "homes"}, l, time.Millisecond)
	if f.Stand("geraete") != "umgesetzt" || f.Stand("standort") != "angefordert" || len(f.Erledigt) != 1 {
		t.Errorf("geraete %q, standort %q, ticked %v", f.Stand("geraete"), f.Stand("standort"), f.Erledigt)
	}
	f.Ohne = true // an older Laib: asked a few times, nothing ticked
	schliesseWuensche(context.Background(), &laib.Client{URL: srv.URL, Token: "homes"}, l, time.Microsecond)
	if !slices.Equal(f.Erledigt, []int{11}) {
		t.Errorf("ticked %v", f.Erledigt)
	}
}
