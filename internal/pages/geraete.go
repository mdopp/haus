package pages

import (
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"

	"github.com/mdopp/haus/internal/app"
	"github.com/mdopp/haus/internal/devices"
)

// Geraete lists the paired phones with Entfernen and says how to pair one:
// type the address into the app (it pairs itself through /pair-device), or
// mint a token here and put the setup URI into a QR code the app scans. The
// public origin is what the phone will call.
func Geraete(store *devices.Store, publicURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !adminOnly(w, r) { // under /box: the admins' (haus#12); phones pair through /pair-device
			return
		}
		user := r.Header.Get("Remote-User")
		origin := publicURL
		if origin == "" {
			origin = "https://" + r.Host
		}
		var b strings.Builder
		b.WriteString(head("Geräte"))
		b.WriteString("<nav><a href=/box>Box</a><a href=/box/geraete>Geräte</a></nav><h1>Geräte</h1>")
		if r.Method == http.MethodPost {
			r.ParseForm()
			if id := r.Form.Get("entfernen"); id != "" {
				store.Revoke(id)
				http.Redirect(w, r, "/box/geraete", http.StatusSeeOther)
				return
			}
			if user == "" {
				http.Error(w, "kein Remote-User: diese Seite gehört hinter Authelia", http.StatusForbidden)
				return
			}
			d, tok, err := store.Mint(r.Form.Get("name"), user)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			qr := app.Scheme + "://setup?server=" + url.QueryEscape(origin) + "&token=" + url.QueryEscape(tok)
			fmt.Fprintf(&b, "<h2>%s angelegt</h2><p>Inhalt für einen QR-Code, den die App scannt (einmalig sichtbar):</p><p class=k><code>%s</code></p>",
				html.EscapeString(d.Name), html.EscapeString(qr))
		}
		fmt.Fprintf(&b, "<h2>Neues Telefon</h2><p>In der App die Adresse <b>%s</b> eingeben. Die App öffnet die Anmeldung und koppelt sich selbst.</p>", html.EscapeString(origin))
		b.WriteString("<p class=k>Oder hier einen Zugang für einen QR-Code anlegen:</p><form method=post><input name=name placeholder='Name, z.B. Pixel von Martin'> <button>Anlegen</button></form>")
		b.WriteString("<h2>Verbunden</h2><table>")
		for _, d := range store.List() {
			last := "nie"
			if !d.LastSeen.IsZero() {
				last = d.LastSeen.Local().Format("02.01. 15:04")
			}
			fmt.Fprintf(&b, "<tr><td>%s</td><td class=k>%s · seit %s · zuletzt %s</td><td><form method=post><input type=hidden name=entfernen value=%s><button>Entfernen</button></form></td></tr>",
				html.EscapeString(d.Name), html.EscapeString(d.UID), d.Created.Local().Format("02.01.2006"), last, d.ID)
		}
		b.WriteString("</table>")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(b.String()))
	}
}
