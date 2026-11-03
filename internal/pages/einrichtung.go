package pages

import (
	"context"
	"errors"
	"html"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mdopp/homes/internal/assistent"
	"github.com/mdopp/homes/internal/cards"
	"github.com/mdopp/homes/internal/laib"
)

// Einrichtung (haus#188; owner 2026-10-28: „es gibt häufige/regelmäßige und
// einmalige/seltene Dinge“): what a person sets up once is a task, not a
// page in the menu. Mehr shows the open ones at the top, each with its
// guided page; Start says it quietly while an important one is open;
// Deine Aufgaben lists them all, the done ones to do again. Done is what
// Homes sees, never a tick.
const aufgabenPath = "/aufgaben"

// aufgabe is one thing for one person to set up.
type aufgabe struct {
	titel, warum string
	stand        string // how far, „2 von 5 Aufnahmen“; "" for none
	href         string // the page that leads through it
	gespraech    int    // a Box-Aufgabe done together with the Bote: its id (laib#878)
	wichtig      bool   // security and access before the optional
	erledigt     bool
}

// mehrLaibWait is how long Mehr waits for Laib's tasks when it has none
// kept yet (owner 2026-11-03: /mehr took seconds). Laib's aufgabe verb takes
// its MCP lock alone and waits behind any tool running; the read goes on
// without the page, and the next visit has it (laib.KeptAufgaben).
var mehrLaibWait = 500 * time.Millisecond

// mitLaib are the person's tasks with Laib's own for them (laib#878): the
// household's and theirs; the admins' stay on Box. Laib away: Homes' own.
// kept (Mehr) takes the list Homes keeps for a minute and waits at most
// mehrLaibWait for one; Deine Aufgaben asks Laib each time.
func mitLaib(r *http.Request, cfg StartConfig, kept bool) []aufgabe {
	as := einrichtung(r, cfg)
	user := r.Header.Get("Remote-User")
	if cfg.Laib == nil || user == "" {
		return as
	}
	var la laib.Aufgaben
	var err error
	if kept {
		ctx, cancel := context.WithTimeout(r.Context(), mehrLaibWait)
		var ok bool
		la, ok, err = cfg.Laib.KeptAufgaben(ctx, user)
		cancel()
		if !ok && err == nil {
			err = errors.New("no list yet")
		}
	} else {
		fetch(r, func(ctx context.Context) { la, err = cfg.Laib.MeineAufgaben(ctx, user) })
	}
	if err != nil {
		if !errors.Is(err, laib.ErrNoTool) {
			log.Printf("homes: einrichtung: laib: %v", err)
		}
		return as
	}
	var mine []aufgabe
	for _, t := range la.Aufgaben {
		own := t.Fuer == "person:"+user
		if !own && t.Fuer != "alle" {
			continue
		}
		a := aufgabe{titel: t.Titel, warum: t.Warum, wichtig: own, erledigt: t.Erledigt != nil}
		if t.Weg != nil && strings.HasPrefix(t.Weg.Seite, "/") && !strings.HasPrefix(t.Weg.Seite, "//") {
			a.href = t.Weg.Seite
		}
		if t.Weg != nil && t.Weg.Gespraech && cfg.Bote != nil {
			a.gespraech = t.ID
		}
		if a.href == "" {
			a.href = aufgabenPath + "#aufgabe-" + strconv.Itoa(t.ID)
		}
		mine = append(mine, a)
	}
	all := append(mine, as...)
	var wichtig, rest []aufgabe
	for _, a := range all {
		if a.wichtig {
			wichtig = append(wichtig, a)
		} else {
			rest = append(rest, a)
		}
	}
	return append(wichtig, rest...)
}

// mitAssistent is a task's „Gemeinsam mit <name>“: the Sprechen field's
// post, the Bote reads the task and leads through it (laib#878).
func mitAssistent(id int) string {
	return `<form method=post action=/sprechen><input type=hidden name=nachricht value="Aufgabe #` + strconv.Itoa(id) + ` mit dir erledigen">` +
		`<button data-laeuft="Fragt ` + html.EscapeString(assistent.Name()) + ` …">Gemeinsam mit ` + html.EscapeString(assistent.Name()) + `</button></form>`
}

