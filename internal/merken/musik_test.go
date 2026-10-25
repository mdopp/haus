package merken

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// owner 2026-10-25: a shared song is read as a song — YouTube by its
// oEmbed, Spotify by its page — not as a page with YouTube's footer.
func TestLied(t *testing.T) {
	videos := map[string][2]string{ // v= → title, channel
		"wanda":  {"Wanda - Salbei (Official Video)", "wandamusikVEVO"},
		"topic":  {"Salbei", "Wanda - Topic"},
		"lyrics": {"Bilderbuch – Bungalow [Lyric Video]", "Bilderbuch"},
		"kochen": {"Lasagne wie beim Italiener - so geht's", "Chefkoch"},
		"vevo":   {"Bologna", "WandaVEVO"},
	}
	yt := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := r.URL.Query().Get("url")
		for k, v := range videos {
			if len(u) > len(k) && u[len(u)-len(k):] == k {
				json.NewEncoder(w).Encode(map[string]string{"title": v[0], "author_name": v[1]})
				return
			}
		}
		http.NotFound(w, r)
	}))
	defer yt.Close()
	was := YouTubeBase
	YouTubeBase = yt.URL
	defer func() { YouTubeBase = was }()
	f := Fetcher{AllowPrivate: true}
	for link, want := range map[string]string{
		"https://youtube.com/watch?v=wanda":      "Wanda – Salbei",
		"https://www.youtube.com/watch?v=topic":  "Wanda – Salbei",
		"https://youtu.be/lyrics":                "Bilderbuch – Bungalow",
		"https://music.youtube.com/watch?v=vevo": "Wanda – Bologna",
	} {
		l, err := f.Lied(context.Background(), link)
		if err != nil || l.Name() != want || l.Quelle != "YouTube" || l.Link != link {
			t.Errorf("%s → %+v %v, want %s", link, l, err, want)
		}
	}
	if _, err := f.Lied(context.Background(), "https://youtube.com/watch?v=kochen"); !errors.Is(err, ErrKeinLied) {
		t.Errorf("a cooking video is no song: %v", err)
	}
	if _, err := f.Lied(context.Background(), "https://www.chefkoch.de/rezepte/1"); !errors.Is(err, ErrKeinLied) {
		t.Errorf("a page is no song: %v", err)
	}

	page := `<html><head><title>Never Gonna Give You Up - Song, Lyrics, and Music Video by Rick Astley | Spotify</title>` +
		`<meta property="og:title" content="Never Gonna Give You Up"><meta property="og:description" content="Rick Astley · Whenever You Need Somebody · Song · 1987"></head></html>`
	l, err := spotifyLied("https://open.spotify.com/track/x", page, "https://open.spotify.com/track/x", false)
	if err != nil || l.Name() != "Rick Astley – Never Gonna Give You Up" || l.Album {
		t.Errorf("spotify: %+v %v", l, err)
	}
}
