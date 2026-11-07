package pages

import (
	"context"
	"html"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/mdopp/homes/internal/abruf"
	"github.com/mdopp/homes/internal/papierkorb"

	"github.com/mdopp/homes/internal/ansicht"
	"github.com/mdopp/homes/internal/bote"
	"github.com/mdopp/homes/internal/caldav"
	"github.com/mdopp/homes/internal/cards"
	"github.com/mdopp/homes/internal/discogs"
	"github.com/mdopp/homes/internal/einkauf"
	"github.com/mdopp/homes/internal/einrichten"
	"github.com/mdopp/homes/internal/energie"
	"github.com/mdopp/homes/internal/favoriten"
	"github.com/mdopp/homes/internal/gesehen"
	"github.com/mdopp/homes/internal/gesichter"
	"github.com/mdopp/homes/internal/ha"
	"github.com/mdopp/homes/internal/judge"
	"github.com/mdopp/homes/internal/laib"
	"github.com/mdopp/homes/internal/modul"
	"github.com/mdopp/homes/internal/napi"
	"github.com/mdopp/homes/internal/notes"
	"github.com/mdopp/homes/internal/paperless"
	"github.com/mdopp/homes/internal/podcasts"
	"github.com/mdopp/homes/internal/qmd"
	"github.com/mdopp/homes/internal/reitti"
	"github.com/mdopp/homes/internal/samba"
	"github.com/mdopp/homes/internal/schalter"
	"github.com/mdopp/homes/internal/texte"
	"github.com/mdopp/homes/internal/usage"
)

