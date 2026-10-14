package pages

import (
	"fmt"
	"strings"
	"testing"
)

func allDay(uid, day, next, summary string) string {
	return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:" + uid + "\r\nDTSTART;VALUE=DATE:" + day + "\r\nDTEND;VALUE=DATE:" + next + "\r\nSUMMARY:" + summary + "\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
}

func eventAt(uid, start, end, summary, location string) string {
	return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:" + uid + "\r\nDTSTART:" + start + "\r\nDTEND:" + end + "\r\nSUMMARY:" + summary + "\r\nLOCATION:" + location + "\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
}

// haus#12: Kalender — today, the next 14 days, the open reminders. The
// clock is Wednesday 14.10.2026, 10:00 in Berlin.
func TestKalenderShowsTodayTheNextTwoWeeksAndReminders(t *testing.T) {
	dav, cal := calendar(t)
	dav.Put("/haus/familie/0.ics", event("0", "20261013T080000Z", "20261013T090000Z", "Gestern vorbei"))
	dav.Put("/haus/familie/1.ics", event("1", "20261014T060000Z", "20261014T070000Z", "Frühstück"))
	dav.Put("/haus/familie/2.ics", eventAt("2", "20261014T110000Z", "20261014T120000Z", "Mittag mit Oma", "Café Weiß"))
	dav.Put("/haus/familie/3.ics", allDay("3", "20261014", "20261015", "Omas Geburtstag"))
	dav.Put("/haus/familie/4.ics", eventAt("4", "20261015T130000Z", "20261015T140000Z", "Zahnarzt", "Praxis Dr. Berg"))
	dav.Put("/haus/familie/5.ics", event("5", "20261023T160000Z", "20261023T180000Z", "Elternabend"))
	dav.Put("/haus/familie/6.ics", allDay("6", "20261028", "20261029", "Letzter Tag"))
	dav.Put("/haus/familie/7.ics", event("7", "20261029T090000Z", "20261029T100000Z", "Zu weit weg"))
	dav.Put("/haus/familie/a.ics", todo("a", "Müll rausbringen", "DUE:20261015T050000Z\r\n"))
	dav.Put("/haus/familie/b.ics", todo("b", "Rechnung bezahlen", "DUE:20261012T070000Z\r\n"))
	dav.Put("/haus/familie/c.ics", todo("c", "Irgendwann Keller", ""))
	dav.Put("/haus/familie/d.ics", todo("d", "Schon erledigt", "STATUS:COMPLETED\r\nDUE:20261014T070000Z\r\n"))
	code, body, _ := familyPage(t, Kalender, StartConfig{Calendar: cal}, "GET", "/kalender", "family")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	mustContain(t, body,
		"<h2 id=karte-heute>Heute</h2>", "Omas Geburtstag", "ganztägig",
		"Frühstück", "08:00–09:00", "Mittag mit Oma", "13:00–14:00", "Café Weiß",
		"<h2 id=karte-naechste-14-tage>Nächste 14 Tage</h2>",
		"Zahnarzt", "morgen 15:00", "Praxis Dr. Berg",
		"Elternabend", "Fr 23.10. 18:00",
		"Letzter Tag", "Mi 28.10. ganztägig",
		"<h2 id=karte-erinnerungen>Erinnerungen</h2>",
		"Müll rausbringen", "morgen 07:00",
		"Rechnung bezahlen", "überfällig seit Mo 12.10. 09:00",
		"Irgendwann Keller")
	mustNotContain(t, body, "Gestern vorbei", "Zu weit weg", "Schon erledigt", "<form", "<button", "<script")
	// order: the cards, and within each, soonest first; undated reminders last
	before(t, body, "karte-heute", "karte-naechste-14-tage")
	before(t, body, "karte-naechste-14-tage", "karte-erinnerungen")
	before(t, body, "Omas Geburtstag", "Frühstück")
	before(t, body, "Frühstück", "Mittag mit Oma")
	before(t, body, "Mittag mit Oma", "karte-naechste-14-tage")
	before(t, body, "Zahnarzt", "Elternabend")
	before(t, body, "Elternabend", "Letzter Tag")
	before(t, body, "Rechnung bezahlen", "Müll rausbringen")
	before(t, body, "Müll rausbringen", "Irgendwann Keller")
}

// An empty day and an empty list are said plainly.
func TestKalenderSaysEmptyPlainly(t *testing.T) {
	_, cal := calendar(t)
	_, body, _ := familyPage(t, Kalender, StartConfig{Calendar: cal}, "GET", "/kalender", "")
	mustContain(t, body, "Heute keine Termine", "Keine Termine in den nächsten 14 Tagen", "Keine offenen Erinnerungen")
}

// An appointment that began yesterday and runs on is today's, "bis" its end.
func TestKalenderRunningAppointment(t *testing.T) {
	dav, cal := calendar(t)
	dav.Put("/haus/familie/1.ics", event("1", "20261013T200000Z", "20261014T090000Z", "Nachtschicht"))
	_, body, _ := familyPage(t, Kalender, StartConfig{Calendar: cal}, "GET", "/kalender", "")
	mustContain(t, body, "Nachtschicht", "bis 11:00")
	before(t, body, "karte-heute", "Nachtschicht")
	before(t, body, "Nachtschicht", "karte-naechste-14-tage")
}

