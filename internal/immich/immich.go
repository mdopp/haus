// Package immich is Homes' way into Immich, the household's photos
// (photos.<domain>, laib's immich template): the people its face
// recognition knows by name, each one's face, and a person per contact
// (owner 2026-11-06). Immich keeps people per user, so Homes works with an
// API key of the owner's account (HOMES_IMMICH_KEY: person.read,
// person.create, person.update, user.read). The API as Immich 3.3.5
// serves it (server/src/controllers/person.controller.ts,
// search.controller.ts, user.controller.ts).
package immich

import (
	"bytes"
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
	// Geburtstag is Immich's birthDate, JJJJ-MM-TT; "" none
	Geburtstag string `json:"birthDate,omitempty"`
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

// Wessen is the e-mail of the Immich account the key belongs to.
func (c *Client) Wessen(ctx context.Context) (string, error) {
	var answer struct{ Email string }
	err := c.getJSON(ctx, "/api/users/me", &answer)
	return answer.Email, err
}

// Suchen are the people Immich finds for name, hidden and faceless ones
// too (GET /api/search/person; Immich's trigram match: compare the names).
func (c *Client) Suchen(ctx context.Context, name string) ([]Person, error) {
	var out []Person
	err := c.getJSON(ctx, "/api/search/person?"+url.Values{"name": {name}, "withHidden": {"true"}}.Encode(), &out)
	return out, err
}

// Anlegen makes a person with no face yet (POST /api/people): Immich's
// grid shows it only once a face is merged into it.
func (c *Client) Anlegen(ctx context.Context, name, geburtstag string) (Person, error) {
	var p Person
	body, err := c.send(ctx, http.MethodPost, "/api/people", felder(name, geburtstag))
	if err == nil {
		err = json.Unmarshal(body, &p)
	}
	return p, err
}

// Aendern sets a person's name and birthday (PUT /api/people/{id}; "" is
// none).
func (c *Client) Aendern(ctx context.Context, id, name, geburtstag string) error {
	_, err := c.send(ctx, http.MethodPut, "/api/people/"+url.PathEscape(id), felder(name, geburtstag))
	return err
}

func felder(name, geburtstag string) []byte {
	f := map[string]any{"name": name, "birthDate": nil}
	if geburtstag != "" {
		f["birthDate"] = geburtstag
	}
	b, _ := json.Marshal(f)
	return b
}

// FehltRecht is Immich refusing for a permission the key lacks (its auth
// guard: 403 „Missing required permission: <recht>“).
type FehltRecht struct{ Recht string }

func (e FehltRecht) Error() string { return "immich: the key lacks " + e.Recht }

// Antwort is Immich answering other than 200 or 201.
type Antwort struct {
	Pfad string
	Code int
}

func (a Antwort) Error() string { return fmt.Sprintf("immich: %s answered %d", a.Pfad, a.Code) }

func (c *Client) getJSON(ctx context.Context, path string, v any) error {
	body, err := c.get(ctx, path)
	if err == nil {
		err = json.Unmarshal(body, v)
	}
	return err
}

// maxAntwort bounds an answer: a page of people, or one face.
const maxAntwort = 8 << 20

func (c *Client) get(ctx context.Context, path string) ([]byte, error) {
	return c.send(ctx, http.MethodGet, path, nil)
}

func (c *Client) send(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.URL, "/")+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", c.Key)
	req.Header.Set("Accept", "application/json, image/jpeg")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 20 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("immich: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		var answer struct{ Message string }
		json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&answer)
		if recht, ok := strings.CutPrefix(answer.Message, "Missing required permission: "); ok {
			return nil, FehltRecht{recht}
		}
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, Antwort{strings.SplitN(path, "?", 2)[0], resp.StatusCode}
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxAntwort))
}
