// Command haus is the family's surface for the Laib box (laib ADR 0014):
// pages for people, the API the Haus app's widgets speak (laib ADR 0022).
// The Box keys come from Laib, the house from Home Assistant, the tools from
// the Haushalt source.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
	_ "time/tzdata" // the image is scratch: TZ needs the zone database inside the binary

	"github.com/mdopp/haus/internal/abs"
	"github.com/mdopp/haus/internal/ansicht"
	"github.com/mdopp/haus/internal/bote"
	"github.com/mdopp/haus/internal/caldav"
	"github.com/mdopp/haus/internal/cards"
	"github.com/mdopp/haus/internal/devices"
	"github.com/mdopp/haus/internal/discogs"
	"github.com/mdopp/haus/internal/einkauf"
	"github.com/mdopp/haus/internal/favoriten"
	"github.com/mdopp/haus/internal/ha"
	"github.com/mdopp/haus/internal/haushalt"
	"github.com/mdopp/haus/internal/jellyfin"
	"github.com/mdopp/haus/internal/laib"
	"github.com/mdopp/haus/internal/musicbrainz"
	"github.com/mdopp/haus/internal/napi"
	"github.com/mdopp/haus/internal/pages"
	"github.com/mdopp/haus/internal/persona"
	"github.com/mdopp/haus/internal/podcasts"
	"github.com/mdopp/haus/internal/samba"
	"github.com/mdopp/haus/internal/schalter"
	"github.com/mdopp/haus/internal/usage"
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
	bote        *bote.Client       // haus#12: the Sprechen field's Bote; nil, no field
	samba       *samba.Dir         // haus#52: the network drive's passwords; nil, no page
	sambaHost   string
	usage       *usage.Counter  // haus#50: what the family uses, anonymously; nil, nothing counted
	voice       *usage.Voice    // haus#50: the voice control's runs from HA; nil, not counted
	welcome     bool            // haus#56: Start sends a device's first visit to /willkommen
	stt         pages.STT       // haus#63: the Sprechen field's microphone through HA's speech-to-text; no provider, no microphone
	haPublic    string          // haus#64: Home Assistant's public address, Geräte's "In Home Assistant öffnen"; empty, no link
	inverted    map[string]bool // haus#64: covers wired the wrong way round, shown and set turned over
	radicale    string          // haus#66: Radicale's public address, its web page and the phones' CalDAV addresses; empty, none
	views       *ansicht.Store  // haus#66: each person's calendar colours and hidden calendars
	// haus#68: the Dokumente page's mail address, Paperless's public address and whether it is LAN-only
	docsMail, paperlessPublic string
	paperlessLAN              bool
	podcasts                  *podcasts.Service // haus#123: the family's podcasts in Audiobookshelf; nil, no page
}

