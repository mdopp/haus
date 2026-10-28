package caldav

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mdopp/haus/internal/caldav/davfake"
)

// a box after laib#495: Haus's account, Lea's home with her mirrors, her
// own calendars and one she made with Haus
func mirrorBox(t *testing.T) (*davfake.Server, *Client) {
	t.Helper()
	fake := davfake.New("haus", "pw")
	t.Cleanup(fake.Close)
	fake.AddCalendar("/haus/haushalt/", "Haushalt")
	fake.AddCalendar("/haus/familie-geburtstage/", "Geburtstage")
	fake.AddCalendar("/haus/abo-feiertage-ni/", "Feiertage Niedersachsen")
	fake.AddPrincipal("lea")
	fake.AddCalendar("/lea/google-familie/", "Familie")
	fake.AddCalendar("/lea/abo-muell/", "Müllabfuhr")
	fake.AddCalendar("/lea/privat-sport/", "Sport")
	fake.AddCalendar("/lea/kalender/", "Leas eigener") // Radicale would not even list it to Haus
	fake.AddAddressBook("/lea/google-kontakte/", "Takeout")
	fake.SetSynced("/lea/google-familie/", "2026-10-14T07:55:00Z", "")
	fake.SetSynced("/lea/abo-muell/", "2026-10-14T05:00:00Z", "Abgleich seit 3 h nicht gelungen")
	return fake, &Client{URL: fake.URL, User: "haus", Password: "pw", Loc: time.UTC}
}

func ev(uid, start, summary string) string {
	return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:" + uid + "\r\nDTSTART:" + start + "\r\nDTEND:" + start + "\r\nSUMMARY:" + summary + "\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
}

// laib#495: the Haushalt's Termine (the Bote, Start, Kalender) include
// every person's calendars from outside; a person's own stay theirs.
func TestEventsIncludeTheMirrorsAndNotThePrivateOnes(t *testing.T) {
	fake, c := mirrorBox(t)
	fake.Put("/haus/haushalt/1.ics", ev("1", "20261014T100000Z", "Müll raus"))
	fake.Put("/lea/google-familie/2.ics", ev("2", "20261014T110000Z", "Zahnarzt"))
	fake.Put("/lea/abo-muell/3.ics", ev("3", "20261014T120000Z", "Gelbe Tonne"))
	fake.Put("/lea/privat-sport/4.ics", ev("4", "20261014T130000Z", "Laufen"))
	fake.Put("/lea/kalender/5.ics", ev("5", "20261014T140000Z", "Geheim"))
	from, to := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	evs, err := c.Events(context.Background(), from, to)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range evs {
		got = append(got, e.Summary+"@"+e.CalendarName)
	}
	if strings.Join(got, ",") != "Müll raus@Haushalt,Zahnarzt@Familie,Gelbe Tonne@Müllabfuhr" {
		t.Fatalf("events: %v", got)
	}
	evs, _ = c.PersonEvents(context.Background(), "lea", from, to)
	if len(evs) != 4 || evs[3].Summary != "Laufen" || !strings.HasSuffix(evs[3].Calendar, "/lea/privat-sport/") {
		t.Fatalf("Lea's own: %+v", evs)
	}
	if evs, _ := c.PersonEvents(context.Background(), "ben", from, to); len(evs) != 3 {
		t.Fatalf("Ben sees Lea's calendar nur für sie: %+v", evs)
	}
}

