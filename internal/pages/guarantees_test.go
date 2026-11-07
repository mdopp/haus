package pages

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mdopp/homes/internal/laib"
)

// fakeLaib answers `laib status --json` over MCP with the given readiness and rows.
func fakeLaib(t *testing.T, ready map[string]any, rows []map[string]any) *laib.Client {
	t.Helper()
	return fakeLaibWith(t, ready, rows, nil)
}

// fakeLaibWith is fakeLaib with more answers, by tool name (laib_inbox …).
func fakeLaibWith(t *testing.T, ready map[string]any, rows []map[string]any, more map[string]string) *laib.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		text := "[]"
		if req.Params.Name == "laib_befunde" { // diag ran, found nothing
			text = `{"at":"2026-11-06T10:00:00Z","findings":[]}`
		}
		if m, ok := more[req.Params.Name]; ok {
			text = m
		}
		if req.Params.Name == "laib_status" {
			b, _ := json.Marshal(map[string]any{"ready": ready, "services": rows})
			text = string(b)
		}
		json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"content": []map[string]string{{"text": text}}}})
	}))
	t.Cleanup(srv.Close)
	return &laib.Client{URL: srv.URL, Token: "t"}
}

func get(h http.HandlerFunc, groups string) string { return getAt(h, "/box?ganz=1", groups) }

// getAt is a Box page (Übersicht, Aufgaben, Auslastung, Haushalt and its
// rows), whole.
func getAt(h http.HandlerFunc, target, groups string) string {
	req := httptest.NewRequest("GET", target, nil)
	req.Header.Set("Remote-User", "anna")
	if groups != "" {
		req.Header.Set("Remote-Groups", groups)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Body.String()
}

// haus#2, haus#35: the guarantees in the admin's words are the Box
// Übersicht's, one line (owner 2026-11-06); the family's Start says the
// box in one calm line (TestStartBoxLine).
func TestBoxShowsTheGuarantees(t *testing.T) {
	now := time.Now()
	ready := map[string]any{"state": "bereit", "operator": "läuft",
		"lastBackup": map[string]any{"at": now.Add(-3 * time.Hour).UTC().Format(time.RFC3339), "repos": 1, "ok": true},
		"lastChange": map[string]any{"summary": "Paperless wird installiert — unter paperless.box.test nur im Heimnetz erreichbar", "at": now.Add(-20 * time.Hour).UTC().Format(time.RFC3339), "reversible": true}}
	rows := []map[string]any{{"name": "immich", "state": "grün"}, {"name": "paperless", "state": "grün"}}
	admin := get(Box(fakeLaib(t, ready, rows), "https://laib.box.test"), "admins,family")
	mustContain(t, admin, "<b class=gruen>Alles in Ordnung.</b> · Letzte Sicherung ", ", an 1 Ort · Letzte Änderung: Paperless wird installiert",
		`<a href="https://laib.box.test/inbox">Rückgängig</a>`)
	// the family's Start has none of it
	_, body := startPage(t, StartConfig{Laib: fakeLaib(t, ready, rows), LaibURL: "https://laib.box.test"}, "family,admins")
	mustNotContain(t, body, "Alles läuft", "Letzte Sicherung", "an einem Ort", "Letzte Änderung", "Paperless wird installiert", "Rückgängig")
}

// A red service is on top, not „Alles in Ordnung“; no backup is said.
func TestBoxSaysWhatIsBroken(t *testing.T) {
	ready := map[string]any{"state": "bereit", "operator": "läuft", "lastBackup": map[string]any{"ok": false, "repos": 0}}
	rows := []map[string]any{{"name": "immich", "title": "Immich", "beschreibung": "Fotos", "state": "rot"}, {"name": "paperless", "state": "grün"}}
	body := get(Box(fakeLaib(t, ready, rows), "https://laib.box.test"), "admins")
	mustContain(t, body, `<span class="punkt rot" role=img aria-label="rot"></span><span class=was><b>Immich</b>`, "Noch keine gültige Sicherung")
	mustNotContain(t, body, "Alles in Ordnung", "<b>paperless</b>")
}

// owner 2026-10-27: a service is named as on Laib's page — its title big,
// what it is beside it, its id small; an older Laib without a title: the id
func TestBoxNamesServicesAsLaibDoes(t *testing.T) {
	rows := []map[string]any{{"name": "media", "title": "Jellyfin", "beschreibung": "Filme, Serien, Musik", "state": "gelb"}, {"name": "alt", "state": "rot"}}
	body := get(BoxWith(fakeLaib(t, map[string]any{"state": "bereit"}, rows), "https://laib.box.test", nil), "admins")
	mustContain(t, body, `<b>Jellyfin</b> <span class=k>Filme, Serien, Musik</span> <small class=k>media</small>`, `<b>alt</b>`)
	mustNotContain(t, body, `<small class=k>alt</small>`)
}
