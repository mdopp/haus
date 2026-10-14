package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/haus/internal/caldav"
	"github.com/mdopp/haus/internal/caldav/davfake"
	"github.com/mdopp/haus/internal/ha"
	"github.com/mdopp/haus/internal/ha/hafake"
	"github.com/mdopp/haus/internal/haushalt"
	"github.com/mdopp/haus/internal/notes"
	"github.com/mdopp/haus/internal/paperless"
	"github.com/mdopp/haus/internal/paperless/paperlessfake"
)

// a site with every backend set, each from a fake; the HA and Radicale
// fakes for a test to add to
func fullSite(t *testing.T) (site, *hafake.HA, *davfake.Server) {
	t.Helper()
	s := testSite(t)
	fake := hafake.New("ha-token")
	t.Cleanup(fake.Close)
	fake.SetRegistries([]map[string]any{{"area_id": "kueche", "name": "Küche"}}, nil,
		[]map[string]any{{"entity_id": "light.decke", "area_id": "kueche"}})
	fake.Set("light.decke", "on", map[string]any{"friendly_name": "Deckenlampe", "brightness": 128})
	fake.Set("switch.kaffee", "on", map[string]any{"friendly_name": "Kaffeemaschine"})
	fake.Set("sensor.temp", "21.5", map[string]any{"friendly_name": "Temperatur", "unit_of_measurement": "°C"})

	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	dav := davfake.New("bote", "pw")
	t.Cleanup(dav.Close)
	dav.AddCalendar("/bote/familie/", "Haushalt", "VEVENT", "VTODO")
	dav.Put("/bote/familie/milch-1.ics", "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VTODO\r\nUID:milch-1\r\nDTSTAMP:20261010T080000Z\r\n"+
		"SUMMARY:Milch kaufen\r\nDUE:20261015T160000Z\r\nSTATUS:NEEDS-ACTION\r\nEND:VTODO\r\nEND:VCALENDAR\r\n")
	dav.AddAddressBook("/bote/kontakte/", "Haushalt")
	dav.Put("/bote/kontakte/b.vcf", "BEGIN:VCARD\r\nVERSION:4.0\r\nUID:urn:uuid:b\r\nFN:Bernd Bauer\r\nTEL:0170 1234567\r\nEMAIL:bernd@example.org\r\nBDAY:--0314\r\nEND:VCARD\r\n")
	cal := &caldav.Client{URL: dav.URL, User: "bote", Password: "pw", Loc: loc}

	docs := paperlessfake.New("pl-token")
	t.Cleanup(docs.Close)
	docs.Add(paperlessfake.Doc{ID: 2, Title: "Stromrechnung", Created: "2026-09-30"})

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "einkauf.md"), []byte("# Einkauf\n\nMilch"), 0o644); err != nil {
		t.Fatal(err)
	}
	nd, err := notes.Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	s.hh = haushalt.Config{Calendar: cal, Contacts: cal, Notes: nd, Loc: loc,
		Documents: &paperless.Client{URL: docs.URL, Token: "pl-token", PublicURL: "https://paperless.example"}}
	s.napi.HA = &ha.Client{URL: fake.URL, Token: "ha-token"}
	s.napi.Haushalt = s.hh
	s.napi.Validate = func(_ context.Context, b string) bool { return b == "dev1" }
	return s, fake, dav
}

func napiGet(t *testing.T, h http.Handler, path string) map[string]any {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	req.Header.Set("Authorization", "Bearer dev1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &out) != nil {
		t.Fatalf("GET %s → %d %s", path, rec.Code, rec.Body.String())
	}
	return out
}

