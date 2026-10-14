package haushalt

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/haus/internal/caldav"
	"github.com/mdopp/haus/internal/caldav/davfake"
	"github.com/mdopp/haus/internal/notes"
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
}

func newSetup(t *testing.T, calendar, withNotes bool) setup {
	t.Helper()
	s := setup{}
	cfg := Config{Token: tok, Loc: berlin(t), Version: "test"}
	if calendar {
		s.fake = davfake.New("bote", "pw")
		t.Cleanup(s.fake.Close)
		s.fake.AddCalendar("/bote/familie/", "Familie")
		cfg.Calendar = &caldav.Client{URL: s.fake.URL, User: "bote", Password: "pw", Loc: cfg.Loc}
	}
	if withNotes {
		s.notes = t.TempDir()
		d, err := notes.Open(s.notes)
		if err != nil {
			t.Fatal(err)
		}
		cfg.Notes = d
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
	s := newSetup(t, false, true)
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
	s := newSetup(t, true, true)
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
	not := []string{"notiz_anlegen", "notiz_lesen", "notizen_suchen"}
	both := append(append([]string{}, cal...), not...)
	sort.Strings(both)
	for _, c := range []struct {
		cal, notes bool
		want       []string
	}{{false, false, nil}, {true, false, cal}, {false, true, not}, {true, true, both}} {
		got := toolNames(t, newSetup(t, c.cal, c.notes).h)
		if strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Fatalf("calendar %v notes %v: %v", c.cal, c.notes, got)
		}
	}
}

func TestFromEnv(t *testing.T) {
	for _, k := range []string{"HAUS_HAUSHALT_TOKEN", "HAUS_RADICALE_URL", "HAUS_RADICALE_USER", "HAUS_RADICALE_PASSWORD", "HAUS_RADICALE_CALENDAR", "HAUS_NOTES_DIR"} {
		t.Setenv(k, "")
	}
	t.Setenv("TZ", "Europe/Berlin")
	cfg, err := FromEnv()
	if err != nil || cfg.Calendar != nil || cfg.Notes != nil || cfg.Token != "" || cfg.Loc.String() != "Europe/Berlin" {
		t.Fatalf("unset: %+v %v", cfg, err)
	}
	t.Setenv("HAUS_RADICALE_URL", "radicale:5232")
	if _, err := FromEnv(); err == nil {
		t.Fatal("a malformed Radicale address refuses to start")
	}
	t.Setenv("HAUS_RADICALE_URL", "http://host.containers.internal:5232/")
	if cfg, err := FromEnv(); err != nil || cfg.Calendar != nil {
		t.Fatalf("an address without the account is off, not an error: %+v %v", cfg, err)
	}
	t.Setenv("HAUS_RADICALE_USER", "bote")
	t.Setenv("HAUS_RADICALE_PASSWORD", "pw")
	t.Setenv("HAUS_RADICALE_CALENDAR", "/familie/kalender/")
	t.Setenv("HAUS_NOTES_DIR", filepath.Join(t.TempDir(), "fehlt"))
	cfg, err = FromEnv()
	if err != nil || cfg.Calendar == nil || cfg.Calendar.URL != "http://host.containers.internal:5232" || cfg.Calendar.User != "bote" || cfg.Calendar.Calendar != "/familie/kalender/" {
		t.Fatalf("calendar: %+v %v", cfg.Calendar, err)
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
	s := newSetup(t, true, false)
	text, isErr := call(t, s.h, "termin_anlegen", map[string]any{"titel": "Zahnarzt", "beginn": "2026-10-14T15:00", "ort": "Praxis, Hauptstr. 1"})
	if isErr || !strings.Contains(text, "Familie") || !strings.Contains(text, "Mi 14.10.2026 15:00–16:00: Zahnarzt (Praxis, Hauptstr. 1)") {
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
}

// A note is a Markdown file in the share; search and read go through the
// same tools; no name leaves the directory.
func TestNoteTools(t *testing.T) {
	s := newSetup(t, false, true)
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
