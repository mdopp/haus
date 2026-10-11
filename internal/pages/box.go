// Package pages renders Haus's HTML. Behind Authelia: Caddy sends
// Remote-User, and Haus listens on loopback only, so nothing else reaches
// it. One page per file; no framework.
package pages

import (
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"

	"github.com/mdopp/haus/internal/laib"
)

const style = `<style>body{font:15px/1.45 system-ui,sans-serif;margin:0;padding:1.2rem;max-width:56rem;color:#1d1d1b;background:#faf9f6}
h1{font-size:1.3rem;margin:.2rem 0 1rem}h2{font-size:1rem;margin:1.4rem 0 .4rem;letter-spacing:.02em;text-transform:uppercase;color:#6b665c}
table{border-collapse:collapse;width:100%}td,th{text-align:left;padding:.3rem .5rem;border-bottom:1px solid #e6e2d8}
.gruen{color:#2b7a3d}.gelb{color:#b07a00}.rot{color:#b3261e}.k{font-variant-numeric:tabular-nums;color:#6b665c}
form{display:inline}button{font:inherit;padding:.25rem .7rem;border:1px solid #c9c3b6;border-radius:.4rem;background:#fff;cursor:pointer}
nav a{margin-right:1rem}</style>`

// Box is the admins' page: readiness, inbox with verdicts, services, journal.
func Box(c *laib.Client, human *laib.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := r.Header.Get("Remote-User")
		ctx := r.Context()
		if r.Method == http.MethodPost {
			// POST /box/inbox/{id}/{decision}
			parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
			if len(parts) == 4 && parts[1] == "inbox" && human != nil && human.Token != "" {
				var err error
				if parts[3] == "approve" {
					_, err = human.Call(ctx, "approve", parts[2])
				} else {
					_, err = human.Call(ctx, "reject", parts[2])
				}
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadGateway)
					return
				}
			}
			http.Redirect(w, r, "/box", http.StatusSeeOther)
			return
		}
		ready, rows, err := c.Status(ctx)
		var b strings.Builder
		b.WriteString("<!doctype html><meta charset=utf-8><meta name=viewport content='width=device-width,initial-scale=1'><title>Box · Haus</title>" + style)
		b.WriteString("<nav><a href=/box>Box</a><a href=/box/geraete>Geräte</a></nav>")
		if err != nil {
			fmt.Fprintf(&b, "<h1>Box</h1><p class=rot>Laib antwortet nicht: %s</p>", html.EscapeString(err.Error()))
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(b.String()))
			return
		}
		fmt.Fprintf(&b, "<h1>Box · <span class=%s>%s</span> <span class=k>· %d Dienste · %d grün · %d gelb · %d rot · Drift %d</span></h1>",
			readyClass(ready), html.EscapeString(ready.State), ready.Services, ready.Green, ready.Yellow, ready.Red, ready.Drift)
		fmt.Fprintf(&b, "<p>Operator: <span class=%s>%s</span></p>", operatorClass(ready.Operator), html.EscapeString(ready.Operator))
		if ready.Lock != "" {
			fmt.Fprintf(&b, "<p class=gelb>gesperrt: %s</p>", html.EscapeString(ready.Lock))
		}
		if user != "" {
			fmt.Fprintf(&b, "<p class=k>angemeldet als %s</p>", html.EscapeString(user))
		}
		b.WriteString("<h2>Inbox</h2>")
		if ps, err := c.Inbox(ctx); err != nil {
			fmt.Fprintf(&b, "<p class=rot>%s</p>", html.EscapeString(err.Error()))
		} else if len(ps) == 0 {
			b.WriteString("<p class=k>leer</p>")
		} else {
			b.WriteString("<table>")
			for _, p := range ps {
				fmt.Fprintf(&b, "<tr><td class=k>#%d</td><td>%s<br><span class=k>%s · %s · %s · %s</span></td><td>", p.ID, html.EscapeString(p.Reason), html.EscapeString(p.Actor), p.Tier, p.Class, p.Created.Format("02.01. 15:04"))
				if human != nil && human.Token != "" {
					fmt.Fprintf(&b, "<form method=post action=/box/inbox/%d/approve><button>Freigeben</button></form> <form method=post action=/box/inbox/%d/reject><button>Ablehnen</button></form>", p.ID, p.ID)
				} else {
					b.WriteString("<span class=k>entscheiden auf der Laib-Seite</span>")
				}
				b.WriteString("</td></tr>")
			}
			b.WriteString("</table>")
		}
		b.WriteString("<h2>Was läuft</h2><table>")
		for _, s := range rows {
			fmt.Fprintf(&b, "<tr><td class=%s>%s</td><td>%s</td><td class=k>%s</td></tr>", cls(s.State), html.EscapeString(s.State), html.EscapeString(s.Name), html.EscapeString(s.Detail))
		}
		b.WriteString("</table><h2>Journal · 24h</h2><table>")
		if es, err := c.Journal(ctx, "24h"); err == nil {
			for i := len(es) - 1; i >= 0 && i >= len(es)-25; i-- {
				e := es[i]
				fmt.Fprintf(&b, "<tr><td class=k>%s</td><td>%s</td><td>%s</td><td class=%s>%s</td><td class=k>%s</td></tr>", e.T.Local().Format("02.01. 15:04"), html.EscapeString(e.Actor), html.EscapeString(e.Verb+" "+e.Service), result(e.Result), e.Result, html.EscapeString(trim(e.Reason+" "+e.Detail, 90)))
			}
		}
		b.WriteString("</table>")
		fmt.Fprintf(&b, "<p class=k>Stand %s</p>", time.Now().Format("02.01.2006 15:04"))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(b.String()))
	}
}

func operatorClass(s string) string {
	switch s {
	case "läuft":
		return "gruen"
	case "fehlt", "antwortet nicht":
		return "rot"
	case "aus":
		return "k"
	}
	return "gelb"
}

func readyClass(r laib.Readiness) string {
	if r.State == "bereit" {
		return "gruen"
	}
	return "gelb"
}
func cls(s string) string {
	switch s {
	case "grün":
		return "gruen"
	case "rot":
		return "rot"
	}
	return "gelb"
}
func result(s string) string {
	switch s {
	case "green":
		return "gruen"
	case "red":
		return "rot"
	}
	return "k"
}
func trim(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
