// Package pages renders Homes' HTML. Behind Authelia: Caddy sends
// Remote-User, and Homes listens on loopback only, so nothing else reaches
// it. One page per file; no framework.
package pages

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mdopp/homes/internal/devices"
	"github.com/mdopp/homes/internal/laib"
	"github.com/mdopp/homes/internal/usage"
	"github.com/mdopp/homes/internal/werkstatt"
)

// adminHead opens an admin page (haus#58): the family pages' frame (#35)
// with Box marked in the tab bar, the admin cards' style and Box's own two
// links. No assistant's name: it appears only where you talk to it.
func adminHead(r *http.Request, cfg StartConfig, title string) *strings.Builder {
	b := familyHead(r, cfg, title, "Box")
	b.WriteString(adminStyle)
	return b
}

// adminStyle is the admin cards in the house palette: tables as cards,
// section labels, the states' colours, Box's sub-links as pills.
const adminStyle = `<style>.unter{display:flex;gap:6px;margin:18px 0 0}.unter a{flex:1 1 auto;display:inline-flex;align-items:center;justify-content:center;min-height:40px;padding:0 10px;border-radius:999px;background:var(--card);color:var(--ink);text-decoration:none;font-size:.85rem}.unter a[aria-current]{color:var(--accent);background:var(--accent-soft);font-weight:600}
#operator summary{display:flex;gap:.3em;align-items:center;min-height:44px;font-weight:600;cursor:pointer}#alle-skills{width:20px;height:20px}#alle-skills+label{display:inline-flex;align-items:center;min-height:44px;margin:0 0 6px 8px}#alle-skills:not(:checked)~ul .gut{display:none}
h2{font-size:.95rem;font-weight:700;color:var(--muted);margin:26px 2px 8px}
table{width:100%;border-collapse:separate;border-spacing:0;background:var(--card);border-radius:14px;overflow:hidden}
td,th{text-align:left;padding:10px 12px;border-bottom:1px solid var(--line);vertical-align:top;overflow-wrap:break-word}tr:last-child td{border-bottom:0}
.gruen{color:var(--ok)}.gelb{color:var(--warn)}.rot{color:var(--danger)}.k{font-variant-numeric:tabular-nums}
form{display:inline}input:not([type]),input[type=text],input[type=email]{font:inherit;color:var(--ink);background:var(--card);border:1px solid var(--edge);border-radius:999px;padding:0 16px;min-height:44px;max-width:100%}
.zeilen{list-style:none;margin:0;padding:0;background:var(--card);border-radius:14px;overflow:hidden}
.zeilen li{display:flex;gap:10px;align-items:flex-start;padding:10px 14px;border-bottom:1px solid var(--line)}.zeilen li:last-child{border-bottom:0}
.zeilen .was{flex:1;min-width:0;overflow-wrap:break-word}.zeilen b{font-weight:600}.zeilen .wann{display:block;font-size:.8rem;color:var(--muted);font-variant-numeric:tabular-nums}
.zeilen .text{display:block;font-size:.9rem;color:var(--muted)}.punkt.gelb{background:var(--warn)}.punkt.rot{background:var(--danger)}.punkt.k{background:var(--faint)}
code{overflow-wrap:anywhere;font:.8rem ui-monospace,SFMono-Regular,Menlo,monospace}</style>`

// boxPage is one of Box's pages (owner 2026-10-26: "zu viele vor allem
// lange Themen vermischt"): where it is, its tab, its heading.
type boxPage struct{ path, name, title string }

// boxPages are Box's four tabs (owner 2026-11-06), then Haushalt's rows
// that are pages of their own, under the tab Haushalt; Geräte and Adresse
// are their own handlers.
var boxPages = []boxPage{
	{"/box", "Übersicht", ""},
	{AufgabenPath, "Aufgaben", "Box-Aufgaben"},
	{auslastungPath, "Auslastung", "Auslastung"}, // owner 2026-11-06: what the box carries now (laib#1076)
	{haushaltPath, "Haushalt", "Haushalt"},
	{"/box/personen", "Haushalt", "Personen"},
	{"/box/nutzung", "Haushalt", "Nutzung"},
	{moduleBoxPath, "Haushalt", "Was Homes kann"}, // ADR 0001 H7: every module, its backend, its wish, its switch
}

