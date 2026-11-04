package pages

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mdopp/homes/internal/cards"
	"github.com/mdopp/homes/internal/laib"
	"github.com/mdopp/homes/internal/modul"
	"github.com/mdopp/homes/internal/texte"
)

// What a box lacks leaves a trace (homes ADR 0001 §10, owner 2026-11-04:
// „Keine Funktion soll versteckt werden.“): Mehr ends with a quiet card,
// one line per module that is off — its title and where it stands — and
// for a signed-in person „Hier anfordern“, a wish to Laib (laib#1038).
// Grey, no red, no warning sign: it reads as „gibt es hier noch nicht“.
// The words are internal/texte's.
const (
	SpurenPath    = "/mehr/spuren"    // the card with Laib's wishes, after the page
	AnfordernPath = "/mehr/anfordern" // POST modul: the wish
	ModulPages    = "/module/"        // /module/<name>, admins: where a wish's Box-Aufgabe leads
)

// wishWait is how long „Hier anfordern“ waits for Laib to file the wish.
var wishWait = 20 * time.Second

const spurenStyle = `<style>.spuren li{color:var(--muted)}.spuren .spur{font-weight:500}.spuren .k{color:var(--muted)}
.spuren button{min-height:44px;padding:0 14px;background:var(--track);color:var(--ink);border-radius:12px;font:inherit;font-size:.9rem}</style>`

// spurenKarte is Mehr's card of traces; "" on a box that lacks nothing.
// Mehr never waits for Laib: with its stand kept (a minute) the card is
// whole, else it shows the traces' own words and fills itself from
// SpurenPath.
func spurenKarte(r *http.Request, cfg StartConfig) string {
	off := spurModule(cfg)
	if len(off) == 0 {
		return ""
	}
	if cfg.Laib == nil {
		return spurenCard(r, off, modul.Wuensche{}, "")
	}
	if je, ok := cfg.Laib.PeekWuensche(cfg.Module.Frage()); ok {
		return spurenCard(r, off, laib.ModulWuensche(je, true), "")
	}
	return nachladen(SpurenPath, spurenCard(r, off, modul.Wuensche{}, ""), spurenCard(r, off, modul.Wuensche{}, texte.T(texte.SpurenStandNicht)))
}

// spurModule are the modules whose trace is on Mehr: off, and not the
// admins' own (their trace is on the Box page).
func spurModule(cfg StartConfig) []modul.Modul {
	if cfg.Module == nil {
		return nil
	}
	var out []modul.Modul
	for _, m := range cfg.Module.Module() {
		if !m.An() && !m.Admins {
			out = append(out, m)
		}
	}
	return out
}

// wuensche asks Laib for the wishes within backendWait; ok is false when
// Laib does not answer (an older Laib answers, and takes none).
func wuensche(ctx context.Context, cfg StartConfig) (modul.Wuensche, bool) {
	ctx, cancel := context.WithTimeout(ctx, backendWait)
	defer cancel()
	if cfg.Laib == nil {
		return modul.Wuensche{}, true
	}
	if geht, err := cfg.Laib.NimmtWuensche(ctx); !geht {
		return modul.Wuensche{}, err == nil
	}
	je, err := cfg.Laib.Wuensche(ctx, cfg.Module.Frage())
	if err != nil {
		log.Printf("homes: spuren: the wishes from Laib: %v", err)
	}
	return laib.ModulWuensche(je, true), err == nil || je != nil
}

// spurenCard draws the traces; note, when set, is a last quiet line.
func spurenCard(r *http.Request, off []modul.Modul, ws modul.Wuensche, note string) string {
	var b strings.Builder
	b.WriteString(spurenStyle + `<section class="karte spuren" aria-labelledby=spuren><h2 id=spuren>` + html.EscapeString(texte.T(texte.SpurenTitel)) +
		`</h2><p class=hinweis>` + html.EscapeString(texte.T(texte.SpurenHinweis)) + `</p><ul>`)
	signedIn, back := r.Header.Get("Remote-User") != "", ""
	if r.URL.Path != mehrPath && r.URL.Path != SpurenPath { // a module's own page: the wish ends there (modulAus)
		back = `<input type=hidden name=zurueck value="` + html.EscapeString(r.URL.Path) + `">`
	}
	for _, m := range off {
		spur := texte.T(texte.ID("spur." + m.Spur(wunschVon(ws, m.Name))))
		b.WriteString(`<li class=tu id="spur-` + html.EscapeString(m.Name) + `"><div><span class=spur>` + html.EscapeString(m.Titel) +
			`</span> <span class=k>` + html.EscapeString(spur) + `</span></div>`)
		if signedIn && m.Anfordern(ws) {
			b.WriteString(`<form method=post action=` + AnfordernPath + `><input type=hidden name=modul value="` + html.EscapeString(m.Name) +
				`">` + back + `<button data-laeuft="` + html.EscapeString(texte.T(texte.Anfordernd)) + `" aria-label="` + html.EscapeString(m.Titel+": "+texte.T(texte.Anfordern)) +
				`">` + html.EscapeString(texte.T(texte.Anfordern)) + `</button></form>`)
		}
		b.WriteString(`</li>`)
	}
	if note != "" {
		b.WriteString(`<li class=voll><span class=k>` + html.EscapeString(note) + `</span></li>`)
	}
	b.WriteString(`</ul></section>`)
	return b.String()
}

