package pages

import (
	"bytes"
	"context"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/homes/internal/caldav/davfake"
	"github.com/mdopp/homes/internal/kontaktantwort"
)

const pruefenKarte = "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:h1\r\nFN:Anna Beispiel\r\nN:Beispiel;Anna;;;\r\nTEL;TYPE=CELL:+49 151 23456789\r\n" +
	"EMAIL:anna@example.org\r\nORG:Bäckerei\r\nX-IBAN:DE00\r\nEND:VCARD\r\n"

type noDNS struct{}

func (noDNS) LookupMX(context.Context, string) ([]*net.MX, error) {
	return []*net.MX{{Host: "mx.example."}}, nil
}
func (noDNS) LookupHost(context.Context, string) ([]string, error) { return []string{"192.0.2.1"}, nil }

// pruefenCfg is a house with one shared card, a Handy-Kontakt of anna's
// and the store; the cards' text is dav's.
func pruefenCfg(t *testing.T) (StartConfig, *davfake.Server) {
	t.Helper()
	dav, c := calendar(t)
	dav.AddAddressBook("/homes/haushalt-kontakte/", "Haushalt")
	dav.Put("/homes/haushalt-kontakte/h1.vcf", pruefenKarte)
	dav.AddAddressBook("/anna/handy-kontakte/", "Handy-Kontakte")
	dav.Put("/anna/handy-kontakte/k1.vcf", "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:3789r1-2B4D\r\nFN:Oma <b>Hilde</b>\r\nTEL;TYPE=HOME:0711 445566\r\nEND:VCARD\r\n")
	now := startNow(t)
	return StartConfig{Contacts: c, Antworten: &kontaktantwort.Store{Path: filepath.Join(t.TempDir(), kontaktantwort.Datei)}, DNS: noDNS{},
		KontaktURL: "https://kontakt.example.org", Loc: berlin(t), Now: func() time.Time { return now }}, dav
}

