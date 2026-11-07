// Command homes is the family's surface for the Laib box (laib ADR 0014):
// pages for people, the API the Homes app's widgets speak (laib ADR 0022).
// The Box keys come from Laib, the house from Home Assistant, the tools from
// the Haushalt source.
package main

import (
	"context"
	"errors"
	"github.com/mdopp/homes/internal/papierkorb"
	"log"
	"maps"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata" // the image is scratch: TZ needs the zone database inside the binary

	"github.com/mdopp/homes/internal/abs"
	"github.com/mdopp/homes/internal/ansicht"
	"github.com/mdopp/homes/internal/assistent"
	"github.com/mdopp/homes/internal/bote"
	"github.com/mdopp/homes/internal/caldav"
	"github.com/mdopp/homes/internal/devices"
	"github.com/mdopp/homes/internal/discogs"
	"github.com/mdopp/homes/internal/einkauf"
	"github.com/mdopp/homes/internal/einrichten"
	"github.com/mdopp/homes/internal/favoriten"
	"github.com/mdopp/homes/internal/gesehen"
	"github.com/mdopp/homes/internal/gesichter"
	"github.com/mdopp/homes/internal/ha"
	"github.com/mdopp/homes/internal/haushalt"
	"github.com/mdopp/homes/internal/jellyfin"
	"github.com/mdopp/homes/internal/laib"
	"github.com/mdopp/homes/internal/modul"
	"github.com/mdopp/homes/internal/musicbrainz"
	"github.com/mdopp/homes/internal/napi"
	"github.com/mdopp/homes/internal/pages"
	"github.com/mdopp/homes/internal/podcasts"
	"github.com/mdopp/homes/internal/reitti"
	"github.com/mdopp/homes/internal/samba"
	"github.com/mdopp/homes/internal/schalter"
	"github.com/mdopp/homes/internal/usage"
	"github.com/mdopp/homes/internal/werkstatt"
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
	dataDir     string          // Homes' data folder (HOMES_DATA): the Papierkorb (haus#174)
	// haus#68: the Dokumente page's mail address, Paperless's public address and whether it is LAN-only
	docsMail, paperlessPublic string
	paperlessLAN              bool
	podcasts                  *podcasts.Service            // haus#123: the family's podcasts in Audiobookshelf; nil, no page
	gesehen                   *gesehen.Service             // haus#152: what each person watched and heard; nil, no page
	musik                     string                       // haus#107: the music inbox's folder (musicInbox); "", no Musik page
	boteSignal                string                       // haus#175: the house's Signal number (HOMES_BOTE_SIGNAL), sprechen's setup; "", none
	domain                    string                       // the box's domain, HOMES_PUBLIC_URL's after homes.: the services' addresses
	dienste                   map[string]bool              // the services Laib runs (dienstePflegen): the link modules; nil, Laib has not said yet
	archive                   map[string]napi.ArchivIntern // each person's message archive (HOMES_ARCHIV_INTERN): a contact's Nachrichten
	gesichter                 *gesichter.Gesichter         // Immich for the contacts (HOMES_IMMICH_*): Fotos, faces, a person per contact; nil, none
}

// routes is Homes' whole surface: /napi for the app, /mcp/haushalt for the
// Bote, and the pages behind the proxy secret.
func routes(s site) http.Handler {
	mux := http.NewServeMux()
	s.napi.Usage, s.napi.MusicInbox = s.usage, s.musik // the app's songs and ZIPs go where the Musik page looks (haus#107)
	// a contact's links in the phones' cards, the same as on its page (owner 2026-11-05)
	karte := pages.StartConfig{PublicURL: s.publicURL, PaperlessPublicURL: s.paperlessPublic, Gesichter: s.gesichter, Module: s.napi.Module}
	karte.Archive, karte.ArchivURL = archivOf(s)
	s.napi.Haushalt.Karte = pages.KontaktKarte(karte)
	mux.Handle("/napi/", napi.Handler(s.napi))
	// the Bote's bearer, not the proxy secret: the Bote calls the host port from its pod
	// (laib ADR 0012 §4; the template publishes 8650 lan:false, the firewall keeps
	// the LAN out). Not through Caddy: the route has no /mcp/ bypass.
	mux.Handle("/mcp/haushalt", haushalt.Handler(s.hh))
	// counted behind the proxy secret: only what Caddy let through is a page view (haus#50)
	mux.Handle("/", pages.RequireProxy(s.proxySecret, pages.Counted(s.usage, familyMux(s))))
	return legacyHost(s.publicURL, mux) // haus.<domain>'s pages to homes.<domain> (laib#1021)
}