// StartConfig is what the family's pages read (Start, Räume, Kalender,
// Notizen). A nil backend is not configured, and no page says anything
// about it.
type StartConfig struct {
	Laib   *laib.Client
	Module *modul.Liste // the box's areas (ADR 0001): Mehr's traces of what it lacks (§10); nil: none
	// Einrichtung: each person's setup tasks, one per module with its own app (owner 2026-11-04); nil: none
	Einrichtung *einrichten.Quellen
	Witness     *schalter.Witness // who switched what, into the log and Laib's journal (haus#89); nil: the log only
	Fetcher     abruf.Fetcher     // how Notizen fetches a page to keep (haus#117); the tests' allows their 127.0.0.1
	Podcasts    *podcasts.Service // the family's podcasts in Audiobookshelf (haus#123); nil: no page
	Gesehen     *gesehen.Service  // what each person watched and heard, and the film and book wish list (haus#152); nil: no page
	Einkauf     *einkauf.Liste    // the household's shopping list in Radicale (haus#118); nil: no page
	Wuensche    *einkauf.Wuensche // the music wish list (owner 2026-10-25); nil: a song link is a note
	Discogs     *discogs.Regale   // each person's records on Discogs, connected through the app (laib#696); nil: no Discogs
	Reitti      *reitti.Reitti    // the household's timeline, each person's token (Standortverlauf, owner 2026-11-02); nil: no page
	LaibURL     string            // where Rückgängig lives (the guarantees, admins only)
	HA          *ha.Client
	Energy      energie.Map // which sensors Energie reads (haus#12); empty: by friendly name
	// OhneEnergie: energie is switched off (ADR 0001 H6) while HA stays for
	// Geräte: no Energie tab, no energy line on Start
	OhneEnergie bool
	Calendar    *caldav.Client
	Contacts    *caldav.Client // a contact's page (haus#28)
	Notes       *notes.Dir
	Library     *qmd.Client       // Notizen's search by meaning (haus#13); nil: by words, said so
	Judge       *judge.Client     // the house model's tidy-up of Notizen's hits (haus#13); nil: none
	Docs        *paperless.Client // a document's page sends on to Paperless (haus#28)
	Favorites   *favoriten.Store  // each person's things on Start (laib ADR 0017), pinned with ☆ (haus#35)
	Bote        *bote.Client      // the Sprechen field's (haus#12); nil: no field, no tab
	Samba       *samba.Dir        // the network drive's passwords (haus#52, laib#296); nil: no page
	SambaHost   string            // the box's address in the house, shown as \\<host>\data; empty: no how-to
	Loc         *time.Location    // the house's time zone; default time.Local
	Now         func() time.Time  // for the tests; default time.Now
	Usage       *usage.Counter    // what the family uses, anonymously (haus#50); nil: nothing counted
	PublicURL   string            // Homes' own address, https://homes.<domain>
	Welcome     bool              // Start sends a person to Willkommen the first time a device opens it (haus#56)
	STT         STT               // the Sprechen field's microphone, HA's speech-to-text (haus#63); no provider: no microphone
	// HAPublicURL is Home Assistant's public address (HOMES_HA_PUBLIC_URL,
	// https://home.<domain>): Geräte's "In Home Assistant öffnen" (haus#64); empty: no link
	HAPublicURL string
	// Inverted are the covers wired the wrong way round (HOMES_COVER_INVERTED,
	// haus#64): Homes shows and sets them turned over, Hoch is HA's close
	Inverted map[string]bool
	// RadicalePublicURL is Radicale as phones reach it (HOMES_RADICALE_PUBLIC_URL,
	// https://caldav.<domain>, haus#66): its web page and each calendar's
	// address for the phone; empty: neither
	RadicalePublicURL string
	Views             *ansicht.Store // each person's colours and hidden calendars on Kalender (haus#66); nil: none
	// The Dokumente page (haus#68): the address mail is filed from
	// (HOMES_DOCUMENTS_MAIL; empty: no mail button), the way into
	// Paperless's inbox (HOMES_UPLOAD_DIR; nil: no upload), Paperless's
	// public address (HOMES_PAPERLESS_PUBLIC_URL; empty: no link) and
	// whether it opens only at home (HOMES_PAPERLESS_LAN_ONLY)
	DocumentsMail string
	Ablage        *napi.Uploads
	// Sicherung is the message archives' inbox (HOMES_ARCHIV_DIR, haus#207):
	// the page „Daten sichern“ reads the person's state from it; nil:
	// the page says the box has no archive
	Sicherung          *napi.Sicherung
	Papierkorb         *papierkorb.Korb // what the family deleted, for 30 days (haus#174); nil: deletes go at once
	MusicInbox         string           // laib's music inbox, which beets sorts in (haus#107); "": no Musik page
	PaperlessPublicURL string
	PaperlessLANOnly   bool
	// a contact's link to the viewer's message archive (owner 2026-11-05): the
	// persons who have one (HOMES_ARCHIV_INTERN), where it opens (archiv.<domain>,
	// the house's network only) and the house's calling code for its search
	Archive      map[string]bool
	ArchivURL    string
	PhoneCountry string
	// Gesichter is Immich for the contacts (owner 2026-11-05): their „Fotos“,
	// a face for a card without a picture, the suggestions; nil: none of it
	Gesichter *gesichter.Gesichter
}

// backendWait is how long a page waits for any one backend: a slow one
// is left out, the page still comes. A variable for the tests.
var backendWait = 4 * time.Second

const (
	maxGeraete = 12 // tiles in Deine Geräte
	maxHeute   = 3  // lines in Heute (owner 2026-11-07)
	horizon    = 14 // days ahead on Kalender
)

// What Start says when a part cannot do its job, or is empty (haus#35).
const (
	saysNoSwitching = "Schalten geht im Moment nicht."
	saysPinHow      = "Tippe unter Geräte bei einem Gerät auf ☆, dann erscheint es hier."
	saysNichtsHeute = "Heute und morgen steht nichts im Kalender."
)

