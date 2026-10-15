package main

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mdopp/haus/internal/devices"
	"github.com/mdopp/haus/internal/laib"
	"github.com/mdopp/haus/internal/napi"
)

func testSite(t *testing.T) site {
	t.Helper()
	store, err := devices.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ops := &laib.Client{URL: "http://127.0.0.1:1", Token: "ops"}
	return site{napi: napi.Config{Laib: ops}, store: store, ops: ops, proxySecret: "geheim", laibURL: "https://laib.example"}
}

// haus#26, laib ADR 0022: the app has no TWA, so Haus serves no Digital
// Asset Links statement: /.well-known/assetlinks.json is a path like any
// other, behind the proxy secret and landing on Start.
func TestNoAssetLinks(t *testing.T) {
	h := routes(testSite(t))
	for _, secret := range []string{"", "geheim"} {
		req := httptest.NewRequest("GET", "/.well-known/assetlinks.json", nil)
		if secret != "" {
			req.Header.Set("X-Haus-Proxy", secret)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		body := rec.Body.String()
		if rec.Code == 200 || strings.Contains(rec.Header().Get("Content-Type"), "json") || strings.Contains(body, "android_app") {
			t.Errorf("proxy secret %q: assetlinks served: %d %s %q", secret, rec.Code, rec.Header().Get("Content-Type"), body)
		}
	}
}

// Who gets in where: the pages only with the proxy secret Caddy adds,
// /healthz always, /napi with a device token (401, not the pages' 403), the
// Haushalt source with the Bote's bearer.
func TestRoutesGuards(t *testing.T) {
	h := routes(testSite(t))
	for _, c := range []struct {
		method, path string
		want         int
	}{
		{"GET", "/start", 403},
		{"POST", "/pair-device", 403},
		{"GET", "/pair-device", 403},
		{"GET", "/box/geraete", 403},
		{"POST", "/schalten", 403},
		{"POST", "/raeume/light.flur", 403},
		{"GET", "/schrift/geist-1.401.woff2", 403},
		{"GET", "/healthz", 200},
		{"GET", "/napi/box/home", 401},
		{"POST", "/mcp/haushalt", 401},
	} {
		req := httptest.NewRequest(c.method, c.path, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
		req.Header.Set("Remote-User", "anna")
		req.Header.Set("Remote-Groups", "admins")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Errorf("%s %s → %d %s, want %d", c.method, c.path, rec.Code, rec.Body.String(), c.want)
		}
	}
}
