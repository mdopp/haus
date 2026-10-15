package pages

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/mdopp/haus/internal/caldav"
	"github.com/mdopp/haus/internal/cards"
	"github.com/mdopp/haus/internal/favoriten"
	"github.com/mdopp/haus/internal/ha"
	"github.com/mdopp/haus/internal/laib"
	"github.com/mdopp/haus/internal/notes"
	"github.com/mdopp/haus/internal/paperless"
)

// StartConfig is what the family's pages read (Start, Räume, Kalender,
// Notizen). A nil backend is not configured, and no page says anything
// about it.
type StartConfig struct {
	Laib      *laib.Client
	LaibURL   string // where Rückgängig lives (the guarantees, admins only)
	HA        *ha.Client
	Calendar  *caldav.Client
	Contacts  *caldav.Client // a contact's page (haus#28)
	Notes     *notes.Dir
	Docs      *paperless.Client // a document's page sends on to Paperless (haus#28)
	Favorites *favoriten.Store  // each person's favorites on Start (laib ADR 0017)
	Loc       *time.Location    // the house's time zone; default time.Local
	Now       func() time.Time  // for the tests; default time.Now
}

// backendWait is how long a page waits for any one backend: a slow one
// is left out, the page still comes. A variable for the tests.
var backendWait = 4 * time.Second

const (
	maxNames     = 5  // light names in the Jetzt row
	maxPlaying   = 3  // media players in Jetzt
	maxHaushalt  = 3  // reminders, and notes, in Haushalt
	maxFavoriten = 12 // things in Meine Favoriten
	horizon      = 14 // days ahead for the next appointment
)

// Start is the family's page (haus#12, laib DEKONSTRUKTION): the persona's
// greeting, the three guarantees, then Jetzt, Meine Favoriten and
// Haushalt. It shows and changes nothing (haus#1). A backend that is not
// configured is not mentioned; one not answering is one plain line where
// its rows would be, its error in the log, not on the page.
func Start(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !showOnly(w, r, "Die Startseite") {
			return
		}
		now, loc := cfg.clock()

		var (
			ready    laib.Readiness
			services []laib.Service
			laibErr  error
			states   []ha.State
			haErr    error
			reg      ha.Registry
			events   []caldav.Event
			evErr    error
			todos    []caldav.Todo
			todoErr  error
			recent   []notes.Entry
			notesErr error
			favs     []string
			favErr   error
			reads    []func(context.Context)
		)
		person := r.Header.Get("Remote-User")
		if cfg.Laib != nil {
			reads = append(reads, func(ctx context.Context) { ready, services, laibErr = cfg.Laib.Status(ctx) })
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
		if cfg.Notes != nil {
			reads = append(reads, func(ctx context.Context) { recent, notesErr = cfg.Notes.Recent(ctx, maxHaushalt) })
		}
		if cfg.Favorites != nil && cfg.HA != nil && person != "" {
			reads = append(reads, func(context.Context) { favs, favErr = cfg.Favorites.For(person) })
		}
		fetch(r, reads...)
		logErrs("start", map[string]error{"laib": laibErr, "home assistant": haErr, "calendar": evErr, "reminders": todoErr, "notes": notesErr, "favorites": favErr})

		b := familyHead(r, cfg, "Start", "Start")
		b.WriteString("<h1 class=sr>Start</h1>")
		switch {
		case cfg.Laib == nil:
		case laibErr != nil:
			b.WriteString("<p class=k>Wie es der Box geht, lässt sich gerade nicht sagen.</p>")
		default:
			b.WriteString(guarantees(ready, services, isAdmin(r), cfg.LaibURL))
		}

		jetzt := cards.Card{Title: "Jetzt"}
		switch {
		case cfg.HA == nil:
		case haErr != nil:
			jetzt.Add(saysNoHouse, "")
		default:
			home(&jetzt, states, reg.Hidden)
			jetzt.Link = &cards.Link{Href: "/raeume", Text: "Alle Räume"}
		}
		switch {
		case cfg.Calendar == nil:
		case evErr != nil:
			jetzt.Add(saysNoCalendar, "")
		case len(events) > 0:
			e := events[0]
			jetzt.AddLink("Nächster Termin: "+summary(e.Summary), eventWhen(e, now, loc), "/kalender")
		}
		b.WriteString(jetzt.HTML())

		b.WriteString(favoriteCard(favs, favErr, states, haErr, reg).HTML())

		haushalt := cards.Card{Title: "Haushalt"}
		switch {
		case cfg.Calendar == nil:
		case todoErr != nil:
			if evErr == nil { // the calendar is said once
				haushalt.Add(saysNoCalendar, "")
			}
		default:
			for i, t := range todos {
				if i == maxHaushalt {
					break
				}
				d := "Erinnerung"
				if w := dueWords(t, now, loc); w != "" {
					d += " · " + w
				}
				haushalt.AddLink(summary(t.Summary), d, "/kalender")
			}
		}
		switch {
		case cfg.Notes == nil:
		case notesErr != nil:
			haushalt.Add(saysNoNotes, "")
		default:
			for _, n := range recent {
				haushalt.AddLink(summary(n.Title), "Notiz · geändert "+dayWord(n.Changed, now, loc)+" "+n.Changed.In(loc).Format("15:04"), cards.NotePage(n.Name))
			}
			haushalt.Link = &cards.Link{Href: "/notizen", Text: "Alle Notizen"}
		}
		b.WriteString(haushalt.HTML())
		writePage(w, http.StatusOK, b)
	}
}