// frag sends one request as Homes' own page would (anna, same-origin) or, user "", as anyone.
func frag(h http.Handler, method, target, user, groups string, form url.Values) *httptest.ResponseRecorder {
	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	req := httptest.NewRequest(method, target, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
	}
	if user != "" {
		req.Header.Set("Remote-User", user)
		req.Header.Set("Remote-Groups", groups)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

var linkRE = regexp.MustCompile(`https://kontakt\.example\.org/([A-Za-z0-9_-]{43})`)

// startLink makes the link for buch/uid as anna and returns its token.
func startLink(t *testing.T, cfg StartConfig, buch, uid, groups string) string {
	t.Helper()
	rec := frag(Pruefen(cfg), "POST", PruefenPath, "anna", groups, url.Values{"buch": {buch}, "uid": {uid}})
	m := linkRE.FindStringSubmatch(rec.Body.String())
	if rec.Code != 200 || m == nil {
		t.Fatalf("no link made (%d):\n%s", rec.Code, rec.Body.String())
	}
	mustContain(t, rec.Body.String(), "Link erstellt", "Teilen …", "Kopieren", "nur einmal")
	return m[1]
}

func formOf(vals map[string][]string) url.Values { return url.Values(vals) }

// owner 2026-11-07: the link shows the contact's own fields and nothing
// else of the house; one answer, then dead; the review before → after;
// only what was taken is written, over the card's other lines.
func TestKontaktPruefenFromTheLinkToTheAddressBook(t *testing.T) {
	cfg, dav := pruefenCfg(t)
	var logs bytes.Buffer
	log.SetOutput(&logs)
	defer log.SetOutput(os.Stderr)
	token := startLink(t, cfg, "haushalt", "h1", "family,admins")
	pub := KontaktServer(cfg)

	rec := frag(pub, "GET", "/"+token, "", "", nil)
	body := rec.Body.String()
	if rec.Code != 200 {
		t.Fatalf("form %d", rec.Code)
	}
	mustContain(t, body, "Stimmen deine Kontaktdaten?", `name=vorname type=text value="Anna"`, `name=nachname type=text value="Beispiel"`,
		`value="+49 151 23456789"`, `<option value=mobil selected>`, `value="anna@example.org"`, `name=firma type=text value="Bäckerei"`,
		`action="/`+token+`"`, "Alles stimmt so", "Änderungen senden")
	mustNotContain(t, body, "DE00", "Oma", "/kontakte", "/start", "Abmelden", "<nav")
	for k, want := range map[string]string{"Referrer-Policy": "no-referrer", "Cache-Control": "no-store", "X-Frame-Options": "DENY"} {
		if got := rec.Header().Get(k); got != want {
			t.Errorf("%s: %q, want %q", k, got, want)
		}
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'none'") || !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Errorf("CSP %q", csp)
	}

	answer := formOf(map[string][]string{"vorname": {"Anna"}, "nachname": {"Muster"}, "tel": {"+49 151 99999999", "0711 1234567"}, "tel-art": {"mobil", "geschaeftlich"},
		"mail": {"anna@example.org", ""}, "adr-strasse": {"Neuer Weg 2"}, "adr-plz": {"12345"}, "adr-ort": {"Stadt"}, "adr-land": {"Deutschland"},
		"geb-tag": {"12"}, "geb-monat": {"4"}, "geb-jahr": {""}, "firma": {"Bäckerei"}, "position": {"Inhaberin"}, "aktion": {"senden"}})
	if rec := frag(pub, "POST", "/"+token, "", "", answer); rec.Code != 200 || !strings.Contains(rec.Body.String(), "Danke!") {
		t.Fatalf("answer %d:\n%s", rec.Code, rec.Body.String())
	}
	if card := dav.Objects()["/homes/haushalt-kontakte/h1.vcf"]; card != pruefenKarte {
		t.Fatalf("the answer was written before the review:\n%s", card)
	}
	if rec := frag(pub, "GET", "/"+token, "", "", nil); rec.Code != 404 || !strings.Contains(rec.Body.String(), "Link ungültig oder abgelaufen") {
		t.Errorf("an answered link: %d", rec.Code)
	}
	if rec := frag(pub, "POST", "/"+token, "", "", answer); rec.Code != 404 {
		t.Errorf("a second answer: %d", rec.Code)
	}

	open := cfg.Antworten.ZuPruefen("anna")
	if len(open) != 1 {
		t.Fatalf("%d to review", len(open))
	}
	r := httptest.NewRequest("GET", "/aufgaben", nil)
	r.Header.Set("Remote-User", "anna")
	if as := einrichtung(r, cfg); len(as) == 0 || as[0].titel != "1 Kontakt-Antwort prüfen" || as[0].href != AntwortenPath {
		t.Errorf("tasks %+v", as)
	}
	id := open[0].ID
	rec = frag(Antworten(cfg), "GET", AntwortenPath+"/"+id, "anna", "family", nil)
	body = rec.Body.String()
	mustContain(t, body, "<h1>Anna Beispiel</h1>", "Nachname", `<span class=vorher>Beispiel</span> → <span class=nachher>Muster</span>`,
		`<span class=vorher>+49 151 23456789 (mobil)</span> → <span class=nachher>+49 151 99999999 (mobil)</span>`,
		`0711 1234567 (geschäftlich)</span> <span class=k>(neu)`, "Neuer Weg 2, 12345 Stadt, Deutschland", "12.04.", "Inhaberin",
		"Ausgewählte übernehmen", "Alle übernehmen", "Verwerfen")
	if rec := frag(Antworten(cfg), "GET", AntwortenPath+"/"+id, "ben", "family", nil); rec.Code != 404 {
		t.Errorf("ben opens anna's review: %d", rec.Code)
	}

	// take the family name (0) and the new position (last) only
	got, _ := cfg.Antworten.Eine(id, "anna")
	last := len(got.Aenderungen) - 1
	rec = frag(Antworten(cfg), "POST", AntwortenPath+"/"+id, "anna", "family", url.Values{"aktion": {"auswahl"}, "nimm": {"0", strconv.Itoa(last)}})
	mustContain(t, rec.Body.String(), "2 Änderungen ins Adressbuch geschrieben")
	card := dav.Objects()["/homes/haushalt-kontakte/h1.vcf"]
	mustContain(t, card, "FN:Anna Muster", "N:Muster;Anna;;;", "TITLE:Inhaberin", "TEL;TYPE=CELL:+49 151 23456789", "X-IBAN:DE00")
	mustNotContain(t, card, "99999999", "Neuer Weg", "BDAY")
	if len(cfg.Antworten.ZuPruefen("anna")) != 0 {
		t.Error("a decided answer is still open")
	}
	for _, secret := range []string{"Beispiel", "Muster", "99999999", "anna@example.org", "Neuer Weg", "Inhaberin", token} {
		if strings.Contains(logs.String(), secret) {
			t.Errorf("the log holds %q:\n%s", secret, logs.String())
		}
	}
}

// The one page for every link that is none: never made, malformed, over;
// the same words, the same code. And nothing else of Homes on that listener.
func TestThePublicListenerServesTheFormOnly(t *testing.T) {
	cfg, _ := pruefenCfg(t)
	token := startLink(t, cfg, "privat", "3789r1-2B4D", "family")
	pub := KontaktServer(cfg)
	if rec := frag(pub, "GET", "/"+token, "", "", nil); !strings.Contains(rec.Body.String(), `value="Oma &lt;b&gt;Hilde&lt;/b&gt;"`) && !strings.Contains(rec.Body.String(), "&lt;b&gt;") {
		t.Errorf("the name is not escaped:\n%s", rec.Body.String())
	} else if strings.Contains(rec.Body.String(), "<b>Hilde") {
		t.Error("raw HTML from a card on the public page")
	}
	strip := regexp.MustCompile(`nonce="[^"]*"`)
	var first string
	for _, p := range []string{"/" + strings.Repeat("A", 43), "/x", "/" + token[:42], "/kontakte", "/start", "/napi/kann", "/" + token + "/x", "/" + token + "?a=1", "/healthz"} {
		rec := frag(pub, "GET", p, "anna", "admins", nil)
		b := strip.ReplaceAllString(rec.Body.String(), "")
		if rec.Code != 404 || !strings.Contains(b, "Link ungültig oder abgelaufen") {
			t.Errorf("%s: %d\n%s", p, rec.Code, b)
		}
		if first == "" {
			first = b
		} else if b != first {
			t.Errorf("%s answers differently from a link never made", p)
		}
	}
	if rec := frag(pub, "GET", "/robots.txt", "", "", nil); !strings.Contains(rec.Body.String(), "Disallow: /") {
		t.Errorf("robots.txt: %q", rec.Body.String())
	}
}

func TestThePublicPageRefusesWhatIsNotItsOwnForm(t *testing.T) {
	cfg, _ := pruefenCfg(t)
	token := startLink(t, cfg, "privat", "3789r1-2B4D", "family")
	pub := KontaktServer(cfg)
	req := httptest.NewRequest("POST", "/"+token, strings.NewReader("vorname=X&aktion=senden"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()
	pub.ServeHTTP(rec, req)
	if rec.Code != 404 {
		t.Errorf("a cross-site post: %d", rec.Code)
	}
	big := "vorname=" + strings.Repeat("x", 40<<10)
	req = httptest.NewRequest("POST", "/"+token, strings.NewReader(big))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	pub.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("a 40 KB body: %d", rec.Code)
	}
	rec = frag(pub, "POST", "/"+token, "", "", url.Values{"vorname": {"Hilde"}, "geb-tag": {"31"}, "geb-monat": {"2"}, "aktion": {"senden"}})
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "kein Tag im Kalender") {
		t.Errorf("31.2.: %d\n%s", rec.Code, rec.Body.String())
	}
	if _, err := cfg.Antworten.Zum(token); err != nil {
		t.Errorf("a refused form used the link up: %v", err)
	}
}

func TestThePublicPageIsRateLimited(t *testing.T) {
	cfg, _ := pruefenCfg(t)
	pub := KontaktServer(cfg)
	codes := map[int]int{}
	for range fragePro + 3 {
		req := httptest.NewRequest("GET", "/"+strings.Repeat("B", 43), nil)
		req.Header.Set("X-Forwarded-For", "203.0.113.9")
		rec := httptest.NewRecorder()
		pub.ServeHTTP(rec, req)
		codes[rec.Code]++
	}
	if codes[404] != fragePro || codes[http.StatusTooManyRequests] != 3 {
		t.Errorf("codes %v", codes)
	}
	req := httptest.NewRequest("GET", "/"+strings.Repeat("B", 43), nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.10")
	rec := httptest.NewRecorder()
	pub.ServeHTTP(rec, req)
	if rec.Code != 404 {
		t.Errorf("another address is limited too: %d", rec.Code)
	}
}

// Haushalts-Kontakte are an admin's; a person's own Handy-Kontakte theirs.
// A Handy-Kontakt's accepted changes wait for the phone; discarded, none.
func TestWhoMayStartAndWhereAHandyKontaktGoes(t *testing.T) {
	cfg, dav := pruefenCfg(t)
	if rec := frag(Pruefen(cfg), "POST", PruefenPath, "anna", "family", url.Values{"buch": {"haushalt"}, "uid": {"h1"}}); rec.Code != 403 {
		t.Errorf("a family member starts a house contact: %d", rec.Code)
	}
	req := httptest.NewRequest("POST", PruefenPath, strings.NewReader("buch=privat&uid=3789r1-2B4D"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Remote-User", "anna")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()
	Pruefen(cfg).ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Errorf("a link made from another site: %d", rec.Code)
	}
	code, body, _ := familyPage(t, Pruefen, cfg, "GET", PruefenPath, "family")
	if code != 200 || !strings.Contains(body, "Oma &lt;b&gt;Hilde&lt;/b&gt;") || strings.Contains(body, "Kontakte des Hauses") {
		t.Errorf("the start for the family: %d\n%s", code, body)
	}
	token := startLink(t, cfg, "privat", "3789r1-2B4D", "family")
	frag(KontaktServer(cfg), "POST", "/"+token, "", "", url.Values{"vorname": {"Hilde"}, "nachname": {"Oma"}, "tel": {"0711 445577"}, "tel-art": {"privat"}, "aktion": {"senden"}})
	f := cfg.Antworten.ZuPruefen("anna")[0]
	rec = frag(Antworten(cfg), "POST", AntwortenPath+"/"+f.ID, "anna", "family", url.Values{"aktion": {"alle"}})
	mustContain(t, rec.Body.String(), "gehen an dein Handy")
	if card := dav.Objects()["/anna/handy-kontakte/k1.vcf"]; strings.Contains(card, "445577") {
		t.Error("Homes wrote a Handy-Kontakt itself")
	}
	if fh := cfg.Antworten.FuersHandy("anna"); len(fh) != 1 || fh[0].UID != "3789r1-2B4D" {
		t.Errorf("for the phone: %+v", fh)
	}
}