// Titles and places are text, never markup.
func TestKalenderEscapes(t *testing.T) {
	dav, cal := calendar(t)
	dav.Put("/haus/familie/1.ics", eventAt("1", "20261014T110000Z", "20261014T120000Z", "<script>alert(1)</script>", "<b>Ort</b>"))
	_, body, _ := familyPage(t, Kalender, StartConfig{Calendar: cal}, "GET", "/kalender", "")
	mustContain(t, body, "&lt;script&gt;alert(1)&lt;/script&gt;", "&lt;b&gt;Ort&lt;/b&gt;")
	mustNotContain(t, body, "<script", "<b>Ort")
}

// A long list is cut, the count is not.
func TestKalenderCutsLongLists(t *testing.T) {
	dav, cal := calendar(t)
	for i := 0; i < 60; i++ {
		dav.Put(fmt.Sprintf("/haus/familie/t%02d.ics", i), todo(fmt.Sprintf("t%02d", i), fmt.Sprintf("Aufgabe %02d", i), fmt.Sprintf("DUE:202611%02dT070000Z\r\n", i%28+1)))
	}
	_, body, _ := familyPage(t, Kalender, StartConfig{Calendar: cal}, "GET", "/kalender", "")
	mustContain(t, body, "und 10 weitere", "Aufgabe 00")
}

// Heute and the next 14 days are cut at 50 rows each, the rest counted;
// a list of exactly 50 is not cut and says no "weitere".
func TestKalenderCutsLongDays(t *testing.T) {
	dav, cal := calendar(t)
	for i := 0; i < 60; i++ {
		dav.Put(fmt.Sprintf("/haus/familie/h%02d.ics", i), event(fmt.Sprintf("h%02d", i), fmt.Sprintf("20261014T08%02d00Z", i), fmt.Sprintf("20261014T08%02d30Z", i), fmt.Sprintf("Heute %02d", i)))
		dav.Put(fmt.Sprintf("/haus/familie/n%02d.ics", i), event(fmt.Sprintf("n%02d", i), fmt.Sprintf("20261016T08%02d00Z", i), fmt.Sprintf("20261016T08%02d30Z", i), fmt.Sprintf("Später %02d", i)))
	}
	_, body, _ := familyPage(t, Kalender, StartConfig{Calendar: cal}, "GET", "/kalender", "")
	mustContain(t, body, "Heute 00", "Heute 49", "Später 00", "Später 49")
	mustNotContain(t, body, "Heute 50", "Heute 59", "Später 50", "Später 59")
	if n := strings.Count(body, "und 10 weitere"); n != 2 {
		t.Fatalf("%d cut lists, want 2", n)
	}

	dav2, cal2 := calendar(t)
	for i := 0; i < 50; i++ {
		dav2.Put(fmt.Sprintf("/haus/familie/t%02d.ics", i), todo(fmt.Sprintf("t%02d", i), fmt.Sprintf("Aufgabe %02d", i), ""))
	}
	_, body, _ = familyPage(t, Kalender, StartConfig{Calendar: cal2}, "GET", "/kalender", "")
	mustContain(t, body, "Aufgabe 49")
	mustNotContain(t, body, "weitere")
}

// A reminder due on a day, not at a time, says the day only, and is
// overdue once that day is past — not at midnight of its own day.
func TestKalenderReminderDueOnADay(t *testing.T) {
	dav, cal := calendar(t)
	dav.Put("/haus/familie/a.ics", todo("a", "Heute irgendwann", "DUE;VALUE=DATE:20261014\r\n"))
	dav.Put("/haus/familie/b.ics", todo("b", "Gestern schon", "DUE;VALUE=DATE:20261013\r\n"))
	dav.Put("/haus/familie/c.ics", todo("c", "Freitag", "DUE;VALUE=DATE:20261016\r\n"))
	_, body, _ := familyPage(t, Kalender, StartConfig{Calendar: cal}, "GET", "/kalender", "")
	mustContain(t, body, "Heute irgendwann <span class=k>heute</span>", "Gestern schon <span class=k>überfällig seit gestern</span>",
		"Freitag <span class=k>Fr 16.10.</span>")
	mustNotContain(t, body, "00:00", "überfällig seit heute")

	_, body = startPage(t, StartConfig{Calendar: cal}, "")
	mustContain(t, body, "Erinnerung · heute<", "Erinnerung · überfällig seit gestern<")
	mustNotContain(t, body, "00:00")
}

// Radicale in trouble is left out without a word on the page.
func TestKalenderHidesBrokenBackends(t *testing.T) {
	_, cal := calendar(t)
	cal.Password = "falsch"
	code, body, _ := familyPage(t, Kalender, StartConfig{Calendar: cal}, "GET", "/kalender", "")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	mustNotContain(t, body, "karte-", "Anmeldung abgelehnt", "HTTP", "401", "class=rot", "keine Termine", "Keine")
	mustContain(t, body, "<h1")
}
