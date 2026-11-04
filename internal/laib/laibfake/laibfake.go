// Package laibfake is Laib's MCP as far as a module's wish goes (laib#1038,
// homes ADR 0001 §10): tools/list names laib_wish with modul, laib_wish
// files a wish and answers its stand, laib_aufgabe ticks one off. For the
// tests and cmd/homesdemo only.
package laibfake

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Laib is the fake. Ohne: an older Laib, no wish verb.
type Laib struct {
	mu       sync.Mutex
	je       map[string]map[string]any // modul → its wish, as laib_wish stand answers it
	next     int
	Ohne     bool
	Kaputt   bool              // laib_wish answers an error
	Warte    time.Duration     // laib_wish waits this long first
	Gefragt  []string          // the stand questions, in order
	Erledigt []int             // the Box-Aufgaben ticked off
	Modul    []string          // laib_module's proposals: "<liste>|<grund>" (laib#1036)
	status   map[string]string // laib_status: a service's own status (its JSON)
}

// New is a fake with no wish yet.
func New() *Laib { return &Laib{je: map[string]map[string]any{}, next: 10} }

// Server serves the fake on its own address; its /mcp is Laib's.
func (f *Laib) Server() *httptest.Server { return httptest.NewServer(f) }

// Setze moves a module's wish to stand (with a reason for abgelehnt).
func (f *Laib) Setze(modul, stand, grund string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w := f.je[modul]
	if w == nil {
		f.next++
		w = map[string]any{"modul": modul, "aufgabe": f.next, "seit": time.Now().Format(time.DateOnly), "von": "homes:anna"}
		f.je[modul] = w
	}
	w["stand"] = stand
	delete(w, "grund")
	if grund != "" {
		w["grund"] = grund
	}
}

// Status sets what laib_status says of service: its own status, JSON; "" says nothing of it.
func (f *Laib) Status(service, own string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.status == nil {
		f.status = map[string]string{}
	}
	f.status[service] = own
	if own == "" {
		delete(f.status, service)
	}
}

// DemoBeets fills a beets inbox for cmd/homesdemo as laib's importer leaves it (laib
// templates/beets) and is beets' own status for laib_status: an upload settling, two albums
// sorted in three hours before now, a RAR kept back 40 minutes before. leer: both empty.
func DemoBeets(inbox string, now time.Time, leer bool) string {
	os.RemoveAll(inbox)
	run := filepath.Join(inbox, "_nicht-importiert", "2026-11-04_09-12-00")
	os.MkdirAll(run, 0o755)
	if leer {
		return ""
	}
	os.WriteFile(filepath.Join(inbox, "Sommerplatte.zip"), []byte("PK"), 0o644)
	os.WriteFile(filepath.Join(run, "kassette-1994.rar"), []byte("Rar!"), 0o644)
	os.WriteFile(filepath.Join(run, "GRUND.txt"), []byte("kassette-1994.rar: RAR kann die Box nicht auspacken; bitte als ZIP\n"), 0o644)
	return fmt.Sprintf(`{"wartet":1,"letzte":[{"at":%q,"eingang":"kassette-1994.rar","nicht_importiert":[{"datei":"kassette-1994.rar",`+
		`"grund":"RAR kann die Box nicht auspacken; bitte als ZIP"}]},{"at":%q,"eingang":"Erfundenes Album.zip","titel":24,"alben":2}]}`,
		now.Add(-40*time.Minute).UTC().Format(time.RFC3339), now.Add(-3*time.Hour).UTC().Format(time.RFC3339))
}

// Stand is a module's wish's stand, "" without one.
func (f *Laib) Stand(modul string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if w := f.je[modul]; w != nil {
		return w["stand"].(string)
	}
	return ""
}

func (f *Laib) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Method string `json:"method"`
		Params struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		} `json:"params"`
	}
	json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req)
	if req.Method == "tools/list" {
		json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"tools": f.Tools()}})
		return
	}
	text, isErr, _ := f.Antwort(req.Params.Name, req.Params.Arguments)
	json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"content": []map[string]string{{"text": text}}, "isError": isErr}})
}

