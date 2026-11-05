package caldav

import (
	"cmp"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"path"
	"sort"
	"strings"
	"time"
)

// The household's shared data for the phones' Homes account (haus#218): the
// app keeps an Android account "Homes" whose contacts and calendars every
// phone app sees. What counts as shared is what lives in Homes' own
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
	// for a phone that writes back (FamilyFeed's whole): the objects as
	// stored, and whether the phone may write into the calendar
	Objects  []Stored
	Writable bool
}

// feedID is a calendar's id in the feed: the last part of its path.
func feedID(col Collection) string { return path.Base(strings.TrimRight(urlPath(col.URL), "/")) }

// RemindersID names the calendar the phones show the reminders in: no
// collection of Radicale's, the open reminders of every shared calendar.
const RemindersID = "erinnerungen"

// familyCalendars is every calendar in Homes' account's home that holds
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
// time, recurring ones as they repeat. whole, for a phone that writes
// back, gives each calendar's objects as stored instead (a series whole,
// with its changed instances and alarms) and says where it may write.
func (c *Client) FamilyFeed(ctx context.Context, from, to time.Time, whole bool) ([]FeedCalendar, error) {
	cols, err := c.familyCalendars(ctx)
	if err != nil {
		return nil, err
	}
	var evs []Event
	objs := make([][]object, len(cols))
	errs := make([]error, len(cols))
	if whole {
		q := strings.Replace(allesBody, `name="KIND"/>`, `name="VEVENT"><C:time-range start="`+utc(from)+`" end="`+utc(to)+`"/></C:comp-filter>`, 1)
		parallel(len(cols), func(i int) { objs[i], errs[i] = c.objects(ctx, cols[i], q) })
		err = errors.Join(errs...)
	} else {
		evs, err = c.events(ctx, cols, from, to)
	}
	if err != nil {
		return nil, err
	}
	todos, err := c.OpenTodos(ctx)
	if err != nil {
		return nil, err
	}
	var out []FeedCalendar
	at := map[string]int{}
	for i, col := range cols {
		at[col.URL] = len(out)
		k := FeedCalendar{ID: feedID(col), Name: col.Name, Kind: kindOf(feedID(col)), Writable: whole && !isMirror(col.URL)}
		for _, o := range objs[i] {
			k.Objects = append(k.Objects, Stored{ETag: o.etag, Data: o.data})
		}
		out = append(out, k)
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
// X-HAUS-KALENDER (its id) with X-HAUS-ART (its kind; both keep the old
// name, the app's parser reads them, laib#1021). An expanded
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
		if k.Writable {
			fold(&b, "X-HOMES-SCHREIBBAR:TRUE")
		}
		feedEvents(&b, k.Events, now)
		for _, o := range k.Objects {
			tagged(&b, o, "VEVENT")
		}
		feedTodos(&b, k.Todos, now, loc)
		fold(&b, "END:VCALENDAR")
	}
	return b.String()
}

// feedEvents writes expanded appointments; a series' instances share
// their UID, so each carries a RECURRENCE-ID, its start.
func feedEvents(b *strings.Builder, evs []Event, now time.Time) {
	uids := map[string]int{}
	for _, e := range evs {
		uids[e.UID]++
	}
	for _, e := range evs {
		fold(b, "BEGIN:VEVENT")
		fold(b, "UID:"+EscapeText(e.UID))
		fold(b, "DTSTAMP:"+utc(now))
		if uids[e.UID] > 1 {
			fold(b, "RECURRENCE-ID"+when(e.Start, e.AllDay))
		}
		fold(b, "DTSTART"+when(e.Start, e.AllDay))
		fold(b, "DTEND"+when(e.End, e.AllDay))
		fold(b, "SUMMARY:"+EscapeText(e.Summary))
		if e.Location != "" {
			fold(b, "LOCATION:"+EscapeText(e.Location))
		}
		if e.Description != "" {
			fold(b, "DESCRIPTION:"+EscapeText(e.Description))
		}
		fold(b, "END:VEVENT")
	}
}

