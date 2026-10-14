package caldav

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func berlin(t *testing.T) *time.Location {
	t.Helper()
	l, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// RFC 5545 §3.3.11: backslash, semicolon, comma and the newline are escaped
// in TEXT; what goes in comes back out.
func TestTextEscaping(t *testing.T) {
	in := "Arzt; Dr. Müller, Praxis\\2\nzweite Zeile"
	esc := EscapeText(in)
	if esc != `Arzt\; Dr. Müller\, Praxis\\2\nzweite Zeile` {
		t.Fatalf("escaped: %q", esc)
	}
	if got := UnescapeText(esc); got != in {
		t.Fatalf("round trip: %q", got)
	}
	if got := EscapeText("a\x00b\x1bc\r\nd"); got != `abc\nd` {
		t.Fatalf("control characters are not TEXT: %q", got)
	}
}

// A long summary with umlauts is folded at 75 octets without cutting a
// UTF-8 sequence, and reads back whole.
func TestFoldingRoundTrip(t *testing.T) {
	summary := strings.Repeat("Größere Übung für Jürgen, ", 8)
	ics := Event{UID: "u1", Summary: summary, Start: time.Date(2026, 10, 14, 13, 0, 0, 0, time.UTC), End: time.Date(2026, 10, 14, 14, 0, 0, 0, time.UTC)}.ICS(time.Now())
	if !strings.HasSuffix(ics, "\r\n") {
		t.Fatal("content lines end in CRLF")
	}
	for _, l := range strings.Split(strings.TrimSuffix(ics, "\r\n"), "\r\n") {
		if len(l) > 75 {
			t.Fatalf("line of %d octets: %q", len(l), l)
		}
		if !utf8.ValidString(l) {
			t.Fatalf("a fold cut a UTF-8 sequence: %q", l)
		}
	}
	root, err := Parse(ics)
	if err != nil {
		t.Fatal(err)
	}
	evs := Events(root, time.UTC)
	if len(evs) != 1 || evs[0].Summary != summary {
		t.Fatalf("read back: %+v", evs)
	}
}

// What phones write: a TZID start, a DATE all-day event, a UTC event with a
// DURATION, a quoted parameter holding a colon, a VALARM whose DESCRIPTION
// must not become the event's summary.
func TestReadsWhatPhonesWrite(t *testing.T) {
	loc := berlin(t)
	ics := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\n" +
		"BEGIN:VEVENT\r\nUID:a\r\nDTSTART;TZID=Europe/Berlin:20261014T150000\r\nDTEND;TZID=Europe/Berlin:20261014T160000\r\nSUMMARY:Zahnarzt\r\nLOCATION;ALTREP=\"http://x.example/a:b\":Praxis\\, Hauptstr. 1\r\n" +
		"BEGIN:VALARM\r\nACTION:DISPLAY\r\nDESCRIPTION:nicht der Titel\r\nTRIGGER:-PT15M\r\nEND:VALARM\r\nEND:VEVENT\r\n" +
		"BEGIN:VEVENT\r\nUID:b\r\nDTSTART;VALUE=DATE:20261015\r\nSUMMARY:Ferien\r\nEND:VEVENT\r\n" +
		"BEGIN:VEVENT\r\nUID:c\r\nDTSTART:20261016T080000Z\r\nDURATION:PT1H30M\r\nSUMMARY:Lauf\r\n treff\r\nEND:VEVENT\r\n" +
		"BEGIN:VEVENT\r\nUID:d\r\nDTSTART;TZID=W. Europe Standard Time:20261017T100000\r\nSUMMARY:Outlook\r\nEND:VEVENT\r\n" +
		"END:VCALENDAR\r\n"
	root, err := Parse(ics)
	if err != nil {
		t.Fatal(err)
	}
	evs := Events(root, loc)
	if len(evs) != 4 {
		t.Fatalf("events: %+v", evs)
	}
	a, b, c, d := evs[0], evs[1], evs[2], evs[3]
	if a.Summary != "Zahnarzt" || a.Location != "Praxis, Hauptstr. 1" || !a.Start.Equal(time.Date(2026, 10, 14, 13, 0, 0, 0, time.UTC)) || a.End.Sub(a.Start) != time.Hour || a.AllDay {
		t.Fatalf("TZID event: %+v", a)
	}
	if !b.AllDay || b.Start.Format("2006-01-02") != "2026-10-15" || b.End.Sub(b.Start) != 24*time.Hour {
		t.Fatalf("all-day event without DTEND lasts the day: %+v", b)
	}
	if c.Summary != "Lauftreff" || c.End.Sub(c.Start) != 90*time.Minute {
		t.Fatalf("folded summary, DURATION: %+v", c)
	}
	if !d.Start.Equal(time.Date(2026, 10, 17, 10, 0, 0, 0, loc)) {
		t.Fatalf("an unknown TZID is read in the house's zone: %v", d.Start)
	}
}

// A reminder is a VTODO due then with a display alarm at that moment.
func TestTodoCarriesAnAlarm(t *testing.T) {
	due := time.Date(2026, 10, 14, 18, 30, 0, 0, time.UTC)
	ics := Todo{UID: "t1", Summary: "Müll, gelbe Tonne", Due: due}.ICS(time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC))
	for _, want := range []string{"BEGIN:VTODO", "UID:t1", "DTSTAMP:20261014T080000Z", "SUMMARY:Müll\\, gelbe Tonne", "STATUS:NEEDS-ACTION",
		"DUE:20261014T183000Z", "BEGIN:VALARM", "ACTION:DISPLAY", "TRIGGER;VALUE=DATE-TIME:20261014T183000Z", "END:VALARM", "END:VTODO"} {
		if !strings.Contains(ics, want+"\r\n") {
			t.Fatalf("missing %q in\n%s", want, ics)
		}
	}
	root, err := Parse(ics)
	if err != nil {
		t.Fatal(err)
	}
	todos := Todos(root, time.UTC)
	if len(todos) != 1 || todos[0].Summary != "Müll, gelbe Tonne" || !todos[0].Due.Equal(due) || !todos[0].Open() {
		t.Fatalf("read back: %+v", todos)
	}
	done, _ := Parse("BEGIN:VCALENDAR\r\nBEGIN:VTODO\r\nUID:x\r\nSUMMARY:erledigt\r\nSTATUS:NEEDS-ACTION\r\nCOMPLETED:20261001T100000Z\r\nEND:VTODO\r\nEND:VCALENDAR\r\n")
	if ts := Todos(done, time.UTC); len(ts) != 1 || ts[0].Open() {
		t.Fatalf("COMPLETED means done, whatever STATUS says: %+v", ts)
	}
}

