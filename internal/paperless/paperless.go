// Package paperless is Haus's read-only client for the Paperless-ngx REST
// API (laib ADR 0017): the family's documents, searched in full text. It
// sends GET requests only, to the configured address only, with the token
// as "Authorization: Token …"; it never follows a redirect, and no error
// carries the token.
package paperless

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Client talks to one Paperless.
type Client struct {
	URL       string // the API's host, e.g. http://host.containers.internal:8000
	Token     string // an API token of a Paperless user; its rights are what Haus sees
	PublicURL string // where people open Paperless; "": no links
	HTTP      *http.Client
}

// Doc is one hit.
type Doc struct {
	ID            int
	Title         string
	Correspondent string    // "": none, or not readable
	Created       time.Time // zero: unknown
	Link          string    // Paperless's own page for it; "" without PublicURL
}

// ErrStatus is an answer the client did not expect.
type ErrStatus struct{ Code int }

func (e *ErrStatus) Error() string {
	switch {
	case e.Code == http.StatusUnauthorized || e.Code == http.StatusForbidden:
		return fmt.Sprintf("Paperless: Anmeldung abgelehnt (HTTP %d)", e.Code)
	case e.Code >= 300 && e.Code < 400:
		return fmt.Sprintf("Paperless leitet um (HTTP %d); dem folgt Haus nicht", e.Code)
	}
	return fmt.Sprintf("Paperless antwortet HTTP %d", e.Code)
}

func noRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

var defaultHTTP = &http.Client{Timeout: 15 * time.Second, CheckRedirect: noRedirect}

const maxBody = 4 << 20

// get fetches one API path into v.
func (c *Client) get(ctx context.Context, p string, q url.Values, v any) error {
	u := strings.TrimRight(c.URL, "/") + p + "?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return errors.New("Paperless: Adresse unbrauchbar")
	}
	req.Header.Set("Authorization", "Token "+c.Token)
	req.Header.Set("Accept", "application/json")
	h := defaultHTTP
	if c.HTTP != nil {
		hc := *c.HTTP
		hc.CheckRedirect = noRedirect
		h = &hc
	}
	resp, err := h.Do(req)
	if err != nil {
		return c.scrub(fmt.Errorf("Paperless nicht erreichbar: %w", err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return &ErrStatus{resp.StatusCode} // the body stays here: it may echo what we sent
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(v); err != nil {
		return c.scrub(fmt.Errorf("Paperless: Antwort unlesbar: %w", err))
	}
	return nil
}

// scrub keeps the token out of a message, whatever put it there.
func (c *Client) scrub(err error) error {
	if c.Token == "" || !strings.Contains(err.Error(), c.Token) {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), c.Token, "…"))
}

// Search finds documents by full text, the best first: at most max, and
// the total Paperless counted.
func (c *Client) Search(ctx context.Context, q string, max int) ([]Doc, int, error) {
	return c.list(ctx, url.Values{"query": {q}}, max)
}

// Recent lists the max newest documents, by the date they were created.
func (c *Client) Recent(ctx context.Context, max int) ([]Doc, error) {
	docs, _, err := c.list(ctx, url.Values{"ordering": {"-created"}}, max)
	return docs, err
}

// list asks for documents with q (a search or an order), at most max.
func (c *Client) list(ctx context.Context, q url.Values, max int) ([]Doc, int, error) {
	var page struct {
		Count   int `json:"count"`
		Results []struct {
			ID            int    `json:"id"`
			Title         string `json:"title"`
			Correspondent *int   `json:"correspondent"`
			Created       string `json:"created"`
			CreatedDate   string `json:"created_date"`
		} `json:"results"`
	}
	q.Set("page_size", strconv.Itoa(max))
	q.Set("fields", "id,title,correspondent,created,created_date") // not the OCR text
	q.Set("truncate_content", "true")
	if err := c.get(ctx, "/api/documents/", q, &page); err != nil {
		return nil, 0, err
	}
	if len(page.Results) > max {
		page.Results = page.Results[:max]
	}
	ids := map[int]bool{}
	for _, r := range page.Results {
		if r.Correspondent != nil {
			ids[*r.Correspondent] = true
		}
	}
	names := c.correspondents(ctx, ids)
	pub := strings.TrimRight(c.PublicURL, "/")
	out := make([]Doc, 0, len(page.Results))
	for _, r := range page.Results {
		d := Doc{ID: r.ID, Title: r.Title}
		if r.Correspondent != nil {
			d.Correspondent = names[*r.Correspondent]
		}
		for _, s := range []string{r.Created, r.CreatedDate} {
			if len(s) >= 10 {
				if t, err := time.Parse("2006-01-02", s[:10]); err == nil {
					d.Created = t
					break
				}
			}
		}
		if pub != "" {
			d.Link = pub + "/documents/" + strconv.Itoa(r.ID) + "/details"
		}
		out = append(out, d)
	}
	return out, page.Count, nil
}

// correspondents names the ids in one request; one Paperless will not
// name stays nameless rather than failing the search.
func (c *Client) correspondents(ctx context.Context, ids map[int]bool) map[int]string {
	out := map[int]string{}
	if len(ids) == 0 {
		return out
	}
	var list []int
	for id := range ids {
		list = append(list, id)
	}
	sort.Ints(list)
	strs := make([]string, len(list))
	for i, id := range list {
		strs[i] = strconv.Itoa(id)
	}
	var page struct {
		Results []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"results"`
	}
	if err := c.get(ctx, "/api/correspondents/", url.Values{"id__in": {strings.Join(strs, ",")}, "page_size": {strconv.Itoa(len(list))}}, &page); err != nil {
		return out
	}
	for _, r := range page.Results {
		if ids[r.ID] {
			out[r.ID] = r.Name
		}
	}
	return out
}
