package pages

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/mdopp/haus/internal/ansicht"
	"github.com/mdopp/haus/internal/bote"
	"github.com/mdopp/haus/internal/caldav"
	"github.com/mdopp/haus/internal/cards"
	"github.com/mdopp/haus/internal/energie"
	"github.com/mdopp/haus/internal/favoriten"
	"github.com/mdopp/haus/internal/ha"
	"github.com/mdopp/haus/internal/judge"
	"github.com/mdopp/haus/internal/laib"
	"github.com/mdopp/haus/internal/notes"
	"github.com/mdopp/haus/internal/paperless"
	"github.com/mdopp/haus/internal/qmd"
	"github.com/mdopp/haus/internal/samba"
	"github.com/mdopp/haus/internal/usage"
)

// StartConfig is what the family's pages read (Start, Räume, Kalender,
// Notizen). A nil backend is not configured, and no page says anything
// about it.
type StartConfig struct {
	Laib      *laib.Client
	LaibURL   string // where Rückgängig lives (the guarantees, admins only)
	HA        *ha.Client
	Energy    energie.Map // which sensors Energie reads (haus#12); empty: by friendly name
	Calendar  *caldav.Client
	Contacts  *caldav.Client // a contact's page (haus#28)
	Notes     *notes.Dir
	Library   *qmd.Client       // Notizen's search by meaning (haus#13); nil: by words, said so
	Judge     *judge.Client     // the house model's tidy-up of Notizen's hits (haus#13); nil: none
	Docs      *paperless.Client // a document's page sends on to Paperless (haus#28)
	Favorites *favoriten.Store  // each person's things on Start (laib ADR 0017), pinned with ☆ (haus#35)
	Bote      *bote.Client      // the Sprechen field's (haus#12); nil: no field, no tab
	Samba     *samba.Dir        // the network drive's passwords (haus#52, laib#296); nil: no page
	SambaHost string            // the box's address in the house, shown as \\<host>\data; empty: no how-to
	Loc       *time.Location    // the house's time zone; default time.Local
	Now       func() time.Time  // for the tests; default time.Now
	Usage     *usage.Counter    // what the family uses, anonymously (haus#50); nil: nothing counted
	PublicURL string            // Haus's own address, https://haus.<domain>: the apps' addresses on Willkommen (haus#56)
	Welcome   bool              // Start sends a person to Willkommen the first time a device opens it (haus#56)
	STT       STT               // the Sprechen field's microphone, HA's speech-to-text (haus#63); no provider: no microphone
	// HAPublicURL is Home Assistant's public address (HAUS_HA_PUBLIC_URL,
	// https://home.<domain>): Geräte's "In Home Assistant öffnen" (haus#64); empty: no link
	HAPublicURL string
	// Inverted are the covers wired the wrong way round (HAUS_COVER_INVERTED,
	// haus#64): Haus shows and sets them turned over, Hoch is HA's close
	Inverted map[string]bool
	// RadicalePublicURL is Radicale as phones reach it (HAUS_RADICALE_PUBLIC_URL,
	// https://caldav.<domain>, haus#66): its web page and each calendar's
	// address for the phone; empty: neither
	RadicalePublicURL string
	Views             *ansicht.Store // each person's colours and hidden calendars on Kalender (haus#66); nil: none
}

// backendWait is how long a page waits for any one backend: a slow one
// is left out, the page still comes. A variable for the tests.
var backendWait = 4 * time.Second

const (
	maxNames   = 5  // lamp names in the lights' row
	maxPlaying = 3  // media players in Im Haus
	maxGeraete = 12 // tiles in Meine Geräte
	maxAgenda  = 6  // rows in Heute und demnächst
	horizon    = 14 // days ahead for appointments and reminders
)

// What Start says when a part cannot do its job, or is empty: then how it
// fills (haus#35, "leer heißt erklären").
const (
	saysNoSwitching = "Schalten geht im Moment nicht."
	saysPinHow      = "Tippe unter Geräte bei einem Gerät auf ☆, dann erscheint es hier."
	saysPinMore     = "Mit ☆ legst du Geräte auf die Startseite."
	saysNothingDue  = "Nichts in den nächsten zwei Wochen."
	saysAgendaHow   = "Termine und Erinnerungen aus dem Kalender stehen hier."
)

