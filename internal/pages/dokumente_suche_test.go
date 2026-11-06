package pages

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/homes/internal/paperless"
	"github.com/mdopp/homes/internal/paperless/paperlessfake"
)

// dokSucheCfg is a Paperless after dokumente's ordnen with four documents.
func dokSucheCfg(t *testing.T) (*paperlessfake.Server, StartConfig) {
	f := paperlessfake.New("pl")
	t.Cleanup(f.Close)
	f.DocType(1, "Rechnung")
	f.DocType(2, "Versicherungsschein")
	f.DocType(3, "Beleg/Garantie")
	f.Tag(10, "Haus")
	f.Tag(11, "Versicherung")
	f.Correspondent(20, "Stadtwerke")
	f.Correspondent(21, "HUK-Coburg")
	f.Field(30, "Rechnungsnummer", "string")
	f.Field(31, "Versicherungsnummer", "string")
	f.Field(32, "Produkte", "longtext")
	f.Field(33, "Garantie bis", "date")
	f.Field(34, "Person", "select")
	f.Field(35, "Nächster Kündigungstermin", "date")
	f.Field(36, "Kündigungsfrist", "string")
	f.Field(37, "Betrag", "monetary")
	f.Options(34, "Anna", "Ben")
	f.Add(
		paperlessfake.Doc{ID: 1, Title: "Stromrechnung Oktober", DocumentType: 1, Correspondent: 20, Tags: []int{10}, Created: "2026-10-20",
			Fields: map[int]any{30: "RE-4711", 37: "EUR84.50", 34: "opt-Anna"}},
		paperlessfake.Doc{ID: 2, Title: "Kfz-Versicherung 2026", DocumentType: 2, Correspondent: 21, Tags: []int{11}, Created: "2025-11-02",
			Fields: map[int]any{31: "KV 123 456", 32: "Vollkasko\nSchutzbrief", 35: "2027-01-01", 36: "1 Monat"}},
		paperlessfake.Doc{ID: 3, Title: "Kassenbon Elektromarkt", DocumentType: 3, Tags: []int{10}, Created: "2025-03-01",
			Fields: map[int]any{32: "Waschmaschine WM 7", 33: "2027-03-01"}},
		paperlessfake.Doc{ID: 4, Title: "Alter Bon", DocumentType: 3, Created: "2020-01-01", Fields: map[int]any{33: "2022-01-01"}},
	)
	return f, StartConfig{Docs: &paperless.Client{URL: f.URL, Token: "pl"}, PaperlessPublicURL: "https://paperless.box.test", PaperlessLANOnly: true,
		Now: func() time.Time { return time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC) }}
}

func getSuche(cfg StartConfig, path, from string) string {
	req := httptest.NewRequest("GET", path, nil)
	req.Header.Set("Remote-User", "anna")
	req.Header.Set("X-Forwarded-For", from)
	rec := httptest.NewRecorder()
	DokumenteSuche(cfg).ServeHTTP(rec, req)
	return rec.Body.String()
}

// owner 2026-11-06: the Dokumente page opens with the search field, the
// shortcuts, the Bereiche with their counts, the types and the people,
// each a list in Homes.
func TestDokumenteFinden(t *testing.T) {
	_, cfg := dokSucheCfg(t)
	cfg.Ablage = nil
	home := getDokumente(cfg, "/dokumente", map[string]string{"X-Forwarded-For": "192.168.1.23"}).Body.String()
	mustContain(t, home, `<h2 id=finden>Finden</h2><form class=dsuche method=get action=/dokumente/suche role=search><input type=search name=q`,
		`<a href="/dokumente/suche?nur=versicherungen">Versicherungen</a>`, `<a href="/dokumente/suche?nur=garantie">Garantie läuft noch</a>`,
		`<a href="/dokumente/suche?bereich=Haus"><b>Haus</b><span>2 Dokumente</span></a>`, `<b>Versicherung</b><span>1 Dokument</span>`,
		`<a href="/dokumente/suche?typ=Beleg%2FGarantie">Beleg/Garantie <span>2</span></a>`, `<a href="/dokumente/suche?person=Ben">Für Ben</a>`)
	before(t, home, "<h2 id=finden>", "In Paperless öffnen")
}

