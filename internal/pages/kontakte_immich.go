package pages

import (
	"cmp"
	"context"
	"encoding/base64"
	"fmt"
	"html"
	"log"
	"net/http"
	"net/url"

	"github.com/mdopp/homes/internal/caldav"
	"github.com/mdopp/homes/internal/cards"
	"github.com/mdopp/homes/internal/texte"
)

// Immich's named people without a Haushalts-Kontakt (owner 2026-11-05):
// a short, quiet list under the address book. Each can be made a contact
// with its name, its face coming from Immich as on every card without a
// picture (KontaktKarte), or hidden for good. Nothing is made by itself.
const KontakteImmichPath = KontaktePath + "/aus-immich"

// maxVorschlaege is how many the list shows; the rest wait their turn.
const maxVorschlaege = 8

// vorschlaege is the list for the contacts there are; "" without any.
func vorschlaege(cfg StartConfig, all []caldav.Contact) string {
	if !cfg.fotosAn() {
		return ""
	}
	var da []string
	for _, ct := range all {
		da = append(da, ct.Name)
	}
	ps := cfg.Gesichter.Vorschlaege(da)
	if len(ps) == 0 {
		return ""
	}
	t := func(id texte.ID) string { return html.EscapeString(texte.T(id)) }
	b := `<section class="karte vorschlaege" aria-labelledby=k-immich><h2 id=k-immich>` + t(texte.KontakteImmich) + `</h2><p class=k>` + t(texte.KontakteImmichHinweis) + `</p><ul>`
	for _, p := range ps[:min(len(ps), maxVorschlaege)] {
		form := func(tun string, knopf, laeuft texte.ID, leise string) string {
			return `<form method=post action=` + KontakteImmichPath + `><input type=hidden name=tun value=` + tun + `><input type=hidden name=id value="` + html.EscapeString(p.ID) +
				`"><button` + leise + ` data-laeuft="` + t(laeuft) + `">` + t(knopf) + `</button></form>`
		}
		b += "<li>"
		if jpg := cfg.Gesichter.Bild(p.Name); jpg != nil {
			b += `<img src="data:image/jpeg;base64,` + base64.StdEncoding.EncodeToString(jpg) + `" alt="" width=40 height=40>`
		}
		b += "<span>" + html.EscapeString(trimRunes(oneLine(p.Name), 120)) + "</span>" + form("anlegen", texte.KontakteAnlegen, texte.KontakteAnlegend, "") +
			form("ausblenden", texte.KontakteAusblenden, texte.KontakteAusblendend, " class=leise") + "</li>"
	}
	return b + "</ul></section>"
}

// vorschlaegeStyle: the face, the name, the two buttons on one line where they fit.
const vorschlaegeStyle = `<style>.vorschlaege li{display:flex;flex-wrap:wrap;align-items:center;gap:6px 10px}.vorschlaege img{border-radius:50%}.vorschlaege span{flex:1 1 8em}.vorschlaege form{margin:0}</style>`

// gesagtKontakte is the line above the list after a suggestion was taken
// (?angelegt=<uid>, with its name) or hidden (?ausgeblendet=1).
func gesagtKontakte(r *http.Request, all []caldav.Contact) string {
	q := r.URL.Query()
	for _, ct := range all {
		if uid := q.Get("angelegt"); uid != "" && ct.UID == uid {
			return gesagt(fmt.Sprintf(texte.T(texte.KontakteAngelegt), cmp.Or(oneLine(ct.Name), "?")), "")
		}
	}
	if q.Get("ausgeblendet") == "1" {
		return gesagt(texte.T(texte.KontakteAusgeblendet), "")
	}
	return ""
}

// KontakteAusImmich is a suggestion's two buttons: make the contact, or
// hide the person. Either ends on the Kontakte page saying what happened,
// or in one plain sentence why not.
func KontakteAusImmich(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "Das geht nur mit den Knöpfen auf der Kontakte-Seite.", http.StatusMethodNotAllowed)
			return
		}
		if !fromHomes(r) {
			http.Error(w, "Das geht nur von Homes' eigener Seite.", http.StatusForbidden)
			return
		}
		r.ParseForm()
		p, ok := cfg.Gesichter.Vorschlag(r.PostForm.Get("id"))
		if !ok || cfg.Contacts == nil {
			http.Error(w, texte.T(texte.KontakteVorschlagWeg), http.StatusConflict)
			return
		}
		switch r.PostForm.Get("tun") {
		case "ausblenden":
			if err := cfg.Gesichter.Ausblenden(p.ID); err != nil {
				log.Printf("homes: kontakte: hiding immich's %s: %v", p.ID, err)
				http.Error(w, texte.T(texte.KontakteAusblendenNicht), http.StatusInternalServerError)
				return
			}
			http.Redirect(w, r, KontaktePath+"?ausgeblendet=1", http.StatusSeeOther)
		case "anlegen":
			ct := caldav.Contact{UID: caldav.NewUUID(), Name: trimRunes(oneLine(p.Name), 120)}
			var err error
			fetch(r, func(ctx context.Context) {
				var all []caldav.Contact
				if all, _, err = cfg.Contacts.Contacts(ctx); err != nil {
					return
				}
				for _, da := range all { // made meanwhile, on a phone or by a second tap: that one
					if oneLine(da.Name) == ct.Name {
						ct.UID = da.UID
						return
					}
				}
				_, err = cfg.Contacts.PutContact(ctx, ct)
			})
			if err != nil {
				log.Printf("homes: kontakte: making %q from immich: %v", ct.Name, err)
				http.Error(w, texte.T(texte.KontakteAnlegenNicht), http.StatusBadGateway)
				return
			}
			log.Printf("homes: kontakte: %s made %s from immich", r.Header.Get("Remote-User"), cards.ContactPage(ct.UID))
			http.Redirect(w, r, KontaktePath+"?"+url.Values{"angelegt": {ct.UID}}.Encode(), http.StatusSeeOther)
		default:
			http.Error(w, "Das geht hier nicht.", http.StatusBadRequest)
		}
	}
}
