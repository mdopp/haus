package napi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/haus/internal/caldav"
	"github.com/mdopp/haus/internal/caldav/davfake"
	"github.com/mdopp/haus/internal/haushalt"
	"github.com/mdopp/haus/internal/laib"
	"github.com/mdopp/haus/internal/notes"
	"github.com/mdopp/haus/internal/paperless"
	"github.com/mdopp/haus/internal/paperless/paperlessfake"
)

// haus#10, slice 2: the tool widgets (the app's ToolDefs.kt, ToolCells.kt,
// ToolWidgetProvider.kt). Haus lists a tool only when its Haushalt backend is
// set; each def names the row path the app then calls; the one row action is
// ticking a reminder off. With no backend set, the tool paths are 404.

type tools struct {
	h     http.Handler
	dav   *davfake.Server
	docs  *paperlessfake.Server
	notes string
}

type which struct{ cal, contacts, docs, notes bool }

func berlinLoc(t *testing.T) *time.Location {
	t.Helper()
	l, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	return l
}

const (
	openTodo = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//phone//DE\r\nBEGIN:VTODO\r\nUID:milch-1\r\nDTSTAMP:20261010T080000Z\r\n" +
		"SUMMARY:Milch kaufen\r\nDUE:20261015T160000Z\r\nSTATUS:NEEDS-ACTION\r\nBEGIN:VALARM\r\nACTION:DISPLAY\r\nDESCRIPTION:Milch\r\n" +
		"TRIGGER;VALUE=DATE-TIME:20261015T160000Z\r\nEND:VALARM\r\nEND:VTODO\r\nEND:VCALENDAR\r\n"
	laterTodo = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VTODO\r\nUID:reifen-2\r\nDTSTAMP:20261010T080000Z\r\nSUMMARY:Reifen wechseln\r\n" +
		"DUE;VALUE=DATE:20261101\r\nEND:VTODO\r\nEND:VCALENDAR\r\n"
	noDueTodo = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VTODO\r\nUID:irgendwann-3\r\nDTSTAMP:20261010T080000Z\r\nSUMMARY:Keller aufräumen\r\n" +
		"END:VTODO\r\nEND:VCALENDAR\r\n"
	doneTodo = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VTODO\r\nUID:fertig-4\r\nDTSTAMP:20261010T080000Z\r\nSUMMARY:Schon erledigt\r\n" +
		"STATUS:COMPLETED\r\nCOMPLETED:20261011T080000Z\r\nEND:VTODO\r\nEND:VCALENDAR\r\n"
	weeklyTodo = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VTODO\r\nUID:muell-5\r\nDTSTAMP:20261010T080000Z\r\nSUMMARY:Müll rausbringen\r\n" +
		"DUE:20261016T050000Z\r\nRRULE:FREQ=WEEKLY\r\nEND:VTODO\r\nEND:VCALENDAR\r\n"
)

