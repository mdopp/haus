package napi

import (
	"net/http"
	"strings"

	"github.com/mdopp/haus/internal/legacy"
)

// AssetLinks serves /.well-known/assetlinks.json (Digital Asset Links): the
// app's trusted web activity checks the origin against its signing
// certificate's SHA-256 fingerprints, so Haus's pages open without a URL
// bar. fingerprints is the template's comma-separated value — public, not a
// secret. Without it the statement is [] (valid, verifies nothing).
func AssetLinks(fingerprints string) http.Handler {
	fps := []string{}
	for _, f := range strings.Split(fingerprints, ",") {
		if f = strings.TrimSpace(f); f != "" {
			fps = append(fps, f)
		}
	}
	statement := []any{}
	if len(fps) > 0 {
		statement = append(statement, map[string]any{
			"relation": []string{"delegate_permission/common.handle_all_urls"},
			"target":   map[string]any{"namespace": "android_app", "package_name": legacy.AppPackage, "sha256_cert_fingerprints": fps},
		})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, statement)
	})
}
