package pages

import (
	"context"
	"errors"
	"html"
	"log"
	"net/http"
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
.spuren button{min-height:44px;padding:0 14px;background:none;color:var(--ink);border:1px solid var(--edge);border-radius:12px;font:inherit;font-size:.9rem}</style>`

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
	signedIn := r.Header.Get("Remote-User") != ""
	for _, m := range off {
		spur := texte.T(texte.ID("spur." + m.Spur(wunschVon(ws, m.Name))))
		b.WriteString(`<li class=tu id="spur-` + html.EscapeString(m.Name) + `"><div><span class=spur>` + html.EscapeString(m.Titel) +
			`</span> <span class=k>` + html.EscapeString(spur) + `</span></div>`)
		if signedIn && m.Anfordern(ws) {
			b.WriteString(`<form method=post action=` + AnfordernPath + `><input type=hidden name=modul value="` + html.EscapeString(m.Name) +
				`"><button data-laeuft="` + html.EscapeString(texte.T(texte.Anfordernd)) + `" aria-label="` + html.EscapeString(m.Titel+": "+texte.T(texte.Anfordern)) +
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
		http.Redirect(w, r, mehrPath+"#spur-"+m.Name, http.StatusSeeOther)
	}
}

// ModulSeite is /module/<name> (admins): one module, its backend, whether
// it is on and why not, and its wish — where a wish's Box-Aufgabe leads
// (its weg, laib#1038). Homes decides nothing: switching is H7's proposal.
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
		if !showOnly(w, r, m.Titel) {
			return
		}
		b := familyHead(r, cfg, texte.T(texte.ModulSeiteTitel)+": "+m.Titel, "Mehr")
		b.WriteString("<h1>" + html.EscapeString(m.Titel) + "</h1>")
		c := cards.Card{Title: texte.T(texte.ModulSeiteTitel)}
		if m.Backend != "" {
			c.Add("Dienst", m.Backend)
		}
		if m.An() {
			c.Add("Stand", "an")
		} else {
			c.Add("Stand", texte.T(texte.ID("spur."+m.Warum))+" · "+m.Grund)
		}
		ws, _ := wuensche(r.Context(), cfg)
		if x := wunschVon(ws, m.Name); x != nil {
			c.Add("Wunsch", texte.T(texte.ID("spur."+m.Spur(x)))+" · seit "+x.Seit)
		}
		c.AddLink("Box-Aufgaben", "Wer sich was gewünscht hat", AufgabenPath)
		b.WriteString(c.HTML())
		writePage(w, http.StatusOK, b)
	}
}
