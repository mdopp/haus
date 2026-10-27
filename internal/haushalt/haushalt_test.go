package haushalt

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/mdopp/haus/internal/einkauf"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/haus/internal/abs"
	"github.com/mdopp/haus/internal/abs/absfake"
	"github.com/mdopp/haus/internal/caldav"
	"github.com/mdopp/haus/internal/caldav/davfake"
	"github.com/mdopp/haus/internal/judge"
	"github.com/mdopp/haus/internal/judge/proxyfake"
	"github.com/mdopp/haus/internal/library"
	"github.com/mdopp/haus/internal/merken"
	"github.com/mdopp/haus/internal/notes"
	"github.com/mdopp/haus/internal/paperless"
	"github.com/mdopp/haus/internal/paperless/paperlessfake"
	"github.com/mdopp/haus/internal/podcasts"
	"github.com/mdopp/haus/internal/qmd"
	"github.com/mdopp/haus/internal/qmd/qmdfake"
)

const tok = "bote-token"

func berlin(t *testing.T) *time.Location {
	t.Helper()
	l, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	return l
}

type setup struct {
	h     http.Handler
	fake  *davfake.Server
	notes string
	docs  *paperlessfake.Server
	qmd   *qmdfake.Server // the memory library's search (haus#13)
	llm   *proxyfake.Server
}

// want says which backends a setup has.
type want struct {
	cal, notes, contacts, docs bool
	library                    bool   // HAUS_QMD_URL: the memory library (haus#13)
	llm                        bool   // HAUS_LLM_*: the house model tidies its hits (haus#13)
	documents                  bool   // HAUS_QMD_DOCUMENTS: Paperless's documents in the library (haus#13)
	country                    string // HAUS_PHONE_COUNTRY; "" the default
	noPublic                   bool   // no HAUS_PUBLIC_URL: no links to Haus's pages
	noPaperlessPublic          bool   // no HAUS_PAPERLESS_PUBLIC_URL: Haus's document page is a 404
}

const hausPublic = "https://haus.example"

const plToken = "pl-geheim-123"

func newSetup(t *testing.T, w want) setup {
	t.Helper()
	s := setup{}
	cfg := Config{Token: tok, Loc: berlin(t), Version: "test", PhoneCountry: w.country, PublicURL: hausPublic, Fetcher: merken.Fetcher{AllowPrivate: true}}
	if w.noPublic {
		cfg.PublicURL = ""
	}
	if w.cal || w.contacts {
		s.fake = davfake.New("bote", "pw")
		t.Cleanup(s.fake.Close)
		dav := &caldav.Client{URL: s.fake.URL, User: "bote", Password: "pw", Loc: cfg.Loc}
		if w.cal {
			s.fake.AddCalendar("/bote/familie/", "Haushalt")
			cfg.Calendar = dav
		}
		if w.contacts {
			s.fake.AddAddressBook("/bote/kontakte/", "Haushalt")
			cfg.Contacts = dav
		}
	}
	if w.docs {
		s.docs = paperlessfake.New(plToken)
		t.Cleanup(s.docs.Close)
		cfg.Documents = &paperless.Client{URL: s.docs.URL, Token: plToken, PublicURL: "https://paperless.example"}
		if w.noPaperlessPublic {
			cfg.Documents.PublicURL = ""
		}
	}
	if w.notes {
		s.notes = t.TempDir()
		d, err := notes.Open(s.notes)
		if err != nil {
			t.Fatal(err)
		}
		cfg.Notes = d
	}
	if w.library {
		s.qmd = qmdfake.New("notizen")
		t.Cleanup(s.qmd.Close)
		cfg.Library = &qmd.Client{URL: s.qmd.URL, Collection: "notizen"}
		if w.documents {
			cfg.Library.Documents = "dokumente"
		}
	}
	if w.llm {
		s.llm = proxyfake.New("gemma-4-e4b")
		t.Cleanup(s.llm.Close)
		cfg.Judge = &judge.Client{URL: s.llm.Base(), Model: "gemma-4-e4b", Key: "model-key"}
	}
	s.h = Handler(cfg)
	return s
}

func rpc(t *testing.T, h http.Handler, bearer, method string, params any) (int, map[string]any) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 7, "method": method, "params": params})
	req := httptest.NewRequest(http.MethodPost, "/mcp/haushalt", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

// call runs one tool and returns its text and isError.
func call(t *testing.T, h http.Handler, name string, args map[string]any) (string, bool) {
	t.Helper()
	code, out := rpc(t, h, tok, "tools/call", map[string]any{"name": name, "arguments": args})
	if code != 200 {
		t.Fatalf("%s: HTTP %d", name, code)
	}
	res, ok := out["result"].(map[string]any)
	if !ok {
		t.Fatalf("%s: no result: %v", name, out)
	}
	content := res["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	if content[0].(map[string]any)["type"] != "text" {
		t.Fatalf("%s: content type: %v", name, content)
	}
	return text, res["isError"] == true
}

func toolNames(t *testing.T, h http.Handler) []string {
	t.Helper()
	_, out := rpc(t, h, tok, "tools/list", map[string]any{})
	var names []string
	for _, x := range out["result"].(map[string]any)["tools"].([]any) {
		tool := x.(map[string]any)
		schema := tool["inputSchema"].(map[string]any)
		if schema["type"] != "object" || schema["properties"] == nil || schema["required"] == nil || tool["description"] == "" {
			t.Fatalf("tool %v: a typed schema and a description", tool)
		}
		names = append(names, tool["name"].(string))
	}
	sort.Strings(names)
	return names
}

// The endpoint answers only the Bote's bearer; with no token configured it
// answers nobody — never open.
func TestAuth(t *testing.T) {
	s := newSetup(t, want{notes: true})
	for _, bearer := range []string{"", "wrong", tok + "x"} {
		if code, _ := rpc(t, s.h, bearer, "tools/list", nil); code != http.StatusUnauthorized {
			t.Fatalf("bearer %q: %d", bearer, code)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/mcp/haushalt", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Authorization", tok) // no "Bearer "
	rec := httptest.NewRecorder()
	s.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("a bare token is not a bearer: %d", rec.Code)
	}
	open := Handler(Config{Loc: time.UTC})
	for _, bearer := range []string{"", " "} {
		if code, _ := rpc(t, open, bearer, "tools/list", nil); code != http.StatusUnauthorized {
			t.Fatalf("no configured token must refuse %q: %d", bearer, code)
		}
	}
	for _, header := range []string{"Bearer ", "Bearer", tok, "bearer " + tok} {
		for _, h := range []http.Handler{open, s.h} {
			req := httptest.NewRequest(http.MethodPost, "/mcp/haushalt", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
			req.Header.Set("Authorization", header)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("Authorization %q: %d", header, rec.Code)
			}
		}
	}
	if code, _ := rpc(t, s.h, tok, "tools/list", nil); code != 200 {
		t.Fatalf("the Bote: %d", code)
	}
	get := httptest.NewRequest(http.MethodGet, "/mcp/haushalt", nil)
	get.Header.Set("Authorization", "Bearer "+tok)
	rec = httptest.NewRecorder()
	s.h.ServeHTTP(rec, get)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET: %d", rec.Code)
	}
}

// laib's internal/mcpserver shape: initialize, ping, tools/list,
// tools/call, JSON-RPC errors for an unknown tool or method.
func TestProtocolShape(t *testing.T) {
	s := newSetup(t, want{cal: true, notes: true})
	_, out := rpc(t, s.h, tok, "initialize", map[string]any{"protocolVersion": "2025-03-26"})
	if out["jsonrpc"] != "2.0" || out["id"] != float64(7) {
		t.Fatalf("envelope: %v", out)
	}
	res := out["result"].(map[string]any)
	info := res["serverInfo"].(map[string]any)
	if res["protocolVersion"] != "2025-03-26" || info["name"] != "haushalt" || info["version"] != "test" || res["capabilities"].(map[string]any)["tools"] == nil {
		t.Fatalf("initialize: %v", res)
	}
	if ins, _ := res["instructions"].(string); !strings.Contains(ins, "Europe/Berlin") {
		t.Fatalf("the instructions name the house's zone: %q", ins)
	}
	if _, out := rpc(t, s.h, tok, "ping", nil); out["result"] == nil {
		t.Fatalf("ping: %v", out)
	}
	_, out = rpc(t, s.h, tok, "tools/call", map[string]any{"name": "gibtsnicht", "arguments": map[string]any{}})
	if e, _ := out["error"].(map[string]any); e == nil || e["code"] != float64(-32602) {
		t.Fatalf("unknown tool: %v", out)
	}
	_, out = rpc(t, s.h, tok, "resources/list", nil)
	if e, _ := out["error"].(map[string]any); e == nil || e["code"] != float64(-32601) {
		t.Fatalf("unknown method: %v", out)
	}
	req := httptest.NewRequest(http.MethodPost, "/mcp/haushalt", strings.NewReader("{"))
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	s.h.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "-32700") {
		t.Fatalf("parse error: %s", rec.Body)
	}
}

// tools/list shows only what works: a tool whose backend is not set is
// not there (the Bote's tool budget, laib#54).
func TestToolsOnlyWhatWorks(t *testing.T) {
	cal := []string{"erinnerung_anlegen", "erinnerungen", "termin_anlegen", "termine"}
	not := []string{"notiz_anlegen", "notiz_aus_link", "notiz_lesen", "notizen_mit_tag", "notizen_suchen"}
	con := []string{"kontakt_anlegen", "kontakt_ergaenzen", "kontakt_lesen", "kontakte_suchen"}
	doc := []string{"dokumente_suchen", "vertraege_finden"}
	join := func(ls ...[]string) []string {
		var out []string
		for _, l := range ls {
			out = append(out, l...)
		}
		sort.Strings(out)
		return out
	}
	for _, c := range []struct {
		w    want
		want []string
	}{
		{want{}, nil},
		{want{cal: true}, cal},
		{want{notes: true}, not},
		{want{library: true}, nil},
		{want{notes: true, library: true}, join(not, []string{"notizen_finden"})},
		{want{contacts: true}, con},
		{want{docs: true}, doc},
		{want{cal: true, notes: true}, join(cal, not)},
		{want{cal: true, notes: true, contacts: true, docs: true}, join(cal, not, con, doc)},
		{want{cal: true, notes: true, contacts: true, docs: true, library: true}, join(cal, not, con, doc, []string{"notizen_finden"})},
	} {
		got := toolNames(t, newSetup(t, c.w).h)
		if strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Fatalf("%+v: %v", c.w, got)
		}
	}
}