// A Radicale before laib#495 does not let Haus list the root: no mirrors, no error.
func TestNoRootListingIsNoMirrors(t *testing.T) {
	fake := davfake.New("haus", "pw")
	t.Cleanup(fake.Close)
	fake.AddCalendar("/haus/haushalt/", "Haushalt")
	c := &Client{URL: fake.URL, User: "haus", Password: "pw"}
	if _, err := c.Events(context.Background(), time.Now(), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
}

func TestPersonCalendarsSayWhatEachIs(t *testing.T) {
	_, c := mirrorBox(t)
	cals, err := c.PersonCalendars(context.Background(), "lea")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, k := range cals {
		got = append(got, k.Path+"="+k.Kind)
	}
	want := "/haus/abo-feiertage-ni/=abo,/haus/familie-geburtstage/=familie,/haus/haushalt/=haushalt,/lea/abo-muell/=abo,/lea/google-familie/=google,/lea/privat-sport/=privat"
	if strings.Join(got, ",") != want {
		t.Fatalf("got %v\nwant %s", got, want)
	}
	for _, k := range cals {
		switch k.Path {
		case "/lea/google-familie/":
			if !k.Synced.Equal(time.Date(2026, 10, 14, 7, 55, 0, 0, time.UTC)) || k.Problem != "" || !k.ReadOnly() || k.Family {
				t.Fatalf("google: %+v", k)
			}
		case "/lea/abo-muell/":
			if k.Problem == "" {
				t.Fatalf("the problem laib's kalender noted is lost: %+v", k)
			}
		case "/haus/abo-feiertage-ni/":
			if !k.Family || !k.ReadOnly() {
				t.Fatalf("the family's subscription: %+v", k)
			}
		}
	}
}

func TestMakeAndDeleteOnlyWhatHausMakes(t *testing.T) {
	fake, c := mirrorBox(t)
	ctx := context.Background()
	p, err := c.MakeCalendar(ctx, "lea", "schule", "Schule & Hort", false)
	if err != nil || p != "/lea/privat-schule/" || fake.DisplayName(p) != "Schule &amp; Hort" && fake.DisplayName(p) != "Schule & Hort" {
		t.Fatalf("%q %v %q", p, err, fake.DisplayName(p))
	}
	if p, err := c.MakeCalendar(ctx, "lea", "ausfluege", "Ausflüge", true); err != nil || p != "/haus/familie-ausfluege/" {
		t.Fatalf("%q %v", p, err)
	}
	if _, err := c.MakeCalendar(ctx, "lea", "schule", "Schule", false); err != ErrCalendarExists {
		t.Fatalf("twice: %v", err)
	}
	if _, err := c.MakeCalendar(ctx, "", "x", "X", false); err == nil {
		t.Fatal("a calendar for nobody")
	}
	for _, p := range []string{"/lea/google-familie/", "/haus/haushalt/", "/lea/kalender/", "/haus/abo-feiertage-ni/"} {
		if err := c.DeleteCalendar(ctx, p); err == nil {
			t.Fatalf("%s deleted", p)
		}
	}
	if err := c.DeleteCalendar(ctx, "/lea/privat-schule/"); err != nil {
		t.Fatal(err)
	}
	for _, m := range fake.Methods {
		if strings.HasPrefix(m, "DELETE") && m != "DELETE /lea/privat-schule/" {
			t.Fatalf("sent %s", m)
		}
	}
}

// haus#184: on the box every request took 0.13-0.3 s (Radicale's login) and
// Kalender asked about twelve one after another; the homes, the calendars and
// their reports now go at once, so a page waits for a few requests, not all
func TestPersonEventsAsksRadicaleAtOnce(t *testing.T) {
	fake, c := mirrorBox(t)
	for _, p := range []string{"ben", "cara", "dora", "emil"} {
		fake.AddPrincipal(p)
		fake.AddCalendar("/"+p+"/google-familie/", "Familie "+p)
	}
	var mu sync.Mutex
	inFlight, most, total := 0, 0, 0
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		inFlight++
		total++
		most = max(most, inFlight)
		mu.Unlock()
		time.Sleep(40 * time.Millisecond)
		mu.Lock()
		inFlight--
		mu.Unlock()
		u, _ := url.Parse(fake.URL)
		httputil.NewSingleHostReverseProxy(u).ServeHTTP(w, r)
	}))
	t.Cleanup(slow.Close)
	c.URL = slow.URL
	from, to := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	start := time.Now()
	if _, err := c.PersonEvents(context.Background(), "lea", from, to); err != nil {
		t.Fatal(err)
	}
	took := time.Since(start)
	if most < 3 {
		t.Errorf("at most %d requests at once of %d", most, total)
	}
	if took > time.Duration(total)*40*time.Millisecond/2 {
		t.Errorf("%d requests took %v, about one after another", total, took)
	}
}
