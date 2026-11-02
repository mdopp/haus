package napi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mdopp/haus/internal/caldav"
	"github.com/mdopp/haus/internal/caldav/davfake"
	"github.com/mdopp/haus/internal/haushalt"
)

// laib ADR 0029: the app sends all of a person's phone contacts; Haus, as
// its own Radicale account, mirrors them into <person>/handy-kontakte/.

func vcard(uid, fn string) string {
	s := "BEGIN:VCARD\r\nVERSION:3.0\r\n"
	if uid != "" {
		s += "UID:" + uid + "\r\n"
	}
	return s + "FN:" + fn + "\r\nTEL;TYPE=CELL:+49 170 1234567\r\nEND:VCARD\r\n"
}

func newHandy(t *testing.T, dav *caldav.Client) http.Handler {
	t.Helper()
	people := map[string]string{"anna1": "anna", "ben1": "ben", "waise1": ""}
	return Handler(Config{Validate: StaticValidator([]string{"anna1", "ben1", "waise1"}),
		Haushalt: haushalt.Config{Contacts: dav},
		Whoami:   func(b string) (string, bool) { p, ok := people[b]; return p, ok }})
}

func sendKontakte(t *testing.T, h http.Handler, bearer, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest("POST", "/napi/kontakte", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Content-Type", "text/vcard; charset=utf-8")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func fakeRadicale(t *testing.T) (*davfake.Server, http.Handler) {
	fake := davfake.New("haus", "pw")
	t.Cleanup(fake.Close)
	return fake, newHandy(t, &caldav.Client{URL: fake.URL, User: "haus", Password: "pw"})
}

func cardsUnder(fake *davfake.Server, prefix string) map[string]string {
	out := map[string]string{}
	for p, v := range fake.Objects() {
		if strings.HasPrefix(p, prefix) {
			out[p] = v
		}
	}
	return out
}

func want(t *testing.T, out map[string]any, kontakte, neu, geaendert, entfernt float64) {
	t.Helper()
	if out["kontakte"] != kontakte || out["neu"] != neu || out["geaendert"] != geaendert || out["entfernt"] != entfernt {
		t.Fatalf("answer %v, want kontakte %v neu %v geaendert %v entfernt %v", out, kontakte, neu, geaendert, entfernt)
	}
}

// The first upload makes the book and writes each card at a place its UID
// names; the same upload again writes nothing; a contact gone from the
// phone goes from the book, a changed one is written over.
func TestKontakteMirror(t *testing.T) {
	fake, h := fakeRadicale(t)
	all := vcard("0r1-2A.%3D", "Oma Erna") + vcard("lookup-2", "Ben Bauer") + vcard("lookup-3", "Eva Müller")
	code, out := sendKontakte(t, h, "anna1", all)
	if code != 200 {
		t.Fatalf("first upload → %d %v", code, out)
	}
	want(t, out, 3, 3, 0, 0)
	if got := fake.AddressBooks(); len(got) != 1 || got[0] != "/anna/handy-kontakte/" || fake.DisplayName(got[0]) != "Handy-Kontakte" {
		t.Fatalf("books %v", got)
	}
	cards := cardsUnder(fake, "/anna/handy-kontakte/")
	if len(cards) != 3 {
		t.Fatalf("cards %v", cards)
	}
	for p, v := range cards {
		name := strings.TrimPrefix(p, "/anna/handy-kontakte/")
		if len(name) != 36 || strings.Trim(strings.TrimSuffix(name, ".vcf"), "0123456789abcdef") != "" {
			t.Errorf("href %q is not a hash of the UID", name)
		}
		if !strings.Contains(v, "X-HAUS-INHALT:") {
			t.Errorf("card without Haus's note: %q", v)
		}
	}
	if _, ok := cards["/anna/handy-kontakte/"+caldav.CardHref("0r1-2A.%3D")]; !ok {
		t.Fatalf("Oma's card is not at her UID's place: %v", cards)
	}

	puts := len(fake.Header)
	code, out = sendKontakte(t, h, "anna1", all)
	if code != 200 {
		t.Fatalf("second upload → %d %v", code, out)
	}
	want(t, out, 3, 0, 0, 0)
	if len(fake.Header) != puts {
		t.Fatalf("an unchanged upload wrote %d cards", len(fake.Header)-puts)
	}

	code, out = sendKontakte(t, h, "anna1", vcard("0r1-2A.%3D", "Oma Erna Schmidt")+vcard("lookup-2", "Ben Bauer"))
	if code != 200 {
		t.Fatalf("third upload → %d %v", code, out)
	}
	want(t, out, 2, 0, 1, 1)
	cards = cardsUnder(fake, "/anna/handy-kontakte/")
	if len(cards) != 2 {
		t.Fatalf("Eva is still there: %v", cards)
	}
	if !strings.Contains(cards["/anna/handy-kontakte/"+caldav.CardHref("0r1-2A.%3D")], "FN:Oma Erna Schmidt") {
		t.Fatalf("Oma's card was not written over: %v", cards)
	}
}

// The person is the device's: Ben's phone writes Ben's book, whatever the
// cards say; a device of nobody writes nothing.
func TestKontaktePersonIsTheDevices(t *testing.T) {
	fake, h := fakeRadicale(t)
	if code, out := sendKontakte(t, h, "ben1", vcard("x", "Anna")); code != 200 {
		t.Fatalf("ben → %d %v", code, out)
	}
	if len(cardsUnder(fake, "/ben/handy-kontakte/")) != 1 || len(cardsUnder(fake, "/anna/")) != 0 {
		t.Fatalf("objects %v", fake.Objects())
	}
	code, out := sendKontakte(t, h, "waise1", vcard("y", "Niemand"))
	if code != 403 || out["fehler"] != saysNoPerson {
		t.Fatalf("a device of nobody → %d %v", code, out)
	}
	if len(fake.Objects()) != 1 {
		t.Fatalf("objects %v", fake.Objects())
	}
}

// A card without a UID cannot be placed: the whole upload is refused, in
// words naming the contact, and nothing is written.
func TestKontakteCardWithoutUID(t *testing.T) {
	fake, h := fakeRadicale(t)
	code, out := sendKontakte(t, h, "anna1", vcard("a", "Anna")+vcard("", "Oma Erna"))
	if code != 400 || !strings.Contains(out["fehler"].(string), "„Oma Erna“ hat keine UID") {
		t.Fatalf("→ %d %v", code, out)
	}
	if len(fake.Methods) != 0 {
		t.Fatalf("Radicale was asked: %v", fake.Methods)
	}
}

// No cards at all — a permission taken back — never empties the mirror.
func TestKontakteEmptyUploadIsRefused(t *testing.T) {
	fake, h := fakeRadicale(t)
	if code, _ := sendKontakte(t, h, "anna1", vcard("a", "Anna")); code != 200 {
		t.Fatal(code)
	}
	for _, body := range []string{"", "\r\n", "nichts"} {
		code, out := sendKontakte(t, h, "anna1", body)
		if code != 409 || out["fehler"] != saysKontakteLeer {
			t.Fatalf("%q → %d %v", body, code, out)
		}
	}
	if len(cardsUnder(fake, "/anna/handy-kontakte/")) != 1 {
		t.Fatalf("the mirror was touched: %v", fake.Objects())
	}
}

// What is not a set of whole cards, not text/vcard, or too big is refused
// in words; without Radicale the route says so.
func TestKontakteRefusals(t *testing.T) {
	_, h := fakeRadicale(t)
	if code, out := sendKontakte(t, h, "anna1", "BEGIN:VCARD\r\nUID:a\r\nFN:A\r\n"); code != 400 || out["fehler"] != saysKontakteBroken {
		t.Fatalf("a cut card → %d %v", code, out)
	}
	req := httptest.NewRequest("POST", "/napi/kontakte", strings.NewReader(vcard("a", "A")))
	req.Header.Set("Authorization", "Bearer anna1")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 415 {
		t.Fatalf("json → %d", rec.Code)
	}
	old := kontakteMaxBody
	kontakteMaxBody = 100
	t.Cleanup(func() { kontakteMaxBody = old })
	if code, out := sendKontakte(t, h, "anna1", strings.Repeat(vcard("a", "A"), 5)); code != 413 || out["fehler"] != saysKontakteTooBig {
		t.Fatalf("too big → %d %v", code, out)
	}
	if code, out := sendKontakte(t, newHandy(t, nil), "anna1", vcard("a", "A")); code != 503 || out["fehler"] != saysNoRadicale {
		t.Fatalf("no Radicale → %d %v", code, out)
	}
}