func toolsWith(t *testing.T, w which) tools {
	t.Helper()
	var calls []string
	lb := fakeLaib(t, &calls)
	t.Cleanup(lb.Close)
	s := tools{}
	hh := haushalt.Config{Loc: berlinLoc(t)}
	if w.cal || w.contacts {
		s.dav = davfake.New("bote", "pw")
		t.Cleanup(s.dav.Close)
		dav := &caldav.Client{URL: s.dav.URL, User: "bote", Password: "pw", Loc: hh.Loc}
		if w.cal {
			s.dav.AddCalendar("/bote/familie/", "Haushalt", "VEVENT", "VTODO")
			s.dav.Put("/bote/familie/milch-1.ics", openTodo)
			s.dav.Put("/bote/familie/reifen-2.ics", laterTodo)
			s.dav.Put("/bote/familie/irgendwann-3.ics", noDueTodo)
			s.dav.Put("/bote/familie/fertig-4.ics", doneTodo)
			s.dav.Put("/bote/familie/muell-5.ics", weeklyTodo)
			hh.Calendar = dav
		}
		if w.contacts {
			s.dav.AddAddressBook("/bote/kontakte/", "Haushalt")
			s.dav.Put("/bote/kontakte/b.vcf", "BEGIN:VCARD\r\nVERSION:4.0\r\nUID:urn:uuid:b\r\nFN:Bernd Bauer\r\nTEL:0170 1234567\r\nTEL:0711 999\r\nEMAIL:bernd@example.org\r\nEND:VCARD\r\n")
			s.dav.Put("/bote/kontakte/a.vcf", "BEGIN:VCARD\r\nVERSION:4.0\r\nUID:urn:uuid:a\r\nFN:anna Arndt\r\nBDAY:--0314\r\nEND:VCARD\r\n")
			hh.Contacts = dav
		}
	}
	if w.docs {
		s.docs = paperlessfake.New("pl-token")
		t.Cleanup(s.docs.Close)
		s.docs.Correspondent(3, "Stadtwerke")
		s.docs.Add(paperlessfake.Doc{ID: 1, Title: "Mietvertrag", Created: "2024-01-02"},
			paperlessfake.Doc{ID: 2, Title: "Stromrechnung", Correspondent: 3, Created: "2026-09-30"})
		hh.Documents = &paperless.Client{URL: s.docs.URL, Token: "pl-token", PublicURL: "https://paperless.example"}
	}
	if w.notes {
		s.notes = t.TempDir()
		for i, n := range []struct{ name, body string }{{"einkauf.md", "# Einkauf\n\nMilch"}, {"urlaub.md", "# Urlaub 2027\n\nIrgendwo"}, {".geheim.md", "# Versteckt"}} {
			p := filepath.Join(s.notes, n.name)
			if err := os.WriteFile(p, []byte(n.body), 0o644); err != nil {
				t.Fatal(err)
			}
			at := time.Date(2026, 10, 1+i, 12, 0, 0, 0, time.UTC)
			os.Chtimes(p, at, at)
		}
		d, err := notes.Open(s.notes)
		if err != nil {
			t.Fatal(err)
		}
		hh.Notes = d
	}
	s.h = Handler(Config{Laib: &laib.Client{URL: lb.URL, Token: "ops"}, Validate: StaticValidator([]string{"dev1"}), Haushalt: hh})
	return s
}

func allTools(t *testing.T) tools {
	return toolsWith(t, which{cal: true, contacts: true, docs: true, notes: true})
}

// noNull walks a decoded JSON value: Android's optString reads null as "null".
func noNull(t *testing.T, where string, v any) {
	t.Helper()
	switch x := v.(type) {
	case nil:
		t.Fatalf("%s: a JSON null went out", where)
	case map[string]any:
		for k, e := range x {
			noNull(t, where+"."+k, e)
		}
	case []any:
		for i, e := range x {
			noNull(t, where+"["+string(rune('0'+i%10))+"]", e)
		}
	}
}

func decode(t *testing.T, body string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("not JSON: %s", body)
	}
	noNull(t, "body", out)
	return out
}

func TestToolPathsWantAPairedDevice(t *testing.T) {
	s := allTools(t)
	for _, p := range []struct{ method, path, body string }{
		{"GET", "/napi/defs/tool", ""},
		{"GET", "/napi/portal/tasks", ""},
		{"GET", "/napi/portal/contacts", ""},
		{"GET", "/napi/portal/documents", ""},
		{"GET", "/napi/portal/notes", ""},
		{"POST", "/napi/action-callback", `{"action_id":"task.set_status","params":{"entity_id":"milch-1","status":"done"}}`},
	} {
		for _, bearer := range []string{"", "wrong"} {
			if code, _ := raw(t, s.h, p.method, p.path, bearer, p.body); code != 401 {
				t.Errorf("%s %s with %q → %d, want 401", p.method, p.path, bearer, code)
			}
		}
	}
	if !strings.Contains(s.dav.Objects()["/bote/familie/milch-1.ics"], "NEEDS-ACTION") {
		t.Fatal("a reminder changed without a token")
	}
}

