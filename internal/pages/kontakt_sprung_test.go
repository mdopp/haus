package pages

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mdopp/homes/internal/haushalt"
	"github.com/mdopp/homes/internal/napi"
	"github.com/mdopp/homes/internal/paperless"
	"github.com/mdopp/homes/internal/paperless/paperlessfake"
)

// Owner 2026-11-05: each household contact has two fixed links, keyed by
// its UID — „Dokumente“ to Paperless, „Nachrichten“ to the viewer's own
// message archive — on its page and in the phones' address book.

const (
	zuHause   = "192.168.1.23"
	unterwegs = "203.0.113.7"
)

// sprungConfig is a box with Paperless (LAN only, with its API) and an
// archive for anna, and an address book with Stadtwerke (an organisation
// Paperless knows), Oma (numbers and an e-mail) and Xaver (nothing).
func sprungConfig(t *testing.T) (StartConfig, *paperlessfake.Server) {
	t.Helper()
	dav, c := addressBook(t, "oma|Oma Erna|0170 1234567|erna@example.org", "x|Xaver||")
	dav.Put("/homes/haushalt-kontakte/berg.vcf", "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:berg\r\nFN:Herr Berg\r\nORG:Stadtwerke\r\nEND:VCARD\r\n")
	fake := paperlessfake.New("pl")
	t.Cleanup(fake.Close)
	fake.Correspondent(7, "Oma Erna")
	fake.Correspondent(8, "stadtwerke")
	return StartConfig{Contacts: c, Docs: &paperless.Client{URL: fake.URL, Token: "pl"}, PaperlessPublicURL: "https://paperless.box.test", PaperlessLANOnly: true,
		Archive: map[string]bool{"anna": true}, ArchivURL: "https://archiv.box.test", PhoneCountry: "49", PublicURL: "https://homes.box.test"}, fake
}

func oeffne(cfg StartConfig, path, user, from string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", path, nil)
	req.Header.Set("Remote-User", user)
	req.Header.Set("X-Forwarded-For", from)
	rec := httptest.NewRecorder()
	Kontakt(cfg).ServeHTTP(rec, req)
	return rec
}

