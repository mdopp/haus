package pages

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"strings"

	"github.com/mdopp/homes/internal/cards"
	"github.com/mdopp/homes/internal/texte"
)

// The Willkommen page (haus#56; owner 2026-11-04: the guides went to each
// module, they went stale in one place): where a new person lands after
// the first login. „Hallo <Vorname>“, the username, and the next steps:
// the person's open setup tasks, each a link to where it is done.
const (
	willkommenPath = "/willkommen"
	welcomeCookie  = "homes_willkommen"
)

// Willkommen is the welcome page. It shows, it changes nothing; it notes
// in a cookie that this device has seen it, so Start sends there only once.
func Willkommen(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !showOnly(w, r, "Die Willkommensseite") {
			return
		}
		user := r.Header.Get("Remote-User")
		fetch(r, func(ctx context.Context) { cfg.Einrichtung.Pruefen(ctx, user) })
		http.SetCookie(w, &http.Cookie{Name: welcomeCookie, Value: "gesehen", Path: "/", MaxAge: 5 * 365 * 24 * 3600, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
		b := familyHead(r, cfg, "Willkommen", "")
		name := user
		if first := strings.Fields(r.Header.Get("Remote-Name")); len(first) > 0 {
			name = first[0]
		}
		b.WriteString("<h1>" + html.EscapeString(strings.TrimSpace(fmt.Sprintf(texte.T(texte.Hallo), name))) + "</h1>")
		if user != "" {
			b.WriteString("<p>" + html.EscapeString(texte.T(texte.Benutzername)) + ` <span style="font-family:ui-monospace,Menlo,Consolas,monospace">` + html.EscapeString(user) + "</span></p>")
		}
		next := cards.Card{Title: texte.T(texte.Schritte)}
		for _, a := range offen(einrichtung(r, cfg)) {
			next.AddLink(a.titel, a.warum, a.href)
		}
		if len(next.Rows) == 0 {
			b.WriteString("<p class=k>" + html.EscapeString(texte.T(texte.AllesFertig)) + "</p>")
		} else {
			b.WriteString(next.HTML())
		}
		b.WriteString("<p><a href=/start>" + html.EscapeString(texte.T(texte.ZurStartseite)) + "</a></p>")
		writePage(w, http.StatusOK, b)
	}
}

// firstVisit is Start sending a person to the Willkommen page the first
// time this device opens Homes (haus#56).
func firstVisit(w http.ResponseWriter, r *http.Request, cfg StartConfig) bool {
	if !cfg.Welcome || r.Header.Get("Remote-User") == "" || r.URL.RawQuery != "" {
		return false
	}
	if _, err := r.Cookie(welcomeCookie); err == nil {
		return false
	}
	http.Redirect(w, r, willkommenPath, http.StatusSeeOther)
	return true
}