// marke is Start's head, the app's start screen in words and mark: the
// icon, the name and the claim, so the page and the app are recognised as
// one (owner 2026-11-03).
var marke = func() string {
	svg, err := symbole.ReadFile("symbol/favicon.svg") // inline: no request, no login in the way
	if err != nil {
		panic(err)
	}
	s := strings.Replace(string(svg), `width="72" height="72"`, `width="52" height="52" aria-hidden="true"`, 1)
	s = strings.ReplaceAll(s, `"clip"`, `"marke-clip"`)
	s = strings.ReplaceAll(s, "url(#clip)", "url(#marke-clip)")
	return "<header class=marke>" + s + "<div><h1>Homes</h1><p class=claim>Dein Zuhause, klar im Blick.</p></div></header>"
}()

// sprechenKnopf is the way to Sprechen from Start's head (owner 2026-11-07:
// a small, unobtrusive entry, not a field in the way).
const sprechenKnopf = `<a class=sprechen-knopf href=/sprechen aria-label=Sprechen title=Sprechen><svg viewBox="0 0 24 24" width=22 height=22 aria-hidden=true fill=none stroke=currentColor stroke-width=2 stroke-linecap=round stroke-linejoin=round><path d="M21 12a8 8 0 0 1-11.6 7.1L4 20l1-4.4A8 8 0 1 1 21 12z"/></svg></a>`

// Start is the family's page (haus#12, haus#35; owner 2026-11-07: „Rigoros
// einfach“, docs/DOMAIN.md Gestaltungsregeln): Homes' mark and the day, then
// at most four blocks, each its state and at most one action — Braucht dich
// (only when something is open), Deine Geräte, Heute, Im Haus. A backend not
// configured is not mentioned; one not answering is one plain line, its error in the log.
func Start(cfg StartConfig) http.HandlerFunc {
	var box *boxStatus
	if cfg.Laib != nil {
		box = boxStatusFor(cfg.Laib)
	}
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case StartBoxPath: // Braucht dich with Laib's part, after the page (haus#67)
			startBox(w, r, cfg, box)
			return
		case "/start", "":
		default:
			Elsewhere(cfg)(w, r)
			return
		}
		if !showOnly(w, r, "Die Startseite") || firstVisit(w, r, cfg) {
			return
		}
		now, loc := cfg.clock()

		var (
			kept    *boxRead
			states  []ha.State
			haErr   error
			reg     ha.Registry
			events  []caldav.Event
			evErr   error
			todos   []caldav.Todo
			todoErr error
			favs    []string
			favErr  error
			reads   []func(context.Context)
		)
		person := r.Header.Get("Remote-User")
		if box != nil { // laib's status as kept, never waited for (haus#67)
			reads = append(reads, func(context.Context) {
				if ready, services, err, ok := box.read(boxInline); ok && err == nil {
					kept = &boxRead{ready, services}
				}
			})
		}
		if cfg.HA != nil {
			reads = append(reads,
				func(ctx context.Context) { states, haErr = cfg.HA.States(ctx) },
				// what HA hides stays hidden; until the registry has been read, nothing is
				func(ctx context.Context) { reg, _ = cfg.HA.CachedRegistry(ctx) })
		}
		if cfg.Calendar != nil {
			reads = append(reads,
				func(ctx context.Context) { events, evErr = cfg.Calendar.Events(ctx, now, endOfTomorrow(now, loc)) },
				func(ctx context.Context) { todos, todoErr = cfg.Calendar.OpenTodos(ctx) })
		}
		pins := cfg.Favorites != nil && cfg.HA != nil && person != ""
		if pins {
			reads = append(reads, func(context.Context) { favs, favErr = cfg.Favorites.For(person) })
		}
		fetch(r, reads...)
		states = cfg.allAsSeen(states) // a cover wired the wrong way round, turned over (haus#64)
		logErrs("start", map[string]error{"home assistant": haErr, "calendar": evErr, "reminders": todoErr, "favorites": favErr})

		b := familyHead(r, cfg, "Homes", "Start")
		b.WriteString(liveRefresh("alle", 120)) // lamps and players by push; the day and Braucht dich every two minutes
		head := marke
		if cfg.Bote != nil {
			head = strings.Replace(marke, "</header>", sprechenKnopf+"</header>", 1)
		}
		b.WriteString(head + "<p class=datum>" + dateWords(now, loc) + "</p>")
		b.WriteString(brauchtDichSpaeter(r, cfg, kept))
		if pins {
			b.WriteString(deineGeraete(favs, favErr, states, haErr, reg, now.In(loc).Format("15:04")))
		}
		if cfg.Calendar != nil {
			b.WriteString(heute(events, evErr, todos, todoErr, now, loc))
		}
		if cfg.HA != nil {
			b.WriteString(imHaus(cfg, states, haErr, reg))
		}
		writePage(w, http.StatusOK, b)
	}
}

