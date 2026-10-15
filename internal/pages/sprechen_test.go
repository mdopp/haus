package pages

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/haus/internal/bote"
	"github.com/mdopp/haus/internal/bote/botefake"
	"github.com/mdopp/haus/internal/persona"
)

func fakeBote(t *testing.T) (*botefake.Server, *bote.Client) {
	t.Helper()
	fake := botefake.New("k-bote-0123456789abcdef")
	t.Cleanup(fake.Close)
	return fake, &bote.Client{URL: fake.Base(), Key: fake.Key}
}

// talkPage is /sprechen as user sees it, from the one handler h, which
// holds the last exchange.
func talkPage(t *testing.T, h http.HandlerFunc, user string) (int, string, http.Header) {
	t.Helper()
	req := httptest.NewRequest("GET", "/sprechen", nil)
	if user != "" {
		req.Header.Set("Remote-User", user)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String(), rec.Header()
}

// say posts the Sprechen field as a browser on Haus's own page does.
func say(t *testing.T, h http.HandlerFunc, user, text string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	return post(t, h, "/sprechen", url.Values{"nachricht": {text}}, user, hdr)
}

// haus#12: Start has the Sprechen field at the top, under the day and
// above the cards: one plain field and Senden, posting to Haus itself, no
// script. It names no persona: the persona speaks where you talk to it
// (haus#35). Without the Bote there is no field and no word about it.
func TestStartHasTheSprechenField(t *testing.T) {
	House = persona.Persona{Display: "Joshua", Greeting: map[string]string{"de": "Wollen wir ein Spiel spielen?"}}
	defer func() { House = persona.Default() }()
	fake, hc := house(t)
	fake.Set("light.flur", "on", map[string]any{"friendly_name": "Flurlicht"})
	_, bc := fakeBote(t)
	_, body := startPage(t, StartConfig{HA: hc, Bote: bc}, "family")
	mustContain(t, body, "<form method=post action=/sprechen class=sprechen>", ">Sprechen</label>", "name=nachricht", "maxlength=1000",
		"required", "<button>Senden</button>")
	before(t, body, "class=datum", "action=/sprechen")
	before(t, body, "action=/sprechen", "karte-im-haus")
	mustNotContain(t, body, "<script", "Joshua", "Wollen wir", "Bote", "Hermes", "8642")

	_, body = startPage(t, StartConfig{HA: hc}, "family")
	mustNotContain(t, body, "/sprechen", "Sprechen", "Bote")
}

// haus#12: the field is on every family page through the tab bar —
// Sprechen, between Notizen and Box, marked on its own page — and only
// with the Bote.
func TestSprechenIsATab(t *testing.T) {
	_, bc := fakeBote(t)
	nd := notesDir(t, map[string]time.Duration{"Einkaufsliste": time.Hour})
	cfg := StartConfig{Notes: nd, Bote: bc}
	for _, p := range []struct {
		page func(StartConfig) http.HandlerFunc
		path string
	}{{Start, "/start"}, {Notizen, "/notizen"}} {
		_, body, _ := familyPage(t, p.page, cfg, "GET", p.path, "family,admins")
		mustContain(t, body, "href=/sprechen><svg")
		before(t, body, "href=/notizen", "href=/sprechen")
		before(t, body, "href=/sprechen", "href=/box")
	}
	_, body, _ := talkPage(t, Sprechen(cfg), "anna")
	mustContain(t, body, "href=/sprechen aria-current=page>")
	_, body, _ = familyPage(t, Start, StartConfig{Notes: nd}, "GET", "/start", "family")
	mustNotContain(t, body, "href=/sprechen")
}

// haus#12, haus#1: a sentence goes to the Bote as the person who wrote it
// (Remote-User, Remote-Name), and the reply comes back on /sprechen,
// plainly, under the persona's name, after the person's own words. A
// reload sends nothing again (303). Each person sees only their own.
func TestSprechenRelaysAndShowsTheReply(t *testing.T) {
	House = persona.Persona{Display: "Joshua"}
	defer func() { House = persona.Default() }()
	fake, bc := fakeBote(t)
	fake.Reply = "Das Licht im Flur ist an."
	h := Sprechen(StartConfig{Bote: bc})

	rec := say(t, h, "anna", "  Mach das Licht im Flur an ", map[string]string{"Remote-Name": "Anna Muster"})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/sprechen" {
		t.Fatalf("→ %d %q, want 303 /sprechen", rec.Code, rec.Header().Get("Location"))
	}
	seen := fake.Seen()
	if len(seen) != 1 || seen[0].SessionKey != "haus:anna" {
		t.Fatalf("the Bote saw %v", seen)
	}
	msgs := seen[0].Messages()
	if msgs[len(msgs)-1]["content"] != "Mach das Licht im Flur an" || !strings.Contains(msgs[0]["content"], "Anna Muster") {
		t.Fatalf("the Bote got %v", msgs)
	}

	code, body, _ := talkPage(t, h, "anna")
	if code != 200 {
		t.Fatalf("GET /sprechen → %d", code)
	}
	mustContain(t, body, "<title>Sprechen · Zuhause</title>", "<h1>Sprechen</h1>", "Mach das Licht im Flur an", "Das Licht im Flur ist an.",
		">Du<", ">Joshua<", "action=/sprechen")
	before(t, body, "action=/sprechen", "Mach das Licht im Flur an")
	before(t, body, "Mach das Licht im Flur an", "Das Licht im Flur ist an.")
	if len(fake.Seen()) != 1 {
		t.Fatal("showing the page asked the Bote again")
	}

	_, body, _ = talkPage(t, h, "ben")
	mustNotContain(t, body, "Mach das Licht", "Das Licht im Flur ist an.")
	mustContain(t, body, "Joshua", "action=/sprechen")
}

