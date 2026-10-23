// Package pages renders Haus's HTML. Behind Authelia: Caddy sends
// Remote-User, and Haus listens on loopback only, so nothing else reaches
// it. One page per file; no framework.
package pages

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
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
td,th{text-align:left;padding:10px 12px;border-bottom:1px solid var(--line);vertical-align:top;overflow-wrap:break-word}tr:last-child td{border-bottom:0}
.gruen{color:var(--ok)}.gelb{color:var(--warn)}.rot{color:var(--danger)}.k{font-variant-numeric:tabular-nums}
form{display:inline}input:not([type]),input[type=text],input[type=email]{font:inherit;color:var(--ink);background:var(--card);border:1px solid var(--edge);border-radius:999px;padding:0 16px;min-height:44px;max-width:100%}
.zeilen{list-style:none;margin:0;padding:0;background:var(--card);border:1px solid var(--edge);border-radius:14px;overflow:hidden}
.zeilen li{display:flex;gap:10px;align-items:flex-start;padding:10px 14px;border-bottom:1px solid var(--line)}.zeilen li:last-child{border-bottom:0}
.zeilen .was{flex:1;min-width:0;overflow-wrap:break-word}.zeilen b{font-weight:600}.zeilen .wann{display:block;font-size:.8rem;color:var(--muted);font-variant-numeric:tabular-nums}
.zeilen .text{display:block;font-size:.9rem;color:var(--muted)}.punkt.gelb{background:var(--warn)}.punkt.rot{background:var(--danger)}.punkt.k{background:var(--faint)}
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
		if r.URL.Path == BoxPartPath {
			boxPartAnswer(w, r, cfg)
			return
		}
		writeBox(w, r, cfg, http.StatusOK, invitation{})
	}
}

// BoxPartPath is where the Box page's script fetches each part (haus#81).
const BoxPartPath = "/box/teil"

// boxPartWait is how long one part may take: laib's status takes a second,
// the journal a few hundred kilobytes. A variable for the tests.
var boxPartWait = 10 * time.Second

// saysPartLoading and saysPartFailed are a part's place before it came,
// and when it did not.
const (
	saysPartLoading = "lädt …"
	saysPartFailed  = "Konnte nicht geladen werden. Die Seite neu laden; sonst weiß laib diag mehr."
)

// boxSlot is one place on the Box page: its element, its heading while
// it loads, and the part that fills it.
type boxSlot struct{ id, title, part string }

// boxSlots are the Box page's places, in the page's order; a part may
// fill several (laib's status: the top and Was läuft).
func boxSlots(cfg BoxConfig) []boxSlot {
	slots := []boxSlot{{"box-stand", "", "stand"}, {"box-inbox", "Inbox", "inbox"}, {"box-einladen", "Person einladen", "einladen"},
		{"box-bericht", "Morgenbericht", "journal"}}
	if cfg.Family.Usage != nil {
		slots = append(slots, boxSlot{"box-genutzt", "Was wird genutzt", "genutzt"})
	}
	if cfg.Tickets != nil {
		slots = append(slots, boxSlot{"box-werkstatt", "Werkstatt", "werkstatt"})
	}
	return append(slots, boxSlot{"box-laeuft", "Was läuft", "stand"}, boxSlot{"box-journal", "Journal · 24h", "journal"})
}