// A word, a piece of a number or a product finds its document; the row
// says date, sender, type and its number, and opens it in Paperless.
func TestDokumenteSuche(t *testing.T) {
	f, cfg := dokSucheCfg(t)
	page := getSuche(cfg, "/dokumente/suche?q=4711", "192.168.1.23")
	mustContain(t, page, "<h1>Gesucht: „4711“</h1>", `value="4711"`, `<a href="/dokumente/1">Stromrechnung Oktober</a>`,
		"20.10.2026 · Stadtwerke · Rechnung · Rechnung RE-4711 · 84,50 €", `<div id=dsuche-treffer`)
	// the part alone, for the search field's fetch; nothing asked, nothing
	part := getSuche(cfg, "/dokumente/suche?q=waschmasch&teil=1", "192.168.1.23")
	if strings.Contains(part, "<html") || !strings.Contains(part, "Kassenbon Elektromarkt") || strings.Contains(part, "Stromrechnung") {
		t.Errorf("the part:\n%s", part)
	}
	if empty := getSuche(cfg, "/dokumente/suche?teil=1", "192.168.1.23"); empty != "" {
		t.Errorf("nothing asked: %q", empty)
	}
	mustContain(t, getSuche(cfg, "/dokumente/suche?q=123456&teil=1", "192.168.1.23"), "Kfz-Versicherung 2026", "Vers.-Nr. KV 123 456")
	mustContain(t, getSuche(cfg, "/dokumente/suche?q=gibtsnicht&teil=1", "192.168.1.23"), "Nichts gefunden für „gibtsnicht“")
	// a Bereich and a person narrow; the search field keeps them
	haus := getSuche(cfg, "/dokumente/suche?bereich=Haus&person=Anna", "192.168.1.23")
	mustContain(t, haus, "<h1>Haus Für Anna</h1>", `<input type=hidden name=bereich value="Haus"><input type=hidden name=person value="Anna">`, "Stromrechnung Oktober")
	if strings.Contains(haus, "Kassenbon") {
		t.Errorf("Ben's receipt for Anna:\n%s", haus)
	}
	// away from a LAN-only Paperless: said once, the rows without a link
	away := getSuche(cfg, "/dokumente/suche?q=4711", "203.0.113.7")
	mustContain(t, away, "Paperless öffnet sich nur zu Hause im WLAN", "<li>Stromrechnung Oktober <span class=k>")
	// Paperless failing: a plain sentence, not „nichts gefunden“
	f.Status = 500
	cfg.Docs = &paperless.Client{URL: f.URL, Token: "pl"} // a new client: nothing kept
	mustContain(t, getSuche(cfg, "/dokumente/suche?q=4711", "192.168.1.23"), "Paperless antwortet gerade nicht.")
}

// The shortcuts: each insurance by its kind, number and „kündbar bis“; the
// warranties still running.
func TestDokumenteAbkuerzungen(t *testing.T) {
	_, cfg := dokSucheCfg(t)
	v := getSuche(cfg, "/dokumente/suche?nur=versicherungen", "192.168.1.23")
	mustContain(t, v, "<h1>Versicherungen</h1>", `<a href="/dokumente/2">Vollkasko</a>`, "HUK-Coburg · Vers.-Nr. KV 123 456 · kündbar bis 01.12.2026")
	g := getSuche(cfg, "/dokumente/suche?nur=garantie", "192.168.1.23")
	mustContain(t, g, "<h1>Garantie läuft noch</h1>", `>Waschmaschine WM 7</a>`, "Garantie bis 01.03.2027")
	if strings.Contains(g, "Alter Bon") {
		t.Errorf("an ended warranty:\n%s", g)
	}
	mustContain(t, getSuche(StartConfig{}, "/dokumente/suche", ""), "Die Dokumente sind hier noch nicht eingerichtet.")
}
