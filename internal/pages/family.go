package pages

import (
	"context"
	"html"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mdopp/haus/internal/cards"
)

// The family's pages (haus#12): Start, Räume, Kalender, Notizen. They read
// one config (StartConfig), share one nav and the persona's header, and
// show without changing anything (haus#1). A page whose backend is not set
// is not served: it lands on Start. A path Haus does not serve at all is
// a 404 with the way to Start (Elsewhere).

// What a family page says, in one plain line where the rows would be,
// when a backend does not answer (haus#12, haus#33): what is missing, in
// the family's words, never the backend's name or its error (laib ADR
// 0021), which go to the log; once a page, and the page a 200. Inside a
// card when the card has other rows or names what is missing (Start's
// Jetzt and Haushalt, Kalender's Erinnerungen), else on its own where the
// cards would be; an item page is the line and the way back (missing).
const (
	saysNoHouse    = "Licht und Geräte antworten gerade nicht."
	saysNoRooms    = "Die Räume werden gerade gelesen — gleich nochmal versuchen."
	saysNoCalendar = "Der Kalender antwortet gerade nicht."
	saysNoContacts = "Die Kontakte lassen sich gerade nicht lesen."
	saysNoNotes    = "Die Notizen lassen sich gerade nicht lesen."
	saysWordsOnly  = "Die Suche nach Bedeutung geht gerade nicht; gesucht wird nur nach den Wörtern."
	saysNoMatch    = "Keine Notiz passt wirklich zu deiner Frage."
	byWordsTitle   = "Notizen mit Wörtern aus deiner Frage"
	// what to do when a search finds nothing (haus#13): no example word,
	// since every word of the query has just failed
	saysTryAnother  = "Versuch es mit einem anderen Wort, das in der Notiz stehen könnte."
	saysNoFavorites = "Deine Favoriten lassen sich gerade nicht lesen."
	saysFavsUnseen  = "Deine Favoriten sind gerade nicht zu sehen."
)

// tabs are the family's pages in the tab bar, in this order: the name,
// where it is, its icon (24×24, stroked).
var tabs = []struct{ name, href, icon string }{
	{"Start", "/start", `<path d="M4 11l8-7 8 7v9h-5v-6H9v6H4z"/>`},
	{"Räume", "/raeume", `<rect x="4" y="4" width="7" height="7" rx="1.5"/><rect x="13" y="4" width="7" height="7" rx="1.5"/><rect x="4" y="13" width="7" height="7" rx="1.5"/><rect x="13" y="13" width="7" height="7" rx="1.5"/>`},
	{"Kalender", "/kalender", `<rect x="4" y="5" width="16" height="15" rx="2"/><path d="M4 10h16M9 3v4M15 3v4"/>`},
	{"Notizen", "/notizen", `<path d="M6 4h9l3 3v13H6z"/><path d="M9 11h6M9 15h6"/>`},
	{"Box", "/box", `<rect x="3" y="7" width="18" height="12" rx="2"/><path d="M3 11h18M8 15h2"/>`},
}

// familyNav is the tab bar (haus#35): Start · Räume · Kalender · Notizen,
// each only when its backend is set, and Box for admins; the page itself
// (current, by name) is marked.
func familyNav(r *http.Request, cfg StartConfig, current string) string {
	show := map[string]bool{"Start": true, "Räume": cfg.HA != nil, "Kalender": cfg.Calendar != nil, "Notizen": cfg.Notes != nil, "Box": isAdmin(r)}
	var b strings.Builder
	b.WriteString("<nav class=tabs aria-label=Seiten>")
	for _, t := range tabs {
		if !show[t.name] {
			continue
		}
		b.WriteString("<a href=" + t.href)
		if t.name == current {
			b.WriteString(" aria-current=page")
		}
		b.WriteString("><svg viewBox=\"0 0 24 24\" aria-hidden=true>" + t.icon + "</svg>" + t.name + "</a>")
	}
	b.WriteString("</nav>")
	return b.String()
}