// routes is Haus's whole surface: /napi for the app, /mcp/haushalt for the
// Bote, and the pages behind the proxy secret.
func routes(s site) http.Handler {
	mux := http.NewServeMux()
	s.napi.Usage = s.usage
	mux.Handle("/napi/", napi.Handler(s.napi))
	// the Bote's bearer, not the proxy secret: the Bote calls the host port from its pod
	// (laib ADR 0012 §4; the template publishes 8650 lan:false, the firewall keeps
	// the LAN out). Not through Caddy: the route has no /mcp/ bypass.
	mux.Handle("/mcp/haushalt", haushalt.Handler(s.hh))
	pagesMux := http.NewServeMux()
	// the family's pages (haus#12): each card only from a backend that is set; a
	// page whose backend is not set says what is missing (laib#418)
	family := pages.StartConfig{Podcasts: s.podcasts, Laib: s.ops, Witness: s.napi.Witness, LaibURL: s.laibURL, HA: s.napi.HA, Energy: s.napi.Energy, Calendar: s.hh.Calendar, Contacts: s.hh.Contacts,
		Notes: s.hh.Notes, Library: s.hh.Library, Judge: s.hh.Judge, Docs: s.hh.Documents, Loc: s.hh.Loc, Favorites: s.favorites, Bote: s.bote,
		Samba: s.samba, SambaHost: s.sambaHost, Usage: s.usage, PublicURL: s.publicURL, Welcome: s.welcome, STT: s.stt,
		HAPublicURL: s.haPublic, Inverted: s.inverted, RadicalePublicURL: s.radicale, Views: s.views,
		DocumentsMail: s.docsMail, PaperlessPublicURL: s.paperlessPublic, PaperlessLANOnly: s.paperlessLAN}
	// the lists, the music wish list and the shelf, before any page takes its copy of family
	family.Einkauf, family.Wuensche = s.hh.Einkauf, s.hh.Wuensche
	if s.hh.Wuensche != nil {
		family.Regal = s.hh.Wuensche.Regal
	}
	family.MusicInbox = os.Getenv("HAUS_MUSIC_INBOX")
	if fi, err := os.Stat(musicInboxMount); family.MusicInbox == "" && err == nil && fi.IsDir() {
		family.MusicInbox = musicInboxMount // where laib's haus template mounts it (laib#639)
	}
	if family.MusicInbox != "" { // haus#107: the music inbox beets sorts in
		if fi, err := os.Stat(family.MusicInbox); err != nil || !fi.IsDir() {
			log.Printf("haus: HAUS_MUSIC_INBOX %s is not a folder (yet): uploads answer 503 until it is", family.MusicInbox)
		}
		pagesMux.HandleFunc(pages.MusikPath, pages.Musik(family))
		pagesMux.HandleFunc(pages.MusikTeilPath, pages.Musik(family))
		pagesMux.HandleFunc(pages.MusikStandPath, pages.Musik(family)) // haus#113: the import's line
	}
	if s.napi.UploadDir != "" { // haus#68: the page's way into Paperless's inbox, the app's folder
		family.Ablage = napi.NewUploads(s.napi.UploadDir)
	}
	start := pages.Start(family) // one: the page and its box line share laib's status (haus#67)
	pagesMux.HandleFunc("/start", start)
	if s.podcasts != nil {
		pagesMux.HandleFunc(pages.PodcastsPath, pages.Podcasts(family)) // haus#123
	}
	live := &pages.Live{HA: family.HA} // the pages' push: Home Assistant's changes and Haus's own
	pagesMux.Handle(pages.LivePath, live)
	if s.hh.Einkauf != nil { // haus#118: the list, ticked on one phone, fetched again on the others
		s.hh.Einkauf.Changed = func(r caldav.ListRef) { live.Tell(pages.ListeLive(r)) }
		if family.Regal != nil {
			pagesMux.HandleFunc(pages.PlattePath, pages.Platte(family))
			pagesMux.HandleFunc(pages.PlatteBildPath, pages.Platte(family))
		}
		pagesMux.HandleFunc(pages.EinkaufPath, pages.Einkauf(family))
		pagesMux.HandleFunc(pages.EinkaufRezeptePath, pages.Einkauf(family))
	}
	pagesMux.HandleFunc(pages.StartBoxPath, start)
	pagesMux.HandleFunc("/mehr", pages.Mehr(family)) // the bar's fourth entry, with Abmelden (haus#67)
	pagesMux.HandleFunc("/raeume", pages.Raeume(family))
	pagesMux.HandleFunc("/energie", pages.Energie(family))
	pagesMux.HandleFunc("/energie/", pages.Energie(family)) // a path it does not serve: 404 and the way back
	pagesMux.HandleFunc("/medien", pages.Medien(family))
	pagesMux.HandleFunc("/medien/", pages.Medien(family)) // a path it does not serve: 404 and the way back
	pagesMux.HandleFunc("/kalender", pages.Kalender(family))
	pagesMux.HandleFunc("/kalender/verwalten", pages.KalenderVerwalten(family))     // Deine Kalender: hinzufügen, Farbe, Trennen (haus#66)
	pagesMux.HandleFunc("/kalender/hinzufuegen", pages.KalenderHinzufuegen(family)) // Google, ein Abo, ein neuer Kalender
	pagesMux.HandleFunc("/kalender/aendern", pages.KalenderAendern(family))         // one calendar's buttons
	pagesMux.HandleFunc("/notizen", pages.Notizen(family))
	// each thing's and each tool item's own page, which the widgets name (haus#28)
	pagesMux.HandleFunc(cards.ThingPages, pages.Ding(family))
	pagesMux.HandleFunc(cards.ReminderPages, pages.Erinnerung(family))
	pagesMux.HandleFunc(cards.ContactPages, pages.Kontakt(family))
	pagesMux.HandleFunc(pages.DokumentePath, pages.Dokumente(family)) // the ways into the archive (haus#68)
	pagesMux.HandleFunc(pages.ScannenPath, pages.Scannen(family))     // the Dokumente page's scan orders to Laib (haus#76, laib#568)
	pagesMux.HandleFunc(cards.DocumentPages, pages.Dokument(family))
	pagesMux.HandleFunc(cards.NotePages, pages.Notizen(family))                   // a note's own page
	pagesMux.HandleFunc(cards.SwitchPath, pages.Schalten(family))                 // a page's switch, the widgets' rule (haus#35)
	pagesMux.HandleFunc("/sprechen", pages.Sprechen(family))                      // the Sprechen field to the Bote, and its reply (haus#12)
	pagesMux.HandleFunc(pages.HoerenPath, pages.Hoeren(family))                   // the field's microphone, HA's speech-to-text (haus#63)
	pagesMux.HandleFunc("/netzlaufwerk", pages.Netzlaufwerk(family))              // each person's network-drive password (haus#52)
	pagesMux.HandleFunc("/willkommen", pages.Willkommen(family))                  // a new person's apps and how to set them up (haus#56)
	pagesMux.HandleFunc("/adresse", pages.Adresse(family))                        // the household's address, admins only (laib#561)
	pagesMux.HandleFunc("/adresse/vorschlagen", pages.AdresseVorschlagen(family)) // its form, a proposal to Laib
	pagesMux.HandleFunc("/abmelden", pages.Abmelden(s.publicURL))                 // Authelia's logout, back to Haus (haus#61)
	pagesMux.HandleFunc("/schrift/", pages.Font())                                // Geist, from Haus itself (haus#35)
	boxCfg := pages.BoxConfig{Laib: s.ops, LaibURL: s.laibURL, Tickets: s.tickets, Family: family, Voice: s.voice}
	box := pages.BoxPage(boxCfg)
	pagesMux.HandleFunc("/box", box)
	pagesMux.HandleFunc("/box/einladen", pages.Einladen(boxCfg))    // the card "Person einladen", admins only (haus#56)
	pagesMux.HandleFunc(pages.AufgabenPath, pages.Aufgaben(boxCfg)) // the Box-Aufgaben: Erledigt, Später (haus#115)
	pagesMux.HandleFunc("/box/geraete", pages.Geraete(s.store, s.publicURL, family))
	pagesMux.HandleFunc("/pair-device", pages.PairDevice(s.store, family)) // the app's own pairing path (haus#6)
	pagesMux.HandleFunc("/box/", box)
	pagesMux.HandleFunc("/persona/icon.svg", pages.Icon())
	pagesMux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	pagesMux.HandleFunc("/", pages.Elsewhere(family)) // "/" is Start; every path not served is a 404 with the way there
	// counted behind the proxy secret: only what Caddy let through is a page view (haus#50)
	mux.Handle("/", pages.RequireProxy(s.proxySecret, pages.Counted(s.usage, pagesMux)))
	return mux
}

