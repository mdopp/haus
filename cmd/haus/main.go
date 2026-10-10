// Command haus is the family's surface for the Laib box (laib ADR 0014):
// pages for people, the companion API for the Android widgets. First cut
// (laib#28): the Box page and /napi/servicebay/* from Laib; every other
// /napi path still comes from Solaris, which also validates device tokens.
package main

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

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
		log.Fatal("haus: LAIB_TOKEN is required (laib token mint ops --note haus)")
	}
	var human *laib.Client
	if t := os.Getenv("LAIB_HUMAN_TOKEN"); t != "" {
		human = &laib.Client{URL: ops.URL, Token: t}
	}
	cfg := napi.Config{Laib: ops, Human: human}
	if s := os.Getenv("SOLARIS_URL"); s != "" {
		u, err := url.Parse(s)
		if err != nil {
			log.Fatalf("haus: SOLARIS_URL: %v", err)
		}
		cfg.Upstream = u
		cfg.Validate = napi.SolarisValidator(u, nil)
	}
	if t := os.Getenv("HAUS_DEVICE_TOKENS"); t != "" {
		// a trial box without Solaris: fixed device tokens from the environment
		static := napi.StaticValidator(strings.Split(t, ","))
		if prev := cfg.Validate; prev != nil {
			cfg.Validate = func(ctx context.Context, b string) bool { return static(ctx, b) || prev(ctx, b) }
		} else {
			cfg.Validate = static
		}
	}
	mux := http.NewServeMux()
	mux.Handle("/napi/", napi.Handler(cfg))
	mux.HandleFunc("/box", pages.Box(ops, human))
	mux.HandleFunc("/box/", pages.Box(ops, human))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/box", http.StatusFound) })
	addr := env("HAUS_LISTEN", "127.0.0.1:8650")
	log.Printf("haus %s: listening on %s, laib %s, solaris %s", Version, addr, ops.URL, os.Getenv("SOLARIS_URL"))
	log.Fatal((&http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}).ListenAndServe())
}
