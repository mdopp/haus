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
const adminStyle = `<style>.unter{display:flex;gap:8px;margin:18px 0 0;overflow-x:auto;padding-bottom:4px}.unter a{flex:none}.unter a{display:inline-flex;align-items:center;min-height:40px;padding:0 16px;border-radius:999px;background:var(--card);border:1px solid var(--edge);color:var(--ink);text-decoration:none;font-size:.85rem}.unter a[aria-current]{color:var(--accent);border-color:var(--accent)}
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

// boxPage is one of Box's pages (owner 2026-10-26: "zu viele vor allem
// lange Themen vermischt"): where it is, its pill, its heading.
type boxPage struct{ path, name, title string }

// boxPages are Box's pages in the pills' order; Geräte is its own handler.
var boxPages = []boxPage{
	{"/box", "Übersicht", ""},
	{AufgabenPath, "Aufgaben", "Box-Aufgaben"},
	{"/box/dienste", "Dienste", "Dienste"},
	{"/box/operator", "Operator", "Was der Operator kann"},
	{"/box/personen", "Personen", "Personen"},
	{"/box/nutzung", "Nutzung", "Nutzung"},
}

// boxPageOf is the page a path shows; a POST's answer shows the page its
// card is on (an invitation: Personen).
func boxPageOf(path string, cfg BoxConfig) (boxPage, bool) {
	if path == invitePath {
		path = "/box/personen"
	}
	for _, p := range boxPages {
		if p.path == path && (p.path != "/box/nutzung" || cfg.Family.Usage != nil) {
			return p, true
		}
	}
	return boxPage{}, false
}

// unterNav is Box's pages, the current one marked; Nutzung only where it
// is counted. It scrolls sideways on a phone.
func unterNav(current string, usage bool) string {
	var b strings.Builder
	b.WriteString("<nav class=unter aria-label=Box>")
	// Adresse is set once, when the box is set up: an admin's page, under Box (owner 2026-10-27)
	for _, p := range append(append([]boxPage{}, boxPages...), boxPage{"/box/geraete", "Geräte", ""}, boxPage{adressePath, "Adresse", ""}) {
		if p.name == "Nutzung" && !usage {
			continue
		}
		b.WriteString("<a href=" + p.path)
		if p.name == current {
			b.WriteString(" aria-current=page")
		}
		b.WriteString(">" + p.name + "</a>")
	}
	b.WriteString("</nav>")
	return b.String()
}

// Box is the admins' page: readiness, how many inbox cards wait, what is
// not green, the morning report. The full service list, the inbox's cards
// and the journal are Laib's own page's (haus#157, owner 2026-10-27);
// Box links there. laibURL is that page.
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
		if _, ok := boxPageOf(r.URL.Path, cfg); !ok {
			missing{http.StatusNotFound, "Diese Seite gibt es nicht.", "Box", "/box", "Box"}.write(w, r, cfg.Family)
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

// boxSlotsOn are one Box page's places, in its order; a part may fill
// several (laib's status: the top and Was läuft), each where it is.
func boxSlotsOn(cfg BoxConfig, page string) []boxSlot {
	switch page {
	case "/box":
		slots := []boxSlot{{"box-stand", "", "stand"}, {"box-inbox", "Inbox", "inbox"}, {"box-bericht", "Morgenbericht", "bericht"}}
		if cfg.Tickets != nil {
			slots = append(slots, boxSlot{"box-werkstatt", "Werkstatt", "werkstatt"})
		}
		return slots
	case AufgabenPath:
		return []boxSlot{{"box-aufgaben", "Box-Aufgaben", "aufgaben"}}
	case "/box/dienste":
		return []boxSlot{{"box-dienste", "Nicht grün", "stand"}}
	case "/box/personen":
		return []boxSlot{{"box-einladen", "Person einladen", "einladen"}}
	case "/box/operator": // owner 2026-10-27: the skills, read only (laib#839)
		return []boxSlot{{"box-operator", "Was der Operator kann", "operator"}}
	case "/box/nutzung":
		if cfg.Family.Usage != nil {
			return []boxSlot{{"box-genutzt", "Was wird genutzt", "genutzt"}}
		}
	}
	return nil
}

// boxSlots are all Box's places: the parts the script may ask for.
func boxSlots(cfg BoxConfig) []boxSlot {
	var all []boxSlot
	for _, p := range boxPages {
		all = append(all, boxSlotsOn(cfg, p.path)...)
	}
	return all
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
			return map[string]string{"box-stand": "<h1>Box</h1><p class=rot>" + saysLaibSilent + "</p>" + einzelheiten(err.Error()), "box-dienste": ""}
		}
		var b strings.Builder
		// the three guarantees, for everyone in the family (haus#2)
		b.WriteString(guarantees(ready, rows, isAdmin(r), cfg.LaibURL))
		fmt.Fprintf(&b, "<h1>Box · <span class=%s>%s</span> <span class=k>· %d Dienste · %d grün · %d gelb · %d rot · Drift %d</span></h1>",
			readyClass(ready), html.EscapeString(ready.State), ready.Services, ready.Green, ready.Yellow, ready.Red, ready.Drift)
		fmt.Fprintf(&b, "<p>Operator: <span class=%s>%s</span></p>", operatorClass(ready.Operator), html.EscapeString(ready.Operator))
		if ready.Aufgaben > 0 { // the Box-Aufgaben's badge (haus#115)
			due := ""
			if ready.AufgabenFaellig > 0 {
				due = fmt.Sprintf(", <span class=gelb>%d fällig</span>", ready.AufgabenFaellig)
			}
			fmt.Fprintf(&b, "<p><a href=%s>%d Box-Aufgaben offen</a>%s</p>", AufgabenPath, ready.Aufgaben, due)
		}
		if ready.Lock != "" {
			fmt.Fprintf(&b, "<p class=gelb>gesperrt: %s</p>", html.EscapeString(ready.Lock))
		}
		// what is not green, on the overview and under Dienste; the full
		// list is Laib's page's (haus#157)
		notGreen := notGreenList(rows)
		if notGreen != "" {
			b.WriteString(notGreen + "<p>" + laibLink(cfg, "/", "Alle Dienste auf der Laib-Seite") + "</p>")
		}
		d := "<h2>Nicht grün</h2>" + notGreen
		if notGreen == "" {
			d = "<p class=gruen>Alle Dienste laufen.</p>"
		}
		d += "<p>" + laibLink(cfg, "/", "Alle Dienste auf der Laib-Seite") + "</p>"
		return map[string]string{"box-stand": b.String(), "box-dienste": d}
	case "inbox":
		// a count and the way to Laib's inbox: the cards and deciding are
		// Laib's page's (ADR 0012, haus#157)
		var b strings.Builder
		b.WriteString("<h2>Inbox</h2>")
		if ps, err := c.Inbox(ctx); err != nil {
			b.WriteString("<p class=gelb>Laib antwortet gerade nicht.</p>")
		} else {
			switch len(ps) {
			case 0:
				b.WriteString("<p class=k>Keine Karte wartet auf eine Entscheidung.</p>")
			case 1:
				b.WriteString("<p>1 Karte wartet auf eine Entscheidung.</p>")
			default:
				fmt.Fprintf(&b, "<p>%d Karten warten auf eine Entscheidung.</p>", len(ps))
			}
		}
		b.WriteString("<p>" + laibLink(cfg, "/inbox", "Entscheiden auf der Laib-Seite") + "</p>")
		return map[string]string{"box-inbox": b.String()}
	case "einladen":
		return map[string]string{"box-einladen": inviteCard(ctx, c, inv)}
	case "aufgaben":
		return map[string]string{"box-aufgaben": aufgabenCard(ctx, c, time.Now(), "", "")}
	case "operator":
		now, loc := cfg.Family.clock()
		return map[string]string{"box-operator": operatorCard(ctx, c, now, loc)}
	case "bericht":
		// the newest morning report only; the journal is Laib's page's
		// (haus#157)
		reports, err := c.JournalLatest(ctx, 1, "report")
		return map[string]string{"box-bericht": morningReport(reports, err) + "<p>" + laibLink(cfg, "/journal", "Das Journal auf der Laib-Seite") + "</p>"}
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
	writeBoxWith(w, r, cfg, code, inv, nil)
}

// writeBoxWith is writeBox with places filled already (a POST's outcome in
// its card, haus#115).
func writeBoxWith(w http.ResponseWriter, r *http.Request, cfg BoxConfig, code int, inv invitation, pre map[string]string) {
	page, _ := boxPageOf(r.URL.Path, cfg)
	if page.path == "" {
		page = boxPages[0]
	}
	slots := boxSlotsOn(cfg, page.path)
	filled := map[string]string{}
	for id, h := range pre {
		filled[id] = h
	}
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
	title := "Box"
	if page.title != "" {
		title = page.title
	}
	b := adminHead(r, cfg.Family, title)
	b.WriteString(unterNav(page.name, cfg.Family.Usage != nil))
	if page.title != "" {
		b.WriteString("<h1>" + html.EscapeString(page.title) + "</h1>")
	}
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

// notGreenList is the services that are not green as lines, red first,
// each by Laib's name with its detail; "" when all are green.
func notGreenList(rows []laib.Service) string {
	var bad []laib.Service
	for _, s := range rows {
		if cls(s.State) != "gruen" {
			bad = append(bad, s)
		}
	}
	if len(bad) == 0 {
		return ""
	}
	sort.SliceStable(bad, func(i, j int) bool { return cls(bad[i].State) == "rot" && cls(bad[j].State) != "rot" })
	var b strings.Builder
	b.WriteString("<ul class=zeilen>")
	for _, s := range bad {
		fmt.Fprintf(&b, "<li>%s<span class=was>%s", dot(cls(s.State), s.State), serviceLabel(s))
		if d := oneLine(s.Detail); d != "" {
			b.WriteString("<span class=text>" + linkAufgaben(html.EscapeString(trim(d, 160))) + "</span>")
		}
		b.WriteString("</span></li>")
	}
	b.WriteString("</ul>")
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
