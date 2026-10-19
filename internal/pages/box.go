// Package pages renders Haus's HTML. Behind Authelia: Caddy sends
// Remote-User, and Haus listens on loopback only, so nothing else reaches
// it. One page per file; no framework.
package pages

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"

	"github.com/mdopp/haus/internal/laib"
	"github.com/mdopp/haus/internal/persona"
	"github.com/mdopp/haus/internal/usage"
	"github.com/mdopp/haus/internal/werkstatt"
)

// House is the house persona the family sees (laib ADR 0020), set once at
// start; plain Haus without one.
var House = persona.Default()

// adminHead opens an admin page (haus#58): the family pages' frame (#35)
// with Box marked in the tab bar, the admin cards' style and Box's own two
// links. No persona: it speaks only where you talk to it.
func adminHead(r *http.Request, cfg StartConfig, title string) *strings.Builder {
	b := familyHead(r, cfg, title, "Box")
	b.WriteString(adminStyle)
	return b
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

// adminStyle is the admin cards in the house palette: tables as cards,
// section labels, the states' colours, Box's sub-links as pills.
const adminStyle = `<style>.unter{display:flex;gap:8px;margin:18px 0 0}.unter a{display:inline-flex;align-items:center;min-height:40px;padding:0 16px;border-radius:999px;background:var(--card);border:1px solid var(--edge);color:var(--ink);text-decoration:none;font-size:.85rem}.unter a[aria-current]{color:var(--accent);border-color:var(--accent)}
h2{font-size:.72rem;font-weight:600;letter-spacing:.1em;text-transform:uppercase;color:var(--muted);margin:26px 2px 8px}
table{width:100%;border-collapse:separate;border-spacing:0;background:var(--card);border:1px solid var(--edge);border-radius:14px;overflow:hidden}
td,th{text-align:left;padding:10px 12px;border-bottom:1px solid var(--line);vertical-align:top;overflow-wrap:anywhere}tr:last-child td{border-bottom:0}
.gruen{color:var(--ok)}.gelb{color:var(--warn)}.rot{color:var(--danger)}.k{font-variant-numeric:tabular-nums}
form{display:inline}input:not([type]),input[type=text],input[type=email]{font:inherit;color:var(--ink);background:var(--card);border:1px solid var(--edge);border-radius:999px;padding:0 16px;min-height:44px;max-width:100%}
code{overflow-wrap:anywhere;font:.8rem ui-monospace,SFMono-Regular,Menlo,monospace}</style>`

// unterNav is Box's two pages, the current one marked.
func unterNav(current string) string {
	mark := func(n string) string {
		if n == current {
			return " aria-current=page"
		}
		return ""
	}
	return "<nav class=unter><a href=/box" + mark("Box") + ">Box</a><a href=/box/geraete" + mark("Geräte") + ">Geräte</a></nav>"
}

// Box is the admins' page: readiness, the inbox (read-only — deciding
// happens on Laib's own page, the one approval surface, ADR 0012), what
// runs, the journal. laibURL is where Freigeben lives.
func Box(c *laib.Client, laibURL string) http.HandlerFunc { return BoxWith(c, laibURL, nil) }

// BoxWith is Box with the Werkstatt's tickets as a card (haus#15); nil, no card.
func BoxWith(c *laib.Client, laibURL string, tickets *werkstatt.Tickets) http.HandlerFunc {
	return BoxPage(BoxConfig{Laib: c, LaibURL: laibURL, Tickets: tickets})
}

// BoxConfig is what the Box page reads.
type BoxConfig struct {
	Laib    *laib.Client
	LaibURL string
	Tickets *werkstatt.Tickets // the Werkstatt card (haus#15); nil, no card
	Family  StartConfig        // what this box has, and Usage: the "Was wird genutzt" card (haus#50); nil Usage, no card
	Voice   *usage.Voice       // the voice control's counting, to say when HA refuses it
}

// BoxPage is the Box page with all its cards.
func BoxPage(cfg BoxConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			http.Error(w, "Haus entscheidet nichts: Freigeben und Ablehnen gibt es nur auf der Laib-Seite", http.StatusForbidden)
			return
		}
		if !adminOnly(w, r) {
			return
		}
		writeBox(w, r, cfg, http.StatusOK, invitation{})
	}
}

