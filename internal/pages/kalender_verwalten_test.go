package pages

import (
	"bytes"
	"encoding/json"
	"github.com/mdopp/haus/internal/papierkorb"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mdopp/haus/internal/ansicht"
	"github.com/mdopp/haus/internal/caldav"
	"github.com/mdopp/haus/internal/caldav/davfake"
	"github.com/mdopp/haus/internal/laib"
)

const secretAddress = "https://calendar.google.com/calendar/ical/anna%40gmail.com/private-0123456789abcdef/basic.ics"

// a Laib with laib#495's tool: it keeps the subscriptions (and the
// addresses, as secrets.age would) and answers without them
type calLaib struct {
	mu     sync.Mutex
	calls  []map[string]any
	abos   []map[string]any
	refuse string
	old    bool
}

func (f *calLaib) client(t *testing.T) *laib.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Params struct {
				Name string         `json:"name"`
				Args map[string]any `json:"arguments"`
			} `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		f.mu.Lock()
		defer f.mu.Unlock()
		text, isErr := "", false
		a := req.Params.Args
		switch {
		case req.Params.Name != "laib_calendar" || f.old:
			text, isErr = "unknown tool "+req.Params.Name, true
		case f.refuse != "":
			f.calls = append(f.calls, a)
			text, isErr = "laib: "+f.refuse, true
		default:
			f.calls = append(f.calls, a)
			switch a["action"] {
			case "subscribe":
				f.abos = append(f.abos, map[string]any{"person": a["person"], "name": a["name"], "kind": a["kind"], "title": a["title"], "since": "2026-10-14T08:00:00Z"})
			case "unsubscribe":
				var keep []map[string]any
				for _, x := range f.abos {
					if x["person"] != a["person"] || x["name"] != a["name"] {
						keep = append(keep, x)
					}
				}
				f.abos = keep
			}
			b, _ := json.Marshal(map[string]any{"message": "ok", "abos": f.abos})
			text = string(b)
		}
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{"content": []map[string]string{{"type": "text", "text": text}}, "isError": isErr}})
	}))
	t.Cleanup(srv.Close)
	return &laib.Client{URL: srv.URL, Token: "t"}
}

func kalenderBox(t *testing.T) (*davfake.Server, StartConfig, *calLaib) {
	t.Helper()
	fake := davfake.New("haus", "pw")
	t.Cleanup(fake.Close)
	fake.AddCalendar("/haus/haushalt/", "Haushalt")
	fake.AddPrincipal("anna")
	fake.AddCalendar("/anna/google-familie/", "Familie")
	fake.SetSynced("/anna/google-familie/", "2026-10-14T07:55:00Z", "")
	fake.AddCalendar("/anna/abo-muell/", "Müllabfuhr")
	fake.SetSynced("/anna/abo-muell/", "2026-10-14T05:00:00Z", "Kalender „Müllabfuhr“: seit 3 h nicht abgeglichen")
	l := &calLaib{abos: []map[string]any{{"person": "anna", "name": "familie", "kind": "google", "title": "Familie", "since": "2026-10-12T08:00:00Z"},
		{"person": "anna", "name": "muell", "kind": "abo", "title": "Müllabfuhr", "since": "2026-10-01T08:00:00Z"}}}
	cfg := StartConfig{Calendar: &caldav.Client{URL: fake.URL, User: "haus", Password: "pw", Loc: berlin(t)}, Laib: l.client(t),
		RadicalePublicURL: "https://caldav.box.test", Views: ansicht.Open(t.TempDir())}
	return fake, cfg, l
}

// haus#66: Deine Kalender lists each calendar with where it comes from and
// when it was last mirrored, what went wrong, the phone's address, and the
// three ways to add one; nothing shows an address laib keeps.
func TestDeineKalenderListsWhereEachComesFrom(t *testing.T) {
	_, cfg, _ := kalenderBox(t)
	code, body, _ := familyPage(t, KalenderVerwalten, cfg, "GET", kalenderVerwalten, "family")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	mustContain(t, body, "<h1>Deine Kalender</h1>", "Haushalt", "der Haushalt, für alle",
		"Familie", "aus Google, nur lesen · verbunden seit Mo 12.10. · abgeglichen alle 15 Minuten · zuletzt abgeglichen vor 5 min",
		"Müllabfuhr", "abonniert, nur lesen", "seit 3 h nicht abgeglichen",
		`value="https://caldav.box.test/anna/google-familie/"`, "DAVx⁵", "Anmeldung wie bei Haus",
		`href="https://caldav.box.test/.web/"`, "In Radicale öffnen", "Adressbuch fürs Telefon", `value="https://caldav.box.test/haus/haushalt-kontakte/"`,
		"Google-Kalender verbinden", "Geheime Adresse im iCal-Format", "Kalender integrieren",
		"Öffentlichen Kalender abonnieren", "Feiertage", "Schulferien", "Niedersachsen", "Neuen Kalender anlegen", "nur für mich",
		"Trennen…")
	mustNotContain(t, body, "private-", "<script>alert")
	// the Haushalt is not Haus's to delete, a mirror not to delete but to Trennen
	before(t, body, "Haushalt", "Familie")
	if strings.Count(body, "Löschen…") != 0 {
		t.Fatal("a calendar Haus did not make offers Löschen")
	}
}

func TestDeineKalenderWithoutLaibsToolOffersOnlyANewCalendar(t *testing.T) {
	_, cfg, l := kalenderBox(t)
	l.old = true
	_, body, _ := familyPage(t, KalenderVerwalten, cfg, "GET", kalenderVerwalten, "family")
	mustContain(t, body, saysNoLaibCal, "Neuen Kalender anlegen")
	mustNotContain(t, body, "Google-Kalender verbinden")
}

// The secret address goes to laib once, as the tool's stdin, and is in no
// argument, redirect or log line.
func TestGoogleVerbindenHandsTheAddressToLaibOnce(t *testing.T) {
	_, cfg, l := kalenderBox(t)
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	rec := post(t, KalenderHinzufuegen(cfg), kalenderAdd, url.Values{"weg": {"google"}, "name": {"Arbeit Büro"}, "adresse": {secretAddress}}, "anna", nil)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != kalenderVerwalten+"?gesagt=verbunden" {
		t.Fatalf("%d %v", rec.Code, rec.Header())
	}
	c := l.calls[len(l.calls)-1]
	if c["action"] != "subscribe" || c["person"] != "anna" || c["name"] != "arbeit-buero" || c["kind"] != "google" || c["title"] != "Arbeit Büro" || c["stdin"] != secretAddress+"\n" {
		t.Fatalf("laib got %v", c)
	}
	for k, v := range c {
		if k != "stdin" && strings.Contains(v.(string), "private-") {
			t.Fatalf("the address in argument %s", k)
		}
	}
	if strings.Contains(logs.String(), "private-") || !strings.Contains(logs.String(), "verbindet google anna/arbeit-buero") {
		t.Fatalf("log: %s", logs.String())
	}
	// laib's refusal is said in Haus's words, the address in neither
	l.refuse = "Das ist nicht Googles „Geheime Adresse im iCal-Format“ (https://calendar.google.com/…/basic.ics)."
	rec = post(t, KalenderHinzufuegen(cfg), kalenderAdd, url.Values{"weg": {"google"}, "name": {"X"}, "adresse": {"https://example.org/x.ics"}}, "anna", nil)
	if rec.Header().Get("Location") != kalenderVerwalten+"?gesagt=keine-google" {
		t.Fatalf("%v", rec.Header())
	}
	_, body, _ := familyPage(t, KalenderVerwalten, cfg, "GET", kalenderVerwalten+"?gesagt=keine-google", "family")
	mustContain(t, body, "Das ist nicht Googles")
	// another site's form never reaches laib
	n := len(l.calls)
	rec = post(t, KalenderHinzufuegen(cfg), kalenderAdd, url.Values{"weg": {"google"}, "name": {"X"}, "adresse": {secretAddress}}, "anna", map[string]string{"Sec-Fetch-Site": "cross-site"})
	if rec.Code != http.StatusForbidden || len(l.calls) != n {
		t.Fatalf("cross-site: %d, %d calls", rec.Code, len(l.calls)-n)
	}
}

func TestFeiertageForTheFamilyFromTheList(t *testing.T) {
	_, cfg, l := kalenderBox(t)
	rec := post(t, KalenderHinzufuegen(cfg), kalenderAdd, url.Values{"weg": {"abo"}, "vorlage": {"feiertage"}, "land": {"BY"}, "fuer": {"familie"}}, "anna", nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("%d", rec.Code)
	}
	c := l.calls[len(l.calls)-1]
	if c["person"] != "haus" || c["kind"] != "abo" || c["name"] != "feiertage-bayern" || c["title"] != "Feiertage Bayern" ||
		c["stdin"] != "https://openholidaysapi.org/PublicHolidays?countryIsoCode=DE&subdivisionCode=DE-BY&languageIsoCode=DE\n" {
		t.Fatalf("laib got %v", c)
	}
	post(t, KalenderHinzufuegen(cfg), kalenderAdd, url.Values{"weg": {"abo"}, "vorlage": {"eigene"}, "name": {"Müll"}, "adresse": {"webcal://gemeinde.example/muell.ics"}, "fuer": {"ich"}}, "anna", nil)
	if c := l.calls[len(l.calls)-1]; c["person"] != "anna" || c["name"] != "muell" || c["stdin"] != "webcal://gemeinde.example/muell.ics\n" {
		t.Fatalf("own address: %v", c)
	}
}

func TestNeuerKalenderForTheFamilyOrForOneself(t *testing.T) {
	fake, cfg, _ := kalenderBox(t)
	for fuer, want := range map[string]string{"familie": "/haus/familie-ausfluege/", "ich": "/anna/privat-ausfluege/"} {
		rec := post(t, KalenderHinzufuegen(cfg), kalenderAdd, url.Values{"weg": {"neu"}, "name": {"Ausflüge"}, "fuer": {fuer}}, "anna", nil)
		if rec.Header().Get("Location") != kalenderVerwalten+"?gesagt=angelegt" {
			t.Fatalf("%s: %v", fuer, rec.Header())
		}
		if fake.DisplayName(want) != "Ausflüge" {
			t.Fatalf("%s: not made at %s: %v", fuer, want, fake.Calendars())
		}
	}
	rec := post(t, KalenderHinzufuegen(cfg), kalenderAdd, url.Values{"weg": {"neu"}, "name": {"Ausflüge"}, "fuer": {"ich"}}, "anna", nil)
	if rec.Header().Get("Location") != kalenderVerwalten+"?gesagt=gibt-es-schon" {
		t.Fatalf("twice: %v", rec.Header())
	}
	_, body, _ := familyPage(t, KalenderVerwalten, cfg, "GET", kalenderVerwalten, "family")
	mustContain(t, body, "nur für dich", "Löschen…")
}

// Colour and Ausblenden are the person's own view: Kalender leaves the
// calendar out for them, colours the rest, and names where each comes from.
func TestFarbeUndAusblendenChangeOnlyThePersonsKalender(t *testing.T) {
	fake, cfg, _ := kalenderBox(t)
	fake.Put("/anna/google-familie/1.ics", event("1", "20261014T110000Z", "20261014T120000Z", "Zahnarzt"))
	fake.Put("/anna/abo-muell/2.ics", event("2", "20261014T120000Z", "20261014T130000Z", "Gelbe Tonne"))
	if rec := post(t, KalenderAendern(cfg), kalenderChange, url.Values{"kalender": {"/anna/google-familie/"}, "tu": {"farbe"}, "farbe": {"#c92a2a"}}, "anna", nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("%d", rec.Code)
	}
	post(t, KalenderAendern(cfg), kalenderChange, url.Values{"kalender": {"/anna/abo-muell/"}, "tu": {"aus"}}, "anna", nil)
	_, body, _ := familyPage(t, Kalender, cfg, "GET", "/kalender", "family")
	mustContain(t, body, "Zahnarzt", `background:#c92a2a`, "13:00–14:00 · Familie", "Kalender hinzufügen und verwalten")
	mustNotContain(t, body, "Gelbe Tonne")
	_, body, _ = familyPageAs(t, Kalender, cfg, "GET", "/kalender", "ben", "family")
	mustContain(t, body, "Gelbe Tonne")
	mustNotContain(t, body, `background:#c92a2a`)
	// someone else's calendar, a path that is none: refused
	for _, p := range []string{"/ben/privat-x/", "/anna/../ben/x/", "/anna/"} {
		if rec := post(t, KalenderAendern(cfg), kalenderChange, url.Values{"kalender": {p}, "tu": {"aus"}}, "anna", nil); rec.Code != http.StatusForbidden {
			t.Fatalf("%s: %d", p, rec.Code)
		}
	}
}

