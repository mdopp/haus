package pages

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/mdopp/haus/internal/abs"
	"github.com/mdopp/haus/internal/abs/absfake"
	"github.com/mdopp/haus/internal/merken"
	"github.com/mdopp/haus/internal/podcasts"
)

// haus#123: Podcasts under Mehr — subscribe by a link, one's own podcasts
// with Abo beenden (asked first), their newest episodes; the sentence of
// what happened on the page itself; what cannot be found said plainly, the
// link kept; only for the person signed in, only from Haus's own page.
func TestPodcastsPage(t *testing.T) {
	fake := absfake.New("k")
	defer fake.Close()
	feeds := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Write([]byte(`<?xml version="1.0"?><rss><channel><title>x</title></channel></rss>`))
	}))
	defer feeds.Close()
	fake.AddFeed(feeds.URL+"/lage", "Lage der Nation")
	svc := podcasts.Open(&abs.Client{URL: fake.URL, Token: "k"}, t.TempDir())
	svc.Fetch = merken.Fetcher{AllowPrivate: true}
	cfg := StartConfig{Podcasts: svc}
	h := Podcasts(cfg)

	_, body, _ := familyPage(t, Podcasts, cfg, "GET", PodcastsPath, "family")
	mustContain(t, body, "<h1>Podcasts</h1>", saysPodcastsHow, `<input type=hidden name=tun value=abonnieren>`, "Noch kein Podcast abonniert")
	mustContain(t, mehrOf(t, body), `href=/podcasts aria-current=page>`)

	rec := post(t, h, PodcastsPath, url.Values{"tun": {"abonnieren"}, "link": {feeds.URL + "/lage"}}, "anna", nil)
	if rec.Code != 200 {
		t.Fatalf("%d", rec.Code)
	}
	mustContain(t, rec.Body.String(), "<div class=gut role=status><span class=punkt></span><p>Abonniert: „Lage der Nation“.", "<h2 id=k-meine>Meine Podcasts</h2>",
		`data-frage="„Lage der Nation“ nicht mehr abonnieren?"`, "Folge 5 <span class=k>Lage der Nation · ") // the newest three, fetched at once
	fake.Episode("Lage der Nation", "LdN 401", "")
	_, body, _ = familyPageAs(t, Podcasts, cfg, "GET", PodcastsPath, "anna", "family")
	mustContain(t, body, "LdN 401 <span class=k>Lage der Nation · ")
	_, body, _ = familyPageAs(t, Podcasts, cfg, "GET", PodcastsPath, "ben", "family")
	mustContain(t, body, "Noch kein Podcast abonniert")
	mustNotContain(t, body, "LdN 401")

	rec = post(t, h, PodcastsPath, url.Values{"tun": {"abonnieren"}, "link": {"kein link"}}, "anna", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("no link: %d", rec.Code)
	}
	mustContain(t, rec.Body.String(), merken.ErrNoAddress.Error(), `value="kein link"`)
	if rec := post(t, h, PodcastsPath, url.Values{"tun": {"abonnieren"}, "link": {feeds.URL + "/lage"}}, "anna", map[string]string{"Sec-Fetch-Site": "cross-site"}); rec.Code != http.StatusForbidden {
		t.Errorf("cross-site → %d", rec.Code)
	}
	mine, _ := svc.Meine("anna")
	rec = post(t, h, PodcastsPath, url.Values{"tun": {"beenden"}, "feed": {mine[0].Feed}}, "anna", nil)
	mustContain(t, rec.Body.String(), "Abo beendet: „Lage der Nation“. Er ist von der Box genommen.")
	if len(fake.Items()) != 0 {
		t.Fatalf("still in ABS: %v", fake.Items())
	}
	if code, _, _ := familyPage(t, Podcasts, StartConfig{}, "GET", PodcastsPath, "family"); code != http.StatusNotFound {
		t.Errorf("without ABS → %d", code)
	}
}
