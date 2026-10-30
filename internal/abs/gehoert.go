package abs

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// What a person listened to (haus#152): ABS keeps each user's progress per
// book; Haus's user is an administrator (laib#676), so it may read every
// family member's — GET /api/users/:id, as UserController.findOne answers
// it, with each progress's displayTitle. Reading only.

// ErrNoUser is a person without an account in Audiobookshelf (they never
// signed in there).
var ErrNoUser = errors.New("kein Konto in Audiobookshelf")

// Gehoert is one book a person listened to: its title, how far (0–1),
// whether it is finished and when it was last heard.
type Gehoert struct {
	ItemID, Titel string
	Fortschritt   float64
	Fertig        bool
	Zuletzt       time.Time
}

// userID is the ABS user whose name is the person's (case aside).
func (c *Client) userID(ctx context.Context, person string) (string, error) {
	var out struct {
		Users []struct {
			ID       string `json:"id"`
			Username string `json:"username"`
		} `json:"users"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/users", nil, &out); err != nil {
		return "", err
	}
	for _, u := range out.Users {
		if person != "" && strings.EqualFold(u.Username, person) {
			return u.ID, nil
		}
	}
	return "", ErrNoUser
}

// Gehoerte are the books the person started or finished, last heard
// first, at most n; podcast episodes are left out (Podcasts has them).
func (c *Client) Gehoerte(ctx context.Context, person string, n int) ([]Gehoert, error) {
	id, err := c.userID(ctx, person)
	if err != nil {
		return nil, err
	}
	var out struct {
		MediaProgress []struct {
			LibraryItemID string  `json:"libraryItemId"`
			EpisodeID     string  `json:"episodeId"`
			MediaItemType string  `json:"mediaItemType"`
			DisplayTitle  string  `json:"displayTitle"`
			Progress      float64 `json:"progress"`
			IsFinished    bool    `json:"isFinished"`
			HideFromCont  bool    `json:"hideFromContinueListening"`
			LastUpdate    int64   `json:"lastUpdate"`
			FinishedAt    int64   `json:"finishedAt"`
		} `json:"mediaProgress"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/users/"+url.PathEscape(id), nil, &out); err != nil {
		return nil, err
	}
	var res []Gehoert
	for _, p := range out.MediaProgress {
		if p.EpisodeID != "" || (p.MediaItemType != "" && p.MediaItemType != "book") || p.DisplayTitle == "" || (p.Progress <= 0 && !p.IsFinished) {
			continue
		}
		at := p.LastUpdate
		if p.FinishedAt > at {
			at = p.FinishedAt
		}
		res = append(res, Gehoert{ItemID: p.LibraryItemID, Titel: p.DisplayTitle, Fortschritt: p.Progress, Fertig: p.IsFinished, Zuletzt: time.UnixMilli(at)})
	}
	sort.SliceStable(res, func(i, j int) bool { return res[i].Zuletzt.After(res[j].Zuletzt) })
	if len(res) > n {
		res = res[:n]
	}
	return res, nil
}

// HatBuch is the title of a book in the box's book libraries whose title is
// the one asked for (compared plain), if there is one.
func (c *Client) HatBuch(ctx context.Context, title string, plain func(string) string) (string, bool, error) {
	title = strings.Join(strings.Fields(title), " ")
	var libs struct {
		Libraries []struct {
			ID        string `json:"id"`
			MediaType string `json:"mediaType"`
		} `json:"libraries"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/libraries", nil, &libs); err != nil {
		return "", false, err
	}
	for _, l := range libs.Libraries {
		if l.MediaType != "book" {
			continue
		}
		var out struct {
			Book []struct {
				LibraryItem struct {
					Media struct {
						Metadata struct {
							Title string `json:"title"`
						} `json:"metadata"`
					} `json:"media"`
				} `json:"libraryItem"`
			} `json:"book"`
		}
		if err := c.do(ctx, http.MethodGet, "/api/libraries/"+url.PathEscape(l.ID)+"/search?limit=10&q="+url.QueryEscape(title), nil, &out); err != nil {
			return "", false, err
		}
		for _, b := range out.Book {
			if t := b.LibraryItem.Media.Metadata.Title; plain(t) == plain(title) {
				return t, true, nil
			}
		}
	}
	return "", false, nil
}