// boxPageOf is the page a path shows; a POST's answer shows the page its
// card is on (an invitation: Personen).
func boxPageOf(path string, cfg BoxConfig) (boxPage, bool) {
	if path == invitePath {
		path = "/box/personen"
	}
	for _, p := range boxPages {
		if p.path == path && (p.path != "/box/nutzung" || cfg.Family.Usage != nil) && (p.path != moduleBoxPath || cfg.Family.Module != nil) {
			return p, true
		}
	}
	return boxPage{}, false
}

// unterNav is Box's four tabs, the current one marked; they fit a phone.
func unterNav(current string) string {
	var b strings.Builder
	b.WriteString("<nav class=unter aria-label=Box>")
	for _, p := range boxPages[:4] {
		b.WriteString("<a href=" + p.path)
		if p.name == current {
			b.WriteString(" aria-current=page")
		}
		b.WriteString(">" + p.name + "</a>")
	}
	b.WriteString("</nav>")
	return b.String()
}

// BoxConfig is what the Box page reads.
type BoxConfig struct {
	Laib    *laib.Client
	LaibURL string
	Tickets *werkstatt.Tickets // the Werkstatt card (haus#15); nil, no card
	Family  StartConfig        // what this box has, and Usage: the "Was wird genutzt" card (haus#50); nil Usage, no card
	Voice   *usage.Voice       // the voice control's counting, to say when HA refuses it
	Devices *devices.Store     // the paired phones, Haushalt's row Geräte; nil, no row
}

// BoxPage is the Box page with all its cards.
func BoxPage(cfg BoxConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			http.Error(w, "Homes entscheidet nichts: Freigeben und Ablehnen gibt es nur auf der Laib-Seite", http.StatusForbidden)
			return
		}
		if !adminOnly(w, r) {
			return
		}
		// Dienste and Operator are on the Übersicht since 2026-11-06; a bookmark finds them
		if to, ok := map[string]string{"/box/dienste": "/box", "/box/operator": "/box#operator"}[r.URL.Path]; ok {
			http.Redirect(w, r, to, http.StatusFound)
			return
		}
		if r.URL.Path == BoxPartPath {
			boxPartAnswer(w, r, cfg)
			return
		}
		if _, ok := boxPageOf(r.URL.Path, cfg); !ok {
			missing{http.StatusNotFound, "Diese Seite gibt es nicht.", "Box", "/box", "Box"}.write(w, r, cfg.Family)
			return
		}
		writeBox(w, r, cfg, http.StatusOK, invitation{}, nil)
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

// boxSlot is one place on a Box page: the part that fills it (its element
// is box-<part>) and its heading while it loads.
type boxSlot struct{ part, title string }

// boxSlotsOn are one Box page's places, in its order.
func boxSlotsOn(cfg BoxConfig, page string) []boxSlot {
	switch page {
	case "/box":
		return []boxSlot{{"stand", ""}, {"operator", "Operator"}}
	case AufgabenPath:
		if cfg.Tickets != nil { // owner 2026-11-06: the Werkstatt's tickets beside the Box-Aufgaben
			return []boxSlot{{"aufgaben", ""}, {"werkstatt", "Werkstatt"}}
		}
		return []boxSlot{{"aufgaben", ""}}
	case "/box/personen":
		return []boxSlot{{"einladen", "Person einladen"}}
	case "/box/nutzung":
		return []boxSlot{{"genutzt", "Was wird genutzt"}}
	}
	return []boxSlot{{strings.TrimPrefix(page, "/box/"), ""}} // auslastung, haushalt, module
}

// boxPart reads one part and renders it, within boxPartWait (haus#81).
func boxPart(ctx context.Context, r *http.Request, cfg BoxConfig, part string, inv invitation) string {
	ctx, cancel := context.WithTimeout(ctx, boxPartWait)
	defer cancel()
	c := cfg.Laib
	switch part {
	case "stand":
		return achtung(ctx, r, cfg)
	case "einladen":
		return inviteCard(ctx, c, inv)
	case "aufgaben":
		return aufgabenCard(ctx, c, time.Now(), "", "")
	case "haushalt":
		return haushaltCard(ctx, cfg)
	case "operator":
		now, loc := cfg.Family.clock()
		return operatorCard(ctx, c, now, loc)
	case "auslastung":
		return auslastungCard(ctx, c)
	case "genutzt":
		return usageCard(ctx, cfg.Family, cfg.Voice)
	case "module":
		return moduleCard(ctx, cfg.Family)
	case "werkstatt":
		return werkstattCard(ctx, cfg.Tickets)
	}
	return ""
}

