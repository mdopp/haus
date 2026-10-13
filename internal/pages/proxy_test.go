package pages

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mdopp/haus/internal/devices"
)

// haus#1: without Caddy's secret nobody reaches a page, however the
// Remote-User header reads.
func TestPagesNeedTheProxySecret(t *testing.T) {
	hit := 0
	h := RequireProxy("s3cret", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit++ }))
	for _, c := range []struct {
		header string
		want   int
	}{{"", 403}, {"wrong", 403}, {"s3cret", 200}} {
		req := httptest.NewRequest("GET", "/box", nil)
		req.Header.Set("Remote-User", "admin")
		if c.header != "" {
			req.Header.Set("X-Haus-Proxy", c.header)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Fatalf("header %q → %d, want %d", c.header, rec.Code, c.want)
		}
	}
	if hit != 1 {
		t.Fatalf("the page ran %d times, want 1", hit)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	if rec.Code != 200 {
		t.Fatal("healthz must stay free")
	}
	if rec := httptest.NewRecorder(); func() int {
		RequireProxy("", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).ServeHTTP(rec, httptest.NewRequest("GET", "/box", nil))
		return rec.Code
	}() != 403 {
		t.Fatal("an empty secret must not open the pages")
	}
}

// haus#1: the Box page decides nothing — a POST is refused, the page links
// to Laib's inbox.
func TestBoxDecidesNothing(t *testing.T) {
	h := Box(nil, "https://laib.box.test")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/box/inbox/7/approve", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("a verdict through Haus → %d, want 403", rec.Code)
	}
}

// haus#5: the family pairs "the app", never "Solaris".
func TestPairingPageSaysApp(t *testing.T) {
	store, err := devices.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/box/geraete", strings.NewReader("name=Pixel"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Remote-User", "anna")
	rec := httptest.NewRecorder()
	Geraete(store, "https://haus.example").ServeHTTP(rec, req)
	if body := rec.Body.String(); strings.Contains(strings.ToLower(body), "solaris-app") || !strings.Contains(body, "App verbinden") {
		t.Fatalf("the pairing page names Solaris:\n%s", body)
	}
}
