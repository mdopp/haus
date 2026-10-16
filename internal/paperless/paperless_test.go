package paperless

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mdopp/haus/internal/paperless/paperlessfake"
)

const token = "pl-geheim-123"

// A search is one GET /api/documents/?query=… with the token, at most max
// results, only the fields it shows (no OCR text), and the correspondents'
// names in one more request; the link is built from the public address.
func TestSearch(t *testing.T) {
	fake := paperlessfake.New(token)
	defer fake.Close()
	fake.Correspondent(3, "Stadtwerke")
	fake.Add(
		paperlessfake.Doc{ID: 12, Title: "Rechnung März", Content: strings.Repeat("Strom ", 10000), Correspondent: 3, Created: "2025-03-14"},
		paperlessfake.Doc{ID: 13, Title: "Rechnung April", Correspondent: 9, Created: "2025-04-14T00:00:00+02:00"},
		paperlessfake.Doc{ID: 14, Title: "Rechnung Mai"},
		paperlessfake.Doc{ID: 15, Title: "Mietvertrag"},
	)
	c := &Client{URL: fake.URL + "/", Token: token, PublicURL: "https://paperless.example/"}
	docs, total, err := c.Search(context.Background(), "rechnung", 2)
	if err != nil || total != 3 || len(docs) != 2 {
		t.Fatalf("%+v %d %v", docs, total, err)
	}
	d := docs[0]
	if d.ID != 12 || d.Title != "Rechnung März" || d.Correspondent != "Stadtwerke" || d.Created.Format("2006-01-02") != "2025-03-14" || d.Link != "https://paperless.example/documents/12/details" {
		t.Fatalf("first: %+v", d)
	}
	if docs[1].Correspondent != "" || docs[1].Created.Format("2006-01-02") != "2025-04-14" {
		t.Fatalf("an unknown correspondent is no name; a date-time is its day: %+v", docs[1])
	}
	seen := fake.Seen()
	if len(seen) != 2 {
		t.Fatalf("requests: %d", len(seen))
	}
	q := seen[0].URL.Query()
	if seen[0].URL.Path != "/api/documents/" || q.Get("query") != "rechnung" || q.Get("page_size") != "2" || !strings.Contains(q.Get("fields"), "title") || strings.Contains(q.Get("fields"), "content") {
		t.Fatalf("search request: %s", seen[0].URL)
	}
	if seen[1].URL.Path != "/api/correspondents/" || seen[1].URL.Query().Get("id__in") != "3,9" {
		t.Fatalf("correspondents request: %s", seen[1].URL)
	}
	for _, r := range seen {
		if r.Header.Get("Authorization") != "Token "+token || r.Method != http.MethodGet {
			t.Fatalf("%s %s: %v", r.Method, r.URL, r.Header)
		}
	}
	// without a public address: no link, never the internal one
	c.PublicURL = ""
	if docs, _, _ := c.Search(context.Background(), "mietvertrag", 20); len(docs) != 1 || docs[0].Link != "" {
		t.Fatalf("no link: %+v", docs)
	}
}

// Paperless refusing or failing is an error that names the status and
// never the token, even when Paperless echoes it.
func TestErrorsKeepTheToken(t *testing.T) {
	fake := paperlessfake.New(token)
	defer fake.Close()
	c := &Client{URL: fake.URL, Token: "falsch-" + token}
	_, _, err := c.Search(context.Background(), "x", 20)
	if err == nil || !strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), token) {
		t.Fatalf("refused: %v", err)
	}
	c.Token = token
	fake.Status = http.StatusInternalServerError
	_, _, err = c.Search(context.Background(), "x", 20)
	if err == nil || !strings.Contains(err.Error(), "500") || strings.Contains(err.Error(), token) {
		t.Fatalf("failing: %v", err)
	}
	down := &Client{URL: "http://127.0.0.1:1", Token: token}
	if _, _, err := down.Search(context.Background(), "x", 20); err == nil || strings.Contains(err.Error(), token) {
		t.Fatalf("unreachable: %v", err)
	}
}

// A redirect is not followed: the token never goes to another address.
func TestNoRedirect(t *testing.T) {
	var hits atomic.Int32
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Write([]byte(`{"count":1,"results":[{"id":1,"title":"x"}]}`))
	}))
	defer evil.Close()
	fake := paperlessfake.New(token)
	defer fake.Close()
	fake.Redirect = evil.URL + "/api/documents/"
	c := &Client{URL: fake.URL, Token: token}
	if _, _, err := c.Search(context.Background(), "x", 20); err == nil {
		t.Fatal("a redirect is an error")
	}
	if hits.Load() != 0 {
		t.Fatal("followed the redirect")
	}
}

