package pages

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/mdopp/homes/internal/laib"
)

// overviewLaib is a Laib with these service rows, this many open inbox
// cards and these findings from laib_befunde ("" is unknown tool: an older
// Laib; "kaputt" an error; "nie" not measured yet), measured 12 min ago;
// ready changes the readiness; it records the tools Homes asked for.
func overviewLaib(t *testing.T, services []map[string]any, inbox int, diag string, ready func(map[string]any), asked *[]string) BoxConfig {
	t.Helper()
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		mu.Lock()
		*asked = append(*asked, req.Params.Name)
		mu.Unlock()
		text, isErr := "[]", false
		switch req.Params.Name {
		case "laib_status":
			rd := allGreen(t)
			if ready != nil {
				ready(rd)
			}
			b, _ := json.Marshal(map[string]any{"ready": rd, "services": services})
			text = string(b)
		case "laib_inbox":
			ps := []map[string]any{}
			for i := 0; i < inbox; i++ {
				ps = append(ps, map[string]any{"id": 40 + i, "actor": "pi", "reason": "immich: mehr Speicher", "tier": "apply", "status": "open", "class": "values"})
			}
			b, _ := json.Marshal(ps)
			text = string(b)
		case "laib_befunde":
			at, _ := json.Marshal(time.Now().Add(-12 * time.Minute))
			text, isErr = `{"at":`+string(at)+`,"findings":`+diag+`}`, diag == "" || diag == "kaputt"
			switch diag {
			case "":
				text = "unknown tool laib_befunde"
			case "kaputt":
				text = "kaputt"
			case "nie":
				text = `{"at":null,"findings":[]}`
			}
		case "laib_diag":
			t.Error("the page ran laib diag")
		}
		json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"content": []map[string]string{{"text": text}}, "isError": isErr}})
	}))
	t.Cleanup(srv.Close)
	return BoxConfig{Laib: &laib.Client{URL: srv.URL, Token: "t"}, LaibURL: "https://laib.box.test"}
}

// owner 2026-11-06, „Was braucht mich?“: the Übersicht's top is only what
// needs the admin, red before yellow — services not green, laib diag's
// red and yellow findings (info and a service already shown are not),
// the due Box-Aufgaben, the decisions waiting — and the guarantees below.
func TestBoxOverviewShowsWhatNeedsTheAdmin(t *testing.T) {
	var asked []string
	diag := `[{"severity":"gelb","probe":"bote","detail":"Der Bote antwortet nicht auf die Probe","say":"Signal kommt vielleicht nicht an."},
		{"severity":"rot","probe":"unit","service":"immich","detail":"immich.service ist failed"},
		{"severity":"rot","probe":"raid","detail":"md127: Spiegel fehlt, siehe Box-Aufgabe #3"},
		{"severity":"info","probe":"watchdog","detail":"kein Wächter von außen"}]`
	cfg := overviewLaib(t, []map[string]any{
		{"name": "adguard", "state": "grün", "title": "Werbefilter"},
		{"name": "paperless", "state": "gelb", "title": "Dokumente", "detail": "Redis langsam"},
		{"name": "immich", "state": "rot", "title": "Fotos", "detail": "startet nicht"},
	}, 2, diag, func(r map[string]any) { r["aufgabenFaellig"] = 1 }, &asked)
	page := get(BoxPage(cfg), "admins")
	mustContain(t, page, "<b>Dokumente</b>", "Redis langsam", "<b>Fotos</b>", "startet nicht",
		"<b>Signal kommt vielleicht nicht an.</b>", "Der Bote antwortet nicht", "<b>raid</b>", `<a href="/box/aufgaben#aufgabe-3">Box-Aufgabe #3</a>`,
		"<a href=/box/aufgaben>1 Box-Aufgabe fällig</a>", `<a href="https://laib.box.test/inbox">2 Entscheidungen warten</a>`,
		"Letzte Sicherung", "Letzte Änderung: Paperless wird installiert", "Befunde gemessen vor 12 min", "<h2>Morgenbericht</h2>", `<details id=operator>`)
	mustNotContain(t, page, "Werbefilter", "kein Wächter", "immich.service ist failed", "Alles in Ordnung", "immich: mehr Speicher", "Laib das Verb befunde kennt")
	before(t, page, "<b>Fotos</b>", "<b>raid</b>")              // red first
	before(t, page, "<b>raid</b>", "<b>Dokumente</b>")          // then yellow
	before(t, page, "<b>Signal kommt", "1 Box-Aufgabe fällig")  // the due Aufgaben after the findings
	before(t, page, "1 Box-Aufgabe fällig", "2 Entscheidungen") // the decisions last
	before(t, page, "2 Entscheidungen", "Letzte Sicherung")     // the guarantees below
}

