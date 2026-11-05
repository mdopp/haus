package caldav

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/homes/internal/caldav/davfake"
)

// Owner 2026-11-05: the Homes account is writable. A phone writes one
// appointment or card back by its UID, only over the version it saw, and
// what it does not show stays as it was.

const storedSeries = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//other app//EN\r\n" +
	"BEGIN:VEVENT\r\nUID:serie\r\nDTSTAMP:20261001T000000Z\r\nDTSTART;TZID=Europe/Berlin:20261102T090000\r\nDTEND;TZID=Europe/Berlin:20261102T100000\r\n" +
	"RRULE:FREQ=WEEKLY\r\nSUMMARY:Chor\r\nATTENDEE:mailto:anna@example.org\r\nX-HAUS-ART:bote\r\n" +
	"BEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER:-PT30M\r\nDESCRIPTION:Chor\r\nEND:VALARM\r\nEND:VEVENT\r\n" +
	"BEGIN:VEVENT\r\nUID:serie\r\nRECURRENCE-ID;TZID=Europe/Berlin:20261109T090000\r\nDTSTART;TZID=Europe/Berlin:20261109T110000\r\nDTEND;TZID=Europe/Berlin:20261109T120000\r\nSUMMARY:Chor später\r\nEND:VEVENT\r\n" +
	"END:VCALENDAR\r\n"

// what a phone sends: its fields, its two alarms, and something of its own
const phoneSeries = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Homes App//DE\r\nBEGIN:VEVENT\r\nUID:serie\r\nDTSTAMP:20261105T080000Z\r\n" +
	"DTSTART;TZID=Europe/Berlin:20261102T093000\r\nDURATION:PT1H\r\nRRULE:FREQ=WEEKLY;UNTIL=20261231T230000Z\r\nEXDATE;TZID=Europe/Berlin:20261116T093000\r\n" +
	"SUMMARY:Chor, neu\r\nX-PHONE-ONLY:1\r\n" +
	"BEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER:-PT10M\r\nEND:VALARM\r\nBEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER:-P1D\r\nEND:VALARM\r\n" +
	"END:VEVENT\r\nEND:VCALENDAR\r\n"

func schreibFake(t *testing.T) (*davfake.Server, *Client) {
	t.Helper()
	fake := davfake.New("homes", "pw")
	t.Cleanup(fake.Close)
	fake.AddCalendar("/homes/haushalt/", "Haushalt", "VEVENT", "VTODO")
	fake.AddCalendar("/homes/abo-ferien/", "Ferien")
	fake.AddAddressBook("/homes/haushalt-kontakte/", "Haushalt")
	fake.Put("/homes/haushalt/x1.ics", storedSeries)
	fake.Put("/homes/abo-ferien/f.ics", phoneEvent("ferien", "20261220T000000Z", "20261221T000000Z", "Ferien"))
	fake.Put("/homes/haushalt-kontakte/oma.vcf", "BEGIN:VCARD\r\nVERSION:4.0\r\nUID:oma\r\nFN:Oma Erna\r\nTEL;TYPE=cell:+49 170 1\r\nPHOTO:data:image/jpeg;base64,AAAA\r\nX-IBAN:DE00\r\nEND:VCARD\r\n")
	now := time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)
	return fake, &Client{URL: fake.URL, User: "homes", Password: "pw", Loc: berlin(t), Now: func() time.Time { return now }}
}

func TestWriteSharedKeepsWhatThePhoneDoesNotShow(t *testing.T) {
	fake, c := schreibFake(t)
	ctx := context.Background()
	at, err := c.FamilyEvent(ctx, "haushalt", "serie")
	if err != nil || at.Name != "x1.ics" || at.ETag == "" || at.Title() != "Chor" {
		t.Fatalf("found %+v %v", at, err)
	}
	if _, err := c.WriteShared(ctx, at, "serie", phoneSeries, `"stale"`); !errors.Is(err, ErrChanged) {
		t.Fatalf("over another version: %v", err)
	}
	got, err := c.WriteShared(ctx, at, "serie", phoneSeries, at.ETag)
	if err != nil || got.ETag == "" || got.ETag == at.ETag {
		t.Fatalf("write: %+v %v", got, err)
	}
	ics := fake.Objects()["/homes/haushalt/x1.ics"]
	for _, want := range []string{"SUMMARY:Chor, neu", "DTSTART;TZID=Europe/Berlin:20261102T093000", "DURATION:PT1H", "RRULE:FREQ=WEEKLY;UNTIL=20261231T230000Z",
		"EXDATE;TZID=Europe/Berlin:20261116T093000", "TRIGGER:-PT10M", "TRIGGER:-P1D", // the phone's
		"ATTENDEE:mailto:anna@example.org", "X-HAUS-ART:bote", "PRODID:-//other app//EN", "SUMMARY:Chor später", // kept
		"DTSTAMP:20261105T090000Z", "LAST-MODIFIED:20261105T090000Z"} {
		if !strings.Contains(ics, want+"\r\n") {
			t.Errorf("lacks %q:\n%s", want, ics)
		}
	}
	for _, gone := range []string{"DTEND;TZID=Europe/Berlin:20261102T100000", "TRIGGER:-PT30M", "X-PHONE-ONLY", "DTSTAMP:20261001", "DTSTAMP:20261105T080000Z"} {
		if strings.Contains(ics, gone) {
			t.Errorf("still has %q:\n%s", gone, ics)
		}
	}
	if strings.Count(ics, "BEGIN:VEVENT") != 2 || strings.Count(ics, "DTSTAMP") != 1 {
		t.Errorf("not one master and its changed instance:\n%s", ics)
	}
}

