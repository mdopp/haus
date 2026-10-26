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

// Box reads on a phone: Was läuft and the journal are lines, not narrow
// table columns broken mid-word — a state a coloured dot with its word for
// a screen reader, what is not green first; an entry when and who above,
// what and why below, "human:" and a verb said twice ("scan scan") gone.
func TestBoxReadsAsLines(t *testing.T) {
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
			b, _ := json.Marshal(map[string]any{"ready": map[string]any{"state": "bereit", "operator": "läuft"}, "services": []map[string]any{
				{"name": "adguard", "state": "grün"},
				{"name": "auth", "state": "grün", "detail": "Passwort-Mails über smtp.gmail.com · geprüft vor 28 h"},
				{"name": "paperless", "state": "gelb", "detail": "Redis langsam"}}})
			text = string(b)
		case "laib_journal":
			now := time.Now().UTC()
			b, _ := json.Marshal([]map[string]any{
				{"t": now.Add(-2 * time.Hour), "actor": "human:core", "verb": "apply", "result": "green", "reason": "#69 Den Scanner einrichten"},
				{"t": now.Add(-time.Hour), "actor": "human:core", "verb": "scan", "service": "scan", "result": "info", "detail": "pruefen"}})
			text = string(b)
		}
		json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"content": []map[string]string{{"text": text}}}})
	}))
	t.Cleanup(srv.Close)
	h := Box(&laib.Client{URL: srv.URL, Token: "t"}, "https://laib.box.test")
	page := getAt(h, "/box/dienste?ganz=1", "admins") + getAt(h, "/box/journal?ganz=1", "admins")
	mustContain(t, page, "overflow-wrap:break-word",
		`<h2>Was läuft</h2><ul class=zeilen><li><span class="punkt gelb" role=img aria-label="gelb"></span><span class=was><b>paperless</b><span class=text>Redis langsam</span>`,
		`<span class="punkt gruen" role=img aria-label="grün"></span><span class=was><b>auth</b><span class=text>Passwort-Mails über smtp.gmail.com · geprüft vor 28 h</span>`,
		`<span class="punkt k" role=img aria-label="info"></span><span class=was><span class=wann>`, ` · core</span><b>scan</b><span class=text>pruefen</span>`,
		`<span class="punkt gruen" role=img aria-label="green"></span>`, `<b>apply</b><span class=text>#69 Den Scanner einrichten</span>`)
	mustNotContain(t, page, "td,th{text-align:left;padding:10px 12px;border-bottom:1px solid var(--line);vertical-align:top;overflow-wrap:anywhere", "human:core", "scan scan", ">green<", ">info<")
	before(t, page, "<b>paperless</b>", "<b>adguard</b>")
	before(t, page, "<b>scan</b>", "<b>apply</b>") // newest first
	if strings.Contains(page, "<h2>Journal · 24h</h2><table>") {
		t.Error("the journal is still a table")
	}
}
