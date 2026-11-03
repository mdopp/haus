package pages

import (
	"encoding/json"
	"github.com/mdopp/homes/internal/bote"
	"github.com/mdopp/homes/internal/laib"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mdopp/homes/internal/samba"
	"github.com/mdopp/homes/internal/samba/sambafake"
)

// haus#188 (owner 2026-10-28): what a person sets up once is a task — open
// ones in Mehr's „Einrichtung“, the important first, gone once done; Start
// says an important one quietly; Deine Aufgaben lists them all, the done
// ones to do again
func TestEinrichtungKommtUndGeht(t *testing.T) {
	fs := sambafake.New(t, map[string]int64{"anna": 0, "ben": 1777777777}, true)
	dc, _ := discogsFake(t) // anna has connected hers
	cfg := StartConfig{Samba: &samba.Dir{Path: fs.Dir}, Discogs: dc}

	_, body, _ := familyPage(t, Mehr, cfg, "GET", mehrPath, "family")
	e := body[strings.Index(body, "<h2 id=karte-einrichtung>Einrichtung</h2>"):]
	e = e[:strings.Index(e, "</section>")]
	mustContain(t, e, `<a href="/netzlaufwerk">Netzlaufwerk-Passwort festlegen</a>`)
	mustNotContain(t, e, "Discogs verbinden", "Stimme", "Aufnahmen") // connected already; nothing trains a wake word any more
	_, start := startPage(t, cfg, "family")
	mustContain(t, start, `<a href=/aufgaben>1 Sache einzurichten</a>: Netzlaufwerk-Passwort festlegen`)

	// the password set: nothing open, nothing on Start
	if err := cfg.Samba.Set("anna", "ein-gutes-passwort"); err != nil {
		t.Fatal(err)
	}
	_, body, _ = familyPage(t, Mehr, cfg, "GET", mehrPath, "family")
	mustNotContain(t, body, "karte-einrichtung")
	_, start = startPage(t, cfg, "family")
	mustNotContain(t, start, "einzurichten")

	_, body, _ = familyPage(t, DeineAufgaben, cfg, "GET", aufgabenPath, "family")
	mustContain(t, body, "<h1>Deine Aufgaben</h1>", "Alles eingerichtet.", "<h2 id=karte-erledigt>Erledigt</h2>", "Noch einmal: Damit du die Dateien", `<a href="/discogs">Discogs verbinden</a>`)
}

// laib#878: Laib's tasks for the person and the household join Einrichtung
// (the admins' stay on Box); one with a Homes page leads there, one done
// together with the Bote has „Gemeinsam mit Ada“
func TestEinrichtungMitLaibsAufgaben(t *testing.T) {
	named(t, "Ada")
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
 {"id":8,"titel":"Homes-App koppeln","fuer":"alle","weg":{"seite":"/willkommen"}},
 {"id":9,"titel":"RAID prüfen","fuer":"admins"},
 {"id":10,"titel":"Bens Sache","fuer":"person:ben"},
 {"id":11,"titel":"Alte Aufgabe","fuer":"person:anna","erledigt":{"am":"2026-10-27","von":"laib","wie":"Befund weg"}}]}`
		}
		json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"content": []map[string]string{{"text": text}}}})
	}))
	t.Cleanup(srv.Close)
	cfg := StartConfig{Laib: &laib.Client{URL: srv.URL, Token: "t"}, Bote: &bote.Client{}}
	_, body, _ := familyPage(t, Mehr, cfg, "GET", MehrAufgabenPath, "family") // Mehr's card, after the page
	if asked["von"] != "anna" || asked["action"] != "list" {
		t.Errorf("laib asked %v", asked)
	}
	mustContain(t, body, `<a href="/aufgaben#aufgabe-7">Strato-Eintrag für fotos setzen</a>`, `<a href="/willkommen">Homes-App koppeln</a>`)
	mustNotContain(t, body, "RAID prüfen", "Bens Sache", "Alte Aufgabe")
	_, body, _ = familyPage(t, DeineAufgaben, cfg, "GET", aufgabenPath, "family")
	mustContain(t, body, `<section class=karte id=aufgabe-7><h2>Strato-Eintrag für fotos setzen</h2>`,
		`<input type=hidden name=nachricht value="Aufgabe #7 mit dir erledigen"><button data-laeuft="Fragt Ada …">Gemeinsam mit Ada</button>`,
		"<h2 id=karte-erledigt>Erledigt</h2>", "Alte Aufgabe")
}
