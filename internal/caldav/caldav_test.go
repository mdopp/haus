package caldav

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/haus/internal/caldav/davfake"
)

func phoneEvent(uid, start, end, summary string) string {
	return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:" + uid + "\r\nDTSTART:" + start + "\r\nDTEND:" + end + "\r\nSUMMARY:" + summary + "\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
}

func count(methods []string, prefix string) int {
	n := 0
	for _, m := range methods {
		if strings.HasPrefix(m, prefix) {
			n++
		}
	}
	return n
}

// Writes go to the calendar named "Haushalt", never to whichever sorts
// first: a birthdays calendar or a task list must not catch the Bote's
// appointments.
func TestWritesGoToHaushalt(t *testing.T) {
	fake := davfake.New("bote", "pw")
	defer fake.Close()
	fake.AddCalendar("/bote/a-birthdays/", "Geburtstage", "VEVENT")
	fake.AddCalendar("/bote/aufgaben/", "Aufgaben", "VTODO")
	fake.AddCalendar("/bote/zz/", "Haushalt", "VEVENT", "VTODO")
	c := &Client{URL: fake.URL, User: "bote", Password: "pw", Loc: berlin(t)}
	ctx := context.Background()
	col, err := c.PutEvent(ctx, Event{Summary: "Zahnarzt", Start: time.Date(2026, 10, 14, 13, 0, 0, 0, time.UTC), End: time.Date(2026, 10, 14, 14, 0, 0, 0, time.UTC)})
	if err != nil || col.Name != "Haushalt" {
		t.Fatalf("event: %+v %v", col, err)
	}
	col, err = c.PutTodo(ctx, Todo{Summary: "Müll", Due: time.Date(2026, 10, 14, 18, 0, 0, 0, time.UTC)})
	if err != nil || col.Name != "Haushalt" {
		t.Fatalf("todo: %+v %v", col, err)
	}
	for p := range fake.Objects() {
		if !strings.HasPrefix(p, "/bote/zz/") || !strings.HasSuffix(p, ".ics") {
			t.Fatalf("stray object %s", p)
		}
	}
	if len(fake.Objects()) != 2 || count(fake.Methods, "MKCALENDAR") != 0 {
		t.Fatalf("objects %v, methods %v", fake.Objects(), fake.Methods)
	}
	for _, h := range fake.Header {
		if h.Get("If-None-Match") != "*" || !strings.HasPrefix(h.Get("Content-Type"), "text/calendar") {
			t.Fatalf("a PUT never replaces: %v", h)
		}
	}
}