// boxPartAnswer answers BoxPartPath?was=<part>: {"box-<part>": "<html>"};
// a part on none of the pages is a 404.
func boxPartAnswer(w http.ResponseWriter, r *http.Request, cfg BoxConfig) {
	part := r.URL.Query().Get("was")
	for _, p := range boxPages {
		for _, s := range boxSlotsOn(cfg, p.path) {
			if s.part == part {
				body, _ := json.Marshal(map[string]string{"box-" + part: boxPart(r.Context(), r, cfg, part, invitation{})})
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Cache-Control", "no-store")
				w.Write(body)
				return
			}
		}
	}
	http.NotFound(w, r)
}

// writeBox renders a Box page (haus#81): the frame at once, each part in
// its place as it comes — the page's script fetches every part from
// BoxPartPath at the same time, each on its own. With ?ganz=1, and
// without script (noscript sends there), all parts are read at the same
// time and the page comes whole. inv is what the invite card says after a
// POST to /box/einladen (haus#56): that card is then on the page at once;
// pre are parts filled already (a POST's outcome in its card, haus#115).
func writeBox(w http.ResponseWriter, r *http.Request, cfg BoxConfig, code int, inv invitation, pre map[string]string) {
	page, _ := boxPageOf(r.URL.Path, cfg)
	if page.path == "" {
		page = boxPages[0]
	}
	slots := boxSlotsOn(cfg, page.path)
	filled := map[string]string{}
	for part, h := range pre {
		filled[part] = h
	}
	whole := r.URL.Query().Get("ganz") == "1"
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, s := range slots {
		if _, done := filled[s.part]; done || !whole && (s.part != "einladen" || inv.empty()) {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			got := boxPart(r.Context(), r, cfg, s.part, inv)
			mu.Lock()
			defer mu.Unlock()
			filled[s.part] = got
		}()
	}
	wg.Wait()
	b := adminHead(r, cfg.Family, cmp.Or(page.title, "Box"))
	b.WriteString(unterNav(page.name))
	if page.title != "" {
		b.WriteString("<h1>" + html.EscapeString(page.title) + "</h1>")
	}
	var parts []string
	for _, s := range slots {
		h, ok := filled[s.part]
		if !ok {
			if s.part == "stand" {
				h = "<h1>Box</h1>"
			} else if s.title != "" {
				h = "<h2>" + html.EscapeString(s.title) + "</h2>"
			}
			h += "<p class=k>" + saysPartLoading + "</p>"
			parts = append(parts, s.part)
		}
		b.WriteString("<div id=box-" + s.part + " data-teil=" + s.part + " aria-busy=" + strconv.FormatBool(!ok) + ">" + h + "</div>")
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
	if page.path == auslastungPath {
		b.WriteString(auslastungStyle + auslastungScript)
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
		return "<h2>Morgenbericht</h2><p class=rot>" + saysLaibSilent + "</p>" + einzelheiten(err.Error())
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

// laibLink is a link to a place on Laib's own page.
func laibLink(cfg BoxConfig, path, text string) string {
	return "<a href=\"" + html.EscapeString(strings.TrimRight(cfg.LaibURL, "/")+path) + "\">" + text + "</a>"
}

// serviceLabel is a service as Laib's page shows it: its title, what it
// is beside it, its id small.
func serviceLabel(s laib.Service) string {
	out := "<b>" + html.EscapeString(s.Titel()) + "</b>"
	if d := strings.TrimSpace(s.Beschreibung); d != "" {
		out += " <span class=k>" + html.EscapeString(d) + "</span>"
	}
	if s.Titel() != s.Name {
		out += " <small class=k>" + html.EscapeString(s.Name) + "</small>"
	}
	return out
}
