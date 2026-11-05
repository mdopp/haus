// Package immich is Homes' way into Immich, the household's photos
// (photos.<domain>, laib's immich template): the people its face
// recognition knows by name, and each one's face. Immich keeps people per
// user, so Homes reads them with an API key of the owner's account
// (HOMES_IMMICH_KEY, permission person.read; owner 2026-11-05). The API as
// Immich 3.3 serves it (server/src/controllers/person.controller.ts).
package immich

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
)

// Person is one of Immich's people.
type Person struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Stand is Immich's updatedAt: it changes with the name or the face
	Stand string `json:"updatedAt,omitempty"`
}

// Client reaches Immich inside the box.
type Client struct {
	URL  string       // HOMES_IMMICH_URL, http://host.containers.internal:2283
	Key  string       // HOMES_IMMICH_KEY: the owner's API key
	HTTP *http.Client // nil: a client with a 20 s timeout
}

// FromEnv is Immich from HOMES_IMMICH_URL and HOMES_IMMICH_KEY; nil
// without either.
func FromEnv() *Client {
	u, k := strings.TrimRight(os.Getenv("HOMES_IMMICH_URL"), "/"), strings.TrimSpace(os.Getenv("HOMES_IMMICH_KEY"))
	if u == "" || k == "" {
		return nil
	}
	return &Client{URL: u, Key: k}
}

// maxSeiten bounds the people read: 500 a page, 10,000 in all.
const maxSeiten = 20

// People are the named people Immich does not hide, every page of them.
func (c *Client) People(ctx context.Context) ([]Person, error) {
	var out []Person
	for page := 1; page <= maxSeiten; page++ {
		var answer struct {
			People []struct {
				Person
				Hidden bool `json:"isHidden"`
			} `json:"people"`
			Next bool `json:"hasNextPage"`
		}
		body, err := c.get(ctx, "/api/people?"+url.Values{"withHidden": {"false"}, "size": {"500"}, "page": {fmt.Sprint(page)}}.Encode())
		if err == nil {
			err = json.Unmarshal(body, &answer)
		}
		if err != nil {
			return nil, err
		}
		for _, p := range answer.People {
			if strings.TrimSpace(p.Name) != "" && !p.Hidden && p.ID != "" {
				out = append(out, p.Person)
			}
		}
		if !answer.Next {
			break
		}
	}
	return out, nil
}

// Gesicht is the person's face as Immich cut it: a JPEG of 250 px.
func (c *Client) Gesicht(ctx context.Context, id string) ([]byte, error) {
	return c.get(ctx, "/api/people/"+url.PathEscape(id)+"/thumbnail")
}

// maxAntwort bounds an answer: a page of people, or one face.
const maxAntwort = 8 << 20

func (c *Client) get(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.URL, "/")+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", c.Key)
	req.Header.Set("Accept", "application/json, image/jpeg")
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 20 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("immich: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("immich: %s answered %d", strings.SplitN(path, "?", 2)[0], resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxAntwort))
}