// The catalog in the shape ToolDefs.parseCatalog reads: every def listable
// (an api path under /napi, a title field), ids the placed widgets already
// carry (task, contacts, doc, note), no compose or item route Haus has no page for.
func TestToolCatalog(t *testing.T) {
	s := allTools(t)
	code, body := raw(t, s.h, "GET", "/napi/defs/tool", "dev1", "")
	if code != 200 {
		t.Fatalf("defs: %d %s", code, body)
	}
	out := decode(t, body)
	if out["ok"] != true || out["kind"] != "tool" {
		t.Fatalf("envelope: %s", body)
	}
	defs, _ := out["defs"].([]any)
	byID := map[string]map[string]any{}
	var ids []string
	for _, d := range defs {
		m := d.(map[string]any)
		id, _ := m["tool-id"].(string)
		ids = append(ids, id)
		byID[id] = m
	}
	if strings.Join(ids, ",") != "contacts,doc,note,task" {
		t.Fatalf("tools, by id: %v", ids)
	}
	paths := map[string]string{"task": "/napi/portal/tasks", "contacts": "/napi/portal/contacts", "doc": "/napi/portal/documents", "note": "/napi/portal/notes"}
	for id, d := range byID {
		if d["tool-api-path"] != paths[id] {
			t.Errorf("%s: tool-api-path %v, want %s", id, d["tool-api-path"], paths[id])
		}
		if l, _ := d["tool-label"].(string); l == "" {
			t.Errorf("%s: no label", id)
		}
		schema, _ := d["tool-cell-schema"].(map[string]any)
		if title, _ := schema["title"].(string); title == "" {
			t.Errorf("%s: a schema without title is not listable: %v", id, schema)
		}
		for _, k := range []string{"tool-compose-path", "tool-item-id-field"} {
			if _, ok := d[k]; ok {
				t.Errorf("%s: %s names a page Haus does not have", id, k)
			}
		}
		if id != "task" {
			if a, _ := d["tool-actions"].([]any); len(a) != 0 {
				t.Errorf("%s: only the reminder has a row action: %v", id, a)
			}
		}
	}
	task := byID["task"]
	if a, _ := task["tool-actions"].([]any); len(a) != 1 || a[0] != "task.set_status" {
		t.Fatalf("task actions: %v", task["tool-actions"])
	}
	if a, _ := task["tool-cell-schema"].(map[string]any)["actions"].([]any); len(a) != 1 || a[0] != "task.set_status" {
		t.Fatalf("the row offers the action: %v", task["tool-cell-schema"])
	}
	params, _ := task["tool-action-params"].(map[string]any)["task.set_status"].(map[string]any)
	if params["entity_id"] != "$id" || params["status"] != "done" {
		t.Fatalf("params: entity_id from the row's id, status the literal done: %v", task["tool-action-params"])
	}
	titled, _ := task["tool-actions-titled"].([]any)
	if len(titled) != 1 || titled[0].(map[string]any)["id"] != "task.set_status" || titled[0].(map[string]any)["title"] == "" {
		t.Fatalf("titled: %v", titled)
	}
}

func TestToolCatalogListsOnlyWhatIsSet(t *testing.T) {
	s := toolsWith(t, which{notes: true})
	code, body := raw(t, s.h, "GET", "/napi/defs/tool", "dev1", "")
	out := decode(t, body)
	defs, _ := out["defs"].([]any)
	if code != 200 || len(defs) != 1 || defs[0].(map[string]any)["tool-id"] != "note" {
		t.Fatalf("notes only: %d %s", code, body)
	}
	// one Haus does not know is 404, passed on to nobody
	if code, body := raw(t, s.h, "POST", "/napi/action-callback", "dev1", `{"action_id":"task.set_status","params":{"entity_id":"x","status":"done"}}`); code != 404 || !strings.Contains(body, "unknown_action") {
		t.Fatalf("set_status without a calendar: %d %s", code, body)
	}
}

