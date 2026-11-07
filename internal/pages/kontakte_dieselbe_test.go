package pages

import (
	"fmt"
	"image/color"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mdopp/homes/internal/gesichter"
	"github.com/mdopp/homes/internal/immich"
	"github.com/mdopp/homes/internal/immich/immichfake"
)

// dieselbeConfig: anna's key; Immich has six named persons, Anna (40
// photos) the first, each with three unnamed faces near it (u0 closest to
// Anna, 1 photo): 18 pairs; the hourly run done.
func dieselbeConfig(t *testing.T) (StartConfig, *immichfake.Server) {
	t.Helper()
	_, c := addressBook(t, "x|Xaver||")
	fake := immichfake.New("key")
	t.Cleanup(fake.Close)
	for i, name := range []string{"Anna", "Ben", "Carla", "Dora", "Emil", "Frida"} {
		id := "p-" + strings.ToLower(name)
		fake.Person(id, name, "1", false, faceJPEG(t, color.RGBA{200, uint8(30 * i), 40, 255}))
		fake.Lage(id, 40-i, float64(100*i), 0, false)
		for j := range 3 {
			u := fmt.Sprint("u", 3*i+j)
			fake.Person(u, "", "1", false, faceJPEG(t, color.RGBA{40, uint8(10 * j), 40, 255}))
			fake.Lage(u, 3*i+j+1, float64(100*i+j+1), 0, false)
		}
	}
	g := &gesichter.Gesichter{Immich: &immich.Client{URL: fake.URL, Key: "key"}, Kontakte: c, Web: "https://photos.box.test", Dir: t.TempDir()}
	g.Auffrischen(t.Context())
	g.Vorschlagen(t.Context())
	return StartConfig{Contacts: c, Gesichter: g}, fake
}

// antworte posts an answer as anna from Homes' own page.
func antworte(cfg StartConfig, user string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", DieselbePath, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Remote-User", user)
	req.Header.Set("Remote-Email", user+"@box.test")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	rec := httptest.NewRecorder()
	Dieselbe(cfg).ServeHTTP(rec, req)
	return rec
}

// Only the key's owner: a link under Kontakte and Meine Kontakte while
// there are pairs, the page with the two faces, names, photo counts and
// three buttons; anyone else goes to Kontakte, and a post of theirs
// merges nothing.
func TestDieselbeNurEigentuemer(t *testing.T) {
	cfg, fake := dieselbeConfig(t)
	mustContain(t, as(Kontakte, cfg, KontaktePath, "anna", "anna@box.test").Body.String(), `<a href=/kontakte/dieselbe>Dieselbe Person?</a> · 18 Vorschläge`)
	mustContain(t, as(MeineKontakte, cfg, MeineKontaktePath, "anna", "anna@box.test").Body.String(), `<a href=/kontakte/dieselbe>Dieselbe Person?</a>`)
	mustNotContain(t, as(Kontakte, cfg, KontaktePath, "ben", "ben@box.test").Body.String(), DieselbePath)
	rec := as(Dieselbe, cfg, DieselbePath, "anna", "anna@box.test")
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, body)
	}
	mustContain(t, body, "<h1>Dieselbe Person?</h1>", "<b>Anna</b><span>40 Fotos</span>", "<i>ohne Namen</i><span>1 Foto</span>", `src="data:image/jpeg;base64,`,
		`name=a value="p-anna"`, `name=b value="u0"`, `value=ja data-laeuft="Führt zusammen …">Ja, dieselbe Person</button>`, `>Nein</button>`, `>Überspringen</button>`)
	if strings.Count(body, "<section class=paar>") != 10 || !strings.Contains(body, "Seite 1 von 2") {
		t.Fatalf("not ten a page: %d", strings.Count(body, "<section class=paar>"))
	}
	mustContain(t, as(Dieselbe, cfg, DieselbePath+"?seite=2", "anna", "anna@box.test").Body.String(), "Seite 2 von 2", `href="/kontakte/dieselbe?seite=1">Zurück`)
	if rec := as(Dieselbe, cfg, DieselbePath, "ben", "ben@box.test"); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != KontaktePath {
		t.Fatalf("ben: %d", rec.Code)
	}
	if rec := antworte(cfg, "ben", url.Values{"a": {"p-anna"}, "b": {"u0"}, "antwort": {"ja"}}); rec.Code != http.StatusSeeOther || len(fake.Merges()) != 0 {
		t.Fatalf("ben merged: %d %v", rec.Code, fake.Merges())
	}
}