func TestFromEnv(t *testing.T) {
	for _, k := range []string{"HAUS_HAUSHALT_TOKEN", "HAUS_RADICALE_URL", "HAUS_RADICALE_USER", "HAUS_RADICALE_PASSWORD", "HAUS_RADICALE_CALENDAR",
		"HAUS_RADICALE_ADDRESSBOOK", "HAUS_NOTES_DIR", "HAUS_PAPERLESS_URL", "HAUS_PAPERLESS_TOKEN", "HAUS_PAPERLESS_PUBLIC_URL"} {
		t.Setenv(k, "")
	}
	t.Setenv("TZ", "Europe/Berlin")
	cfg, err := FromEnv()
	if err != nil || cfg.Calendar != nil || cfg.Contacts != nil || cfg.Documents != nil || cfg.Notes != nil || cfg.Token != "" || cfg.Loc.String() != "Europe/Berlin" {
		t.Fatalf("unset: %+v %v", cfg, err)
	}
	t.Setenv("HAUS_RADICALE_URL", "radicale:5232")
	if _, err := FromEnv(); err == nil {
		t.Fatal("a malformed Radicale address refuses to start")
	}
	t.Setenv("HAUS_RADICALE_URL", "http://host.containers.internal:5232/")
	if cfg, err := FromEnv(); err != nil || cfg.Calendar != nil || cfg.Contacts != nil {
		t.Fatalf("an address without the account is off, not an error: %+v %v", cfg, err)
	}
	t.Setenv("HAUS_RADICALE_USER", "bote")
	t.Setenv("HAUS_RADICALE_PASSWORD", "pw")
	t.Setenv("HAUS_RADICALE_CALENDAR", "/familie/kalender/")
	t.Setenv("HAUS_RADICALE_ADDRESSBOOK", "/familie/kontakte/")
	t.Setenv("HAUS_NOTES_DIR", filepath.Join(t.TempDir(), "fehlt"))
	cfg, err = FromEnv()
	if err != nil || cfg.Calendar == nil || cfg.Calendar.URL != "http://host.containers.internal:5232" || cfg.Calendar.User != "bote" || cfg.Calendar.Calendar != "/familie/kalender/" {
		t.Fatalf("calendar: %+v %v", cfg.Calendar, err)
	}
	if cfg.Contacts == nil || cfg.Contacts.URL != cfg.Calendar.URL || cfg.Contacts.AddressBook != "/familie/kontakte/" {
		t.Fatalf("contacts on the same account: %+v", cfg.Contacts)
	}
	if cfg.Notes != nil {
		t.Fatal("a notes dir that is not there leaves the note tools out")
	}
	t.Setenv("HAUS_NOTES_DIR", t.TempDir())
	t.Setenv("HAUS_HAUSHALT_TOKEN", tok)
	if cfg, _ := FromEnv(); cfg.Notes == nil || cfg.Token != tok {
		t.Fatalf("notes and token: %+v", cfg)
	}
}

// An appointment lands in the family's calendar and is read back in the
// house's time; a reminder is a VTODO with an alarm (ADR 0017 §3: where
// people see it).
func TestCalendarTools(t *testing.T) {
	s := newSetup(t, want{cal: true})
	text, isErr := call(t, s.h, "termin_anlegen", map[string]any{"titel": "Zahnarzt", "beginn": "2026-10-14T15:00", "ort": "Praxis, Hauptstr. 1"})
	if isErr || !strings.Contains(text, "Haushalt") || !strings.Contains(text, "Mi 14.10.2026 15:00–16:00: Zahnarzt (Praxis, Hauptstr. 1)") {
		t.Fatalf("termin_anlegen: %q %v", text, isErr)
	}
	var ics string
	for _, v := range s.fake.Objects() {
		ics = v
	}
	for _, want := range []string{"DTSTART:20261014T130000Z", "DTEND:20261014T140000Z", "SUMMARY:Zahnarzt", `LOCATION:Praxis\, Hauptstr. 1`} {
		if !strings.Contains(ics, want) {
			t.Fatalf("stored ICS lacks %q:\n%s", want, ics)
		}
	}
	text, isErr = call(t, s.h, "termin_anlegen", map[string]any{"titel": "Ferien", "beginn": "2026-10-19", "ende": "2026-10-23"})
	if isErr || !strings.Contains(text, "Mo 19.10.2026 – Fr 23.10.2026 ganztägig: Ferien") {
		t.Fatalf("all-day: %q", text)
	}
	text, isErr = call(t, s.h, "termine", map[string]any{"von": "2026-10-14"})
	if isErr || text != "Mi 14.10.2026 15:00–16:00: Zahnarzt (Praxis, Hauptstr. 1)" {
		t.Fatalf("termine (one day): %q", text)
	}
	text, _ = call(t, s.h, "termine", map[string]any{"von": "2026-10-14", "bis": "2026-10-31"})
	if lines := strings.Split(text, "\n"); len(lines) != 2 || !strings.Contains(lines[1], "Ferien") {
		t.Fatalf("termine (range): %q", text)
	}
	text, _ = call(t, s.h, "termine", map[string]any{"von": "2026-11-01", "bis": "2026-11-02"})
	if !strings.HasPrefix(text, "Keine Termine") {
		t.Fatalf("empty: %q", text)
	}
	for _, bad := range []map[string]any{
		{"titel": "x", "beginn": "morgen"},
		{"titel": "x", "beginn": "2026-10-14T15:00", "ende": "2026-10-14T14:00"},
		{"titel": "x", "beginn": "2026-10-14", "ende": "2026-10-14T14:00"},
		{"beginn": "2026-10-14T15:00"},
	} {
		if text, isErr := call(t, s.h, "termin_anlegen", bad); !isErr {
			t.Fatalf("%v: want a tool error, got %q", bad, text)
		}
	}
	if _, isErr := call(t, s.h, "termine", map[string]any{"von": "2026-10-14", "bis": "2026-10-13"}); !isErr {
		t.Fatal("bis before von is an error")
	}

	text, isErr = call(t, s.h, "erinnerung_anlegen", map[string]any{"text": "Gelbe Tonne raus", "wann": "2026-10-15T19:00"})
	if isErr || !strings.Contains(text, "Do 15.10.2026 19:00: Gelbe Tonne raus") {
		t.Fatalf("erinnerung_anlegen: %q %v", text, isErr)
	}
	var todo string
	for _, v := range s.fake.Objects() {
		if strings.Contains(v, "BEGIN:VTODO") {
			todo = v
		}
	}
	for _, want := range []string{"DUE:20261015T170000Z", "BEGIN:VALARM", "TRIGGER;VALUE=DATE-TIME:20261015T170000Z", "STATUS:NEEDS-ACTION"} {
		if !strings.Contains(todo, want) {
			t.Fatalf("VTODO lacks %q:\n%s", want, todo)
		}
	}
	s.fake.Put("/bote/familie/done.ics", "BEGIN:VCALENDAR\r\nBEGIN:VTODO\r\nUID:d\r\nSUMMARY:erledigt\r\nSTATUS:COMPLETED\r\nEND:VTODO\r\nEND:VCALENDAR\r\n")
	text, isErr = call(t, s.h, "erinnerungen", nil)
	if isErr || text != "Do 15.10.2026 19:00: Gelbe Tonne raus" {
		t.Fatalf("erinnerungen: %q", text)
	}
	// a reminder due on a day has no time: never "00:00" (haus#12 review)
	s.fake.Put("/bote/familie/tag.ics", "BEGIN:VCALENDAR\r\nBEGIN:VTODO\r\nUID:t\r\nSUMMARY:Geburtstagskarte\r\nDUE;VALUE=DATE:20261016\r\nSTATUS:NEEDS-ACTION\r\nEND:VTODO\r\nEND:VCALENDAR\r\n")
	text, isErr = call(t, s.h, "erinnerungen", nil)
	if isErr || !strings.Contains(text, "Fr 16.10.2026: Geburtstagskarte") || strings.Contains(text, "00:00") {
		t.Fatalf("a day-only reminder: %q", text)
	}
}

// A note is a Markdown file in the share; search and read go through the
// same tools; no name leaves the directory.
func TestNoteTools(t *testing.T) {
	s := newSetup(t, want{notes: true})
	text, isErr := call(t, s.h, "notiz_anlegen", map[string]any{"titel": "WLAN Gäste", "text": "Netz: Gast\nPasswort steht am Router"})
	if isErr || !strings.Contains(text, "wlan-gaeste.md") {
		t.Fatalf("notiz_anlegen: %q", text)
	}
	if b, err := os.ReadFile(filepath.Join(s.notes, "wlan-gaeste.md")); err != nil || !strings.HasPrefix(string(b), "# WLAN Gäste\n") {
		t.Fatalf("the file: %q %v", b, err)
	}
	if text, _ := call(t, s.h, "notiz_anlegen", map[string]any{"titel": "WLAN Gäste", "text": "nochmal"}); !strings.Contains(text, "wlan-gaeste-2.md") {
		t.Fatalf("never overwrites: %q", text)
	}
	text, isErr = call(t, s.h, "notizen_suchen", map[string]any{"text": "router"})
	if isErr || !strings.Contains(text, "## WLAN Gäste (wlan-gaeste.md)") || !strings.Contains(text, "Passwort steht am Router") || strings.Contains(text, "wlan-gaeste-2.md") {
		t.Fatalf("notizen_suchen: %q", text)
	}
	text, isErr = call(t, s.h, "notiz_lesen", map[string]any{"titel": "wlan gäste"})
	if isErr || !strings.Contains(text, "Netz: Gast") {
		t.Fatalf("notiz_lesen: %q", text)
	}
	outside := filepath.Join(filepath.Dir(s.notes), "geheim.md")
	os.WriteFile(outside, []byte("# geheim\nnicht hier"), 0o644)
	for _, ref := range []string{"../geheim.md", outside, "/etc/passwd"} {
		if text, isErr := call(t, s.h, "notiz_lesen", map[string]any{"titel": ref}); !isErr || strings.Contains(text, "nicht hier") || strings.Contains(text, "root:") {
			t.Fatalf("%q: %q %v", ref, text, isErr)
		}
	}
	if text, _ := call(t, s.h, "notizen_suchen", map[string]any{"text": "kommt nicht vor"}); !strings.HasPrefix(text, "Keine Notiz") {
		t.Fatalf("no hit: %q", text)
	}
}

