package pages

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/mdopp/haus/internal/caldav"
	"github.com/mdopp/haus/internal/cards"
)

// maxCalendarRows is the most one Kalender card lists.
const maxCalendarRows = 50

// Kalender is today, the next 14 days and the open reminders, from the
// household's Radicale account (haus#12, laib ADR 0017) — every calendar
// it can read, like the Bote's tools. Radicale not answering is Start's
// plain line, once (haus#33): for the appointments where their cards
// would be, for the reminders alone in their card; its error goes to the
// log.
func Kalender(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !showOnly(w, r, "Der Kalender") {
			return
		}
		if cfg.Calendar == nil {
			notSetCalendar.write(w, r, cfg, "Kalender")
			return
		}
		now, loc := cfg.clock()
		y, m, d := now.Date()
		today := time.Date(y, m, d, 0, 0, 0, 0, loc)
		tomorrow := today.AddDate(0, 0, 1)
		var (
			events  []caldav.Event
			evErr   error
			todos   []caldav.Todo
			todoErr error
		)
		fetch(r,
			func(ctx context.Context) {
				// today and the 14 days after it: [today 00:00, the 15th day 00:00)
				events, evErr = cfg.Calendar.Events(ctx, today, today.AddDate(0, 0, horizon+1))
			},
			func(ctx context.Context) { todos, todoErr = cfg.Calendar.OpenTodos(ctx) })
		logErrs("kalender", map[string]error{"calendar": evErr, "reminders": todoErr})

		b := familyHead(r, cfg, "Kalender", "Kalender")
		b.WriteString("<h1>Kalender</h1>")
		if evErr != nil {
			b.WriteString("<p class=k>" + saysNoCalendar + "</p>")
		} else {
			heute := cards.Card{Title: "Heute"}
			next := cards.Card{Title: "Nächste 14 Tage"}
			nToday, nNext := 0, 0
			for _, e := range events {
				if e.Start.Before(tomorrow) {
					if nToday++; nToday <= maxCalendarRows {
						heute.Add(summary(e.Summary), withPlace(todaySpan(e, today, tomorrow, loc), e.Location))
					}
					continue
				}
				if nNext++; nNext <= maxCalendarRows {
					when := dayWord(e.Start, now, loc) + " " + e.Start.In(loc).Format("15:04")
					if e.AllDay {
						when = dayWord(e.Start, now, loc) + " ganztägig"
					}
					next.Add(summary(e.Summary), withPlace(when, e.Location))
				}
			}
			more(&heute, nToday-maxCalendarRows)
			more(&next, nNext-maxCalendarRows)
			if heute.Empty() {
				heute.Add("Heute keine Termine", "")
			}
			if next.Empty() {
				next.Add("Keine Termine in den nächsten 14 Tagen", "")
			}
			b.WriteString(heute.HTML())
			b.WriteString(next.HTML())
		}
		switch {
		case todoErr != nil && evErr == nil: // the calendar is said once
			rem := cards.Card{Title: "Erinnerungen"}
			rem.Add(saysNoCalendar, "")
			b.WriteString(rem.HTML())
		case todoErr == nil:
			rem := cards.Card{Title: "Erinnerungen"}
			for i, t := range todos {
				if i == maxCalendarRows {
					break
				}
				if uid := strings.TrimSpace(t.UID); uid != "" {
					rem.AddLink(summary(t.Summary), dueWords(t, now, loc), cards.ReminderPage(uid))
				} else {
					rem.Add(summary(t.Summary), dueWords(t, now, loc))
				}
			}
			more(&rem, len(todos)-maxCalendarRows)
			if rem.Empty() {
				rem.Add("Keine offenen Erinnerungen", "")
			}
			b.WriteString(rem.HTML())
		}
		writePage(w, http.StatusOK, b)
	}
}

// todaySpan is an appointment's time within today: 13:00–14:00, ab 20:00,
// bis 11:00, ganztägig.
func todaySpan(e caldav.Event, today, tomorrow time.Time, loc *time.Location) string {
	if e.AllDay {
		return "ganztägig"
	}
	from, to := "", ""
	if !e.Start.Before(today) {
		from = e.Start.In(loc).Format("15:04")
	}
	if e.End.Before(tomorrow) && !e.End.Equal(e.Start) {
		to = e.End.In(loc).Format("15:04")
	}
	switch {
	case from != "" && to != "":
		return from + "–" + to
	case from != "":
		if e.End.Equal(e.Start) {
			return from
		}
		return "ab " + from
	case to != "":
		return "bis " + to
	}
	return "den ganzen Tag"
}

// dueWords is a reminder's due time: morgen 07:00, überfällig seit Mo
// 12.10. 09:00; for one due on a day the day only (heute, Fr 16.10.),
// overdue once that day is past; nothing without a due date.
func dueWords(t caldav.Todo, now time.Time, loc *time.Location) string {
	if t.Due.IsZero() {
		return ""
	}
	if t.DueAllDay {
		y, m, d := now.In(loc).Date()
		dy, dm, dd := t.Due.In(loc).Date()
		day := dayWord(t.Due, now, loc)
		if time.Date(dy, dm, dd, 0, 0, 0, 0, loc).Before(time.Date(y, m, d, 0, 0, 0, 0, loc)) {
			return "überfällig seit " + day
		}
		return day
	}
	at := dayWord(t.Due, now, loc) + " " + t.Due.In(loc).Format("15:04")
	if t.Due.Before(now) {
		return "überfällig seit " + at
	}
	return at
}

// withPlace adds where, when there is a where.
func withPlace(when, place string) string {
	if p := trimRunes(oneLine(place), 80); p != "" {
		return strings.TrimSpace(when + " · " + p)
	}
	return when
}
