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
}

func (f *inviteLaib) client(t *testing.T) *laib.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Params struct {
				Name string         `json:"name"`
				Args map[string]any `json:"arguments"`
			} `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		text, isErr := "[]", false
		switch req.Params.Name {
		case "laib_status":
			b, _ := json.Marshal(map[string]any{"ready": map[string]any{"state": "bereit", "operator": "läuft"}, "services": []map[string]any{{"name": "immich", "state": "grün"}}})
			text = string(b)
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
	box := get(BoxPage(BoxConfig{Laib: f.client(t), LaibURL: "https://laib.box.test"}), "admins")
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
	if rec.Code != 303 || rec.Header().Get("Location") != "/box" {
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
	box := get(BoxPage(BoxConfig{Laib: f.client(t), LaibURL: "https://laib.box.test"}), "admins")
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
