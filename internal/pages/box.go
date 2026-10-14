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
	"github.com/mdopp/haus/internal/persona"
)

// House is the house persona the family sees (laib ADR 0020), set once at
// start; plain Haus without one.
var House = persona.Default()

// head opens a page: the title in the persona's name, a header with its
// icon, name and greeting in its colours.
func head(title string) string {
	icon := ""
	if len(House.Icon) > 0 {
		icon = `<img src="/persona/icon.svg" alt="" width=28 height=28>`
	}
	greet := ""
	if g := House.Greet("de"); g != "" {
		greet = "<span class=gruss>" + html.EscapeString(g) + "</span>"
	}
	return "<!doctype html><meta charset=utf-8><meta name=viewport content='width=device-width,initial-scale=1'><title>" + html.EscapeString(title) + " · " + html.EscapeString(House.Display) + "</title>" + style +
		"<style>header{display:flex;gap:.6rem;align-items:center;flex-wrap:wrap;margin:-1.2rem -1.2rem 1rem;padding:.6rem 1.2rem;background:" + House.Ground() + ";color:" + House.Accent() + "}header b{font-size:1.05rem}header .gruss{opacity:.85}</style>" +
		"<header>" + icon + "<b>" + html.EscapeString(House.Display) + "</b>" + greet + "</header>"
}

// Icon serves the persona's icon.svg.
func Icon() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if len(House.Icon) == 0 {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
		w.Write(House.Icon)
	}
}

const style = `<style>body{font:15px/1.45 system-ui,sans-serif;margin:0;padding:1.2rem;max-width:56rem;color:#1d1d1b;background:#faf9f6}
h1{font-size:1.3rem;margin:.2rem 0 1rem}h2{font-size:1rem;margin:1.4rem 0 .4rem;letter-spacing:.02em;text-transform:uppercase;color:#6b665c}
table{border-collapse:collapse;width:100%}td,th{text-align:left;padding:.3rem .5rem;border-bottom:1px solid #e6e2d8}
.gruen{color:#2b7a3d}.gelb{color:#b07a00}.rot{color:#b3261e}.k{font-variant-numeric:tabular-nums;color:#6b665c}
form{display:inline}button{font:inherit;padding:.25rem .7rem;border:1px solid #c9c3b6;border-radius:.4rem;background:#fff;cursor:pointer}
nav a{margin-right:1rem}</style>`

// Box is the admins' page: readiness, the inbox (read-only — deciding
// happens on Laib's own page, the one approval surface, ADR 0012), what
// runs, the journal. laibURL is where Freigeben lives.
func Box(c *laib.Client, laibURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := r.Header.Get("Remote-User")
		ctx := r.Context()
		if r.Method == http.MethodPost {
			http.Error(w, "Haus entscheidet nichts: Freigeben und Ablehnen gibt es nur auf der Laib-Seite", http.StatusForbidden)
			return
		}
		ready, rows, err := c.Status(ctx)
		var b strings.Builder
		b.WriteString(head("Box"))
		b.WriteString("<nav><a href=/box>Box</a><a href=/box/geraete>Geräte</a></nav>")
		if err != nil {
			fmt.Fprintf(&b, "<h1>Box</h1><p class=rot>Laib antwortet nicht: %s</p>", html.EscapeString(err.Error()))
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(b.String()))
			return
		}
		// the three guarantees, for everyone in the family (haus#2)
		b.WriteString(guarantees(ready, rows, strings.Contains(","+r.Header.Get("Remote-Groups")+",", ",admins,"), laibURL))
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
				fmt.Fprintf(&b, "<tr><td class=k>#%d</td><td>%s<br><span class=k>%s · %s · %s · %s</span>", p.ID, html.EscapeString(p.Reason), html.EscapeString(p.Actor), p.Tier, p.Class, p.Created.Format("02.01. 15:04"))
				for _, why := range p.Reasons {
					fmt.Fprintf(&b, "<br><span class=gelb>%s</span>", html.EscapeString(why))
				}
				b.WriteString("</td></tr>")
			}
			b.WriteString("</table>")
			fmt.Fprintf(&b, "<p><a href=\"%s/inbox\">Entscheiden auf der Laib-Seite</a> <span class=k>— die eine Stelle, an der freigegeben wird</span></p>", html.EscapeString(strings.TrimRight(laibURL, "/")))
		}
		// the window reaches back to yesterday's report; the journal table stays at 24h
		es, jerr := c.Journal(ctx, "36h")
		b.WriteString(morningReport(es, jerr))
		b.WriteString("<h2>Was läuft</h2><table>")
		for _, s := range rows {
			fmt.Fprintf(&b, "<tr><td class=%s>%s</td><td>%s</td><td class=k>%s</td></tr>", cls(s.State), html.EscapeString(s.State), html.EscapeString(s.Name), html.EscapeString(s.Detail))
		}
		b.WriteString("</table><h2>Journal · 24h</h2><table>")
		if jerr == nil {
			shown := 0
			for i := len(es) - 1; i >= 0 && shown < 25; i-- {
				e := es[i]
				if time.Since(e.T) > 24*time.Hour {
					break
				}
				shown++
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

// morningReport is the Operator's newest report (laib ADR 0013): the last
// journal note of kind report. No report is said plainly.
func morningReport(es []laib.Entry, err error) string {
	if err != nil {
		return "<h2>Morgenbericht</h2><p class=rot>Laib antwortet nicht: " + html.EscapeString(err.Error()) + "</p>"
	}
	for i := len(es) - 1; i >= 0; i-- {
		if e := es[i]; e.Verb == "note" && e.Reason == "report" {
			return fmt.Sprintf("<h2>Morgenbericht <span class=k>%s</span></h2><p>%s</p>", e.T.Local().Format("02.01. 15:04"), html.EscapeString(e.Detail))
		}
	}
	return "<h2>Morgenbericht</h2><p class=k>noch keiner: Der Operator schreibt ihn jeden Morgen, wenn er an ist.</p>"
}