// boxPart reads one part and renders its places: slot id → HTML. Each
// part is read on its own, within boxPartWait (haus#81).
func boxPart(ctx context.Context, r *http.Request, cfg BoxConfig, part string, inv invitation) map[string]string {
	ctx, cancel := context.WithTimeout(ctx, boxPartWait)
	defer cancel()
	c := cfg.Laib
	switch part {
	case "stand":
		ready, rows, err := c.Status(ctx)
		if err != nil {
			return map[string]string{"box-stand": "<h1>Box</h1><p class=rot>Laib antwortet nicht: " + html.EscapeString(err.Error()) + "</p>", "box-laeuft": ""}
		}
		var b strings.Builder
		// the three guarantees, for everyone in the family (haus#2)
		b.WriteString(guarantees(ready, rows, isAdmin(r), cfg.LaibURL))
		fmt.Fprintf(&b, "<h1>Box · <span class=%s>%s</span> <span class=k>· %d Dienste · %d grün · %d gelb · %d rot · Drift %d</span></h1>",
			readyClass(ready), html.EscapeString(ready.State), ready.Services, ready.Green, ready.Yellow, ready.Red, ready.Drift)
		fmt.Fprintf(&b, "<p>Operator: <span class=%s>%s</span></p>", operatorClass(ready.Operator), html.EscapeString(ready.Operator))
		if ready.Lock != "" {
			fmt.Fprintf(&b, "<p class=gelb>gesperrt: %s</p>", html.EscapeString(ready.Lock))
		}
		// one line per service, its state a dot (the word for a screen
		// reader); what is not green first
		var l strings.Builder
		l.WriteString("<h2>Was läuft</h2><ul class=zeilen>")
		order := map[string]int{"rot": 0, "gelb": 1, "gruen": 2}
		sorted := append([]laib.Service(nil), rows...)
		sort.SliceStable(sorted, func(i, j int) bool { return order[cls(sorted[i].State)] < order[cls(sorted[j].State)] })
		for _, s := range sorted {
			fmt.Fprintf(&l, "<li>%s<span class=was><b>%s</b>", dot(cls(s.State), s.State), html.EscapeString(s.Name))
			if d := oneLine(s.Detail); d != "" {
				l.WriteString("<span class=text>" + html.EscapeString(d) + "</span>")
			}
			l.WriteString("</span></li>")
		}
		l.WriteString("</ul>")
		return map[string]string{"box-stand": b.String(), "box-laeuft": l.String()}
	case "inbox":
		var b strings.Builder
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
			fmt.Fprintf(&b, "<p><a href=\"%s/inbox\">Entscheiden auf der Laib-Seite</a> <span class=k>— die eine Stelle, an der freigegeben wird</span></p>", html.EscapeString(strings.TrimRight(cfg.LaibURL, "/")))
		}
		return map[string]string{"box-inbox": b.String()}
	case "einladen":
		return map[string]string{"box-einladen": inviteCard(ctx, c, inv)}
	case "journal":
		// the window reaches back to yesterday's report; the journal table stays at 24h
		es, jerr := c.Journal(ctx, "36h")
		var b strings.Builder
		// one entry a line: when and who small above, what and why below,
		// the result a dot
		b.WriteString("<h2>Journal · 24h</h2><ul class=zeilen>")
		if jerr == nil {
			shown := 0
			for i := len(es) - 1; i >= 0 && shown < 25; i-- {
				e := es[i]
				if time.Since(e.T) > 24*time.Hour {
					break
				}
				shown++
				what := e.Verb
				if e.Service != "" && e.Service != e.Verb {
					what += " " + e.Service
				}
				fmt.Fprintf(&b, "<li>%s<span class=was><span class=wann>%s · %s</span><b>%s</b>", dot(result(e.Result), e.Result),
					e.T.Local().Format("02.01. 15:04"), html.EscapeString(strings.TrimPrefix(e.Actor, "human:")), html.EscapeString(what))
				if why := trim(oneLine(e.Reason+" "+e.Detail), 140); why != "" {
					b.WriteString("<span class=text>" + html.EscapeString(why) + "</span>")
				}
				b.WriteString("</span></li>")
			}
		}
		b.WriteString("</ul>")
		return map[string]string{"box-bericht": morningReport(es, jerr), "box-journal": b.String()}
	case "genutzt":
		return map[string]string{"box-genutzt": usageCard(ctx, cfg.Family, cfg.Voice)}
	case "werkstatt":
		if cfg.Tickets != nil {
			return map[string]string{"box-werkstatt": werkstattCard(ctx, cfg.Tickets)}
		}
	}
	return nil
}

