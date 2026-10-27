package pages

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/mdopp/haus/internal/laib"
)

// a Laib with laib#561's tool: show answers the block, set files a
// proposal and changes nothing (a human approves it on Laib's page)
type zuhauseLaib struct {
	mu     sync.Mutex
	calls  []map[string]any
	block  map[string]any
	refuse string // laib's error text for set
	old    bool   // no laib_zuhause at all
	denied bool   // policy.yml without zuhause for the haus hand
}

func (f *zuhauseLaib) client(t *testing.T) *laib.Client {
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
		case req.Params.Name != "laib_zuhause" || f.old:
			text, isErr = "unknown tool "+req.Params.Name, true
		case f.denied:
			f.calls = append(f.calls, a)
			text, isErr = "haus may not zuhause", true
		case a["action"] == "show":
			f.calls = append(f.calls, a)
			b, _ := json.Marshal(f.block)
			text = string(b)
		case f.refuse != "":
			f.calls = append(f.calls, a)
			text, isErr = f.refuse, true
		default:
			f.calls = append(f.calls, a)
			text = `{"proposal":7,"message":"Vorschlag #7: die neue Adresse gilt nach der Freigabe durch einen Menschen."}`
		}
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{"content": []map[string]string{{"type": "text", "text": text}}, "isError": isErr}})
	}))
	t.Cleanup(srv.Close)
	return &laib.Client{URL: srv.URL, Token: "t"}
}

func adresseBox(t *testing.T) (StartConfig, *zuhauseLaib) {
	t.Helper()
	l := &zuhauseLaib{block: map[string]any{"street": "Musterweg 12", "postcode": "12345", "city": "Musterstadt", "country": "DE",
		"latitude": 52.1234, "longitude": 8.0456, "elevation": 63}}
	return StartConfig{Laib: l.client(t), LaibURL: "https://laib.box.test"}, l
}

// laib#561: the admins read the household's address as Laib has it, and a
// form to change it, already filled in; what it is for in one line; and
// two ways to the coordinates, neither of which sends anything by itself.
func TestAdresseShowsTheAddressAndTheForm(t *testing.T) {
	cfg, l := adresseBox(t)
	code, body, _ := familyPage(t, Adresse, cfg, "GET", adressePath, "family,admins")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	mustContain(t, body, "<h1>Adresse</h1>", "Musterweg 12", "12345 Musterstadt", "52.1234", "8.0456", "63 m",
		"Home Assistant", "Solar", "Müll",
		`name=street`, `value="Musterweg 12"`, `name=postcode`, `value="12345"`, `name=city`, `value="Musterstadt"`,
		`name=country`, `value="DE"`, `name=latitude`, `value="52.1234"`, `name=longitude`, `value="8.0456"`, `name=elevation`, `value="63"`,
		"action="+adresseSet, "Vorschlagen", "Position aus diesem Gerät übernehmen", "navigator.geolocation",
		"https://www.openstreetmap.org/search", "target=_blank", "OpenStreetMap")
	if len(l.calls) != 1 || !reflect.DeepEqual(l.calls[0], map[string]any{"action": "show"}) {
		t.Fatalf("laib got %v", l.calls)
	}
	// owner 2026-10-27: Adresse is set once, at setup: under Box, beside its pages
	mustContain(t, body, `<nav class=unter aria-label=Box>`, "<a href="+adressePath+" aria-current=page>Adresse</a>")

	// nothing set yet: the form with Deutschland, nothing pretended
	l.block = map[string]any{}
	_, body, _ = familyPage(t, Adresse, cfg, "GET", adressePath, "admins")
	mustContain(t, body, "Noch keine Adresse eingetragen", `name=country`, `value="DE"`)
	mustNotContain(t, body, "Musterweg")
}

// Vorschlagen sends exactly the fields that are filled in (the whole block:
// what is left out is removed), says what happens next and where to approve,
// and the address is in neither the redirect nor the log.
func TestAdresseVorschlagenSendsExactlyTheFields(t *testing.T) {
	cfg, l := adresseBox(t)
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	form := url.Values{"street": {" Musterweg  12 "}, "postcode": {"12345"}, "city": {"Musterstadt"}, "country": {"de"},
		"latitude": {"52,1234"}, "longitude": {"8.0456"}, "elevation": {""}}
	rec := post(t, AdresseVorschlagen(cfg), adresseSet, form, "anna", map[string]string{"Remote-Groups": "admins"})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("%d %v", rec.Code, rec.Header())
	}
	want := map[string]any{"action": "set", "street": "Musterweg 12", "postcode": "12345", "city": "Musterstadt", "country": "DE",
		"latitude": "52.1234", "longitude": "8.0456"}
	if len(l.calls) != 1 || !reflect.DeepEqual(l.calls[0], want) {
		t.Fatalf("laib got %v, want %v", l.calls, want)
	}
	loc := rec.Header().Get("Location")
	if loc != adressePath+"?gesagt=vorgeschlagen&vorschlag=7" {
		t.Fatalf("redirect %q", loc)
	}
	logged := regexp.MustCompile(`(?m)^\d{4}/\d\d/\d\d \d\d:\d\d:\d\d `).ReplaceAllString(logs.String(), "") // the log's own time may hold a "52"
	for _, v := range []string{"Muster", "12345", "52.12", "8.0456"} {
		if strings.Contains(strings.TrimPrefix(loc, adressePath+"?gesagt=vorgeschlagen&vorschlag=7"), v) || strings.Contains(logged, v) {
			t.Fatalf("%q in the redirect %q or the log %q", v, loc, logs.String())
		}
	}
	_, body, _ := familyPage(t, Adresse, cfg, "GET", loc, "admins")
	mustContain(t, body, "Vorgeschlagen. Die Änderung gilt, sobald du sie in der Inbox freigibst.", `href="https://laib.box.test/inbox"`, "#7")
	// a street only: the rest is not sent, so Laib removes it
	l.calls = nil
	post(t, AdresseVorschlagen(cfg), adresseSet, url.Values{"street": {"Musterweg 12"}, "country": {""}}, "anna", map[string]string{"Remote-Groups": "admins"})
	if len(l.calls) != 1 || !reflect.DeepEqual(l.calls[0], map[string]any{"action": "set", "street": "Musterweg 12"}) {
		t.Fatalf("street only: laib got %v", l.calls)
	}
}