// imHaus answers "who is home, and what is on?" in one line (owner
// 2026-11-07): the people at home, the lamps on, what plays, and the
// energy now leading to Energie — with one action: the lamps off, else a
// player's Pause. What HA hides is left out; a group is not a lamp.
func imHaus(cfg StartConfig, states []ha.State, haErr error, reg ha.Registry) string {
	const open = "<section class=\"karte im-haus\" aria-labelledby=karte-im-haus><h2 id=karte-im-haus>Im Haus</h2><ul>"
	if haErr != nil {
		return open + "<li>" + saysNoHouse + " " + saysNoSwitching + "</li></ul></section>"
	}
	persons := 0
	var home, plays []string
	var pause *cards.Action
	twins := twinPlayers(states, reg, false)
	for _, s := range states {
		if !shown(s, reg.Hidden) || twins[s.EntityID] {
			continue
		}
		domain, _, _ := strings.Cut(s.EntityID, ".")
		switch {
		case domain == "person":
			persons++
			if s.State == "home" {
				home = append(home, friendly(s))
			}
		case domain == "media_player" && s.State == "playing":
			plays = append(plays, friendly(s))
			if f, _ := number(s.Attributes["supported_features"]); int64(f)&pauseFeature != 0 && pause == nil {
				pause = &cards.Action{Label: "Pause", Entity: s.EntityID, Do: "media_player.media_pause", Back: "/start"}
			}
		}
	}
	var parts []string
	if persons > 0 {
		sort.Strings(home)
		parts = append(parts, map[bool]string{true: "Niemand zu Hause", false: andList(home) + " da"}[len(home) == 0])
	}
	var action *cards.Action
	switch lamps := litLamps(states, reg.Hidden); len(lamps) {
	case 0:
		parts = append(parts, "Alle Lichter aus")
	case 1:
		parts = append(parts, friendly(lamps[0])+" an")
		action = &cards.Action{Label: "Aus", Entity: lamps[0].EntityID, Do: "light.turn_off", Back: "/start"}
	default:
		parts = append(parts, strconv.Itoa(len(lamps))+" Lichter an")
		action = &cards.Action{Label: "Alle aus", Do: allLightsOff, Back: "/start"}
	}
	if len(plays) > 0 {
		sort.Strings(plays)
		parts = append(parts, andList(plays)+map[bool]string{true: " spielt", false: " spielen"}[len(plays) == 1])
	}
	if action == nil {
		action = pause
	}
	line := html.EscapeString(strings.Join(parts, " · "))
	if e := energieKurz(cfg, states); e != "" {
		line += " · <a href=/energie>" + html.EscapeString(e) + "</a>"
	}
	if action == nil {
		return open + "<li>" + line + "</li></ul></section>"
	}
	return open + "<li class=tu><div>" + line + "</div>" + action.Form() + "</li></ul></section>"
}

// pauseFeature is HA's MediaPlayerEntityFeature.PAUSE.
const pauseFeature = 1

// shown: not a group (an entity carrying an entity_id list, light groups
// too: its members count), and not hidden by HA.
func shown(s ha.State, hidden map[string]bool) bool {
	_, group := s.Attributes["entity_id"].([]any)
	return !group && !hidden[s.EntityID]
}