// A hanging Paperless is cut off by the request's deadline.
func TestTimeout(t *testing.T) {
	hang := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer hang.Close()
	c := &Client{URL: hang.URL, Token: token}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, _, err := c.Search(ctx, "x", 20); err == nil || time.Since(start) > 5*time.Second {
		t.Fatalf("%v after %v", err, time.Since(start))
	}
	if defaultHTTP.Timeout == 0 || defaultHTTP.Timeout > 30*time.Second {
		t.Fatalf("a client timeout of its own: %v", defaultHTTP.Timeout)
	}
}

// A Paperless that ignores page_size still gives at most max.
func TestAtMostMax(t *testing.T) {
	fake := paperlessfake.New(token)
	defer fake.Close()
	fake.IgnorePageSize = true
	for i := 1; i <= 5; i++ {
		fake.Add(paperlessfake.Doc{ID: i, Title: "Rechnung"})
	}
	c := &Client{URL: fake.URL, Token: token}
	if docs, total, err := c.Search(context.Background(), "rechnung", 2); err != nil || len(docs) != 2 || total != 5 {
		t.Fatalf("%d %d %v", len(docs), total, err)
	}
}

// A caller's own HTTP client does not follow redirects either.
func TestNoRedirectOwnClient(t *testing.T) {
	var hits atomic.Int32
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer evil.Close()
	fake := paperlessfake.New(token)
	defer fake.Close()
	fake.Redirect = evil.URL + "/api/documents/"
	own := &http.Client{}
	c := &Client{URL: fake.URL, Token: token, HTTP: own}
	if _, _, err := c.Search(context.Background(), "x", 20); err == nil || hits.Load() != 0 {
		t.Fatalf("%v, %d hits", err, hits.Load())
	}
	if own.CheckRedirect != nil {
		t.Fatal("the caller's client is not changed")
	}
}

// The token is taken out of any error that would carry it, here a
// transport error naming an address with it.
func TestScrub(t *testing.T) {
	c := &Client{URL: "http://127.0.0.1:1/" + token, Token: token}
	_, _, err := c.Search(context.Background(), "x", 20)
	if err == nil || strings.Contains(err.Error(), token) || !strings.Contains(err.Error(), "nicht erreichbar") {
		t.Fatalf("%v", err)
	}
}

