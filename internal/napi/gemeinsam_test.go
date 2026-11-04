package napi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/homes/internal/caldav"
	"github.com/mdopp/homes/internal/caldav/davfake"
	"github.com/mdopp/homes/internal/haushalt"
)

// haus#218: the household's shared contacts, calendars and reminders for
// the phones' Homes account — what is in Homes' own account's home, nothing
// of a person's, no lists.

var gemeinsamNow = time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)

func gemeinsamHandler(t *testing.T, dav *caldav.Client) http.Handler {
	t.Helper()
	people := map[string]string{"anna1": "anna", "waise1": ""}
	berlin, _ := time.LoadLocation("Europe/Berlin")
	return Handler(Config{Validate: StaticValidator([]string{"anna1", "waise1"}),
		Haushalt: haushalt.Config{Calendar: dav, Contacts: dav, Loc: berlin}, Konto: dav,
		Whoami: func(b string) (string, bool) { p, ok := people[b]; return p, ok },
		Now:    func() time.Time { return gemeinsamNow }})
}

func gemeinsamFake(t *testing.T) (*davfake.Server, http.Handler) {
	t.Helper()
	fake := davfake.New("homes", "pw")
	t.Cleanup(fake.Close)
	berlin, _ := time.LoadLocation("Europe/Berlin")
	dav := &caldav.Client{URL: fake.URL, User: "homes", Password: "pw", Loc: berlin}

	fake.AddCalendar("/homes/haushalt/", "Haushalt", "VEVENT", "VTODO")
	fake.AddCalendar("/homes/familie-urlaub/", "Urlaub")
	fake.AddCalendar("/homes/einkauf/", "Einkauf", "VTODO") // a list, no calendar
	fake.AddAddressBook("/homes/haushalt-kontakte/", "Haushalt")
	fake.Put("/homes/haushalt/muell.ics", "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:muell\r\nDTSTART:20261105T060000Z\r\nDTEND:20261105T063000Z\r\nSUMMARY:Müll, Papier\r\nDESCRIPTION:raus\\nbis 7\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")
	fake.Put("/homes/familie-urlaub/see.ics", "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:see\r\nDTSTART;VALUE=DATE:20261220\r\nDTEND;VALUE=DATE:20261223\r\nSUMMARY:Am See\r\nLOCATION:Ammersee\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")
	fake.Put("/homes/haushalt/arzt.ics", "BEGIN:VCALENDAR\r\nBEGIN:VTODO\r\nUID:arzt\r\nSUMMARY:Arzt anrufen\r\nDUE:20261104T080000Z\r\nEND:VTODO\r\nEND:VCALENDAR\r\n")
	fake.Put("/homes/haushalt/blumen.ics", "BEGIN:VCALENDAR\r\nBEGIN:VTODO\r\nUID:blumen\r\nSUMMARY:Blumen gießen\r\nDUE;TZID=Europe/Berlin:20261106T180000\r\nRRULE:FREQ=WEEKLY\r\nEND:VTODO\r\nEND:VCALENDAR\r\n")
	fake.Put("/homes/haushalt/erledigt.ics", "BEGIN:VCALENDAR\r\nBEGIN:VTODO\r\nUID:erledigt\r\nSUMMARY:Erledigt\r\nDUE:20261101T080000Z\r\nSTATUS:COMPLETED\r\nCOMPLETED:20261101T090000Z\r\nEND:VTODO\r\nEND:VCALENDAR\r\n")
	fake.Put("/homes/haushalt/irgendwann.ics", "BEGIN:VCALENDAR\r\nBEGIN:VTODO\r\nUID:irgendwann\r\nSUMMARY:Irgendwann\r\nEND:VTODO\r\nEND:VCALENDAR\r\n")
	fake.Put("/homes/einkauf/milch.ics", "BEGIN:VCALENDAR\r\nBEGIN:VTODO\r\nUID:milch\r\nSUMMARY:Milch\r\nEND:VTODO\r\nEND:VCALENDAR\r\n")
	fake.Put("/homes/haushalt-kontakte/oma.vcf", "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:oma\r\nFN:Oma Erna\r\nTEL;TYPE=CELL:+49 170 1234567\r\nEND:VCARD\r\n")

	// a person's own: never on the others' phones
	fake.AddPrincipal("anna")
	fake.AddCalendar("/anna/privat-arbeit/", "Arbeit")
	fake.AddCalendar("/anna/google-familie/", "Google Familie")
	fake.AddAddressBook("/anna/handy-kontakte/", "Handy-Kontakte")
	fake.Put("/anna/privat-arbeit/x.ics", "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:privat\r\nDTSTART:20261105T060000Z\r\nDTEND:20261105T070000Z\r\nSUMMARY:Privat\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")
	fake.Put("/anna/google-familie/y.ics", "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:google\r\nDTSTART:20261105T060000Z\r\nDTEND:20261105T070000Z\r\nSUMMARY:Google\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")
	fake.Put("/anna/handy-kontakte/z.vcf", "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:handy\r\nFN:Annas Zahnarzt\r\nEND:VCARD\r\n")
	return fake, gemeinsamHandler(t, dav)
}

