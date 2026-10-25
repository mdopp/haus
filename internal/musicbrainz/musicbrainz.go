// Package musicbrainz asks MusicBrainz (the open music database, no key)
// where a wished song or album is to be had (owner 2026-10-25: "erst
// einmal herausfinden ob die Band und der Song irgendwo ist / wo genau"):
// the album a song is on, when it comes out, its label, and the shops and
// pages the band, the album and the label name — Bandcamp above all, where
// a record usually comes with its download at once. Shops are never asked
// themselves: Bandcamp lets no program in, and Haus does not get round
// that. At most one request a second, as MusicBrainz asks.
package musicbrainz

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mdopp/haus/internal/jellyfin"
	"github.com/mdopp/haus/internal/merken"
)

// Client is MusicBrainz's web service.
type Client struct {
	Base  string // https://musicbrainz.org; the tests' own
	Fetch merken.Fetcher
	Pause time.Duration // between requests; default 1.1 s

	mu   sync.Mutex
	last time.Time
}

// New is MusicBrainz itself.
func New(f merken.Fetcher) *Client { return &Client{Base: "https://musicbrainz.org", Fetch: f} }

// Link is a place to get it: where ("Bandcamp", "Apple Music", "Label
// Wandamusik OG"), what it is ("Platte und Download", "Download",
// "Versand", "Seite der Band"), and the address.
type Link struct {
	Wo, Was, URL string
}

// Fund is what MusicBrainz knows of a wish.
type Fund struct {
	Album    string // the album (or single) it is on
	Datum    string // its first release, JJJJ-MM-TT or shorter
	Kommt    bool   // the date is still to come: it can be pre-ordered
	Label    string
	Links    []Link
	Gefunden bool
	Gesucht  time.Time
}

// ErrNichts is a wish MusicBrainz does not know.
var ErrNichts = errors.New("MusicBrainz kennt das nicht.")

func (c *Client) get(ctx context.Context, path string, q url.Values, v any) error {
	c.mu.Lock()
	pause := c.Pause
	if pause == 0 {
		pause = 1100 * time.Millisecond
	}
	if wait := pause - time.Since(c.last); wait > 0 {
		select {
		case <-ctx.Done():
			c.mu.Unlock()
			return ctx.Err()
		case <-time.After(wait):
		}
	}
	c.last = time.Now()
	c.mu.Unlock()
	if q == nil {
		q = url.Values{}
	}
	q.Set("fmt", "json")
	u, err := url.Parse(strings.TrimRight(c.Base, "/") + "/ws/2/" + path + "?" + q.Encode())
	if err != nil {
		return err
	}
	_, _, raw, _, err := c.Fetch.Get(ctx, u)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("musicbrainz %s: %w", path, err)
	}
	return nil
}

