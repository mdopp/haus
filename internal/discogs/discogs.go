// Package discogs is the household's records on Discogs (owner
// 2026-10-25): its collection — what is on the shelf — and its wantlist,
// read and written with the account's personal access token (laib#696:
// HAUS_DISCOGS_TOKEN). Haus finds a record by its barcode, its catalogue
// number or its name, adds what the owner confirmed, keeps record wishes on
// the wantlist and sees when one arrived in the collection. Discogs asks
// for at most 60 requests a minute and a User-Agent naming the program.
package discogs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Client is Discogs for one account.
type Client struct {
	Base  string // https://api.discogs.com; the tests' own
	Token string
	HTTP  *http.Client

	mu   sync.Mutex
	user string
}

// FromEnv is Discogs from HAUS_DISCOGS_TOKEN, or nil without it.
func FromEnv() *Client {
	t := strings.TrimSpace(os.Getenv("HAUS_DISCOGS_TOKEN"))
	if t == "" {
		return nil
	}
	return &Client{Base: "https://api.discogs.com", Token: t}
}

const userAgent = "Haus/1.0 +https://github.com/mdopp/haus"

// Error is Discogs saying no.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string { return fmt.Sprintf("discogs %d: %s", e.Status, e.Message) }

func (c *Client) do(ctx context.Context, method, path string, q url.Values, in, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	u := strings.TrimRight(c.Base, "/") + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	var body io.Reader
	if in != nil {
		b, _ := json.Marshal(in)
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Discogs token="+c.Token)
	req.Header.Set("User-Agent", userAgent)
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
		var e struct {
			Message string `json:"message"`
		}
		json.Unmarshal(raw, &e)
		return &Error{res.StatusCode, e.Message}
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// User is the account the token belongs to.
func (c *Client) User(ctx context.Context) (string, error) {
	c.mu.Lock()
	u := c.user
	c.mu.Unlock()
	if u != "" {
		return u, nil
	}
	var id struct {
		Username string `json:"username"`
	}
	if err := c.do(ctx, http.MethodGet, "/oauth/identity", nil, nil, &id); err != nil {
		return "", err
	}
	if id.Username == "" {
		return "", errors.New("discogs: the token names no account")
	}
	c.mu.Lock()
	c.user = id.Username
	c.mu.Unlock()
	return id.Username, nil
}

// Treffer is one release found: the shelf's way of naming a record.
type Treffer struct {
	ID      int      `json:"id"`
	Master  int      `json:"master_id"`
	Title   string   `json:"title"` // "Wanda - Amore"
	Year    string   `json:"year"`
	Country string   `json:"country"`
	Format  []string `json:"format"` // Vinyl, LP, Album, …
	Label   []string `json:"label"`
	Catno   string   `json:"catno"`
	Barcode []string `json:"barcode"`
	Thumb   string   `json:"thumb"`
	Grund   string   `json:"-"` // why it is a hit: "Barcode stimmt", "Katalognummer stimmt"
}

// Kuenstler and Titel are the release's title in its two halves.
func (t Treffer) Kuenstler() string {
	a, _, _ := strings.Cut(t.Title, " - ")
	return artistName(a)
}

// Titel is the record's own name.
func (t Treffer) Titel() string {
	if _, b, ok := strings.Cut(t.Title, " - "); ok {
		return strings.TrimSpace(b)
	}
	return t.Title
}

func (c *Client) search(ctx context.Context, q url.Values) ([]Treffer, error) {
	q.Set("type", "release")
	q.Set("per_page", "10")
	var out struct {
		Results []Treffer `json:"results"`
	}
	if err := c.do(ctx, http.MethodGet, "/database/search", q, nil, &out); err != nil {
		return nil, err
	}
	return out.Results, nil
}

// Hinweise is what the photos or the scan said of a record.
type Hinweise struct {
	Barcode, Katalognummer, Label, Kuenstler, Titel string
}

// digits is a barcode's digits alone.
func digits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// EAN says whether a barcode's check digit is right (EAN-13, EAN-8,
// UPC-A): a misread digit fails it.
func EAN(code string) bool {
	d := digits(code)
	if len(d) != 8 && len(d) != 12 && len(d) != 13 {
		return false
	}
	sum := 0
	for i := len(d) - 2; i >= 0; i-- {
		n := int(d[i] - '0')
		if (len(d)-2-i)%2 == 0 {
			n *= 3
		}
		sum += n
	}
	return (10-sum%10)%10 == int(d[len(d)-1]-'0')
}

// Finden looks for the record: by its barcode first (nearly always the
// one pressing), else its catalogue number and label, else its artist and
// title on vinyl. At most five, each with why.
func (c *Client) Finden(ctx context.Context, h Hinweise) ([]Treffer, error) {
	var out []Treffer
	seen := map[int]bool{}
	take := func(ts []Treffer, why string) {
		for _, t := range ts {
			if !seen[t.ID] && len(out) < 5 {
				seen[t.ID] = true
				t.Grund = why
				out = append(out, t)
			}
		}
	}
	if code := digits(h.Barcode); len(code) >= 8 {
		ts, err := c.search(ctx, url.Values{"barcode": {code}})
		if err != nil {
			return nil, err
		}
		take(ts, "Barcode stimmt")
		if len(out) > 0 {
			return out, nil
		}
	}
	if h.Katalognummer != "" {
		q := url.Values{"catno": {h.Katalognummer}}
		if h.Label != "" {
			q.Set("label", h.Label)
		}
		ts, err := c.search(ctx, q)
		if err != nil {
			return nil, err
		}
		take(ts, "Katalognummer stimmt")
	}
	if h.Kuenstler != "" || h.Titel != "" {
		q := url.Values{"format": {"Vinyl"}}
		if h.Kuenstler != "" {
			q.Set("artist", h.Kuenstler)
		}
		if h.Titel != "" {
			q.Set("release_title", h.Titel)
		}
		ts, err := c.search(ctx, q)
		if err != nil {
			return nil, err
		}
		take(ts, "Künstler und Titel")
	}
	return out, nil
}

// Hinzufuegen puts a release into the collection (its first folder,
// "Uncategorized").
func (c *Client) Hinzufuegen(ctx context.Context, release int) error {
	u, err := c.User(ctx)
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodPost, "/users/"+url.PathEscape(u)+"/collection/folders/1/releases/"+strconv.Itoa(release), nil, nil, nil)
}

// Wuenschen puts a release on the wantlist.
func (c *Client) Wuenschen(ctx context.Context, release int) error {
	u, err := c.User(ctx)
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodPut, "/users/"+url.PathEscape(u)+"/wants/"+strconv.Itoa(release), nil, nil, nil)
}

