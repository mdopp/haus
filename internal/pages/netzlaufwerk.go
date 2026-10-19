package pages

import (
	"errors"
	"html"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mdopp/haus/internal/samba"
)

// What the Netzlaufwerk page says (haus#52, laib#296), in the family's
// words: what it is for, how to connect, whether you have a password.
const (
	saysWhatItIs     = "Mit dem Netzlaufwerk öffnest du die Dateien des Hauses – Fotos, Dokumente, Musik – direkt im Explorer unter Windows, im Finder auf dem Mac oder in der Dateien-App auf dem Handy. Du meldest dich dort mit deinem Namen und einem eigenen Passwort an, das du hier festlegst."
	saysNoneYet      = "Noch kein Passwort – bis du eins festlegst, kommst du nicht aufs Netzlaufwerk."
	saysSameIsFine   = "Mindestens 8 Zeichen. Es darf dasselbe sein wie dein Familien-Passwort, wenn du magst – dann merkst du dir nur eins. Geräte, die schon verbunden sind, fragen nach dem neuen."
	saysNotFreed     = "Dein Konto ist nicht für das Netzlaufwerk freigegeben: es ist für die Familie und die Admins. Ein neues Konto kommt nach ein paar Minuten dazu."
	saysNoDrive      = "Auf dieser Box gibt es kein Netzlaufwerk."
	saysDiffer       = "Die beiden Passwörter sind nicht gleich. Bitte noch einmal."
	saysBadPassword  = "Das Passwort braucht 8 bis 128 Zeichen, in einer Zeile."
	saysDriveDown    = "Das Netzlaufwerk antwortet gerade nicht. Später noch einmal versuchen."
	saysUnconfirmed  = "Übergeben, aber das Netzlaufwerk hat es nicht bestätigt. Gleich noch einmal nachsehen."
	saysOnlyAdmins   = "Das Passwort einer anderen Person kann nur ein Admin ändern."
	saysTellThem     = "Sag der Person das neue Passwort; sie kann es danach hier selbst ändern."
	netzlaufwerkPath = "/netzlaufwerk"
)

// Netzlaufwerk is the network drive's page: the person's own password,
// set by them; for an admin everyone's, to reset. The password goes to
// file-share's Samba through package samba's FIFO, never to a log or a
// disk, and is never written back into the page.
func Netzlaufwerk(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cfg.Samba == nil {
			notSetSamba.write(w, r, cfg, "Netzlaufwerk")
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		person := r.Header.Get("Remote-User")
		if person == "" {
			http.Error(w, "kein Remote-User: diese Seite gehört hinter Authelia", http.StatusForbidden)
			return
		}
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			done := ""
			if who := r.URL.Query().Get("gespeichert"); who != "" {
				done = "Gespeichert. Das neue Passwort gilt ab sofort."
				if who != person {
					done = "Für " + who + " gespeichert. " + saysTellThem
				}
			} else if who := r.URL.Query().Get("entfernt"); who != "" {
				done = "Das Passwort von " + who + " ist entfernt; " + who + " legt hier ein neues fest."
			}
			writeNetzlaufwerk(w, r, cfg, http.StatusOK, done, "")
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "GET, HEAD, POST")
			http.Error(w, "Nur Anzeigen und das Formular.", http.StatusMethodNotAllowed)
			return
		}
		if !fromHaus(r) {
			http.Error(w, "Das geht nur von Haus' eigener Seite.", http.StatusForbidden)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Das Formular ist nicht lesbar.", http.StatusBadRequest)
			return
		}
		who, do := r.PostForm.Get("wer"), r.PostForm.Get("tun")
		if who == "" {
			who = person
		}
		if who != person && !isAdmin(r) {
			writeNetzlaufwerk(w, r, cfg, http.StatusForbidden, "", saysOnlyAdmins)
			return
		}
		var err error
		switch do {
		case "setzen":
			pw := r.PostForm.Get("passwort")
			if pw != r.PostForm.Get("nochmal") {
				writeNetzlaufwerk(w, r, cfg, http.StatusBadRequest, "", saysDiffer)
				return
			}
			err = cfg.Samba.Set(who, pw)
		case "entfernen":
			if !isAdmin(r) {
				writeNetzlaufwerk(w, r, cfg, http.StatusForbidden, "", saysOnlyAdmins)
				return
			}
			err = cfg.Samba.Clear(who)
		default:
			http.Error(w, "Unbekannter Knopf.", http.StatusBadRequest)
			return
		}
		if err == nil {
			key := "gespeichert"
			if do == "entfernen" {
				key = "entfernt"
			}
			http.Redirect(w, r, netzlaufwerkPath+"?"+key+"="+url.QueryEscape(who), http.StatusSeeOther)
			return
		}
		log.Printf("haus: netzlaufwerk: %s for %s by %s: %v", do, who, person, err) // the error, never the password
		code, say := http.StatusServiceUnavailable, saysDriveDown
		switch {
		case errors.Is(err, samba.ErrPassword):
			code, say = http.StatusBadRequest, saysBadPassword
		case errors.Is(err, samba.ErrNotMember):
			code, say = http.StatusForbidden, saysNotFreed
		case errors.Is(err, samba.ErrUnconfirmed):
			say = saysUnconfirmed
		case errors.Is(err, samba.ErrNoShare):
			say = saysNoDrive
		}
		writeNetzlaufwerk(w, r, cfg, code, "", say)
	}
}

