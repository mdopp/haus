package pages

import (
	"context"
	"errors"
	"html"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/mdopp/homes/internal/reitti"
)

// Standortverlauf (owner 2026-11-02): the Homes app can record where its
// person was and Homes passes the points on to the household's Reitti,
// into that person's own timeline. For that Homes needs the person's Reitti
// token once: pasted here, checked with Reitti, kept for them alone and
// never shown back — the page says only since when it is connected, and
// offers Trennen.
const (
	StandortPath = "/standort"

	saysStandortWas       = "Die Homes-App kann aufzeichnen, wo du unterwegs warst, und schickt es in deinen eigenen Zeitstrahl in Reitti. Der Verlauf bleibt auf eurer Box, in deinem Reitti-Konto."
	saysStandortHow       = "Dafür braucht Homes einmal einen Schlüssel aus deinem Reitti-Konto: In Reitti unter Einstellungen → API-Token einen neuen Token erstellen, ihn mit einem Gerät verbinden („Mit Gerät verbinden“), dann den Token hier einfügen."
	saysStandortApp       = "Danach in der Homes-App unter „Standortverlauf“ die Aufzeichnung einschalten."
	saysStandortVerbunden = "Verbunden: die Homes-App schickt deine Punkte jetzt an Reitti."
	saysStandortGetrennt  = "Getrennt: Homes schickt nichts mehr an Reitti. Die Homes-App behält neue Punkte, bis du wieder verbindest; in Reitti kannst du den Token unter Einstellungen → API-Token ganz löschen."
	saysStandortLeer      = "Füge den Token aus Reitti ein; das Feld war leer."
	saysStandortFalsch    = "Reitti kennt diesen Token nicht. Kopiere ihn noch einmal ganz aus Reitti (Einstellungen → API-Token)."
	saysStandortGeraet    = "Reitti hat den Token nicht angenommen. Meist fehlt ihm das Gerät: in Reitti unter Einstellungen → API-Token „Mit Gerät verbinden“ wählen, dann hier noch einmal."
	saysStandortWeg       = "Reitti antwortet gerade nicht. Versuch es in ein paar Minuten noch einmal."
	saysStandortNotSaved  = "Das ließ sich gerade nicht speichern. Versuch es noch einmal; hilft das nicht, sag dem Admin Bescheid."
)

var notSetReitti = notSet{"Der Standortverlauf ist hier noch nicht eingerichtet.", "Für Admins: Homes kennt Reitti nicht (HOMES_REITTI_URL, die Adresse von Reitti in der Box)."}

// Standort is the page and its two forms (tun=verbinden with the token,
// tun=trennen).
func Standort(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != StandortPath {
			Elsewhere(cfg)(w, r)
			return
		}
		if cfg.Reitti == nil {
			notSetReitti.write(w, r, cfg, "Standortverlauf")
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		user := r.Header.Get("Remote-User")
		if user == "" {
			http.Error(w, "kein Remote-User: diese Seite gehört hinter Authelia", http.StatusForbidden)
			return
		}
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			said := ""
			switch r.URL.Query().Get("ok") {
			case "verbunden":
				said = saysStandortVerbunden
			case "getrennt":
				said = saysStandortGetrennt
			}
			writeStandort(w, r, cfg, http.StatusOK, said, "")
			return
		case http.MethodPost:
		default:
			w.Header().Set("Allow", "GET, HEAD, POST")
			http.Error(w, "Nur Anzeigen und das Formular.", http.StatusMethodNotAllowed)
			return
		}
		if !fromHomes(r) {
			http.Error(w, "Das geht nur von Homes' eigener Seite.", http.StatusForbidden)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Das Formular ist nicht lesbar.", http.StatusBadRequest)
			return
		}
		switch r.PostForm.Get("tun") {
		case "verbinden":
			token := strings.TrimSpace(r.PostForm.Get("token"))
			if token == "" {
				writeStandort(w, r, cfg, http.StatusBadRequest, "", saysStandortLeer)
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
			defer cancel()
			if err := cfg.Reitti.Verbinden(ctx, user, token); err != nil {
				log.Printf("homes: standort: connecting %s: %v", user, err) // the error, never the token
				code, say := http.StatusBadGateway, saysStandortWeg
				var u *reitti.Unavailable
				switch {
				case errors.Is(err, reitti.ErrToken):
					code, say = http.StatusBadRequest, saysStandortFalsch
				case errors.As(err, &u) && u.Status >= 400:
					say = saysStandortGeraet
				case !errors.As(err, &u):
					code, say = http.StatusInternalServerError, saysStandortNotSaved
				}
				writeStandort(w, r, cfg, code, "", say)
				return
			}
			log.Printf("homes: standort: %s connected reitti", user)
			http.Redirect(w, r, StandortPath+"?ok=verbunden", http.StatusSeeOther)
		case "trennen":
			if err := cfg.Reitti.Trennen(user); err != nil {
				log.Printf("homes: standort: %s: %v", user, err)
				writeStandort(w, r, cfg, http.StatusInternalServerError, "", saysStandortNotSaved)
				return
			}
			log.Printf("homes: standort: %s disconnected reitti", user)
			http.Redirect(w, r, StandortPath+"?ok=getrennt", http.StatusSeeOther)
		default:
			http.Error(w, "Unbekannter Knopf.", http.StatusBadRequest)
		}
	}
}

func writeStandort(w http.ResponseWriter, r *http.Request, cfg StartConfig, code int, said, problem string) {
	user := r.Header.Get("Remote-User")
	b := familyHead(r, cfg, "Standortverlauf", "")
	b.WriteString(netzStyle + keyHintStyle)
	b.WriteString("<h1>Standortverlauf</h1><p class=lead>" + html.EscapeString(saysStandortWas) + "</p>")
	if said != "" {
		b.WriteString("<div class=gut role=status><span class=punkt></span><p>" + html.EscapeString(said) + "</p></div>")
	}
	if problem != "" {
		b.WriteString("<div class=achtung role=alert><p>" + html.EscapeString(problem) + "</p></div>")
	}
	_, loc := cfg.clock()
	if k, ok := cfg.Reitti.Konto(user); ok {
		mine := cardsList("Reitti")
		mine.row("Verbunden seit "+dayWords(k.Seit, loc), "", true)
		mine.row(saysStandortApp, "", false)
		b.WriteString(mine.html())
		b.WriteString(`<form method=post action=` + StandortPath + ` data-frage="Homes soll nichts mehr an Reitti schicken?"><input type=hidden name=tun value=trennen><button data-laeuft="Trennt …">Trennen</button></form>`)
	} else {
		b.WriteString("<form class=passwort method=post action=" + StandortPath + "><h2>Mit Reitti verbinden</h2><input type=hidden name=tun value=verbinden>" +
			"<p class=k>" + html.EscapeString(saysStandortHow) + "</p>")
		if u := AppURL(OnDomain(cfg.Reitti.PublicURL, cfg.PublicURL)); u != "" {
			b.WriteString(`<p class=wege><a class=knopf href="` + html.EscapeString(u) + `">Reitti öffnen</a></p>`)
		}
		// type=password: the token is not shown while it is pasted, and never written back
		b.WriteString(`<div class=feld><label for=reitti-token>Token aus Reitti</label><input id=reitti-token name=token type=password autocomplete=off spellcheck=false required></div>` +
			`<button data-laeuft="Prüft bei Reitti …">Verbinden</button></form>`)
		b.WriteString("<p class=k>" + html.EscapeString(saysStandortApp) + "</p>")
	}
	writePage(w, code, b)
}
