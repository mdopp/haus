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
	"regexp"
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
	Tags          []string  // the tags' names, as the document lists them
	// Start is the beginning of its text as Paperless lists it (it cuts
	// at 550 characters); only ByIDs reads it.
	Start string
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
	return c.getN(ctx, p, q, v, maxBody)
}

// getN is get reading at most limit bytes of the answer.
func (c *Client) getN(ctx context.Context, p string, q url.Values, v any, limit int64) error {
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
	if err := json.NewDecoder(io.LimitReader(resp.Body, limit)).Decode(v); err != nil {
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
	return c.list(ctx, url.Values{"query": {q}}, max, shown)
}

// Recent lists the max newest documents, by the date they were created.
func (c *Client) Recent(ctx context.Context, max int) ([]Doc, error) {
	docs, _, err := c.list(ctx, url.Values{"ordering": {"-created"}}, max, shown)
	return docs, err
}

// apiDoc is a document as the API lists it; content and modified only
// when asked for.
type apiDoc struct {
	ID            int    `json:"id"`
	Title         string `json:"title"`
	Correspondent *int   `json:"correspondent"`
	Created       string `json:"created"`
	CreatedDate   string `json:"created_date"`
	Tags          []int  `json:"tags"`
	Content       string `json:"content"`
	Modified      string `json:"modified"`
}

// shown are the fields a hit shows: not the OCR text
const shown = "id,title,correspondent,created,created_date,tags"

// list asks for documents with q (a search or an order), at most max;
// fields is what it reads of each.
func (c *Client) list(ctx context.Context, q url.Values, max int, fields string) ([]Doc, int, error) {
	var page struct {
		Count   int      `json:"count"`
		Results []apiDoc `json:"results"`
	}
	q.Set("page_size", strconv.Itoa(max))
	q.Set("fields", fields)
	q.Set("truncate_content", "true")
	if err := c.get(ctx, "/api/documents/", q, &page); err != nil {
		return nil, 0, err
	}
	if len(page.Results) > max {
		page.Results = page.Results[:max]
	}
	return c.named(ctx, page.Results), page.Count, nil
}

// named turns what the API listed into Docs, the correspondents and tags
// by name (one request each).
func (c *Client) named(ctx context.Context, rs []apiDoc) []Doc {
	ids, tagIDs := map[int]bool{}, map[int]bool{}
	for _, r := range rs {
		if r.Correspondent != nil {
			ids[*r.Correspondent] = true
		}
		for _, t := range r.Tags {
			tagIDs[t] = true
		}
	}
	names := c.names(ctx, "/api/correspondents/", ids)
	tags := c.names(ctx, "/api/tags/", tagIDs)
	out := make([]Doc, 0, len(rs))
	for _, r := range rs {
		d := Doc{ID: r.ID, Title: r.Title, Start: r.Content}
		if r.Correspondent != nil {
			d.Correspondent = names[*r.Correspondent]
		}
		for _, t := range r.Tags {
			if n, ok := tags[t]; ok {
				d.Tags = append(d.Tags, n)
			}
		}
		for _, s := range []string{r.Created, r.CreatedDate} {
			if len(s) >= 10 {
				if t, err := time.Parse("2006-01-02", s[:10]); err == nil {
					d.Created = t
					break
				}
			}
		}
		d.Link = c.Page(r.ID)
		out = append(out, d)
	}
	return out
}

// idList is ids as Paperless's id__in takes them.
func idList(ids []int) string {
	strs := make([]string, len(ids))
	for i, id := range ids {
		strs[i] = strconv.Itoa(id)
	}
	return strings.Join(strs, ",")
}

// ByIDs reads the documents with these ids, in one request, each with the
// start of its text: those Paperless still has and lets this user see.
// One deleted or hidden since is not in the answer (haus#13: a hit of the
// memory library is checked here, as a note is against its folder).
func (c *Client) ByIDs(ctx context.Context, ids []int) ([]Doc, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	docs, _, err := c.list(ctx, url.Values{"id__in": {idList(ids)}}, len(ids), shown+",content")
	return docs, err
}

// Stamp is a document's id and when Paperless last changed it (zero: not
// said).
type Stamp struct {
	ID       int
	Modified time.Time
}

// pageSize is a listing's page; maxPages bounds the listing.
const pageSize = 100

var maxPages = 2000

// Stamps lists every document this user may see, id and last change, page
// by page in id order. It is all of them or an error, never a part: what
// it leaves out, a mirror would delete.
func (c *Client) Stamps(ctx context.Context) ([]Stamp, error) {
	var out []Stamp
	for page := 1; ; page++ {
		if page > maxPages {
			return nil, fmt.Errorf("Paperless: mehr als %d Seiten Dokumente", maxPages)
		}
		var p struct {
			Count   int      `json:"count"`
			Next    *string  `json:"next"`
			Results []apiDoc `json:"results"`
		}
		q := url.Values{"fields": {"id,modified"}, "page_size": {strconv.Itoa(pageSize)}, "page": {strconv.Itoa(page)}, "ordering": {"id"}}
		if err := c.get(ctx, "/api/documents/", q, &p); err != nil {
			return nil, err
		}
		for _, r := range p.Results {
			out = append(out, Stamp{ID: r.ID, Modified: modified(r.Modified)})
		}
		if p.Next == nil || len(p.Results) == 0 {
			if len(out) < p.Count {
				return nil, fmt.Errorf("Paperless: %d von %d Dokumenten gelistet", len(out), p.Count)
			}
			return out, nil
		}
	}
}

func modified(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// Text is one document whole, as the memory library indexes it: what a
// hit shows, its OCR text and its last change.
type Text struct {
	Doc
	Content  string
	Modified time.Time
}

// maxTexts caps one answer with OCR text: a few long documents.
const maxTexts = 64 << 20

// Texts reads the documents with these ids with their OCR text, in one
// request: those Paperless still has.
func (c *Client) Texts(ctx context.Context, ids []int) ([]Text, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var page struct {
		Results []apiDoc `json:"results"`
	}
	q := url.Values{"id__in": {idList(ids)}, "page_size": {strconv.Itoa(len(ids))}, "fields": {shown + ",content,modified"}}
	if err := c.getN(ctx, "/api/documents/", q, &page, maxTexts); err != nil {
		return nil, err
	}
	docs := c.named(ctx, page.Results)
	out := make([]Text, len(docs))
	for i, d := range docs {
		d.Start = ""
		out[i] = Text{Doc: d, Content: page.Results[i].Content, Modified: modified(page.Results[i].Modified)}
	}
	return out, nil
}

// markRE is the random part Haus puts in a filed file's name
// (<stem>-<mark>.<ext>, napi's place).
var markRE = regexp.MustCompile(`^[0-9a-f]{8}$`)

// Filed is the document Paperless made of the file Haus filed with mark
// in its name (haus#68): Paperless keeps the consumed file's name as the
// document's original_filename. 0 while it is still being read, or when
// the mark is not exactly one document; an error for a mark that is none
// (nothing asked) or Paperless failing.
func (c *Client) Filed(ctx context.Context, mark string) (int, error) {
	if !markRE.MatchString(mark) {
		return 0, errors.New("Paperless: keine Ablage-Marke")
	}
	var page struct {
		Count   int      `json:"count"`
		Results []apiDoc `json:"results"`
	}
	q := url.Values{"original_filename__icontains": {"-" + mark + "."}, "fields": {"id"}, "page_size": {"2"}}
	if err := c.get(ctx, "/api/documents/", q, &page); err != nil {
		return 0, err
	}
	if page.Count != 1 || len(page.Results) != 1 {
		return 0, nil
	}
	return page.Results[0].ID, nil
}

// Page is Paperless's own page for a document; "" without PublicURL.
func (c *Client) Page(id int) string {
	pub := strings.TrimRight(c.PublicURL, "/")
	if pub == "" {
		return ""
	}
	return pub + "/documents/" + strconv.Itoa(id) + "/details"
}

// names names the ids of one kind (correspondents, tags) in one request;
// one Paperless will not name stays nameless rather than failing the
// search.
func (c *Client) names(ctx context.Context, path string, ids map[int]bool) map[int]string {
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
	if err := c.get(ctx, path, url.Values{"id__in": {strings.Join(strs, ",")}, "page_size": {strconv.Itoa(len(list))}}, &page); err != nil {
		return out
	}
	for _, r := range page.Results {
		if ids[r.ID] {
			out[r.ID] = r.Name
		}
	}
	return out
}

// Have is which of these original file names Paperless still holds a
// document of (a scan's documents, haus 2026-10-27: the owner deletes
// some); an error when Paperless does not answer, then nothing is known.
func (c *Client) Have(ctx context.Context, names []string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, n := range names {
		if n == "" || out[n] {
			continue
		}
		var page struct {
			Count int `json:"count"`
		}
		q := url.Values{"original_filename__icontains": {n}, "fields": {"id"}, "page_size": {"1"}}
		if err := c.get(ctx, "/api/documents/", q, &page); err != nil {
			return nil, err
		}
		out[n] = page.Count > 0
	}
	return out, nil
}
