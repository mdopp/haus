package pages

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/mdopp/haus/internal/abs"
	"github.com/mdopp/haus/internal/abs/absfake"
	"github.com/mdopp/haus/internal/caldav"
	"github.com/mdopp/haus/internal/caldav/davfake"
	"github.com/mdopp/haus/internal/gesehen"
	"github.com/mdopp/haus/internal/jellyfin"
)

// filmeFake is Jellyfin's watch history for the page: per person, an
// error, or a wait longer than the page's.
type filmeFake struct {
	seen map[string][]jellyfin.Gesehen
	err  error
	slow bool
}

func (f *filmeFake) Geschaut(ctx context.Context, person string, n int) ([]jellyfin.Gesehen, error) {
	if f.slow {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if f.err != nil {
		return nil, f.err
	}
	if _, ok := f.seen[person]; !ok {
		return nil, jellyfin.ErrNoUser
	}
	return f.seen[person], nil
}

func (f *filmeFake) HatFilm(context.Context, string) (string, bool, error) { return "", false, nil }

// haus#152: Gesehen & Gehört under Mehr — the person's films and series
// from Jellyfin and books from Audiobookshelf, last first, with the
// episode, the year, how far; „Was als Nächstes?“ puts a title on the film
// and book wish list and says so on the page; a missing account, an app
// not answering or answering too late each one plain line; a wish that
// fails keeps its title; only from Haus's own page; no backends, no page.
func TestGesehenPage(t *testing.T) {
	now := startNow(t)
	filme := &filmeFake{seen: map[string][]jellyfin.Gesehen{
		"anna": {{Name: "Dark", Serie: true, Folge: "S2E3 · Der Plan", Zuletzt: now.Add(-24 * time.Hour)}, {Name: "Dune", Jahr: 2021, Fertig: true, Zuletzt: now.Add(-72 * time.Hour)}},
		"ben":  nil}}
	books := absfake.New("k")
	defer books.Close()
	books.Hoert("anna", "Krabat", 0.4, false, now.Add(-time.Hour))
	books.Hoert("anna", "Momo", 1, true, now.Add(-48*time.Hour))
	dav := davfake.New("haus", "pw")
	defer dav.Close()
	svc := &gesehen.Service{Filme: filme, Buecher: &abs.Client{URL: books.URL, Token: "k"}, DAV: &caldav.Client{URL: dav.URL, User: "haus", Password: "pw"}}
	cfg := StartConfig{Gesehen: svc, Loc: berlin(t), Now: func() time.Time { return now }}

	code, body, _ := familyPage(t, Gesehen, cfg, "GET", GesehenPath, "family")
	if code != 200 {
		t.Fatalf("%d", code)
	}
	mustContain(t, body, "<h1>Gesehen &amp; Gehört</h1>", "Dark <span class=k>Serie · S2E3 · Der Plan · gestern", "Dune <span class=k>Film · 2021 · ",
		"Krabat <span class=k>Hörbuch · 40 % · heute", "Momo <span class=k>Hörbuch · zu Ende gehört · ", "<h2 id=k-naechstes>Was als Nächstes?</h2>",
		`<input type=radio name=art value=Film checked>`, `data-laeuft="Schaut nach …"`, "Noch nichts gewünscht")
	before(t, body, "Dark", "Dune")
	before(t, body, "Krabat", "Momo")
	mustContain(t, mehrOf(t, body), `href=/gesehen aria-current=page>`)

	_, body, _ = familyPageAs(t, Gesehen, cfg, "GET", GesehenPath, "ben", "family")
	mustContain(t, body, saysJellyfinNone, saysABSNoUser)
	mustNotContain(t, body, "Krabat", "Dark")
	_, body, _ = familyPageAs(t, Gesehen, cfg, "GET", GesehenPath, "carla", "family")
	mustContain(t, body, saysJellyfinNoUser)

	h := Gesehen(cfg)
	rec := post(t, h, GesehenPath, url.Values{"tun": {"wuenschen"}, "titel": {"Arrival"}, "art": {"Film"}}, "anna", nil)
	if rec.Code != 200 {
		t.Fatalf("wish: %d", rec.Code)
	}
	mustContain(t, rec.Body.String(), "<div class=gut role=status><span class=punkt></span><p>Auf der Wunschliste „Filme &amp; Bücher“: „Arrival“.", "Arrival (Film) <span class=k>von anna")
	rec = post(t, h, GesehenPath, url.Values{"tun": {"wuenschen"}, "titel": {"Krabat"}, "art": {"Buch"}}, "anna", nil)
	mustContain(t, rec.Body.String(), "Krabat (Buch)", `<input type=radio name=art value=Buch checked>`)
	rec = post(t, h, GesehenPath, url.Values{"tun": {"wuenschen"}, "titel": {"  "}}, "anna", nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("no title: %d", rec.Code)
	}
	mustContain(t, rec.Body.String(), "<div class=achtung role=alert><p>"+gesehen.ErrNoTitle.Error())
	if rec := post(t, h, GesehenPath, url.Values{"tun": {"wuenschen"}, "titel": {"X"}}, "anna", map[string]string{"Sec-Fetch-Site": "cross-site"}); rec.Code != http.StatusForbidden {
		t.Errorf("cross-site → %d", rec.Code)
	}
	if rec := post(t, h, GesehenPath, url.Values{"tun": {"loeschen"}}, "anna", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("another tun → %d", rec.Code)
	}

	// Radicale away: the wish fails plainly, its title stays in the field
	gone := &gesehen.Service{Filme: filme, DAV: &caldav.Client{URL: dav.URL + "/weg", User: "haus", Password: "falsch"}}
	rec = post(t, Gesehen(StartConfig{Gesehen: gone}), GesehenPath, url.Values{"tun": {"wuenschen"}, "titel": {"Arrival"}, "art": {"Serie"}}, "anna", nil)
	mustContain(t, rec.Body.String(), saysWunschDown, `value="Arrival"`, `<input type=radio name=art value=Serie checked>`)
	mustNotContain(t, rec.Body.String(), "401", "jellyfin")

	// Jellyfin refusing, then too slow: one plain line each, the rest of the page still there
	filme.err = errors.New("jellyfin 500")
	_, body, _ = familyPage(t, Gesehen, cfg, "GET", GesehenPath, "family")
	mustContain(t, body, saysJellyfinDown, "Krabat")
	filme.err, filme.slow = nil, true
	old := backendWait
	backendWait = 100 * time.Millisecond
	defer func() { backendWait = old }()
	_, body, _ = familyPage(t, Gesehen, cfg, "GET", GesehenPath, "family")
	mustContain(t, body, saysJellyfinDown, "Krabat")

	// only Audiobookshelf, no list: no Geschaut card, no wish form
	_, body, _ = familyPage(t, Gesehen, StartConfig{Gesehen: &gesehen.Service{Buecher: svc.Buecher}}, "GET", GesehenPath, "family")
	mustContain(t, body, "Krabat")
	mustNotContain(t, body, "Geschaut", "Was als Nächstes?")
	if code, _, _ := familyPage(t, Gesehen, StartConfig{}, "GET", GesehenPath, "family"); code != http.StatusNotFound {
		t.Errorf("without backends → %d", code)
	}
	if code, _, _ := familyPage(t, Gesehen, cfg, "GET", GesehenPath+"/x", "family"); code != http.StatusNotFound {
		t.Errorf("below → %d", code)
	}
}