// feedTodos writes the reminders as appointments that ring.
func feedTodos(b *strings.Builder, todos []Todo, now time.Time, loc *time.Location) {
	for _, t := range todos {
		fold(b, "BEGIN:VEVENT")
		fold(b, "UID:"+EscapeText(t.UID))
		fold(b, "DTSTAMP:"+utc(now))
		switch {
		case t.DueAllDay:
			fold(b, "DTSTART"+when(t.Due, true))
			fold(b, "DTEND"+when(t.Due.AddDate(0, 0, 1), true))
		case t.RRule != "" && zoned(loc):
			fold(b, "DTSTART;TZID="+loc.String()+":"+t.Due.In(loc).Format("20060102T150405"))
			fold(b, "DTEND;TZID="+loc.String()+":"+t.Due.In(loc).Format("20060102T150405"))
		default:
			fold(b, "DTSTART"+when(t.Due, false))
			fold(b, "DTEND"+when(t.Due, false))
		}
		if t.RRule != "" {
			fold(b, "RRULE:"+t.RRule)
		}
		fold(b, "SUMMARY:"+EscapeText(t.Summary))
		if !t.DueAllDay {
			for _, l := range []string{"BEGIN:VALARM", "ACTION:DISPLAY", "DESCRIPTION:" + EscapeText(t.Summary), "TRIGGER:PT0S", "END:VALARM"} {
				fold(b, l)
			}
		}
		fold(b, "END:VEVENT")
	}
}

// tagged writes an object's comps as stored, each with X-HOMES-ETAG, the
// version a phone writes back over.
func tagged(b *strings.Builder, o Stored, comp string) {
	blocks(unfold(o.Data), comp, func(ls []string, block bool) {
		for i, l := range ls {
			if block && i == len(ls)-1 {
				fold(b, "X-HOMES-ETAG:"+EscapeText(o.ETag))
			}
			if block {
				fold(b, l)
			}
		}
	})
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

// FamilyCards is every card of the address books in Homes' account's home
// (and the configured one), each as its text in Radicale; partial says a
// cap cut the read short. tags, for a phone that writes back, gives each
// card that can be written back its X-HOMES-ETAG.
func (c *Client) FamilyCards(ctx context.Context, tags bool) (cards []string, partial bool, err error) {
	all, partial, err := c.Cards(ctx)
	if err != nil {
		return nil, false, err
	}
	for _, cd := range all {
		switch {
		case cd.data == "":
			cards = append(cards, cd.Contact.VCF(c.now())) // one of a file of several cards
		case tags && cd.etag != "":
			var b strings.Builder
			tagged(&b, Stored{ETag: cd.etag, Data: cd.data}, "VCARD")
			cards = append(cards, b.String())
		default:
			cards = append(cards, cd.data)
		}
	}
	return cards, partial, nil
}

// versionBody asks a home for its collections' kind and version.
const versionBody = `<?xml version="1.0" encoding="utf-8"?>
<D:propfind xmlns:D="DAV:" xmlns:CS="http://calendarserver.org/ns/">
  <D:prop><D:resourcetype/><CS:getctag/><D:sync-token/></D:prop>
</D:propfind>`

// FamilyVersions is a cheap fingerprint of what the phones' Homes account
// holds (haus#223): one PROPFIND of Homes' home for each collection's
// getctag (its sync-token where it has none), which Radicale changes with
// every write into it — Homes' own, a phone's over DAVx⁵, laib's kalender
// mirroring an abo-*. kontakte covers the address books, kalender the
// calendars, the reminders in them included; the lists stay out, as they
// do from the feed. Equal strings: nothing the phones show changed.
func (c *Client) FamilyVersions(ctx context.Context) (kontakte, kalender string, err error) {
	home, err := c.home()
	if err != nil {
		return "", "", err
	}
	var books, cals []string
	add := func(href string, ps multistatusProp) {
		v := cmp.Or(ps.CTag, ps.SyncToken)
		switch {
		case ps.ResourceType.AddressBook != nil:
			books = append(books, href+"="+v)
		case ps.ResourceType.Calendar != nil && !isList(href):
			cals = append(cals, href+"="+v)
		}
	}
	read := func(u, depth string) error {
		code, raw, err := c.do(ctx, "PROPFIND", u, depth, versionBody, nil)
		if err != nil {
			return err
		}
		if code == http.StatusNotFound {
			return nil
		}
		if code != http.StatusMultiStatus {
			return &ErrStatus{"PROPFIND", code}
		}
		var ms multistatus
		if err := xml.Unmarshal(raw, &ms); err != nil {
			return fmt.Errorf("Radicale: PROPFIND unlesbar: %w", err)
		}
		for _, r := range ms.Responses {
			for _, ps := range r.Propstat {
				add(r.Href, ps.Prop)
			}
		}
		return nil
	}
	if err := read(home, "1"); err != nil {
		return "", "", err
	}
	for _, conf := range []string{c.Calendar, c.AddressBook} {
		if conf == "" {
			continue
		}
		u, err := c.resolve(strings.TrimRight(conf, "/") + "/")
		if err != nil {
			return "", "", err
		}
		if err := read(u, "0"); err != nil {
			return "", "", err
		}
	}
	sort.Strings(books)
	sort.Strings(cals)
	return strings.Join(books, "\n"), strings.Join(cals, "\n"), nil
}
