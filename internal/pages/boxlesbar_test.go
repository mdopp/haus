package pages

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mdopp/haus/internal/laib"
)

// Box reads on a phone: what is not green is lines, not narrow table
// columns broken mid-word — a state a coloured dot with its word for a
// screen reader, red first; the green ones are Laib's page's (haus#157).
func TestBoxReadsAsLines(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		text := "[]"
		if req.Params.Name == "laib_status" {
			b, _ := json.Marshal(map[string]any{"ready": map[string]any{"state": "bereit", "operator": "läuft"}, "services": []map[string]any{
				{"name": "adguard", "state": "grün"},
				{"name": "auth", "state": "rot", "detail": "Passwort-Mails über smtp.gmail.com · geprüft vor 28 h"},
				{"name": "paperless", "state": "gelb", "detail": "Redis langsam"}}})
			text = string(b)
		}
		json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"content": []map[string]string{{"text": text}}}})
	}))
	t.Cleanup(srv.Close)
	h := Box(&laib.Client{URL: srv.URL, Token: "t"}, "https://laib.box.test")
	page := getAt(h, "/box/dienste?ganz=1", "admins")
	mustContain(t, page, "overflow-wrap:break-word",
		`<h2>Nicht grün</h2><ul class=zeilen><li><span class="punkt rot" role=img aria-label="rot"></span><span class=was><b>auth</b><span class=text>Passwort-Mails über smtp.gmail.com · geprüft vor 28 h</span>`,
		`<span class="punkt gelb" role=img aria-label="gelb"></span><span class=was><b>paperless</b><span class=text>Redis langsam</span>`)
	mustNotContain(t, page, "td,th{text-align:left;padding:10px 12px;border-bottom:1px solid var(--line);vertical-align:top;overflow-wrap:anywhere", "<b>adguard</b>")
	before(t, page, "<b>auth</b>", "<b>paperless</b>")
}
