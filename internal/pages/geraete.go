package pages

import (
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"

	"github.com/mdopp/haus/internal/devices"
	"github.com/mdopp/haus/internal/legacy"
)

// Geraete pairs phones: a name in, the app's setup link out (shown once),
// the list with Entfernen. The public origin is what the phone will call.
func Geraete(store *devices.Store, publicURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := r.Header.Get("Remote-User")
		origin := publicURL
		if origin == "" {
			origin = "https://" + r.Host
		}
		var b strings.Builder
		b.WriteString("<!doctype html><meta charset=utf-8><meta name=viewport content='width=device-width,initial-scale=1'><title>Geräte · Haus</title>" + style)
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
			link := legacy.AppScheme + "://setup?server=" + url.QueryEscape(origin) + "&token=" + url.QueryEscape(tok)
			fmt.Fprintf(&b, "<h2>%s verbunden</h2><p>Auf dem Telefon diesen Link öffnen (einmalig sichtbar):</p><p><a href=\"%s\">App verbinden</a></p><p class=k>Server %s · Token beginnt mit %s…</p>",
				html.EscapeString(d.Name), html.EscapeString(link), html.EscapeString(origin), html.EscapeString(tok[:16]))
		}
		b.WriteString("<h2>Neues Gerät</h2><form method=post><input name=name placeholder='Name, z.B. Pixel von Martin'> <button>Verbinden</button></form>")
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