// Start is the family's page (haus#12, haus#35): "Zuhause" and the day,
// then each part answering one question a family member has — who is
// home and what is on (Im Haus, one action a row), my own things (Meine
// Geräte, switched in place), what comes up (Heute und demnächst) — and
// the box in one calm line at the bottom, at the top when something is
// wrong. A backend that is not configured is not mentioned; one not
// answering is one plain line where its rows would be, its error in the
// log, not on the page.
func Start(cfg StartConfig) http.HandlerFunc {
	var box *boxStatus
	if cfg.Laib != nil {
		box = &boxStatus{laib: cfg.Laib}
	}
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case StartBoxPath: // the box line, after the page (haus#67)
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
			ready    laib.Readiness
			services []laib.Service
			laibErr  error
			boxOK    bool
			states   []ha.State
			haErr    error
			reg      ha.Registry
			events   []caldav.Event
			evErr    error
			todos    []caldav.Todo
			todoErr  error
			favs     []string
			favErr   error
			reads    []func(context.Context)
		)
		person := r.Header.Get("Remote-User")
		if box != nil { // laib's status kept, waited for briefly (haus#67)
			reads = append(reads, func(context.Context) { ready, services, laibErr, boxOK = box.read(boxInline) })
		}
		if cfg.HA != nil {
			reads = append(reads,
				func(ctx context.Context) { states, haErr = cfg.HA.States(ctx) },
				// what HA hides stays hidden; until the registry has been read, nothing is
				func(ctx context.Context) { reg, _ = cfg.HA.CachedRegistry(ctx) })
		}
		if cfg.Calendar != nil {
			reads = append(reads,
				func(ctx context.Context) { events, evErr = cfg.Calendar.Events(ctx, now, now.AddDate(0, 0, horizon)) },
				func(ctx context.Context) { todos, todoErr = cfg.Calendar.OpenTodos(ctx) })
		}
		pins := cfg.Favorites != nil && cfg.HA != nil && person != ""
		if pins {
			reads = append(reads, func(context.Context) { favs, favErr = cfg.Favorites.For(person) })
		}
		fetch(r, reads...)
		states = cfg.allAsSeen(states) // a cover wired the wrong way round, turned over (haus#64)
		logErrs("start", map[string]error{"laib": laibErr, "home assistant": haErr, "calendar": evErr, "reminders": todoErr, "favorites": favErr})

		b := familyHead(r, cfg, "Zuhause", "Start")
		b.WriteString("<h1>Zuhause</h1><p class=datum>" + dateWords(now, loc) + "</p>")
		var top, bottom string
		if cfg.Laib != nil {
			quiet := haErr == nil && evErr == nil && todoErr == nil && favErr == nil
			if boxOK {
				top, bottom = familyBox(ready, services, laibErr, quiet, now, loc)
			} else {
				top, bottom = boxLater(quiet)
			}
		}
		b.WriteString(top)
		if cfg.Bote != nil {
			b.WriteString(sprechenField(cfg, false))
		}
		if cfg.HA != nil {
			b.WriteString(imHaus(states, haErr, reg.Hidden).HTML())
		}
		if pins {
			b.WriteString(meineGeraete(favs, favErr, states, haErr, reg).HTML())
		}
		if cfg.Calendar != nil {
			b.WriteString(agenda(events, evErr, todos, todoErr, now, loc).HTML())
		}
		b.WriteString(bottom)
		b.WriteString(toNetzlaufwerk(cfg.Samba, person))
		b.WriteString(toWillkommen(cfg))
		writePage(w, http.StatusOK, b)
	}
}

