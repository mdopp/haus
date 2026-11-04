// Package jellyfin is what Homes asks of Jellyfin (laib#695): whether a song
// or an album is in the household's own music already — for the music wish
// list (a shared song that is there is "hast du schon"; a wish that has
// arrived is ticked off), and the albums last added with their covers (Musik).
// Reading only, with one API key as Laib hands it.
package jellyfin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mdopp/homes/internal/namen"
)

// Client is Jellyfin behind its API key.
type Client struct {
	URL   string // http://host.containers.internal:8096
	Token string
	HTTP  *http.Client
}

// FromEnv is Jellyfin from HOMES_JELLYFIN_URL and HOMES_JELLYFIN_TOKEN, or nil
// without both.
func FromEnv() *Client {
	u, t := strings.TrimRight(strings.TrimSpace(os.Getenv("HOMES_JELLYFIN_URL")), "/"), strings.TrimSpace(os.Getenv("HOMES_JELLYFIN_TOKEN"))
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
	q := url.Values{"searchTerm": {title}, "IncludeItemTypes": {"Audio,MusicAlbum"}, "Recursive": {"true"},
		"Fields": {"AlbumArtist,Artists,Album"}, "Limit": {"50"}}
	var out struct {
		Items []struct {
			Name        string   `json:"Name"`
			Type        string   `json:"Type"`
			Album       string   `json:"Album"`
			AlbumArtist string   `json:"AlbumArtist"`
			Artists     []string `json:"Artists"`
		} `json:"Items"`
	}
	if err := c.get(ctx, "/Items", q, &out); err != nil {
		return Treffer{}, false, err
	}
	want, by := namen.Plain(title), namen.Plain(artist)
	for _, it := range out.Items {
		if namen.Plain(it.Name) != want {
			continue
		}
		artists := append([]string{it.AlbumArtist}, it.Artists...)
		ok := by == ""
		for _, a := range artists {
			ok = ok || (namen.Plain(a) != "" && (namen.Plain(a) == by || strings.Contains(namen.Plain(a), by) || strings.Contains(by, namen.Plain(a))))
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

// Album is one album in the music: Jellyfin's id, its name, its artist.
type Album struct {
	ID, Name, Kuenstler string
	Bild                bool // it has a cover
}

// Neueste are the albums last added to the music, newest first, at most
// n, and how many albums the music holds.
func (c *Client) Neueste(ctx context.Context, n int) ([]Album, int, error) {
	q := url.Values{"IncludeItemTypes": {"MusicAlbum"}, "Recursive": {"true"}, "SortBy": {"DateCreated,SortName"}, "SortOrder": {"Descending"},
		"Limit": {strconv.Itoa(n)}, "Fields": {"AlbumArtist"}, "EnableTotalRecordCount": {"true"}}
	var out struct {
		Items []struct {
			ID          string            `json:"Id"`
			Name        string            `json:"Name"`
			AlbumArtist string            `json:"AlbumArtist"`
			ImageTags   map[string]string `json:"ImageTags"`
		} `json:"Items"`
		Total int `json:"TotalRecordCount"`
	}
	if err := c.get(ctx, "/Items", q, &out); err != nil {
		return nil, 0, err
	}
	albums := []Album{}
	for _, it := range out.Items {
		albums = append(albums, Album{ID: it.ID, Name: it.Name, Kuenstler: it.AlbumArtist, Bild: it.ImageTags["Primary"] != ""})
	}
	return albums, out.Total, nil
}

// Bild is an album's cover, small (Homes shows it in its own page): its
// bytes and type. id is Jellyfin's, 32 hex digits; anything else is no cover.
func (c *Client) Bild(ctx context.Context, id string) ([]byte, string, error) {
	if len(id) != 32 || strings.Trim(strings.ToLower(id), "0123456789abcdef") != "" {
		return nil, "", fmt.Errorf("jellyfin: no such item %q", id)
	}
	raw, ctype, err := c.fetch(ctx, "/Items/"+id+"/Images/Primary", url.Values{"fillWidth": {"240"}, "fillHeight": {"240"}, "quality": {"85"}})
	if err == nil && !strings.HasPrefix(ctype, "image/") {
		err = fmt.Errorf("jellyfin: a cover of type %q", ctype)
	}
	return raw, ctype, err
}

// get asks Jellyfin and reads its JSON answer.
func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	raw, _, err := c.fetch(ctx, path, q)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("jellyfin: %w", err)
	}
	return nil
}

// fetch asks Jellyfin, with the client's token: the answer's bytes and type.
func (c *Client) fetch(ctx context.Context, path string, q url.Values) ([]byte, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL+path+"?"+q.Encode(), nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", `MediaBrowser Token="`+c.Token+`", Client="Homes"`)
	h := c.HTTP
	if h == nil {
		h = http.DefaultClient
	}
	res, err := h.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if res.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("jellyfin %d: %s", res.StatusCode, strings.TrimSpace(string(raw[:min(len(raw), 200)])))
	}
	return raw, res.Header.Get("Content-Type"), nil
}