type credit []struct {
	Name   string `json:"name"`
	Artist struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"artist"`
}

func (cr credit) is(artist string) bool {
	want := jellyfin.Plain(artist)
	for _, a := range cr {
		if p := jellyfin.Plain(a.Name); p != "" && (p == want || jellyfin.Plain(a.Artist.Name) == want) {
			return true
		}
	}
	return want == ""
}

func (cr credit) id() string {
	if len(cr) == 0 {
		return ""
	}
	return cr[0].Artist.ID
}

type relations []struct {
	Type string `json:"type"`
	URL  struct {
		Resource string `json:"resource"`
	} `json:"url"`
}

// Suchen finds where artist's title is: a song (album false) or an album.
func (c *Client) Suchen(ctx context.Context, artist, title string, album bool, now time.Time) (Fund, error) {
	quote := func(s string) string { return `"` + strings.ReplaceAll(s, `"`, ``) + `"` }
	var releaseID, groupID, artistID string
	f := Fund{Gesucht: now}
	if album {
		var out struct {
			Groups []struct {
				ID       string `json:"id"`
				Title    string `json:"title"`
				Type     string `json:"primary-type"`
				First    string `json:"first-release-date"`
				Credit   credit `json:"artist-credit"`
				Releases []struct {
					ID string `json:"id"`
				} `json:"releases"`
			} `json:"release-groups"`
		}
		if err := c.get(ctx, "release-group", url.Values{"query": {"releasegroup:" + quote(title) + " AND artist:" + quote(artist)}, "limit": {"10"}}, &out); err != nil {
			return f, err
		}
		for _, g := range out.Groups {
			if jellyfin.Plain(g.Title) == jellyfin.Plain(title) && g.Credit.is(artist) && len(g.Releases) > 0 {
				f.Album, f.Datum, groupID, artistID, releaseID = g.Title, g.First, g.ID, g.Credit.id(), g.Releases[0].ID
				break
			}
		}
	} else {
		var out struct {
			Recordings []struct {
				Title    string `json:"title"`
				Credit   credit `json:"artist-credit"`
				Releases []struct {
					ID    string `json:"id"`
					Title string `json:"title"`
					Date  string `json:"date"`
					Group struct {
						ID   string `json:"id"`
						Type string `json:"primary-type"`
					} `json:"release-group"`
				} `json:"releases"`
			} `json:"recordings"`
		}
		if err := c.get(ctx, "recording", url.Values{"query": {"recording:" + quote(title) + " AND artist:" + quote(artist)}, "limit": {"10"}}, &out); err != nil {
			return f, err
		}
		type cand struct{ id, title, date, group, kind, artist string }
		var cs []cand
		for _, r := range out.Recordings {
			if jellyfin.Plain(r.Title) != jellyfin.Plain(title) || !r.Credit.is(artist) {
				continue
			}
			for _, rel := range r.Releases {
				cs = append(cs, cand{rel.ID, rel.Title, rel.Date, rel.Group.ID, rel.Group.Type, r.Credit.id()})
			}
		}
		// an album before a single before anything else; the earliest of a kind
		rank := map[string]int{"Album": 0, "EP": 1, "Single": 2}
		sort.SliceStable(cs, func(i, j int) bool {
			ri, ok := rank[cs[i].kind]
			if !ok {
				ri = 3
			}
			rj, ok := rank[cs[j].kind]
			if !ok {
				rj = 3
			}
			if ri != rj {
				return ri < rj
			}
			return cs[i].date < cs[j].date
		})
		if len(cs) > 0 {
			f.Album, f.Datum, releaseID, groupID, artistID = cs[0].title, cs[0].date, cs[0].id, cs[0].group, cs[0].artist
		}
	}
	if releaseID == "" {
		return f, ErrNichts
	}
	f.Gefunden = true
	f.Kommt = f.Datum != "" && f.Datum > now.Format("2006-01-02")
	seen := map[string]bool{}
	add := func(rels relations, owner string) {
		for _, r := range rels {
			u := r.URL.Resource
			if seen[u] {
				continue
			}
			if l, ok := classify(r.Type, u, owner); ok {
				seen[u] = true
				f.Links = append(f.Links, l)
			}
		}
	}
	var rel struct {
		Labels []struct {
			Label *struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"label"`
		} `json:"label-info"`
		Relations relations `json:"relations"`
	}
	if err := c.get(ctx, "release/"+releaseID, url.Values{"inc": {"labels url-rels"}}, &rel); err != nil {
		return f, err
	}
	add(rel.Relations, "")
	labelID := ""
	for _, l := range rel.Labels {
		if l.Label != nil && l.Label.Name != "" && !strings.EqualFold(l.Label.Name, "[no label]") {
			f.Label, labelID = l.Label.Name, l.Label.ID
			break
		}
	}
	var group struct {
		Relations relations `json:"relations"`
	}
	if groupID != "" && c.get(ctx, "release-group/"+groupID, url.Values{"inc": {"url-rels"}}, &group) == nil {
		add(group.Relations, "")
	}
	var band struct {
		Relations relations `json:"relations"`
	}
	if artistID != "" && c.get(ctx, "artist/"+artistID, url.Values{"inc": {"url-rels"}}, &band) == nil {
		add(band.Relations, "Band")
	}
	var label struct {
		Relations relations `json:"relations"`
	}
	if labelID != "" && c.get(ctx, "label/"+labelID, url.Values{"inc": {"url-rels"}}, &label) == nil {
		add(label.Relations, "Label "+f.Label)
	}
	f.Links = tidy(f.Links)
	return f, nil
}

// classify is a MusicBrainz link as a place to get it; links that are no
// place to buy (databases, social networks, streaming) are left out.
func classify(kind, u, owner string) (Link, bool) {
	p, err := url.Parse(u)
	if err != nil || (p.Scheme != "https" && p.Scheme != "http") {
		return Link{}, false
	}
	host := strings.TrimPrefix(strings.ToLower(p.Hostname()), "www.")
	wo := shop(host)
	if owner != "" && (wo == host) {
		wo = owner
	}
	switch kind {
	case "bandcamp":
		if owner != "" {
			return Link{"Bandcamp (" + owner + ")", "Platte und Download, meist sofort", u}, true
		}
		return Link{"Bandcamp", "Platte und Download, meist sofort", u}, true
	case "purchase for download":
		if wo == owner || wo == host { // a shop Haus does not know: rather none than a stranger
			return Link{}, false
		}
		return Link{wo, "Download", u}, true
	case "purchase for mail-order":
		return Link{wo, "Versand (Platte, CD)", u}, true
	case "official homepage", "official site":
		if owner == "" {
			return Link{}, false
		}
		return Link{owner, "Seite, oft mit Shop", u}, true
	}
	return Link{}, false
}

// shop is a shop's name for its host.
func shop(host string) string {
	for h, n := range map[string]string{"bandcamp.com": "Bandcamp", "music.apple.com": "Apple Music", "itunes.apple.com": "Apple Music",
		"amazon.de": "Amazon", "amazon.com": "Amazon", "qobuz.com": "Qobuz", "7digital.com": "7digital", "beatport.com": "Beatport",
		"jpc.de": "JPC", "hhv.de": "HHV", "roughtrade.com": "Rough Trade", "discogs.com": "Discogs"} {
		if host == h || strings.HasSuffix(host, "."+h) {
			return n
		}
	}
	return host
}

func order(l Link) int {
	switch {
	case strings.HasPrefix(l.Wo, "Bandcamp"):
		return 0
	case l.Was == "Versand (Platte, CD)":
		return 1
	case strings.HasPrefix(l.Wo, "Label"), l.Wo == "Band":
		return 2
	}
	return 3
}

// maxLinks is how many places a wish shows.
const maxLinks = 6

// tidy is the links in their order — Bandcamp, mail order, band and label,
// shops — each place once (http and https of one page are one), at most
// maxLinks.
func tidy(ls []Link) []Link {
	sort.SliceStable(ls, func(i, j int) bool { return order(ls[i]) < order(ls[j]) })
	seen := map[string]bool{}
	var out []Link
	for _, l := range ls {
		key := l.Wo + "|" + l.Was                                   // a shop: once per kind
		if l.Was != "Download" && l.Was != "Versand (Platte, CD)" { // a page: once per address
			key = strings.TrimRight(strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(l.URL, "https://"), "http://"), "www."), "/")
		}
		if seen[key] || len(out) == maxLinks {
			continue
		}
		seen[key] = true
		out = append(out, l)
	}
	return out
}
