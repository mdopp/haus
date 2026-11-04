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
	Kaputt   bool          // laib_wish answers an error
	Warte    time.Duration // laib_wish waits this long first
	Gefragt  []string      // the stand questions, in order
	Erledigt []int         // the Box-Aufgaben ticked off
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
	return []map[string]any{
		{"name": "laib_wish", "inputSchema": map[string]any{"properties": wish}},
		{"name": "laib_aufgabe", "inputSchema": map[string]any{"properties": map[string]any{"action": map[string]any{}, "id": map[string]any{}, "von": map[string]any{}}}},
	}
}

// Antwort answers one tools/call; ok is false for a tool it does not fake.
func (f *Laib) Antwort(name string, args map[string]any) (text string, isErr, ok bool) {
	str := func(k string) string { s, _ := args[k].(string); return s }
	switch {
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