// imHaus answers "who is home, and what is on?": the people at home, the
// lamps that are on with one button to turn them off, what plays with
// Pause. What HA hides is left out; a group is not a lamp.
func imHaus(states []ha.State, haErr error, hidden map[string]bool) cards.Card {
	c := cards.Card{Title: "Im Haus", Look: cards.Boxes}
	if haErr != nil {
		c.Add(saysNoHouse+" "+saysNoSwitching, "")
		return c
	}
	persons := 0
	var home []string
	type playing struct {
		id, name, what string
		pause          bool
	}
	var plays []playing
	for _, s := range states {
		if !shown(s, hidden) {
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
			title, _ := s.Attributes["media_title"].(string)
			artist, _ := s.Attributes["media_artist"].(string)
			what := trimRunes(oneLine(title), 80)
			if a := trimRunes(oneLine(artist), 60); a != "" {
				if what != "" {
					what += " · "
				}
				what += a
			}
			f, _ := number(s.Attributes["supported_features"])
			plays = append(plays, playing{s.EntityID, friendly(s), what, int64(f)&pauseFeature != 0})
		}
	}
	if persons > 0 {
		sort.Strings(home)
		switch len(home) {
		case 0:
			c.Add("Niemand ist zu Hause", "")
		case 1:
			c.Add(home[0]+" ist da", "")
		default:
			c.Add(andList(home)+" sind da", "")
		}
	}
	lamps := litLamps(states, hidden)
	switch len(lamps) {
	case 0:
		c.Add("Alle Lichter sind aus", "")
	case 1:
		c.Rows = append(c.Rows, cards.Row{Text: "1 Licht an", Detail: friendly(lamps[0]),
			Action: &cards.Action{Label: "Aus", Entity: lamps[0].EntityID, Do: "light.turn_off", Back: "/start"}})
	default:
		names := make([]string, len(lamps))
		for i, l := range lamps {
			names[i] = friendly(l)
		}
		sort.Strings(names)
		list := strings.Join(names[:min(len(names), maxNames)], ", ")
		if len(names) > maxNames {
			list += fmt.Sprintf(" und %d weitere", len(names)-maxNames)
		}
		c.Rows = append(c.Rows, cards.Row{Text: fmt.Sprintf("%d Lichter an", len(lamps)), Detail: list,
			Action: &cards.Action{Label: "Alle aus", Do: allLightsOff, Back: "/start"}})
	}
	sort.SliceStable(plays, func(i, j int) bool { return plays[i].name < plays[j].name })
	for i, p := range plays {
		if i == maxPlaying {
			break
		}
		row := cards.Row{Text: p.name + " spielt", Detail: p.what}
		if p.pause {
			row.Action = &cards.Action{Label: "Pause", Entity: p.id, Do: "media_player.media_pause", Back: "/start"}
		}
		c.Rows = append(c.Rows, row)
	}
	return c
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

// meineGeraete answers "where are my things?": the person's own, in their
// order, as tiles like the phone widget, a lamp's or a switch's switch
// right there. Only what a thing's page shows: a thing of a card domain
// that HA knows and does not hide. None says how to get one.
func meineGeraete(ids []string, err error, states []ha.State, haErr error, reg ha.Registry) cards.Card {
	c := cards.Card{Title: "Meine Geräte", Look: cards.Tiles}
	switch {
	case err != nil:
		c.Add(saysNoFavorites, "")
		return c
	case haErr != nil && len(ids) > 0:
		c.Add(saysFavsUnseen, "")
		return c
	case haErr != nil:
		return c // Im Haus says the house does not answer; how to pin can wait
	}
	byID := make(map[string]ha.State, len(states))
	for _, s := range states {
		byID[s.EntityID] = s
	}
	n := 0
	for _, id := range ids {
		s, ok := byID[id]
		domain, _, _ := strings.Cut(id, ".")
		if !ok || !cards.Domains[domain] || reg.Hidden[id] {
			continue
		}
		if n++; n <= maxGeraete {
			row := tile(domain, s, reg.TempUnit, "/start")
			row.Pin = &cards.Pin{Entity: id, Name: row.Text, On: true, Back: "/start"} // ★ takes it off again (haus#64)
			c.Rows = append(c.Rows, row)
		}
	}
	more(&c, n-maxGeraete)
	if n == 0 {
		c.AddLink(saysPinHow, "", "/raeume")
	} else {
		c.Note, c.NoteHref = saysPinMore, "/raeume" // how more get here, at the top (haus#64)
	}
	return c
}

// tile is one thing as a tile: its name leading to its page, its state in
// a word, on or off, a level for the bar, and a lamp's or a switch's
// switch, which lands back on back.
func tile(domain string, s ha.State, houseUnit, back string) cards.Row {
	r := cards.Row{Text: friendly(s), Href: cards.ThingPage(s.EntityID)}
	switch s.State {
	case "unavailable":
		r.State, r.Off = "Nicht erreichbar", true
		return r
	case "unknown", "":
		r.State, r.Off = "Unbekannt", true
		return r
	}
	switch domain {
	case "light", "switch":
		r.On, r.Off = s.State == "on", s.State == "off"
		r.State = capital(roomDetail(domain, s, houseUnit))
		if v, ok := number(s.Attributes["brightness"]); ok && r.On && domain == "light" {
			r.State, r.Level = "An", percent(v/255*100)
			r.Detail = strconv.Itoa(r.Level) + " %"
		}
		if r.On || r.Off {
			do := domain + ".turn_on"
			if r.On {
				do = domain + ".turn_off"
			}
			r.Action = &cards.Action{Label: friendly(s), Entity: s.EntityID, Do: do, Back: back, Switch: true, On: r.On}
		}
	case "cover":
		r.On, r.Off = s.State == "open" || s.State == "opening", s.State == "closed"
		r.State = capital(map[string]string{"open": "offen", "closed": "zu", "opening": "öffnet", "closing": "schließt", "stopped": "angehalten"}[s.State])
		if r.State == "" {
			r.State = capital(trimRunes(oneLine(s.State), 40))
		}
		if v, ok := number(s.Attributes["current_position"]); ok && r.On {
			r.Level = percent(v)
			r.Detail = strconv.Itoa(r.Level) + " %"
		}
	case "climate":
		unit := tempUnit(s, houseUnit)
		r.On, r.Off = s.State != "off", s.State == "off"
		cur, hasCur := number(s.Attributes["current_temperature"])
		target, hasTarget := number(s.Attributes["temperature"])
		switch {
		case r.Off:
			r.State = "Aus"
			if hasCur {
				r.Detail = decimal(cur) + " " + unit
			}
		case hasCur:
			r.State = decimal(cur) + " " + unit
			if hasTarget {
				r.Detail = "→ " + decimal(target) + " " + unit
			}
		case hasTarget:
			r.State = "→ " + decimal(target) + " " + unit
		default:
			r.State = "An"
		}
	case "media_player":
		r.On, r.Off = s.State == "playing", s.State == "off" || s.State == "standby"
		word := roomDetail(domain, s, houseUnit)
		if head, rest, ok := strings.Cut(word, ": "); ok {
			word, r.Detail = head, rest
		}
		r.State = capital(word)
	default: // a sensor, a door, a lock: its reading
		r.State = capital(roomDetail(domain, s, houseUnit))
	}
	return r
}

// percent is a share in whole percent, 1–100: what is on shows a bar.
func percent(v float64) int {
	return max(1, min(100, int(math.Round(v))))
}

// capital is the word with its first letter upper case: "an" → "An".
func capital(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 {
		return s
	}
	return string(unicode.ToUpper(r)) + s[n:]
}

// agenda answers "what comes up?" (Heute und demnächst): appointments and
// reminders together, by time — an overdue reminder first, one without a
// date last, "offen" — each leading to its page. Notes are not things
// that come; they have their own tab.
func agenda(events []caldav.Event, evErr error, todos []caldav.Todo, todoErr error, now time.Time, loc *time.Location) cards.Card {
	c := cards.Card{Title: "Heute und demnächst", Look: cards.Times}
	type item struct {
		at    time.Time
		dated bool
		row   cards.Row
	}
	var items []item
	if evErr == nil {
		for _, e := range events {
			when, detail := dayWord(e.Start, now, loc)+" "+e.Start.In(loc).Format("15:04"), trimRunes(oneLine(e.Location), 80)
			switch {
			case e.AllDay && e.Start.Before(now):
				when = "heute"
			case e.AllDay:
				when = dayWord(e.Start, now, loc)
			case e.Start.Before(now):
				when, detail = "jetzt", strings.TrimSuffix("bis "+e.End.In(loc).Format("15:04")+" · "+detail, " · ")
			}
			items = append(items, item{e.Start, true, cards.Row{Text: summary(e.Summary), Detail: detail, When: when, Href: "/kalender"}})
		}
	}
	if todoErr == nil {
		until := now.AddDate(0, 0, horizon)
		for _, t := range todos {
			if !t.Due.IsZero() && !t.Due.Before(until) {
				continue
			}
			row := cards.Row{Text: summary(t.Summary), When: dueWords(t, now, loc), Href: "/kalender"}
			if uid := strings.TrimSpace(t.UID); uid != "" {
				row.Href = cards.ReminderPage(uid)
			}
			if t.Due.IsZero() {
				row.When = "offen"
			}
			items = append(items, item{t.Due, !t.Due.IsZero(), row})
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].dated != items[j].dated {
			return items[i].dated
		}
		return items[i].at.Before(items[j].at)
	})
	for i, it := range items {
		if i == maxAgenda {
			c.AddLink("und "+strconv.Itoa(len(items)-maxAgenda)+" weitere", "", "/kalender")
			break
		}
		c.Rows = append(c.Rows, it.row)
	}
	switch {
	case evErr != nil || todoErr != nil:
		c.Add(saysNoCalendar, "")
	case len(items) == 0:
		c.Add(saysNothingDue, saysAgendaHow)
	}
	return c
}

// saysNoSuchPage is what a path Haus does not serve says.
const saysNoSuchPage = "Diese Seite gibt es nicht."

// Elsewhere is "/" and every page path Haus does not serve (haus#12): "/"
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
