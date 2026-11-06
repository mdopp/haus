package pages

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/homes/internal/laib"
)

// a Laib that answers status with the skills' last runs, and the skill
// list or, while Homes may not list them, „unknown tool“
func skillLaib(t *testing.T, list []map[string]any, mayList bool) *laib.Client {
	t.Helper()
	// earlier today in the page's clock (time.Local), also just after midnight: two hours back was yesterday then
	now := time.Now()
	ran := now.Add(-now.Sub(time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)) / 2).UTC().Format(time.RFC3339)
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
				"vorfall":      map[string]any{"at": ran, "outcome": "skipped", "why": "kein roter Dienst"},
				"sicherung":    map[string]any{"at": ran, "outcome": "error", "why": "restic fehlt"}}}}, "services": []any{}})
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
	body := get(BoxWith(skillLaib(t, list, true), "https://laib.box.test", nil), "admins")
	mustContain(t, body, "<details id=operator><summary>Operator: <span class=gelb>unbekannt</span></summary>",
		"<b>Routineblick</b><span class=text>Schaut täglich nach, ob alles läuft.</span><span class=text>täglich 06:30 · Ein Skript sammelt, das Modell urteilt.</span>",
		"Zuletzt heute, ", ": lief, alles gut. eine Notiz",
		"wenn ein Dienst rot wird · Das Modell löst das Problem selbst.", ": übersprungen (kein roter Dienst)",
		"montags 08:00 · Nur ein Skript, kein Modell. · ruht gerade", ": schiefgegangen (restic fehlt)",
		"alle 10 Minuten", "Laib lässt ihn nicht laufen: das Werkzeug exec ist nicht erlaubt", "Ein eigener Skill dieser Box",
		`<a href="https://github.com/mdopp/laib/tree/main/skills/routineblick" target=_blank rel=noopener>Der Skill auf GitHub</a>`,
		"Homes zeigt nur.")
	mustNotContain(t, body, "skills/eigen", "<form", "<button")
	// owner 2026-11-06: first only the skills whose last run was not good (here: the refused one), „Alle Skills zeigen“ the rest
	mustContain(t, body, `<input type=checkbox id=alle-skills><label for=alle-skills>Alle Skills zeigen</label>`, `#alle-skills:not(:checked)~ul .gut{display:none}`,
		`<li class=gut><span class="punkt gruen" role=img aria-label="aktiv"></span><span class=was><b>Routineblick</b>`, `<li><span class="punkt rot" role=img aria-label="verweigert"></span><span class=was><b>Eigen</b>`,
		`<li><span class="punkt k" role=img aria-label="ruht"></span><span class=was><b>Sicherung</b>`, `<li class=gut><span class="punkt gruen" role=img aria-label="aktiv"></span><span class=was><b>Vorfall</b>`, "Noch nicht gelaufen")
	mustNotContain(t, body, "Alle Skills liefen zuletzt gut.")
	if strings.Contains(getAt(BoxWith(skillLaib(t, list, true), "https://laib.box.test", nil), "/box?ganz=1", "family"), "Routineblick") {
		t.Error("the family sees the skills")
	}
}

// Homes may not list them yet: what ran shows by its name, and the page says why the rest is missing
func TestBoxOperatorWithoutTheGrant(t *testing.T) {
	body := get(BoxWith(skillLaib(t, nil, false), "https://laib.box.test", nil), "admins")
	mustContain(t, body, saysSkillsNoList, "<b>Routineblick</b>", "<b>Vorfall</b>", ": übersprungen (kein roter Dienst)", `<li><span class="punkt k" role=img aria-label="unbekannt"></span><span class=was><b>Sicherung</b>`)
	mustNotContain(t, body, "Alle Skills liefen zuletzt gut.")
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