func TestWriteSharedMakesANewOne(t *testing.T) {
	fake, c := schreibFake(t)
	ctx := context.Background()
	at, err := c.FamilyEvent(ctx, "haushalt", "neu-1")
	if err != nil || at.Data != "" {
		t.Fatalf("found %+v %v", at, err)
	}
	sent := phoneEvent("neu-1", "20261110T170000Z", "20261110T180000Z", "Elternabend")
	if _, err := c.WriteShared(ctx, at, "anders", sent, ""); !errors.Is(err, ErrNotOne) {
		t.Fatalf("another UID in the body: %v", err)
	}
	got, err := c.WriteShared(ctx, at, "neu-1", sent, "")
	if err != nil || got.Name != "neu-1.ics" || got.ETag == "" {
		t.Fatalf("made %+v %v", got, err)
	}
	ics := fake.Objects()["/homes/haushalt/neu-1.ics"]
	if !strings.Contains(ics, "PRODID:"+prodID) || !strings.Contains(ics, "UID:neu-1\r\n") || !strings.Contains(ics, "SUMMARY:Elternabend") || strings.Count(ics, "UID:") != 1 {
		t.Fatalf("made:\n%s", ics)
	}
	if h := fake.Header[len(fake.Header)-1]; h.Get("If-None-Match") != "*" {
		t.Fatalf("a new one replaces: %v", h)
	}
	if _, err := c.WriteShared(ctx, got, "neu-1", sent, ""); !errors.Is(err, ErrChanged) {
		t.Fatalf("made twice: %v", err)
	}
}

func TestOnlyTheSharedCalendarsAreWritten(t *testing.T) {
	_, c := schreibFake(t)
	for _, id := range []string{"abo-ferien", RemindersID, "privat-arbeit", "../x"} {
		if _, err := c.FamilyEvent(context.Background(), id, "ferien"); !errors.Is(err, ErrNotShared) {
			t.Errorf("%s: %v", id, err)
		}
	}
}

func TestCardsKeepTheirPhotoAndIBAN(t *testing.T) {
	fake, c := schreibFake(t)
	ctx := context.Background()
	at, err := c.FamilyCard(ctx, "oma")
	if err != nil || at.Collection != "/homes/haushalt-kontakte/" || at.Title() != "Oma Erna" {
		t.Fatalf("found %+v %v", at, err)
	}
	sent := "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:oma\r\nFN:Oma Erna Huber\r\nN:Huber;Erna;;;\r\nTEL;TYPE=CELL:+49 170 2\r\nEND:VCARD\r\n"
	if _, err := c.WriteShared(ctx, at, "oma", sent, at.ETag); err != nil {
		t.Fatal(err)
	}
	vcf := fake.Objects()["/homes/haushalt-kontakte/oma.vcf"]
	for _, want := range []string{"VERSION:4.0", "FN:Oma Erna Huber", "N:Huber;Erna;;;", "TEL;TYPE=CELL:+49 170 2", "PHOTO:data:image/jpeg;base64,AAAA", "X-IBAN:DE00", "REV:20261105T090000Z"} {
		if !strings.Contains(vcf, want+"\r\n") {
			t.Errorf("lacks %q:\n%s", want, vcf)
		}
	}
	if strings.Contains(vcf, "+49 170 1") || strings.Contains(vcf, "VERSION:3.0") {
		t.Errorf("the old number or the phone's version stays:\n%s", vcf)
	}
	at, _ = c.FamilyCard(ctx, "urn:uuid:neu")
	if got, err := c.WriteShared(ctx, at, "urn:uuid:neu", strings.ReplaceAll(sent, "UID:oma", "UID:urn:uuid:neu"), ""); err != nil || got.Name != "neu.vcf" {
		t.Fatalf("new card %+v %v", got, err)
	}
}

