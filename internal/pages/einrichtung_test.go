package pages

import (
	"encoding/json"
	"github.com/mdopp/haus/internal/bote"
	"github.com/mdopp/haus/internal/laib"
	"net/http"
	"net/http/httptest"
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

// laib#878: Laib's tasks for the person and the household join Einrichtung
// (the admins' stay on Box); one with a Haus page leads there, one done
// together with the Bote has „Gemeinsam mit Joshua“
func TestEinrichtungMitLaibsAufgaben(t *testing.T) {
	old := House
	House = persona.Persona{Display: "Joshua"}
	t.Cleanup(func() { House = old })
	var asked map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Params struct {
				Name string         `json:"name"`
				Args map[string]any `json:"arguments"`
			} `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		text := "[]"
		if req.Params.Name == "laib_aufgabe" {
			asked = req.Params.Args
			text = `{"offen":4,"faellig":0,"aufgaben":[
 {"id":7,"titel":"Strato-Eintrag für fotos setzen","warum":"Ohne ihn ist Immich von außen nicht erreichbar.","fuer":"person:anna","weg":{"gespraech":true},"fertig_wenn":"route photos"},
 {"id":8,"titel":"Haus-App koppeln","fuer":"alle","weg":{"seite":"/willkommen"}},
 {"id":9,"titel":"RAID prüfen","fuer":"admins"},
 {"id":10,"titel":"Bens Sache","fuer":"person:ben"},
 {"id":11,"titel":"Alte Aufgabe","fuer":"person:anna","erledigt":{"am":"2026-10-27","von":"laib","wie":"Befund weg"}}]}`
		}
		json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"content": []map[string]string{{"text": text}}}})
	}))
	t.Cleanup(srv.Close)
	cfg := StartConfig{Laib: &laib.Client{URL: srv.URL, Token: "t"}, Bote: &bote.Client{}}
	_, body, _ := familyPage(t, Mehr, cfg, "GET", mehrPath, "family")
	if asked["von"] != "anna" || asked["action"] != "list" {
		t.Errorf("laib asked %v", asked)
	}
	mustContain(t, body, `<a href="/aufgaben#aufgabe-7">Strato-Eintrag für fotos setzen</a>`, `<a href="/willkommen">Haus-App koppeln</a>`)
	mustNotContain(t, body, "RAID prüfen", "Bens Sache", "Alte Aufgabe")
	_, body, _ = familyPage(t, DeineAufgaben, cfg, "GET", aufgabenPath, "family")
	mustContain(t, body, `<section class=karte id=aufgabe-7><h2>Strato-Eintrag für fotos setzen</h2>`,
		`<input type=hidden name=nachricht value="Aufgabe #7 mit dir erledigen"><button data-laeuft="Fragt Joshua …">Gemeinsam mit Joshua</button>`,
		"<h2 id=karte-erledigt>Erledigt</h2>", "Alte Aufgabe")
}