// haus#26: without a backend the tool paths are 404, and a row path whose
// backend is not set is 404 next to the ones that are; nothing is passed on.
func TestWithoutABackendTheToolPathsAre404(t *testing.T) {
	none := toolsWith(t, which{})
	for _, p := range []struct{ method, path, body string }{
		{"GET", "/napi/defs/tool", ""},
		{"GET", "/napi/portal/tasks", ""},
		{"POST", "/napi/action-callback", `{"action_id":"task.set_status"}`},
	} {
		code, body := raw(t, none.h, p.method, p.path, "dev1", p.body)
		notFound(t, "no backend: "+p.method+" "+p.path, code, body)
	}
	s := toolsWith(t, which{cal: true})
	code, body := raw(t, s.h, "GET", "/napi/portal/contacts", "dev1", "")
	notFound(t, "contacts without an address book", code, body)
}

func rows(t *testing.T, h http.Handler, path, key string) []map[string]any {
	t.Helper()
	code, body := raw(t, h, "GET", path, "dev1", "")
	if code != 200 {
		t.Fatalf("%s: %d %s", path, code, body)
	}
	out := decode(t, body)
	if out["ok"] != true {
		t.Fatalf("%s: %s", path, body)
	}
	arr, ok := out[key].([]any)
	if !ok {
		t.Fatalf("%s: no %q list: %s", path, key, body)
	}
	var rs []map[string]any
	for _, r := range arr {
		rs = append(rs, r.(map[string]any))
	}
	return rs
}

func TestTaskRowsAreTheOpenReminders(t *testing.T) {
	s := allTools(t)
	rs := rows(t, s.h, "/napi/portal/tasks", "tasks")
	var titles []string
	for _, r := range rs {
		titles = append(titles, r["title"].(string))
	}
	if strings.Join(titles, "|") != "Milch kaufen|Müll rausbringen|Reifen wechseln|Keller aufräumen" {
		t.Fatalf("open, soonest first, no date last: %v", titles)
	}
	if rs[0]["id"] != "milch-1" || rs[0]["due"] != "Do 15.10.2026 18:00" {
		t.Fatalf("id is the UID, due in house time: %v", rs[0])
	}
	if rs[2]["due"] != "So 01.11.2026" {
		t.Fatalf("a whole day has no time: %v", rs[2])
	}
	if _, ok := rs[3]["due"]; ok {
		t.Fatalf("no due date is no key: %v", rs[3])
	}
}

func TestContactDocumentAndNoteRows(t *testing.T) {
	s := allTools(t)
	cs := rows(t, s.h, "/napi/portal/contacts", "contacts")
	if len(cs) != 2 || cs[0]["name"] != "anna Arndt" || cs[1]["name"] != "Bernd Bauer" {
		t.Fatalf("contacts by name, whatever the case: %v", cs)
	}
	if cs[1]["phone"] != "0170 1234567" || cs[1]["email"] != "bernd@example.org" || cs[1]["id"] != "urn:uuid:b" {
		t.Fatalf("the first number and address: %v", cs[1])
	}
	if cs[0]["birthday"] != "14.03." {
		t.Fatalf("a birthday without a year: %v", cs[0])
	}
	if _, ok := cs[0]["phone"]; ok {
		t.Fatalf("no phone is no key: %v", cs[0])
	}

	ds := rows(t, s.h, "/napi/portal/documents", "documents")
	if len(ds) != 2 || ds[0]["title"] != "Stromrechnung" || ds[0]["correspondent"] != "Stadtwerke" || ds[0]["created"] != "30.09.2026" {
		t.Fatalf("the newest documents first: %v", ds)
	}
	if _, ok := ds[1]["correspondent"]; ok {
		t.Fatalf("no correspondent is no key: %v", ds[1])
	}
	last := s.docs.Seen()[0].URL.Query()
	if last.Get("ordering") != "-created" || last.Has("query") {
		t.Fatalf("the newest, not a search: %v", last)
	}

	ns := rows(t, s.h, "/napi/portal/notes", "notes")
	if len(ns) != 2 || ns[0]["title"] != "Urlaub 2027" || ns[0]["id"] != "urlaub.md" || ns[1]["title"] != "Einkauf" {
		t.Fatalf("the newest notes, hidden ones never: %v", ns)
	}
}