// boxPartAnswer answers BoxPartPath?was=<part>: that part's places as
// JSON, {"<slot id>": "<html>", …}; an unknown part is a 404.
func boxPartAnswer(w http.ResponseWriter, r *http.Request, cfg BoxConfig) {
	part := r.URL.Query().Get("was")
	known := false
	for _, s := range boxSlots(cfg) {
		known = known || s.part == part
	}
	if !known {
		http.NotFound(w, r)
		return
	}
	body, _ := json.Marshal(boxPart(r.Context(), r, cfg, part, invitation{}))
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(body)
}

// writeBox renders the Box page (haus#81): the frame at once, each part in
// its place as it comes — the page's script fetches every part from
// BoxPartPath at the same time, each on its own. With ?ganz=1, and
// without script (noscript sends there), all parts are read at the same
// time and the page comes whole. inv is what the invite card says after a
// POST to /box/einladen (haus#56): that card is then on the page at once.
func writeBox(w http.ResponseWriter, r *http.Request, cfg BoxConfig, code int, inv invitation) {
	slots := boxSlots(cfg)
	filled := map[string]string{}
	whole := r.URL.Query().Get("ganz") == "1"
	if whole || !inv.empty() {
		var mu sync.Mutex
		var wg sync.WaitGroup
		seen := map[string]bool{}
		for _, s := range slots {
			if seen[s.part] || !whole && s.part != "einladen" {
				continue
			}
			seen[s.part] = true
			wg.Add(1)
			go func() {
				defer wg.Done()
				got := boxPart(r.Context(), r, cfg, s.part, inv)
				mu.Lock()
				defer mu.Unlock()
				for id, h := range got {
					filled[id] = h
				}
			}()
		}
		wg.Wait()
	}
	b := adminHead(r, cfg.Family, "Box")
	b.WriteString(unterNav("Box"))
	var parts []string
	seen := map[string]bool{}
	for _, s := range slots {
		h, ok := filled[s.id]
		if !ok {
			if s.title == "" {
				h = "<h1>Box</h1>"
			} else {
				h = "<h2>" + html.EscapeString(s.title) + "</h2>"
			}
			h += "<p class=k>" + saysPartLoading + "</p>"
			if !seen[s.part] {
				seen[s.part] = true
				parts = append(parts, s.part)
			}
		}
		b.WriteString("<div id=" + s.id + " data-teil=" + s.part + " aria-busy=" + strconv.FormatBool(!ok) + ">" + h + "</div>")
	}
	if len(parts) > 0 {
		p, _ := json.Marshal(parts)
		failed, _ := json.Marshal("<p class=rot>" + saysPartFailed + "</p>")
		b.WriteString(`<noscript><meta http-equiv=refresh content="0;url=/box?ganz=1"></noscript>` +
			`<script>(function(){if(!window.fetch){location.replace('/box?ganz=1');return}` + string(p) + `.forEach(function(was){` +
			`fetch('` + BoxPartPath + `?was='+was,{credentials:'same-origin'}).then(function(r){if(!r.ok)throw new Error(r.status);return r.json()})` +
			`.then(function(j){Object.keys(j).forEach(function(id){var e=document.getElementById(id);if(e){e.innerHTML=j[id];e.setAttribute('aria-busy','false')}})})` +
			`.catch(function(){document.querySelectorAll('[data-teil="'+was+'"]').forEach(function(e){e.querySelector('p').outerHTML=` + string(failed) + `;e.setAttribute('aria-busy','false')})})})})()</script>`)
	}
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

// dot is a state as a coloured dot, with its word for a screen reader.
func dot(class, word string) string {
	return `<span class="punkt ` + class + `" role=img aria-label="` + html.EscapeString(word) + `"></span>`
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
