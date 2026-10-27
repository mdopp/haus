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

// fakeLaib answers `laib status --json` over MCP with the given readiness and rows.
func fakeLaib(t *testing.T, ready map[string]any, rows []map[string]any) *laib.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		text := "[]"
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

// getAt is a Box page (haus: Übersicht, Aufgaben, Dienste, Personen,
// Nutzung), whole.
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

// haus#2, haus#35: the three guarantees in the admin's words are the Box
// page's; the family's Start says the box in one calm line (TestStartBoxLine).
func TestBoxShowsTheThreeGuarantees(t *testing.T) {
	now := time.Now()
	ready := map[string]any{"state": "bereit", "services": 2, "green": 2, "operator": "läuft",
		"lastBackup": map[string]any{"at": now.Add(-3 * time.Hour).UTC().Format(time.RFC3339), "repos": 1, "ok": true},
		"lastChange": map[string]any{"proposal": 4, "summary": "Paperless wird installiert — unter paperless.box.test nur im Heimnetz erreichbar", "at": now.Add(-20 * time.Hour).UTC().Format(time.RFC3339), "reversible": true}}
	rows := []map[string]any{{"name": "immich", "state": "grün"}, {"name": "paperless", "state": "grün"}}
	admin := get(Box(fakeLaib(t, ready, rows), "https://laib.box.test"), "admins,family")
	for _, want := range []string{"Alles läuft", "Letzte Sicherung", "an einem Ort", "Letzte Änderung", "Paperless wird installiert", "Rückgängig", "https://laib.box.test/inbox"} {
		if !strings.Contains(admin, want) {
			t.Fatalf("the Box page lacks %q:\n%s", want, admin)
		}
	}
	// the family's Start has none of it
	_, body := startPage(t, StartConfig{Laib: fakeLaib(t, ready, rows), LaibURL: "https://laib.box.test"}, "family,admins")
	mustNotContain(t, body, "Alles läuft", "Letzte Sicherung", "an einem Ort", "Letzte Änderung", "Paperless wird installiert", "Rückgängig")
}

// haus#2: a red service in family words, and someone is on it; a backup
// older than three days is red.
func TestStartPageSaysWhatIsBrokenInFamilyWords(t *testing.T) {
	ready := map[string]any{"state": "bereit", "services": 2, "green": 1, "red": 1, "operator": "läuft",
		"lastBackup": map[string]any{"at": time.Now().Add(-80 * time.Hour).UTC().Format(time.RFC3339), "repos": 1, "ok": true}}
	rows := []map[string]any{{"name": "immich", "title": "Immich", "beschreibung": "Fotos", "state": "rot"}, {"name": "paperless", "state": "grün"}}
	body := get(Box(fakeLaib(t, ready, rows), "https://laib.box.test"), "admins")
	if strings.Contains(body, "Alles läuft") {
		t.Fatal("a red service is not 'Alles läuft'")
	}
	for _, want := range []string{"Immich (Fotos)", "nicht erreichbar", "Operator schaut nach", "class=rot>Letzte Sicherung"} {
		if !strings.Contains(body, want) {
			t.Fatalf("start page lacks %q:\n%s", want, body[:800])
		}
	}
	ready["operator"] = "aus"
	body = get(Box(fakeLaib(t, ready, rows), "https://laib.box.test"), "admins")
	if !strings.Contains(body, "ist informiert") {
		t.Fatal("without the operator, the admin is informed")
	}
	none := map[string]any{"state": "bereit", "services": 1, "green": 1, "operator": "aus", "lastBackup": map[string]any{"ok": false, "repos": 0}}
	body = get(Box(fakeLaib(t, none, rows[1:]), "https://laib.box.test"), "admins")
	if !strings.Contains(body, "class=rot>Letzte Sicherung") || !strings.Contains(body, "noch keine") {
		t.Fatal("no backup at all is red")
	}
}

// owner 2026-10-27: a service is named as on Laib's page — its title big,
// what it is beside it, its id small; an older Laib without a title: the id
func TestBoxNamesServicesAsLaibDoes(t *testing.T) {
	rows := []map[string]any{{"name": "media", "title": "Jellyfin", "beschreibung": "Filme, Serien, Musik", "state": "gelb"}, {"name": "alt", "state": "rot"}}
	body := getAt(BoxWith(fakeLaib(t, map[string]any{"state": "bereit"}, rows), "https://laib.box.test", nil), "/box/dienste?ganz=1", "admins")
	mustContain(t, body, `<b>Jellyfin</b> <span class=k>Filme, Serien, Musik</span> <small class=k>media</small>`, `<b>alt</b>`)
	mustNotContain(t, body, `<small class=k>alt</small>`)
}