// The reply is the Bote's text, shown as text: markup in it, and in the
// person's own words, is escaped,
// a code block (a persona's screen) keeps its lines in a box of its own,
// **bold** is bold, and nothing runs.
func TestSprechenShowsTheReplyAsText(t *testing.T) {
	fake, bc := fakeBote(t)
	fake.Reply = "Hier ist das **Menü**:\n\n```\nJOSHUA> 1 HAUS\n        2 SPIELE\n```\n<script>alert(1)</script> <a href=x>y</a>"
	h := Sprechen(StartConfig{Bote: bc})
	say(t, h, "anna", "Menü <i>bitte</i>", nil)
	_, body, _ := talkPage(t, h, "anna")
	mustContain(t, body, "Menü &lt;i&gt;bitte&lt;/i&gt;", "<strong>Menü</strong>", "<pre>JOSHUA&gt; 1 HAUS\n        2 SPIELE</pre>", "&lt;script&gt;alert(1)&lt;/script&gt;", "&lt;a href=x&gt;")
	mustNotContain(t, body, "<script", "<a href=x>", "<i>", "```", "**")
}

// Only a person, only from Haus's own page (the #35 rule): no Remote-User,
// another site, or neither Sec-Fetch-Site nor Origin is refused, and the
// Bote hears nothing. Nothing but GET, HEAD and POST.
func TestSprechenOnlyFromHaus(t *testing.T) {
	fake, bc := fakeBote(t)
	h := Sprechen(StartConfig{Bote: bc})
	for _, c := range []struct {
		user string
		hdr  map[string]string
	}{
		{"", nil},
		{"anna", map[string]string{"Sec-Fetch-Site": "cross-site"}},
		{"anna", map[string]string{"Sec-Fetch-Site": "same-site"}},
		{"anna", map[string]string{"Sec-Fetch-Site": "", "Origin": "https://evil.test"}},
		{"anna", map[string]string{"Sec-Fetch-Site": ""}},
	} {
		if rec := say(t, h, c.user, "Mach alles aus", c.hdr); rec.Code != http.StatusForbidden {
			t.Errorf("%q %v → %d, want 403", c.user, c.hdr, rec.Code)
		}
	}
	if rec := say(t, h, "anna", "Hallo", map[string]string{"Sec-Fetch-Site": "", "Origin": "http://haus.box.test"}); rec.Code != http.StatusSeeOther {
		t.Errorf("an old browser on Haus's own origin → %d", rec.Code)
	}
	for _, m := range []string{"PUT", "DELETE", "PATCH"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(m, "/sprechen", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s → %d, want 405", m, rec.Code)
		}
	}
	if n := len(fake.Seen()); n != 1 {
		t.Fatalf("the Bote heard %d messages, want only the one from Haus's own origin", n)
	}
}

// An empty field sends nothing; one longer than the field takes is
// refused in a plain line, and sends nothing either.
func TestSprechenSendsNothingEmptyOrTooLong(t *testing.T) {
	fake, bc := fakeBote(t)
	h := Sprechen(StartConfig{Bote: bc})
	rec := say(t, h, "anna", "   ", nil)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/sprechen" {
		t.Fatalf("empty → %d %q", rec.Code, rec.Header().Get("Location"))
	}
	rec = say(t, h, "anna", strings.Repeat("ä", 1001), nil)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Das ist zu lang: höchstens 1000 Zeichen.") {
		t.Fatalf("too long → %d %s", rec.Code, rec.Body.String())
	}
	if n := len(fake.Seen()); n != 0 {
		t.Fatalf("the Bote heard %d messages", n)
	}
	if rec := say(t, h, "anna", strings.Repeat("ä", 1000), nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("1000 characters → %d", rec.Code)
	}
}