func page(h http.Handler, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", path, nil)
	req.Header.Set("X-Haus-Proxy", "geheim")
	req.Header.Set("Remote-User", "anna")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// haus#28: every thing and every tool row a widget shows names its page on
// Haus (page), and that page is there: the thing's own page with its name,
// room and state, a reminder's, a contact's, a note's, and a document's,
// which leads on to Paperless. The app opens page in the browser, where the
// person's Authelia session is.
func TestEveryPageAWidgetIsHandedIsThere(t *testing.T) {
	s, _, _ := fullSite(t)
	h := routes(s)
	type want struct {
		page string
		says []string
	}
	var got []want
	pageOf := func(where string, row map[string]any, says ...string) {
		p, _ := row["page"].(string)
		if !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "/napi/") {
			t.Errorf("%s: no page of Haus's: %v", where, row)
			return
		}
		got = append(got, want{p, says})
	}
	state := napiGet(t, h, "/napi/portal/state?entity_id=light.decke")
	card, _ := state["card"].(map[string]any)
	pageOf("the device widget's card", card, "Deckenlampe", "Küche", "an · 50 %")
	active, _ := napiGet(t, h, "/napi/portal/active")["active"].([]any)
	if len(active) != 2 {
		t.Fatalf("active: %v", active)
	}
	for _, a := range active {
		c := a.(map[string]any)
		pageOf("an active row", c, c["name"].(string))
	}
	for _, tool := range []struct{ path, key, field string }{
		{"/napi/portal/tasks", "tasks", "title"},
		{"/napi/portal/contacts", "contacts", "name"},
		{"/napi/portal/notes", "notes", "title"},
	} {
		rows, _ := napiGet(t, h, tool.path)[tool.key].([]any)
		if len(rows) == 0 {
			t.Fatalf("%s: no rows", tool.path)
		}
		for _, r := range rows {
			row := r.(map[string]any)
			pageOf(tool.path, row, row[tool.field].(string))
		}
	}
	for _, w := range got {
		rec := page(h, w.page)
		body := rec.Body.String()
		if rec.Code != 200 {
			t.Errorf("%s → %d, want 200: %s", w.page, rec.Code, body)
			continue
		}
		for _, s := range w.says {
			if !strings.Contains(body, s) {
				t.Errorf("%s does not say %q:\n%s", w.page, s, body)
			}
		}
	}
	if len(got) < 6 {
		t.Fatalf("only %d pages were handed out", len(got))
	}
	// a document's page is Paperless's own, behind its own login
	docs, _ := napiGet(t, h, "/napi/portal/documents")["documents"].([]any)
	if len(docs) != 1 {
		t.Fatalf("documents: %v", docs)
	}
	p, _ := docs[0].(map[string]any)["page"].(string)
	if rec := page(h, p); rec.Code != http.StatusFound || rec.Header().Get("Location") != "https://paperless.example/documents/2/details" {
		t.Fatalf("document page %q → %d %q", p, rec.Code, rec.Header().Get("Location"))
	}
}

// What is not there says so plainly, a 404 with a way back, never Start
// pretending all is well.
func TestAnItemPageForNothingIsA404(t *testing.T) {
	s, _, _ := fullSite(t)
	h := routes(s)
	for _, p := range []string{"/raeume/light.gibtsnicht", "/kalender/erinnerung/gibtsnicht", "/kontakte/gibtsnicht"} {
		rec := page(h, p)
		if rec.Code != 404 || !strings.Contains(rec.Body.String(), "gibt es nicht") {
			t.Errorf("%s → %d, want a 404 that says so: %s", p, rec.Code, rec.Body.String())
		}
	}
}

// A thing's page shows only what a widget may show: an entity of a card
// domain (the ones /napi/portal/state draws) that HA does not hide. A
// person's whereabouts, a phone's address sensor in another domain, a
// hidden entity: each is a 404, never a page.
func TestAThingPageShowsOnlyCardDomainsNotHidden(t *testing.T) {
	s, fake, _ := fullSite(t)
	fake.SetRegistries([]map[string]any{{"area_id": "kueche", "name": "Küche"}}, nil, []map[string]any{
		{"entity_id": "light.decke", "area_id": "kueche"},
		{"entity_id": "light.versteckt", "hidden_by": "user"},
		{"entity_id": "sensor.diagnose", "entity_category": "diagnostic"},
	})
	fake.Set("person.anna", "Arbeit", map[string]any{"friendly_name": "Anna"})
	fake.Set("device_tracker.anna_handy", "home", map[string]any{"friendly_name": "Annas Handy"})
	fake.Set("zone.home", "1", map[string]any{"friendly_name": "Zuhause"})
	fake.Set("light.versteckt", "off", map[string]any{"friendly_name": "Versteckt"})
	fake.Set("sensor.diagnose", "3", map[string]any{"friendly_name": "Diagnose"})
	h := routes(s)
	for _, p := range []string{"/raeume/person.anna", "/raeume/device_tracker.anna_handy", "/raeume/zone.home",
		"/raeume/light.versteckt", "/raeume/sensor.diagnose"} {
		rec := page(h, p)
		if body := rec.Body.String(); rec.Code != 404 || !strings.Contains(body, "gibt es nicht") ||
			strings.Contains(body, "Arbeit") || strings.Contains(body, "Versteckt") || strings.Contains(body, "Diagnose") {
			t.Errorf("%s → %d, want a 404 that shows nothing of it: %s", p, rec.Code, body)
		}
	}
	if rec := page(h, "/raeume/light.decke"); rec.Code != 200 {
		t.Fatalf("a card domain, not hidden → %d", rec.Code)
	}
}