func TestKontaktSprungDokumente(t *testing.T) {
	cfg, _ := sprungConfig(t)
	// a correspondent of that name: Paperless's list filtered by it
	if rec := oeffne(cfg, "/kontakte/oma/dokumente", "anna", zuHause); rec.Code != http.StatusFound ||
		rec.Header().Get("Location") != "https://paperless.box.test/documents?correspondent__id__in=7" {
		t.Fatalf("match: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	// the organisation's, when the name is none (case does not matter)
	if rec := oeffne(cfg, "/kontakte/berg/dokumente", "anna", zuHause); rec.Header().Get("Location") != "https://paperless.box.test/documents?correspondent__id__in=8" {
		t.Fatalf("organisation: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	// none: Paperless's full-text search for the name
	if rec := oeffne(cfg, "/kontakte/x/dokumente", "anna", zuHause); rec.Code != http.StatusFound ||
		rec.Header().Get("Location") != "https://paperless.box.test/documents?query=Xaver" {
		t.Fatalf("no match: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	// without the token Homes cannot ask: the search
	cfg.Docs = nil
	if rec := oeffne(cfg, "/kontakte/oma/dokumente", "anna", zuHause); rec.Code != http.StatusFound ||
		rec.Header().Get("Location") != "https://paperless.box.test/documents?query=Oma+Erna" {
		t.Fatalf("no token: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	// away: the Dokumente page's line, no dead redirect
	rec := oeffne(cfg, "/kontakte/oma/dokumente", "anna", unterwegs)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), saysPaperlessLAN) || rec.Header().Get("Location") != "" ||
		!strings.Contains(rec.Body.String(), `<a href=/kontakte/oma>Zurück zu Oma Erna</a>`) {
		t.Fatalf("away: %d %s", rec.Code, rec.Body)
	}
	// no Paperless on the box: one plain line
	cfg.PaperlessPublicURL = ""
	if rec := oeffne(cfg, "/kontakte/oma/dokumente", "anna", zuHause); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Auf dieser Box gibt es kein Paperless.") {
		t.Fatalf("no Paperless: %d %s", rec.Code, rec.Body)
	}
	// an unknown contact stays the page's 404
	if rec := oeffne(cfg, "/kontakte/niemand/dokumente", "anna", zuHause); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown: %d", rec.Code)
	}
}

// archivQuery is the msgvault search a redirect to the archive carries.
func archivQuery(t *testing.T, loc string) string {
	t.Helper()
	u, err := url.Parse(loc)
	if err != nil || u.Scheme+"://"+u.Host+u.Path != "https://archiv.box.test/" {
		t.Fatalf("not the archive: %q", loc)
	}
	q := u.Query()
	var explore struct {
		SchemaVersion int
		Query         string
	}
	if q.Get("workspace") != "everything" || q.Get("mode") != "full_text" || json.Unmarshal([]byte(q.Get("explore")), &explore) != nil || explore.SchemaVersion != 2 {
		t.Fatalf("not msgvault's view: %q", loc)
	}
	return explore.Query
}

func TestKontaktSprungNachrichten(t *testing.T) {
	cfg, _ := sprungConfig(t)
	// the numbers as +49…, the e-mail, each quoted, as alternatives
	rec := oeffne(cfg, "/kontakte/oma/nachrichten", "anna", zuHause)
	if rec.Code != http.StatusFound || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("%d %v", rec.Code, rec.Header())
	}
	if q := archivQuery(t, rec.Header().Get("Location")); q != `"+491701234567" | "erna@example.org"` {
		t.Fatalf("query %q", q)
	}
	// neither: the name
	if q := archivQuery(t, oeffne(cfg, "/kontakte/x/nachrichten", "anna", zuHause).Header().Get("Location")); q != `"Xaver"` {
		t.Fatalf("by name %q", q)
	}
	// a viewer without an archive: one plain line
	if rec := oeffne(cfg, "/kontakte/oma/nachrichten", "bert", zuHause); rec.Code != http.StatusOK || rec.Header().Get("Location") != "" ||
		!strings.Contains(rec.Body.String(), "Für dich gibt es auf dieser Box noch kein Archiv für Nachrichten.") {
		t.Fatalf("no archive: %d %s", rec.Code, rec.Body)
	}
	// away: the archive is the house's network's only
	if rec := oeffne(cfg, "/kontakte/oma/nachrichten", "anna", unterwegs); rec.Code != http.StatusOK ||
		!strings.Contains(rec.Body.String(), "Dein Archiv öffnet sich nur zu Hause im WLAN; gerade bist du unterwegs.") {
		t.Fatalf("away: %d %s", rec.Code, rec.Body)
	}
}

// The search takes at most msgvault's eight alternatives, each once.
func TestArchivSucheCapped(t *testing.T) {
	_, c := addressBook(t, "v|Viele|01|")
	cfg := StartConfig{Contacts: c}
	all, _, _ := c.Contacts(t.Context())
	ct := all[0]
	ct.Phones = []string{"0170 1", "+49 170 1", "0170 2", "0170 3", "0170 4", "0170 5", "0170 6", "0170 7", "0170 8", "0170 9"}
	u, _ := url.ParseQuery(archivSuche(cfg, ct))
	var explore struct{ Query string }
	json.Unmarshal([]byte(u.Get("explore")), &explore)
	if n := strings.Count(explore.Query, " | ") + 1; n != maxArchivSuche || strings.Count(explore.Query, `"+491701"`) != 1 {
		t.Fatalf("%d alternatives: %q", n, explore.Query)
	}
}

// The page shows the two as quiet buttons, each only where its backend is.
func TestKontaktZeigtSpruenge(t *testing.T) {
	cfg, _ := sprungConfig(t)
	page := oeffne(cfg, "/kontakte/oma", "anna", zuHause).Body.String()
	mustContain(t, page, `<p class=weiter-in><a href="/kontakte/oma/dokumente">Dokumente in Paperless</a></p>`,
		`<p class=weiter-in><a href="/kontakte/oma/nachrichten">Nachrichten im Archiv</a></p>`)
	cfg.PaperlessPublicURL, cfg.Archive = "", nil
	mustNotContain(t, oeffne(cfg, "/kontakte/oma", "anna", zuHause).Body.String(), "/kontakte/oma/dokumente", "/kontakte/oma/nachrichten")
}

const omaKarte = "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:oma\r\nFN:Oma Erna\r\nURL:https://erna.example/blog\r\nEND:VCARD\r\n"

// The phones' cards: the links go in as web addresses with their labels,
// and come out again of a card a phone sends back, the person's own URL kept.
func TestKontaktKarteRoundTrip(t *testing.T) {
	cfg, _ := sprungConfig(t)
	karte := KontaktKarte(cfg)
	served := karte(omaKarte, "")
	mustContain(t, served, "homes-dokumente.URL;TYPE=homes:https://homes.box.test/kontakte/oma/dokumente\r\nhomes-dokumente.X-ABLabel:Dokumente\r\n",
		"homes-nachrichten.URL;TYPE=homes:https://homes.box.test/kontakte/oma/nachrichten\r\nhomes-nachrichten.X-ABLabel:Nachrichten\r\n",
		"URL:https://erna.example/blog\r\n")
	if !strings.HasSuffix(served, "X-ABLabel:Nachrichten\r\nEND:VCARD\r\n") {
		t.Fatalf("not before the end:\n%s", served)
	}
	// the phone sends them back as plain URLs (homes-android's Vcard.kt), one folded, one on haus.<domain>
	phone := "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:oma\r\nFN:Oma Erna\r\nURL:https://homes.box.test/kontakte/oma/doku\r\n mente\r\n" +
		"URL:https://haus.box.test/kontakte/oma/nachrichten\r\nURL:https://erna.example/blog\r\nURL:https://homes.box.test/kontakte/anders/dokumente\r\nEND:VCARD\r\n"
	back := karte(phone, "oma")
	mustNotContain(t, back, "/kontakte/oma/")
	mustContain(t, back, "URL:https://erna.example/blog", "URL:https://homes.box.test/kontakte/anders/dokumente", "FN:Oma Erna")
	// served again from what was kept: once each, never doubled
	if again := karte(back, ""); strings.Count(again, "/kontakte/oma/dokumente") != 1 || strings.Count(again, "/kontakte/oma/nachrichten") != 1 {
		t.Fatalf("doubled:\n%s", again)
	}
	// none on the box, no Homes address, no UID: as it is
	for _, c := range []StartConfig{{PublicURL: "https://homes.box.test"}, {PaperlessPublicURL: "https://paperless.box.test"}} {
		if got := KontaktKarte(c)(omaKarte, ""); got != omaKarte {
			t.Errorf("%+v changed it:\n%s", c, got)
		}
	}
	if noUID := strings.Replace(omaKarte, "UID:oma\r\n", "", 1); karte(noUID, "") != noUID {
		t.Error("a card without UID changed")
	}
}

// The whole way, through napi and Radicale: the phone gets the links, an
// edit it sends back with them (as plain URLs, beside the person's own)
// stores the edit and the person's URL but not the links, and the next
// feed carries each link once.
func TestKontaktKarteThroughNapi(t *testing.T) {
	cfg, _ := sprungConfig(t)
	dav, c := addressBook(t)
	dav.Put("/homes/haushalt-kontakte/oma.vcf", omaKarte)
	h := napi.Handler(napi.Config{Validate: func(_ context.Context, b string) bool { return b == "anna1" }, Konto: c, Haushalt: haushalt.Config{Loc: berlin(t), Karte: KontaktKarte(cfg)},
		Whoami: func(string) (string, bool) { return "anna", true }})
	call := func(method, path, body string, hdr ...string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer anna1")
		for i := 0; i+1 < len(hdr); i += 2 {
			req.Header.Set(hdr[i], hdr[i+1])
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	feed := call("GET", "/napi/gemeinsam/kontakte?schreibbar=1", "").Body.String()
	mustContain(t, feed, "homes-dokumente.URL;TYPE=homes:https://homes.box.test/kontakte/oma/dokumente", "homes-nachrichten.X-ABLabel:Nachrichten")
	etag := strings.TrimSpace(strings.SplitN(strings.SplitN(feed, "X-HOMES-ETAG:", 2)[1], "\r\n", 2)[0])
	phone := "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:oma\r\nFN:Oma Erna Berg\r\nURL:https://homes.box.test/kontakte/oma/dokumente\r\n" +
		"URL:https://homes.box.test/kontakte/oma/nachrichten\r\nURL:https://erna.example/blog\r\nEND:VCARD\r\n"
	if rec := call("PUT", "/napi/gemeinsam/kontakte/oma", phone, "If-Match", etag); rec.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", rec.Code, rec.Body)
	}
	stored := dav.Objects()["/homes/haushalt-kontakte/oma.vcf"]
	mustContain(t, stored, "FN:Oma Erna Berg", "URL:https://erna.example/blog")
	mustNotContain(t, stored, "/kontakte/oma/", "X-ABLabel")
	again := call("GET", "/napi/gemeinsam/kontakte", "").Body.String()
	if strings.Count(again, "/kontakte/oma/dokumente") != 1 || strings.Count(again, "/kontakte/oma/nachrichten") != 1 || !strings.Contains(again, "URL:https://erna.example/blog") {
		t.Fatalf("the next feed:\n%s", again)
	}
}