func TestTrennenAsksLaibAndLoeschenOnlyWhatHausMade(t *testing.T) {
	fake, cfg, l := kalenderBox(t)
	rec := post(t, KalenderAendern(cfg), kalenderChange, url.Values{"kalender": {"/anna/google-familie/"}, "tu": {"trennen"}}, "anna", nil)
	if rec.Header().Get("Location") != kalenderVerwalten+"?gesagt=getrennt" {
		t.Fatalf("%v", rec.Header())
	}
	if c := l.calls[len(l.calls)-1]; c["action"] != "unsubscribe" || c["person"] != "anna" || c["name"] != "familie" {
		t.Fatalf("laib got %v", c)
	}
	rec = post(t, KalenderAendern(cfg), kalenderChange, url.Values{"kalender": {"/anna/abo-muell/"}, "tu": {"loeschen"}}, "anna", nil)
	if rec.Header().Get("Location") != kalenderVerwalten+"?gesagt=nicht-moeglich" || fake.DisplayName("/anna/abo-muell/") == "" {
		t.Fatalf("a mirror deleted: %v", rec.Header())
	}
	fake.AddCalendar("/haus/familie-x/", "X")
	post(t, KalenderAendern(cfg), kalenderChange, url.Values{"kalender": {"/haus/familie-x/"}, "tu": {"loeschen"}}, "anna", nil)
	if fake.DisplayName("/haus/familie-x/") != "" {
		t.Fatal("not deleted")
	}
}