// writeBox renders the Box page; inv is what the invite card says after
// a POST to /box/einladen (haus#56).
func writeBox(w http.ResponseWriter, r *http.Request, cfg BoxConfig, code int, inv invitation) {
	c, laibURL, tickets := cfg.Laib, cfg.LaibURL, cfg.Tickets
	user := r.Header.Get("Remote-User")
	ctx := r.Context()
	ready, rows, err := c.Status(ctx)
	b := adminHead(r, cfg.Family, "Box")
	b.WriteString(unterNav("Box"))
	if err != nil {
		fmt.Fprintf(b, "<h1>Box</h1><p class=rot>Laib antwortet nicht: %s</p>", html.EscapeString(err.Error()))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(code)
		w.Write([]byte(b.String()))
		return
	}
	// the three guarantees, for everyone in the family (haus#2)
	b.WriteString(guarantees(ready, rows, isAdmin(r), laibURL))
	fmt.Fprintf(b, "<h1>Box · <span class=%s>%s</span> <span class=k>· %d Dienste · %d grün · %d gelb · %d rot · Drift %d</span></h1>",
		readyClass(ready), html.EscapeString(ready.State), ready.Services, ready.Green, ready.Yellow, ready.Red, ready.Drift)
	fmt.Fprintf(b, "<p>Operator: <span class=%s>%s</span></p>", operatorClass(ready.Operator), html.EscapeString(ready.Operator))
	if ready.Lock != "" {
		fmt.Fprintf(b, "<p class=gelb>gesperrt: %s</p>", html.EscapeString(ready.Lock))
	}
	if user != "" {
		fmt.Fprintf(b, "<p class=k>angemeldet als %s</p>", html.EscapeString(user))
	}
	b.WriteString("<h2>Inbox</h2>")
	if ps, err := c.Inbox(ctx); err != nil {
		fmt.Fprintf(b, "<p class=rot>%s</p>", html.EscapeString(err.Error()))
	} else if len(ps) == 0 {
		b.WriteString("<p class=k>leer</p>")
	} else {
		b.WriteString("<table>")
		for _, p := range ps {
			fmt.Fprintf(b, "<tr><td class=k>#%d</td><td>%s<br><span class=k>%s · %s · %s · %s</span>", p.ID, html.EscapeString(p.Reason), html.EscapeString(p.Actor), p.Tier, p.Class, p.Created.Format("02.01. 15:04"))
			for _, why := range p.Reasons {
				fmt.Fprintf(b, "<br><span class=gelb>%s</span>", html.EscapeString(why))
			}
			b.WriteString("</td></tr>")
		}
		b.WriteString("</table>")
		fmt.Fprintf(b, "<p><a href=\"%s/inbox\">Entscheiden auf der Laib-Seite</a> <span class=k>— die eine Stelle, an der freigegeben wird</span></p>", html.EscapeString(strings.TrimRight(laibURL, "/")))
	}
	b.WriteString(inviteCard(ctx, c, inv))
	// the window reaches back to yesterday's report; the journal table stays at 24h
	es, jerr := c.Journal(ctx, "36h")
	b.WriteString(morningReport(es, jerr))
	b.WriteString(usageCard(ctx, cfg.Family, cfg.Voice))
	if tickets != nil {
		b.WriteString(werkstattCard(r.Context(), tickets))
	}
	b.WriteString("<h2>Was läuft</h2><table>")
	for _, s := range rows {
		fmt.Fprintf(b, "<tr><td class=%s>%s</td><td>%s</td><td class=k>%s</td></tr>", cls(s.State), html.EscapeString(s.State), html.EscapeString(s.Name), html.EscapeString(s.Detail))
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
			fmt.Fprintf(b, "<tr><td class=k>%s</td><td>%s</td><td>%s</td><td class=%s>%s</td><td class=k>%s</td></tr>", e.T.Local().Format("02.01. 15:04"), html.EscapeString(e.Actor), html.EscapeString(e.Verb+" "+e.Service), result(e.Result), e.Result, html.EscapeString(trim(e.Reason+" "+e.Detail, 90)))
		}
	}
	b.WriteString("</table>")
	fmt.Fprintf(b, "<p class=k>Stand %s</p>", time.Now().Format("02.01.2006 15:04"))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store") // an invitation's link may be on it
	w.WriteHeader(code)
	w.Write([]byte(b.String()))
}

// adminOnly lets admins through and sends everyone else to Start (303):
// Box is the admins' page (laib DEKONSTRUKTION). It says who sees the
// page, it decides nothing.
func adminOnly(w http.ResponseWriter, r *http.Request) bool {
	if isAdmin(r) {
		return true
	}
	http.Redirect(w, r, "/start", http.StatusSeeOther)
	return false
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

// werkstattCard lists what pi works and what waits for Claude, read-only:
// deciding and working happen in the Werkstatt, not here (haus#1).
func werkstattCard(ctx context.Context, t *werkstatt.Tickets) string {
	ts, err := t.Open(ctx)
	if err != nil {
		return "<h2>Werkstatt</h2><p class=gelb>GitHub ist gerade nicht erreichbar.</p>"
	}
	if len(ts) == 0 {
		return "<h2>Werkstatt</h2><p class=k>keine offenen Tickets</p>"
	}
	var b strings.Builder
	b.WriteString("<h2>Werkstatt</h2><table>")
	for _, x := range ts {
		who := "pi arbeitet daran"
		if x.Label == "needs-claude" {
			who = "wartet auf Claude"
		}
		fmt.Fprintf(&b, "<tr><td class=k>%s#%d</td><td><a href=\"%s\">%s</a></td><td class=k>%s</td></tr>",
			html.EscapeString(x.Repo), x.Number, html.EscapeString(x.URL), html.EscapeString(x.Title), who)
	}
	b.WriteString("</table>")
	return b.String()
}