// familyMux is the family's pages, behind the proxy secret.
// familyOf is what the family's pages are built from: the backends the
// site has, and what Homes keeps itself.
func familyOf(s site) pages.StartConfig {
	// the family's pages (haus#12): each card only from a backend that is set; a
	// page whose backend is not set says what is missing (laib#418)
	family := pages.StartConfig{Podcasts: s.podcasts, Gesehen: s.gesehen, Laib: s.ops, Witness: s.napi.Witness, LaibURL: s.laibURL, HA: s.napi.HA, Energy: s.napi.Energy, Calendar: s.hh.Calendar, Contacts: s.hh.Contacts,
		Notes: s.hh.Notes, Library: s.hh.Library, Judge: s.hh.Judge, Docs: s.hh.Documents, Loc: s.hh.Loc, Favorites: s.favorites, Bote: s.bote,
		Samba: s.samba, SambaHost: s.sambaHost, Usage: s.usage, PublicURL: s.publicURL, Welcome: s.welcome, STT: s.stt,
		HAPublicURL: s.haPublic, Inverted: s.inverted, RadicalePublicURL: s.radicale, Views: s.views,
		DocumentsMail: s.docsMail, PaperlessPublicURL: s.paperlessPublic, PaperlessLANOnly: s.paperlessLAN, Sicherung: s.napi.Sicherung,
		Reitti: s.napi.Reitti, Module: s.napi.Module, Einrichtung: s.napi.Einrichtung, PhoneCountry: s.hh.PhoneCountry, Gesichter: s.gesichter}
	family.Archive, family.ArchivURL = archivOf(s)
	// the lists, the music wish list and the shelf, before any page takes its copy of family
	family.Einkauf, family.Wuensche = s.hh.Einkauf, s.hh.Wuensche
	if s.hh.Wuensche != nil {
		family.Discogs = s.hh.Wuensche.Regale
	}
	family.OhneEnergie = s.napi.OhneEnergie
	family.Papierkorb = &papierkorb.Korb{Dir: s.dataDir} // haus#174: family deletes, for 30 days
	family.MusicInbox = s.musik
	if family.MusicInbox != "" { // haus#107: the music inbox beets sorts in
		if fi, err := os.Stat(family.MusicInbox); err != nil || !fi.IsDir() {
			log.Printf("homes: HOMES_MUSIC_INBOX %s is not a folder (yet): uploads answer 503 until it is", family.MusicInbox)
		}
	}
	if s.napi.UploadDir != "" { // haus#68: the page's way into Paperless's inbox, the app's folder
		family.Ablage = napi.NewUploads(s.napi.UploadDir)
	}
	return family
}

// archivOf is who has a message archive and where it opens: archiv.<domain>,
// one name for every person's (laib's archiv template); none with sicherung off.
func archivOf(s site) (map[string]bool, string) {
	if s.napi.Sicherung == nil || len(s.archive) == 0 {
		return nil, ""
	}
	wer := map[string]bool{}
	for person := range s.archive {
		wer[person] = true
	}
	return wer, einrichten.Web("archiv", s.domain)
}

