package pages

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/mdopp/haus/internal/laib"
	"github.com/mdopp/haus/internal/schalter"
)

// notesLaib keeps the laib_note texts it is given.
func notesLaib(t *testing.T) (*laib.Client, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var notes []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Params struct {
				Name string         `json:"name"`
				Args map[string]any `json:"arguments"`
			} `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.Params.Name == "laib_note" && req.Params.Args["kind"] == "watch" {
			mu.Lock()
			notes = append(notes, req.Params.Args["text"].(string))
			mu.Unlock()
		}
		json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"content": []map[string]string{{"text": "ok"}}}})
	}))
	t.Cleanup(srv.Close)
	return &laib.Client{URL: srv.URL, Token: "t"}, func() []string {
		deadline := time.Now().Add(2 * time.Second)
		for {
			mu.Lock()
			n := append([]string(nil), notes...)
			mu.Unlock()
			if len(n) > 0 || time.Now().After(deadline) {
				return n
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

// haus#82, haus#89: a switch from a page names who pressed it in Laib's
// journal, with HA's context id, so HA's logbook ("Haus") and the journal
// together say who it was. A refused switch notes nothing.
func TestSchaltenNotesWhoItWas(t *testing.T) {
	_, cfg := switchHouse(t)
	c, notes := notesLaib(t)
	cfg.Witness = &schalter.Witness{Laib: c}
	h := Schalten(cfg)
	rec := post(t, h, "/schalten", url.Values{"geraet": {"light.flur"}, "tun": {"light.turn_on"}, "zurueck": {"/start"}}, "carola", nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("%d", rec.Code)
	}
	if got := notes(); len(got) != 1 || got[0] != "ha person=carola service=light.turn_on entity=light.flur context=ctx-1" {
		t.Fatalf("notes %q", got)
	}
	post(t, h, "/schalten", url.Values{"geraet": {"lock.haustuer"}, "tun": {"lock.unlock"}, "zurueck": {"/start"}}, "carola", nil)
	time.Sleep(50 * time.Millisecond)
	if got := notes(); len(got) != 1 {
		t.Errorf("a refused switch noted: %q", got)
	}
}
