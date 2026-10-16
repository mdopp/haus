package main

import (
	"context"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/mdopp/haus/internal/usage"
	"github.com/mdopp/haus/internal/usage/usagefake"
)

// #50: the routes count, behind the proxy secret only: a page Caddy let
// through is a view; one without the secret (403), a path Haus does not
// serve (404) and a /napi call without a paired phone (401) are not.
func TestRoutesCountPageViews(t *testing.T) {
	fake := usagefake.New("um")
	t.Cleanup(fake.Close)
	s := testSite(t)
	s.usage = usage.New(fake.URL, "um")
	h := routes(s)
	for _, q := range []struct{ path, secret string }{
		{"/start", "geheim"},
		{"/start?von=widget.geraet", "geheim"},
		{"/start", ""},
		{"/gibtsnicht", "geheim"},
		{"/napi/box/home", ""},
		{"/healthz", ""},
	} {
		req := httptest.NewRequest("GET", q.path, nil)
		req.Header.Set("Remote-User", "anna")
		if q.secret != "" {
			req.Header.Set("X-Haus-Proxy", q.secret)
		}
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
	s.usage.Flush(context.Background())
	got := map[string]int64{}
	for k, n := range fake.Counts() {
		got[k.App+" "+k.Event] += n
	}
	if want := map[string]int64{"haus seite.start": 2, "haus von.widget.geraet": 1}; !reflect.DeepEqual(got, want) {
		t.Errorf("counted %v, want %v", got, want)
	}
	for _, b := range fake.Bodies() {
		if strings.Contains(b, "anna") || strings.Contains(b, "geheim") {
			t.Errorf("left Haus: %s", b)
		}
	}
}