// A notification gets 202 and no body (the streamable-HTTP transport).
func TestNotificationAccepted(t *testing.T) {
	s := newSetup(t, want{notes: true})
	req := httptest.NewRequest(http.MethodPost, "/mcp/haushalt", strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	s.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted || rec.Body.Len() != 0 {
		t.Fatalf("%d %q", rec.Code, rec.Body)
	}
}

// A number where text belongs is a type error, not a missing argument.
func TestArgumentTypes(t *testing.T) {
	s := newSetup(t, want{cal: true, notes: true})
	text, isErr := call(t, s.h, "termin_anlegen", map[string]any{"titel": 5, "beginn": "2026-10-14T15:00"})
	if !isErr || !strings.Contains(text, "titel") || !strings.Contains(text, "Text") || strings.Contains(text, "fehlt") {
		t.Fatalf("%q %v", text, isErr)
	}
	text, isErr = call(t, s.h, "notiz_anlegen", map[string]any{"titel": "a", "text": []any{"x"}})
	if !isErr || !strings.Contains(text, "text") {
		t.Fatalf("%q %v", text, isErr)
	}
	if len(s.fake.Objects()) != 0 {
		t.Fatal("nothing written on a type error")
	}
}

// A day without a time is 9:00 local, also on the days the clocks change.
func TestDateOnlyReminderOnDSTDays(t *testing.T) {
	s := newSetup(t, want{cal: true})
	for day, due := range map[string]string{"2026-03-29": "DUE:20260329T070000Z", "2026-10-25": "DUE:20261025T080000Z"} {
		text, isErr := call(t, s.h, "erinnerung_anlegen", map[string]any{"text": "Uhren prüfen " + day, "wann": day})
		if isErr || !strings.Contains(text, " 09:00: ") {
			t.Fatalf("%s: %q", day, text)
		}
		found := false
		for _, v := range s.fake.Objects() {
			if strings.Contains(v, day) && strings.Contains(v, due+"\r\n") {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s: no VTODO with %s in %v", day, due, s.fake.Objects())
		}
	}
}

// Writes refuse over-long fields; what is read back is cut short.
func TestLengthCaps(t *testing.T) {
	s := newSetup(t, want{cal: true, notes: true})
	long := strings.Repeat("x", MaxField+1)
	for name, args := range map[string]map[string]any{
		"termin_anlegen":     {"titel": long, "beginn": "2026-10-14T15:00"},
		"termin_anlegen ort": {"titel": "a", "beginn": "2026-10-14T15:00", "ort": long},
		"erinnerung_anlegen": {"text": strings.Repeat("x", MaxReminder+1), "wann": "2026-10-14T15:00"},
		"notiz_anlegen":      {"titel": long, "text": "x"},
	} {
		if text, isErr := call(t, s.h, strings.Fields(name)[0], args); !isErr || !strings.Contains(text, "lang") {
			t.Fatalf("%s: %q %v", name, text, isErr)
		}
	}
	if len(s.fake.Objects()) != 0 {
		t.Fatal("nothing written")
	}
	s.fake.Put("/bote/familie/lang.ics", "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:l\r\nDTSTART:20261014T100000Z\r\nDTEND:20261014T110000Z\r\nSUMMARY:"+strings.Repeat("y", 5000)+"\r\nLOCATION:"+strings.Repeat("z", 5000)+"\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")
	text, _ := call(t, s.h, "termine", map[string]any{"von": "2026-10-14"})
	if len([]rune(text)) > 2*MaxField+100 {
		t.Fatalf("a phone's long title is cut in the answer: %d characters", len([]rune(text)))
	}
}

// Paperless: the address and the token switch the document tool on; a
// malformed address refuses to start; the public address only makes links.
func TestFromEnvPaperless(t *testing.T) {
	for _, k := range []string{"HAUS_RADICALE_URL", "HAUS_NOTES_DIR", "HAUS_PAPERLESS_URL", "HAUS_PAPERLESS_TOKEN", "HAUS_PAPERLESS_PUBLIC_URL"} {
		t.Setenv(k, "")
	}
	t.Setenv("HAUS_PAPERLESS_URL", "http://host.containers.internal:8000/")
	if cfg, err := FromEnv(); err != nil || cfg.Documents != nil {
		t.Fatalf("no token, no documents: %+v %v", cfg.Documents, err)
	}
	t.Setenv("HAUS_PAPERLESS_TOKEN", plToken)
	cfg, err := FromEnv()
	if err != nil || cfg.Documents == nil || cfg.Documents.URL != "http://host.containers.internal:8000" || cfg.Documents.Token != plToken || cfg.Documents.PublicURL != "" {
		t.Fatalf("documents: %+v %v", cfg.Documents, err)
	}
	t.Setenv("HAUS_PAPERLESS_PUBLIC_URL", "https://paperless.example.org/")
	if cfg, _ := FromEnv(); cfg.Documents == nil || cfg.Documents.PublicURL != "https://paperless.example.org" {
		t.Fatalf("public: %+v", cfg.Documents)
	}
	for k, v := range map[string]string{"HAUS_PAPERLESS_URL": "paperless:8000", "HAUS_PAPERLESS_PUBLIC_URL": "javascript:alert(1)"} {
		t.Run(k, func(t *testing.T) {
			t.Setenv(k, v)
			if _, err := FromEnv(); err == nil {
				t.Fatalf("%s=%q refuses to start", k, v)
			} else if strings.Contains(err.Error(), plToken) {
				t.Fatal("the token never goes into a message")
			} else if !strings.Contains(err.Error(), "http(s)://") {
				t.Fatalf("http is allowed, the message says so: %v", err)
			}
		})
	}
}

// A contact lands in the Haushalt address book as a vCard and is found by
// name, by phone however it is written, and by email; the answer is short.
func TestContactTools(t *testing.T) {
	s := newSetup(t, want{contacts: true})
	text, isErr := call(t, s.h, "kontakt_anlegen", map[string]any{"name": "Eva Müller", "telefon": "+49 170 1234567", "email": "eva@example.org", "geburtstag": "1985-04-12"})
	if isErr || !strings.Contains(text, "Haushalt") || !strings.Contains(text, "Eva Müller") || !strings.Contains(text, "12.04.1985") {
		t.Fatalf("kontakt_anlegen: %q %v", text, isErr)
	}
	var vcf string
	for p, v := range s.fake.Objects() {
		if !strings.HasPrefix(p, "/bote/kontakte/") || !strings.HasSuffix(p, ".vcf") {
			t.Fatalf("stray object %s", p)
		}
		vcf = v
	}
	for _, want := range []string{"VERSION:4.0", "FN:Eva Müller", "TEL:+49 170 1234567", "EMAIL:eva@example.org", "BDAY:19850412", "UID:urn:uuid:"} {
		if !strings.Contains(vcf, want) {
			t.Fatalf("vCard lacks %q:\n%s", want, vcf)
		}
	}
	s.fake.Put("/bote/kontakte/oma.vcf", "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:o\r\nFN:Oma Erna\r\nTEL:0711 98765\r\nBDAY:--0309\r\nEND:VCARD\r\n")
	for q, want := range map[string]string{
		"müller": "Eva Müller", "EVA": "Eva Müller", "0170 1234567": "Eva Müller", "1234567": "Eva Müller",
		"example.org": "Eva Müller", "oma": "Oma Erna", "98765": "Oma Erna",
	} {
		text, isErr := call(t, s.h, "kontakte_suchen", map[string]any{"text": q})
		if isErr || !strings.Contains(text, want) || strings.Count(text, "\n") != 0 {
			t.Fatalf("%q: %q", q, text)
		}
	}
	text, _ = call(t, s.h, "kontakte_suchen", map[string]any{"text": "eva"})
	for _, want := range []string{"Eva Müller", "+49 170 1234567", "eva@example.org", "12.04.1985"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the hit shows %q: %q", want, text)
		}
	}
	if text, _ := call(t, s.h, "kontakte_suchen", map[string]any{"text": "oma"}); !strings.Contains(text, "09.03.") {
		t.Fatalf("a birthday without a year: %q", text)
	}
	if text, _ := call(t, s.h, "kontakte_suchen", map[string]any{"text": "niemand"}); !strings.HasPrefix(text, "Kein Kontakt") {
		t.Fatalf("no hit: %q", text)
	}
	n := len(s.fake.Objects())
	for _, bad := range []map[string]any{
		{},
		{"name": "x", "telefon": "ruf an"},
		{"name": "x", "telefon": "0170\nEMAIL:x@y"},
		{"name": "x", "email": "keine-adresse"},
		{"name": "x", "email": "a@b.de\r\nTEL:1"},
		{"name": "x", "geburtstag": "12.04."},
		{"name": "x", "geburtstag": "1985-02-30"},
		{"name": strings.Repeat("x", MaxField+1)},
	} {
		if text, isErr := call(t, s.h, "kontakt_anlegen", bad); !isErr {
			t.Fatalf("%v: want a tool error, got %q", bad, text)
		}
	}
	if _, isErr := call(t, s.h, "kontakte_suchen", map[string]any{"text": ""}); !isErr {
		t.Fatal("an empty search is an error")
	}
	if len(s.fake.Objects()) != n {
		t.Fatal("nothing written on a refused contact")
	}
}

// A newline or a semicolon in a name cannot add a property to the card.
func TestContactNameInjection(t *testing.T) {
	s := newSetup(t, want{contacts: true})
	if text, isErr := call(t, s.h, "kontakt_anlegen", map[string]any{"name": "Eva;\r\nTEL:+49666\nEND:VCARD"}); isErr {
		t.Fatalf("%q", text)
	}
	for _, v := range s.fake.Objects() {
		root, err := caldav.Parse(v)
		if err != nil {
			t.Fatal(err)
		}
		cs := caldav.Contacts(root)
		if len(cs) != 1 || len(cs[0].Phones) != 0 || strings.Contains(v, "\r\nTEL:") {
			t.Fatalf("injected:\n%s", v)
		}
	}
}

// Many hits: a few shown, the rest counted; a long name is cut.
func TestContactSearchSmall(t *testing.T) {
	s := newSetup(t, want{contacts: true})
	for i := 0; i < 15; i++ {
		s.fake.Put(fmt.Sprintf("/bote/kontakte/%d.vcf", i), fmt.Sprintf("BEGIN:VCARD\r\nVERSION:4.0\r\nUID:%d\r\nFN:Müller %d %s\r\nEND:VCARD\r\n", i, i, strings.Repeat("y", 3000)))
	}
	text, isErr := call(t, s.h, "kontakte_suchen", map[string]any{"text": "müller"})
	if isErr || strings.Count(text, "Müller") != maxContacts || !strings.Contains(text, fmt.Sprintf("… und %d weitere", 15-maxContacts)) {
		t.Fatalf("%q", text)
	}
	if len([]rune(text)) > maxContacts*(MaxField+50)+100 {
		t.Fatalf("too long: %d", len([]rune(text)))
	}
}

// Documents: date, correspondent, title, tags and a link to Haus's own
// page for it (haus#28: /dokumente/<id>, which sends the person on to
// Paperless); at most 20; read-only; a Paperless failure is a tool error
// without the token.
func TestDocumentTools(t *testing.T) {
	s := newSetup(t, want{docs: true})
	s.docs.Correspondent(3, "Stadtwerke")
	s.docs.Tag(1, "Rechnung")
	s.docs.Tag(2, "Strom")
	s.docs.Add(paperlessfake.Doc{ID: 12, Title: "Rechnung März", Correspondent: 3, Created: "2025-03-14", Tags: []int{1, 2}})
	text, isErr := call(t, s.h, "dokumente_suchen", map[string]any{"text": "rechnung"})
	if isErr || text != "14.03.2025 · Stadtwerke · Rechnung März · Schlagworte: Rechnung, Strom — https://haus.example/dokumente/12" {
		t.Fatalf("dokumente_suchen: %q %v", text, isErr)
	}
	if text, _ := call(t, s.h, "dokumente_suchen", map[string]any{"text": "steuer"}); !strings.HasPrefix(text, "Kein Dokument") {
		t.Fatalf("no hit: %q", text)
	}
	for i := 0; i < 30; i++ {
		s.docs.Add(paperlessfake.Doc{ID: 100 + i, Title: fmt.Sprint("Kontoauszug ", i)})
	}
	text, _ = call(t, s.h, "dokumente_suchen", map[string]any{"text": "kontoauszug"})
	if strings.Count(text, "Kontoauszug") != 20 || !strings.Contains(text, "… und 10 weitere") {
		t.Fatalf("at most 20: %q", text)
	}
	for _, r := range s.docs.Seen() {
		if r.Method != http.MethodGet {
			t.Fatalf("read-only: %s %s", r.Method, r.URL)
		}
		if r.URL.Path == "/api/documents/" && r.URL.Query().Get("page_size") != "20" {
			t.Fatalf("page_size: %s", r.URL)
		}
	}
	for _, bad := range []string{"", strings.Repeat("x", MaxField+1)} {
		if _, isErr := call(t, s.h, "dokumente_suchen", map[string]any{"text": bad}); !isErr {
			t.Fatalf("%q: want an error", bad)
		}
	}
	s.docs.Status = http.StatusInternalServerError
	text, isErr = call(t, s.h, "dokumente_suchen", map[string]any{"text": "rechnung"})
	if !isErr || !strings.Contains(text, "Paperless") || strings.Contains(text, plToken) {
		t.Fatalf("failure: %q %v", text, isErr)
	}
}

// A number is found however either side writes it: national, +49 or
// 0049, spaces, dashes, slashes; the country is HAUS_PHONE_COUNTRY.
func TestPhoneSearchBothWays(t *testing.T) {
	s := newSetup(t, want{contacts: true})
	s.fake.Put("/bote/kontakte/a.vcf", "BEGIN:VCARD\r\nVERSION:4.0\r\nUID:a\r\nFN:Anna National\r\nTEL:0170 1234567\r\nEND:VCARD\r\n")
	s.fake.Put("/bote/kontakte/b.vcf", "BEGIN:VCARD\r\nVERSION:4.0\r\nUID:b\r\nFN:Bert International\r\nTEL:+49 (0)711 / 98-76-54\r\nEND:VCARD\r\n")
	s.fake.Put("/bote/kontakte/c.vcf", "BEGIN:VCARD\r\nVERSION:4.0\r\nUID:c\r\nFN:Carla Doppelnull\r\nTEL:0049 30 555666\r\nEND:VCARD\r\n")
	s.fake.Put("/bote/kontakte/d.vcf", "BEGIN:VCARD\r\nVERSION:4.0\r\nUID:d\r\nFN:Dora Wien\r\nTEL:+43 660 1234567\r\nEND:VCARD\r\n")
	for q, want := range map[string]string{
		"+49 170 1234567": "Anna", "0049 170 1234567": "Anna", "0170-1234567": "Anna", "+491701234567": "Anna",
		"0711 987654": "Bert", "+49 711 987654": "Bert", "0049711987654": "Bert",
		"030 555666": "Carla", "+49 30 555666": "Carla",
		"+43 660 1234567": "Dora", "0043 660 1234567": "Dora",
	} {
		text, isErr := call(t, s.h, "kontakte_suchen", map[string]any{"text": q})
		if isErr || !strings.Contains(text, want) || strings.Contains(text, "\n") {
			t.Fatalf("%q: %q", q, text)
		}
	}
	// a foreign number written nationally is not a German one
	if text, _ := call(t, s.h, "kontakte_suchen", map[string]any{"text": "+49 660 1234567"}); strings.Contains(text, "Dora") {
		t.Fatalf("+49 660 is not +43 660: %q", text)
	}
	// one or two digits match no phone
	for _, q := range []string{"1", "7", "17", "+4"} {
		if text, _ := call(t, s.h, "kontakte_suchen", map[string]any{"text": q}); !strings.HasPrefix(text, "Kein Kontakt") {
			t.Fatalf("%q matched: %q", q, text)
		}
	}

	at := newSetup(t, want{contacts: true, country: "43"})
	at.fake.Put("/bote/kontakte/d.vcf", "BEGIN:VCARD\r\nVERSION:4.0\r\nUID:d\r\nFN:Dora Wien\r\nTEL:0660 1234567\r\nEND:VCARD\r\n")
	if text, _ := call(t, at.h, "kontakte_suchen", map[string]any{"text": "+43 660 1234567"}); !strings.Contains(text, "Dora") {
		t.Fatalf("HAUS_PHONE_COUNTRY=43: %q", text)
	}
}

func TestFromEnvPhoneCountry(t *testing.T) {
	t.Setenv("HAUS_RADICALE_URL", "")
	t.Setenv("HAUS_NOTES_DIR", "")
	t.Setenv("HAUS_PAPERLESS_URL", "")
	t.Setenv("HAUS_PHONE_COUNTRY", "")
	if cfg, err := FromEnv(); err != nil || cfg.PhoneCountry != "49" {
		t.Fatalf("default: %q %v", cfg.PhoneCountry, err)
	}
	t.Setenv("HAUS_PHONE_COUNTRY", "+43")
	if cfg, err := FromEnv(); err != nil || cfg.PhoneCountry != "43" {
		t.Fatalf("+43: %q %v", cfg.PhoneCountry, err)
	}
	for _, bad := range []string{"Österreich", "0043", "12345"} {
		t.Setenv("HAUS_PHONE_COUNTRY", bad)
		if _, err := FromEnv(); err == nil {
			t.Fatalf("%q refuses to start", bad)
		}
	}
}

// The year 0000 is no birthday.
func TestBirthdayYearZeroRefused(t *testing.T) {
	s := newSetup(t, want{contacts: true})
	if text, isErr := call(t, s.h, "kontakt_anlegen", map[string]any{"name": "x", "geburtstag": "0000-04-12"}); !isErr {
		t.Fatalf("%q", text)
	}
	if len(s.fake.Objects()) != 0 {
		t.Fatal("written")
	}
}

const annaCard = "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:urn:uuid:anna-1\r\nFN:Anna Schmidt\r\nN:Schmidt;Anna;;;\r\nNICKNAME:Anni\r\n" +
	"ORG:Stadtwerke;Kundendienst\r\nTEL;TYPE=CELL:+49 170 1111111\r\nTEL;TYPE=HOME:0711 222222\r\nTEL;TYPE=WORK:0711 333333\r\nTEL:0711 444444\r\n" +
	"item1.EMAIL;TYPE=INTERNET:anna@example.org\r\nEMAIL:anna.schmidt@firma.example\r\nBDAY:1985-04-12\r\n" +
	"ADR;TYPE=HOME:;;Hauptstraße 1;Stuttgart;;70173;Deutschland\r\nitem2.ADR:;;Industriestr. 5;Esslingen;;73728;\r\nEND:VCARD\r\n"

// One contact read whole: every number, every email, the birthday, the
// postal addresses and its page on Haus. It is found by its name (any
// case), by its id, or by a search only it matches; when several match,
// the answer names them with their ids and shows none whole.
func TestContactRead(t *testing.T) {
	s := newSetup(t, want{contacts: true})
	s.fake.Put("/bote/kontakte/anna.vcf", annaCard)
	s.fake.Put("/bote/kontakte/berg.vcf", "BEGIN:VCARD\r\nVERSION:4.0\r\nUID:berg\r\nFN:Anna Berg\r\nTEL:0170 999999\r\nEND:VCARD\r\n")
	full := []string{"Anna Schmidt", "Anni", "Stadtwerke, Kundendienst", "+49 170 1111111", "0711 222222", "0711 333333", "0711 444444",
		"anna@example.org", "anna.schmidt@firma.example", "12.04.1985", "Hauptstraße 1, 70173 Stuttgart, Deutschland",
		"Industriestr. 5, 73728 Esslingen", hausPublic + "/kontakte/urn:uuid:anna-1"}
	for _, q := range []string{"Anna Schmidt", "anna schmidt", "  Anna   Schmidt ", "urn:uuid:anna-1", "anni", "0170 1111111", "firma.example", "schmidt"} {
		text, isErr := call(t, s.h, "kontakt_lesen", map[string]any{"kontakt": q})
		if isErr {
			t.Fatalf("%q: %q", q, text)
		}
		for _, w := range full {
			if !strings.Contains(text, w) {
				t.Fatalf("%q: the answer lacks %q:\n%s", q, w, text)
			}
		}
		if strings.Contains(text, "Anna Berg") || strings.Contains(text, "berg") {
			t.Fatalf("%q: only the one contact:\n%s", q, text)
		}
	}
	// several match: named with their ids, none shown whole
	text, isErr := call(t, s.h, "kontakt_lesen", map[string]any{"kontakt": "anna"})
	if isErr || !strings.HasPrefix(text, "Mehrere Kontakte passen") || !strings.Contains(text, "Anna Schmidt") || !strings.Contains(text, "Anna Berg") ||
		!strings.Contains(text, "urn:uuid:anna-1") || !strings.Contains(text, "berg") || strings.Contains(text, "Hauptstraße") ||
		strings.Contains(text, "kontakt_lesen") || strings.Contains(text, "kontakte_suchen") {
		t.Fatalf("ambiguous: %q %v", text, isErr)
	}
	if i, j := strings.Index(text, "Anna Berg"), strings.Index(text, "Anna Schmidt"); i > j {
		t.Fatalf("by name: %q", text)
	}
	// the same name twice: the id decides
	s.fake.Put("/bote/kontakte/max1.vcf", "BEGIN:VCARD\r\nVERSION:4.0\r\nUID:max-1\r\nFN:Max Muster\r\nTEL:0170 1\r\nEND:VCARD\r\n")
	s.fake.Put("/bote/kontakte/max2.vcf", "BEGIN:VCARD\r\nVERSION:4.0\r\nUID:max-2\r\nFN:Max Muster\r\nTEL:0170 2\r\nEND:VCARD\r\n")
	text, _ = call(t, s.h, "kontakt_lesen", map[string]any{"kontakt": "max muster"})
	if !strings.HasPrefix(text, "Mehrere Kontakte passen") || !strings.Contains(text, "max-1") || !strings.Contains(text, "max-2") {
		t.Fatalf("one name, two cards: %q", text)
	}
	if text, _ := call(t, s.h, "kontakt_lesen", map[string]any{"kontakt": "max-2"}); !strings.Contains(text, "0170 2") || strings.Contains(text, "0170 1") {
		t.Fatalf("by id: %q", text)
	}
	// an exact name wins over a search that matches more
	s.fake.Put("/bote/kontakte/eva1.vcf", "BEGIN:VCARD\r\nVERSION:4.0\r\nUID:eva-1\r\nFN:Eva\r\nTEL:0170 3\r\nEND:VCARD\r\n")
	s.fake.Put("/bote/kontakte/eva2.vcf", "BEGIN:VCARD\r\nVERSION:4.0\r\nUID:eva-2\r\nFN:Eva Lang\r\nEND:VCARD\r\n")
	if text, _ := call(t, s.h, "kontakt_lesen", map[string]any{"kontakt": "eva"}); strings.HasPrefix(text, "Mehrere") || !strings.Contains(text, "0170 3") {
		t.Fatalf("exact name: %q", text)
	}
	// an id wins over a name that is the same text
	s.fake.Put("/bote/kontakte/x.vcf", "BEGIN:VCARD\r\nVERSION:4.0\r\nUID:Eva\r\nFN:Jemand Anders\r\nEND:VCARD\r\n")
	if text, _ := call(t, s.h, "kontakt_lesen", map[string]any{"kontakt": "Eva"}); !strings.Contains(text, "Jemand Anders") ||
		strings.HasPrefix(text, "Mehrere") || strings.Contains(text, "0170 3") {
		t.Fatalf("the id first, and only the id: %q", text)
	}
	if text, isErr := call(t, s.h, "kontakt_lesen", map[string]any{"kontakt": "niemand"}); isErr || text != "Kein Kontakt passt zu „niemand“." {
		t.Fatalf("none: %q %v", text, isErr)
	}
	for _, bad := range []string{"", "  ", strings.Repeat("x", MaxField+1)} {
		if _, isErr := call(t, s.h, "kontakt_lesen", map[string]any{"kontakt": bad}); !isErr {
			t.Fatalf("%q: want an error", bad)
		}
	}
	if len(s.fake.Objects()) != 7 {
		t.Fatal("reading writes nothing")
	}
}

// Many matches: ten named, the rest counted.
func TestContactReadManyMatches(t *testing.T) {
	s := newSetup(t, want{contacts: true})
	for i := 0; i < 14; i++ {
		s.fake.Put(fmt.Sprintf("/bote/kontakte/%d.vcf", i), fmt.Sprintf("BEGIN:VCARD\r\nVERSION:4.0\r\nUID:m-%d\r\nFN:Müller %02d\r\nEND:VCARD\r\n", i, i))
	}
	text, _ := call(t, s.h, "kontakt_lesen", map[string]any{"kontakt": "müller"})
	if strings.Count(text, "Müller") != maxContacts || !strings.Contains(text, "… und 4 weitere") || !strings.Contains(text, "Müller 00") || strings.Contains(text, "Müller 13") {
		t.Fatalf("%q", text)
	}
}

// A card's text cannot add a line to the answer or make it endless: each
// value is one line and cut, at most ten of a kind, the rest counted.
func TestContactReadIsBounded(t *testing.T) {
	s := newSetup(t, want{contacts: true})
	var tels strings.Builder
	for i := 0; i < 25; i++ {
		fmt.Fprintf(&tels, "TEL:0170 %07d\r\n", i)
	}
	s.fake.Put("/bote/kontakte/v.vcf", "BEGIN:VCARD\r\nVERSION:4.0\r\nUID:v\r\nFN:Vroni\\nTEL: 666\r\nNICKNAME:a\\nb\r\nORG:c\\nd\r\n"+tels.String()+
		"EMAIL:v@example.org\\nTEL:666\r\nADR:;;Weg 1\\nZeile;Ort;;;\r\nADR:;;"+strings.Repeat("z", 3000)+";;;;\r\nEND:VCARD\r\n")
	text, isErr := call(t, s.h, "kontakt_lesen", map[string]any{"kontakt": "v"})
	if isErr {
		t.Fatal(text)
	}
	if !strings.Contains(text, "Vroni TEL: 666") || !strings.Contains(text, "Weg 1 Zeile, Ort") || !strings.Contains(text, "… und 15 weitere") ||
		strings.Contains(text, "0170 0000010") || !strings.Contains(text, "0170 0000009") {
		t.Fatalf("%s", text)
	}
	for _, l := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "TEL") {
			t.Fatalf("a line from the card: %q in\n%s", l, text)
		}
	}
	if n := strings.Count(text, "\n") + 1; n > 9 {
		t.Fatalf("%d lines:\n%s", n, text)
	}
	if len([]rune(text)) > 2500 {
		t.Fatalf("too long: %d", len([]rune(text)))
	}
}

