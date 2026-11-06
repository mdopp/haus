package pages

import (
	"context"
	"fmt"
	"html"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/mdopp/homes/internal/caldav"
	"github.com/mdopp/homes/internal/texte"
)

// MeineKontaktePath is Meine Kontakte (owner 2026-11-06): the Immich key
// owner's own Handy-Kontakte, read only, never mixed with the household's
// book. Each leads to its Immich person (/kontakte/privat/<uid>/fotos),
// the one Homes gave it (gesichter.Abgleichen), else the one of its name.
// Only the key's owner sees it: the login Homes saw with the e-mail of
// the key's Immich account (gesichter.Merke).
const MeineKontaktePath = KontaktePath + "/privat"

// privatEigen: the viewer is the Immich key's owner.
func privatEigen(r *http.Request, cfg StartConfig) bool {
	u := r.Header.Get("Remote-User")
	cfg.Gesichter.Merke(u, r.Header.Get("Remote-Email"))
	return cfg.fotosAn() && u != "" && cfg.Gesichter.Wessen() == u
}

// meineKontakteLink is the quiet line under Kontakte, for the key's owner,
// and the one to „Dieselbe Person?“.
func meineKontakteLink(r *http.Request, cfg StartConfig) string {
	if !privatEigen(r, cfg) {
		return ""
	}
	return `<p class=k><a href=` + MeineKontaktePath + `>` + html.EscapeString(texte.T(texte.MeineKontakte)) + `</a> · ` + html.EscapeString(texte.T(texte.MeineKontakteKurz)) + `</p>` + dieselbeLink(cfg)
}

// meineKontakte is the viewer's own Handy-Kontakte.
func meineKontakte(ctx context.Context, cfg StartConfig, person string) ([]caldav.Contact, error) {
	cards, _, err := cfg.Contacts.BookCards(ctx, "/"+url.PathEscape(person)+"/handy-kontakte/")
	var out []caldav.Contact
	for _, cd := range cards {
		if root, err := caldav.Parse(cd.Data); err == nil {
			out = append(out, caldav.Contacts(root)...)
		}
	}
	return out, err
}

// MeineKontakte is the list, and each one's „Fotos“.
func MeineKontakte(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !showOnly(w, r, "Meine Kontakte") {
			return
		}
		if !privatEigen(r, cfg) || cfg.Contacts == nil {
			http.Redirect(w, r, KontaktePath, http.StatusSeeOther)
			return
		}
		var all []caldav.Contact
		var err error
		fetch(r, func(ctx context.Context) { all, err = meineKontakte(ctx, cfg, r.Header.Get("Remote-User")) })
		uid, fotos := strings.CutSuffix(strings.TrimPrefix(r.URL.Path, MeineKontaktePath+"/"), "/fotos")
		if fotos && err == nil {
			for _, ct := range all {
				if ct.UID == uid {
					w.Header().Set("Cache-Control", "no-store")
					http.Redirect(w, r, cfg.Gesichter.Link("privat/"+uid, oneLine(ct.Name)), http.StatusFound)
					return
				}
			}
		}
		if r.URL.Path != MeineKontaktePath {
			missing{http.StatusNotFound, texte.T(texte.MeineKontakteWeg), "Kontakte", MeineKontaktePath, "Zurück zu " + texte.T(texte.MeineKontakte)}.write(w, r, cfg)
			return
		}
		t := func(id texte.ID) string { return html.EscapeString(texte.T(id)) }
		b := familyHead(r, cfg, texte.T(texte.MeineKontakte), "Kontakte")
		b.WriteString(kontakteStyle + "<h1>" + t(texte.MeineKontakte) + "</h1><p class=k>" + t(texte.MeineKontakteHinweis) + "</p>" + dieselbeLink(cfg))
		if recht := cfg.Gesichter.Fehlt(); recht != "" {
			b.WriteString(gesagt("", fmt.Sprintf(texte.T(texte.MeineKontakteRecht), recht)))
		}
		switch {
		case err != nil:
			log.Printf("homes: meine kontakte: %v", err)
			b.WriteString("<p class=k>" + saysNoContacts + "</p>")
		case len(all) == 0:
			b.WriteString("<p class=k>" + t(texte.MeineKontakteLeer) + "</p>")
		default:
			field := t(texte.KontakteSuchen)
			b.WriteString(`<input type=search id=kontakte-suche placeholder="` + field + `" aria-label="` + field + `" autocomplete=off><p class=k id=kontakte-keiner data-vorlage="` + t(texte.KontakteKeiner) + `" hidden></p>`)
			kontakteListe(b, kontaktZeilen(all, func(uid string) string { return MeineKontaktePath + "/" + url.PathEscape(uid) + "/fotos" }), len(all) >= kontakteBuchstaben)
			b.WriteString(kontakteScript)
		}
		writePage(w, http.StatusOK, b)
	}
}