// A thing's state in the family's words, for each kind a widget shows.
func TestAThingPageSaysItsStateInWords(t *testing.T) {
	s, fake, _ := fullSite(t)
	fake.Set("switch.radio", "off", map[string]any{"friendly_name": "Radio"})
	fake.Set("binary_sensor.haustuer", "on", map[string]any{"friendly_name": "Haustür", "device_class": "door"})
	fake.Set("binary_sensor.fenster", "off", map[string]any{"friendly_name": "Fenster", "device_class": "window"})
	fake.Set("binary_sensor.flur", "on", map[string]any{"friendly_name": "Bewegung", "device_class": "motion"})
	fake.Set("lock.keller", "locked", map[string]any{"friendly_name": "Keller"})
	fake.Set("lock.hof", "unlocked", map[string]any{"friendly_name": "Hoftor"})
	h := routes(s)
	for id, want := range map[string]string{
		"switch.radio": "<li>aus ", "binary_sensor.haustuer": "<li>offen ", "binary_sensor.fenster": "<li>geschlossen ",
		"binary_sensor.flur": "<li>an ", "lock.keller": "<li>verriegelt ", "lock.hof": "<li>offen ",
		"sensor.temp": "<li>21.5 °C ", "light.decke": "<li>an · 50 % ",
	} {
		rec := page(h, "/raeume/"+id)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), want) {
			t.Errorf("%s → %d, want %q:\n%s", id, rec.Code, want, rec.Body.String())
		}
	}
}

// What a name or title holds is text, in the heading and the title alike.
func TestItemPagesEscapeWhatTheyShow(t *testing.T) {
	s, fake, dav := fullSite(t)
	evil := "<script>alert(1)</script>"
	fake.Set("sensor.boese", "1", map[string]any{"friendly_name": evil})
	dav.Put("/bote/familie/boese.ics", "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VTODO\r\nUID:boese\r\nDTSTAMP:20261010T080000Z\r\n"+
		"SUMMARY:"+evil+"\r\nEND:VTODO\r\nEND:VCALENDAR\r\n")
	dav.Put("/bote/kontakte/boese.vcf", "BEGIN:VCARD\r\nVERSION:4.0\r\nUID:boese\r\nFN:"+evil+"\r\nEND:VCARD\r\n")
	h := routes(s)
	for _, p := range []string{"/raeume/sensor.boese", "/kalender/erinnerung/boese", "/kontakte/boese"} {
		rec := page(h, p)
		body := rec.Body.String()
		if rec.Code != 200 || strings.Contains(body, "<script>") ||
			!strings.Contains(body, "<h1>&lt;script&gt;alert(1)&lt;/script&gt;</h1>") ||
			!strings.Contains(body, "<title>&lt;script&gt;alert(1)&lt;/script&gt; · ") {
			t.Errorf("%s → %d, not escaped:\n%s", p, rec.Code, body)
		}
	}
}

// A reminder's page says what became of it: cancelled is not done.
func TestAReminderPageSaysCancelledOrDone(t *testing.T) {
	s, _, dav := fullSite(t)
	dav.Put("/bote/familie/abgesagt.ics", "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VTODO\r\nUID:abgesagt\r\nDTSTAMP:20261010T080000Z\r\n"+
		"SUMMARY:Feier\r\nSTATUS:CANCELLED\r\nEND:VTODO\r\nEND:VCALENDAR\r\n")
	dav.Put("/bote/familie/fertig.ics", "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VTODO\r\nUID:fertig\r\nDTSTAMP:20261010T080000Z\r\n"+
		"SUMMARY:Abwasch\r\nSTATUS:COMPLETED\r\nCOMPLETED:20261011T080000Z\r\nEND:VTODO\r\nEND:VCALENDAR\r\n")
	h := routes(s)
	if body := page(h, "/kalender/erinnerung/abgesagt").Body.String(); !strings.Contains(body, "<li>abgesagt") || strings.Contains(body, "erledigt") {
		t.Errorf("cancelled:\n%s", body)
	}
	if body := page(h, "/kalender/erinnerung/fertig").Body.String(); !strings.Contains(body, "<li>erledigt") || strings.Contains(body, "abgesagt") {
		t.Errorf("done:\n%s", body)
	}
	if body := page(h, "/kalender/erinnerung/milch-1").Body.String(); !strings.Contains(body, "<li>offen") {
		t.Errorf("open:\n%s", body)
	}
}

// A document's page needs a real id and Paperless's public address; with
// neither there is nowhere to send the tap: a 404, and no row names a page.
func TestADocumentPageWantsAnIdAndAnAddress(t *testing.T) {
	s, _, _ := fullSite(t)
	h := routes(s)
	for _, p := range []string{"/dokumente/0", "/dokumente/-3", "/dokumente/x"} {
		if rec := page(h, p); rec.Code != 404 {
			t.Errorf("%s → %d, want 404", p, rec.Code)
		}
	}
	s.hh.Documents.PublicURL = ""
	h = routes(s)
	if rec := page(h, "/dokumente/2"); rec.Code != 404 || rec.Header().Get("Location") != "" {
		t.Errorf("without Paperless's address → %d %q, want 404", rec.Code, rec.Header().Get("Location"))
	}
	docs, _ := napiGet(t, h, "/napi/portal/documents")["documents"].([]any)
	if len(docs) != 1 {
		t.Fatalf("documents: %v", docs)
	}
	if p, ok := docs[0].(map[string]any)["page"]; ok {
		t.Fatalf("a document row without Paperless's address names a page: %v", p)
	}
}
