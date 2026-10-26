package abs

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mdopp/haus/internal/merken"
)

// haus#123: the feed behind a link — the feed itself, a website's feed
// link (relative too), an Apple Podcasts page by its id, a Spotify show by
// its name; a Spotify-only show and a page with nothing are said so.
func TestFindFeed(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/feed.xml":
			w.Header().Set("Content-Type", "application/rss+xml")
			w.Write([]byte(`<?xml version="1.0"?><rss version="2.0"><channel><title>Lage</title></channel></rss>`))
		case "/podcast":
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(`<html><head><link rel="alternate" type="application/rss+xml" title="Feed" href="/feed.xml"></head></html>`))
		case "/show/abc":
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(`<html><head><meta property="og:title" content="Lage der Nation | Podcast on Spotify"></head></html>`))
		case "/show/nur":
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(`<html><head><title>Nur Bei Uns - Podcast on Spotify</title></head></html>`))
		case "/leer":
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(`<html><body>nichts</body></html>`))
		case "/lookup":
			if r.URL.Query().Get("id") == "1234567" {
				w.Write([]byte(`{"results":[{"feedUrl":"https://feeds.example/lage"}]}`))
				return
			}
			w.Write([]byte(`{"results":[]}`))
		case "/search":
			if strings.Contains(r.URL.Query().Get("term"), "Lage der Nation") {
				w.Write([]byte(`{"results":[{"feedUrl":"https://feeds.example/lage"}]}`))
				return
			}
			w.Write([]byte(`{"results":[]}`))
		}
	}))
	defer srv.Close()
	old := appleBase
	appleBase = srv.URL
	defer func() { appleBase = old }()
	f := merken.Fetcher{AllowPrivate: true}
	ctx := context.Background()
	for link, want := range map[string]string{
		srv.URL + "/feed.xml":              srv.URL + "/feed.xml",
		"Hör mal: " + srv.URL + "/podcast": srv.URL + "/feed.xml",
	} {
		if got, err := FindFeed(ctx, f, link); err != nil || got.Feed != want || got.ByName != "" {
			t.Errorf("%s → %+v %v", link, got, err)
		}
	}
	// Apple's page: the id through the lookup, without fetching the page (here a podcasts.apple.com address cannot be reached)
	if got, err := FindFeed(ctx, f, "https://podcasts.apple.com/de/podcast/lage-der-nation/id1234567"); err != nil || got.Feed != "https://feeds.example/lage" {
		t.Errorf("apple: %+v %v", got, err)
	}
	if got, err := FindFeed(ctx, f, srv.URL+"/show/abc"); err != nil || got.Feed != "https://feeds.example/lage" || got.ByName != "Lage der Nation" {
		t.Errorf("by name: %+v %v", got, err)
	}
	if _, err := FindFeed(ctx, f, srv.URL+"/leer"); !errors.Is(err, ErrNoFeedFound) {
		t.Errorf("nothing: %v", err)
	}
}

// A Spotify show without a feed anywhere is said to be Spotify's only.
func TestFindFeedSpotifyOnly(t *testing.T) {
	if pageName(`<title>Nur Bei Uns - Podcast on Spotify</title>`) != "Nur Bei Uns" || pageName(`<meta property="og:title" content="A &amp; B | Spotify">`) != "A & B" {
		t.Error("pageName")
	}
	if !isFeed("", `<?xml version="1.0"?><rss>`) || isFeed("text/html", "<html>") || !isFeed("application/atom+xml", "") {
		t.Error("isFeed")
	}
}

// owner 2026-10-26: a shared podcast is subscribed, not kept as a note —
// but a blog with a feed is a page, and no page is searched by its name.
func TestPodcastLink(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/lage":
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(`<html><head><link rel="alternate" type="application/rss+xml" title="Podcast Feed" href="/feed/mp3/"></head></html>`))
		case "/feed/mp3/":
			w.Header().Set("Content-Type", "application/rss+xml")
			w.Write([]byte(`<?xml version="1.0"?><rss xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd"><channel><item><enclosure url="x.mp3" type="audio/mpeg"/></item></channel></rss>`))
		case "/blog":
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(`<html><head><title>Ein Blog</title><link rel="alternate" type="application/rss+xml" href="/blogfeed"></head></html>`))
		case "/blogfeed":
			w.Header().Set("Content-Type", "application/rss+xml")
			w.Write([]byte(`<?xml version="1.0"?><rss><channel><item><title>Text</title></item></channel></rss>`))
		default:
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(`<html><head><title>Lage der Nation</title></head></html>`))
		}
	}))
	defer srv.Close()
	f := merken.Fetcher{AllowPrivate: true}
	ctx := context.Background()
	for link, want := range map[string]bool{
		srv.URL + "/lage":                             true,
		srv.URL + "/feed/mp3/":                        true,
		srv.URL + "/blog":                             false,
		srv.URL + "/irgendwas":                        false, // a page named like a podcast is still a page
		"https://podcasts.apple.com/de/podcast/x/id1": true,
		"https://open.spotify.com/show/2x":            true,
		"https://open.spotify.com/track/2x":           false,
		"kein link":                                   false,
	} {
		if got := PodcastLink(ctx, f, link); got != want {
			t.Errorf("PodcastLink(%s) = %v", link, got)
		}
	}
}