func TestDeleteAndRestoreShared(t *testing.T) {
	fake, c := schreibFake(t)
	ctx := context.Background()
	at, _ := c.FamilyEvent(ctx, "haushalt", "serie")
	if err := c.DeleteShared(ctx, at, `"stale"`); !errors.Is(err, ErrChanged) {
		t.Fatalf("over another version: %v", err)
	}
	if err := c.DeleteShared(ctx, at, at.ETag); err != nil {
		t.Fatal(err)
	}
	if _, ok := fake.Objects()["/homes/haushalt/x1.ics"]; ok {
		t.Fatal("still there")
	}
	if err := c.RestoreShared(ctx, at); err != nil || fake.Objects()["/homes/haushalt/x1.ics"] != storedSeries {
		t.Fatalf("back: %v", err)
	}
}

func TestFamilyFeedWholeForWritingPhones(t *testing.T) {
	_, c := schreibFake(t)
	from := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	cals, err := c.FamilyFeed(context.Background(), from, from.AddDate(1, 0, 0), true)
	if err != nil {
		t.Fatal(err)
	}
	ics := FeedICS(cals, from, berlin(t))
	haushalt, _, _ := strings.Cut(ics[strings.Index(ics, "X-WR-CALNAME:Haushalt"):], "END:VCALENDAR")
	rest := strings.Replace(ics, haushalt, "", 1)
	for _, want := range []string{"X-HAUS-KALENDER:haushalt\r\nX-HAUS-ART:haushalt\r\nX-HOMES-SCHREIBBAR:TRUE", "RRULE:FREQ=WEEKLY", "TRIGGER:-PT30M", "RECURRENCE-ID;TZID=Europe/Berlin:20261109T090000"} {
		if !strings.Contains(haushalt, want) {
			t.Errorf("lacks %q:\n%s", want, ics)
		}
	}
	if strings.Count(haushalt, "X-HOMES-ETAG:") != 2 || strings.Contains(rest, "SCHREIBBAR") || strings.Contains(ics, "PRODID:-//other app") {
		t.Errorf("tags, or a mirror said writable:\n%s", ics)
	}
}

// HOMES_RADICALE_REAL=http://127.0.0.1:5232 (a Radicale with auth none,
// say) checks the writes against a real one by hand; CI never does.
func TestWriteSharedOnARealRadicale(t *testing.T) {
	addr := os.Getenv("HOMES_RADICALE_REAL")
	if addr == "" {
		t.Skip("HOMES_RADICALE_REAL not set")
	}
	user := "homes-" + time.Now().Format("150405")
	c := &Client{URL: addr, User: user, Password: "pw", Loc: berlin(t)}
	ctx := context.Background()
	if err := c.EnsureCalendar(ctx); err != nil {
		t.Fatal(err)
	}
	at, err := c.FamilyEvent(ctx, "haushalt", "serie")
	if err != nil {
		t.Fatal(err)
	}
	if at, err = c.WriteShared(ctx, at, "serie", storedSeries, ""); err != nil || at.ETag == "" {
		t.Fatalf("made: %+v %v", at, err)
	}
	if at, err = c.WriteShared(ctx, at, "serie", phoneSeries, at.ETag); err != nil || at.ETag == "" {
		t.Fatalf("changed: %+v %v", at, err)
	}
	from := time.Now().AddDate(0, 0, -31)
	cals, err := c.FamilyFeed(ctx, from, from.AddDate(1, 0, 0), true)
	if err != nil || !strings.Contains(strings.ReplaceAll(FeedICS(cals, from, berlin(t)), "\r\n ", ""), "X-HOMES-ETAG:"+EscapeText(at.ETag)) {
		t.Fatalf("feed %v:\n%s", err, FeedICS(cals, from, berlin(t)))
	}
	if _, err := c.WriteShared(ctx, at, "serie", phoneSeries, `"stale"`); !errors.Is(err, ErrChanged) {
		t.Fatalf("stale: %v", err)
	}
	if err := c.DeleteShared(ctx, at, at.ETag); err != nil {
		t.Fatal(err)
	}
	card, err := c.FamilyCard(ctx, "oma")
	if err == nil {
		card, err = c.WriteShared(ctx, card, "oma", "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:oma\r\nFN:Oma\r\nEND:VCARD\r\n", "")
	}
	if err != nil || card.ETag == "" {
		t.Fatalf("card: %+v %v", card, err)
	}
}
