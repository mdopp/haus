package merken

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// tracking are the query parameters that say who shared, not what: a
// link with them is the same link (owner 2026-10-26: the same thing shared
// twice is not kept twice).
var tracking = map[string]bool{"si": true, "is": true, "fbclid": true, "gclid": true, "igshid": true, "ref": true, "ref_src": true,
	"feature": true, "mc_cid": true, "mc_eid": true, "_hsenc": true, "_hsmi": true, "spm": true, "share": true, "context": true, "pp": true, "nd": true}

var reSpotifyIntl = regexp.MustCompile(`^/intl-[a-z]{2}(-[a-z]{2})?/`)

// Schluessel is what a link names, for finding it again: no scheme, no
// www./m., no tracking, no fragment, no trailing slash, the parameters
// sorted; a YouTube video by its id whichever way it was shared.
func Schluessel(link string) string {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil || u.Host == "" {
		return strings.TrimSpace(link)
	}
	host := strings.ToLower(u.Hostname())
	for _, p := range []string{"www.", "m.", "mobile."} {
		host = strings.TrimPrefix(host, p)
	}
	q := u.Query()
	switch {
	case host == "youtu.be":
		return "youtube:" + strings.Trim(u.Path, "/")
	case host == "youtube.com" || host == "music.youtube.com":
		if v := q.Get("v"); v != "" {
			return "youtube:" + v
		}
		if id, ok := strings.CutPrefix(u.Path, "/shorts/"); ok {
			return "youtube:" + strings.Trim(id, "/")
		}
	case host == "open.spotify.com":
		u.Path = reSpotifyIntl.ReplaceAllString(u.Path, "/")
	}
	var keys []string
	for k := range q {
		if tracking[strings.ToLower(k)] || strings.HasPrefix(strings.ToLower(k), "utm_") {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var params []string
	for _, k := range keys {
		for _, v := range q[k] {
			params = append(params, url.QueryEscape(k)+"="+url.QueryEscape(v))
		}
	}
	key := host + strings.TrimRight(u.EscapedPath(), "/")
	if len(params) > 0 {
		key += "?" + strings.Join(params, "&")
	}
	return key
}

// Fingerabdruck is what a page says, as a fingerprint: its recipe, album
// or trip as read, else its title and readable text — the parts a note is
// made from, so a page whose ads or dates changed is the same page.
func (s Seite) Fingerabdruck() string {
	var parts any
	switch {
	case s.Rezept != nil:
		parts = s.Rezept
	case s.Album != nil:
		parts = s.Album
	case s.Reise != nil:
		parts = s.Reise
	default:
		text := s.Text
		if r := []rune(text); len(r) > 4000 {
			text = string(r[:4000])
		}
		parts = []string{s.Titel, text}
	}
	b, _ := json.Marshal(parts)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:6])
}