func getGemeinsam(t *testing.T, h http.Handler, path, bearer, etag string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestGemeinsamKontakte(t *testing.T) {
	_, h := gemeinsamFake(t)
	rec := getGemeinsam(t, h, "/napi/gemeinsam/kontakte", "anna1", "")
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/vcard") {
		t.Fatalf("→ %d %q %s", rec.Code, rec.Header().Get("Content-Type"), rec.Body)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "FN:Oma Erna") || !strings.Contains(body, "TEL;TYPE=CELL:+49 170 1234567") {
		t.Fatalf("the household's card, as Radicale keeps it: %s", body)
	}
	if strings.Contains(body, "Zahnarzt") || strings.Count(body, "BEGIN:VCARD") != 1 {
		t.Fatalf("a person's own contacts are not shared: %s", body)
	}
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("no ETag")
	}
	if again := getGemeinsam(t, h, "/napi/gemeinsam/kontakte", "anna1", etag); again.Code != http.StatusNotModified || again.Body.Len() != 0 {
		t.Fatalf("the same state again → %d %s", again.Code, again.Body)
	}
	if other := getGemeinsam(t, h, "/napi/gemeinsam/kontakte", "anna1", `"alt"`); other.Code != 200 {
		t.Fatalf("an older state → %d", other.Code)
	}
}

func TestGemeinsamKalender(t *testing.T) {
	_, h := gemeinsamFake(t)
	rec := getGemeinsam(t, h, "/napi/gemeinsam/kalender", "anna1", "")
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/calendar") {
		t.Fatalf("→ %d %q %s", rec.Code, rec.Header().Get("Content-Type"), rec.Body)
	}
	root, err := caldav.Parse(rec.Body.String())
	if err != nil {
		t.Fatalf("not iCalendar: %v\n%s", err, rec.Body)
	}
	_ = root
	body := strings.ReplaceAll(rec.Body.String(), "\r\n ", "")
	cals := strings.Split(body, "BEGIN:VCALENDAR")[1:]
	var ids []string
	for _, c := range cals {
		for _, l := range strings.Split(c, "\r\n") {
			if id, ok := strings.CutPrefix(l, "X-HAUS-KALENDER:"); ok {
				ids = append(ids, id)
			}
		}
	}
	if strings.Join(ids, ",") != "familie-urlaub,haushalt,erinnerungen" {
		t.Fatalf("the shared calendars, reminders last, no list: %v\n%s", ids, body)
	}
	for _, s := range []string{
		"X-WR-CALNAME:Haushalt", "X-HAUS-ART:familie", "X-WR-CALNAME:Erinnerungen",
		"SUMMARY:Müll\\, Papier", "DESCRIPTION:raus\\nbis 7", "DTSTART:20261105T060000Z",
		"DTSTART;VALUE=DATE:20261220", "DTEND;VALUE=DATE:20261223", "LOCATION:Ammersee",
		// a reminder: at its due time, with an alarm then
		"UID:arzt", "DTSTART:20261104T080000Z", "TRIGGER:PT0S",
		// a recurring one keeps its hour in the house's time zone
		"DTSTART;TZID=Europe/Berlin:20261106T180000", "RRULE:FREQ=WEEKLY",
	} {
		if !strings.Contains(body, s) {
			t.Errorf("missing %q", s)
		}
	}
	for _, s := range []string{"Privat", "SUMMARY:Google", "Erledigt", "Irgendwann", "Milch", "VTODO"} {
		if strings.Contains(body, s) {
			t.Errorf("%q is not on the phones", s)
		}
	}
	if t.Failed() {
		t.Log(body)
	}
	if again := getGemeinsam(t, h, "/napi/gemeinsam/kalender", "anna1", rec.Header().Get("ETag")); again.Code != http.StatusNotModified {
		t.Fatalf("the same state again → %d", again.Code)
	}
}

