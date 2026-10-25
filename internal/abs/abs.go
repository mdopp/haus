// Package abs is what Haus asks of Audiobookshelf (haus#123): the podcast
// library, a feed's podcast, a subscription that downloads by itself and
// keeps only the last few episodes, the newest episodes, and an end to a
// subscription. One API key (a user Laib makes for Haus, laib#676), as a
// bearer; ABS's routes as its server code has them (ApiRouter.js,
// PodcastController.js).
package abs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
	"time"
)

// Client is Audiobookshelf behind its API key.
type Client struct {
	URL   string // http://host.containers.internal:13378
	Token string
	HTTP  *http.Client
}

// FromEnv is ABS from HAUS_ABS_URL and HAUS_ABS_TOKEN, or nil without both.
func FromEnv() *Client {
	u, t := strings.TrimRight(strings.TrimSpace(os.Getenv("HAUS_ABS_URL")), "/"), strings.TrimSpace(os.Getenv("HAUS_ABS_TOKEN"))
	if u == "" || t == "" {
		return nil
	}
	return &Client{URL: u, Token: t}
}

// Keep is how many episodes of a podcast the box keeps (owner, 2026-10-25:
// "immer nur die letzten 3").
const Keep = 3

// ErrNoLibrary is a box whose ABS has no podcast library.
var ErrNoLibrary = errors.New("In Audiobookshelf gibt es keine Podcast-Bibliothek.")

// ErrNoFeed is a feed ABS cannot read.
var ErrNoFeed = errors.New("Unter dieser Adresse ist kein Podcast-Feed.")

// Error is ABS saying no.
type Error struct {
	Status int
	Detail string
}

func (e *Error) Error() string { return fmt.Sprintf("audiobookshelf %d: %s", e.Status, e.Detail) }