// laib refuses values in its own words, which carry them: the page says
// what to check, in a fixed sentence, and neither URL nor log has a value.
func TestAdresseLaibsRefusalOfAValue(t *testing.T) {
	cfg, l := adresseBox(t)
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	l.refuse = `box.yml zuhause: postcode "1!": letters, digits, blanks and dashes, 2 to 10`
	rec := post(t, AdresseVorschlagen(cfg), adresseSet, url.Values{"street": {"Musterweg 12"}, "postcode": {"1!"}}, "anna", map[string]string{"Remote-Groups": "admins"})
	if loc := rec.Header().Get("Location"); loc != adressePath+"?gesagt=plz" {
		t.Fatalf("redirect %q", loc)
	}
	if strings.Contains(logs.String(), "1!") || strings.Contains(logs.String(), "Musterweg") {
		t.Fatalf("log: %s", logs.String())
	}
	_, body, _ := familyPage(t, Adresse, cfg, "GET", adressePath+"?gesagt=plz", "admins")
	mustContain(t, body, "Postleitzahl")
	// one coordinate alone does not go to laib
	l.calls = nil
	rec = post(t, AdresseVorschlagen(cfg), adresseSet, url.Values{"latitude": {"52.1"}}, "anna", map[string]string{"Remote-Groups": "admins"})
	if loc := rec.Header().Get("Location"); loc != adressePath+"?gesagt=koordinaten" || len(l.calls) != 0 {
		t.Fatalf("one coordinate: %q, %d calls", loc, len(l.calls))
	}
}

// An older Laib without the tool, or a policy.yml without zuhause for the
// haus hand: the page says plainly that Haus cannot change the address
// here, offers no form, and a POST says the same.
func TestAdresseWhereLaibRefusesTheTool(t *testing.T) {
	for _, how := range []string{"old", "denied"} {
		cfg, l := adresseBox(t)
		l.old, l.denied = how == "old", how == "denied"
		code, body, _ := familyPage(t, Adresse, cfg, "GET", adressePath, "admins")
		if code != 200 {
			t.Fatalf("%s: status %d", how, code)
		}
		mustContain(t, body, "Auf dieser Box kann Haus die Adresse noch nicht ändern")
		mustNotContain(t, body, "action="+adresseSet, "unknown tool", "may not")
		rec := post(t, AdresseVorschlagen(cfg), adresseSet, url.Values{"street": {"Musterweg 12"}}, "anna", map[string]string{"Remote-Groups": "admins"})
		if loc := rec.Header().Get("Location"); loc != adressePath+"?gesagt=kein-laib" {
			t.Fatalf("%s: redirect %q", how, loc)
		}
	}
}

// Only admins: the family gets no page, no Mehr entry, and its POST never
// reaches Laib; nor does another site's form.
func TestAdresseIsForAdminsOnly(t *testing.T) {
	cfg, l := adresseBox(t)
	code, body, hdr := familyPage(t, Adresse, cfg, "GET", adressePath, "family")
	if code != http.StatusSeeOther || hdr.Get("Location") != "/start" || strings.Contains(body, "Musterweg") {
		t.Fatalf("family: %d %v", code, hdr)
	}
	for _, g := range []string{"family", "admins"} { // not on Mehr: under Box
		_, body, _ = familyPage(t, Mehr, cfg, "GET", "/mehr", g)
		mustNotContain(t, body, "href="+adressePath)
	}
	rec := post(t, AdresseVorschlagen(cfg), adresseSet, url.Values{"street": {"Musterweg 12"}}, "anna", map[string]string{"Remote-Groups": "family"})
	if rec.Code == http.StatusSeeOther && rec.Header().Get("Location") != "/start" {
		t.Fatalf("family POST: %d %v", rec.Code, rec.Header())
	}
	rec = post(t, AdresseVorschlagen(cfg), adresseSet, url.Values{"street": {"Musterweg 12"}}, "anna", map[string]string{"Remote-Groups": "admins", "Sec-Fetch-Site": "cross-site"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-site: %d", rec.Code)
	}
	if len(l.calls) != 0 {
		t.Fatalf("laib got %v", l.calls)
	}
}