func TestBrokenStreams(t *testing.T) {
	for _, bad := range []string{"BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\n", "END:VEVENT\r\n", "BEGIN:VCALENDAR\r\nEND:VEVENT\r\n"} {
		if _, err := Parse(bad); err == nil {
			t.Fatalf("%q: want an error", bad)
		}
	}
}

// A title cannot add a property: a newline is escaped, so the object keeps
// exactly one DTSTART and the title reads back as written.
func TestNoPropertyInjection(t *testing.T) {
	evil := "x\nDTSTART:19700101T000000Z"
	start := time.Date(2026, 10, 14, 13, 0, 0, 0, time.UTC)
	ics := Event{UID: "u", Summary: evil, Location: evil, Start: start, End: start.Add(time.Hour)}.ICS(start)
	n := 0
	for _, l := range strings.Split(ics, "\r\n") {
		if strings.HasPrefix(l, "DTSTART") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d DTSTART lines:\n%s", n, ics)
	}
	root, err := Parse(ics)
	if err != nil {
		t.Fatal(err)
	}
	evs := Events(root, time.UTC)
	if len(evs) != 1 || evs[0].Summary != evil || evs[0].Location != evil || !evs[0].Start.Equal(start) {
		t.Fatalf("read back: %+v", evs)
	}
	todo := Todo{UID: "t", Summary: "y\r\nEND:VTODO\r\nBEGIN:VEVENT", Due: start}.ICS(start)
	root, err = Parse(todo)
	if err != nil {
		t.Fatal(err)
	}
	if ts := Todos(root, time.UTC); len(ts) != 1 || ts[0].Summary != "y\nEND:VTODO\nBEGIN:VEVENT" || len(Events(root, time.UTC)) != 0 {
		t.Fatalf("todo: %+v", ts)
	}
}