// haus#96 (laib#619): how often each calendar is mirrored — chosen when it
// is added (Google suggests every run, a public calendar monthly), said in
// its row, changed there without the address; a value not offered never
// reaches laib.
func TestKalenderAbstandIsChosenAndChanged(t *testing.T) {
	_, cfg, l := kalenderBox(t)
	_, body, _ := familyPage(t, KalenderVerwalten, cfg, "GET", kalenderVerwalten, "family")
	mustContain(t, body, `<option value="15m" selected>alle 15 Minuten</option>`, `<option value="30d" selected>monatlich</option>`,
		"Müllabfuhr</strong> <span class=k>abonniert, nur lesen · verbunden seit", "· abgeglichen monatlich",
		`<button name=tu value=abstand data-laeuft="Speichert …">Abstand ändern</button>`)

	post(t, KalenderHinzufuegen(cfg), kalenderAdd, url.Values{"weg": {"abo"}, "vorlage": {"feiertage"}, "land": {"NI"}, "abstand": {"7d"}}, "anna", nil)
	if c := l.calls[len(l.calls)-1]; c["every"] != "7d" {
		t.Fatalf("subscribe %v", c)
	}
	post(t, KalenderHinzufuegen(cfg), kalenderAdd, url.Values{"weg": {"abo"}, "vorlage": {"schulferien"}, "land": {"NI"}, "abstand": {"5s"}}, "anna", nil)
	if _, set := l.calls[len(l.calls)-1]["every"]; set {
		t.Fatalf("an interval not offered went to laib: %v", l.calls[len(l.calls)-1])
	}

	rec := post(t, KalenderAendern(cfg), kalenderChange, url.Values{"kalender": {"/anna/abo-muell/"}, "tu": {"abstand"}, "abstand": {"1d"}}, "anna", nil)
	if rec.Header().Get("Location") != kalenderVerwalten+"?gesagt=abstand" {
		t.Fatalf("%d %v", rec.Code, rec.Header())
	}
	if c := l.calls[len(l.calls)-1]; c["action"] != "every" || c["person"] != "anna" || c["name"] != "muell" || c["every"] != "1d" || c["stdin"] != nil {
		t.Fatalf("laib got %v", c)
	}
	n := len(l.calls)
	for _, bad := range []url.Values{
		{"kalender": {"/anna/abo-muell/"}, "tu": {"abstand"}, "abstand": {"1s"}},
		{"kalender": {"/haus/haushalt/"}, "tu": {"abstand"}, "abstand": {"1d"}},
	} {
		if rec := post(t, KalenderAendern(cfg), kalenderChange, bad, "anna", nil); rec.Code != http.StatusBadRequest {
			t.Errorf("%v → %d", bad, rec.Code)
		}
	}
	if len(l.calls) != n {
		t.Fatalf("refused changes reached laib: %v", l.calls[n:])
	}
}