// favoriteCard is Meine Favoriten (laib DEKONSTRUKTION, ADR 0017): the
// person's things in their order, each with its state in words and leading
// to its own page. Only what that page shows: a thing of a card domain
// that HA knows and does not hide. Nothing to show, no card.
func favoriteCard(ids []string, err error, states []ha.State, haErr error, reg ha.Registry) cards.Card {
	c := cards.Card{Title: "Meine Favoriten"}
	switch {
	case err != nil:
		c.Add(saysNoFavorites, "")
		return c
	case len(ids) == 0:
		return c
	case haErr != nil:
		c.Add(saysFavsUnseen, "")
		return c
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
		if n++; n <= maxFavoriten {
			c.AddLink(friendly(s), roomDetail(domain, s, reg.TempUnit), cards.ThingPage(id))
		}
	}
	more(&c, n-maxFavoriten)
	return c
}

// ToStart sends every page path Haus does not serve to Start (haus#12): an
// old bookmark never ends on an error.
func ToStart() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/start", http.StatusFound)
	}
}

// isAdmin reads Authelia's Remote-Groups.
func isAdmin(r *http.Request) bool {
	return strings.Contains(","+r.Header.Get("Remote-Groups")+",", ",admins,")
}

// home fills Jetzt from HA's states: who is home, the lights that are on,
// what plays — in that order (laib DEKONSTRUKTION). What HA hides is left
// out.
func home(c *cards.Card, states []ha.State, hidden map[string]bool) {
	var persons, homeNames, lights []string
	type playing struct{ name, what string }
	var plays []playing
	for _, s := range states {
		if _, group := s.Attributes["entity_id"].([]any); group {
			continue // a group (light groups too) is not a lamp; its members count
		}
		if hidden[s.EntityID] {
			continue
		}
		domain, _, _ := strings.Cut(s.EntityID, ".")
		switch {
		case domain == "person":
			persons = append(persons, s.EntityID)
			if s.State == "home" {
				homeNames = append(homeNames, friendly(s))
			}
		case domain == "light" && s.State == "on":
			lights = append(lights, friendly(s))
		case domain == "media_player" && s.State == "playing":
			title, _ := s.Attributes["media_title"].(string)
			artist, _ := s.Attributes["media_artist"].(string)
			what := oneLine(title)
			if a := oneLine(artist); a != "" {
				if what != "" {
					what += " · "
				}
				what += a
			}
			plays = append(plays, playing{friendly(s), what})
		}
	}
	if len(persons) > 0 {
		if len(homeNames) == 0 {
			c.Add("Niemand ist zu Hause", "")
		} else {
			sort.Strings(homeNames)
			c.Add("Zu Hause: "+strings.Join(homeNames, ", "), "")
		}
	}
	switch len(lights) {
	case 0:
		c.Add("Alle Lichter sind aus", "")
	case 1:
		c.Add("1 Licht an", lights[0])
	default:
		sort.Strings(lights)
		names := strings.Join(lights[:min(len(lights), maxNames)], ", ")
		if len(lights) > maxNames {
			names += fmt.Sprintf(" und %d weitere", len(lights)-maxNames)
		}
		c.Add(fmt.Sprintf("%d Lichter an", len(lights)), names)
	}
	sort.SliceStable(plays, func(i, j int) bool { return plays[i].name < plays[j].name })
	for i, p := range plays {
		if i == maxPlaying {
			break
		}
		c.Add("Läuft: "+p.name, p.what)
	}
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

// dayWord says a day the family way, relative to now: heute, morgen,
// gestern, else "Fr 16.10.".
func dayWord(t, now time.Time, loc *time.Location) string {
	t, now = t.In(loc), now.In(loc)
	y, m, d := t.Date()
	ny, nm, nd := now.Date()
	days := int(time.Date(y, m, d, 12, 0, 0, 0, time.UTC).Sub(time.Date(ny, nm, nd, 12, 0, 0, 0, time.UTC)).Hours() / 24)
	switch days {
	case 0:
		return "heute"
	case 1:
		return "morgen"
	case -1:
		return "gestern"
	}
	return [...]string{"So", "Mo", "Di", "Mi", "Do", "Fr", "Sa"}[t.Weekday()] + " " + t.Format("02.01.")
}

// eventWhen is an appointment's time: heute 15:00, morgen ganztägig,
// jetzt bis 11:00 when it already runs.
func eventWhen(e caldav.Event, now time.Time, loc *time.Location) string {
	if e.AllDay {
		if e.Start.Before(now) {
			return "heute ganztägig"
		}
		return dayWord(e.Start, now, loc) + " ganztägig"
	}
	if e.Start.Before(now) {
		return "jetzt, bis " + e.End.In(loc).Format("15:04")
	}
	return dayWord(e.Start, now, loc) + " " + e.Start.In(loc).Format("15:04")
}