// Ja merges in Immich and says into whom; Nein is said and kept; a pair
// gone is said; an answer from elsewhere or no answer is refused; a
// missing person.merge is named on the page.
func TestDieselbeAntworten(t *testing.T) {
	cfg, fake := dieselbeConfig(t)
	as(Kontakte, cfg, KontaktePath, "anna", "anna@box.test") // Homes learns whose key it is
	rec := antworte(cfg, "anna", url.Values{"a": {"p-anna"}, "b": {"u0"}, "antwort": {"ja"}})
	mustContain(t, rec.Body.String(), "Zusammengeführt: Die Fotos gehören jetzt zu Anna.")
	if fmt.Sprint(fake.Merges()) != "[[p-anna u0]]" {
		t.Fatalf("%v", fake.Merges())
	}
	mustContain(t, antworte(cfg, "anna", url.Values{"a": {"u0"}, "b": {"p-anna"}, "antwort": {"nein"}}).Body.String(), "Diesen Vorschlag gibt es nicht mehr.")
	mustContain(t, antworte(cfg, "anna", url.Values{"a": {"p-ben"}, "b": {"u3"}, "antwort": {"nein"}}).Body.String(), "Gemerkt: Nach diesen beiden fragt Homes nicht wieder.")
	alle := as(Dieselbe, cfg, DieselbePath, "anna", "anna@box.test").Body.String() + as(Dieselbe, cfg, DieselbePath+"?seite=2", "anna", "anna@box.test").Body.String()
	mustContain(t, alle, `name=b value="u4"`)
	mustNotContain(t, alle, `name=b value="u3"`)
	if rec := antworte(cfg, "anna", url.Values{"a": {"p-anna"}, "b": {"u2"}}); rec.Code != http.StatusBadRequest {
		t.Fatalf("no answer: %d", rec.Code)
	}
	req := httptest.NewRequest("POST", DieselbePath, strings.NewReader("a=p-anna&b=u2&antwort=ja"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Remote-User", "anna")
	req.Header.Set("Remote-Email", "anna@box.test")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec = httptest.NewRecorder()
	if Dieselbe(cfg).ServeHTTP(rec, req); rec.Code != http.StatusForbidden {
		t.Fatalf("from elsewhere: %d", rec.Code)
	}
	fake.Ohne = []string{"person.merge"}
	cfg.Gesichter.Vorschlagen(t.Context())
	p := cfg.Gesichter.Paare()[0]
	mustContain(t, antworte(cfg, "anna", url.Values{"a": {p.A.ID}, "b": {p.B.ID}, "antwort": {"ja"}}).Body.String(), "fehlt diese Berechtigung: person.merge")
	if len(fake.Merges()) != 1 {
		t.Fatal("merged without the permission")
	}
}

// Each face opens all of its person's photos in Immich in a new tab, and
// under it a grey line each for its places and years and who else is in
// its photos, read in the background (owner 2026-11-07).
func TestDieselbeKontext(t *testing.T) {
	cfg, fake := dieselbeConfig(t)
	fake.Bibliothek = []immichfake.Foto{
		{Wann: "2019-07-01T10:00:00.000Z", Stadt: "Münster", Wer: []string{"p-anna", "p-ben"}},
		{Wann: "2012-07-01T10:00:00.000Z", Land: "Italien", Wer: []string{"p-anna", "u0"}},
	}
	cfg.Gesichter.Umfelder(t.Context())
	body := as(Dieselbe, cfg, DieselbePath, "anna", "anna@box.test").Body.String()
	mustContain(t, body, `<a href="https://photos.box.test/people/p-anna" target=_blank rel=noopener aria-label="Alle Fotos von Anna in Immich"><img`,
		`<a href="https://photos.box.test/people/u0" target=_blank rel=noopener aria-label="Alle Fotos dieses Gesichts in Immich">`,
		`<small>Orte: Italien, Münster · 2012–2019</small><small>Auch dabei: Ben</small>`, `<small>Orte: Italien · 2012</small><small>Auch dabei: Anna</small>`)
}