// frame is the family pages' look (haus#35): the house palette — the
// values of the chat's tokens and haus-android's colors.xml — dark
// first, light when the phone is; Geist from Haus itself, else the
// phone's own font; orange for what you can do, amber for what is on,
// green and red only for a state, never alone.
const frame = `<style>@font-face{font-family:Geist;src:url(` + FontPath + `) format("woff2");font-weight:100 900;font-display:swap}
:root{color-scheme:dark;--bg:#070709;--panel:#0c0c0e;--card:#14141a;--edge:rgba(255,255,255,.12);--line:rgba(255,255,255,.07);--ink:#f2f2f5;--muted:#9aa0ae;--faint:#6b7280;
--accent:#f97316;--accent-soft:rgba(249,115,22,.14);--state:#ffc107;--ok:#10b981;--warn:#f59e0b;--warn-soft:rgba(245,158,11,.12);--danger:#ef5350;--danger-soft:rgba(239,83,80,.12);--track:rgba(255,255,255,.15);--tile:linear-gradient(180deg,#17171d,#0d0d11)}
@media (prefers-color-scheme:light){:root{color-scheme:light;--bg:#f1f5f9;--panel:#ffffff;--card:#ffffff;--edge:rgba(15,23,42,.12);--line:rgba(15,23,42,.08);--ink:#0f172a;--muted:#64748b;--faint:#94a3b8;
--accent:#ea580c;--accent-soft:rgba(234,88,12,.10);--state:#b45309;--ok:#059669;--warn:#b45309;--warn-soft:rgba(180,83,9,.10);--danger:#dc2626;--danger-soft:rgba(220,38,38,.08);--track:rgba(15,23,42,.12);--tile:linear-gradient(180deg,#ffffff,#f6f7f9)}}
*{box-sizing:border-box}html{background:var(--bg)}
body{margin:0 auto;max-width:40rem;padding:0 16px calc(88px + env(safe-area-inset-bottom));background:var(--bg);color:var(--ink);font:16px/1.5 Geist,system-ui,-apple-system,"Segoe UI",Roboto,sans-serif;-webkit-text-size-adjust:100%}
h1{font-size:1.6rem;font-weight:700;letter-spacing:-.01em;line-height:1.2;margin:22px 0 2px;overflow-wrap:anywhere}.datum{margin:0;color:var(--muted);font-size:.9rem}
p{margin:12px 0}a{color:var(--accent)}.k{color:var(--muted)}
button{font:inherit;font-size:.85rem;font-weight:600;color:var(--accent);background:var(--accent-soft);border:0;border-radius:999px;padding:0 16px;min-height:40px;cursor:pointer;white-space:nowrap}
input[type=search]{font:inherit;color:var(--ink);background:var(--card);border:1px solid var(--edge);border-radius:999px;padding:0 16px;min-height:44px;width:min(24rem,62vw)}
form[role=search]{display:flex;gap:8px;margin:16px 0 0}.notiz{white-space:pre-wrap;overflow-wrap:anywhere}
.achtung{margin:16px 0 0;padding:12px 14px;background:var(--danger-soft);border:1px solid var(--danger);border-radius:14px}.achtung p{margin:0}.achtung p+p{margin-top:4px;color:var(--muted)}
.achtung p:first-child::before{content:"";display:inline-block;width:8px;height:8px;border-radius:50%;background:var(--danger);margin-right:8px;vertical-align:1px}
.achtung.selbst{background:var(--warn-soft);border-color:var(--warn)}.achtung.selbst p:first-child::before{background:var(--warn)}
.ruhig{display:flex;gap:8px;align-items:flex-start;margin:26px 2px 0;color:var(--muted);font-size:.8rem}.punkt{width:8px;height:8px;margin-top:.45em;border-radius:50%;background:var(--ok);flex:none}.punkt.grau{background:var(--faint)}
.stern{margin:20px 0 0}.stern p{margin:6px 2px 0;font-size:.85rem}
.tabs{position:fixed;left:0;right:0;bottom:0;z-index:2;display:flex;justify-content:center;background:var(--panel);border-top:1px solid var(--line);padding-bottom:env(safe-area-inset-bottom)}
.tabs a{flex:1;max-width:7.5rem;min-height:56px;display:flex;flex-direction:column;align-items:center;justify-content:center;gap:3px;color:var(--muted);text-decoration:none;font-size:.72rem}
.tabs a[aria-current]{color:var(--accent)}.tabs svg{width:22px;height:22px;fill:none;stroke:currentColor;stroke-width:1.8;stroke-linecap:round;stroke-linejoin:round}</style>`

