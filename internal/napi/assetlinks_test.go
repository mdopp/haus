package napi

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// The app's TWA verifies the origin against its signing fingerprints; Haus
// serves the statement from its template value (not a secret), publicly.
func TestAssetLinks(t *testing.T) {
	rec := httptest.NewRecorder()
	AssetLinks(" AA:BB:CC , DD:EE:FF ,").ServeHTTP(rec, httptest.NewRequest("GET", "/.well-known/assetlinks.json", nil))
	body := strings.TrimSpace(rec.Body.String())
	want := `[{"relation":["delegate_permission/common.handle_all_urls"],"target":{"namespace":"android_app","package_name":"cloud.dopp.solaris","sha256_cert_fingerprints":["AA:BB:CC","DD:EE:FF"]}}]`
	if rec.Code != 200 || body != want || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("assetlinks: %d %s\nwant %s", rec.Code, body, want)
	}
	rec = httptest.NewRecorder()
	AssetLinks("").ServeHTTP(rec, httptest.NewRequest("GET", "/.well-known/assetlinks.json", nil))
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("without fingerprints: %s", rec.Body.String())
	}
}
