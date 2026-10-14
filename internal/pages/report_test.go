package pages

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/haus/internal/laib"
)

// haus#15: the Operator's morning report (laib ADR 0013) is a card on the
// Box page. It is the newest journal note of kind report; the haus hand
// may read the journal, so nothing new is asked of Laib.
func TestBoxShowsTheMorningReport(t *testing.T) {
	ready := map[string]any{"state": "bereit", "services": 1, "green": 1, "operator": "läuft"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		text := "[]"
		switch req.Params.Name {
		case "laib_status":
			b, _ := json.Marshal(map[string]any{"ready": ready, "services": []map[string]any{{"name": "caddy", "state": "grün"}}})
			text = string(b)
		case "laib_journal":
			now := time.Now().UTC()
			b, _ := json.Marshal([]map[string]any{
				{"t": now.Add(-30 * time.Hour), "actor": "ops", "verb": "note", "reason": "report", "result": "info", "detail": "Gestern: alles ruhig."},
				{"t": now.Add(-5 * time.Hour), "actor": "ops", "verb": "note", "reason": "report", "result": "info", "detail": "Nachts: Backup um 03:12 an zwei Orten. Paperless kurz rot, neu gestartet."},
				{"t": now.Add(-1 * time.Hour), "actor": "ops", "verb": "note", "reason": "watch", "result": "info", "detail": "Routine: alles grün."},
			})
			text = string(b)
		}
		json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"content": []map[string]string{{"text": text}}}})
	}))
	t.Cleanup(srv.Close)
	page := get(Box(&laib.Client{URL: srv.URL, Token: "t"}, "https://laib.box.test"), "admins")
	if !strings.Contains(page, "Morgenbericht") || !strings.Contains(page, "Paperless kurz rot, neu gestartet.") {
		t.Fatalf("the newest morning report is missing:\n%s", page)
	}
	if strings.Contains(page, "Gestern: alles ruhig.") && strings.Index(page, "Gestern: alles ruhig.") < strings.Index(page, "Journal") {
		t.Fatalf("an older report is shown as the card:\n%s", page)
	}
}

// Without a report the card says so, in plain words, instead of vanishing.
func TestBoxSaysWhenThereIsNoMorningReport(t *testing.T) {
	page := get(Box(fakeLaib(t, map[string]any{"state": "bereit", "operator": "aus"}, nil), "https://laib.box.test"), "admins")
	if !strings.Contains(page, "Morgenbericht") || !strings.Contains(page, "noch keiner") {
		t.Fatalf("no report must be said plainly:\n%s", page)
	}
}