// An answer bigger than maxBody is not read whole: an error.
func TestMaxBody(t *testing.T) {
	big := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"count":1,"results":[{"id":1,"title":"` + strings.Repeat("x", maxBody+1000) + `"}]}`))
	}))
	defer big.Close()
	c := &Client{URL: big.URL, Token: token}
	if docs, _, err := c.Search(context.Background(), "x", 20); err == nil {
		t.Fatalf("read %d docs past the cap", len(docs))
	}
}

// haus#10: the document widget lists the newest documents, not a search.
func TestRecent(t *testing.T) {
	fake := paperlessfake.New(token)
	defer fake.Close()
	fake.Add(paperlessfake.Doc{ID: 1, Title: "Alt", Created: "2020-01-01"}, paperlessfake.Doc{ID: 2, Title: "Neu", Created: "2026-09-30"})
	c := &Client{URL: fake.URL, Token: token}
	docs, err := c.Recent(context.Background(), 1)
	if err != nil || len(docs) != 1 || docs[0].Title != "Neu" || docs[0].Link != "" {
		t.Fatalf("%+v %v", docs, err)
	}
	q := fake.Seen()[0].URL.Query()
	if q.Has("query") || q.Get("ordering") != "-created" || q.Get("page_size") != "1" || strings.Contains(q.Get("fields"), "content") {
		t.Fatalf("request: %s", fake.Seen()[0].URL)
	}
}

// A document's tags are named in one more request, only when a hit has
// any; an id Paperless does not name is left out, and tags Paperless will
// not name leave the search whole, nameless.
func TestTags(t *testing.T) {
	fake := paperlessfake.New(token)
	defer fake.Close()
	fake.Tag(1, "Rechnung")
	fake.Tag(2, "Strom")
	fake.Add(
		paperlessfake.Doc{ID: 12, Title: "Rechnung März", Tags: []int{2, 1, 7}},
		paperlessfake.Doc{ID: 13, Title: "Rechnung April", Tags: []int{2}},
		paperlessfake.Doc{ID: 14, Title: "Rechnung Mai"},
	)
	c := &Client{URL: fake.URL, Token: token}
	docs, _, err := c.Search(context.Background(), "rechnung", 20)
	if err != nil || len(docs) != 3 {
		t.Fatalf("%+v %v", docs, err)
	}
	if strings.Join(docs[0].Tags, ",") != "Strom,Rechnung" || strings.Join(docs[1].Tags, ",") != "Strom" || len(docs[2].Tags) != 0 {
		t.Fatalf("tags: %q %q %q", docs[0].Tags, docs[1].Tags, docs[2].Tags)
	}
	var tagReqs []string
	for _, r := range fake.Seen() {
		if r.URL.Path == "/api/tags/" {
			tagReqs = append(tagReqs, r.URL.Query().Get("id__in")+"/"+r.URL.Query().Get("page_size"))
		}
		if r.URL.Path == "/api/documents/" && !strings.Contains(r.URL.Query().Get("fields"), "tags") {
			t.Fatalf("fields: %s", r.URL)
		}
	}
	if strings.Join(tagReqs, " ") != "1,2,7/3" {
		t.Fatalf("one tag request for every id: %q", tagReqs)
	}

	before := len(fake.Seen())
	if docs, _, _ := c.Search(context.Background(), "mai", 20); len(docs) != 1 || len(fake.Seen()) != before+1 {
		t.Fatalf("no tags, no tag request: %+v, %d requests", docs, len(fake.Seen())-before)
	}

	fake.FailPath = "/api/tags/"
	docs, _, err = c.Search(context.Background(), "rechnung", 20)
	if err != nil || len(docs) != 3 || len(docs[0].Tags) != 0 || docs[0].Title != "Rechnung März" {
		t.Fatalf("tags unreadable: %+v %v", docs, err)
	}
}

// haus#13: ByIDs reads the documents with these ids in one request, those
// still there; Stamps lists every document page by page, all or an
// error; Texts reads documents with their OCR text.
func TestByIDsStampsTexts(t *testing.T) {
	fake := paperlessfake.New(token)
	defer fake.Close()
	fake.Correspondent(3, "Stadtwerke")
	for i := 1; i <= 205; i++ {
		fake.Add(paperlessfake.Doc{ID: i, Title: fmt.Sprintf("Brief %d", i), Content: "Text " + strconv.Itoa(i), Correspondent: 3, Modified: "2026-10-01T12:00:00.5+02:00"})
	}
	c := &Client{URL: fake.URL, Token: token, PublicURL: "https://paperless.example"}
	docs, err := c.ByIDs(context.Background(), []int{9, 300, 4})
	if err != nil || len(docs) != 2 || docs[0].ID != 4 || docs[1].ID != 9 || docs[0].Correspondent != "Stadtwerke" || docs[0].Link == "" {
		t.Fatalf("%+v %v", docs, err)
	}
	// the start of the text only: Paperless cuts it at 550 characters
	if q := fake.Seen()[0].URL.Query(); q.Get("id__in") != "9,300,4" || !strings.Contains(q.Get("fields"), "content") || q.Get("truncate_content") != "true" {
		t.Fatalf("asked %s", fake.Seen()[0].URL)
	}
	if docs[0].Start != "Text 4" {
		t.Fatalf("start: %q", docs[0].Start)
	}
	if docs, err := c.ByIDs(context.Background(), nil); err != nil || docs != nil {
		t.Fatalf("no ids: %v %v", docs, err)
	}
	stamps, err := c.Stamps(context.Background())
	if err != nil || len(stamps) != 205 || stamps[204].ID != 205 || stamps[0].Modified.UTC().Format(time.RFC3339) != "2026-10-01T10:00:00Z" {
		t.Fatalf("%d stamps %v", len(stamps), err)
	}
	texts, err := c.Texts(context.Background(), []int{5, 6})
	if err != nil || len(texts) != 2 || texts[0].Content != "Text 5" || texts[1].Title != "Brief 6" || texts[1].Modified.IsZero() || texts[0].Start != "" {
		t.Fatalf("%+v %v", texts, err)
	}
	// a page that fails is no listing at all
	fake.FailPath = "/api/documents/"
	if s, err := c.Stamps(context.Background()); err == nil || s != nil {
		t.Fatalf("failing: %v %v", s, err)
	}
}

// A listing that ends short of the count Paperless gave (a page lost, a
// server cutting it) is an error, never a part: what it left out, a
// mirror would delete. A listing that never ends is cut off too.
func TestStampsAllOrNothing(t *testing.T) {
	f := paperlessfake.New("tok")
	defer f.Close()
	for i := 1; i <= 120; i++ {
		f.Add(paperlessfake.Doc{ID: i, Modified: "2026-01-01T00:00:00Z"})
	}
	c := &Client{URL: f.URL, Token: "tok"}
	if got, err := c.Stamps(context.Background()); err != nil || len(got) != 120 {
		t.Fatalf("%d %v", len(got), err)
	}
	f.CountMore = 5
	if got, err := c.Stamps(context.Background()); err == nil {
		t.Fatalf("short listing taken: %d", len(got))
	}
	f.CountMore = 0
	old := maxPages
	maxPages = 1
	defer func() { maxPages = old }()
	if _, err := c.Stamps(context.Background()); err == nil {
		t.Fatal("a listing past maxPages taken")
	}
}
