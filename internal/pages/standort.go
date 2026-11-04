package pages

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/mdopp/homes/internal/reitti"
	"github.com/mdopp/homes/internal/texte"
)

// Standortverlauf (owner 2026-11-02): the Homes app can record where its
// person was and Homes passes the points on to the household's Reitti,
// into that person's own timeline. For that Homes needs the person's Reitti
// token once: pasted here, checked with Reitti, kept for them alone and
// never shown back. Once connected the page leads with the timeline, says
// when the newest point came (or the step while none has), and ends with a
// quiet Trennen (owner 2026-11-04).
const (
	StandortPath = "/standort"

	saysStandortWas       = "Die Homes-App kann aufzeichnen, wo du unterwegs warst, und schickt es in deine eigene Zeitleiste in Reitti. Der Verlauf bleibt auf eurer Box, in deinem Reitti-Konto."
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

// Standort is the page and its two forms (tun=verbinden with the token,
// tun=trennen).
func Standort(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != StandortPath {
			Elsewhere(cfg)(w, r)
			return
		}
		if cfg.Reitti == nil {
			modulAus(w, r, cfg, "standort", StandortPath)
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
	k, verbunden := cfg.Reitti.Konto(r.Header.Get("Remote-User"))
	reittiURL := AppURL(OnDomain(cfg.Reitti.PublicURL, cfg.PublicURL))
	b := familyHead(r, cfg, "Standortverlauf", "Standortverlauf")
	b.WriteString(netzStyle + keyHintStyle + gekoppeltStyle + "<h1>Standortverlauf</h1>")
	if verbunden { // connected: the timeline first, the step only while no point came, Trennen last and quiet
		b.WriteString("<p class=lead>" + html.EscapeString(texte.T(texte.StandortKurz)) + "</p>" + gesagt(said, problem))
		if reittiURL != "" {
			b.WriteString(vorne(reittiURL, texte.T(texte.StandortOeffnen)))
		}
		now, loc := cfg.clock()
		if k.Zuletzt.IsZero() {
			b.WriteString(zustand("gelb", "Verbunden seit "+dayWords(k.Seit, loc)) + "<p class=schritt>" + html.EscapeString(texte.T(texte.StandortEinschalten)) + "</p>")
		} else {
			b.WriteString(zustand("", fmt.Sprintf(texte.T(texte.StandortZuletzt), dayWord(k.Zuletzt, now, loc)+" "+k.Zuletzt.In(loc).Format("15:04"))))
		}
		b.WriteString(leiseTrennen(StandortPath, "Homes soll nichts mehr an Reitti schicken?"))
		writePage(w, code, b)
		return
	}
	b.WriteString("<p class=lead>" + html.EscapeString(saysStandortWas) + "</p>" + gesagt(said, problem) +
		"<form class=passwort method=post action=" + StandortPath + "><h2>Mit Reitti verbinden</h2><input type=hidden name=tun value=verbinden><p class=k>" + html.EscapeString(saysStandortHow) + "</p>")
	if reittiURL != "" {
		b.WriteString(`<p class=wege><a class=knopf href="` + html.EscapeString(reittiURL) + `">Reitti öffnen</a></p>`)
	}
	// type=password: the token is not shown while it is pasted, and never written back
	b.WriteString(`<div class=feld><label for=reitti-token>Token aus Reitti</label><input id=reitti-token name=token type=password autocomplete=off spellcheck=false required></div>` +
		`<button data-laeuft="Prüft bei Reitti …">Verbinden</button></form><p class=k>` + html.EscapeString(saysStandortApp) + "</p>")
	writePage(w, code, b)
}
