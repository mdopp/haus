package pages

import (
	"html"
	"net/http"
	"strconv"
	"strings"

	"github.com/mdopp/haus/internal/cards"
)

// Einrichtung (haus#188; owner 2026-10-28: „es gibt häufige/regelmäßige und
// einmalige/seltene Dinge“): what a person sets up once is a task, not a
// page in the menu. Mehr shows the open ones at the top, each with its
// guided page; Start says it quietly while an important one is open;
// Deine Aufgaben lists them all, the done ones to do again. Done is what
// Haus sees, never a tick.
const aufgabenPath = "/aufgaben"

// aufgabe is one thing for one person to set up.
type aufgabe struct {
	titel, warum string
	stand        string // how far, „2 von 5 Aufnahmen“; "" for none
	href         string // the page that leads through it
	wichtig      bool   // security and access before the optional
	erledigt     bool
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
	if stimmeShown(cfg) { // the wake word with the family's voices (laib#825)
		n := stimmeCount(cfg, uploader(user))
		a := aufgabe{titel: House.Display + " deine Stimme beibringen", warum: "Fünfmal „" + House.Phrase + "“ sagen; dann wacht " + House.Display + " bei dir zuverlässiger auf.",
			href: StimmePath, erledigt: n >= stimmeWanted}
		if n > 0 && n < stimmeWanted {
			a.stand = strconv.Itoa(n) + " von " + strconv.Itoa(stimmeWanted) + " Aufnahmen"
		}
		out = append(out, a)
	}
	if cfg.Discogs != nil { // the household's Discogs app is there; each person connects their own
		_, verbunden := cfg.Discogs.Konto(user)
		out = append(out, aufgabe{titel: "Discogs verbinden", warum: "Dann weiß Haus, welche Platten du hast, und deine Wünsche kommen auf deine Wantlist.",
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
	open := offen(einrichtung(r, cfg))
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
	for _, a := range offen(einrichtung(r, cfg)) {
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
		as := einrichtung(r, cfg)
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
		} else if len(as) > 0 {
			b.WriteString("<p class=k>Alles eingerichtet.</p>")
		}
		if len(done.Rows) > 0 {
			b.WriteString(done.HTML())
		}
		writePage(w, http.StatusOK, b)
	}
}
