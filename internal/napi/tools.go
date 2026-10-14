package napi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/mdopp/haus/internal/caldav"
	"github.com/mdopp/haus/internal/cards"
	"github.com/mdopp/haus/internal/haushalt"
)

// The tool widgets (haus#10): the app's generic list widget reads a catalog
// (GET /napi/defs/tool), fetches each tool's rows from the path the def
// names, maps them through the def's cell schema, and sends a row's action
// to POST /napi/action-callback (the app's ToolDefs.kt, ToolCells.kt).
// Haus's catalog is the Haushalt source's backends that are set: reminders,
// contacts, documents, notes. Haus has no page for creating or opening an
// item, so no def names one (tool-compose-path, tool-item-id-field): a
// guessed route would land on Start.

// maxRows is what a widget shows at most (ToolDefs.MAX_ROWS).
const maxRows = 40

// rowsTimeout bounds a backend read below the app's 15s read timeout.
const rowsTimeout = 12 * time.Second

type toolsAPI struct {
	hh  haushalt.Config
	loc *time.Location
}

func newTools(hh haushalt.Config) *toolsAPI {
	if hh.Calendar == nil && hh.Contacts == nil && hh.Documents == nil && hh.Notes == nil {
		return nil
	}
	loc := hh.Loc
	if loc == nil {
		loc = time.Local
	}
	return &toolsAPI{hh: hh, loc: loc}
}

func (t *toolsAPI) routes(mux *http.ServeMux, auth func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /napi/defs/tool", auth(t.defs))
	// Haus answers the actions it knows; any other is 404 unknown_action.
	mux.HandleFunc("POST /napi/action-callback", auth(t.action))
	if t.hh.Calendar != nil {
		mux.HandleFunc("GET /napi/portal/tasks", auth(t.tasks))
	}
	if t.hh.Contacts != nil {
		mux.HandleFunc("GET /napi/portal/contacts", auth(t.contacts))
	}
	if t.hh.Documents != nil {
		mux.HandleFunc("GET /napi/portal/documents", auth(t.documents))
	}
	if t.hh.Notes != nil {
		mux.HandleFunc("GET /napi/portal/notes", auth(t.notes))
	}
}

// def is one catalog entry in the keys ToolDefs.parseDef reads. Every key
// is present, empty rather than null (Android reads null as "null").
func def(id, label, path string, schema map[string]any) map[string]any {
	return map[string]any{
		"tool-id": id, "tool-label": label, "tool-api-path": path, "tool-cell-schema": schema,
		"tool-actions": []string{}, "tool-actions-titled": []any{}, "tool-action-params": map[string]any{},
	}
}

// GET /napi/defs/tool → {ok, kind:"tool", defs:[…]}, by id.
func (t *toolsAPI) defs(w http.ResponseWriter, r *http.Request) {
	defs := []map[string]any{}
	if t.hh.Contacts != nil {
		defs = append(defs, def("contacts", "Kontakte", "/napi/portal/contacts",
			map[string]any{"title": "name", "meta": []string{"phone", "email", "birthday"}}))
	}
	if t.hh.Documents != nil {
		defs = append(defs, def("doc", "Dokumente", "/napi/portal/documents",
			map[string]any{"title": "title", "meta": []string{"created", "correspondent"}}))
	}
	if t.hh.Notes != nil {
		defs = append(defs, def("note", "Notizen", "/napi/portal/notes",
			map[string]any{"title": "title", "meta": []string{"changed"}}))
	}
	if t.hh.Calendar != nil {
		d := def("task", "Erinnerungen", "/napi/portal/tasks",
			map[string]any{"title": "title", "meta": []string{"due"}, "actions": []string{actionDone}})
		d["tool-actions"] = []string{actionDone}
		d["tool-actions-titled"] = []any{map[string]string{"id": actionDone, "title": "Erledigt"}}
		// "$id" is the row's id field, "done" a literal (ToolCells.resolveParams)
		d["tool-action-params"] = map[string]any{actionDone: map[string]string{"entity_id": "$id", "status": "done"}}
		defs = append(defs, d)
	}
	answer(w, http.StatusOK, map[string]any{"ok": true, "kind": "tool", "defs": defs})
}