// litLamps are the lamps that are on, as Im Haus counts them; "Alle aus"
// turns off exactly these.
func litLamps(states []ha.State, hidden map[string]bool) []ha.State {
	var out []ha.State
	for _, s := range states {
		if strings.HasPrefix(s.EntityID, "light.") && s.State == "on" && shown(s, hidden) {
			out = append(out, s)
		}
	}
	return out
}

// andList is "Anna", "Anna und Ben", "Anna, Ben und Carla".
func andList(xs []string) string {
	if len(xs) < 2 {
		return strings.Join(xs, "")
	}
	return strings.Join(xs[:len(xs)-1], ", ") + " und " + xs[len(xs)-1]
}

// deineGeraete answers "where are my things?": the person's own, in their
// order, as the app's tiles (kacheln); only a thing of a card domain HA
// knows and does not hide. None says how to get one.
func deineGeraete(ids []string, err error, states []ha.State, haErr error, reg ha.Registry, stand string) string {
	c := cards.Card{Title: texte.T(texte.Geraete)}
	switch {
	case err != nil:
		c.Add(saysNoFavorites, "")
		return c.HTML()
	case haErr != nil && len(ids) > 0:
		c.Add(saysFavsUnseen, "")
		return c.HTML()
	case haErr != nil:
		return "" // Im Haus says the house does not answer; how to pin can wait
	}
	byID := make(map[string]ha.State, len(states))
	for _, s := range states {
		byID[s.EntityID] = s
	}
	var ks []kachel
	n := 0
	for _, id := range ids {
		s, ok := byID[id]
		domain, _, _ := strings.Cut(id, ".")
		if !ok || !cards.Domains[domain] || reg.Hidden[id] {
			continue
		}
		if n++; n <= maxGeraete {
			ks = append(ks, neueKachel(s, reg.TempUnit, "/start"))
		}
	}
	if n == 0 {
		c.AddLink(saysPinHow, "", "/raeume")
		return c.HTML()
	}
	return kachelnHTML(c.ID(), ks, n-maxGeraete, stand)
}

// capital is the word with its first letter upper case: "an" → "An".
func capital(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 {
		return s
	}
	return string(unicode.ToUpper(r)) + s[n:]
}

// heute answers "what is today?" (owner 2026-11-07): at most maxHeute
// lines of today and tomorrow — the appointments and the reminders due, an
// overdue one first — each leading to its page, then Kalender. Nothing
// coming is one quiet line.
func heute(events []caldav.Event, evErr error, todos []caldav.Todo, todoErr error, now time.Time, loc *time.Location) string {
	type item struct {
		at  time.Time
		row cards.Row
	}
	var items []item
	until := endOfTomorrow(now, loc)
	if evErr == nil {
		for _, e := range events {
			when := zeit(e.Start, e.AllDay, now, loc)
			if !e.AllDay && e.Start.Before(now) {
				when = "jetzt"
			}
			items = append(items, item{e.Start, cards.Row{Text: summary(e.Summary), When: when, Href: "/kalender"}})
		}
	}
	if todoErr == nil {
		for _, t := range todos {
			if t.Due.IsZero() || !t.Due.Before(until) {
				continue
			}
			row := cards.Row{Text: summary(t.Summary), When: zeit(t.Due, t.DueAllDay, now, loc), Href: "/kalender"}
			if overdue := dueWords(t, now, loc); strings.HasPrefix(overdue, "überfällig") {
				row.When = "überfällig"
			}
			if uid := strings.TrimSpace(t.UID); uid != "" {
				row.Href = cards.ReminderPage(uid)
			}
			items = append(items, item{t.Due, row})
		}
	}
	failed := evErr != nil || todoErr != nil
	if len(items) == 0 && !failed {
		return "<p class=ruhig><span class=\"punkt grau\"></span><a href=/kalender>" + saysNichtsHeute + "</a></p>"
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].at.Before(items[j].at) })
	c := cards.Card{Title: "Heute", Look: cards.Times, Link: &cards.Link{Href: "/kalender", Text: "Kalender →"}}
	for i, it := range items {
		if i == maxHeute {
			c.Link.Text = "Kalender · " + strconv.Itoa(len(items)-maxHeute) + " weitere →"
			break
		}
		c.Rows = append(c.Rows, it.row)
	}
	if failed {
		c.Add(saysNoCalendar, "")
	}
	return c.HTML()
}