// einrichtung are the person's tasks this box has, the important first.
func einrichtung(r *http.Request, cfg StartConfig) []aufgabe {
	user := r.Header.Get("Remote-User")
	if user == "" {
		return nil
	}
	var out []aufgabe
	if cfg.Samba != nil { // the network drive's own password
		if accounts, err := cfg.Samba.Accounts(); err == nil {
			if seit, member := accounts[user]; member {
				out = append(out, aufgabe{titel: "Netzlaufwerk-Passwort festlegen", warum: "Damit du die Dateien des Hauses am Computer öffnen kannst.",
					href: netzlaufwerkPath, wichtig: true, erledigt: !seit.IsZero()})
			}
		}
	}
	if cfg.Discogs != nil { // the household's Discogs app is there; each person connects their own
		_, verbunden := cfg.Discogs.Konto(user)
		out = append(out, aufgabe{titel: "Discogs verbinden", warum: "Dann weiß Homes, welche Platten du hast, und deine Wünsche kommen auf deine Wantlist.",
			href: DiscogsPath, erledigt: verbunden})
	}
	// the important first, then as they came
	var wichtig, rest []aufgabe
	for _, a := range out {
		if a.wichtig {
			wichtig = append(wichtig, a)
		} else {
			rest = append(rest, a)
		}
	}
	return append(wichtig, rest...)
}

func offen(as []aufgabe) []aufgabe {
	var out []aufgabe
	for _, a := range as {
		if !a.erledigt {
			out = append(out, a)
		}
	}
	return out
}

// einrichtungKarte is Mehr's „Einrichtung“: the open tasks only; none, no card.
func einrichtungKarte(r *http.Request, cfg StartConfig) string {
	open := offen(mitLaib(r, cfg, true))
	if len(open) == 0 {
		return ""
	}
	c := cards.Card{Title: "Einrichtung"}
	for _, a := range open {
		says := a.warum
		if a.stand != "" {
			says = a.stand + " · " + says
		}
		if !a.wichtig {
			says = "Wenn du magst: " + says
		}
		c.AddLink(a.titel, says, a.href)
	}
	return c.HTML()
}

// einrichtungZeile is Start's quiet line while an important task is open.
func einrichtungZeile(r *http.Request, cfg StartConfig) string {
	var wichtig []string
	for _, a := range offen(einrichtung(r, cfg)) { // Homes' own only: Start never waits for Laib (haus#67)
		if a.wichtig {
			wichtig = append(wichtig, a.titel)
		}
	}
	if len(wichtig) == 0 {
		return ""
	}
	n := map[bool]string{true: "1 Sache einzurichten", false: strconv.Itoa(len(wichtig)) + " Sachen einzurichten"}[len(wichtig) == 1]
	return `<p class=ruhig><span class="punkt gelb"></span><a href=` + aufgabenPath + `>` + n + `</a>: ` + html.EscapeString(strings.Join(wichtig, ", ")) + `</p>`
}

// DeineAufgaben is the page Deine Aufgaben: what is open, and what is done, to do again.
func DeineAufgaben(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != aufgabenPath {
			missing{http.StatusNotFound, "Diese Seite gibt es nicht.", "Mehr", mehrPath, "Mehr"}.write(w, r, cfg)
			return
		}
		if !showOnly(w, r, "Deine Aufgaben") {
			return
		}
		as := mitLaib(r, cfg, false)
		b := familyHead(r, cfg, "Deine Aufgaben", "Aufgaben")
		b.WriteString("<h1>Deine Aufgaben</h1><p class=k>Was du einmal einrichtest. Ist es erledigt, verschwindet es aus Mehr; hier kannst du es noch einmal machen.</p>")
		if len(as) == 0 {
			b.WriteString("<p class=k>Auf dieser Box gibt es für dich gerade nichts einzurichten.</p>")
		}
		var open, done cards.Card
		open.Title, done.Title = "Offen", "Erledigt"
		for _, a := range as {
			says := a.warum
			if a.stand != "" {
				says = a.stand + " · " + says
			}
			if a.erledigt {
				done.AddLink(a.titel, "Noch einmal: "+says, a.href)
			} else {
				open.AddLink(a.titel, says, a.href)
			}
		}
		if len(open.Rows) > 0 {
			b.WriteString(open.HTML())
			for _, a := range as { // a task done together with the Bote: its button (laib#878)
				if !a.erledigt && a.gespraech > 0 {
					b.WriteString(`<section class=karte id=aufgabe-` + strconv.Itoa(a.gespraech) + `><h2>` + html.EscapeString(a.titel) + `</h2>`)
					if a.warum != "" {
						b.WriteString(`<p class=k>` + html.EscapeString(a.warum) + `</p>`)
					}
					b.WriteString(`<p>` + html.EscapeString(assistent.Name()) + ` führt dich Schritt für Schritt hindurch und prüft am Ende, ob es geklappt hat.</p>` + mitAssistent(a.gespraech) + `</section>`)
				}
			}
		} else if len(as) > 0 {
			b.WriteString("<p class=k>Alles eingerichtet.</p>")
		}
		if len(done.Rows) > 0 {
			b.WriteString(done.HTML())
		}
		writePage(w, http.StatusOK, b)
	}
}
