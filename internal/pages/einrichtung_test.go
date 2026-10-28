package pages

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mdopp/haus/internal/persona"
	"github.com/mdopp/haus/internal/samba"
	"github.com/mdopp/haus/internal/samba/sambafake"
)

// haus#188 (owner 2026-10-28): what a person sets up once is a task — open
// ones in Mehr's „Einrichtung“, the important first, gone once done; Start
// says an important one quietly; Deine Aufgaben lists them all, the done
// ones to do again
func TestEinrichtungKommtUndGeht(t *testing.T) {
	old := House
	House = persona.Persona{Display: "Joshua", Phrase: "Hey Joshua", Wakeword: "hey_joshua"}
	t.Cleanup(func() { House = old })
	fs := sambafake.New(t, map[string]int64{"anna": 0, "ben": 1777777777}, true)
	wake := t.TempDir()
	dc, _ := discogsFake(t) // anna has connected hers
	cfg := StartConfig{Samba: &samba.Dir{Path: fs.Dir}, WakeRecordings: wake, Discogs: dc}

	_, body, _ := familyPage(t, Mehr, cfg, "GET", mehrPath, "family")
	e := body[strings.Index(body, "<h2 id=karte-einrichtung>Einrichtung</h2>"):]
	e = e[:strings.Index(e, "</section>")]
	mustContain(t, e, `<a href="/netzlaufwerk">Netzlaufwerk-Passwort festlegen</a>`, `<a href="/stimme">Joshua deine Stimme beibringen</a>`, "Wenn du magst: Fünfmal „Hey Joshua“ sagen")
	mustNotContain(t, e, "Discogs verbinden") // connected already
	if strings.Index(e, "Netzlaufwerk") > strings.Index(e, "Stimme") {
		t.Error("the important one is not first")
	}
	mustNotContain(t, mehrOf(t, body), ">Stimme<") // no page of its own any more
	_, start := startPage(t, cfg, "family")
	mustContain(t, start, `<a href=/aufgaben>1 Sache einzurichten</a>: Netzlaufwerk-Passwort festlegen`)

	// two takes: the progress; five and the password: nothing open, nothing on Start
	for _, n := range []string{"anna-1.wav", "anna-2.wav"} {
		os.WriteFile(filepath.Join(wake, n), []byte("x"), 0o600)
	}
	_, body, _ = familyPage(t, Mehr, cfg, "GET", mehrPath, "family")
	mustContain(t, body, "2 von 5 Aufnahmen · Fünfmal")
	for _, n := range []string{"anna-3.wav", "anna-4.wav", "anna-5.wav"} {
		os.WriteFile(filepath.Join(wake, n), []byte("x"), 0o600)
	}
	if err := cfg.Samba.Set("anna", "ein-gutes-passwort"); err != nil {
		t.Fatal(err)
	}
	_, body, _ = familyPage(t, Mehr, cfg, "GET", mehrPath, "family")
	mustNotContain(t, body, "karte-einrichtung")
	_, start = startPage(t, cfg, "family")
	mustNotContain(t, start, "einzurichten")

	_, body, _ = familyPage(t, DeineAufgaben, cfg, "GET", aufgabenPath, "family")
	mustContain(t, body, "<h1>Deine Aufgaben</h1>", "Alles eingerichtet.", "<h2 id=karte-erledigt>Erledigt</h2>", "Noch einmal: Fünfmal „Hey Joshua“", `<a href="/discogs">Discogs verbinden</a>`)
}
