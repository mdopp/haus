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

	"github.com/mdopp/haus/internal/devices"
	"github.com/mdopp/haus/internal/laib"
	"github.com/mdopp/haus/internal/napi"
	"github.com/mdopp/haus/internal/pages"
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
	cfg := napi.Config{Laib: ops}
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
	cfg.Validate = napi.Chain(own, paired) // phones paired before Haus keep working through the bridge
	mux := http.NewServeMux()
	mux.Handle("/napi/", napi.Handler(cfg))
	pagesMux := http.NewServeMux()
	pagesMux.HandleFunc("/box", pages.Box(ops, env("LAIB_PUBLIC_URL", "https://laib."+env("LAIB_DOMAIN", "example"))))
	pagesMux.HandleFunc("/box/geraete", pages.Geraete(store, os.Getenv("HAUS_PUBLIC_URL")))
	pagesMux.HandleFunc("/box/", pages.Box(ops, env("LAIB_PUBLIC_URL", "https://laib."+env("LAIB_DOMAIN", "example"))))
	pagesMux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	pagesMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/box", http.StatusFound) })
	mux.Handle("/", pages.RequireProxy(proxySecret, pagesMux))
	addr := env("HAUS_LISTEN", "127.0.0.1:8650")
	log.Printf("haus %s: listening on %s, laib %s, migration bridge %v, pages only behind the proxy secret", Version, addr, ops.URL, bridge)
	log.Fatal((&http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}).ListenAndServe())
}
