package caldav

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/haus/internal/caldav/davfake"
)

func phoneEvent(uid, start, end, summary string) string {
	return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:" + uid + "\r\nDTSTART:" + start + "\r\nDTEND:" + end + "\r\nSUMMARY:" + summary + "\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
}

// The account's calendars come from PROPFIND; appointments go to the first
// that takes VEVENT, reminders to the first that takes VTODO (DAVx⁵ often
// makes a separate task list).
func TestDiscoveryByComponent(t *testing.T) {
	fake := davfake.New("bote", "pw")
	defer fake.Close()
	fake.AddCalendar("/bote/aufgaben/", "Aufgaben", "VTODO")
	fake.AddCalendar("/bote/familie/", "Familie", "VEVENT")
	c := &Client{URL: fake.URL, User: "bote", Password: "pw", Loc: berlin(t)}
	ctx := context.Background()
	col, err := c.PutEvent(ctx, Event{Summary: "Zahnarzt", Start: time.Date(2026, 10, 14, 13, 0, 0, 0, time.UTC), End: time.Date(2026, 10, 14, 14, 0, 0, 0, time.UTC)})
	if err != nil || col.Name != "Familie" {
		t.Fatalf("event: %+v %v", col, err)
	}
	col, err = c.PutTodo(ctx, Todo{Summary: "Müll", Due: time.Date(2026, 10, 14, 18, 0, 0, 0, time.UTC)})
	if err != nil || col.Name != "Aufgaben" {
		t.Fatalf("todo: %+v %v", col, err)
	}
	var ev, td int
	for p, ics := range fake.Objects() {
		switch {
		case strings.HasPrefix(p, "/bote/familie/") && strings.HasSuffix(p, ".ics") && strings.Contains(ics, "BEGIN:VEVENT"):
			ev++
		case strings.HasPrefix(p, "/bote/aufgaben/") && strings.Contains(ics, "BEGIN:VTODO"):
			td++
		default:
			t.Fatalf("stray object %s", p)
		}
	}
	if ev != 1 || td != 1 {
		t.Fatalf("objects: %v", fake.Objects())
	}
	for _, h := range fake.Header {
		if h.Get("If-None-Match") != "*" || !strings.HasPrefix(h.Get("Content-Type"), "text/calendar") {
			t.Fatalf("a PUT never replaces: %v", h)
		}
	}
}