// Without Haus's public address an answer has no link (never an internal
// one); a card without an id has none either.
func TestContactReadWithoutLinks(t *testing.T) {
	s := newSetup(t, want{contacts: true, noPublic: true})
	s.fake.Put("/bote/kontakte/anna.vcf", annaCard)
	if text, _ := call(t, s.h, "kontakt_lesen", map[string]any{"kontakt": "anna"}); !strings.Contains(text, "Hauptstraße 1") || strings.Contains(text, "http") || strings.Contains(text, "/kontakte/") {
		t.Fatalf("no public address: %q", text)
	}
	s = newSetup(t, want{contacts: true})
	s.fake.Put("/bote/kontakte/o.vcf", "BEGIN:VCARD\r\nVERSION:4.0\r\nFN:Ohne Kennung\r\nTEL:0170 4\r\nEND:VCARD\r\n")
	if text, _ := call(t, s.h, "kontakt_lesen", map[string]any{"kontakt": "ohne"}); !strings.Contains(text, "0170 4") || strings.Contains(text, "http") {
		t.Fatalf("no id: %q", text)
	}
}

// A document's link is Haus's page only where that page leads somewhere:
// Haus's own address and Paperless's public one both set. Never
// Paperless's internal address.
func TestDocumentLinks(t *testing.T) {
	for _, c := range []struct {
		w    want
		link bool
	}{
		{want{docs: true}, true},
		{want{docs: true, noPublic: true}, false},
		{want{docs: true, noPaperlessPublic: true}, false},
	} {
		s := newSetup(t, c.w)
		s.docs.Add(paperlessfake.Doc{ID: 12, Title: "Rechnung März"})
		text, isErr := call(t, s.h, "dokumente_suchen", map[string]any{"text": "rechnung"})
		if isErr || strings.Contains(text, s.docs.URL) || strings.Contains(text, "paperless.example") {
			t.Fatalf("%+v: %q %v", c.w, text, isErr)
		}
		if got := strings.Contains(text, " — "+hausPublic+"/dokumente/12"); got != c.link {
			t.Fatalf("%+v: link %v: %q", c.w, got, text)
		}
		if !c.link && text != "Rechnung März" {
			t.Fatalf("%+v: %q", c.w, text)
		}
	}
}