// musicInboxMount is where laib's haus template mounts the music inbox
// (laib#639); Haus takes it when HAUS_MUSIC_INBOX names none.
const musicInboxMount = "/musik-eingang"

// episodeEvery is how often Haus looks for new podcast episodes.
const episodeEvery = 15 * time.Minute

// tellNewEpisodes looks for new episodes every episodeEvery and tells each
// to who subscribed, through Laib's tell (laib#676) — only once Laib has
// the verb, so no episode is taken as told before anyone could be.
// tickWishes looks every wishEvery whether a wish on the music wish list
// has arrived in the music (with Jellyfin), moves it on and tells who
// wished it; and looks up again where a wish is that MusicBrainz did not
// answer for.
func tickWishes(ctx context.Context, w *einkauf.Wuensche, l *laib.Client) {
	t := time.NewTicker(wishEvery)
	defer t.Stop()
	for {
		if err := w.Nachsuchen(ctx); err != nil {
			log.Printf("haus: musikwünsche: looking up again: %v", err)
		}
		done, err := w.Pruefen(ctx)
		if err != nil {
			log.Printf("haus: musikwünsche: %v", err)
		}
		for _, d := range done {
			log.Printf("haus: musikwunsch arrived for %q", d.Person)
			if d.Person == "" {
				continue
			}
			if l == nil {
				continue
			}
			if ok, _ := l.Takes(ctx, "tell", "person"); ok {
				if err := l.Tell(ctx, d.Person, d.Satz); err != nil {
					log.Printf("haus: musikwunsch: telling %s: %v", d.Person, err)
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// wishEvery is how often the music is asked for the wishes.
const wishEvery = 15 * time.Minute

func tellNewEpisodes(ctx context.Context, pods *podcasts.Service, l *laib.Client) {
	t := time.NewTicker(episodeEvery)
	defer t.Stop()
	said := false
	for {
		if err := pods.Nachladen(ctx); err != nil { // a subscription with no episode yet
			log.Printf("haus: podcasts: fetching the first episodes: %v", err)
		}
		if ok, _ := l.Takes(ctx, "tell", "person"); ok {
			msgs, err := pods.Neue(ctx)
			if err != nil {
				log.Printf("haus: podcasts: looking for new episodes: %v", err)
			}
			for _, m := range msgs {
				if err := l.Tell(ctx, m.Person, m.Text); err != nil {
					log.Printf("haus: podcasts: telling %s: %v", m.Person, err)
				}
			}
		} else if !said {
			log.Printf("haus: podcasts: laib has no tell yet (laib#676): new episodes are not told")
			said = true
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// ensureHaushalt makes the household's calendar, address book and shopping list in
// Radicale, trying again with a growing pause while Radicale does not
// answer; done once they are there.
func ensureHaushalt(ctx context.Context, c *caldav.Client) {
	wait := 10 * time.Second
	for {
		tctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := errors.Join(c.EnsureHaushalt(tctx), c.EnsureList(tctx, caldav.Einkauf, einkauf.Name), // and the shopping list (haus#118)
			c.EnsureList(tctx, einkauf.MusikListe, einkauf.MusikName)) // and the music wish list
		cancel()
		if err == nil {
			log.Printf("haus: Haushalt: calendar, address book and shopping list are there")
			return
		}
		log.Printf("haus: Haushalt: making the calendar and address book: %v (again in %v)", err, wait)
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = min(wait*2, 10*time.Minute)
	}
}

func main() {
	if len(os.Args) > 1 { // a one-shot command, not the server
		os.Exit(command(context.Background(), os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
	}
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
	cfg := napi.Config{Laib: ops, House: pages.House, Witness: &schalter.Witness{Laib: ops}}   // who switched what (haus#89)
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
	var pods *podcasts.Service
	if c := abs.FromEnv(); c != nil { // haus#123: HAUS_ABS_URL and HAUS_ABS_TOKEN from laib (laib#676)
		pods = podcasts.Open(c, dataDir)
		hh.Podcasts = pods
	}
	if hh.Calendar != nil {
		hh.Einkauf = einkauf.Open(hh.Calendar) // haus#118: the household's shopping list, haus/einkauf
		// a shared song goes on haus/liste-musik, with its stages and where it is (MusicBrainz)
		hh.Wuensche = &einkauf.Wuensche{Listen: hh.Einkauf, Dir: dataDir, MB: musicbrainz.New(hh.Fetcher)}
		if jf := jellyfin.FromEnv(); jf != nil { // laib#695: "hast du schon", and arrived wishes ticked off
			hh.Wuensche.Bib = jf
		}
		if dc := discogs.FromEnv(); dc != nil { // laib#696: the records on the shelf, the wantlist, Platte erfassen
			hh.Wuensche.Regal = &discogs.Regal{C: dc}
		}
	}
	cfg.Haushalt = hh // haus#10: the tool widgets from the same backends; none set, 404
	// haus#10: the camera capture into the share's Paperless consume folder; unset, 404
	if cfg.UploadDir = os.Getenv("HAUS_UPLOAD_DIR"); cfg.UploadDir != "" {
		if fi, err := os.Stat(cfg.UploadDir); err != nil || !fi.IsDir() {
			log.Printf("haus: HAUS_UPLOAD_DIR %s is not a folder (yet): uploads answer 503 until it is", cfg.UploadDir)
		} else if err := napi.SweepUploads(cfg.UploadDir); err != nil { // parts a stopped Haus left staged
			log.Printf("haus: HAUS_UPLOAD_DIR: sweeping staged parts: %v", err)
		}
	}
	haPublic := pages.AppURL(pages.OnDomain(os.Getenv("HAUS_HA_PUBLIC_URL"), os.Getenv("HAUS_PUBLIC_URL"))) // haus#64
	if haPublic == "" && os.Getenv("HAUS_HA_PUBLIC_URL") != "" {
		log.Printf("haus: HAUS_HA_PUBLIC_URL %q is not an http(s) address: no link to Home Assistant", os.Getenv("HAUS_HA_PUBLIC_URL"))
	}
	radicalePublic := pages.AppURL(pages.OnDomain(os.Getenv("HAUS_RADICALE_PUBLIC_URL"), os.Getenv("HAUS_PUBLIC_URL"))) // haus#66
	if radicalePublic == "" && os.Getenv("HAUS_RADICALE_PUBLIC_URL") != "" {
		log.Printf("haus: HAUS_RADICALE_PUBLIC_URL %q is not an http(s) address: no link to Radicale, no addresses for the phone", os.Getenv("HAUS_RADICALE_PUBLIC_URL"))
	}
	laibURL := env("LAIB_PUBLIC_URL", "https://laib."+env("LAIB_DOMAIN", "example"))
	docsMail := pages.MailAddress(os.Getenv("HAUS_DOCUMENTS_MAIL")) // haus#68
	if docsMail == "" && os.Getenv("HAUS_DOCUMENTS_MAIL") != "" {
		log.Printf("haus: HAUS_DOCUMENTS_MAIL %q is not one plain address: no mail button", os.Getenv("HAUS_DOCUMENTS_MAIL"))
	}
	paperlessPublic := pages.AppURL(pages.OnDomain(os.Getenv("HAUS_PAPERLESS_PUBLIC_URL"), os.Getenv("HAUS_PUBLIC_URL")))
	paperlessLAN, _ := strconv.ParseBool(os.Getenv("HAUS_PAPERLESS_LAN_ONLY"))
	b := bote.FromEnv() // the Sprechen field (haus#12): with the Bote's key only (laib#215)
	// haus#50: counted for usage-metrics, flushed every five minutes; the voice
	// from HA's Assist runs every minute, as HA keeps only the last ten
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	if hh.Calendar != nil { // the household's calendar and address book there from the start (owner 2026-10-24)
		go ensureHaushalt(ctx, hh.Calendar)
		go tickWishes(ctx, hh.Wuensche, ops)
	}
	if pods != nil && ops != nil {
		go tellNewEpisodes(ctx, pods, ops) // haus#123: a new episode to who subscribed
	}
	counter := usage.FromEnv(os.Getenv)
	var voice *usage.Voice
	if counter != nil && cfg.HA != nil {
		voice = &usage.Voice{HA: cfg.HA, Counter: counter, Since: time.Now(), Log: log.Printf}
	}
	// the counter stops only once the server has: a page is counted after
	// it is answered, so the last flush waits for the requests under way (#92)
	counting, stopCounting := context.WithCancel(context.Background())
	defer stopCounting()
	flushed := make(chan struct{})
	go func() { counter.Run(counting, 5*time.Minute); close(flushed) }()
	go voice.Run(ctx, time.Minute)
	mux := routes(site{napi: cfg, hh: hh, store: store, ops: ops, proxySecret: proxySecret, laibURL: laibURL,
		publicURL: os.Getenv("HAUS_PUBLIC_URL"), tickets: werkstatt.FromEnv(), favorites: favoriten.Open(dataDir), bote: b, podcasts: pods,
		samba: samba.FromEnv(), sambaHost: os.Getenv("HAUS_SAMBA_HOST"),
		usage: counter, voice: voice, welcome: true,
		stt:      pages.STT{Provider: os.Getenv("HAUS_STT_PROVIDER"), Language: env("HAUS_STT_LANGUAGE", "de")},
		haPublic: haPublic, inverted: pages.InvertedFromList(os.Getenv("HAUS_COVER_INVERTED")),
		radicale: radicalePublic, views: ansicht.Open(dataDir),
		docsMail: docsMail, paperlessPublic: paperlessPublic, paperlessLAN: paperlessLAN})
	addr := env("HAUS_LISTEN", "127.0.0.1:8650")
	log.Printf("haus %s: listening on %s as %s, laib %s, home assistant %v, haushalt %d tools, memory library %v (tidy-up %v), uploads %q, the Bote %v, counting %v (voice %v), pages only behind the proxy secret", Version, addr, pages.House.Display, ops.URL, cfg.HA != nil, len(haushalt.Tools(hh)), hh.Library != nil, hh.Judge != nil, cfg.UploadDir, b != nil, counter != nil, voice != nil)
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	drained := make(chan struct{})
	go func() {
		<-ctx.Done()
		shut, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shut) // waits for the requests under way
		close(drained)
	}()
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatal(err)
	}
	<-drained
	stop()
	stopCounting()
	<-flushed // the last counts go out before Haus does
}