func familyMux(s site) *http.ServeMux {
	pagesMux := http.NewServeMux()
	family := familyOf(s)
	start := pages.Start(family) // one: the page and its Braucht dich share laib's status (haus#67)
	pagesMux.HandleFunc("/start", start)
	live := &pages.Live{HA: family.HA} // the pages' push: Home Assistant's changes and Homes' own
	pagesMux.Handle(pages.LivePath, live)
	if s.gesehen != nil { // haus#152: the film and book wish list, told like the other lists
		s.gesehen.Changed = func(r caldav.ListRef) { live.Tell(pages.ListeLive(r)) }
	}
	if l := s.hh.Einkauf; l != nil || s.hh.Wuensche != nil { // haus#118: the list, ticked on one phone, fetched again on the others
		if l == nil { // einkauf off: the music wish list's lists are musik's own (ADR 0001 H4)
			l = s.hh.Wuensche.Listen
		}
		l.Changed = func(r caldav.ListRef) { live.Tell(pages.ListeLive(r)) }
	}
	gs := pages.Gruppen(family) // each module's pages (ADR 0001 §1, homes#243)
	for _, name := range slices.Sorted(maps.Keys(gs)) {
		for _, r := range gs[name] {
			pagesMux.Handle(r.Muster, r.Handler)
		}
	}
	pagesMux.HandleFunc(pages.StartBoxPath, start)
	pagesMux.HandleFunc("/mehr", pages.Mehr(family))
	pagesMux.HandleFunc(pages.MehrAufgabenPath, pages.Mehr(family)) // its Einrichtung card, after the page
	pagesMux.HandleFunc(pages.SpurenPath, pages.Mehr(family))       // its traces of what the box lacks (ADR 0001 §10)
	pagesMux.HandleFunc(pages.AnfordernPath, pages.Anfordern(family))
	pagesMux.HandleFunc(pages.ModulPages, pages.ModulSeite(family))               // a module, admins: where a wish leads
	pagesMux.HandleFunc(pages.ZusammenPath, pages.Zusammen(family))               // „So hängt alles zusammen“, under Mehr
	pagesMux.HandleFunc("/aufgaben", pages.DeineAufgaben(family))                 // haus#188: Deine Aufgaben // the bar's fourth entry, with Abmelden (haus#67)
	pagesMux.HandleFunc("/willkommen", pages.Willkommen(family))                  // hello and the next steps (haus#56)
	pagesMux.HandleFunc(pages.EinrichtenPages, pages.Einrichten(family))          // a module's setup, ticked by hand (owner 2026-11-04)
	pagesMux.HandleFunc("/adresse", pages.Adresse(family))                        // the household's address, admins only (laib#561)
	pagesMux.HandleFunc("/adresse/vorschlagen", pages.AdresseVorschlagen(family)) // its form, a proposal to Laib
	pagesMux.HandleFunc("/abmelden", pages.Abmelden(s.publicURL))                 // Authelia's logout, back to Homes (haus#61)
	pagesMux.HandleFunc("/schrift/", pages.Font())                                // Geist, from Homes itself (haus#35)
	pagesMux.HandleFunc(pages.SymbolPath, pages.Symbol())                         // the icon and the web app manifest (owner 2026-11-02)
	boxCfg := pages.BoxConfig{Laib: s.ops, LaibURL: s.laibURL, Tickets: s.tickets, Family: family, Voice: s.voice, Devices: s.store}
	box := pages.BoxPage(boxCfg)
	pagesMux.HandleFunc("/box", box)
	pagesMux.HandleFunc("/box/einladen", pages.Einladen(boxCfg))    // the card "Person einladen", admins only (haus#56)
	pagesMux.HandleFunc(pages.AufgabenPath, pages.Aufgaben(boxCfg)) // the Box-Aufgaben: Erledigt, Später (haus#115)
	pagesMux.HandleFunc("/box/geraete", pages.Geraete(s.store, s.publicURL, family))
	pagesMux.HandleFunc("/pair-device", pages.PairDevice(s.store, family)) // the app's own pairing path (haus#6)
	pagesMux.HandleFunc("/box/", box)
	pagesMux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	pagesMux.HandleFunc("/", pages.Elsewhere(family)) // "/" is Start; every path not served is a 404 with the way there
	return pagesMux
}

// musicInboxMount is where laib's homes template mounts the music inbox
// (laib#639); Homes takes it when HOMES_MUSIC_INBOX names none.
const musicInboxMount = "/musik-eingang"

// musicInbox is the music inbox's folder: HOMES_MUSIC_INBOX, else the
// template's mount when it is there; "" without one.
func musicInbox() string {
	if d := os.Getenv("HOMES_MUSIC_INBOX"); d != "" {
		return d
	}
	if fi, err := os.Stat(musicInboxMount); err == nil && fi.IsDir() {
		return musicInboxMount
	}
	return ""
}

