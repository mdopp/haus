// Command haus is the family's surface for the Laib box (laib ADR 0014):
// pages for people, the API the Haus app's widgets speak (laib ADR 0022).
// The Box keys come from Laib, the house from Home Assistant, the tools from
// the Haushalt source.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"
	_ "time/tzdata" // the image is scratch: TZ needs the zone database inside the binary

	"github.com/mdopp/haus/internal/cards"
	"github.com/mdopp/haus/internal/devices"
	"github.com/mdopp/haus/internal/favoriten"
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

// site is what the routes are built from; main reads it from the environment.
type site struct {
	napi        napi.Config
	hh          haushalt.Config
	store       *devices.Store
	ops         *laib.Client
	proxySecret string
	laibURL     string
	publicURL   string
	tickets     *werkstatt.Tickets // haus#15: the Werkstatt card; nil, no card
	favorites   *favoriten.Store   // haus#12, haus#35: each person's things on Start, pinned with ☆
}

// routes is Haus's whole surface: /napi for the app, /mcp/haushalt for the
// Bote, and the pages behind the proxy secret.
func routes(s site) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/napi/", napi.Handler(s.napi))
	// the Bote's bearer, not the proxy secret: the Bote calls the host port from its pod
	// (laib ADR 0012 §4; the template publishes 8650 lan:false, the firewall keeps
	// the LAN out). Not through Caddy: the route has no /mcp/ bypass.
	mux.Handle("/mcp/haushalt", haushalt.Handler(s.hh))
	pagesMux := http.NewServeMux()
	// the family's pages (haus#12): each card only from a backend that is set; a
	// page whose backend is not set lands on Start
	family := pages.StartConfig{Laib: s.ops, LaibURL: s.laibURL, HA: s.napi.HA, Calendar: s.hh.Calendar, Contacts: s.hh.Contacts,
		Notes: s.hh.Notes, Library: s.hh.Library, Judge: s.hh.Judge, Docs: s.hh.Documents, Loc: s.hh.Loc, Favorites: s.favorites}
	pagesMux.HandleFunc("/start", pages.Start(family))
	pagesMux.HandleFunc("/raeume", pages.Raeume(family))
	pagesMux.HandleFunc("/medien", pages.Medien(family))
	pagesMux.HandleFunc("/kalender", pages.Kalender(family))
	pagesMux.HandleFunc("/notizen", pages.Notizen(family))
	// each thing's and each tool item's own page, which the widgets name (haus#28)
	pagesMux.HandleFunc(cards.ThingPages, pages.Ding(family))
	pagesMux.HandleFunc(cards.ReminderPages, pages.Erinnerung(family))
	pagesMux.HandleFunc(cards.ContactPages, pages.Kontakt(family))
	pagesMux.HandleFunc(cards.DocumentPages, pages.Dokument(family))
	pagesMux.HandleFunc(cards.NotePages, pages.Notizen(family))   // a note's own page
	pagesMux.HandleFunc(cards.SwitchPath, pages.Schalten(family)) // a page's switch, the widgets' rule (haus#35)
	pagesMux.HandleFunc("/schrift/", pages.Font())                // Geist, from Haus itself (haus#35)
	pagesMux.HandleFunc("/box", pages.BoxWith(s.ops, s.laibURL, s.tickets))
	pagesMux.HandleFunc("/box/geraete", pages.Geraete(s.store, s.publicURL))
	pagesMux.HandleFunc("/pair-device", pages.PairDevice(s.store)) // the app's own pairing path (haus#6)
	pagesMux.HandleFunc("/box/", pages.BoxWith(s.ops, s.laibURL, s.tickets))
	pagesMux.HandleFunc("/persona/icon.svg", pages.Icon())
	pagesMux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	pagesMux.HandleFunc("/", pages.ToStart()) // "/" and every path not served land on Start
	mux.Handle("/", pages.RequireProxy(s.proxySecret, pagesMux))
	return mux
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
	dataDir := env("HAUS_DATA", "/data")
	store, err := devices.Open(dataDir)
	if err != nil {
		log.Fatalf("haus: devices: %v", err)
	}
	pages.House = persona.Load(env("HAUS_PERSONA_DIR", "/persona"), os.Getenv("HAUS_PERSONA")) // laib ADR 0020; plain Haus without it
	cfg := napi.Config{Laib: ops, House: pages.House}
	cfg.Whoami = func(b string) (string, bool) {
		d, ok := store.Lookup(b)
		return d.UID, ok
	}
	// only phones paired through Haus; nothing old is carried (laib ADR 0022 §4)
	cfg.Validate = func(_ context.Context, b string) bool { _, ok := store.Lookup(b); return ok }
	if cfg.HA, err = ha.FromEnv(); err != nil { // the house paths and the event stream from HA; unset, 404
		log.Fatalf("haus: %v", err)
	}
	if cfg.Energy, err = napi.EnergyFromEnv(); err != nil {
		log.Fatalf("haus: %v", err)
	}
	// laib ADR 0017: the Haushalt MCP source; a tool only when its backend is set
	hh, err := haushalt.FromEnv()
	if err != nil {
		log.Fatalf("haus: %v", err)
	}
	hh.Version = Version
	cfg.Haushalt = hh // haus#10: the tool widgets from the same backends; none set, 404
	// haus#10: the camera capture into the share's Paperless consume folder; unset, 404
	if cfg.UploadDir = os.Getenv("HAUS_UPLOAD_DIR"); cfg.UploadDir != "" {
		if fi, err := os.Stat(cfg.UploadDir); err != nil || !fi.IsDir() {
			log.Printf("haus: HAUS_UPLOAD_DIR %s is not a folder (yet): uploads answer 503 until it is", cfg.UploadDir)
		} else if err := napi.SweepUploads(cfg.UploadDir); err != nil { // parts a stopped Haus left staged
			log.Printf("haus: HAUS_UPLOAD_DIR: sweeping staged parts: %v", err)
		}
	}
	laibURL := env("LAIB_PUBLIC_URL", "https://laib."+env("LAIB_DOMAIN", "example"))
	mux := routes(site{napi: cfg, hh: hh, store: store, ops: ops, proxySecret: proxySecret, laibURL: laibURL,
		publicURL: os.Getenv("HAUS_PUBLIC_URL"), tickets: werkstatt.FromEnv(), favorites: favoriten.Open(dataDir)})
	addr := env("HAUS_LISTEN", "127.0.0.1:8650")
	log.Printf("haus %s: listening on %s as %s, laib %s, home assistant %v, haushalt %d tools, memory library %v (tidy-up %v), uploads %q, pages only behind the proxy secret", Version, addr, pages.House.Display, ops.URL, cfg.HA != nil, len(haushalt.Tools(hh)), hh.Library != nil, hh.Judge != nil, cfg.UploadDir)
	log.Fatal((&http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}).ListenAndServe())
}
