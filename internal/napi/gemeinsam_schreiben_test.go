package napi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/homes/internal/caldav"
	"github.com/mdopp/homes/internal/haushalt"
	"github.com/mdopp/homes/internal/papierkorb"
)

// Owner 2026-11-05: the Homes account is writable. A phone sends one
// appointment or card back by its UID, over the version it last saw; a
// delete goes into the Papierkorb.

func schreibHandler(t *testing.T) (http.Handler, *papierkorb.Korb, func() map[string]string) {
	t.Helper()
	fake, _ := gemeinsamFake(t)
	fake.AddCalendar("/homes/abo-ferien/", "Ferien")
	berlin, _ := time.LoadLocation("Europe/Berlin")
	dav := &caldav.Client{URL: fake.URL, User: "homes", Password: "pw", Loc: berlin, Now: func() time.Time { return gemeinsamNow }}
	korb := &papierkorb.Korb{Dir: t.TempDir(), Now: func() time.Time { return gemeinsamNow }}
	people := map[string]string{"anna1": "anna", "waise1": ""}
	h := Handler(Config{Validate: StaticValidator([]string{"anna1", "waise1"}), Haushalt: haushalt.Config{Loc: berlin}, Konto: dav, Papierkorb: korb,
		Whoami: func(b string) (string, bool) { p, ok := people[b]; return p, ok }, Now: func() time.Time { return gemeinsamNow }})
	return h, korb, fake.Objects
}

func send(t *testing.T, h http.Handler, method, path, body string, hdr ...string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer anna1")
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

const elternabend = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:abend@phone\r\nDTSTART:20261110T170000Z\r\nDTEND:20261110T180000Z\r\nSUMMARY:Elternabend\r\nBEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER:-PT15M\r\nEND:VALARM\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"

func TestGemeinsamTerminSchreiben(t *testing.T) {
	h, _, objects := schreibHandler(t)
	path := "/napi/gemeinsam/kalender/haushalt/abend@phone"
	if rec := send(t, h, "PUT", path, elternabend); rec.Code != http.StatusPreconditionRequired {
		t.Fatalf("no precondition: %d", rec.Code)
	}
	rec := send(t, h, "PUT", path, elternabend, "If-None-Match", "*", "Content-Type", "text/calendar")
	var made struct{ UID, Kalender, ETag string }
	if rec.Code != http.StatusCreated || json.Unmarshal(rec.Body.Bytes(), &made) != nil || made.UID != "abend@phone" || made.Kalender != "haushalt" ||
		made.ETag == "" || rec.Header().Get("ETag") != made.ETag {
		t.Fatalf("made: %d %s %v", rec.Code, rec.Body, rec.Header())
	}
	if ics := objects()["/homes/haushalt/abend@phone.ics"]; !strings.Contains(ics, "TRIGGER:-PT15M") {
		t.Fatalf("stored:\n%s", ics)
	}
	changed := strings.Replace(elternabend, "Elternabend", "Elternabend, Aula", 1)
	rec = send(t, h, "PUT", path, changed, "If-Match", made.ETag)
	var now struct{ ETag string }
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &now) != nil || now.ETag == made.ETag {
		t.Fatalf("changed: %d %s", rec.Code, rec.Body)
	}
	rec = send(t, h, "PUT", path, elternabend, "If-Match", made.ETag) // the phone's old version
	if rec.Code != http.StatusPreconditionFailed || rec.Header().Get("ETag") != now.ETag || !strings.Contains(rec.Body.String(), "Elternabend, Aula") {
		t.Fatalf("conflict: %d %v %s", rec.Code, rec.Header(), rec.Body)
	}
	for p, want := range map[string]int{"/napi/gemeinsam/kalender/abo-ferien/x": 403, "/napi/gemeinsam/kalender/erinnerungen/x": 403, "/napi/gemeinsam/kalender/haushalt/anders": 400} {
		if rec := send(t, h, "PUT", p, elternabend, "If-None-Match", "*"); rec.Code != want {
			t.Errorf("%s: %d %s, want %d", p, rec.Code, rec.Body, want)
		}
	}
}

func TestGemeinsamLoeschenInDenPapierkorb(t *testing.T) {
	h, korb, objects := schreibHandler(t)
	feed := getGemeinsam(t, h, "/napi/gemeinsam/kontakte?schreibbar=1", "anna1", "").Body.String()
	etag := strings.TrimSpace(strings.SplitN(strings.SplitN(feed, "X-HOMES-ETAG:", 2)[1], "\r\n", 2)[0])
	if etag == "" {
		t.Fatalf("no tag:\n%s", feed)
	}
	if rec := send(t, h, "DELETE", "/napi/gemeinsam/kontakte/oma", ""); rec.Code != http.StatusPreconditionRequired {
		t.Fatalf("no If-Match: %d", rec.Code)
	}
	if rec := send(t, h, "DELETE", "/napi/gemeinsam/kontakte/oma", "", "If-Match", `"0"`); rec.Code != http.StatusPreconditionFailed || !strings.Contains(rec.Body.String(), "FN:Oma Erna") {
		t.Fatalf("stale: %d %s", rec.Code, rec.Body)
	}
	if len(korb.Liste("")) != 0 {
		t.Fatal("a refused delete stays in the Papierkorb")
	}
	if rec := send(t, h, "DELETE", "/napi/gemeinsam/kontakte/oma", "", "If-Match", etag); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body)
	}
	if _, ok := objects()["/homes/haushalt-kontakte/oma.vcf"]; ok {
		t.Fatal("still in Radicale")
	}
	es := korb.Liste("kontakt")
	var d caldav.Stored
	if len(es) != 1 || es[0].Name != "Oma Erna" || es[0].Wer != "anna" || json.Unmarshal(es[0].Daten, &d) != nil || d.Collection != "/homes/haushalt-kontakte/" || !strings.Contains(d.Data, "TEL") {
		t.Fatalf("Papierkorb: %+v %+v", es, d)
	}
	if rec := send(t, h, "DELETE", "/napi/gemeinsam/kontakte/oma", "", "If-Match", etag); rec.Code != http.StatusPreconditionFailed || rec.Body.Len() != 0 {
		t.Fatalf("gone already: %d %s", rec.Code, rec.Body)
	}
}

func TestGemeinsamSchreibbarFeed(t *testing.T) {
	h, _, _ := schreibHandler(t)
	ics := getGemeinsam(t, h, "/napi/gemeinsam/kalender?schreibbar=1", "anna1", "").Body.String()
	if strings.Count(ics, "X-HOMES-SCHREIBBAR:TRUE") != 2 || !strings.Contains(ics, "X-HOMES-ETAG:") || !strings.Contains(ics, "SUMMARY:Müll, Papier") {
		t.Fatalf("haushalt and familie-urlaub writable, abo-ferien not:\n%s", ics)
	}
	if old := getGemeinsam(t, h, "/napi/gemeinsam/kalender", "anna1", "").Body.String(); strings.Contains(old, "X-HOMES-") {
		t.Fatalf("the phones that only read get what they got:\n%s", old)
	}
}
