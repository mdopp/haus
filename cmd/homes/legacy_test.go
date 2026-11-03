package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// laib#1021: the box routes the old name haus.<domain> here too. A page
// asked for there goes to the same path on homes.<domain> (308); the old
// app's /napi/ calls, the calls from inside the box and everything under
// the new name are served where they are.
func TestLegacyHostRedirectsPagesOnly(t *testing.T) {
	s := testSite(t)
	s.publicURL = "https://homes.dopp.example"
	h := routes(s)
	for _, c := range []struct {
		host, method, target string
		want                 int
		location             string
	}{
		// the old name's pages, whatever else rides along: to the new name
		{"haus.dopp.example", "GET", "/start", 308, "https://homes.dopp.example/start"},
		{"haus.dopp.example", "GET", "/", 308, "https://homes.dopp.example/"},
		{"Haus.Dopp.Example:443", "GET", "/kalender?tag=2026-11-03", 308, "https://homes.dopp.example/kalender?tag=2026-11-03"},
		{"haus.dopp.example", "POST", "/pair-device", 308, "https://homes.dopp.example/pair-device"},
		{"haus.dopp.example", "GET", "/napi", 308, "https://homes.dopp.example/napi"},
		// what the old app calls with its device token: served (401 without one)
		{"haus.dopp.example", "GET", "/napi/whoami", 401, ""},
		{"haus.dopp.example", "GET", "/napi/app", 401, ""},
		{"haus.dopp.example", "POST", "/napi/sicherung/uploads", 401, ""},
		// from inside the box: never sent anywhere
		{"host.containers.internal:8650", "POST", "/mcp/haushalt", 401, ""},
		{"127.0.0.1:8650", "GET", "/healthz", 200, ""},
		{"127.0.0.1:8650", "GET", "/start", 403, ""},
		// the new name, and another name on the domain: as they are
		{"homes.dopp.example", "GET", "/start", 403, ""},
		{"homes.dopp.example", "GET", "/healthz", 200, ""},
		{"haus.example", "GET", "/start", 403, ""},
		{"haushalt.dopp.example", "GET", "/start", 403, ""},
	} {
		req := httptest.NewRequest(c.method, c.target, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
		req.Host = c.host
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != c.want || rec.Header().Get("Location") != c.location {
			t.Errorf("%s %s%s → %d %q, want %d %q", c.method, c.host, c.target, rec.Code, rec.Header().Get("Location"), c.want, c.location)
		}
	}
}

// The old name is derived from HOMES_PUBLIC_URL, never a fixed domain; with
// no such address, or one still under the old name, nothing is redirected.
func TestLegacyHostDerived(t *testing.T) {
	for pub, want := range map[string]string{
		"https://homes.dopp.example":  "haus.dopp.example",
		"https://homes.dopp.example/": "haus.dopp.example",
		"http://homes.box.test:8650":  "haus.box.test",
		"https://Homes.Beispiel.DE":   "haus.beispiel.de",
		"":                            "",
		"homes.dopp.example":          "",
		"https://localhost":           "",
		"https://127.0.0.1":           "",
		"https://haus.dopp.example":   "",
		"ftp://homes.dopp.example":    "",
	} {
		if got, _ := oldHost(pub); got != want {
			t.Errorf("oldHost(%q) = %q, want %q", pub, got, want)
		}
	}
	s := testSite(t)
	s.publicURL = "https://haus.dopp.example" // a box not moved yet: no loop
	req := httptest.NewRequest("GET", "/start", nil)
	req.Host = "haus.dopp.example"
	rec := httptest.NewRecorder()
	routes(s).ServeHTTP(rec, req)
	if rec.Code == 308 {
		t.Fatalf("redirected to itself: %q", rec.Header().Get("Location"))
	}
}
