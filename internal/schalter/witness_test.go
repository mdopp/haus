package schalter

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/mdopp/haus/internal/laib"
)

// noteLaib is a Laib that keeps laib_note's arguments, or refuses the
// verb to the haus hand as a Laib before the owner's approval does.
type noteLaib struct {
	mu     sync.Mutex
	notes  []map[string]any
	refuse bool
}

func (f *noteLaib) client(t *testing.T) *laib.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Params struct {
				Name string         `json:"name"`
				Args map[string]any `json:"arguments"`
			} `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		text, isErr := "noted", false
		switch {
		case req.Params.Name != "laib_note":
			text = "[]"
		case f.refuse:
			text, isErr = "haus may not note: policy.yml limits it to status, inbox, journal", true
		default:
			f.mu.Lock()
			f.notes = append(f.notes, req.Params.Args)
			f.mu.Unlock()
		}
		json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"content": []map[string]string{{"text": text}}, "isError": isErr}})
	}))
	t.Cleanup(srv.Close)
	return &laib.Client{URL: srv.URL, Token: "t"}
}

// logged is what fn writes to the log.
func logged(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	old, flags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer func() { log.SetOutput(old); log.SetFlags(flags) }()
	fn()
	return buf.String()
}

// haus#82, haus#89: every switch names its person — a line in Haus's log
// and a watch note in Laib's journal with HA's context id; a name that
// could write into the line is "?"; Laib refusing the verb is said once
// and the switch goes on; no Laib, the log only.
func TestWitnessNamesThePerson(t *testing.T) {
	f := &noteLaib{}
	w := &Witness{Laib: f.client(t)}
	out := logged(t, func() {
		w.Saw("anna", "light.turn_off", "light.flur", "01HCTX")
		w.wait()
		w.Saw("anna\nperson=ben", "light.turn_on", "light.flur", "")
		w.wait()
	})
	line := "ha person=anna service=light.turn_off entity=light.flur context=01HCTX"
	if !strings.Contains(out, "haus: "+line+"\n") || strings.Contains(out, "person=ben") {
		t.Errorf("log:\n%s", out)
	}
	if len(f.notes) != 2 || f.notes[0]["kind"] != "watch" || f.notes[0]["text"] != line {
		t.Fatalf("notes %v", f.notes)
	}
	if f.notes[1]["text"] != "ha person=? service=light.turn_on entity=light.flur context=" {
		t.Errorf("forged name: %v", f.notes[1])
	}

	f.refuse = true
	w = &Witness{Laib: f.client(t)}
	out = logged(t, func() {
		for range 3 {
			w.Saw("ben", "switch.turn_off", "switch.kaffee", "c")
			w.wait()
		}
	})
	if n := strings.Count(out, "takes no notes"); n != 1 {
		t.Errorf("refusal said %d times:\n%s", n, out)
	}
	if n := strings.Count(out, "ha person=ben"); n != 3 {
		t.Errorf("%d log lines, want 3:\n%s", n, out)
	}

	var none *Witness
	out = logged(t, func() { none.Saw("carla", "light.turn_on", "light.bad", "x") })
	if !strings.Contains(out, "ha person=carla service=light.turn_on entity=light.bad context=x") {
		t.Errorf("nil witness: %q", out)
	}
}
