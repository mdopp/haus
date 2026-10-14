package pages

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mdopp/haus/internal/caldav"
	"github.com/mdopp/haus/internal/cards"
	"github.com/mdopp/haus/internal/ha"
	"github.com/mdopp/haus/internal/laib"
	"github.com/mdopp/haus/internal/notes"
)

// StartConfig is what the Start page reads. A nil backend is not
// configured, and the page says nothing about it.
type StartConfig struct {
	Laib     *laib.Client
	LaibURL  string // where Rückgängig lives (the guarantees, admins only)
	HA       *ha.Client
	Calendar *caldav.Client
	Notes    *notes.Dir
	Loc      *time.Location   // the house's time zone; default time.Local
	Now      func() time.Time // for the tests; default time.Now
}

// backendWait is how long the page waits for any one backend: a slow one
// is left out, the page still comes.
const backendWait = 4 * time.Second

const (
	maxNames    = 5  // light names in the Jetzt row
	maxPlaying  = 3  // media players in Jetzt
	maxHaushalt = 3  // reminders, and notes, in Haushalt
	horizon     = 14 // days ahead for the next appointment
)

// Start is the family's page (haus#12, laib DEKONSTRUKTION): the persona's
// greeting, the three guarantees, then Jetzt and Haushalt. It shows and
// changes nothing (haus#1). A backend that is not configured or not
// answering is left out; its error goes to the log, not to the family.
func Start(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "Die Startseite zeigt nur, sie ändert nichts.", http.StatusMethodNotAllowed)
			return
		}
		loc := cfg.Loc
		if loc == nil {
			loc = time.Local
		}
		now := time.Now()
		if cfg.Now != nil {
			now = cfg.Now()
		}
		now = now.In(loc)

		var (
			wg       sync.WaitGroup
			ready    laib.Readiness
			services []laib.Service
			laibErr  error
			states   []ha.State
			haErr    error
			events   []caldav.Event
			evErr    error
			todos    []caldav.Todo
			todoErr  error
			recent   []notes.Entry
			notesErr error
		)
		run := func(f func(context.Context)) {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ctx, cancel := context.WithTimeout(r.Context(), backendWait)
				defer cancel()
				f(ctx)
			}()
		}
		if cfg.Laib != nil {
			run(func(ctx context.Context) { ready, services, laibErr = cfg.Laib.Status(ctx) })
		}
		if cfg.HA != nil {
			run(func(ctx context.Context) { states, haErr = cfg.HA.States(ctx) })
		}
		if cfg.Calendar != nil {
			run(func(ctx context.Context) { events, evErr = cfg.Calendar.Events(ctx, now, now.AddDate(0, 0, horizon)) })
			run(func(ctx context.Context) { todos, todoErr = cfg.Calendar.OpenTodos(ctx) })
		}
		if cfg.Notes != nil {
			run(func(ctx context.Context) { recent, notesErr = cfg.Notes.Recent(ctx, maxHaushalt) })
		}
		wg.Wait()
		for what, err := range map[string]error{"laib": laibErr, "home assistant": haErr, "calendar": evErr, "reminders": todoErr, "notes": notesErr} {
			if err != nil {
				log.Printf("haus: start page: %s: %v", what, err)
			}
		}

		var b strings.Builder
		b.WriteString(head("Start"))
		b.WriteString(cards.Style)
		b.WriteString("<style>.sr{position:absolute;width:1px;height:1px;overflow:hidden;clip:rect(0 0 0 0);white-space:nowrap}</style>")
		b.WriteString("<nav><a href=/start aria-current=page>Start</a>")
		if isAdmin(r) {
			b.WriteString("<a href=/box>Box</a>")
		}
		b.WriteString("</nav><h1 class=sr>Start</h1>")
		switch {
		case cfg.Laib == nil:
		case laibErr != nil:
			b.WriteString("<p class=k>Wie es der Box geht, lässt sich gerade nicht sagen.</p>")
		default:
			b.WriteString(guarantees(ready, services, isAdmin(r), cfg.LaibURL))
		}

		jetzt := cards.Card{Title: "Jetzt"}
		if cfg.HA != nil && haErr == nil {
			home(&jetzt, states)
		}
		if cfg.Calendar != nil && evErr == nil && len(events) > 0 {
			e := events[0]
			jetzt.Add("Nächster Termin: "+summary(e.Summary), eventWhen(e, now, loc))
		}
		b.WriteString(jetzt.HTML())

		haushalt := cards.Card{Title: "Haushalt"}
		if cfg.Calendar != nil && todoErr == nil {
			for i, t := range todos {
				if i == maxHaushalt {
					break
				}
				d := "Erinnerung"
				if !t.Due.IsZero() {
					d += " · " + dayWord(t.Due, now, loc) + " " + t.Due.In(loc).Format("15:04")
				}
				haushalt.Add(summary(t.Summary), d)
			}
		}
		if cfg.Notes != nil && notesErr == nil {
			for _, n := range recent {
				haushalt.Add(summary(n.Title), "Notiz · geändert "+dayWord(n.Changed, now, loc)+" "+n.Changed.In(loc).Format("15:04"))
			}
		}
		b.WriteString(haushalt.HTML())

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(b.String()))
	}
}

// ToStart sends every page path Haus does not serve to Start (haus#12): an
// installed app's route never ends on an error. The app's hash routes
// (/#/p/start, …) arrive as "/", the fragment stays in the browser.
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
// what plays — in that order (laib DEKONSTRUKTION).
func home(c *cards.Card, states []ha.State) {
	var persons, homeNames, lights []string
	type playing struct{ name, what string }
	var plays []playing
	for _, s := range states {
		if _, group := s.Attributes["entity_id"].([]any); group {
			continue // a group (light groups too) is not a lamp; its members count
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
