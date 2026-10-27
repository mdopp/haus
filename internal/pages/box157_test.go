package pages

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/mdopp/haus/internal/laib"
)

// a Laib with these service rows and this many open inbox cards; it
// records the tools Haus asked for
func dedupLaib(t *testing.T, services []map[string]any, inbox int, asked *[]string) BoxConfig {
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
		text := "[]"
		switch req.Params.Name {
		case "laib_status":
			b, _ := json.Marshal(map[string]any{"ready": allGreen(t), "services": services})
			text = string(b)
		case "laib_inbox":
			ps := []map[string]any{}
			for i := 0; i < inbox; i++ {
				ps = append(ps, map[string]any{"id": 40 + i, "actor": "pi", "reason": "immich: mehr Speicher", "tier": "apply", "status": "open", "class": "values"})
			}
			b, _ := json.Marshal(ps)
			text = string(b)
		}
		json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"content": []map[string]string{{"text": text}}}})
	}))
	t.Cleanup(srv.Close)
	return BoxConfig{Laib: &laib.Client{URL: srv.URL, Token: "t"}, LaibURL: "https://laib.box.test"}
}

// haus#157: Laib's own page has the full service list; /box/dienste shows
// only what is not green, by the names Laib's page uses (#156), and the
// way to Laib's page.
func TestBoxDiensteShowsOnlyWhatIsNotGreen(t *testing.T) {
	var asked []string
	cfg := dedupLaib(t, []map[string]any{
		{"name": "adguard", "state": "grün", "title": "Werbefilter"},
		{"name": "paperless", "state": "gelb", "title": "Dokumente", "detail": "Redis langsam"},
		{"name": "immich", "state": "rot", "title": "Fotos", "detail": "startet nicht"},
	}, 0, &asked)
	page := getAt(BoxPage(cfg), "/box/dienste?ganz=1", "admins")
	mustContain(t, page, "<h1>Dienste</h1>", "<b>Dokumente</b>", "Redis langsam", "<b>Fotos</b>", "startet nicht",
		`<a href="https://laib.box.test/">Alle Dienste auf der Laib-Seite</a>`)
	mustNotContain(t, page, "Werbefilter", "adguard", "Was läuft", "Alle Dienste laufen")
	before(t, page, "<b>Fotos</b>", "<b>Dokumente</b>") // red first
}

// All green: one line saying so, and the way to Laib's page.
func TestBoxDiensteAllGreenIsOneLine(t *testing.T) {
	var asked []string
	cfg := dedupLaib(t, []map[string]any{{"name": "adguard", "state": "grün", "title": "Werbefilter"}}, 0, &asked)
	page := getAt(BoxPage(cfg), "/box/dienste?ganz=1", "admins")
	mustContain(t, page, "Alle Dienste laufen.", `<a href="https://laib.box.test/">Alle Dienste auf der Laib-Seite</a>`)
	mustNotContain(t, page, "Werbefilter", "<ul class=zeilen>")
}

// The Inbox on Box is a count and the way to Laib's inbox, where
// deciding happens; the cards themselves are Laib's page's.
func TestBoxInboxIsACountAndTheLink(t *testing.T) {
	link := `<a href="https://laib.box.test/inbox">Entscheiden auf der Laib-Seite</a>`
	for n, want := range map[int]string{0: "Keine Karte wartet auf eine Entscheidung.", 1: "1 Karte wartet auf eine Entscheidung.", 3: "3 Karten warten auf eine Entscheidung."} {
		var asked []string
		page := get(BoxPage(dedupLaib(t, nil, n, &asked)), "admins")
		mustContain(t, page, "<h2>Inbox</h2>", want, link)
		mustNotContain(t, page, "immich: mehr Speicher", "#40", "<table>")
	}
}

// No journal copy: no Journal page, no journal list, and the overview
// asks Laib for the morning report only.
func TestBoxHasNoJournalCopy(t *testing.T) {
	var asked []string
	cfg := dedupLaib(t, nil, 0, &asked)
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
