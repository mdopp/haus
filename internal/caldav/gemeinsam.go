package caldav

import (
	"context"
	"net/url"
	"path"
	"strings"
	"time"
)

// The household's shared data for the phones' Haus account (haus#218): the
// app keeps an Android account "Haus" whose contacts and calendars every
// phone app sees. What counts as shared is what lives in Haus's own
// account's home — the Haushalt calendar and address book, the calendars
// "für die Familie" (familie-*) and the family's subscriptions (abo-*) —
// never a person's own calendars or mirrors (privat-*, google-*, a person's
// abo-*): a person's own data goes from the phone to their archive, not
// back to everyone's phone. Lists (the shopping list, liste-*) are no
// calendars and stay out.

// FeedCalendar is one shared calendar as the phones get it: its id (the
// last part of its path), its name, its kind (kindOf), and what is in it.
type FeedCalendar struct {
	ID, Name, Kind string
	Events         []Event
	Todos          []Todo // the reminders' calendar only
}

// RemindersID names the calendar the phones show the reminders in: no
// collection of Radicale's, the open reminders of every shared calendar.
const RemindersID = "erinnerungen"

// familyCalendars is every calendar in Haus's account's home that holds
// appointments, and the configured one.
func (c *Client) familyCalendars(ctx context.Context) ([]Collection, error) {
	cols, err := c.list(ctx, false)
	if err != nil {
		return nil, err
	}
	var out []Collection
	seen := map[string]bool{}
	if c.Calendar != "" {
		col, err := c.configured(c.calendars())
		if err != nil {
			return nil, err
		}
		out, seen[col.URL] = append(out, col), true
	}
	for _, col := range cols {
		if col.Takes("VEVENT") && !seen[col.URL] {
			out, seen[col.URL] = append(out, col), true
		}
	}
	return out, nil
}

// FamilyFeed is the shared calendars with their appointments overlapping
// [from, to), recurring ones expanded by Radicale, and last the reminders'
// calendar: every open reminder of the shared calendars that has a due
// time, recurring ones as they repeat.
func (c *Client) FamilyFeed(ctx context.Context, from, to time.Time) ([]FeedCalendar, error) {
	cols, err := c.familyCalendars(ctx)
	if err != nil {
		return nil, err
	}
	evs, err := c.events(ctx, cols, from, to)
	if err != nil {
		return nil, err
	}
	todos, err := c.OpenTodos(ctx)
	if err != nil {
		return nil, err
	}
	var out []FeedCalendar
	at := map[string]int{}
	for _, col := range cols {
		u, _ := url.Parse(col.URL)
		id := path.Base(strings.TrimRight(u.Path, "/"))
		at[col.URL] = len(out)
		out = append(out, FeedCalendar{ID: id, Name: col.Name, Kind: kindOf(id)})
	}
	for _, e := range evs {
		if i, ok := at[e.Calendar]; ok {
			out[i].Events = append(out[i].Events, e)
		}
	}
	rem := FeedCalendar{ID: RemindersID, Name: "Erinnerungen", Kind: RemindersID}
	for _, t := range todos {
		if !t.Due.IsZero() {
			rem.Todos = append(rem.Todos, t)
		}
	}
	return append(out, rem), nil
}

// FeedICS renders the shared calendars as one iCalendar stream (RFC 5545
// §3.4: one VCALENDAR after the other), each named by X-WR-CALNAME and
// X-HAUS-KALENDER (its id) with X-HAUS-ART (its kind). An expanded
// series' instances share their UID, so each carries a RECURRENCE-ID, its
// start. A reminder is a VEVENT at its due time with a display alarm then
// (a whole-day one without), a recurring one with its RRULE in loc, the
// house's time zone, so it keeps its hour across daylight saving.
func FeedICS(cals []FeedCalendar, now time.Time, loc *time.Location) string {
	var b strings.Builder
	for _, k := range cals {
		for _, l := range []string{"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:" + prodID,
			"X-WR-CALNAME:" + EscapeText(k.Name), "X-HAUS-KALENDER:" + EscapeText(k.ID), "X-HAUS-ART:" + EscapeText(k.Kind)} {
			fold(&b, l)
		}
		uids := map[string]int{}
		for _, e := range k.Events {
			uids[e.UID]++
		}
		for _, e := range k.Events {
			fold(&b, "BEGIN:VEVENT")
			fold(&b, "UID:"+EscapeText(e.UID))
			fold(&b, "DTSTAMP:"+utc(now))
			if uids[e.UID] > 1 {
				fold(&b, "RECURRENCE-ID"+when(e.Start, e.AllDay))
			}
			fold(&b, "DTSTART"+when(e.Start, e.AllDay))
			fold(&b, "DTEND"+when(e.End, e.AllDay))
			fold(&b, "SUMMARY:"+EscapeText(e.Summary))
			if e.Location != "" {
				fold(&b, "LOCATION:"+EscapeText(e.Location))
			}
			if e.Description != "" {
				fold(&b, "DESCRIPTION:"+EscapeText(e.Description))
			}
			fold(&b, "END:VEVENT")
		}
		for _, t := range k.Todos {
			fold(&b, "BEGIN:VEVENT")
			fold(&b, "UID:"+EscapeText(t.UID))
			fold(&b, "DTSTAMP:"+utc(now))
			switch {
			case t.DueAllDay:
				fold(&b, "DTSTART"+when(t.Due, true))
				fold(&b, "DTEND"+when(t.Due.AddDate(0, 0, 1), true))
			case t.RRule != "" && zoned(loc):
				fold(&b, "DTSTART;TZID="+loc.String()+":"+t.Due.In(loc).Format("20060102T150405"))
				fold(&b, "DTEND;TZID="+loc.String()+":"+t.Due.In(loc).Format("20060102T150405"))
			default:
				fold(&b, "DTSTART"+when(t.Due, false))
				fold(&b, "DTEND"+when(t.Due, false))
			}
			if t.RRule != "" {
				fold(&b, "RRULE:"+t.RRule)
			}
			fold(&b, "SUMMARY:"+EscapeText(t.Summary))
			if !t.DueAllDay {
				for _, l := range []string{"BEGIN:VALARM", "ACTION:DISPLAY", "DESCRIPTION:" + EscapeText(t.Summary), "TRIGGER:PT0S", "END:VALARM"} {
					fold(&b, l)
				}
			}
			fold(&b, "END:VEVENT")
		}
		fold(&b, "END:VCALENDAR")
	}
	return b.String()
}

// when is a DTSTART's or DTEND's parameters and value: a day as DATE, a
// time in UTC.
func when(t time.Time, allDay bool) string {
	if allDay {
		return ";VALUE=DATE:" + t.Format(dateLayout)
	}
	return ":" + utc(t)
}

// zoned is a time zone with an IANA name a phone knows.
func zoned(loc *time.Location) bool {
	return loc != nil && loc.String() != "Local" && loc.String() != "UTC" && loc.String() != ""
}

// FamilyCards is every card of the address books in Haus's account's home
// (and the configured one), each as its text in Radicale; partial says a
// cap cut the read short.
func (c *Client) FamilyCards(ctx context.Context) (cards []string, partial bool, err error) {
	all, partial, err := c.Cards(ctx)
	if err != nil {
		return nil, false, err
	}
	for _, cd := range all {
		if cd.data != "" {
			cards = append(cards, cd.data)
			continue
		}
		cards = append(cards, cd.Contact.VCF(c.now())) // one of a file of several cards
	}
	return cards, partial, nil
}