// haus#174: a deleted calendar with its appointments waits 30 days; „Rückgängig“
// makes it again with all of them; another person's own is not theirs to take back
func TestKalenderRueckgaengig(t *testing.T) {
	fake, cfg, _ := kalenderBox(t)
	now := time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)
	cfg.Papierkorb = &papierkorb.Korb{Dir: t.TempDir(), Now: func() time.Time { return now }}
	fake.AddCalendar("/haus/familie-urlaub/", "Urlaub")
	fake.Put("/haus/familie-urlaub/a.ics", event("a", "20261101T090000Z", "20261101T100000Z", "Flug"))
	fake.Put("/haus/familie-urlaub/b.ics", event("b", "20261108T090000Z", "20261108T100000Z", "Rückflug"))

	rec := post(t, KalenderAendern(cfg), kalenderChange, url.Values{"kalender": {"/haus/familie-urlaub/"}, "name": {"Urlaub"}, "tu": {"loeschen"}}, "anna", nil)
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, kalenderVerwalten+"?gesagt=geloescht&korb=") || fake.DisplayName("/haus/familie-urlaub/") != "" {
		t.Fatalf("loeschen → %q", loc)
	}
	_, body, _ := familyPage(t, KalenderVerwalten, cfg, "GET", loc, "family")
	mustContain(t, body, "Der Kalender ist gelöscht.", "„Urlaub“ <small class=k>2 Termine · bis 27.11.", `value=zurueckholen data-laeuft="Holt zurück …">Rückgängig</button>`,
		"Gelöscht, noch 30 Tage zurückzuholen")
	id := loc[strings.Index(loc, "korb=")+5:]

	if rec := post(t, KalenderAendern(cfg), kalenderChange, url.Values{"kalender": {"/haus/familie-urlaub/"}, "name": {"Urlaub"}, "tu": {"zurueckholen"}, "id": {id}}, "ben", nil); rec.Header().Get("Location") != kalenderVerwalten+"?gesagt=zurueck" {
		t.Fatalf("zurueckholen → %d %v %s", rec.Code, rec.Header(), rec.Body)
	}
	if fake.DisplayName("/haus/familie-urlaub/") != "Urlaub" {
		t.Fatal("not made again")
	}
	back := 0
	for p := range fake.Objects() {
		if strings.HasPrefix(p, "/haus/familie-urlaub/") {
			back++
		}
	}
	if back != 2 {
		t.Fatalf("appointments back: %d of 2", back)
	}
	if rec := post(t, KalenderAendern(cfg), kalenderChange, url.Values{"kalender": {"/haus/familie-urlaub/"}, "name": {"Urlaub"}, "tu": {"zurueckholen"}, "id": {id}}, "anna", nil); rec.Code != http.StatusConflict {
		t.Errorf("twice → %d", rec.Code)
	}
}