// Entwuenschen takes a release off the wantlist; one not on it is no error.
func (c *Client) Entwuenschen(ctx context.Context, release int) error {
	u, err := c.User(ctx)
	if err != nil {
		return err
	}
	err = c.do(ctx, http.MethodDelete, "/users/"+url.PathEscape(u)+"/wants/"+strconv.Itoa(release), nil, nil, nil)
	var e *Error
	if errors.As(err, &e) && e.Status == http.StatusNotFound {
		return nil
	}
	return err
}

// Platte is a record on the shelf.
type Platte struct {
	Release, Master int
	Kuenstler       string
	Titel           string
	Jahr            int
}

// Sammlung is the whole collection (every page of folder 0).
func (c *Client) Sammlung(ctx context.Context) ([]Platte, error) {
	u, err := c.User(ctx)
	if err != nil {
		return nil, err
	}
	var all []Platte
	for page := 1; page <= 50; page++ {
		var out struct {
			Pagination struct {
				Pages int `json:"pages"`
			} `json:"pagination"`
			Releases []struct {
				ID    int `json:"id"`
				Basic struct {
					Title   string `json:"title"`
					Year    int    `json:"year"`
					Master  int    `json:"master_id"`
					Artists []struct {
						Name string `json:"name"`
					} `json:"artists"`
				} `json:"basic_information"`
			} `json:"releases"`
		}
		q := url.Values{"page": {strconv.Itoa(page)}, "per_page": {"100"}}
		if err := c.do(ctx, http.MethodGet, "/users/"+url.PathEscape(u)+"/collection/folders/0/releases", q, nil, &out); err != nil {
			return nil, err
		}
		for _, r := range out.Releases {
			p := Platte{Release: r.ID, Master: r.Basic.Master, Titel: r.Basic.Title, Jahr: r.Basic.Year}
			if len(r.Basic.Artists) > 0 {
				p.Kuenstler = artistName(r.Basic.Artists[0].Name)
			}
			all = append(all, p)
		}
		if page >= out.Pagination.Pages {
			break
		}
	}
	return all, nil
}

// artistName is Discogs's name without its number for namesakes: "Wanda
// (2)" is Wanda.
func artistName(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, " ("); i > 0 && strings.HasSuffix(s, ")") {
		if _, err := strconv.Atoi(s[i+2 : len(s)-1]); err == nil {
			return s[:i]
		}
	}
	return s
}

// Album finds an album's main release on vinyl by artist and title (for
// the wantlist): the master's main release, else the first vinyl release.
func (c *Client) Album(ctx context.Context, artist, title string) (Treffer, bool, error) {
	var out struct {
		Results []Treffer `json:"results"`
	}
	q := url.Values{"type": {"master"}, "artist": {artist}, "release_title": {title}, "per_page": {"5"}}
	if err := c.do(ctx, http.MethodGet, "/database/search", q, nil, &out); err != nil {
		return Treffer{}, false, err
	}
	for _, m := range out.Results {
		var master struct {
			Main int `json:"main_release"`
		}
		if err := c.do(ctx, http.MethodGet, "/masters/"+strconv.Itoa(m.ID), nil, nil, &master); err == nil && master.Main != 0 {
			m.Master, m.ID = m.ID, master.Main
			return m, true, nil
		}
	}
	ts, err := c.search(ctx, url.Values{"artist": {artist}, "release_title": {title}, "format": {"Vinyl"}})
	if err != nil || len(ts) == 0 {
		return Treffer{}, false, err
	}
	return ts[0], true, nil
}

// Bild is a release's cover, small: its bytes and type (Haus shows it
// through its own address; a phone never asks Discogs).
func (c *Client) Bild(ctx context.Context, release int) ([]byte, string, error) {
	var r struct {
		Thumb  string `json:"thumb"`
		Images []struct {
			URI150 string `json:"uri150"`
		} `json:"images"`
	}
	if err := c.do(ctx, http.MethodGet, "/releases/"+strconv.Itoa(release), nil, nil, &r); err != nil {
		return nil, "", err
	}
	src := r.Thumb
	if len(r.Images) > 0 && r.Images[0].URI150 != "" {
		src = r.Images[0].URI150
	}
	if !strings.HasPrefix(src, "https://i.discogs.com/") {
		return nil, "", errors.New("discogs: no cover")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	req.Header.Set("User-Agent", userAgent)
	h := c.HTTP
	if h == nil {
		h = http.DefaultClient
	}
	res, err := h.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer res.Body.Close()
	ct := res.Header.Get("Content-Type")
	if res.StatusCode != 200 || !strings.HasPrefix(ct, "image/") {
		return nil, "", errors.New("discogs: no cover")
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	return b, ct, err
}