func wunschVon(ws modul.Wuensche, name string) *modul.Wunsch {
	if w, ok := ws.Je[name]; ok {
		return &w
	}
	return nil
}

// spuren answers SpurenPath: the card with Laib's wishes, or with the
// line that they cannot be read; "" when the box lacks nothing.
func spuren(w http.ResponseWriter, r *http.Request, cfg StartConfig) {
	if !showOnly(w, r, "Mehr") {
		return
	}
	card := ""
	if off := spurModule(cfg); len(off) > 0 {
		ws, ok := wuensche(r.Context(), cfg)
		note := ""
		if !ok {
			note = texte.T(texte.SpurenStandNicht)
		}
		card = spurenCard(r, off, ws, note)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write([]byte(card))
}

// Anfordern is AnfordernPath: POST modul files the signed-in person's wish
// for an off module with Laib (laib#1038), then back to Mehr, where the
// trace shows its new state. A refusal is one sentence, said on the page.
func Anfordern(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, texte.T(texte.AnfordernGibtsNicht), http.StatusMethodNotAllowed)
			return
		}
		say := func(code int, id texte.ID) { http.Error(w, texte.T(id), code) }
		user := r.Header.Get("Remote-User")
		var m modul.Modul
		ok := cfg.Module != nil && fromHomes(r)
		if ok {
			m, ok = cfg.Module.Eins(r.FormValue("modul"))
		}
		switch {
		case !ok || m.An():
			say(http.StatusNotFound, texte.AnfordernGibtsNicht)
			return
		case cfg.Laib == nil:
			say(http.StatusServiceUnavailable, texte.AnfordernKeinLaib)
			return
		}
		ws, _ := wuensche(r.Context(), cfg)
		if !ws.Geht {
			say(http.StatusServiceUnavailable, texte.AnfordernKeinLaib)
			return
		}
		if m.Anfordern(ws) { // one open already: nothing filed again
			ctx, cancel := context.WithTimeout(r.Context(), wishWait)
			_, err := cfg.Laib.Wuenschen(ctx, m.Name, m.Titel, user)
			cancel()
			switch {
			case errors.Is(err, context.DeadlineExceeded):
				log.Printf("homes: anfordern %s for %s: %v", m.Name, user, err)
				say(http.StatusGatewayTimeout, texte.AnfordernStumm)
				return
			case err != nil:
				log.Printf("homes: anfordern %s for %s: %v", m.Name, user, err)
				say(http.StatusBadGateway, texte.AnfordernNicht)
				return
			}
			log.Printf("homes: anfordern %s for %s", m.Name, user)
		}
		to := mehrPath + "#spur-" + m.Name
		if z := r.FormValue("zurueck"); strings.HasPrefix(z, "/") && !strings.HasPrefix(z, "//") && !strings.ContainsAny(z, `\?#`) {
			to = z // back to the module's own page (modulAus)
		}
		http.Redirect(w, r, to, http.StatusSeeOther)
	}
}

// modulAus is the page at path of a module this box lacks (the audit, owner 2026-11-04): its
// trace and „Hier anfordern“, as on Mehr, for an admin the module's page; below path, a 404.
func modulAus(w http.ResponseWriter, r *http.Request, cfg StartConfig, name, path string) {
	if r.URL.Path != path {
		Elsewhere(cfg)(w, r)
		return
	}
	m, ok := modul.Modul{Name: name, Titel: texte.Titel(name), Warum: modul.Fehlt}, false
	if x, found := cfg.Module.Eins(name); found && !x.An() { // on, its backend unset: nothing to wish for
		m, ok = x, true
	}
	var ws modul.Wuensche
	if ok {
		ws, _ = wuensche(r.Context(), cfg)
	}
	b := familyHead(r, cfg, m.Titel, m.Titel)
	b.WriteString("<h1>" + html.EscapeString(m.Titel) + "</h1>" + spurenCard(r, []modul.Modul{m}, ws, ""))
	if ok && isAdmin(r) {
		b.WriteString(`<p class=ruhig><a href="` + ModulPages + name + `">` + html.EscapeString(texte.T(texte.ModulSeiteTitel)) + `</a></p>`)
	}
	writePage(w, http.StatusOK, b)
}

