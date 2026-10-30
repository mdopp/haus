package jellyfin

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// What a person watched (haus#152): Jellyfin keeps each user's played
// state; reading another user's needs an administrator, so this is a
// second client, with the API key Laib's media template mints for Haus
// (HAUS_JELLYFIN_KEY), not the music-only token of Haus's own user.
// Reading only.

// KeyFromEnv is Jellyfin from HAUS_JELLYFIN_URL and HAUS_JELLYFIN_KEY, or
// nil without both.
func KeyFromEnv() *Client {
	u, k := strings.TrimRight(strings.TrimSpace(os.Getenv("HAUS_JELLYFIN_URL")), "/"), strings.TrimSpace(os.Getenv("HAUS_JELLYFIN_KEY"))
	if u == "" || k == "" {
		return nil
	}
	return &Client{URL: u, Token: k}
}

// ErrNoUser is a person without an account in Jellyfin (they never
// signed in there).
var ErrNoUser = errors.New("kein Konto in Jellyfin")

// Gesehen is one film or series a person watched: its name, the episode's
// for a series ("S2E3 · Name"), the year, whether it is played to the end
// and when it was last played.
type Gesehen struct {
	ID, Name, Folge string
	Serie           bool
	Jahr            int
	Fertig          bool
	Zuletzt         time.Time
}

// userID is the Jellyfin user whose name is the person's (case aside):
// the family's accounts come from LLDAP, with the same name (laib#256).
func (c *Client) userID(ctx context.Context, person string) (string, error) {
	var users []struct {
		Name string `json:"Name"`
		ID   string `json:"Id"`
	}
	if err := c.get(ctx, "/Users", url.Values{}, &users); err != nil {
		return "", err
	}
	for _, u := range users {
		if person != "" && strings.EqualFold(u.Name, person) {
			return u.ID, nil
		}
	}
	return "", ErrNoUser
}

// Geschaut are the films and series the person played, last played
// first, at most n; a series once, with its last episode.
func (c *Client) Geschaut(ctx context.Context, person string, n int) ([]Gesehen, error) {
	id, err := c.userID(ctx, person)
	if err != nil {
		return nil, err
	}
	q := url.Values{"userId": {id}, "IncludeItemTypes": {"Movie,Episode"}, "Recursive": {"true"}, "Filters": {"IsPlayed"},
		"SortBy": {"DatePlayed"}, "SortOrder": {"Descending"}, "Limit": {"200"}, "Fields": {"ProductionYear"}, "EnableImages": {"false"}}
	var out struct {
		Items []struct {
			ID                string `json:"Id"`
			Name              string `json:"Name"`
			Type              string `json:"Type"`
			SeriesID          string `json:"SeriesId"`
			SeriesName        string `json:"SeriesName"`
			ParentIndexNumber int    `json:"ParentIndexNumber"`
			IndexNumber       int    `json:"IndexNumber"`
			ProductionYear    int    `json:"ProductionYear"`
			UserData          struct {
				Played         bool      `json:"Played"`
				LastPlayedDate time.Time `json:"LastPlayedDate"`
			} `json:"UserData"`
		} `json:"Items"`
	}
	if err := c.get(ctx, "/Items", q, &out); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var res []Gesehen
	for _, it := range out.Items {
		g := Gesehen{ID: it.ID, Name: it.Name, Jahr: it.ProductionYear, Fertig: it.UserData.Played, Zuletzt: it.UserData.LastPlayedDate}
		if it.Type == "Episode" {
			key := it.SeriesID
			if key == "" {
				key = it.SeriesName
			}
			if seen[key] || it.SeriesName == "" {
				continue
			}
			seen[key] = true
			g.ID, g.Name, g.Serie, g.Jahr = key, it.SeriesName, true, 0
			g.Folge = it.Name
			if it.ParentIndexNumber > 0 && it.IndexNumber > 0 {
				g.Folge = "S" + strconv.Itoa(it.ParentIndexNumber) + "E" + strconv.Itoa(it.IndexNumber) + " · " + it.Name
			}
		}
		res = append(res, g)
		if len(res) == n {
			break
		}
	}
	return res, nil
}

// HatFilm is the name of a film or series in Jellyfin whose name is the
// one asked for (compared plain), if there is one.
func (c *Client) HatFilm(ctx context.Context, name string) (string, bool, error) {
	q := url.Values{"searchTerm": {name}, "IncludeItemTypes": {"Movie,Series"}, "Recursive": {"true"}, "Limit": {"20"}, "EnableImages": {"false"}}
	var out struct {
		Items []struct {
			Name string `json:"Name"`
		} `json:"Items"`
	}
	if err := c.get(ctx, "/Items", q, &out); err != nil {
		return "", false, err
	}
	for _, it := range out.Items {
		if Plain(it.Name) == Plain(name) {
			return it.Name, true, nil
		}
	}
	return "", false, nil
}