// Nothing needs the admin: one calm line with the guarantees, no list,
// no decision count.
func TestBoxOverviewCalmIsOneLine(t *testing.T) {
	var asked []string
	cfg := overviewLaib(t, []map[string]any{{"name": "adguard", "state": "grün", "title": "Werbefilter"}}, 0, `[{"severity":"info","probe":"watchdog","detail":"kein Wächter"}]`, nil, &asked)
	page := getAt(BoxPage(cfg), "/box/teil?was=stand", "admins")
	mustContain(t, page, "Alles in Ordnung.", "Letzte Sicherung", "Rückgängig", "Befunde gemessen vor 12 min", "Morgenbericht")
	mustNotContain(t, page, "Werbefilter", "zeilen", "Entscheidung", "kein Wächter", "noch nicht gelaufen")
}

// A Laib without befunde (before laib#1085): the red lines laib status
// carries stand in, and the page says yellow findings are missing: never
// „Alles in Ordnung“ over what was not measured. befunde failing is said
// too, and so is a box where laib diag has not run yet.
func TestBoxOverviewWithoutDiag(t *testing.T) {
	var asked []string
	cfg := overviewLaib(t, nil, 0, "", func(r map[string]any) { r["problems"] = []string{"disk: /var zu 97 % voll"} }, &asked)
	page := get(BoxPage(cfg), "admins")
	mustContain(t, page, "disk: /var zu 97 % voll", "sobald Laib das Verb befunde kennt")
	mustNotContain(t, page, "Alles in Ordnung", "Befunde gemessen")
	cfg = overviewLaib(t, nil, 0, "nie", nil, &asked)
	page = get(BoxPage(cfg), "admins")
	mustContain(t, page, "laib diag ist noch nicht gelaufen")
	mustNotContain(t, page, "Alles in Ordnung", "Befunde gemessen", "Laib das Verb befunde kennt")
	cfg = overviewLaib(t, nil, 0, "kaputt", nil, &asked)
	page = get(BoxPage(cfg), "admins")
	mustContain(t, page, "Die Befunde von laib diag lassen sich gerade nicht lesen.")
	mustNotContain(t, page, "Alles in Ordnung")
}

// No journal copy: no Journal page, no journal list, and the overview
// asks Laib for the morning report only.
func TestBoxHasNoJournalCopy(t *testing.T) {
	var asked []string
	cfg := overviewLaib(t, nil, 0, "[]", nil, &asked)
	h := BoxPage(cfg)
	page := get(h, "admins")
	mustContain(t, page, "<h2>Morgenbericht</h2>", `<a href="https://laib.box.test/journal">Das Journal auf der Laib-Seite</a>`)
	mustNotContain(t, page, "Journal · 24h", "box-journal", "href=/box/journal")
	journals := 0
	for _, a := range asked {
		if a == "laib_journal" {
			journals++
		}
	}
	if journals != 1 {
		t.Errorf("asked the journal %d times: %v", journals, asked)
	}
	if rec := boxAt(h, "/box/journal", "admins"); rec.Code != http.StatusNotFound {
		t.Errorf("/box/journal → %d", rec.Code)
	}
}

// Dienste and Operator are on the Übersicht now; their old pages lead there.
func TestBoxOldPagesLeadToTheOverview(t *testing.T) {
	var asked []string
	h := BoxPage(overviewLaib(t, nil, 0, "[]", nil, &asked))
	for from, to := range map[string]string{"/box/dienste": "/box", "/box/operator": "/box#operator"} {
		if rec := boxAt(h, from, "admins"); rec.Code != http.StatusFound || rec.Header().Get("Location") != to {
			t.Errorf("%s → %d %q", from, rec.Code, rec.Header().Get("Location"))
		}
		if rec := boxAt(h, from, "family"); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/start" {
			t.Errorf("family %s → %d", from, rec.Code)
		}
	}
}

// seit: minutes for the first two hours, then hours, then the day.
func TestSeit(t *testing.T) {
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.Local)
	for d, want := range map[time.Duration]string{30 * time.Second: "gerade eben", 12 * time.Minute: "vor 12 min", 119 * time.Minute: "vor 119 min", 5 * time.Hour: "vor 5 h"} {
		if got := seit(now.Add(-d), now); got != want {
			t.Errorf("%v: %q, want %q", d, got, want)
		}
	}
	if got := seit(now.Add(-50*time.Hour), now); got != when(now.Add(-50*time.Hour)) {
		t.Errorf("two days: %q", got)
	}
}