// episodeEvery is how often Homes looks for new podcast episodes.
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
			log.Printf("homes: musikwünsche: looking up again: %v", err)
		}
		done, err := w.Pruefen(ctx)
		if err != nil {
			log.Printf("homes: musikwünsche: %v", err)
		}
		for _, d := range done {
			log.Printf("homes: musikwunsch arrived for %q", d.Person)
			if d.Person == "" {
				continue
			}
			if l == nil {
				continue
			}
			if ok, _ := l.Takes(ctx, "tell", "person"); ok {
				if err := l.Tell(ctx, d.Person, d.Satz); err != nil {
					log.Printf("homes: musikwunsch: telling %s: %v", d.Person, err)
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
			log.Printf("homes: podcasts: fetching the first episodes: %v", err)
		}
		if ok, _ := l.Takes(ctx, "tell", "person"); ok {
			msgs, err := pods.Neue(ctx)
			if err != nil {
				log.Printf("homes: podcasts: looking for new episodes: %v", err)
			}
			for _, m := range msgs {
				if err := l.Tell(ctx, m.Person, m.Text); err != nil {
					log.Printf("homes: podcasts: telling %s: %v", m.Person, err)
				}
			}
		} else if !said {
			log.Printf("homes: podcasts: laib has no tell yet (laib#676): new episodes are not told")
			said = true
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// ensureHaushalt makes, in the household's one Radicale account, the
// collections of the modules that are on (ADR 0001 H5): the calendar for
// kalender and konto, the address book for kontakte and konto, the
// shopping list for einkauf (haus#118), the music wish list for musik;
// trying again with a growing pause while Radicale does not answer; done
// once they are there.
func ensureHaushalt(ctx context.Context, c *caldav.Client, l *modul.Liste) {
	var steps []func(context.Context) error
	if l.An("kalender") || l.An("konto") {
		steps = append(steps, c.EnsureCalendar)
	}
	if l.An("kontakte") || l.An("konto") {
		steps = append(steps, c.EnsureBook)
	}
	for _, list := range []struct {
		modul string
		ref   caldav.ListRef
		name  string
	}{{"einkauf", caldav.Einkauf, einkauf.Name}, {"musik", einkauf.MusikListe, einkauf.MusikName}} {
		if l.An(list.modul) {
			steps = append(steps, func(ctx context.Context) error { return c.EnsureList(ctx, list.ref, list.name) })
		}
	}
	wait := 10 * time.Second
	for len(steps) > 0 {
		tctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		var errs []error
		for _, step := range steps {
			errs = append(errs, step(tctx))
		}
		cancel()
		err := errors.Join(errs...)
		if err == nil {
			log.Printf("homes: Haushalt: the collections of the modules on are there")
			return
		}
		log.Printf("homes: Haushalt: making the collections: %v (again in %v)", err, wait)
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
		log.Fatal("homes: LAIB_TOKEN is required (minted on laib add homes as the homes hand)")
	}
	proxySecret := os.Getenv("HOMES_PROXY_SECRET")
	if proxySecret == "" {
		log.Fatal("homes: HOMES_PROXY_SECRET is required — Caddy sends it as X-Homes-Proxy, nothing else may reach the pages (ADR 0012 §3)")
	}
	dataDir := env("HOMES_DATA", "/data")
	store, err := devices.Open(dataDir)
	if err != nil {
		log.Fatalf("homes: devices: %v", err)
	}
	assistent.Set(os.Getenv("HOMES_ASSISTANT"))                          // the name, from Laib's box.yml assistant:; "Homes" without it
	cfg := napi.Config{Laib: ops, Witness: &schalter.Witness{Laib: ops}} // who switched what (haus#89)
	cfg.Whoami = func(b string) (string, bool) {
		d, ok := store.Lookup(b)
		return d.UID, ok
	}
	// only phones paired through Homes; nothing old is carried (laib ADR 0022 §4)
	cfg.Validate = func(_ context.Context, b string) bool { _, ok := store.Lookup(b); return ok }
	// one favorites store for the pages' ☆ and the app's start screen
	cfg.Favorites = favoriten.Open(dataDir)
	if cfg.HA, err = ha.FromEnv(); err != nil { // the house paths and the event stream from HA; unset, 404
		log.Fatalf("homes: %v", err)
	}
	if cfg.Energy, err = napi.EnergyFromEnv(); err != nil {
		log.Fatalf("homes: %v", err)
	}
	// laib ADR 0017: the Haushalt MCP source; a tool only when its backend is set
	hh, err := haushalt.FromEnv()
	if err != nil {
		log.Fatalf("homes: %v", err)
	}
	hh.Version = Version
	var pods *podcasts.Service
	if c := abs.FromEnv(); c != nil { // haus#123: HOMES_ABS_URL and HOMES_ABS_TOKEN from laib (laib#676)
		pods = podcasts.Open(c, dataDir)
		hh.Podcasts = pods
	}
	gs := gesehenFromEnv(hh.Calendar)
	if hh.Calendar != nil {
		hh.Einkauf = einkauf.Open(hh.Calendar) // haus#118: the household's shopping list, haus/einkauf
		// a shared song goes on haus/liste-musik, with its stages and where it is (MusicBrainz)
		hh.Wuensche = &einkauf.Wuensche{Listen: hh.Einkauf, Dir: dataDir, MB: musicbrainz.New(hh.Fetcher)}
		if jf := jellyfin.FromEnv(); jf != nil { // laib#695: "hast du schon", and arrived wishes ticked off
			hh.Wuensche.Bib = jf
		}
		if app := discogs.AppFromEnv(); app != nil { // each person's own Discogs, through the household's app (laib#696)
			hh.Wuensche.Regale = &discogs.Regale{App: app, Dir: dataDir}
		}
	}
	cfg.Haushalt = hh       // haus#10: the tool widgets from the same backends; none set, 404
	cfg.Konto = hh.Calendar // haus#218: the phone's Homes account, on the same Radicale account
	cfg.Papierkorb = &papierkorb.Korb{Dir: dataDir}
	// haus#10: the camera capture into the share's Paperless consume folder; unset, 404
	if cfg.UploadDir = os.Getenv("HOMES_UPLOAD_DIR"); cfg.UploadDir != "" {
		if fi, err := os.Stat(cfg.UploadDir); err != nil || !fi.IsDir() {
			log.Printf("homes: HOMES_UPLOAD_DIR %s is not a folder (yet): uploads answer 503 until it is", cfg.UploadDir)
		} else if err := napi.SweepUploads(cfg.UploadDir); err != nil { // parts a stopped Homes left staged
			log.Printf("homes: HOMES_UPLOAD_DIR: sweeping staged parts: %v", err)
		}
	}
	// haus#207, laib ADR 0029: the message archives' inbox, one folder per
	// person, where the app's phone backups land; unset, /napi/sicherung is 503
	var archive map[string]napi.ArchivIntern
	if dir := os.Getenv("HOMES_ARCHIV_DIR"); dir != "" {
		cfg.Sicherung = napi.NewSicherung(dir, dataDir)
		// a new phone's restore (haus#209): each person's archive for the WhatsApp media,
		// HOMES_ARCHIV_INTERN and HOMES_ARCHIV_SECRET_<PERSON>; without them the media answer 503
		archive = napi.ArchivInternFromEnv(os.Getenv)
		cfg.Sicherung.SetArchive(archive)
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
			log.Printf("homes: HOMES_ARCHIV_DIR %s is not a folder (yet): backups answer 503 until it is", dir)
		}
	}
	// the app's Standortverlauf (owner 2026-11-02): Reitti inside the box,
	// HOMES_REITTI_URL (http://host.containers.internal:<port>); unset, /napi/standort is 503
	cfg.Reitti = reitti.FromEnv(dataDir)
	haPublic := pages.AppURL(pages.OnDomain(os.Getenv("HOMES_HA_PUBLIC_URL"), os.Getenv("HOMES_PUBLIC_URL"))) // haus#64
	if haPublic == "" && os.Getenv("HOMES_HA_PUBLIC_URL") != "" {
		log.Printf("homes: HOMES_HA_PUBLIC_URL %q is not an http(s) address: no link to Home Assistant", os.Getenv("HOMES_HA_PUBLIC_URL"))
	}
	radicalePublic := pages.AppURL(pages.OnDomain(os.Getenv("HOMES_RADICALE_PUBLIC_URL"), os.Getenv("HOMES_PUBLIC_URL"))) // haus#66
	if radicalePublic == "" && os.Getenv("HOMES_RADICALE_PUBLIC_URL") != "" {
		log.Printf("homes: HOMES_RADICALE_PUBLIC_URL %q is not an http(s) address: no link to Radicale, no addresses for the phone", os.Getenv("HOMES_RADICALE_PUBLIC_URL"))
	}
	laibURL := env("LAIB_PUBLIC_URL", "https://laib."+env("LAIB_DOMAIN", "example"))
	docsMail := pages.MailAddress(os.Getenv("HOMES_DOCUMENTS_MAIL")) // haus#68
	if docsMail == "" && os.Getenv("HOMES_DOCUMENTS_MAIL") != "" {
		log.Printf("homes: HOMES_DOCUMENTS_MAIL %q is not one plain address: no mail button", os.Getenv("HOMES_DOCUMENTS_MAIL"))
	}
	paperlessPublic := pages.AppURL(pages.OnDomain(os.Getenv("HOMES_PAPERLESS_PUBLIC_URL"), os.Getenv("HOMES_PUBLIC_URL")))
	paperlessLAN, _ := strconv.ParseBool(os.Getenv("HOMES_PAPERLESS_LAN_ONLY"))
	b := bote.FromEnv() // the Sprechen field (haus#12): with the Bote's key only (laib#215)
	// haus#50: counted for usage-metrics, flushed every five minutes; the voice
	// from HA's Assist runs every minute, as HA keeps only the last ten
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	counter := usage.FromEnv(os.Getenv)
	var voice *usage.Voice
	if counter != nil && cfg.HA != nil {
		voice = &usage.Voice{HA: cfg.HA, Counter: counter, Since: time.Now(), Log: log.Printf}
	}
	st := site{napi: cfg, hh: hh, store: store, ops: ops, proxySecret: proxySecret, laibURL: laibURL,
		publicURL: os.Getenv("HOMES_PUBLIC_URL"), tickets: werkstatt.FromEnv(), favorites: cfg.Favorites, bote: b, podcasts: pods, gesehen: gs,
		samba: samba.FromEnv(), sambaHost: os.Getenv("HOMES_SAMBA_HOST"),
		usage: counter, voice: voice, welcome: true,
		stt:      pages.STT{Provider: os.Getenv("HOMES_STT_PROVIDER"), Language: env("HOMES_STT_LANGUAGE", "de")},
		haPublic: haPublic, inverted: pages.InvertedFromList(os.Getenv("HOMES_COVER_INVERTED")),
		radicale: radicalePublic, views: ansicht.Open(dataDir, os.Getenv("HOMES_RADICALE_USER")), dataDir: dataDir,
		docsMail: docsMail, paperlessPublic: paperlessPublic, paperlessLAN: paperlessLAN, musik: musicInbox(), boteSignal: strings.TrimSpace(os.Getenv("HOMES_BOTE_SIGNAL")),
		domain: einrichten.Domain(os.Getenv("HOMES_PUBLIC_URL")), archive: archive, gesichter: gesichter.FromEnv(dataDir)}
	start := st                     // what the list is built from again once Laib says what runs (dienstePflegen)
	st.napi.Module = logModule(&st) // HOMES_MODULE_AUS takes what it switches off out of st
	st.napi.Einrichtung = einrichtung(st, devices.Gekoppelt(store), jellyfin.KeyFromEnv())
	// the counter stops only once the server has: a page is counted after
	// it is answered, so the last flush waits for the requests under way (#92)
	counting, stopCounting := context.WithCancel(context.Background())
	defer stopCounting()
	flushed := make(chan struct{})
	go func() { st.usage.Run(counting, 5*time.Minute); close(flushed) }()
	go st.voice.Run(ctx, time.Minute)
	if hh.Calendar != nil { // the collections there from the start (owner 2026-10-24), of the modules on
		go ensureHaushalt(ctx, hh.Calendar, st.napi.Module)
	}
	go quellenWegStart(ctx, st.hh.Contacts, dataDir) // once: the „Quelle:“ lines out of the firm cards (owner 2026-11-05)
	startLeaf(ctx, st)
	go schliesseWuensche(ctx, ops, st.napi.Module, time.Minute) // a wish for what is on now is done (ADR 0001 §10)
	go dienstePflegen(ctx, start, st.napi.Module, 30*time.Second, time.Hour)
	go st.gesichter.Pflegen(ctx, time.Hour, st.hh.Contacts) // Immich's people hourly, a face only when new, changed or a week old; each contact a person
	mux := routes(st)
	addr := env("HOMES_LISTEN", "127.0.0.1:8650")
	log.Printf("homes %s: listening on %s as %s, laib %s, home assistant %v, haushalt %d tools, memory library %v (tidy-up %v), uploads %q, the Bote %v, counting %v (voice %v), pages only behind the proxy secret", Version, addr, assistent.Name(), ops.URL, cfg.HA != nil, len(haushalt.Tools(hh)), hh.Library != nil, hh.Judge != nil, cfg.UploadDir, b != nil, counter != nil, voice != nil)
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
	<-flushed // the last counts go out before Homes does
}

// gesehenFromEnv is Gesehen & Gehört's backends (haus#152): Jellyfin with
// its API key (HOMES_JELLYFIN_KEY, laib's media template mints it), ABS with
// Homes' key, the wish list on dav; nil when neither app is there.
func gesehenFromEnv(dav *caldav.Client) *gesehen.Service {
	g := &gesehen.Service{DAV: dav}
	if jf := jellyfin.KeyFromEnv(); jf != nil {
		g.Filme = jf
	}
	if a := abs.FromEnv(); a != nil {
		g.Buecher = a
	}
	if g.Filme == nil && g.Buecher == nil {
		return nil
	}
	return g
}