// haus#28: every row with an id names its page on Haus, which the app
// opens on a tap: a reminder's and a contact's own page, a document's
// (which leads on to Paperless), a note's. Each id is escaped as one path
// part; a note's name keeps its folders.
func TestEveryToolRowNamesItsPage(t *testing.T) {
	s := allTools(t)
	for _, c := range []struct{ path, key string }{
		{"/napi/portal/tasks?done=1", "tasks"}, {"/napi/portal/contacts", "contacts"},
		{"/napi/portal/documents", "documents"}, {"/napi/portal/notes", "notes"},
	} {
		for _, r := range rows(t, s.h, c.path, c.key) {
			want := map[string]string{
				"tasks": "/kalender/erinnerung/", "contacts": "/kontakte/", "documents": "/dokumente/", "notes": "/notizen/",
			}[c.key] + url.PathEscape(fmt.Sprint(r["id"]))
			if r["page"] != want {
				t.Errorf("%s: page %v, want %q: %v", c.key, r["page"], want, r)
			}
		}
	}
	// a reminder whose UID needs escaping
	s.dav.Put("/bote/familie/x.ics", "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VTODO\r\nUID:a/b c?\r\nDTSTAMP:20261010T080000Z\r\nSUMMARY:Seltsam\r\nEND:VTODO\r\nEND:VCALENDAR\r\n")
	for _, r := range rows(t, s.h, "/napi/portal/tasks", "tasks") {
		if r["id"] == "a/b c?" && r["page"] != "/kalender/erinnerung/a%2Fb%20c%3F" {
			t.Fatalf("an odd UID: %v", r)
		}
	}
}

func TestToolRowsWhenTheBackendFails(t *testing.T) {
	s := allTools(t)
	s.docs.Status = 500
	code, body := raw(t, s.h, "GET", "/napi/portal/documents", "dev1", "")
	if code != 502 || strings.Contains(body, "pl-token") {
		t.Fatalf("Paperless down: %d %s", code, body)
	}
}

func callback(t *testing.T, h http.Handler, body string) (int, map[string]any) {
	t.Helper()
	code, b := raw(t, h, "POST", "/napi/action-callback", "dev1", body)
	var out map[string]any
	json.Unmarshal([]byte(b), &out)
	return code, out
}

func TestTickingAReminderOff(t *testing.T) {
	s := allTools(t)
	code, out := callback(t, s.h, `{"action_id":"task.set_status","params":{"entity_id":"milch-1","status":"done"}}`)
	if code != 200 || out["ok"] != true {
		t.Fatalf("done: %d %v", code, out)
	}
	ics := s.dav.Objects()["/bote/familie/milch-1.ics"]
	for _, want := range []string{"STATUS:COMPLETED", "\r\nCOMPLETED:", "PERCENT-COMPLETE:100", "UID:milch-1", "SUMMARY:Milch kaufen", "BEGIN:VALARM", "PRODID:-//phone//DE"} {
		if !strings.Contains(ics, want) {
			t.Fatalf("completed object lacks %q:\n%s", want, ics)
		}
	}
	if strings.Contains(ics, "NEEDS-ACTION") || strings.Count(ics, "STATUS:") != 1 {
		t.Fatalf("one status, the new one:\n%s", ics)
	}
	var put http.Header
	for _, h := range s.dav.Header {
		put = h
	}
	if put.Get("If-Match") == "" || put.Get("If-None-Match") != "" {
		t.Fatalf("the write must be conditional on what was read: %v", put)
	}
	for _, r := range rows(t, s.h, "/napi/portal/tasks", "tasks") {
		if r["id"] == "milch-1" {
			t.Fatal("a ticked-off reminder is no longer a row")
		}
	}
	// again: it is done already, which is what was asked
	if code, out := callback(t, s.h, `{"action_id":"task.set_status","params":{"entity_id":"milch-1","status":"done"}}`); code != 200 || out["ok"] != true {
		t.Fatalf("done twice: %d %v", code, out)
	}
	// status absent means done, as the contract's handler reads it
	if code, _ := callback(t, s.h, `{"action_id":"task.set_status","params":{"entity_id":"reifen-2"}}`); code != 200 {
		t.Fatalf("no status: %d", code)
	}
}