// rowsFailed: the widget keeps its last good rows; the detail stays in the log.
func rowsFailed(w http.ResponseWriter, what string, err error) {
	log.Printf("haus: tool rows %s: %v", what, err)
	answer(w, http.StatusBadGateway, map[string]any{"ok": false, "error": "backend_unavailable"})
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func first(vs []string) string {
	for _, v := range vs {
		if v = oneLine(v); v != "" {
			return v
		}
	}
	return ""
}

// GET /napi/portal/tasks[?done=1] → {ok, tasks:[{id?, page?, title, status, due?}]}:
// the open reminders, the soonest first, those without a date last; with
// done=1 the ticked-off and cancelled ones after them, the latest first.
// status is the app's word: open,
// done, dismissed. id is the UID trimmed, as the app trims what it sends.
func (t *toolsAPI) tasks(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), rowsTimeout)
	defer cancel()
	var todos []caldav.Todo
	var err error
	if r.URL.Query().Get("done") == "1" {
		todos, err = t.hh.Calendar.Todos(ctx)
	} else {
		todos, err = t.hh.Calendar.OpenTodos(ctx)
	}
	if err != nil {
		rowsFailed(w, "tasks", err)
		return
	}
	var open, closed []caldav.Todo
	for _, td := range todos {
		if td.Open() {
			open = append(open, td)
		} else {
			closed = append(closed, td)
		}
	}
	sort.SliceStable(closed, func(i, j int) bool { return closed[i].Completed.After(closed[j].Completed) })
	out := []map[string]any{}
	for _, td := range append(open, closed...) {
		if len(out) == maxRows {
			break
		}
		row := map[string]any{"title": oneLine(td.Summary), "status": "open"}
		if row["title"] == "" {
			row["title"] = "–"
		}
		if !td.Open() {
			row["status"] = "done"
			if strings.EqualFold(td.Status, "CANCELLED") {
				row["status"] = "dismissed"
			}
		}
		if id := strings.TrimSpace(td.UID); id != "" {
			row["id"], row["page"] = id, cards.ReminderPage(id)
		}
		switch {
		case td.Due.IsZero():
		case td.DueAllDay:
			row["due"] = haushalt.Day(td.Due.In(t.loc))
		default:
			row["due"] = haushalt.DayTime(td.Due.In(t.loc))
		}
		out = append(out, row)
	}
	answer(w, http.StatusOK, map[string]any{"ok": true, "tasks": out})
}

// GET /napi/portal/contacts → {ok, contacts:[{id?, page?, name, phone?, email?, birthday?}]}, by name.
func (t *toolsAPI) contacts(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), rowsTimeout)
	defer cancel()
	all, _, err := t.hh.Contacts.Contacts(ctx)
	if err != nil {
		rowsFailed(w, "contacts", err)
		return
	}
	var cs []caldav.Contact
	for _, ct := range all {
		if ct.Name = oneLine(ct.Name); ct.Name == "" {
			ct.Name = oneLine(ct.Org)
		}
		if ct.Name != "" {
			cs = append(cs, ct)
		}
	}
	sort.SliceStable(cs, func(i, j int) bool { return strings.ToLower(cs[i].Name) < strings.ToLower(cs[j].Name) })
	out := []map[string]any{}
	for _, ct := range cs {
		if len(out) == maxRows {
			break
		}
		row := map[string]any{"name": ct.Name}
		for k, v := range map[string]string{"id": ct.UID, "phone": first(ct.Phones), "email": first(ct.Emails)} {
			if v != "" {
				row[k] = v
			}
		}
		if ct.UID != "" {
			row["page"] = cards.ContactPage(ct.UID)
		}
		if ct.Birthday != "" {
			row["birthday"] = haushalt.BirthdayText(ct.Birthday)
		}
		out = append(out, row)
	}
	answer(w, http.StatusOK, map[string]any{"ok": true, "contacts": out})
}