func (c *Client) do(ctx context.Context, method, p string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, c.URL+p, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	h := c.HTTP
	if h == nil {
		h = http.DefaultClient
	}
	res, err := h.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if res.StatusCode >= 300 {
		return &Error{res.StatusCode, strings.TrimSpace(string(raw[:min(len(raw), 200)]))}
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// Library is the podcast library and the folder new podcasts go to.
type Library struct {
	ID, FolderID, FolderPath string
}

// PodcastLibrary is the first library of mediaType podcast, and its first
// folder.
func (c *Client) PodcastLibrary(ctx context.Context) (Library, error) {
	var out struct {
		Libraries []struct {
			ID        string `json:"id"`
			MediaType string `json:"mediaType"`
			Folders   []struct {
				ID       string `json:"id"`
				FullPath string `json:"fullPath"`
			} `json:"folders"`
		} `json:"libraries"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/libraries", nil, &out); err != nil {
		return Library{}, err
	}
	for _, l := range out.Libraries {
		if l.MediaType == "podcast" && len(l.Folders) > 0 {
			return Library{ID: l.ID, FolderID: l.Folders[0].ID, FolderPath: l.Folders[0].FullPath}, nil
		}
	}
	return Library{}, ErrNoLibrary
}

// Podcast is a feed's podcast, as ABS reads it.
type Podcast struct {
	Title       string `json:"title"`
	Author      string `json:"author"`
	Description string `json:"description"`
	ImageURL    string `json:"imageUrl"`
	FeedURL     string `json:"feedUrl"`
	Language    string `json:"language"`
}

// Feed asks ABS to read a feed: its podcast, or ErrNoFeed.
func (c *Client) Feed(ctx context.Context, feedURL string) (Podcast, error) {
	var out struct {
		Podcast struct {
			Metadata Podcast `json:"metadata"`
		} `json:"podcast"`
	}
	err := c.do(ctx, http.MethodPost, "/api/podcasts/feed", map[string]string{"rssFeed": feedURL}, &out)
	var ae *Error
	if errors.As(err, &ae) && ae.Status < 500 {
		return Podcast{}, ErrNoFeed
	}
	if err != nil {
		return Podcast{}, err
	}
	p := out.Podcast.Metadata
	if p.FeedURL == "" {
		p.FeedURL = feedURL
	}
	if p.Title == "" {
		return Podcast{}, ErrNoFeed
	}
	return p, nil
}

// Item is a podcast in the library.
type Item struct {
	ID      string
	Title   string
	FeedURL string
}

// Podcasts are the library's podcasts.
func (c *Client) Podcasts(ctx context.Context, lib Library) ([]Item, error) {
	var out struct {
		Results []struct {
			ID    string `json:"id"`
			Media struct {
				Metadata struct {
					Title   string `json:"title"`
					FeedURL string `json:"feedUrl"`
				} `json:"metadata"`
			} `json:"media"`
		} `json:"results"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/libraries/"+lib.ID+"/items?limit=500", nil, &out); err != nil {
		return nil, err
	}
	var items []Item
	for _, r := range out.Results {
		items = append(items, Item{ID: r.ID, Title: r.Media.Metadata.Title, FeedURL: r.Media.Metadata.FeedURL})
	}
	return items, nil
}

// folderName is a podcast's folder: its title without what a path cannot hold.
func folderName(title string) string {
	r := strings.NewReplacer("/", "-", "\\", "-", ":", " -", "*", "", "?", "", "\"", "", "<", "", ">", "", "|", "-", "\x00", "")
	n := strings.Join(strings.Fields(r.Replace(title)), " ")
	n = strings.Trim(n, ". ")
	if n == "" {
		n = "Podcast"
	}
	if r := []rune(n); len(r) > 120 {
		n = string(r[:120])
	}
	return n
}

// Subscribe adds the podcast to the library: it downloads new episodes by
// itself and keeps only the last Keep. It answers the item's id.
func (c *Client) Subscribe(ctx context.Context, lib Library, p Podcast) (string, error) {
	var out struct {
		ID string `json:"id"`
	}
	in := map[string]any{
		"libraryId": lib.ID, "folderId": lib.FolderID, "path": path.Join(lib.FolderPath, folderName(p.Title)),
		"media": map[string]any{
			"metadata":             map[string]any{"title": p.Title, "author": p.Author, "description": p.Description, "feedUrl": p.FeedURL, "imageUrl": p.ImageURL, "language": p.Language},
			"autoDownloadEpisodes": true,
		},
	}
	if err := c.do(ctx, http.MethodPost, "/api/podcasts", in, &out); err != nil {
		return "", err
	}
	if err := c.KeepLast(ctx, out.ID); err != nil {
		return out.ID, err
	}
	return out.ID, nil
}

// KeepLast sets a podcast to keep only the last Keep episodes and to fetch
// at most Keep new ones at a time.
func (c *Client) KeepLast(ctx context.Context, itemID string) error {
	return c.do(ctx, http.MethodPatch, "/api/items/"+itemID+"/media", map[string]any{"maxEpisodesToKeep": Keep, "maxNewEpisodesToDownload": Keep}, nil)
}

// Remove takes a podcast out of the library, its files too.
func (c *Client) Remove(ctx context.Context, itemID string) error {
	return c.do(ctx, http.MethodDelete, "/api/items/"+itemID+"?hard=1", nil, nil)
}

// Episode is one downloaded episode.
type Episode struct {
	ID, ItemID, Podcast, Title, Description string
	Published                               time.Time
}

// Recent is the library's newest episodes, newest first.
func (c *Client) Recent(ctx context.Context, lib Library, n int) ([]Episode, error) {
	var out struct {
		Episodes []struct {
			ID            string `json:"id"`
			LibraryItemID string `json:"libraryItemId"`
			Title         string `json:"title"`
			Description   string `json:"description"`
			PublishedAt   int64  `json:"publishedAt"`
			Podcast       struct {
				Metadata struct {
					Title string `json:"title"`
				} `json:"metadata"`
			} `json:"podcast"`
		} `json:"episodes"`
	}
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/libraries/%s/recent-episodes?limit=%d", lib.ID, n), nil, &out); err != nil {
		return nil, err
	}
	var eps []Episode
	for _, e := range out.Episodes {
		eps = append(eps, Episode{ID: e.ID, ItemID: e.LibraryItemID, Podcast: e.Podcast.Metadata.Title, Title: e.Title,
			Description: e.Description, Published: time.UnixMilli(e.PublishedAt)})
	}
	return eps, nil
}