// An expanded series' instances share their UID: each says which one it is.
func TestGemeinsamSeriesInstances(t *testing.T) {
	cals := []caldav.FeedCalendar{{ID: "haushalt", Name: "Haushalt", Kind: "haushalt", Events: []caldav.Event{
		{UID: "s", Summary: "Chor", Start: gemeinsamNow, End: gemeinsamNow.Add(time.Hour)},
		{UID: "s", Summary: "Chor", Start: gemeinsamNow.AddDate(0, 0, 7), End: gemeinsamNow.AddDate(0, 0, 7).Add(time.Hour)},
		{UID: "einmal", Summary: "Einmal", Start: gemeinsamNow, End: gemeinsamNow},
	}}}
	out := caldav.FeedICS(cals, gemeinsamNow, time.UTC)
	if strings.Count(out, "RECURRENCE-ID:") != 2 || !strings.Contains(out, "RECURRENCE-ID:20261110T100000Z") {
		t.Fatalf("each instance with its RECURRENCE-ID, the single one without:\n%s", out)
	}
}

func TestGemeinsamRefusals(t *testing.T) {
	_, h := gemeinsamFake(t)
	for _, p := range []string{"/napi/gemeinsam/kontakte", "/napi/gemeinsam/kalender"} {
		if rec := getGemeinsam(t, h, p, "", ""); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without a device → %d", p, rec.Code)
		}
		if rec := getGemeinsam(t, h, p, "waise1", ""); rec.Code != http.StatusForbidden {
			t.Errorf("%s from a device of nobody → %d", p, rec.Code)
		}
	}
	none := Handler(Config{Validate: StaticValidator([]string{"anna1"}),
		Whoami: func(string) (string, bool) { return "anna", true }})
	for _, p := range []string{"/napi/gemeinsam/kontakte", "/napi/gemeinsam/kalender"} {
		rec := getGemeinsam(t, none, p, "anna1", "")
		var out map[string]string
		json.Unmarshal(rec.Body.Bytes(), &out)
		if rec.Code != http.StatusServiceUnavailable || out["fehler"] != saysNoCalendarHere {
			t.Errorf("%s without Radicale → %d %v", p, rec.Code, out)
		}
	}
}

// Radicale refusing is a sentence, never a half answer the phone would mirror.
func TestGemeinsamRadicaleDown(t *testing.T) {
	fake := davfake.New("homes", "pw")
	t.Cleanup(fake.Close)
	h := gemeinsamHandler(t, &caldav.Client{URL: fake.URL, User: "homes", Password: "falsch"})
	for _, p := range []string{"/napi/gemeinsam/kontakte", "/napi/gemeinsam/kalender"} {
		rec := getGemeinsam(t, h, p, "anna1", "")
		var out map[string]string
		json.Unmarshal(rec.Body.Bytes(), &out)
		if rec.Code != http.StatusBadGateway || out["fehler"] != saysGemeinsamFail {
			t.Errorf("%s → %d %v", p, rec.Code, out)
		}
	}
}