func TestReminderActionRules(t *testing.T) {
	s := allTools(t)
	before := s.dav.Objects()
	for _, c := range []struct {
		body   string
		code   int
		reason string
		why    string
	}{
		{`not json`, 400, "invalid_json", "a body that is not JSON"},
		{`{"params":{}}`, 400, "no_action_id", "no action id"},
		{`{"action_id":"task.add","params":{"title":"x"}}`, 404, "unknown_action", "Haus adds nothing from a row"},
		{`{"action_id":"doc.classify","params":{}}`, 404, "unknown_action", "nor classifies"},
		{`{"action_id":"model.release","params":{}}`, 404, "unknown_action", "nor anything Haus does not know"},
		{`{"action_id":"task.set_status","params":{"entity_id":"milch-1","status":"dismissed"}}`, 400, "bad_params", "done only"},
		{`{"action_id":"task.set_status","params":{"entity_id":"milch-1","status":"open"}}`, 400, "bad_params", "no reopening"},
		{`{"action_id":"task.set_status","params":{"status":"done"}}`, 400, "bad_params", "which one"},
		{`{"action_id":"task.set_status","params":{"entity_id":7,"status":"done"}}`, 400, "bad_params", "an id is text"},
		{`{"action_id":"task.set_status","params":{"entity_id":"a\nb","status":"done"}}`, 400, "bad_params", "no line breaks in a UID"},
		{`{"action_id":"task.set_status","params":{"entity_id":"` + strings.Repeat("x", 600) + `","status":"done"}}`, 400, "bad_params", "nor a novel"},
		{`{"action_id":"task.set_status","params":{"entity_id":"gibtsnicht","status":"done"}}`, 410, "not_found", "an unknown reminder: not 404, which the app reads as an unknown action"},
		{`{"action_id":"task.set_status","params":{"entity_id":"milch","status":"done"}}`, 410, "not_found", "a UID matches whole, not in part"},
		{`{"action_id":"task.set_status","params":{"entity_id":"muell-5","status":"done"}}`, 409, "recurring", "ticking off a series would end it"},
	} {
		code, out := callback(t, s.h, c.body)
		if code != c.code || out["reason"] != c.reason || out["ok"] != false {
			t.Errorf("%s: %d %v, want %d %s", c.why, code, out, c.code, c.reason)
		}
	}
	after := s.dav.Objects()
	for k, v := range before {
		if after[k] != v {
			t.Errorf("%s changed by a refused action", k)
		}
	}
}

func TestReminderActionWhenRadicaleFails(t *testing.T) {
	s := allTools(t)
	s.dav.Redirect = "http://elsewhere.example/"
	code, out := callback(t, s.h, `{"action_id":"task.set_status","params":{"entity_id":"milch-1","status":"done"}}`)
	if code != 502 || out["reason"] != "calendar_unavailable" {
		t.Fatalf("Radicale in trouble: %d %v", code, out)
	}
}