// ModulSeite is /module/<name> (admins): one module, its backend, whether
// it is on and why not, its wish — where a wish's Box-Aufgabe leads (its
// weg, laib#1038) — and its switch (H7): a proposal to Laib, which the
// owner approves in Laib's Inbox. Homes decides nothing (haus#1).
func ModulSeite(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var m modul.Modul
		ok := cfg.Module != nil && isAdmin(r)
		if ok {
			m, ok = cfg.Module.Eins(strings.TrimPrefix(r.URL.Path, ModulPages))
		}
		if !ok {
			Elsewhere(cfg)(w, r)
			return
		}
		if r.Method == http.MethodPost {
			modulSchalten(w, r, cfg, m)
			return
		}
		if !showOnly(w, r, m.Titel) {
			return
		}
		b := familyHead(r, cfg, texte.T(texte.ModulSeiteTitel)+": "+m.Titel, "Mehr")
		b.WriteString("<h1>" + html.EscapeString(m.Titel) + "</h1>")
		if nr := r.URL.Query().Get("vorgeschlagen"); nr != "" {
			b.WriteString(`<p class=hinweis role=status>` + html.EscapeString(texte.T(texte.Vorgeschlagen)) + ` <a href="` +
				html.EscapeString(strings.TrimRight(cfg.LaibURL, "/")+"/inbox") + `">` + html.EscapeString(texte.T(texte.ZurInbox)) + `</a></p>`)
		}
		c := cards.Card{Title: texte.T(texte.ModulSeiteTitel)}
		if m.Backend != "" {
			c.Add(texte.T(texte.ModulDienst), m.Backend)
		}
		if m.An() {
			c.Add(texte.T(texte.ModulStand), texte.T(texte.ModulAn))
		} else {
			c.Add(texte.T(texte.ModulStand), texte.T(texte.ID("spur."+m.Warum))+" · "+m.Grund)
		}
		ws, _ := wuensche(r.Context(), cfg)
		if x := wunschVon(ws, m.Name); x != nil {
			c.Add(texte.T(texte.ModulWunsch), texte.T(texte.ID("spur."+m.Spur(x)))+" · "+x.Seit)
		}
		c.AddLink("Box-Aufgaben", "", AufgabenPath)
		b.WriteString(c.HTML())
		b.WriteString(schalterForm(m))
		writePage(w, http.StatusOK, b)
	}
}

// schalterForm is the module's switch for an admin: off for one that is
// on, on again for one switched off; none for one whose backend is
// missing (its reason says what to install) or that cannot be switched.
func schalterForm(m modul.Modul) string {
	tun, label := "", ""
	switch {
	case !m.Schaltbar:
		return ""
	case m.An():
		tun, label = "aus", texte.T(texte.AusVorschlagen)
	case m.Warum == modul.Aus:
		tun, label = "an", texte.T(texte.AnVorschlagen)
	default:
		return ""
	}
	return `<form method=post action="` + html.EscapeString(ModulPages+m.Name) + `"><input type=hidden name=tun value=` + tun +
		`><button data-laeuft="` + html.EscapeString(texte.T(texte.Vorschlagend)) + `">` + html.EscapeString(label) + `</button></form>`
}

// modulSchalten proposes MODULE_AUS with m switched (laib#1036): the list
// of modules switched off now, with m added or taken out. Then back to
// the module's page, which says the proposal waits; a refusal is one
// sentence, said on the page.
func modulSchalten(w http.ResponseWriter, r *http.Request, cfg StartConfig, m modul.Modul) {
	say := func(code int, id texte.ID) { http.Error(w, texte.T(id), code) }
	an := r.FormValue("tun") == "an"
	switch {
	case !fromHomes(r):
		http.Error(w, "Das geht nur von Homes' eigener Seite.", http.StatusForbidden)
		return
	case !m.Schaltbar || an == (m.Warum != modul.Aus) || !an && !m.An():
		say(http.StatusConflict, texte.NichtSchaltbar)
		return
	case cfg.Laib == nil:
		say(http.StatusServiceUnavailable, texte.VorschlagKeinLaib)
		return
	}
	var aus []string
	for _, x := range cfg.Module.Module() {
		if x.Warum == modul.Aus && x.Name != m.Name {
			aus = append(aus, x.Name)
		}
	}
	grund := fmt.Sprintf(texte.T(texte.GrundVorschlagAn), m.Titel)
	if !an {
		aus, grund = append(aus, m.Name), fmt.Sprintf(texte.T(texte.GrundVorschlagAus), m.Titel)
	}
	ctx, cancel := context.WithTimeout(r.Context(), wishWait)
	defer cancel()
	if geht, err := cfg.Laib.NimmtModulAus(ctx); !geht && err == nil {
		say(http.StatusServiceUnavailable, texte.VorschlagKeinLaib)
		return
	}
	p, err := cfg.Laib.ModulAusVorschlagen(ctx, strings.Join(aus, ","), grund)
	switch {
	case errors.Is(err, laib.ErrNoTool):
		say(http.StatusServiceUnavailable, texte.VorschlagKeinLaib)
		return
	case errors.Is(err, context.DeadlineExceeded):
		say(http.StatusGatewayTimeout, texte.VorschlagStumm)
		return
	case err != nil:
		log.Printf("homes: modul %s %s vorschlagen: %v", m.Name, r.FormValue("tun"), err)
		say(http.StatusBadGateway, texte.VorschlagNicht)
		return
	}
	log.Printf("homes: %s schlägt vor: %s %s, Vorschlag #%d", r.Header.Get("Remote-User"), m.Name, r.FormValue("tun"), p.Proposal)
	http.Redirect(w, r, ModulPages+m.Name+"?vorgeschlagen="+strconv.Itoa(max(p.Proposal, 1)), http.StatusSeeOther)
}
