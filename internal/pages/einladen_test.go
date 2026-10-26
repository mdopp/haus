package pages

import (
	"bytes"
	"encoding/json"
	"html"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/mdopp/haus/internal/laib"
)

// a Laib with laib#360's two tools, or without them (an older Laib);
// it keeps what person_invite was called with
type inviteLaib struct {
	mu      sync.Mutex
	calls   []map[string]any
	reply   map[string]any // person_invite's answer
	refuse  string         // person_invite's refusal, a tool error
	pending []map[string]any
	old     bool // no person_* tools at all
	signal  bool // person_invite takes signal, person_signal is there (laib#596)
}

func (f *inviteLaib) client(t *testing.T) *laib.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string `json:"method"`
			Params struct {
				Name string         `json:"name"`
				Args map[string]any `json:"arguments"`
			} `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.Method == "tools/list" {
			props := map[string]any{"user": map[string]any{}, "email": map[string]any{}}
			tools := []map[string]any{}
			if f.signal {
				props["signal"] = map[string]any{"type": "string"}
				tools = append(tools, map[string]any{"name": "laib_person_signal", "inputSchema": map[string]any{"type": "object",
					"properties": map[string]any{"user": map[string]any{}, "number": map[string]any{}}}})
			}
			tools = append(tools, map[string]any{"name": "laib_person_invite", "inputSchema": map[string]any{"type": "object", "properties": props}})
			json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"tools": tools}})
			return
		}
		text, isErr := "[]", false
		switch req.Params.Name {
		case "laib_status":
			b, _ := json.Marshal(map[string]any{"ready": map[string]any{"state": "bereit", "operator": "läuft"}, "services": []map[string]any{{"name": "immich", "state": "grün"}}})
			text = string(b)
		case "laib_person_signal":
			f.mu.Lock()
			f.calls = append(f.calls, req.Params.Args)
			f.mu.Unlock()
			text = `{"proposal":81,"message":"Vorschlag #81: Signal-Nummer für anna."}`
		case "laib_person_invite", "laib_person_pending":
			if f.old {
				text, isErr = "unknown tool "+req.Params.Name, true
				break
			}
			if req.Params.Name == "laib_person_pending" {
				b, _ := json.Marshal(f.pending)
				text = string(b)
				break
			}
			f.mu.Lock()
			f.calls = append(f.calls, req.Params.Args)
			f.mu.Unlock()
			if f.refuse != "" {
				text, isErr = f.refuse, true
				break
			}
			b, _ := json.Marshal(f.reply)
			text = string(b)
		}
		json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"content": []map[string]string{{"text": text}}, "isError": isErr}})
	}))
	t.Cleanup(srv.Close)
	return &laib.Client{URL: srv.URL, Token: "t"}
}

func invite(t *testing.T, f *inviteLaib, form url.Values, groups string) *httptest.ResponseRecorder {
	t.Helper()
	return post(t, Einladen(BoxConfig{Laib: f.client(t), LaibURL: "https://laib.box.test"}), "/box/einladen", form, "core", map[string]string{"Remote-Groups": groups})
}

var anna = url.Values{"vorname": {"Anna"}, "nachname": {"Müller"}, "benutzer": {""}, "email": {"anna@example.org"}, "gruppe": {"family"}}

// haus#56: the Box card invites a person through Laib's verb (laib#360);
// with mail it says the one sentence, the user name is the first name's.
func TestEinladenWithMailSaysWhatHappens(t *testing.T) {
	f := &inviteLaib{reply: map[string]any{"user": "anna", "mailed": true, "expires": "24h", "message": "Anna bekommt eine Mail mit einem Link, um ihr Passwort zu setzen."}}
	rec := invite(t, f, anna, "admins")
	page := rec.Body.String()
	if rec.Code != 200 || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("%d %q\n%s", rec.Code, rec.Header().Get("Cache-Control"), page)
	}
	if len(f.calls) != 1 {
		t.Fatalf("person_invite called %d times", len(f.calls))
	}
	want := map[string]any{"user": "anna", "first": "Anna", "last": "Müller", "email": "anna@example.org", "group": "family"}
	for k, v := range want {
		if f.calls[0][k] != v {
			t.Errorf("person_invite %s = %v, want %v (%v)", k, f.calls[0][k], v, f.calls[0])
		}
	}
	if !strings.Contains(page, "--group admins") || strings.Contains(page, "name=gruppe") {
		t.Errorf("no group choice; admins are made at the console")
	}
	if _, ok := f.calls[0]["resend"]; ok {
		t.Errorf("a first invitation is no resend: %v", f.calls[0])
	}
	for _, w := range []string{"Anna bekommt eine Mail mit einem Link, um ihr Passwort zu setzen.", "Der Link gilt noch 24 Stunden", "„anna“", "Willkommen", "<h2 id=einladen>Person einladen</h2>"} {
		if !strings.Contains(page, w) {
			t.Errorf("the page lacks %q", w)
		}
	}
	if strings.Contains(page, "Link kopieren") {
		t.Errorf("with mail there is no link to copy")
	}
}

// Without mail on the box: the link to pass on, a copy button, its
// lifetime; never in the log.
func TestEinladenWithoutMailShowsTheLink(t *testing.T) {
	var logs bytes.Buffer
	log.SetOutput(&logs)
	defer log.SetOutput(os.Stderr)
	link := "https://auth.box.test/reset-password/step2?token=geheim123"
	f := &inviteLaib{reply: map[string]any{"user": "anna", "mailed": false, "link": link, "expires": "2026-10-19T12:00:00Z"}}
	page := invite(t, f, anna, "admins,family").Body.String()
	for _, w := range []string{"kein Mailversand", "Schick Anna den Link", "zum Beispiel per Signal", `value="` + link + `"`, "Link kopieren", "Der Link gilt bis 19. Oktober 2026", "Erneut senden"} {
		if !strings.Contains(page, w) {
			t.Errorf("the page lacks %q:\n%s", w, page)
		}
	}
	if strings.Contains(logs.String(), "geheim123") {
		t.Fatalf("the link is in the log: %s", logs.String())
	}
}

// Laib's refusal is said as Laib says it; nothing is invented.
func TestEinladenSaysLaibsRefusal(t *testing.T) {
	f := &inviteLaib{refuse: "Den Benutzer anna gibt es schon."}
	rec := invite(t, f, anna, "admins")
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "Nicht eingeladen: Den Benutzer anna gibt es schon.") {
		t.Fatalf("%d\n%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `value="anna@example.org"`) {
		t.Errorf("the form keeps what was typed")
	}
}

// An older Laib without the verb: the card says so plainly, no form.
func TestEinladenWithoutTheToolSaysSo(t *testing.T) {
	f := &inviteLaib{old: true}
	rec := invite(t, f, anna, "admins")
	if rec.Code != http.StatusNotImplemented || !strings.Contains(rec.Body.String(), saysInviteMissing) {
		t.Fatalf("%d\n%s", rec.Code, rec.Body.String())
	}
	box := getAt(BoxPage(BoxConfig{Laib: f.client(t), LaibURL: "https://laib.box.test"}), "/box/personen?ganz=1", "admins")
	if !strings.Contains(box, saysInviteMissing) || strings.Contains(box, "action=/box/einladen") {
		t.Fatalf("the Box card without the tool:\n%s", box)
	}
}

// Admins only, from Haus's own page only; what is not filled in is said
// and nothing goes to Laib.
func TestEinladenOnlyAdminsAndOnlyComplete(t *testing.T) {
	f := &inviteLaib{reply: map[string]any{"user": "anna", "mailed": true}}
	if rec := invite(t, f, anna, "family"); rec.Code != 403 {
		t.Errorf("a family member invites: %d", rec.Code)
	}
	h := Einladen(BoxConfig{Laib: f.client(t)})
	if rec := post(t, h, "/box/einladen", anna, "core", map[string]string{"Remote-Groups": "admins", "Sec-Fetch-Site": "cross-site"}); rec.Code != 403 {
		t.Errorf("from another site: %d", rec.Code)
	}
	for form, say := range map[string]string{
		"vorname=&email=a@b.de&gruppe=family":                  saysNeedFirst,
		"vorname=Anna&email=anna&gruppe=family":                saysBadEmail,
		"vorname=Anna&benutzer=Anna+M&email=a@b.de&gruppe=fam": saysBadUser,
	} {
		v, _ := url.ParseQuery(form)
		rec := invite(t, f, v, "admins")
		if rec.Code != 400 || !strings.Contains(rec.Body.String(), html.EscapeString(say)) {
			t.Errorf("%s: %d, want %q", form, rec.Code, say)
		}
	}
	if len(f.calls) != 0 {
		t.Fatalf("Laib was called: %v", f.calls)
	}
	req := httptest.NewRequest("GET", "/box/einladen", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 303 || rec.Header().Get("Location") != "/box/personen" {
		t.Errorf("GET: %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

// laib#362: the haus hand invites to the family only; a forged
// gruppe=admins still asks for family.
func TestEinladenInvitesToTheFamilyOnly(t *testing.T) {
	f := &inviteLaib{reply: map[string]any{"user": "anna", "mailed": true}}
	v := url.Values{"vorname": {"Anna"}, "email": {"anna@example.org"}, "gruppe": {"admins"}}
	if rec := invite(t, f, v, "admins"); rec.Code != 200 || len(f.calls) != 1 || f.calls[0]["group"] != "family" {
		t.Fatalf("%d %v", rec.Code, f.calls)
	}
}

// The card lists who has no password yet; "Erneut senden" resends with
// resend:true and the group left as it is.
func TestEinladenListsThePendingAndResends(t *testing.T) {
	f := &inviteLaib{pending: []map[string]any{{"user": "ben", "name": "Ben Beispiel", "email": "ben@example.org", "invited_at": "2026-10-17T12:00:00Z"}},
		reply: map[string]any{"user": "ben", "mailed": true, "message": "Ben bekommt die Mail noch einmal."}}
	box := getAt(BoxPage(BoxConfig{Laib: f.client(t), LaibURL: "https://laib.box.test"}), "/box/personen?ganz=1", "admins")
	for _, w := range []string{saysPendingTitle, "Ben Beispiel", "ben · ben@example.org", "eingeladen am 17. Oktober 2026", "Erneut senden", "name=erneut value=1"} {
		if !strings.Contains(box, w) {
			t.Errorf("the card lacks %q", w)
		}
	}
	rec := invite(t, f, url.Values{"erneut": {"1"}, "benutzer": {"ben"}, "email": {"ben@example.org"}, "vorname": {"Ben"}, "nachname": {"Beispiel"}}, "admins")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Ben bekommt die Mail noch einmal.") {
		t.Fatalf("%d\n%s", rec.Code, rec.Body.String())
	}
	if c := f.calls[0]; c["resend"] != true || c["user"] != "ben" || c["group"] != nil {
		t.Errorf("resend called with %v", c)
	}
}

func TestSuggestUser(t *testing.T) {
	for in, want := range map[string]string{"Anna": "anna", " Jürgen ": "juergen", "Anna-Lena": "anna", "Marie Luise": "marie", "Øyvind": "yvind", "": ""} {
		if got := suggestUser(in); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
}

func TestLifetime(t *testing.T) {
	for in, want := range map[string]string{"24h": "noch 24 Stunden", "72h": "noch 3 Tage", "1h": "noch eine Stunde", "30m": "noch 30 Minuten", "drei Tage": "drei Tage", "": ""} {
		if got := lifetime(in); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
}

// haus#87 (laib#596): the Signal number goes with the invitation — the
// field only once Laib's person_invite takes it; the number as E.164 from
// what an admin types, a wrong one said; after it, that the number waits
// for the owner's approval, with the way to the proposal.
func TestEinladenTakesTheSignalNumber(t *testing.T) {
	f := &inviteLaib{reply: map[string]any{"user": "carola", "mailed": true}}
	box := getAt(BoxPage(BoxConfig{Laib: f.client(t), LaibURL: "https://laib.box.test"}), "/box/personen?ganz=1", "admins")
	mustNotContain(t, box, "Signal-Nummer", `name=signal`)
	// a number typed at an older Laib is not passed on
	invite(t, f, url.Values{"vorname": {"Carola"}, "email": {"c@example.org"}, "signal": {"+49 170 0000001"}}, "admins")
	if _, sent := f.calls[0]["signal"]; sent {
		t.Fatalf("signal sent to a Laib that does not take it: %v", f.calls[0])
	}

	f = &inviteLaib{signal: true, reply: map[string]any{"user": "carola", "mailed": true, "proposal": 77}}
	box = getAt(BoxPage(BoxConfig{Laib: f.client(t), LaibURL: "https://laib.box.test"}), "/box/personen?ganz=1", "admins")
	mustContain(t, box, `<label for=signal>Signal-Nummer</label><input id=signal name=signal type=tel`, saysSignalHint)
	rec := invite(t, f, url.Values{"vorname": {"Carola"}, "email": {"c@example.org"}, "signal": {"0049 (170) 000-0001"}}, "admins")
	if rec.Code != 200 || len(f.calls) != 1 || f.calls[0]["signal"] != "+491700000001" {
		t.Fatalf("%d %v", rec.Code, f.calls)
	}
	mustContain(t, rec.Body.String(), saysSignalWaits, `<a href="https://laib.box.test/inbox">Zur Inbox (Vorschlag #77)</a>`)

	// none given: none sent, nothing said about it
	rec = invite(t, f, url.Values{"vorname": {"Ben"}, "email": {"b@example.org"}}, "admins")
	if _, sent := f.calls[1]["signal"]; sent {
		t.Errorf("empty signal sent: %v", f.calls[1])
	}
	mustNotContain(t, rec.Body.String(), saysSignalWaits)

	// a wrong one: said, nothing sent, the form keeps it
	for _, bad := range []string{"0170 0000001", "+0 123", "+49abc", "+4917000000011111111"} {
		rec = invite(t, f, url.Values{"vorname": {"Dora"}, "email": {"d@example.org"}, "signal": {bad}}, "admins")
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), saysBadSignal) {
			t.Errorf("%q → %d", bad, rec.Code)
		}
	}
	if len(f.calls) != 2 {
		t.Fatalf("a wrong number reached Laib: %v", f.calls[2:])
	}
}