// Without a "Haushalt" calendar the first write makes one, beside the
// account's other calendars.
func TestNoHaushaltMakesOne(t *testing.T) {
	fake := davfake.New("bote", "pw")
	defer fake.Close()
	fake.AddCalendar("/bote/a-birthdays/", "Geburtstage")
	c := &Client{URL: fake.URL, User: "bote", Password: "pw"}
	col, err := c.PutEvent(context.Background(), Event{Summary: "y", Start: time.Now(), End: time.Now().Add(time.Hour)})
	if err != nil || col.Name != "Haushalt" || col.URL != fake.URL+"/bote/haushalt/" {
		t.Fatalf("%+v %v", col, err)
	}
	for p := range fake.Objects() {
		if !strings.HasPrefix(p, "/bote/haushalt/") {
			t.Fatalf("stray object %s", p)
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
	if evs, err := c.Events(ctx, time.Now(), time.Now().Add(time.Hour)); err != nil || len(evs) != 0 {
		t.Fatalf("empty account: %v %v", evs, err)
	}
	if got := fake.Calendars(); len(got) != 0 {
		t.Fatalf("reading creates nothing: %v", got)
	}
	col, err := c.PutTodo(ctx, Todo{Summary: "x", Due: time.Now()})
	if err != nil || col.Name != "Haushalt" {
		t.Fatalf("todo: %+v %v", col, err)
	}
	if _, err := c.PutEvent(ctx, Event{Summary: "y", Start: time.Now(), End: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if got := fake.Calendars(); len(got) != 1 || got[0] != "/bote/haushalt/" {
		t.Fatalf("one Haushalt calendar for both: %v", got)
	}
}

// Another client made "Haushalt" between our PROPFIND and our MKCALENDAR:
// the refusal (409 here, 405 on Radicale) means look again and use it.
func TestMkcalendarRace(t *testing.T) {
	fake := davfake.New("bote", "pw")
	defer fake.Close()
	fake.ExistsStatus = http.StatusConflict
	fake.CreateAfterPropfind("/bote/haushalt/", "Haushalt", "VEVENT", "VTODO")
	c := &Client{URL: fake.URL, User: "bote", Password: "pw"}
	col, err := c.PutTodo(context.Background(), Todo{Summary: "x", Due: time.Now()})
	if err != nil || col.Name != "Haushalt" {
		t.Fatalf("%+v %v (methods %v)", col, err, fake.Methods)
	}
	if count(fake.Methods, "PROPFIND") != 2 || count(fake.Methods, "PUT /bote/haushalt/") != 1 {
		t.Fatalf("rediscovered after the refusal: %v", fake.Methods)
	}

	// no right to make one, and none there: an error, not a write elsewhere
	fake2 := davfake.New("bote", "pw")
	defer fake2.Close()
	fake2.AddCalendar("/bote/a-birthdays/", "Geburtstage")
	fake2.MkcalendarStatus = http.StatusForbidden
	c2 := &Client{URL: fake2.URL, User: "bote", Password: "pw"}
	if _, err := c2.PutEvent(context.Background(), Event{Summary: "y", Start: time.Now(), End: time.Now().Add(time.Hour)}); err == nil {
		t.Fatal("want an error")
	}
	if len(fake2.Objects()) != 0 {
		t.Fatalf("written elsewhere: %v", fake2.Objects())
	}
}

// A configured collection takes the writes, no MKCALENDAR; it is read
// along with the account's own calendars.
func TestConfiguredCalendar(t *testing.T) {
	fake := davfake.New("bote", "pw")
	defer fake.Close()
	fake.AddCalendar("/familie/kalender/", "Kalender")
	fake.AddCalendar("/bote/geburtstage/", "Geburtstage")
	fake.Put("/bote/geburtstage/g.ics", phoneEvent("g", "20261014T100000Z", "20261014T110000Z", "Oma"))
	c := &Client{URL: fake.URL, User: "bote", Password: "pw", Calendar: "/familie/kalender/"}
	col, err := c.PutEvent(context.Background(), Event{Summary: "y", Start: time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC), End: time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)})
	if err != nil || col.URL != fake.URL+"/familie/kalender/" {
		t.Fatalf("%+v %v", col, err)
	}
	if count(fake.Methods, "MKCALENDAR") != 0 {
		t.Fatalf("methods: %v", fake.Methods)
	}
	evs, err := c.Events(context.Background(), time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC))
	if err != nil || len(evs) != 2 || evs[0].Summary != "y" || evs[1].Summary != "Oma" {
		t.Fatalf("reads the configured and the own calendars: %+v %v", evs, err)
	}
}

// termine reads every calendar that holds appointments, merged and sorted;
// a range is a REPORT calendar-query in UTC, recurrences expanded by the
// server; what comes back outside the range is dropped.
func TestEventsFromAllCalendars(t *testing.T) {
	fake := davfake.New("bote", "pw")
	defer fake.Close()
	fake.AddCalendar("/bote/a-birthdays/", "Geburtstage", "VEVENT")
	fake.AddCalendar("/bote/aufgaben/", "Aufgaben", "VTODO")
	fake.AddCalendar("/bote/haushalt/", "Haushalt", "VEVENT", "VTODO")
	fake.Put("/bote/haushalt/b.ics", phoneEvent("b", "20261014T150000Z", "20261014T160000Z", "später"))
	fake.Put("/bote/a-birthdays/a.ics", phoneEvent("a", "20261014T070000Z", "20261014T080000Z", "früher"))
	fake.Put("/bote/haushalt/out.ics", phoneEvent("out", "20261020T070000Z", "20261020T080000Z", "nächste Woche"))
	fake.Put("/bote/haushalt/t.ics", "BEGIN:VCALENDAR\r\nBEGIN:VTODO\r\nUID:t\r\nSUMMARY:kein Termin\r\nEND:VTODO\r\nEND:VCALENDAR\r\n")
	loc := berlin(t)
	c := &Client{URL: fake.URL, User: "bote", Password: "pw", Loc: loc}
	from := time.Date(2026, 10, 14, 0, 0, 0, 0, loc)
	evs, err := c.Events(context.Background(), from, from.AddDate(0, 0, 1))
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 2 || evs[0].Summary != "früher" || evs[1].Summary != "später" {
		t.Fatalf("both calendars, sorted, in range: %+v", evs)
	}
	if count(fake.Methods, "REPORT /bote/aufgaben/") != 0 {
		t.Fatalf("a task list holds no appointments: %v", fake.Methods)
	}
	q := fake.Reports[len(fake.Reports)-1]
	for _, want := range []string{`calendar-query`, `<C:comp-filter name="VEVENT">`, `<C:time-range start="20261013T220000Z" end="20261014T220000Z"/>`, `<C:expand start="20261013T220000Z"`} {
		if !strings.Contains(q, want) {
			t.Fatalf("REPORT lacks %q:\n%s", want, q)
		}
	}
}

// Open reminders from every task calendar: not completed, not cancelled;
// soonest first, undated last.
func TestOpenTodos(t *testing.T) {
	fake := davfake.New("bote", "pw")
	defer fake.Close()
	fake.AddCalendar("/bote/haushalt/", "Haushalt")
	fake.AddCalendar("/bote/aufgaben/", "Aufgaben", "VTODO")
	todo := func(uid, extra string) string {
		return "BEGIN:VCALENDAR\r\nBEGIN:VTODO\r\nUID:" + uid + "\r\nSUMMARY:" + uid + "\r\n" + extra + "END:VTODO\r\nEND:VCALENDAR\r\n"
	}
	fake.Put("/bote/haushalt/1.ics", todo("ohne", ""))
	fake.Put("/bote/haushalt/2.ics", todo("spaet", "DUE:20261020T100000Z\r\n"))
	fake.Put("/bote/aufgaben/3.ics", todo("frueh", "DUE:20261015T100000Z\r\n"))
	fake.Put("/bote/haushalt/4.ics", todo("fertig", "STATUS:COMPLETED\r\nDUE:20261001T100000Z\r\n"))
	fake.Put("/bote/haushalt/5.ics", todo("abgesagt", "STATUS:CANCELLED\r\n"))
	c := &Client{URL: fake.URL, User: "bote", Password: "pw"}
	ts, err := c.OpenTodos(context.Background())
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

// An href naming another host is not followed: Basic auth only ever goes
// to the Radicale address configured.
func TestForeignHrefIgnored(t *testing.T) {
	fake := davfake.New("bote", "pw")
	defer fake.Close()
	fake.AddCalendar("http://evil.invalid/bote/h/", "Haushalt")
	c := &Client{URL: fake.URL, User: "bote", Password: "pw"}
	if _, err := c.Events(context.Background(), time.Now(), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("a foreign calendar is skipped, not read: %v", err)
	}
	col, err := c.PutEvent(context.Background(), Event{Summary: "y", Start: time.Now(), End: time.Now().Add(time.Hour)})
	if err != nil || !strings.HasPrefix(col.URL, fake.URL+"/") {
		t.Fatalf("write went to %+v %v", col, err)
	}
	bad := &Client{URL: fake.URL, User: "bote", Password: "pw", Calendar: "http://evil.invalid/x/"}
	if _, err := bad.PutEvent(context.Background(), Event{Summary: "y", Start: time.Now(), End: time.Now().Add(time.Hour)}); err == nil {
		t.Fatal("a configured calendar on another host is refused")
	}
}

// A wrong password is an error that says so, not an empty calendar.
func TestWrongPassword(t *testing.T) {
	fake := davfake.New("bote", "pw")
	defer fake.Close()
	c := &Client{URL: fake.URL, User: "bote", Password: "nope"}
	_, err := c.Events(context.Background(), time.Now(), time.Now().Add(time.Hour))
	if err == nil || !strings.Contains(err.Error(), "Anmeldung abgelehnt") {
		t.Fatalf("want the refusal, got %v", err)
	}
	if strings.Contains(err.Error(), "nope") {
		t.Fatal("the password never goes into a message")
	}
}
