package pages

import (
	_ "embed"
	"net/http"
)

// Geist, served by Haus itself (haus#35): the family's box asks no one
// else for a font, and a page without it falls back to the phone's own
// (schrift/OFL.txt: the licence, and which cut this is). The version is in
// the path, so the browser keeps it for good.
//
//go:embed schrift/geist-1b5ebf.woff2
var geist []byte

// FontPath is where the pages find Geist.
const FontPath = "/schrift/geist-1b5ebf.woff2"

// Font serves Geist at FontPath, and nothing else under /schrift/.
func Font() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "nur lesen", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path != FontPath {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "font/woff2")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.Write(geist)
	}
}