// Many tags: five named, the rest counted; a tag cannot add a line.
func TestDocumentTagsBounded(t *testing.T) {
	s := newSetup(t, want{docs: true})
	var ids []int
	for i := 1; i <= 8; i++ {
		s.docs.Tag(i, fmt.Sprintf("T%d", i))
		ids = append(ids, i)
	}
	s.docs.Tag(1, "Eins\nZwei")
	s.docs.Add(paperlessfake.Doc{ID: 5, Title: "Brief", Tags: ids})
	text, _ := call(t, s.h, "dokumente_suchen", map[string]any{"text": "brief"})
	if text != "Brief · Schlagworte: Eins Zwei, T2, T3, T4, T5 (+3) — "+hausPublic+"/dokumente/5" {
		t.Fatalf("%q", text)
	}
}

// A backend that does not answer is a plain tool error, never a crash:
// which one, and to try again later; never the password, the token, an
// internal address or the transport's words (those go to Haus's log).
func TestBackendDown(t *testing.T) {
	s := newSetup(t, want{cal: true, contacts: true, docs: true})
	s.fake.Close()
	s.docs.Close()
	for _, c := range []struct{ tool, arg, says string }{
		{"kontakt_lesen", "kontakt", "Radicale"},
		{"kontakte_suchen", "text", "Radicale"},
		{"kontakt_anlegen", "name", "Radicale"},
		{"termine", "von", "Radicale"},
		{"dokumente_suchen", "text", "Paperless"},
	} {
		arg := "anna"
		if c.tool == "termine" {
			arg = "2026-10-15"
		}
		text, isErr := call(t, s.h, c.tool, map[string]any{c.arg: arg})
		if !isErr || !strings.Contains(text, c.says) || !strings.Contains(text, "später") {
			t.Fatalf("%s: %q %v", c.tool, text, isErr)
		}
		for _, never := range []string{plToken, "pw", "127.0.0.1", "http", "dial", "tcp", "refused", "\n"} {
			if strings.Contains(text, never) {
				t.Fatalf("%s says %q: %q", c.tool, never, text)
			}
		}
	}
	// an answer Haus did not expect stays as plain as before
	s = newSetup(t, want{docs: true})
	s.docs.Status = http.StatusInternalServerError
	if text, _ := call(t, s.h, "dokumente_suchen", map[string]any{"text": "x"}); text != "Paperless antwortet HTTP 500" {
		t.Fatalf("%q", text)
	}
}