// A slow Bote does not hold the page forever: after boteWait the page
// says so in one plain line — and that it may still happen, so nobody
// sends it twice blindly. Never a reply, never an error.
func TestSprechenSaysASlowBotePlainly(t *testing.T) {
	House = persona.Persona{Display: "Joshua"}
	defer func() { House = persona.Default() }()
	wait := boteWait
	boteWait = 200 * time.Millisecond
	defer func() { boteWait = wait }()
	fake, bc := fakeBote(t)
	fake.Hang = 5 * time.Second
	h := Sprechen(StartConfig{Bote: bc})
	start := time.Now()
	rec := say(t, h, "anna", "Wie wird das Wetter?", nil)
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("the page waited %s", d)
	}
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("→ %d", rec.Code)
	}
	_, body, _ := talkPage(t, h, "anna")
	mustContain(t, body, "Wie wird das Wetter?", "Joshua hat nach einer Minute noch nicht geantwortet.", "bevor du es noch einmal schickst")
	mustNotContain(t, body, "Erledigt", ">Joshua<") // Haus says it, not in the persona's name
	mustNotLeak(t, body, "deadline", "context", "timeout", "Bote", "Hermes")
}

// A Bote that is down, refuses Haus's key or fails its run is one plain
// line: the persona does not answer right now. Never its error, its
// address or its name, never a crash; the page is a 200.
func TestSprechenSaysABoteThatDoesNotAnswerPlainly(t *testing.T) {
	House = persona.Persona{Display: "Joshua"}
	defer func() { House = persona.Default() }()
	failing, bc := fakeBote(t)
	failing.Status = 502
	closed := botefake.New("k")
	closed.Close()
	for what, c := range map[string]*bote.Client{
		"failed run":  bc,
		"wrong key":   {URL: failing.Base(), Key: "falsch-falsch-falsch"},
		"not there":   {URL: closed.Base(), Key: "k"},
		"empty reply": nil,
	} {
		if c == nil {
			empty, ec := fakeBote(t)
			empty.Reply = " "
			c = ec
		}
		h := Sprechen(StartConfig{Bote: c})
		if rec := say(t, h, "anna", "Hallo", nil); rec.Code != http.StatusSeeOther {
			t.Fatalf("%s → %d", what, rec.Code)
		}
		code, body, _ := talkPage(t, h, "anna")
		if code != 200 {
			t.Fatalf("%s: page %d", what, code)
		}
		mustContain(t, body, "Hallo", "Joshua antwortet gerade nicht. Versuch es später noch einmal.")
		mustNotContain(t, body, ">Joshua<")
		mustNotLeak(t, body, "502", "Agent run", "Invalid API key", "agent_incomplete", "Bote", "Hermes", "8642", "refused")
	}
}

// Without the Bote on the box: /sprechen lands on Start like every page
// whose backend is not set, and a post from a page still open from before
// says plainly that nobody answers — a 200, never a crash.
func TestSprechenWithoutTheBote(t *testing.T) {
	h := Sprechen(StartConfig{})
	code, _, hdr := talkPage(t, h, "anna")
	if code != http.StatusFound || hdr.Get("Location") != "/start" {
		t.Fatalf("GET → %d %q, want 302 /start", code, hdr.Get("Location"))
	}
	rec := say(t, h, "anna", "Hallo", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Haus antwortet gerade nicht.") || !strings.Contains(rec.Body.String(), "href=/start") {
		t.Fatalf("POST → %d %s", rec.Code, rec.Body.String())
	}
}

// Before anything is said, the page says what the field is for: who reads
// it and where the answer appears ("leer heißt erklären", haus#35).
func TestSprechenEmptySaysWhatItIsFor(t *testing.T) {
	House = persona.Persona{Display: "Joshua"}
	defer func() { House = persona.Default() }()
	_, bc := fakeBote(t)
	_, body, _ := talkPage(t, Sprechen(StartConfig{Bote: bc}), "anna")
	mustContain(t, body, "Was du hier schreibst, liest Joshua. Die Antwort erscheint hier; das kann einen Moment dauern.")
}
