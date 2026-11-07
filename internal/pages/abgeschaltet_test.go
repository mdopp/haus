package pages

import (
	"net/http"
	"testing"

	"github.com/mdopp/homes/internal/modul"
)

// A module switched off on purpose (MODULE_AUS) is never shown to the
// family nor offered for request, also when its backend is gone too
// (owner 2026-11-07); one only missing keeps its trace. The admins see it
// under Box → Haushalt → Module with its reason.
func TestAbgeschaltetNieFuerDieFamilie(t *testing.T) {
	l, err := modul.Bauen([]modul.Modul{
		{Name: "dokumente", Titel: "Dokumente", Warum: modul.Aus, Grund: "Auf dieser Box ausgeschaltet. Auf dieser Box läuft kein Paperless."},
		{Name: "standort", Titel: "Standortverlauf", Warum: modul.Fehlt, Grund: "kein Reitti"},
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := StartConfig{Module: l}
	_, body, _ := familyPage(t, Mehr, cfg, "GET", mehrPath, "family")
	mustContain(t, body, `id="spur-standort"`)
	mustNotContain(t, body, `id="spur-dokumente"`, `value="dokumente"`)
	if code, _, _ := familyPage(t, Dokumente, cfg, "GET", DokumentePath, "family"); code != http.StatusNotFound {
		t.Fatalf("its page for the family → %d", code)
	}
	if code, _, _ := familyPage(t, Anfordern, cfg, "POST", AnfordernPath+"?modul=dokumente", "family"); code == http.StatusOK || code == http.StatusSeeOther {
		t.Fatalf("a wish for it → %d", code)
	}
	page := getAt(BoxPage(BoxConfig{Family: cfg}), "/box/module?ganz=1", "admins")
	mustContain(t, page, "Dokumente", "Auf dieser Box ausgeschaltet. Auf dieser Box läuft kein Paperless.")
}