// netzStyle is the page's form: the fields as Sprechen's, a full-width
// button on the phone.
const netzStyle = `<style>.lead{color:var(--muted)}.feld{margin:14px 0 0}.feld label{display:block;font-size:.85rem;color:var(--muted);margin:0 0 6px}
.feld input,.feld select{width:100%;font:inherit;color:var(--ink);background:var(--card);border:1px solid var(--edge);border-radius:12px;padding:0 14px;min-height:44px}
.passwort{margin:22px 0 0;padding:14px;background:var(--card);border:1px solid var(--edge);border-radius:14px}.passwort h2{margin:0;font-size:1rem}
.passwort .feld:first-of-type{margin-top:10px}.passwort p.k{font-size:.85rem;margin:10px 0 0}.passwort button{margin:14px 0 0;width:100%}
@media (min-width:36rem){.passwort button{width:auto}}.adresse{font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;font-size:.9rem;color:var(--ink)}
.gut{display:flex;gap:8px;align-items:flex-start;margin:16px 0 0;padding:12px 14px;border:1px solid var(--ok);border-radius:14px}.gut p{margin:0}</style>`

func writeNetzlaufwerk(w http.ResponseWriter, r *http.Request, cfg StartConfig, code int, done, problem string) {
	person := r.Header.Get("Remote-User")
	b := familyHead(r, cfg, "Netzlaufwerk", "")
	b.WriteString(netzStyle)
	b.WriteString("<h1>Netzlaufwerk</h1><p class=lead>" + html.EscapeString(saysWhatItIs) + "</p>")
	if done != "" {
		b.WriteString("<div class=gut role=status><span class=punkt></span><p>" + html.EscapeString(done) + "</p></div>")
	}
	if problem != "" {
		b.WriteString("<div class=achtung role=alert><p>" + html.EscapeString(problem) + "</p></div>")
	}
	acc, err := cfg.Samba.Accounts()
	if err != nil {
		if !errors.Is(err, samba.ErrNoShare) {
			log.Printf("haus: netzlaufwerk: status: %v", err)
		}
		b.WriteString("<p>" + html.EscapeString(saysNoDrive) + "</p>")
		writePage(w, code, b)
		return
	}
	since, member := acc[person]
	if !member {
		b.WriteString("<p>" + html.EscapeString(saysNotFreed) + "</p>")
		writePage(w, code, b)
		return
	}
	_, loc := cfg.clock()
	mine := cardsList("Dein Zugang")
	mine.row("Benutzername", person, true)
	if since.IsZero() {
		mine.row(saysNoneYet, "", false)
	} else {
		mine.row("Passwort gesetzt am "+dayWords(since, loc), "", false)
	}
	b.WriteString(mine.html())
	title := "Passwort festlegen"
	if !since.IsZero() {
		title = "Neues Passwort"
	}
	b.WriteString(passwordForm(title, person, nil, saysSameIsFine))
	if cfg.SambaHost != "" {
		h := html.EscapeString(cfg.SambaHost)
		how := cardsList("So verbindest du dich")
		how.raw("Windows", `Explorer, oben in die Adresszeile: <span class=adresse>\\`+h+`\data</span>`)
		how.raw("Mac", `Finder → Gehe zu → Mit Server verbinden: <span class=adresse>smb://`+h+`/data</span>`)
		how.raw("Handy", `Dateien-App → Mit Server verbinden: <span class=adresse>smb://`+h+`/data</span>`)
		how.row("Nur zu Hause im WLAN.", "", false)
		b.WriteString(how.html())
	}
	if isAdmin(r) {
		names := make([]string, 0, len(acc))
		for n := range acc {
			names = append(names, n)
		}
		sort.Strings(names)
		fam := cardsList("Passwörter der Familie")
		for _, n := range names {
			state := "noch keins"
			if !acc[n].IsZero() {
				state = "seit " + dayWords(acc[n], loc)
			}
			row := "<div>" + html.EscapeString(n) + "<span class=k>" + html.EscapeString(state) + "</span></div>"
			if !acc[n].IsZero() {
				row += "<form method=post action=" + netzlaufwerkPath + "><input type=hidden name=tun value=entfernen><input type=hidden name=wer value=\"" + html.EscapeString(n) + "\">" +
					"<button aria-label=\"Passwort von " + html.EscapeString(n) + " entfernen\">Entfernen</button></form>"
			}
			fam.items = append(fam.items, "<li class=tu>"+row+"</li>")
		}
		b.WriteString(fam.html())
		b.WriteString(passwordForm("Für jemanden neu setzen", "", names, saysTellThem))
	}
	writePage(w, code, b)
}

