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
