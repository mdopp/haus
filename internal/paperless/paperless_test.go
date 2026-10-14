package paperless

import (
	"context"
	"net/http"
	"net/http/httptest"
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