// Review of slice 2: the action body, the answers the app can tell apart.
func TestActionBodyIsCappedBeforeAnythingElse(t *testing.T) {
	big := `{"action_id":"task.set_status","params":{"entity_id":"` + strings.Repeat("a", 71<<10) + `","status":"done"}}`
	s := toolsWith(t, which{cal: true})
	before := s.dav.Objects()
	if code, out := callback(t, s.h, big); code != 413 || out["reason"] != "too_large" || out["ok"] != false {
		t.Fatalf("a body over 64 KB: %d %v", code, out)
	}
	if len(s.dav.Objects()) != len(before) {
		t.Fatal("a cut body changed the calendar")
	}
}

func TestReminderActionAnswers(t *testing.T) {
	s := allTools(t)
	s.dav.Put("/bote/familie/abgesagt.ics", "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VTODO\r\nUID:abgesagt\r\nSUMMARY:Abgesagt\r\nSTATUS:CANCELLED\r\nEND:VTODO\r\nEND:VCALENDAR\r\n")
	if code, out := callback(t, s.h, `{"action_id":"task.set_status","params":{"entity_id":"abgesagt","status":"done"}}`); code != 409 || out["reason"] != "cancelled" {
		t.Fatalf("cancelled: %d %v", code, out)
	}
	s.dav.RaceOnPut = "/bote/familie/milch-1.ics"
	if code, out := callback(t, s.h, `{"action_id":"task.set_status","params":{"entity_id":"milch-1","status":"done"}}`); code != 409 || out["reason"] != "changed" {
		t.Fatalf("changed in between: %d %v", code, out)
	}
	if strings.Contains(s.dav.Objects()["/bote/familie/milch-1.ics"], "COMPLETED") {
		t.Fatal("written over the phone's change")
	}
}

func TestTaskRowsCapTrimAndDone(t *testing.T) {
	s := allTools(t)
	for i := 0; i < 45; i++ {
		s.dav.Put(fmt.Sprintf("/bote/familie/viele-%02d.ics", i), fmt.Sprintf("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VTODO\r\nUID:viele-%02d\r\nSUMMARY:Viele %02d\r\nDUE:20261020T%02d0000Z\r\nEND:VTODO\r\nEND:VCALENDAR\r\n", i, i, i%24))
	}
	if rs := rows(t, s.h, "/napi/portal/tasks", "tasks"); len(rs) != 40 {
		t.Fatalf("at most 40 rows: %d", len(rs))
	}

	s = allTools(t)
	s.dav.Put("/bote/familie/spaced.ics", "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VTODO\r\nUID: spaced \r\nSUMMARY:Mit Leerzeichen\r\nEND:VTODO\r\nEND:VCALENDAR\r\n")
	open := rows(t, s.h, "/napi/portal/tasks", "tasks")
	found := false
	for _, r := range open {
		if r["status"] != "open" {
			t.Fatalf("open rows say so: %v", r)
		}
		if r["title"] == "Mit Leerzeichen" {
			found = r["id"] == "spaced"
		}
	}
	if !found {
		t.Fatalf("the row id is trimmed as the app trims it: %v", open)
	}
	if code, _ := callback(t, s.h, `{"action_id":"task.set_status","params":{"entity_id":"spaced","status":"done"}}`); code != 200 {
		t.Fatalf("the trimmed id ticks it off: %d", code)
	}
	// done=1: the ticked-off ones too, after the open ones
	all := rows(t, s.h, "/napi/portal/tasks?done=1", "tasks")
	var tail []string
	for _, r := range all[len(open)-1:] {
		tail = append(tail, r["title"].(string)+"="+r["status"].(string))
	}
	if strings.Join(tail, ",") != "Mit Leerzeichen=done,Schon erledigt=done" {
		t.Fatalf("done=1: the done ones last, the latest first: %v", tail)
	}
	for _, r := range all[:len(open)-1] {
		if r["status"] != "open" {
			t.Fatalf("open first: %v", all)
		}
	}
}