// GET /napi/portal/documents → {ok, documents:[{id, title, created?, correspondent?, link?, page?}]}:
// the newest in Paperless, what its token's user may see. A page only when
// Paperless's public address is known: Haus's page sends the tap on there.
func (t *toolsAPI) documents(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), rowsTimeout)
	defer cancel()
	docs, err := t.hh.Documents.Recent(ctx, maxRows)
	if err != nil {
		rowsFailed(w, "documents", err)
		return
	}
	out := []map[string]any{}
	for _, d := range docs {
		row := map[string]any{"id": d.ID, "title": oneLine(d.Title)}
		if row["title"] == "" {
			row["title"] = "–"
		}
		if !d.Created.IsZero() {
			row["created"] = d.Created.Format("02.01.2006")
		}
		if c := oneLine(d.Correspondent); c != "" {
			row["correspondent"] = c
		}
		if d.Link != "" {
			row["link"], row["page"] = d.Link, cards.DocumentPage(d.ID)
		}
		out = append(out, row)
	}
	answer(w, http.StatusOK, map[string]any{"ok": true, "documents": out})
}

// GET /napi/portal/notes → {ok, notes:[{id, page, title, changed}]}: the newest notes.
func (t *toolsAPI) notes(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), rowsTimeout)
	defer cancel()
	es, err := t.hh.Notes.Recent(ctx, maxRows)
	if err != nil {
		rowsFailed(w, "notes", err)
		return
	}
	out := []map[string]any{}
	for _, e := range es {
		out = append(out, map[string]any{"id": e.Name, "page": cards.NotePage(e.Name), "title": e.Title, "changed": haushalt.DayTime(e.Changed.In(t.loc))})
	}
	answer(w, http.StatusOK, map[string]any{"ok": true, "notes": out})
}

// actionDone ticks a reminder off: the one row action Haus has. It is not
// destructive (the phone unticks it), so it needs no confirmed; there is
// no action here that would.
const actionDone = "task.set_status"

func refuseAction(w http.ResponseWriter, code int, reason string) {
	answer(w, code, map[string]any{"ok": false, "reason": reason})
}

// POST /napi/action-callback {action_id, params, confirmed?} → {ok}; refusals
// carry the reason ApiClient.actionOutcome reads. A body over 64 KB is 413
// too_large before anything else, never cut and handled.
func (t *toolsAPI) action(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ActionID any            `json:"action_id"`
		Params   map[string]any `json:"params"`
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
	if err != nil { // nothing of a cut body is handled
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			refuseAction(w, http.StatusRequestEntityTooLarge, "too_large")
		} else {
			refuseAction(w, http.StatusBadRequest, "invalid_json")
		}
		return
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		refuseAction(w, http.StatusBadRequest, "invalid_json")
		return
	}
	id, _ := body.ActionID.(string)
	if id == "" {
		refuseAction(w, http.StatusBadRequest, "no_action_id")
		return
	}
	if id != actionDone || t.hh.Calendar == nil {
		refuseAction(w, http.StatusNotFound, "unknown_action")
		return
	}
	uid, ok := body.Params["entity_id"].(string)
	status := any("done")
	if s, has := body.Params["status"]; has {
		status = s
	}
	if !ok || status != "done" {
		refuseAction(w, http.StatusBadRequest, "bad_params")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), rowsTimeout)
	defer cancel()
	err = haushalt.ReminderDone(ctx, t.hh.Calendar, uid)
	switch {
	case err == nil:
		answer(w, http.StatusOK, map[string]any{"ok": true})
	case errors.Is(err, haushalt.ErrBadUID):
		refuseAction(w, http.StatusBadRequest, "bad_params")
	case errors.Is(err, caldav.ErrNoTodo): // not 404: the app reads that as an unknown action
		refuseAction(w, http.StatusGone, "not_found")
	case errors.Is(err, caldav.ErrCancelled):
		refuseAction(w, http.StatusConflict, "cancelled")
	case errors.Is(err, caldav.ErrRecurring):
		refuseAction(w, http.StatusConflict, "recurring")
	case errors.Is(err, caldav.ErrChanged):
		refuseAction(w, http.StatusConflict, "changed")
	default:
		log.Printf("haus: action %s: %v", id, err)
		refuseAction(w, http.StatusBadGateway, "calendar_unavailable")
	}
}