// Tools is tools/list's tools: laib_wish with modul (unless Ohne), laib_aufgabe.
func (f *Laib) Tools() []map[string]any {
	wish := map[string]any{"text": map[string]any{}}
	if !f.Ohne {
		wish["modul"], wish["stand"], wish["von"] = map[string]any{}, map[string]any{}, map[string]any{}
	}
	tools := []map[string]any{
		{"name": "laib_wish", "inputSchema": map[string]any{"properties": wish}},
		{"name": "laib_aufgabe", "inputSchema": map[string]any{"properties": map[string]any{"action": map[string]any{}, "id": map[string]any{}, "von": map[string]any{}}}},
	}
	if !f.Ohne {
		tools = append(tools, map[string]any{"name": "laib_module", "inputSchema": map[string]any{"properties": map[string]any{"action": map[string]any{}, "liste": map[string]any{}, "grund": map[string]any{}}}})
	}
	return tools
}

// Antwort answers one tools/call; ok is false for a tool it does not fake.
func (f *Laib) Antwort(name string, args map[string]any) (text string, isErr, ok bool) {
	str := func(k string) string { s, _ := args[k].(string); return s }
	switch {
	case name == "laib_status":
		f.mu.Lock()
		defer f.mu.Unlock()
		svc := []map[string]any{}
		if own, ok := f.status[str("service")]; ok {
			svc = append(svc, map[string]any{"name": str("service"), "detail": "", "eigenes": json.RawMessage(own)})
		}
		b, _ := json.Marshal(map[string]any{"services": svc})
		return string(b), false, true
	case name == "laib_aufgabe" && str("action") == "erledigt":
		id, _ := strconv.Atoi(str("id"))
		f.mu.Lock()
		defer f.mu.Unlock()
		f.Erledigt = append(f.Erledigt, id)
		for _, w := range f.je {
			if w["aufgabe"] == id {
				w["stand"] = map[bool]string{true: "umgesetzt", false: "abgelehnt"}[str("von") == ""]
			}
		}
		return `{"message":"Box-Aufgabe erledigt."}`, false, true
	case name == "laib_module" && !f.Ohne:
		time.Sleep(f.Warte)
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.Kaputt {
			return "laib-haushalt antwortet nicht", true, true
		}
		f.Modul = append(f.Modul, str("liste")+"|"+str("grund"))
		return fmt.Sprintf(`{"proposal":%d,"class":"values","message":"Vorschlag #%d: nach der Freigabe in der Inbox startet Homes mit der neuen Auswahl."}`, 16+len(f.Modul), 16+len(f.Modul)), false, true
	case name != "laib_wish" || f.Ohne:
		return "unknown tool " + name, true, name == "laib_wish"
	}
	time.Sleep(f.Warte)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Kaputt {
		return "laib-haushalt antwortet nicht", true, true
	}
	if q := str("stand"); q != "" {
		f.Gefragt = append(f.Gefragt, q)
		out := []map[string]any{}
		for _, pair := range strings.Fields(q) {
			if w := f.je[strings.SplitN(pair, "=", 2)[0]]; w != nil {
				out = append(out, w)
			}
		}
		b, _ := json.Marshal(out)
		return string(b), false, true
	}
	m := str("modul")
	if w := f.je[m]; w != nil && (w["stand"] == "angefordert" || w["stand"] == "wartet" || w["stand"] == "eingerichtet") {
		return fmt.Sprintf(`{"aufgabe":%v,"neu":false,"message":"Der Wunsch liegt schon beim Admin."}`, w["aufgabe"]), false, true
	}
	f.next++
	f.je[m] = map[string]any{"modul": m, "stand": "angefordert", "aufgabe": f.next, "seit": time.Now().Format(time.DateOnly), "von": "homes:" + str("von")}
	return fmt.Sprintf(`{"aufgabe":%d,"neu":true,"message":"Der Wunsch liegt beim Admin als Box-Aufgabe #%d."}`, f.next, f.next), false, true
}
