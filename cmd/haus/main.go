// Command haus is the family's surface for the Laib box (laib ADR 0014):
// pages for people, the companion API for the Android widgets. The box keys
// come from Laib; every other /napi path, and phones paired before Haus, go
// through the migration bridge (internal/napi/bridge.go) while it is set.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"
	_ "time/tzdata" // the image is scratch: TZ needs the zone database inside the binary

	"github.com/mdopp/haus/internal/devices"
	"github.com/mdopp/haus/internal/ha"
	"github.com/mdopp/haus/internal/haushalt"
	"github.com/mdopp/haus/internal/laib"
	"github.com/mdopp/haus/internal/napi"
	"github.com/mdopp/haus/internal/pages"
	"github.com/mdopp/haus/internal/persona"
	"github.com/mdopp/haus/internal/werkstatt"
)

var Version = "dev"

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	ops := &laib.Client{URL: env("LAIB_URL", "http://host.containers.internal:5900"), Token: os.Getenv("LAIB_TOKEN")}
	if ops.Token == "" {
		log.Fatal("haus: LAIB_TOKEN is required (minted on laib add haus as the haus hand)")
	}
	proxySecret := os.Getenv("HAUS_PROXY_SECRET")
	if proxySecret == "" {
		log.Fatal("haus: HAUS_PROXY_SECRET is required — Caddy sends it as X-Haus-Proxy, nothing else may reach the pages (ADR 0012 §3)")
	}
	store, err := devices.Open(env("HAUS_DATA", "/data"))
	if err != nil {
		log.Fatalf("haus: devices: %v", err)
	}
	pages.House = persona.Load(env("HAUS_PERSONA_DIR", "/persona"), os.Getenv("HAUS_PERSONA")) // laib ADR 0020; plain Haus without it
	cfg := napi.Config{Laib: ops, House: pages.House}
	own := func(ctx context.Context, b string) bool { _, ok := store.Lookup(b); return ok }
	cfg.Whoami = func(b string) (string, bool) {
		d, ok := store.Lookup(b)
		return d.UID, ok
	}
	bridge, paired, err := napi.BridgeFromEnv()
	if err != nil {
		log.Fatalf("haus: %v", err)
	}
	cfg.Bridge = bridge
	if cfg.HA, err = ha.FromEnv(); err != nil { // haus#10: the house paths from HA; unset, they stay on the bridge
		log.Fatalf("haus: %v", err)
	}
	if cfg.Energy, err = napi.EnergyFromEnv(); err != nil {
		log.Fatalf("haus: %v", err)
	}
	cfg.Validate = napi.Chain(own, paired) // phones paired before Haus keep working through the bridge
	// laib ADR 0017: the Haushalt MCP source; a tool only when its backend is set
	hh, err := haushalt.FromEnv()
	if err != nil {
		log.Fatalf("haus: %v", err)
	}
	hh.Version = Version
	cfg.Haushalt = hh // haus#10: the tool widgets from the same backends; none set, they stay on the bridge
	// haus#10: the camera capture into the share's Paperless consume folder; unset, the bridge's
	if cfg.UploadDir = os.Getenv("HAUS_UPLOAD_DIR"); cfg.UploadDir != "" {
		if fi, err := os.Stat(cfg.UploadDir); err != nil || !fi.IsDir() {
			log.Printf("haus: HAUS_UPLOAD_DIR %s is not a folder (yet): uploads answer 503 until it is", cfg.UploadDir)
		} else if err := napi.SweepUploads(cfg.UploadDir); err != nil { // parts a stopped Haus left staged
			log.Printf("haus: HAUS_UPLOAD_DIR: sweeping staged parts: %v", err)
		}
	}
	mux := http.NewServeMux()
	mux.Handle("/napi/", napi.Handler(cfg))
	// the Bote's bearer, not the proxy secret: Hermes calls the host port from its pod
	// (laib ADR 0012 §4; the template publishes 8650 lan:false, the firewall keeps
	// the LAN out). Not through Caddy: the route has no /mcp/ bypass.
	mux.Handle("/mcp/haushalt", haushalt.Handler(hh))
	// public, like /napi: the app's verifier fetches it without a login (the route bypasses Authelia for /.well-known/)
	mux.Handle("GET /.well-known/assetlinks.json", napi.AssetLinks(os.Getenv("HAUS_ANDROID_FINGERPRINTS")))
	laibURL := env("LAIB_PUBLIC_URL", "https://laib."+env("LAIB_DOMAIN", "example"))
	pagesMux := http.NewServeMux()
	// the family's page (haus#12): each card only from a backend that is set
	pagesMux.HandleFunc("/start", pages.Start(pages.StartConfig{Laib: ops, LaibURL: laibURL, HA: cfg.HA, Calendar: hh.Calendar, Notes: hh.Notes, Loc: hh.Loc}))
	tickets := werkstatt.FromEnv() // haus#15: the Werkstatt card; HAUS_WERKSTATT_REPOS unset, no card
	pagesMux.HandleFunc("/box", pages.BoxWith(ops, laibURL, tickets))
	pagesMux.HandleFunc("/box/geraete", pages.Geraete(store, os.Getenv("HAUS_PUBLIC_URL")))
	pagesMux.HandleFunc("/pair-device", pages.PairDevice(store)) // the app's own pairing path (haus#6)
	pagesMux.HandleFunc("/box/", pages.BoxWith(ops, laibURL, tickets))
	pagesMux.HandleFunc("/persona/icon.svg", pages.Icon())
	pagesMux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	pagesMux.HandleFunc("/", pages.ToStart()) // "/", the app's /#/p/… routes and every path not served land on Start
	mux.Handle("/", pages.RequireProxy(proxySecret, pagesMux))
	addr := env("HAUS_LISTEN", "127.0.0.1:8650")
	log.Printf("haus %s: listening on %s as %s, laib %s, migration bridge %v, home assistant %v, haushalt %d tools, uploads %q, pages only behind the proxy secret", Version, addr, pages.House.Display, ops.URL, bridge, cfg.HA != nil, len(haushalt.Tools(hh)), cfg.UploadDir)
	log.Fatal((&http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}).ListenAndServe())
}
