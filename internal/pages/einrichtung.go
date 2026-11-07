package pages

import (
	"context"
	"errors"
	"html"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/mdopp/homes/internal/assistent"
	"github.com/mdopp/homes/internal/cards"
	"github.com/mdopp/homes/internal/laib"
)

// Einrichtung (haus#188; owner 2026-10-28: „es gibt häufige/regelmäßige und
// einmalige/seltene Dinge“): what a person sets up once is a task, not a
// page in the menu. Mehr shows the open ones at the top, each with its
// guided page; Start says it quietly while an important one is open;
// Deine Aufgaben lists them all, the done ones to do again. Done is what
// Homes sees; a module's app, where nothing can tell, the person's tick.
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

// mitLaib are the person's tasks with Laib's own for them (laib#878): the
// household's and theirs; the admins' stay on Box. Laib away: Homes' own.
// Deine Aufgaben asks Laib each time; Mehr never waits for it (einrichtungKarte).
func mitLaib(r *http.Request, cfg StartConfig) []aufgabe {
	as := einrichtung(r, cfg)
	user := r.Header.Get("Remote-User")
	if cfg.Laib == nil || user == "" {
		return as
	}
	var la laib.Aufgaben
	var err error
	fetch(r, func(ctx context.Context) { la, err = cfg.Laib.MeineAufgaben(ctx, user) })
	if err != nil {
		logLaibAufgaben(err)
		return as
	}
	return mitLaibs(r, cfg, as, la)
}

func logLaibAufgaben(err error) {
	if !errors.Is(err, laib.ErrNoTool) {
		log.Printf("homes: einrichtung: laib: %v", err)
	}
}

// mitLaibs joins Laib's list la to Homes' own as, the important first.
func mitLaibs(r *http.Request, cfg StartConfig, as []aufgabe, la laib.Aufgaben) []aufgabe {
	user := r.Header.Get("Remote-User")
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
	for _, a := range cfg.Einrichtung.Fuer(user) { // each module's own app, and the Homes app (owner 2026-11-04)
		out = append(out, aufgabe{titel: a.Titel, warum: a.Warum, href: einrichtenHref(a.Modul), erledigt: a.Erledigt != nil})
	}
	for _, a := range cfg.Einrichtung.Fragen(user) { // the key owner's questions in Immich, while open
		out = append(out, aufgabe{titel: a.Titel, warum: a.Warum, href: DieselbePath})
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

// MehrAufgabenPath is where Mehr's Einrichtung card fills itself from
// (owner 2026-11-03: „Warum werden die Aufgaben nicht async geladen?“).
const MehrAufgabenPath = "/mehr/aufgaben"

// What the Einrichtung card says of Laib's tasks while they are read, and
// when they cannot be; the row leads to Deine Aufgaben, script or not.
const (
	saysAufgabenLesen = "Deine Aufgaben von der Box werden gerade gelesen …"
	saysAufgabenNicht = "Die Aufgaben von der Box lassen sich gerade nicht lesen."
	aufgabenRowTitle  = "Alle Aufgaben"
)

// einrichtungKarte is Mehr's „Einrichtung“: the open tasks only; none, no
// card. Mehr never waits for Laib: with Laib's list kept (laib.KeptAufgaben,
// a minute) the card is whole; without, it shows Homes' own and a row that
// leads to Deine Aufgaben, and fills itself from MehrAufgabenPath.
func einrichtungKarte(r *http.Request, cfg StartConfig) string {
	user := r.Header.Get("Remote-User")
	if cfg.Laib == nil || user == "" {
		return einrichtungCard(offen(einrichtung(r, cfg)), "")
	}
	if la, ok := cfg.Laib.PeekAufgaben(user); ok {
		return einrichtungCard(offen(mitLaibs(r, cfg, einrichtung(r, cfg), la)), "")
	}
	own := offen(einrichtung(r, cfg))
	return nachladen(MehrAufgabenPath, einrichtungCard(own, saysAufgabenLesen), einrichtungCard(own, saysAufgabenNicht))
}

// mehrAufgaben answers MehrAufgabenPath: the Einrichtung card with Laib's
// tasks, once Laib has answered (the list kept a minute behind it), or with
// the line that it cannot be read; "" when nothing is open.
func mehrAufgaben(w http.ResponseWriter, r *http.Request, cfg StartConfig) {
	if !showOnly(w, r, "Mehr") {
		return
	}
	user := r.Header.Get("Remote-User")
	card := einrichtungCard(offen(einrichtung(r, cfg)), "")
	if cfg.Laib != nil && user != "" {
		ctx, cancel := context.WithTimeout(r.Context(), backendWait)
		la, ok, err := cfg.Laib.KeptAufgaben(ctx, user)
		cancel()
		switch {
		case ok:
			card = einrichtungCard(offen(mitLaibs(r, cfg, einrichtung(r, cfg), la)), "")
		case errors.Is(err, laib.ErrNoTool): // an older Laib: Homes' own only
		default:
			if err == nil {
				err = context.DeadlineExceeded
			}
			logLaibAufgaben(err)
			card = einrichtungCard(offen(einrichtung(r, cfg)), saysAufgabenNicht)
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write([]byte(card))
}

// einrichtungCard is the card for open; laibSays, when set, is a last row
// for Laib's tasks not in hand, leading to Deine Aufgaben.
func einrichtungCard(open []aufgabe, laibSays string) string {
	c := cards.Card{Title: "Deine Aufgaben"} // one name for them, Mehr's list and the page (the audit)
	for _, a := range open {
		says := a.warum
		if a.stand != "" {
			says = a.stand + " · " + says
		}
		c.AddLink(a.titel, says, a.href)
	}
	if laibSays != "" {
		c.AddLink(aufgabenRowTitle, laibSays, aufgabenPath)
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
		fetch(r, func(ctx context.Context) { cfg.Einrichtung.Pruefen(ctx, r.Header.Get("Remote-User")) }) // a module's own check (Jellyfin)
		as := mitLaib(r, cfg)
		b := familyHead(r, cfg, "Deine Aufgaben", "Deine Aufgaben")
		b.WriteString("<h1>Deine Aufgaben" + zahl(len(offen(as))) + "</h1><p class=k>Was du einmal einrichtest. Ist es erledigt, verschwindet es aus Mehr; hier kannst du es noch einmal machen.</p>")
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