func TestFromEnvPublicURL(t *testing.T) {
	for _, k := range []string{"HAUS_RADICALE_URL", "HAUS_NOTES_DIR", "HAUS_PAPERLESS_URL"} {
		t.Setenv(k, "")
	}
	for v, want := range map[string]string{"": "", "https://haus.example.org/": "https://haus.example.org", "http://haus.lan:8650": "http://haus.lan:8650",
		"haus.example.org": "", "javascript:alert(1)": ""} {
		t.Setenv("HAUS_PUBLIC_URL", v)
		if cfg, err := FromEnv(); err != nil || cfg.PublicURL != want {
			t.Fatalf("%q: %q %v", v, cfg.PublicURL, err)
		}
	}
}

// A read the address book cut short (too many cards, or too slow) says so
// when nothing was found, so "no such contact" is never claimed on half
// the cards.
func TestContactReadPartial(t *testing.T) {
	fake := davfake.New("bote", "pw")
	t.Cleanup(fake.Close)
	fake.AddAddressBook("/bote/kontakte/", "Haushalt")
	fake.Put("/bote/kontakte/a.vcf", "BEGIN:VCARD\r\nVERSION:4.0\r\nUID:a\r\nFN:Anna\r\nEND:VCARD\r\n")
	fake.Put("/bote/kontakte/b.vcf", "BEGIN:VCARD\r\nVERSION:4.0\r\nUID:b\r\nFN:Bert\r\nEND:VCARD\r\n")
	fake.StallAfter = 1
	dav := &caldav.Client{URL: fake.URL, User: "bote", Password: "pw", HTTP: &http.Client{Timeout: 300 * time.Millisecond}}
	h := Handler(Config{Token: tok, Loc: berlin(t), Contacts: dav})
	text, isErr := call(t, h, "kontakt_lesen", map[string]any{"kontakt": "niemand"})
	if isErr || !strings.HasPrefix(text, "Kein Kontakt passt") || !strings.Contains(text, "Nur ein Teil des Adressbuchs") {
		t.Fatalf("%q %v", text, isErr)
	}
}

// A tag without a name is no tag: not shown, not counted.
func TestDocumentBlankTags(t *testing.T) {
	s := newSetup(t, want{docs: true, noPublic: true})
	for i := 1; i <= 6; i++ {
		s.docs.Tag(i, fmt.Sprintf("T%d", i))
	}
	s.docs.Tag(9, "  ")
	s.docs.Add(paperlessfake.Doc{ID: 5, Title: "Brief", Tags: []int{9, 1, 2, 3, 4, 5}})
	if text, _ := call(t, s.h, "dokumente_suchen", map[string]any{"text": "brief"}); text != "Brief · Schlagworte: T1, T2, T3, T4, T5" {
		t.Fatalf("%q", text)
	}
}

// The link names the card's UID exactly as the page matches it, padding
// and all; the lookup reads the UID as the query is read, one line.
func TestContactLinkIsTheExactUID(t *testing.T) {
	s := newSetup(t, want{contacts: true})
	s.fake.Put("/bote/kontakte/p.vcf", "BEGIN:VCARD\r\nVERSION:4.0\r\nUID: ab  cd \r\nFN:Paula\r\nEND:VCARD\r\n")
	text, _ := call(t, s.h, "kontakt_lesen", map[string]any{"kontakt": "ab cd"})
	if !strings.Contains(text, "Paula") || !strings.Contains(text, hausPublic+"/kontakte/%20ab%20%20cd%20") {
		t.Fatalf("%q", text)
	}
}

// haus#13: notizen_finden searches the notes by meaning through the memory
// library: a note found by a word it does not contain, each with its
// title, its file, a short passage and its page on Haus. The answer names
// no tool and nothing technical (laib ADR 0021); nothing hidden or outside
// the folder.
func TestFindNotesByMeaning(t *testing.T) {
	s := newSetup(t, want{notes: true, library: true})
	write := func(name, body string, means ...string) {
		p := filepath.Join(s.notes, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(body), 0o644)
		s.qmd.Add(qmdfake.Doc{Path: name, Title: "qmd", Body: body, Means: means})
	}
	write("kaminkehrer.md", "# Kaminkehrer\n\nHerr Brandl kommt einmal im Jahr im Oktober.\nRechnung aufheben.\n", "schornsteinfeger")
	write("rezepte/Größen & Maße.md", "# Größen\n\nLena 31\n", "schornsteinfeger")
	write(".stversions/alt.md", "# Alt\n\nGEHEIM\n", "schornsteinfeger")
	text, isErr := call(t, s.h, "notizen_finden", map[string]any{"text": "Wann kommt der Schornsteinfeger?"})
	if isErr {
		t.Fatalf("%q", text)
	}
	for _, w := range []string{"## Kaminkehrer (kaminkehrer.md)", "Herr Brandl kommt einmal im Jahr im Oktober.",
		hausPublic + "/notizen/kaminkehrer.md", "## Größen (rezepte/Größen & Maße.md)", hausPublic + "/notizen/rezepte/Gr%C3%B6%C3%9Fen%20&%20Ma%C3%9Fe.md"} {
		if !strings.Contains(text, w) {
			t.Fatalf("lacks %q:\n%s", w, text)
		}
	}
	if strings.Index(text, "Kaminkehrer") > strings.Index(text, "Größen") {
		t.Fatalf("qmd's order: %s", text)
	}
	for _, never := range []string{"GEHEIM", "Alt", "qmd", "notizen_suchen", "notiz_lesen", "notizen_finden", "lex", "vec", "@@", "127.0.0.1", "Bedeutung geht"} {
		if strings.Contains(text, never) {
			t.Fatalf("says %q:\n%s", never, text)
		}
	}
	// without Haus's address: no link, the file still named
	s2 := newSetup(t, want{notes: true, library: true, noPublic: true})
	os.WriteFile(filepath.Join(s2.notes, "kaminkehrer.md"), []byte("# Kaminkehrer\n\nHerr Brandl\n"), 0o644)
	s2.qmd.Add(qmdfake.Doc{Path: "kaminkehrer.md", Title: "Kaminkehrer", Body: "# Kaminkehrer\n\nHerr Brandl\n", Means: []string{"schornsteinfeger"}})
	if text, _ := call(t, s2.h, "notizen_finden", map[string]any{"text": "Schornsteinfeger"}); !strings.Contains(text, "(kaminkehrer.md)") || strings.Contains(text, "http") || strings.Contains(text, "/notizen/") {
		t.Fatalf("no public URL: %q", text)
	}
	// a blank search asks back, as the word search does
	if text, isErr := call(t, s.h, "notizen_finden", map[string]any{"text": " "}); !isErr || text == "" {
		t.Fatalf("blank: %q %v", text, isErr)
	}
}

// When the library does not answer or is still indexing, notizen_finden
// is the word search and says so first, plainly; not a transport error,
// not empty while words match.
func TestFindNotesReduced(t *testing.T) {
	for _, set := range []func(*qmdfake.Server){
		func(f *qmdfake.Server) { f.Close() },
		func(f *qmdfake.Server) { f.Indexing = true },
		func(f *qmdfake.Server) { f.Status = 500 },
	} {
		s := newSetup(t, want{notes: true, library: true})
		os.WriteFile(filepath.Join(s.notes, "kaminkehrer.md"), []byte("# Kaminkehrer\n\nHerr Brandl kommt im Oktober.\n"), 0o644)
		s.qmd.Add(qmdfake.Doc{Path: "kaminkehrer.md", Title: "Kaminkehrer", Body: "# Kaminkehrer\n\nHerr Brandl kommt im Oktober.\n", Means: []string{"schornsteinfeger"}})
		set(s.qmd)
		text, isErr := call(t, s.h, "notizen_finden", map[string]any{"text": "Brandl"})
		if isErr || !strings.HasPrefix(text, reducedNote) || !strings.Contains(text, "## Kaminkehrer (kaminkehrer.md)") ||
			!strings.Contains(text, hausPublic+"/notizen/kaminkehrer.md") {
			t.Fatalf("%q %v", text, isErr)
		}
		text, isErr = call(t, s.h, "notizen_finden", map[string]any{"text": "Schornsteinfeger"})
		if isErr || !strings.HasPrefix(text, reducedNote) || !strings.Contains(text, "Keine Notiz enthält „Schornsteinfeger“") {
			t.Fatalf("no word hit: %q %v", text, isErr)
		}
		for _, never := range []string{"127.0.0.1", "http://127", "refused", "dial", "tcp", "qmd", "500", "notizen_suchen"} {
			if strings.Contains(text, never) {
				t.Fatalf("says %q: %q", never, text)
			}
		}
	}
}

func TestFromEnvLibrary(t *testing.T) {
	for _, k := range []string{"HAUS_RADICALE_URL", "HAUS_PAPERLESS_URL", "HAUS_QMD_URL", "HAUS_QMD_COLLECTION", "HAUS_LLM_URL", "HAUS_LLM_MODEL", "HAUS_LLM_KEY"} {
		t.Setenv(k, "")
	}
	t.Setenv("HAUS_NOTES_DIR", t.TempDir())
	if cfg, err := FromEnv(); err != nil || cfg.Library != nil {
		t.Fatalf("unset: %+v %v", cfg.Library, err)
	}
	t.Setenv("HAUS_QMD_URL", "127.0.0.1:8181")
	if _, err := FromEnv(); err == nil {
		t.Fatal("a malformed library address refuses to start")
	}
	t.Setenv("HAUS_QMD_URL", "http://127.0.0.1:8181/")
	cfg, err := FromEnv()
	if err != nil || cfg.Library == nil || cfg.Library.URL != "http://127.0.0.1:8181" || cfg.Library.Collection != "notizen" {
		t.Fatalf("set: %+v %v", cfg.Library, err)
	}
	t.Setenv("HAUS_QMD_COLLECTION", "haus-notizen")
	if cfg, _ := FromEnv(); cfg.Library == nil || cfg.Library.Collection != "haus-notizen" || cfg.Judge != nil {
		t.Fatalf("collection: %+v %+v", cfg.Library, cfg.Judge)
	}
	// the house model, when the box has the model role
	t.Setenv("HAUS_LLM_URL", "http://host.containers.internal:11435/v1")
	t.Setenv("HAUS_LLM_MODEL", "gemma-4-e4b")
	t.Setenv("HAUS_LLM_KEY", "model-key")
	if cfg, err := FromEnv(); err != nil || cfg.Judge == nil || cfg.Judge.Model != "gemma-4-e4b" {
		t.Fatalf("house model: %+v %v", cfg.Judge, err)
	}
	// one of the three missing: no tidy-up, and Haus still starts
	t.Setenv("HAUS_LLM_KEY", "")
	if cfg, err := FromEnv(); err != nil || cfg.Judge != nil || cfg.Library == nil {
		t.Fatalf("no key: %+v %v", cfg.Judge, err)
	}
	// the library maps its hits to the notes folder: without one, no library
	t.Setenv("HAUS_NOTES_DIR", "")
	if cfg, err := FromEnv(); err != nil || cfg.Library != nil {
		t.Fatalf("no notes: %+v %v", cfg.Library, err)
	}
}