// passwordForm is the form that sets one: for who (a choice among names
// when who is ""), twice, never pre-filled.
func passwordForm(title, who string, names []string, hint string) string {
	var b strings.Builder
	b.WriteString("<form class=passwort method=post action=" + netzlaufwerkPath + "><h2>" + html.EscapeString(title) + "</h2><input type=hidden name=tun value=setzen>")
	id := "eigen"
	if who != "" {
		b.WriteString("<input type=hidden name=wer value=\"" + html.EscapeString(who) + "\">")
	} else {
		id = "fuer"
		b.WriteString("<div class=feld><label for=wer>Für</label><select id=wer name=wer>")
		for _, n := range names {
			b.WriteString("<option>" + html.EscapeString(n) + "</option>")
		}
		b.WriteString("</select></div>")
	}
	b.WriteString("<div class=feld><label for=" + id + "-pw>Neues Passwort für das Netzlaufwerk</label><input id=" + id + "-pw name=passwort type=password autocomplete=new-password minlength=" + strconv.Itoa(samba.MinLen) + " maxlength=" + strconv.Itoa(samba.MaxLen) + " required></div>")
	b.WriteString("<div class=feld><label for=" + id + "-nochmal>Noch einmal</label><input id=" + id + "-nochmal name=nochmal type=password autocomplete=new-password minlength=" + strconv.Itoa(samba.MinLen) + " maxlength=" + strconv.Itoa(samba.MaxLen) + " required></div>")
	b.WriteString("<p class=k>" + html.EscapeString(hint) + "</p><button>Passwort speichern</button></form>")
	return b.String()
}

// a card in the cards' look, with rows of plain text or of the page's own markup
type netzCard struct {
	title string
	items []string
}

func cardsList(title string) *netzCard { return &netzCard{title: title} }

func (c *netzCard) row(text, detail string, strong bool) {
	t := html.EscapeString(text)
	if detail != "" {
		t += "<span class=k>" + html.EscapeString(detail) + "</span>"
	}
	if strong {
		t = "<span class=k>" + html.EscapeString(text) + "</span>" + html.EscapeString(detail)
	}
	c.items = append(c.items, "<li>"+t+"</li>")
}

func (c *netzCard) raw(label, markup string) {
	c.items = append(c.items, "<li>"+html.EscapeString(label)+"<span class=k>"+markup+"</span></li>")
}

func (c *netzCard) html() string {
	return "<section class=karte><h2>" + html.EscapeString(c.title) + "</h2><ul>" + strings.Join(c.items, "") + "</ul></section>"
}

// dayWords is a day as the family says it: "17. Oktober 2026".
func dayWords(t time.Time, loc *time.Location) string {
	t = t.In(loc)
	return strconv.Itoa(t.Day()) + ". " + months[t.Month()-1] + " " + strconv.Itoa(t.Year())
}

// toNetzlaufwerk is Start's way to the page, for a member only: a to-do
// while they have no password, a quiet link once they have one.
func toNetzlaufwerk(d *samba.Dir, person string) string {
	if d == nil || person == "" {
		return ""
	}
	acc, err := d.Accounts()
	since, member := acc[person]
	if err != nil || !member {
		return ""
	}
	if since.IsZero() {
		c := cardsList("Netzlaufwerk")
		c.items = append(c.items, "<li><a href="+netzlaufwerkPath+">Noch kein Passwort fürs Netzlaufwerk<span class=k>Festlegen, um die Dateien des Hauses am Computer oder Handy zu öffnen</span></a></li>")
		return c.html()
	}
	return "<p class=ruhig><span class=\"punkt grau\"></span><a href=" + netzlaufwerkPath + ">Passwort fürs Netzlaufwerk ändern</a></p>"
}