// zeit is a line's time in Heute: 14:00 today, morgen 08:30, heute or
// morgen for a whole day.
func zeit(t time.Time, allDay bool, now time.Time, loc *time.Location) string {
	day := dayWord(t, now, loc)
	switch {
	case allDay && t.Before(now):
		return "heute"
	case allDay:
		return day
	case day == "heute":
		return t.In(loc).Format("15:04")
	}
	return day + " " + t.In(loc).Format("15:04")
}

// endOfTomorrow is the midnight after tomorrow, in loc: Heute's end.
func endOfTomorrow(now time.Time, loc *time.Location) time.Time {
	y, m, d := now.In(loc).Date()
	return time.Date(y, m, d+2, 0, 0, 0, 0, loc)
}

// saysNoSuchPage is what a path Homes does not serve says.
const saysNoSuchPage = "Diese Seite gibt es nicht."

// Elsewhere is "/" and every page path Homes does not serve (haus#12): "/"
// is Start; anything else is a 404 in the house's own frame, one plain
// line and the way to Start — never a blank error, never Start
// pretending all is well.
func Elsewhere(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/start", http.StatusFound)
			return
		}
		missing{http.StatusNotFound, saysNoSuchPage, "Nicht gefunden", "/start", "Zur Startseite"}.write(w, r, cfg)
	}
}

// isAdmin reads Authelia's Remote-Groups.
func isAdmin(r *http.Request) bool {
	return strings.Contains(","+r.Header.Get("Remote-Groups")+",", ",admins,")
}

func friendly(s ha.State) string {
	if n, _ := s.Attributes["friendly_name"].(string); oneLine(n) != "" {
		return trimRunes(oneLine(n), 60)
	}
	return s.EntityID
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func trimRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// summary is a calendar or note title for a row.
func summary(s string) string {
	if s = oneLine(s); s == "" {
		return "(ohne Titel)"
	}
	return trimRunes(s, 120)
}

// daysAgo is how many calendar days t lies before now, in loc: 0 today, 1
// yesterday, -1 tomorrow.
func daysAgo(t, now time.Time, loc *time.Location) int {
	y, m, d := t.In(loc).Date()
	ny, nm, nd := now.In(loc).Date()
	return int(time.Date(ny, nm, nd, 12, 0, 0, 0, time.UTC).Sub(time.Date(y, m, d, 12, 0, 0, 0, time.UTC)).Hours() / 24)
}

var (
	weekdays = [...]string{"Sonntag", "Montag", "Dienstag", "Mittwoch", "Donnerstag", "Freitag", "Samstag"}
	months   = [...]string{"Januar", "Februar", "März", "April", "Mai", "Juni", "Juli", "August", "September", "Oktober", "November", "Dezember"}
)

// dayWord says a day the family way, relative to now: heute, morgen,
// gestern, else "Fr 16.10.".
func dayWord(t, now time.Time, loc *time.Location) string {
	switch daysAgo(t, now, loc) {
	case 0:
		return "heute"
	case -1:
		return "morgen"
	case 1:
		return "gestern"
	}
	t = t.In(loc)
	return weekdays[t.Weekday()][:2] + " " + t.Format("02.01.")
}

// dateWords is the day at the top of Start: "Mittwoch, 14. Oktober".
func dateWords(now time.Time, loc *time.Location) string {
	now = now.In(loc)
	return weekdays[now.Weekday()] + ", " + strconv.Itoa(now.Day()) + ". " + months[now.Month()-1]
}
