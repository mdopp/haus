package merken

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"
)

// Lied is a song or an album a music link names (owner 2026-10-25: a
// shared song goes on the household's music wish list, not into a note).
type Lied struct {
	Kuenstler, Titel string
	Album            bool   // an album, not a song
	Link             string // as shared
	Quelle           string // YouTube, Spotify
}

// Name is the wish as the list says it: "Wanda – Salbei".
func (l Lied) Name() string {
	if l.Kuenstler == "" {
		return l.Titel
	}
	return l.Kuenstler + " – " + l.Titel
}

// YouTubeBase is where YouTube's oEmbed is asked; a variable for the tests.
var YouTubeBase = "https://www.youtube.com"

var (
	// what a music video's title says beside the song: (Official Video),
	// [Official Music Video], (Lyric Video), (Visualizer), (HD) …
	reVideoNoise = regexp.MustCompile(`(?i)\s*[\(\[][^\)\]]*(official|video|audio|lyric|visuali[sz]er|musikvideo|hd|4k|clip|remaster)[^\)\]]*[\)\]]`)
	// a title that is a music video's
	reMusicVideo = regexp.MustCompile(`(?i)official\s+(music\s+)?(video|audio)|lyric\s+video|visuali[sz]er|musikvideo|offizielles\s+video`)
	reDash       = regexp.MustCompile(`\s+[-–—]\s+`)
)

// MusikLink is a link to a song or an album: YouTube Music always, a
// YouTube video when it says it is a music video (only known after asking,
// see Lied), a Spotify track or album.
func MusikLink(u *url.URL) bool {
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	switch {
	case host == "music.youtube.com", host == "youtube.com", host == "m.youtube.com", host == "youtu.be":
		return true
	case host == "open.spotify.com":
		return strings.HasPrefix(u.Path, "/track/") || strings.HasPrefix(u.Path, "/album/") || strings.Contains(u.Path, "/track/") || strings.Contains(u.Path, "/album/")
	}
	return false
}

// ErrKeinLied is a music link that names no song: a YouTube video that
// is not a music video, a page without a title.
var ErrKeinLied = errors.New("Das ist kein Lied.")

// Lied reads the song behind a music link: YouTube through its oEmbed
// (title and channel; "Wanda - Salbei (Official Video)" by wandamusikVEVO
// is Wanda – Salbei), Spotify from its page (og:title the song or album,
// og:description "Artist · Album · Song · Year"). A YouTube video that is
// no music video is ErrKeinLied: it is a page to keep as a note.
func (f Fetcher) Lied(ctx context.Context, link string) (Lied, error) {
	u, err := Address(link)
	if err != nil {
		return Lied{}, err
	}
	if !MusikLink(u) {
		return Lied{}, ErrKeinLied
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	if host == "open.spotify.com" {
		return f.spotify(ctx, u)
	}
	return f.youtube(ctx, u, host == "music.youtube.com")
}

func (f Fetcher) youtube(ctx context.Context, u *url.URL, musicApp bool) (Lied, error) {
	api, err := url.Parse(YouTubeBase + "/oembed?format=json&url=" + url.QueryEscape(u.String()))
	if err != nil {
		return Lied{}, err
	}
	_, _, raw, _, err := f.Get(ctx, api)
	if err != nil {
		return Lied{}, err
	}
	var o struct {
		Title  string `json:"title"`
		Author string `json:"author_name"`
	}
	if json.Unmarshal(raw, &o) != nil || strings.TrimSpace(o.Title) == "" {
		return Lied{}, ErrKeinLied
	}
	channel := strings.TrimSpace(o.Author)
	topic := strings.HasSuffix(channel, " - Topic")
	vevo := strings.HasSuffix(strings.ToLower(channel), "vevo")
	if !musicApp && !topic && !vevo && !reMusicVideo.MatchString(o.Title) {
		return Lied{}, ErrKeinLied
	}
	title := strings.TrimSpace(reVideoNoise.ReplaceAllString(o.Title, ""))
	l := Lied{Link: u.String(), Quelle: "YouTube"}
	if parts := reDash.Split(title, 2); len(parts) == 2 && parts[0] != "" && parts[1] != "" {
		l.Kuenstler, l.Titel = strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	} else {
		l.Titel = title
		artist := strings.TrimSuffix(channel, " - Topic")
		if vevo {
			artist = artist[:len(artist)-4]
		}
		l.Kuenstler = strings.TrimSpace(artist)
	}
	return l, nil
}

func (f Fetcher) spotify(ctx context.Context, u *url.URL) (Lied, error) {
	final, page, err := f.Fetch(ctx, u)
	if err != nil {
		return Lied{}, err
	}
	return spotifyLied(final, page, u.String(), strings.Contains(u.Path, "/album/"))
}

// spotifyLied reads a Spotify page: og:title the song or album,
// og:description "Artist · Album · Song · Year".
func spotifyLied(final, page, link string, album bool) (Lied, error) {
	s := Lesen(final, page)
	meta := map[string]string{}
	for _, m := range reMeta.FindAllString(page, -1) {
		a := attrs(m)
		if k := strings.ToLower(a["property"]); k != "" && a["content"] != "" {
			meta[k] = clean(a["content"])
		}
	}
	title := first(meta["og:title"], s.Titel)
	if title == "" {
		return Lied{}, ErrKeinLied
	}
	l := Lied{Titel: title, Link: link, Quelle: "Spotify", Album: album}
	if parts := strings.Split(meta["og:description"], " · "); len(parts) > 0 && strings.TrimSpace(parts[0]) != "" {
		l.Kuenstler = strings.TrimSpace(parts[0])
	}
	return l, nil
}