// The Haushalt's passages are plain the same way the page's are: a table
// row as its cells, no header row, no pipes, no list dashes (haus#13).
func TestFindNotesPlainPassages(t *testing.T) {
	s := newSetup(t, want{notes: true, library: true})
	table := "# Handwerker\n\n| Gewerk | Firma | Telefon | Notiz |\n|---|---|---|---|\n| Heizung | Huber GmbH | 089 111 | Wartung |\n"
	list := "# Putzplan\n\n- Bad: Anna, samstags\n- Küche: Ben\n"
	os.WriteFile(filepath.Join(s.notes, "handwerker.md"), []byte(table), 0o644)
	os.WriteFile(filepath.Join(s.notes, "putzplan.md"), []byte(list), 0o644)
	s.qmd.Add(qmdfake.Doc{Path: "handwerker.md", Title: "Handwerker", Body: table, Means: []string{"haus"}},
		qmdfake.Doc{Path: "putzplan.md", Title: "Putzplan", Body: list, Means: []string{"haus"}})
	text, isErr := call(t, s.h, "notizen_finden", map[string]any{"text": "Haus"})
	if isErr || !strings.Contains(text, "Gewerk Heizung · Firma Huber GmbH · Telefon 089 111 · Notiz Wartung") || !strings.Contains(text, "\nBad: Anna, samstags\n") {
		t.Fatalf("%q", text)
	}
	for _, never := range []string{"|", "Gewerk · Firma", "Gewerk Firma", "- Bad", "- Küche", "---"} {
		if strings.Contains(text, never) {
			t.Fatalf("says %q:\n%s", never, text)
		}
	}
}

// notizen_finden uses the cut and the tidy-up as the page does (haus#13):
// a note far from the question is not named, and the house model narrows
// what is left. The Bote waits for its answer anyway, so the tidy-up has
// longer than the page's budget.
func TestFindNotesCutAndTidy(t *testing.T) {
	s := newSetup(t, want{notes: true, library: true, llm: true})
	write := func(name, body string, vec float64) {
		os.WriteFile(filepath.Join(s.notes, name), []byte(body), 0o644)
		s.qmd.Add(qmdfake.Doc{Path: name, Title: "qmd", Body: body, Means: []string{"schornsteinfeger"}, Vec: vec})
	}
	write("kaminkehrer.md", "# Kaminkehrer\n\nHerr Brandl kommt im Oktober.\n", 0.54)
	write("heizung.md", "# Gastherme\n\nWartung im September.\n", 0.45)
	write("dachrinne.md", "# Dachrinne\n\nIm November reinigen.\n", 0.31)
	s.llm.Content = `{"ids":[1]}`
	s.llm.Hang = library.PageTidy + 300*time.Millisecond
	text, isErr := call(t, s.h, "notizen_finden", map[string]any{"text": "Wann kommt der Schornsteinfeger?"})
	if isErr || !strings.Contains(text, "## Kaminkehrer (kaminkehrer.md)") || strings.Contains(text, "Gastherme") || strings.Contains(text, "Dachrinne") {
		t.Fatalf("%q", text)
	}
	// the house model not loaded: the cut's two, never the far one
	s.llm.Lease.Alias = "gemma-4-12b"
	text, _ = call(t, s.h, "notizen_finden", map[string]any{"text": "Wann kommt der Schornsteinfeger?"})
	if !strings.Contains(text, "(kaminkehrer.md)") || !strings.Contains(text, "(heizung.md)") || strings.Contains(text, "Dachrinne") {
		t.Fatalf("not loaded: %q", text)
	}
	for _, never := range []string{"gemma", "Modell", "Bedeutung geht"} {
		if strings.Contains(text, never) {
			t.Fatalf("says %q: %q", never, text)
		}
	}
	for _, r := range s.llm.Seen() {
		if r.Path == "/api/model-lease" && r.Method != http.MethodGet {
			t.Fatalf("the Haushalt took the lease: %s", r.Method)
		}
	}
}

// When the library answered but no note is close, notizen_finden says
// that plainly — not that the search is down — and names what the words
// find as such; with no word hit, only that (haus#13).
func TestFindNotesNothingClose(t *testing.T) {
	s := newSetup(t, want{notes: true, library: true})
	body := "# Garten\n\nIm März das Hochbeet vorbereiten.\n"
	os.WriteFile(filepath.Join(s.notes, "garten.md"), []byte(body), 0o644)
	s.qmd.Add(qmdfake.Doc{Path: "garten.md", Title: "Garten", Body: body, Means: []string{"everest", "gipfel"}, Vec: 0.17})
	text, isErr := call(t, s.h, "notizen_finden", map[string]any{"text": "Wie hoch ist der Mount Everest?"})
	if isErr || !strings.HasPrefix(text, "Keine Notiz passt wirklich zu „Wie hoch ist der Mount Everest?“.") ||
		!strings.Contains(text, "Notizen mit Wörtern aus der Frage:") || !strings.Contains(text, "## Garten (garten.md)") {
		t.Fatalf("%q %v", text, isErr)
	}
	if strings.Index(text, "Notizen mit Wörtern") > strings.Index(text, "## Garten") {
		t.Fatalf("the label after the hit: %q", text)
	}
	text, isErr = call(t, s.h, "notizen_finden", map[string]any{"text": "Gipfel"})
	if isErr || text != "Keine Notiz passt wirklich zu „Gipfel“." {
		t.Fatalf("no word hit: %q %v", text, isErr)
	}
	for _, q := range []string{"Wie hoch ist der Mount Everest?", "Gipfel"} {
		text, _ := call(t, s.h, "notizen_finden", map[string]any{"text": q})
		for _, never := range []string{reducedNote, "Bedeutung", "geht gerade nicht", "Keine Notiz enthält", "qmd"} {
			if strings.Contains(text, never) {
				t.Fatalf("%s: says %q: %q", q, never, text)
			}
		}
	}
}

// haus#13: with the documents in the library, notizen_finden finds notes
// and documents in one search: a document with its title, date and
// sender as Paperless has them, the passage that matched, and its page on
// Haus. The tool says it covers documents.
func TestFindNotesAndDocuments(t *testing.T) {
	s := newSetup(t, want{notes: true, library: true, docs: true, documents: true})
	os.WriteFile(filepath.Join(s.notes, "kaminkehrer.md"), []byte("# Kaminkehrer\n\nHerr Brandl kommt im Oktober.\n"), 0o644)
	s.qmd.Add(qmdfake.Doc{Path: "kaminkehrer.md", Title: "Kaminkehrer", Body: "# Kaminkehrer\n\nHerr Brandl kommt im Oktober.\n", Means: []string{"termin"}, Vec: 0.5},
		qmdfake.Doc{Collection: "dokumente", Path: "7.md", Title: "Stromvertrag", Body: "Vertragslaufzeit bis 31.12.2027.\n# Stromvertrag\nDatum: 14.03.2025",
			Means: []string{"termin"}, Vec: 0.48})
	s.docs.Correspondent(3, "Stadtwerke Musterstadt")
	s.docs.Add(paperlessfake.Doc{ID: 7, Title: "Stromvertrag", Content: "Vertragslaufzeit bis 31.12.2027.", Correspondent: 3, Created: "2025-03-14"})
	text, isErr := call(t, s.h, "notizen_finden", map[string]any{"text": "Termin"})
	want := "## Kaminkehrer (kaminkehrer.md)\nHerr Brandl kommt im Oktober.\nhttps://haus.example/notizen/kaminkehrer.md\n\n" +
		"## Dokument: Stromvertrag (14.03.2025, Stadtwerke Musterstadt)\nVertragslaufzeit bis 31.12.2027.\nhttps://haus.example/dokumente/7"
	if isErr || text != want {
		t.Fatalf("%q\nwant\n%q", text, want)
	}
	_, out := rpc(t, s.h, tok, "tools/list", nil)
	if b, _ := json.Marshal(out); !strings.Contains(string(b), "Findet Notizen und Dokumente der Familie") {
		t.Fatalf("description: %s", b)
	}
	// nothing at all: said for both
	if text, _ := call(t, s.h, "notizen_finden", map[string]any{"text": "Quantenchromodynamik"}); !strings.HasPrefix(text, "Keine Notiz und kein Dokument passt") {
		t.Fatalf("%q", text)
	}
	// Paperless down: the notes, and one line first that the documents did not answer
	s.docs.Close()
	text, isErr = call(t, s.h, "notizen_finden", map[string]any{"text": "Termin"})
	if isErr || !strings.HasPrefix(text, "Die Dokumente antworten gerade nicht; gesucht wurde nur in den Notizen.\n\n## Kaminkehrer") || strings.Contains(text, "Stromvertrag") {
		t.Fatalf("down: %q", text)
	}
	if strings.Contains(text, s.docs.URL) || strings.Contains(text, "Paperless") || strings.Contains(text, plToken) {
		t.Fatalf("leaks: %q", text)
	}
}

// HAUS_QMD_DOCUMENTS names the documents' collection; it counts only with
// Paperless set, and it is never the notes' own.
func TestFromEnvDocumentsCollection(t *testing.T) {
	t.Setenv("HAUS_NOTES_DIR", t.TempDir())
	t.Setenv("HAUS_QMD_URL", "http://127.0.0.1:8181")
	t.Setenv("HAUS_QMD_DOCUMENTS", "dokumente")
	if cfg, err := FromEnv(); err != nil || cfg.Library == nil || cfg.Library.Documents != "" {
		t.Fatalf("without Paperless: %+v %v", cfg.Library, err)
	}
	t.Setenv("HAUS_PAPERLESS_URL", "http://host.containers.internal:8000")
	t.Setenv("HAUS_PAPERLESS_TOKEN", "pl")
	if cfg, err := FromEnv(); err != nil || cfg.Library == nil || cfg.Library.Documents != "dokumente" {
		t.Fatalf("with Paperless: %+v %v", cfg.Library, err)
	}
	t.Setenv("HAUS_QMD_DOCUMENTS", "notizen")
	if _, err := FromEnv(); err == nil {
		t.Fatal("the notes' collection taken for the documents'")
	}
}

