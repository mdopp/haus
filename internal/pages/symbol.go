package pages

import (
	"embed"
	"net/http"
	"path"
)

// Haus's own icon (owner, 2026-11-02): the terminal prompt inside a green
// house, the same mark as the app's. symbol/icon.svg is the master;
// tools/icons renders the other files from it, never by hand.
//
//go:embed symbol/favicon.svg symbol/favicon-32.png symbol/apple-touch-icon.png symbol/icon-192.png symbol/icon-512.png symbol/icon-maskable-512.png
var symbole embed.FS

// SymbolPath is where the pages find the icons and the web app manifest.
const SymbolPath = "/symbol/"

// manifest makes Haus a web app on a phone's home screen: its name, Start,
// and the icons. The ground is the icon's.
const manifest = `{"name":"Haus","short_name":"Haus","lang":"de","start_url":"/start","scope":"/","display":"standalone",` +
	`"background_color":"#0c120e","theme_color":"#0c0c0e","icons":[` +
	`{"src":"/symbol/favicon.svg","sizes":"any","type":"image/svg+xml","purpose":"any"},` +
	`{"src":"/symbol/icon-192.png","sizes":"192x192","type":"image/png","purpose":"any"},` +
	`{"src":"/symbol/icon-512.png","sizes":"512x512","type":"image/png","purpose":"any"},` +
	`{"src":"/symbol/icon-maskable-512.png","sizes":"512x512","type":"image/png","purpose":"maskable"}]}`

// symbolHead is the head's part for the icons: the tab, the home screen
// (iOS's touch icon, everyone else's manifest). The manifest goes with the
// Authelia cookie, or the proxy turns it away.
const symbolHead = "<link rel=icon href=/symbol/favicon.svg type=image/svg+xml><link rel=icon href=/symbol/favicon-32.png sizes=32x32 type=image/png>" +
	"<link rel=apple-touch-icon href=/symbol/apple-touch-icon.png><link rel=manifest href=/symbol/haus.webmanifest crossorigin=use-credentials>"

// Symbol serves the icons and the manifest under SymbolPath, and nothing else there.
func Symbol() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "nur lesen", http.StatusMethodNotAllowed)
			return
		}
		name := path.Base(r.URL.Path)
		if r.URL.Path != SymbolPath+name {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=86400")
		if name == "haus.webmanifest" {
			w.Header().Set("Content-Type", "application/manifest+json")
			w.Write([]byte(manifest))
			return
		}
		b, err := symbole.ReadFile("symbol/" + name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if path.Ext(name) == ".svg" {
			w.Header().Set("Content-Type", "image/svg+xml")
			w.Header().Set("Content-Security-Policy", "default-src 'none'")
		} else {
			w.Header().Set("Content-Type", "image/png")
		}
		w.Write(b)
	}
}
