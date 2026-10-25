// Package jellyfin is what Haus asks of Jellyfin (laib#695): whether a song
// or an album is in the household's own music already — for the music wish
// list (a shared song that is there is "hast du schon"; a wish that has
// arrived is ticked off). Reading only, with one API key as Laib hands it.
package jellyfin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode"
)

// Client is Jellyfin behind its API key.
type Client struct {
	URL   string // http://host.containers.internal:8096
	Token string
	HTTP  *http.Client
}

// FromEnv is Jellyfin from HAUS_JELLYFIN_URL and HAUS_JELLYFIN_TOKEN, or nil
// without both.
func FromEnv() *Client {
	u, t := strings.TrimRight(strings.TrimSpace(os.Getenv("HAUS_JELLYFIN_URL")), "/"), strings.TrimSpace(os.Getenv("HAUS_JELLYFIN_TOKEN"))
	if u == "" || t == "" {
		return nil
	}
	return &Client{URL: u, Token: t}
}

// Treffer is where the music has it: the album, and the song when a song
// was asked for.
type Treffer struct {
	Album, Titel string
}

// Find looks for the song (or album) title by artist in the music: a
// song or album whose name is the title and whose artist is the artist,
// both compared plain (case, accents, punctuation aside). An empty artist
// matches any.
func (c *Client) Find(ctx context.Context, artist, title string) (Treffer, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	q := url.Values{"searchTerm": {title}, "IncludeItemTypes": {"Audio,MusicAlbum"}, "Recursive": {"true"},
		"Fields": {"AlbumArtist,Artists,Album"}, "Limit": {"50"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL+"/Items?"+q.Encode(), nil)
	if err != nil {
		return Treffer{}, false, err
	}
	req.Header.Set("Authorization", `MediaBrowser Token="`+c.Token+`", Client="Haus"`)
	h := c.HTTP
	if h == nil {
		h = http.DefaultClient
	}
	res, err := h.Do(req)
	if err != nil {
		return Treffer{}, false, err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if res.StatusCode != http.StatusOK {
		return Treffer{}, false, fmt.Errorf("jellyfin %d: %s", res.StatusCode, strings.TrimSpace(string(raw[:min(len(raw), 200)])))
	}
	var out struct {
		Items []struct {
			Name        string   `json:"Name"`
			Type        string   `json:"Type"`
			Album       string   `json:"Album"`
			AlbumArtist string   `json:"AlbumArtist"`
			Artists     []string `json:"Artists"`
		} `json:"Items"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return Treffer{}, false, fmt.Errorf("jellyfin: %w", err)
	}
	want, by := Plain(title), Plain(artist)
	for _, it := range out.Items {
		if Plain(it.Name) != want {
			continue
		}
		artists := append([]string{it.AlbumArtist}, it.Artists...)
		ok := by == ""
		for _, a := range artists {
			ok = ok || (Plain(a) != "" && (Plain(a) == by || strings.Contains(Plain(a), by) || strings.Contains(by, Plain(a))))
		}
		if !ok {
			continue
		}
		if it.Type == "MusicAlbum" {
			return Treffer{Album: it.Name}, true, nil
		}
		return Treffer{Album: it.Album, Titel: it.Name}, true, nil
	}
	return Treffer{}, false, nil
}

// Plain is a name compared: lower case, letters and digits only, umlauts
// as their base letter.
func Plain(s string) string {
	r := strings.NewReplacer("ä", "a", "ö", "o", "ü", "u", "ß", "ss", "é", "e", "è", "e", "á", "a", "à", "a", "ó", "o", "í", "i", "&", "und")
	s = r.Replace(strings.ToLower(s))
	var b strings.Builder
	for _, c := range s {
		if unicode.IsLetter(c) || unicode.IsDigit(c) {
			b.WriteRune(c)
		}
	}
	return b.String()
}