// haus#116: Joshua files a note with its kind and tags, finds the notes
// by tag, and is told which tags there are; a search hit says its kind
// and tags.
func TestNoteToolsWithTags(t *testing.T) {
	s := newSetup(t, want{notes: true})
	text, isErr := call(t, s.h, "notiz_anlegen", map[string]any{"titel": "Lasagne", "text": "Nudeln, Tomaten", "art": "rezept", "tags": "Kochen, italienisch"})
	if isErr || !strings.Contains(text, "lasagne.md") {
		t.Fatalf("%q", text)
	}
	if b, _ := os.ReadFile(filepath.Join(s.notes, "lasagne.md")); !strings.HasPrefix(string(b), "---\nart: rezept\ntags: [kochen, italienisch]\n---\n") {
		t.Fatalf("file %q", b)
	}
	call(t, s.h, "notiz_anlegen", map[string]any{"titel": "Rom", "text": "Kolosseum", "art": "reise", "tags": "reisen, italienisch"})
	if text, _ := call(t, s.h, "notizen_mit_tag", map[string]any{"tags": "kochen"}); !strings.Contains(text, "- Lasagne (lasagne.md) · Rezept #kochen #italienisch") || strings.Contains(text, "Rom") {
		t.Fatalf("by tag: %q", text)
	}
	if text, _ := call(t, s.h, "notizen_mit_tag", map[string]any{}); !strings.Contains(text, "#italienisch (2)") {
		t.Fatalf("tags: %q", text)
	}
	if text, _ := call(t, s.h, "notizen_mit_tag", map[string]any{"tags": "garten"}); !strings.Contains(text, "Keine Notiz trägt #garten") {
		t.Fatalf("none: %q", text)
	}
	if text, _ := call(t, s.h, "notizen_suchen", map[string]any{"text": "Kolosseum"}); !strings.Contains(text, "## Rom (rom.md) · Reise #reisen #italienisch") {
		t.Fatalf("search: %q", text)
	}
}

// haus#117: a link sent to the Bote becomes a note: what was kept in a
// line, and the note's page in Haus; the same link again is said so.
func TestNotizAusLink(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><head><script type="application/ld+json">{"@type":"Recipe","name":"Apfelkuchen","recipeIngredient":["4 Äpfel"],"recipeInstructions":["Backen."]}</script></head></html>`))
	}))
	defer site.Close()
	s := newSetup(t, want{notes: true})
	text, isErr := call(t, s.h, "notiz_aus_link", map[string]any{"link": site.URL + "/kuchen", "hinweis": "#sonntag"})
	if isErr || !strings.Contains(text, "Rezept gemerkt: Apfelkuchen, 1 Zutat, 1 Schritt.") || !strings.Contains(text, "In Haus: "+hausPublic+"/notizen/apfelkuchen.md") {
		t.Fatalf("%q", text)
	}
	if text, _ := call(t, s.h, "notiz_aus_link", map[string]any{"link": site.URL + "/kuchen"}); !strings.Contains(text, "schon gemerkt") {
		t.Fatalf("twice: %q", text)
	}
}

// haus#123: a podcast link sent to the Bote subscribes the person who
// sent it; told what was subscribed.
func TestPodcastAbonnieren(t *testing.T) {
	fake := absfake.New("k")
	defer fake.Close()
	feeds := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Write([]byte(`<?xml version="1.0"?><rss><channel><title>x</title></channel></rss>`))
	}))
	defer feeds.Close()
	fake.AddFeed(feeds.URL+"/lage", "Lage der Nation")
	svc := podcasts.Open(&abs.Client{URL: fake.URL, Token: "k"}, t.TempDir())
	svc.Fetch = merken.Fetcher{AllowPrivate: true}
	h := Handler(Config{Token: tok, Loc: berlin(t), Podcasts: svc})
	text, isErr := call(t, h, "podcast_abonnieren", map[string]any{"link": feeds.URL + "/lage", "person": "anna"})
	if isErr || !strings.HasPrefix(text, "Abonniert: „Lage der Nation“.") {
		t.Fatalf("%q", text)
	}
	if mine, _ := svc.Meine("anna"); len(mine) != 1 {
		t.Fatalf("%v", mine)
	}
}

// haus#118: "Lasagne und Curry für 4 auf die Einkaufsliste", "Milch auf
// die Einkaufsliste" — the same thing added up, the recipe as its origin;
// a recipe not kept said so; the list read back.
func TestEinkaufsliste(t *testing.T) {
	dav := davfake.New("haus", "pw")
	defer dav.Close()
	l := einkauf.Open(&caldav.Client{URL: dav.URL, User: "haus", Password: "pw"})
	if err := l.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "lasagne.md"), []byte("---\nart: rezept\n---\n# Lasagne\n\n*Für 4 Portionen*\n\n## Zutaten\n\n- 2 Zwiebeln\n- 200 g Mehl\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "curry.md"), []byte("---\nart: rezept\n---\n# Rotes Curry\n\n*Für 2 Portionen*\n\n## Zutaten\n\n- 1 Zwiebel\n"), 0o644)
	nd, err := notes.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	h := Handler(Config{Token: tok, Loc: berlin(t), Einkauf: l, Notes: nd})
	text, isErr := call(t, h, "einkaufsliste_dazu", map[string]any{"rezepte": "Lasagne und das Curry", "portionen": "4", "dinge": "Milch"})
	if isErr || !strings.HasPrefix(text, "Auf der Liste: 3 neu, 0 dazugezählt. Aus den Rezepten: Lasagne, Rotes Curry.") || !strings.Contains(text, "- 4 Zwiebeln") {
		t.Fatalf("%q", text)
	}
	text, isErr = call(t, h, "einkaufsliste_dazu", map[string]any{"dinge": "1 l Milch", "rezepte": "Pizza"})
	if isErr || !strings.Contains(text, "0 neu, 1 dazugezählt") || !strings.Contains(text, "Nicht gefunden: Pizza") {
		t.Fatalf("%q", text)
	}
	if text, isErr = call(t, h, "einkaufsliste_dazu", map[string]any{"rezepte": "Pizza"}); !isErr || !strings.Contains(text, "Kein gemerktes Rezept heißt „Pizza“") {
		t.Errorf("only a missing recipe: %q", text)
	}
	// "auf meine Liste": the person's own, made on first use, the household's untouched
	dav.AddPrincipal("anna")
	text, isErr = call(t, h, "einkaufsliste_dazu", map[string]any{"dinge": "Socken", "liste": "meine", "person": "anna"})
	if isErr || !strings.HasPrefix(text, "Neue eigene Liste „Meine Liste“ angelegt. Auf der Liste: 1 neu") {
		t.Fatalf("%q", text)
	}
	if text, _ = call(t, h, "einkaufsliste", map[string]any{"liste": "meine", "person": "anna"}); text != "Auf der Liste:\n- Socken\n" {
		t.Errorf("%q", text)
	}
	text, _ = call(t, h, "einkaufsliste", map[string]any{})
	if text != "Auf der Liste:\n- 1 l Milch\n- 4 Zwiebeln (Lasagne, Rotes Curry)\n- 200 g Mehl (Lasagne)\n" {
		t.Errorf("%q", text)
	}
}

// haus#139: "Wann läuft die Kfz-Versicherung aus?" is answered from the
// contract fields, with the document's link; a contract without fields is
// asked through the document chat, and without that said so; never an
// answer without a document.
func TestVertraegeFinden(t *testing.T) {
	f := paperlessfake.New("tok")
	defer f.Close()
	f.DocType(1, "Versicherung")
	f.Tag(9, "prüfen")
	f.Field(11, "Anbieter", "string")
	f.Field(12, "Nächster Kündigungstermin", "date")
	f.Field(13, "Kündigungsfrist", "string")
	f.Field(14, "Betrag", "monetary")
	f.Field(15, "Zahlungsintervall", "string")
	f.Add(paperlessfake.Doc{ID: 1, Title: "Kfz-Versicherung 2026", DocumentType: 1,
		Fields: map[int]any{11: "HUK-Coburg", 12: "2027-01-01", 13: "1 Monat", 14: "EUR84.50", 15: "monatlich"}},
		paperlessfake.Doc{ID: 2, Title: "Hausratversicherung", DocumentType: 1, Tags: []int{9}})
	h := Handler(Config{Token: tok, Loc: berlin(t), Documents: &paperless.Client{URL: f.URL, Token: "tok", PublicURL: "https://paperless.box"}})
	text, isErr := call(t, h, "vertraege_finden", map[string]any{"frage": "Wann läuft die Kfz-Versicherung aus?"})
	want := "Kfz-Versicherung 2026 (Versicherung)\n- Anbieter: HUK-Coburg\n- Kündigungsfrist: 1 Monat\n- Nächster Kündigungstermin: 01.01.2027\n" +
		"- Kündigen bis spätestens: 01.12.2026\n- Betrag: 84,50 € monatlich\nDokument: https://paperless.box/documents/1/details"
	if isErr || text != want {
		t.Fatalf("%q", text)
	}
	text, _ = call(t, h, "vertraege_finden", map[string]any{"frage": "Was kostet die Hausratversicherung?"})
	if !strings.Contains(text, "keine Vertragsdaten eingetragen; die Antwort steht nur im Dokument selbst") || !strings.Contains(text, "Unsicher") || !strings.Contains(text, "/documents/2/details") {
		t.Errorf("no fields, no chat: %q", text)
	}
	f.ChatAnswer = "Der Beitrag beträgt 12,40 € im Monat."
	if text, _ = call(t, h, "vertraege_finden", map[string]any{"frage": "Was kostet die Hausratversicherung?"}); !strings.Contains(text, "Paperless liest im Dokument: Der Beitrag beträgt 12,40 € im Monat.") || !strings.Contains(text, "/documents/2/details") {
		t.Errorf("the chat: %q", text)
	}
	if text, _ = call(t, h, "vertraege_finden", map[string]any{"frage": "Wann endet der Handyvertrag?"}); !strings.HasPrefix(text, "Keinen Vertrag gefunden") {
		t.Errorf("none: %q", text)
	}
}
