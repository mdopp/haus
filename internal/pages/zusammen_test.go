package pages

import (
	"net/http"
	"strings"
	"testing"

	"github.com/mdopp/homes/internal/modul"
)

// TestZusammenShowsOnlyWhatTheBoxHas: the web's nodes and the „Das geht“
// moments follow the modules; a moment needing an off module is gone, and
// „Hier anfordern“ appears only while something is off.
func TestZusammenShowsOnlyWhatTheBoxHas(t *testing.T) {
	alle := StartConfig{Module: modulListe(t, "kalender", "kontakte", "konto", "dokumente", "sicherung", "standort", "musik", "fotos", "geraete", "sprechen")}
	code, body, _ := familyPage(t, Zusammen, alle, "GET", ZusammenPath, "family")
	if code != http.StatusOK {
		t.Fatalf("code %d", code)
	}
	mustContain(t, body, "<h1>So hängt alles zusammen</h1>", `<svg class=netz`, "Haushalts-Kalender", "Bank &amp; IBAN", "Zeitleiste", "Wunschliste",
		`href="/kontakte"`, "Von der Rechnung zum Anruf", "Ein Tipp zu allen Dokumenten eines Kontakts", `href="/kalender"`, `href="/standort"`,
		"Waschmaschine fertig, Nachricht in Signal", `href="/raeume"`, "prefers-reduced-motion")
	mustNotContain(t, body, "Etwas fehlt?")

	ms := []modul.Modul{{Name: "kalender"}, {Name: "konto"}, {Name: "standort", Warum: modul.Fehlt, Grund: "kein Reitti"}}
	wenig, err := modul.Bauen(ms)
	if err != nil {
		t.Fatal(err)
	}
	_, body, _ = familyPage(t, Zusammen, StartConfig{Module: wenig}, "GET", ZusammenPath, "family")
	mustContain(t, body, "Termin auf dem Handy verschieben, alle sehen es", "Telefonbuch", "Etwas fehlt? Hier anfordern", `href="/mehr#spuren"`)
	mustNotContain(t, body, "Von der Rechnung zum Anruf", "Google-Zeitachse", "Waschmaschine", "Bank &amp; IBAN", ">Zeitleiste<", ">Fotos<")
	if strings.Count(body, "<line ") != 2 { // Termine and Telefonbuch hang on the contact
		t.Errorf("want 2 lines from the contact:\n%s", body)
	}

	if code, _, _ := familyPage(t, Zusammen, alle, "POST", ZusammenPath, "family"); code != http.StatusMethodNotAllowed {
		t.Errorf("POST: %d", code)
	}
}

// TestMehrLeadsToZusammen: Mehr has the row, with its line.
func TestMehrLeadsToZusammen(t *testing.T) {
	_, body, _ := familyPage(t, Mehr, StartConfig{}, "GET", mehrPath, "family")
	mustContain(t, body, `href="/zusammen"`, "So hängt alles zusammen", "Von der Rechnung zum Anruf: was deine Daten miteinander können")
}