// familyHead opens a family page: its title, the frame, the cards' style
// and the tab bar (current marks the page's tab). The page writes its own
// h1.
func familyHead(r *http.Request, cfg StartConfig, title, current string) *strings.Builder {
	b := &strings.Builder{}
	if title != "Zuhause" {
		title += " · Zuhause"
	}
	b.WriteString("<!doctype html><html lang=de><meta charset=utf-8><meta name=viewport content='width=device-width,initial-scale=1,viewport-fit=cover'>" +
		"<meta name=color-scheme content='dark light'><meta name=theme-color content='#0c0c0e'><title>" + html.EscapeString(title) + "</title>")
	b.WriteString(frame)
	b.WriteString(cards.Style)
	b.WriteString(familyNav(r, cfg, current))
	return b
}

// fromHaus is a POST the person sent from Haus's own page (haus#35): the
// browser says same-origin (Sec-Fetch-Site), or, from one too old to say,
// the Origin is Haus's own host; and Authelia names the person. A form on
// another site cannot switch the house with the family's session, and a
// POST that says neither is refused: Haus's own form always sends one.
func fromHaus(r *http.Request) bool {
	if r.Header.Get("Remote-User") == "" {
		return false
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" {
		return site == "same-origin"
	}
	if o := r.Header.Get("Origin"); o != "" {
		u, err := url.Parse(o)
		return err == nil && u.Host != "" && u.Host == r.Host
	}
	return false
}

// showOnly answers anything but GET and HEAD with 405: a page shows; what
// changes something has its own form and path (/schalten, a thing's ☆).
func showOnly(w http.ResponseWriter, r *http.Request, what string) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return true
	}
	w.Header().Set("Allow", "GET, HEAD")
	http.Error(w, what+" zeigt nur, ändert nichts.", http.StatusMethodNotAllowed)
	return false
}

// toStart sends a page whose backend is not set to Start.
func toStart(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/start", http.StatusFound)
}

// clock is the page's now, in the house's time zone.
func (cfg StartConfig) clock() (time.Time, *time.Location) {
	loc := cfg.Loc
	if loc == nil {
		loc = time.Local
	}
	now := time.Now()
	if cfg.Now != nil {
		now = cfg.Now()
	}
	return now.In(loc), loc
}

// fetch runs each backend read at once, each within backendWait: a slow
// one is left out, the page still comes.
func fetch(r *http.Request, reads ...func(context.Context)) {
	var wg sync.WaitGroup
	for _, f := range reads {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(r.Context(), backendWait)
			defer cancel()
			f(ctx)
		}()
	}
	wg.Wait()
}

// logErrs puts a page's backend errors in Haus's log, not on the page.
func logErrs(page string, errs map[string]error) {
	for what, err := range errs {
		if err != nil {
			log.Printf("haus: %s page: %s: %v", page, what, err)
		}
	}
}

// writePage sends the page.
func writePage(w http.ResponseWriter, code int, b *strings.Builder) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	w.Write([]byte(b.String()))
}

// missing is an item's page for something that is not there (404), or
// whose backend does not answer (haus#28, haus#33: a 200 with the page's
// plain line, like Start): one plain sentence and the way back, never
// Start pretending all is well.
type missing struct {
	code     int
	say      string // the one sentence
	page     string // the page's name, and the nav entry it marks
	back     string // where back leads
	backText string
}

func (m missing) write(w http.ResponseWriter, r *http.Request, cfg StartConfig) {
	tab := m.page
	if tab == "Zuhause" {
		tab = "Start"
	}
	b := familyHead(r, cfg, m.page, tab)
	b.WriteString("<h1>" + html.EscapeString(m.page) + "</h1><p>" + html.EscapeString(m.say) + "</p>")
	b.WriteString("<p><a href=" + m.back + ">" + html.EscapeString(m.backText) + "</a></p>")
	writePage(w, m.code, b)
}

// more closes a cut list: how many were left out.
func more(c *cards.Card, n int) {
	if n > 0 {
		c.Add("und "+strconv.Itoa(n)+" weitere", "")
	}
}
