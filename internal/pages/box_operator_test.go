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

// a Laib that answers status with the skills' last runs, and the skill
// list or, while Haus may not list them, „unknown tool“
func skillLaib(t *testing.T, list []map[string]any, mayList bool) *laib.Client {
	t.Helper()
	ran := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		res := map[string]any{}
		switch req.Params.Name {
		case "laib_status":
			b, _ := json.Marshal(map[string]any{"ready": map[string]any{"state": "bereit", "operatorWork": map[string]any{"skills": map[string]any{
				"routineblick": map[string]any{"at": ran, "outcome": "green", "caused": "eine Notiz"},
				"vorfall":      map[string]any{"at": ran, "outcome": "skipped", "why": "kein roter Dienst"}}}}, "services": []any{}})
			res["content"] = []map[string]string{{"text": string(b)}}
		case "laib_skill":
			if !mayList {
				res["content"], res["isError"] = []map[string]string{{"text": "unknown tool laib_skill"}}, true
				break
			}
			b, _ := json.Marshal(list)
			res["content"] = []map[string]string{{"text": string(b)}}
		default:
			res["content"] = []map[string]string{{"text": "[]"}}
		}
		json.NewEncoder(w).Encode(map[string]any{"result": res})
	}))
	t.Cleanup(srv.Close)
	return &laib.Client{URL: srv.URL, Token: "t"}
}

// owner 2026-10-27: „ich möchte irgendwo die Skills sehen können“ (laib#839)
func TestBoxShowsWhatTheOperatorCan(t *testing.T) {
	list := []map[string]any{
		{"name": "routineblick", "source": "builtin", "description": "Schaut täglich nach, ob alles läuft.", "trigger": map[string]string{"daily": "06:30"}, "modell": "urteil", "status": "active"},
		{"name": "vorfall", "source": "builtin", "description": "Kümmert sich um einen roten Dienst.", "trigger": map[string]string{"event": "red"}, "modell": "frei", "status": "active"},
		{"name": "sicherung", "source": "builtin", "description": "Prüft die Sicherung.", "trigger": map[string]string{"weekly": "Mo 08:00"}, "modell": "nein", "status": "paused"},
		{"name": "eigen", "source": "state", "description": "Ein eigener.", "trigger": map[string]string{"every": "10m"}, "status": "refused", "refused": "das Werkzeug exec ist nicht erlaubt"},
	}
	body := getAt(BoxWith(skillLaib(t, list, true), "https://laib.box.test", nil), "/box/operator?ganz=1", "admins")
	mustContain(t, body, "<h2>Was der Operator kann</h2>",
		"<b>Routineblick</b><span class=text>Schaut täglich nach, ob alles läuft.</span><span class=text>täglich 06:30 · Ein Skript sammelt, das Modell urteilt.</span>",
		"Zuletzt heute, ", ": lief, alles gut. eine Notiz",
		"wenn ein Dienst rot wird · Das Modell löst das Problem selbst.", ": übersprungen (kein roter Dienst)",
		"montags 08:00 · Nur ein Skript, kein Modell. · ruht gerade", "Noch nicht gelaufen",
		"alle 10 Minuten", "Laib lässt ihn nicht laufen: das Werkzeug exec ist nicht erlaubt", "Ein eigener Skill dieser Box",
		`<a href="https://github.com/mdopp/laib/tree/main/skills/routineblick" target=_blank rel=noopener>Der Skill auf GitHub</a>`,
		`<a href=/box/operator aria-current=page>Operator</a>`, "Haus zeigt nur.")
	mustNotContain(t, body, "skills/eigen", "<form", "<button")
	if strings.Contains(getAt(BoxWith(skillLaib(t, list, true), "https://laib.box.test", nil), "/box/operator?ganz=1", "family"), "Was der Operator kann") {
		t.Error("the family sees the skills")
	}
}

// Haus may not list them yet: what ran shows by its name, and the page says why the rest is missing
func TestBoxOperatorWithoutTheGrant(t *testing.T) {
	body := getAt(BoxWith(skillLaib(t, nil, false), "https://laib.box.test", nil), "/box/operator?ganz=1", "admins")
	mustContain(t, body, saysSkillsNoList, "<b>Routineblick</b>", "<b>Vorfall</b>", ": übersprungen (kein roter Dienst)")
	mustNotContain(t, body, "Laib antwortet nicht")
}

func TestSkillOutcomesInWords(t *testing.T) {
	for raw, want := range map[string]string{"green": "lief, alles gut", "unchanged": "lief, nichts Neues", "skipped": "übersprungen",
		"escalated": "an dich weitergegeben", "rejected": "abgewiesen", "error": "schiefgegangen"} {
		if skillOutcome[raw] != want {
			t.Errorf("%s → %q", raw, skillOutcome[raw])
		}
	}
	if _, ok := skillOutcome["neu"]; ok {
		t.Error("an unknown word is shown as it comes")
	}
}