// haus#87 (laib#596): someone who already has an account gets the number
// later, on the same card: Benutzername and Nummer, one person_signal
// proposal, said as waiting for the owner with the way to it; only once
// Laib has the verb; a wrong name or number reaches nothing.
func TestEinladenSetsTheSignalNumberLater(t *testing.T) {
	old := &inviteLaib{}
	mustNotContain(t, getAt(BoxPage(BoxConfig{Laib: old.client(t), LaibURL: "https://laib.box.test"}), "/box/personen?ganz=1", "admins"), "Signal-Nummer nachtragen")

	f := &inviteLaib{signal: true}
	mustContain(t, getAt(BoxPage(BoxConfig{Laib: f.client(t), LaibURL: "https://laib.box.test"}), "/box/personen?ganz=1", "admins"),
		"<h3 id=signal-nachtragen>Signal-Nummer nachtragen</h3>", `<input type=hidden name=tun value=signal>`, `name=signal type=tel required`)
	rec := invite(t, f, url.Values{"tun": {"signal"}, "benutzer": {"anna"}, "signal": {"0170 0000001"}}, "admins")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), saysBadSignal) {
		t.Fatalf("no country code → %d", rec.Code)
	}
	rec = invite(t, f, url.Values{"tun": {"signal"}, "benutzer": {"Anna M"}, "signal": {"+491700000001"}}, "admins")
	if rec.Code != http.StatusBadRequest || len(f.calls) != 0 {
		t.Fatalf("bad user → %d %v", rec.Code, f.calls)
	}
	rec = invite(t, f, url.Values{"tun": {"signal"}, "benutzer": {"anna"}, "signal": {"+49 170 0000001"}}, "admins")
	if rec.Code != 200 || len(f.calls) != 1 || f.calls[0]["user"] != "anna" || f.calls[0]["number"] != "+491700000001" {
		t.Fatalf("%d %v", rec.Code, f.calls)
	}
	mustContain(t, rec.Body.String(), "Signal-Nummer für anna eingetragen: Sie gilt, sobald der Vorschlag freigegeben ist.",
		`<a href="https://laib.box.test/inbox">Zur Inbox (Vorschlag #81)</a>`)
	if rec := invite(t, f, url.Values{"tun": {"signal"}, "benutzer": {"anna"}, "signal": {"+491700000001"}}, "family"); rec.Code != http.StatusForbidden {
		t.Errorf("family → %d", rec.Code)
	}
}