// A fresh service account has no calendar: the first write makes
// "Haushalt" (VEVENT and VTODO); reading an empty account is no error.
func TestFreshAccountGetsAHaushaltCalendar(t *testing.T) {
	fake := davfake.New("bote", "pw")
	defer fake.Close()
	c := &Client{URL: fake.URL, User: "bote", Password: "pw"}
	ctx := context.Background()
	if evs, _, err := c.Events(ctx, time.Now(), time.Now().Add(time.Hour)); err != nil || len(evs) != 0 {
		t.Fatalf("empty account: %v %v", evs, err)
	}
	if got := fake.Calendars(); len(got) != 0 {
		t.Fatalf("reading creates nothing: %v", got)
	}
	col, err := c.PutTodo(ctx, Todo{Summary: "x", Due: time.Now()})
	if err != nil || col.Name != "Haushalt" {
		t.Fatalf("todo: %+v %v", col, err)
	}
	if got := fake.Calendars(); len(got) != 1 || got[0] != "/bote/haushalt/" {
		t.Fatalf("calendars: %v", got)
	}
	if _, err := c.PutEvent(ctx, Event{Summary: "y", Start: time.Now(), End: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if got := fake.Calendars(); len(got) != 1 {
		t.Fatalf("one Haushalt calendar for both: %v", got)
	}
}

// A configured collection is used as is, no discovery.
func TestConfiguredCalendar(t *testing.T) {
	fake := davfake.New("bote", "pw")
	defer fake.Close()
	fake.AddCalendar("/familie/kalender/", "Kalender")
	c := &Client{URL: fake.URL, User: "bote", Password: "pw", Calendar: "/familie/kalender/"}
	if _, err := c.PutEvent(context.Background(), Event{Summary: "y", Start: time.Now(), End: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	for _, m := range fake.Methods {
		if strings.HasPrefix(m, "PROPFIND") {
			t.Fatalf("no discovery with HAUS_RADICALE_CALENDAR: %v", fake.Methods)
		}
	}
}

// A range is a REPORT calendar-query with the range in UTC, recurrences
// expanded by the server; what comes back outside the range is dropped.
func TestEventsInARange(t *testing.T) {
	fake := davfake.New("bote", "pw")
	defer fake.Close()
	fake.AddCalendar("/bote/familie/", "Familie", "VEVENT", "VTODO")
	fake.Put("/bote/familie/b.ics", phoneEvent("b", "20261014T150000Z", "20261014T160000Z", "später"))
	fake.Put("/bote/familie/a.ics", phoneEvent("a", "20261014T070000Z", "20261014T080000Z", "früher"))
	fake.Put("/bote/familie/out.ics", phoneEvent("out", "20261020T070000Z", "20261020T080000Z", "nächste Woche"))
	fake.Put("/bote/familie/t.ics", "BEGIN:VCALENDAR\r\nBEGIN:VTODO\r\nUID:t\r\nSUMMARY:kein Termin\r\nEND:VTODO\r\nEND:VCALENDAR\r\n")
	loc := berlin(t)
	c := &Client{URL: fake.URL, User: "bote", Password: "pw", Loc: loc}
	from := time.Date(2026, 10, 14, 0, 0, 0, 0, loc)
	evs, col, err := c.Events(context.Background(), from, from.AddDate(0, 0, 1))
	if err != nil || col.Name != "Familie" {
		t.Fatal(col, err)
	}
	if len(evs) != 2 || evs[0].Summary != "früher" || evs[1].Summary != "später" {
		t.Fatalf("sorted, in range: %+v", evs)
	}
	q := fake.Reports[len(fake.Reports)-1]
	for _, want := range []string{`calendar-query`, `<C:comp-filter name="VEVENT">`, `<C:time-range start="20261013T220000Z" end="20261014T220000Z"/>`, `<C:expand start="20261013T220000Z"`} {
		if !strings.Contains(q, want) {
			t.Fatalf("REPORT lacks %q:\n%s", want, q)
		}
	}
}

// Open reminders: not completed, not cancelled; soonest first, undated last.
func TestOpenTodos(t *testing.T) {
	fake := davfake.New("bote", "pw")
	defer fake.Close()
	fake.AddCalendar("/bote/familie/", "Familie")
	todo := func(uid, extra string) string {
		return "BEGIN:VCALENDAR\r\nBEGIN:VTODO\r\nUID:" + uid + "\r\nSUMMARY:" + uid + "\r\n" + extra + "END:VTODO\r\nEND:VCALENDAR\r\n"
	}
	fake.Put("/bote/familie/1.ics", todo("ohne", ""))
	fake.Put("/bote/familie/2.ics", todo("spaet", "DUE:20261020T100000Z\r\n"))
	fake.Put("/bote/familie/3.ics", todo("frueh", "DUE:20261015T100000Z\r\n"))
	fake.Put("/bote/familie/4.ics", todo("fertig", "STATUS:COMPLETED\r\nDUE:20261001T100000Z\r\n"))
	fake.Put("/bote/familie/5.ics", todo("abgesagt", "STATUS:CANCELLED\r\n"))
	c := &Client{URL: fake.URL, User: "bote", Password: "pw"}
	ts, _, err := c.OpenTodos(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, x := range ts {
		names = append(names, x.Summary)
	}
	if strings.Join(names, ",") != "frueh,spaet,ohne" {
		t.Fatalf("open todos: %v", names)
	}
	if q := fake.Reports[0]; !strings.Contains(q, `<C:comp-filter name="VTODO">`) || !strings.Contains(q, `<C:prop-filter name="COMPLETED"><C:is-not-defined/>`) {
		t.Fatalf("REPORT: %s", q)
	}
}

// A wrong password is an error that says so, not an empty calendar.
func TestWrongPassword(t *testing.T) {
	fake := davfake.New("bote", "pw")
	defer fake.Close()
	c := &Client{URL: fake.URL, User: "bote", Password: "nope"}
	_, _, err := c.Events(context.Background(), time.Now(), time.Now().Add(time.Hour))
	if err == nil || !strings.Contains(err.Error(), "Anmeldung abgelehnt") {
		t.Fatalf("want the refusal, got %v", err)
	}
	if strings.Contains(err.Error(), "nope") {
		t.Fatal("the password never goes into a message")
	}
}
