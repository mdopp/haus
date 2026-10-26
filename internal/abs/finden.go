package abs

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	"net/url"
	"regexp"
	"strings"

	"github.com/mdopp/haus/internal/merken"
)

// Found is a podcast's feed and how it was found, in the family's words
// when it was a guess ("über den Namen").
type Found struct {
	Feed    string
	ByName  string // the name searched for when the link had no feed of its own
	Spotify bool   // the link was Spotify's
}

// appleBase is Apple's podcast directory; a variable for the tests.
var appleBase = "https://itunes.apple.com"

// ErrNoFeedFound is a link with no feed to find.
var ErrNoFeedFound = errors.New("Zu diesem Link finde ich keinen Podcast-Feed. Schick mir den Link von der Website des Podcasts oder aus Apple Podcasts.")

// ErrSpotifyOnly is a show only Spotify has: it has no feed at all.
var ErrSpotifyOnly = errors.New("Diesen Podcast gibt es offenbar nur bei Spotify; solche Podcasts haben keinen Feed, den die Box abonnieren kann.")

var (
	reAppleID  = regexp.MustCompile(`/id(\d{5,})`)
	reAltFeed  = regexp.MustCompile(`(?is)<link\s[^>]*>`)
	reHref     = regexp.MustCompile(`(?is)href\s*=\s*["']([^"']+)["']`)
	reType     = regexp.MustCompile(`(?is)type\s*=\s*["']application/(rss|atom)\+xml["']`)
	reTitleTag = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	reOGTitle  = regexp.MustCompile(`(?is)<meta\s[^>]*property\s*=\s*["']og:title["'][^>]*content\s*=\s*["']([^"']+)["']`)
)

// FindFeed is the feed behind a link someone sent: the feed itself, an
// Apple Podcasts page (by its id through Apple's lookup), a website's
// feed link, or — for a Spotify show or a page without one — a search on
// Apple's directory by the show's name.
func FindFeed(ctx context.Context, f merken.Fetcher, link string) (Found, error) {
	u, err := merken.Address(link)
	if err != nil {
		return Found{}, err
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	if strings.HasSuffix(host, "podcasts.apple.com") || strings.HasSuffix(host, "itunes.apple.com") {
		if m := reAppleID.FindStringSubmatch(u.Path); m != nil {
			if feed, err := appleLookup(ctx, f, appleBase+"/lookup?entity=podcast&id="+m[1]); err == nil {
				return Found{Feed: feed}, nil
			}
		}
	}
	final, ct, raw, _, err := f.Get(ctx, u)
	if err != nil {
		return Found{}, err
	}
	body := string(raw[:min(len(raw), 1<<20)])
	if isFeed(ct, body) {
		return Found{Feed: final}, nil
	}
	spotify := strings.HasSuffix(host, "spotify.com")
	if !spotify {
		for _, l := range reAltFeed.FindAllString(body, -1) {
			if !reType.MatchString(l) {
				continue
			}
			if h := reHref.FindStringSubmatch(l); h != nil {
				if ref, err := url.Parse(html.UnescapeString(h[1])); err == nil {
					base, _ := url.Parse(final)
					return Found{Feed: base.ResolveReference(ref).String()}, nil
				}
			}
		}
	}
	name := pageName(body)
	if name == "" {
		return Found{}, ErrNoFeedFound
	}
	feed, err := appleLookup(ctx, f, appleBase+"/search?media=podcast&limit=1&term="+url.QueryEscape(name))
	if err != nil {
		if spotify {
			return Found{}, ErrSpotifyOnly
		}
		return Found{}, ErrNoFeedFound
	}
	return Found{Feed: feed, ByName: name, Spotify: spotify}, nil
}

// isFeed is an RSS or Atom document.
func isFeed(ct, body string) bool {
	if strings.Contains(ct, "rss") || strings.Contains(ct, "atom") {
		return true
	}
	head := strings.ToLower(strings.TrimSpace(body[:min(len(body), 600)]))
	return (strings.HasPrefix(head, "<?xml") || strings.HasPrefix(head, "<rss")) && (strings.Contains(head, "<rss") || strings.Contains(head, "<feed"))
}

// pageName is a show's name from its page: og:title, else <title>, without
// what sites add ("| Podcast on Spotify").
func pageName(body string) string {
	name := ""
	if m := reOGTitle.FindStringSubmatch(body); m != nil {
		name = m[1]
	} else if m := reTitleTag.FindStringSubmatch(body); m != nil {
		name = m[1]
	}
	name = html.UnescapeString(strings.TrimSpace(name))
	for _, sep := range []string{" | ", " – ", " - "} {
		if i := strings.Index(name, sep); i > 0 {
			name = name[:i]
		}
	}
	return strings.TrimSpace(name)
}

// appleLookup asks Apple's podcast directory (lookup or search) and takes
// the first result's feedUrl.
func appleLookup(ctx context.Context, f merken.Fetcher, api string) (string, error) {
	u, err := url.Parse(api)
	if err != nil {
		return "", err
	}
	_, _, raw, _, err := f.Get(ctx, u)
	if err != nil {
		return "", err
	}
	var out struct {
		Results []struct {
			FeedURL string `json:"feedUrl"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || len(out.Results) == 0 || out.Results[0].FeedURL == "" {
		return "", ErrNoFeedFound
	}
	return out.Results[0].FeedURL, nil
}

// podcastHosts are where a link is a podcast by its address alone.
var podcastHosts = []string{"podcasts.apple.com", "pca.st", "pocketcasts.com", "overcast.fm", "castbox.fm", "podcasts.google.com", "podcastaddict.com", "fyyd.de"}

var (
	reEnclosure = regexp.MustCompile(`(?is)<enclosure\s[^>]*type\s*=\s*["']audio/`)
	reItunes    = regexp.MustCompile(`(?i)xmlns:itunes\s*=`)
)

// PodcastLink says whether a shared link is a podcast to subscribe to,
// not a page to keep: a podcast app's or directory's address, a Spotify
// show, a feed with audio, or a page whose own feed carries audio. A blog's
// feed without audio is no podcast; a page is never searched by its name.
func PodcastLink(ctx context.Context, f merken.Fetcher, link string) bool {
	u, err := merken.Address(link)
	if err != nil {
		return false
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	for _, h := range podcastHosts {
		if host == h || strings.HasSuffix(host, "."+h) {
			return true
		}
	}
	if host == "open.spotify.com" {
		return strings.Contains(u.Path, "/show/") || strings.Contains(u.Path, "/episode/")
	}
	final, ct, raw, _, err := f.Get(ctx, u)
	if err != nil {
		return false
	}
	body := string(raw[:min(len(raw), 1<<20)])
	if isFeed(ct, body) {
		return audioFeed(body)
	}
	for _, l := range reAltFeed.FindAllString(body, -1) {
		if !reType.MatchString(l) {
			continue
		}
		h := reHref.FindStringSubmatch(l)
		if h == nil {
			continue
		}
		ref, err := url.Parse(html.UnescapeString(h[1]))
		if err != nil {
			continue
		}
		base, _ := url.Parse(final)
		fu := base.ResolveReference(ref)
		_, _, feed, _, err := f.Get(ctx, fu)
		if err == nil && audioFeed(string(feed[:min(len(feed), 1<<20)])) {
			return true
		}
		break // the page's first feed is its own; a second is a comments feed
	}
	return false
}

// audioFeed is a feed whose items carry audio, or that names itself for
// podcast apps (the iTunes namespace).
func audioFeed(body string) bool {
	return reEnclosure.MatchString(body) || reItunes.MatchString(body)
}
