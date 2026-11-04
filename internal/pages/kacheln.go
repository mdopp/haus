package pages

import (
	_ "embed"
	"fmt"
	"html"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mdopp/homes/internal/cards"
	"github.com/mdopp/homes/internal/ha"
	"github.com/mdopp/homes/internal/texte"
)

// kachel is a thing on Start as the app's tile (homes-android#245, A2;
// owner, 2026-11-04: „Bitte auf der Webseite genau so umsetzen!“): its
// picture on a round of its state's colour says the state, words only what
// the picture cannot. A tap switches it (a blind's opens its sheet), the
// level opens the sheet, a long press its page; a cover has ▲ ■ ▼.
type kachel struct {
	s       ha.State
	domain  string
	name    string          // without the room HA appends (kurzname)
	farbe   string          // the state's colour, a palette variable
	worte   string          // only what the picture cannot say
	stufe   *cards.Slider   // a dimmer's or a blind's level; nil, none
	tippen  *cards.Action   // what a tap does; nil: it opens the page
	knoepfe []*cards.Action // a cover's ▲ ■ ▼
	allein  bool            // left without a neighbour on a phone: the whole row
}

// neueKachel is s as a tile whose buttons land back on back.
func neueKachel(s ha.State, houseUnit, back string) kachel {
	domain, _, _ := strings.Cut(s.EntityID, ".")
	k := kachel{s: s, domain: domain, name: kurzname(friendly(s)), farbe: farbeVon(domain, s), worte: worteVon(domain, s, houseUnit)}
	switch domain {
	case "light":
		k.stufe = dimmer(s, back)
	case "cover":
		k.stufe = coverSlider(s, back)
		for _, a := range coverButtons(s, back) {
			a.Label, a.Bild = k.name+": "+a.Label, strings.ToLower(a.Label) // hoch, stopp, runter
			k.knoepfe = append(k.knoepfe, a)
		}
		if n := len(k.knoepfe); k.stufe == nil && n > 0 { // a tap opens what is shut, else shuts it
			k.tippen = k.knoepfe[map[bool]int{true: 0, false: n - 1}[s.State == "closed"]]
		}
		return k
	}
	if a := umschalten(s, back); a != nil {
		a.Label = k.name
		k.tippen = a
	}
	return k
}

// umschalten is a lamp's or a switch's switch to back; nil for anything else or out of reach.
func umschalten(s ha.State, back string) *cards.Action {
	domain, _, _ := strings.Cut(s.EntityID, ".")
	if domain != "light" && domain != "switch" || s.State != "on" && s.State != "off" {
		return nil
	}
	on := s.State == "on"
	return &cards.Action{Label: friendly(s), Entity: s.EntityID, Do: domain + map[bool]string{true: ".turn_off", false: ".turn_on"}[on], Back: back, Switch: true, On: on}
}

// art is what a thing is drawn as: a cover a garage („tor“, a garage or a
// gate, as the app's widgets) or a blind („rollo“); else its domain.
func art(domain string, s ha.State) string {
	if dc, _ := s.Attributes["device_class"].(string); domain == "cover" {
		return map[bool]string{true: "tor", false: "rollo"}[dc == "garage" || dc == "gate"]
	}
	return domain
}

// farben are the states' colours, as the app's widgets' (DeviceLook.tint):
// amber on, green shut and locked, orange a garage or a lock open, blue
// moving, red jammed; else grey, a dimmer grey for nothing known.
var farben = map[string]string{"lock.locked": "--ok", "lock.unlocked": "--warn", "lock.open": "--accent", "lock.jammed": "--danger", "rollo.open": "--state",
	"tor.open": "--warn", "tor.closed": "--ok", "on": "--state", "playing": "--state", "unavailable": "--faint", "unknown": "--faint", "": "--faint",
	"opening": "--unterwegs", "closing": "--unterwegs", "locking": "--unterwegs", "unlocking": "--unterwegs"}

// farbeVon is the state's colour, a palette variable.
func farbeVon(domain string, s ha.State) string {
	for _, key := range []string{art(domain, s) + "." + s.State, s.State} {
		if c, ok := farben[key]; ok {
			return c
		}
	}
	return map[bool]string{true: "--state", false: "--muted"}[domain == "climate" && s.State != "off"]
}

// worteVon is what a tile still says in words: a thing that does not
// answer, one on its way, a jammed lock, and what has no picture of its
// state (a heating's reading, a player). A lamp, a switch, a cover and a
// lock say nothing else: their picture does.
func worteVon(domain string, s ha.State, houseUnit string) string {
	switch c := farben[s.State]; {
	case c == "--unterwegs":
		return roomDetail(domain, s, houseUnit) + " …"
	case c != "--faint" && s.State != "jammed" && map[string]bool{"light": true, "switch": true, "cover": true, "lock": true}[domain]:
		return ""
	}
	return roomDetail(domain, s, houseUnit)
}

// kurzname is the name as one short line, as the app's: a last or first
// word that only repeats the start of another (the room HA appends,
// „Garagentor Garage“) goes — „Garagentor“.
func kurzname(name string) string {
	words := strings.Fields(name)
	repeats := func(w string, rest []string) bool {
		return utf8.RuneCountInString(w) >= 3 && slices.ContainsFunc(rest, func(r string) bool { return strings.HasPrefix(strings.ToLower(r), strings.ToLower(w)) })
	}
	switch n := len(words); {
	case n < 2:
	case repeats(words[n-1], words[:n-1]):
		words = words[:n-1]
	case repeats(words[0], words[1:]):
		words = words[1:]
	}
	return strings.Join(words, " ")
}

// bildVon is the widgets' picture of the thing in its state (DeviceLook.icon),
// a blind part-way the lowered one over the raised one down to its position.
func bildVon(domain string, s ha.State) string {
	name := art(domain, s)
	switch name {
	case "lock":
		w, ok := map[string]string{"locked": "zu", "locking": "zu", "unlocked": "auf", "unlocking": "auf", "open": "offen", "opening": "offen", "jammed": "klemmt"}[s.State]
		if !ok {
			w = "unbekannt" // no shackle: it claims nothing
		}
		name = "schloss-" + w
	case "tor", "rollo":
		name += map[bool]string{true: "-auf", false: "-zu"}[s.State == "open" || s.State == "opening"]
	case "light":
		name = map[bool]string{true: "licht-an", false: "licht-aus"}[s.State == "on"]
	}
	inner, ok := bilder[name]
	if !ok {
		inner = bilder["geraet"]
	}
	if pos, ok := number(s.Attributes["current_position"]); ok && name == "rollo-auf" && pos >= 1 && pos <= 99 {
		h := strconv.FormatFloat(24*(100-pos)/100, 'f', 1, 64)
		inner += `<svg width=24 height=` + h + ` viewBox="0 0 24 ` + h + `">` + bilder["rollo-zu"] + `</svg>`
	}
	return `<svg viewBox="0 0 24 24">` + inner + `</svg>`
}

// html is the tile as a grid item; the class says its layout.
func (k kachel) html() string {
	var b strings.Builder
	class := "kachel"
	if k.farbe != "--muted" && k.farbe != "--faint" && k.farbe != "--ok" {
		class += " aktiv" // on, open or moving: a wash of its colour
	}
	if len(k.knoepfe) > 0 {
		class += " breit"
	} else if k.allein {
		class += " allein"
	}
	page := html.EscapeString(cards.ThingPage(k.s.EntityID))
	b.WriteString(`<li class="` + class + `" style="--farbe:var(` + k.farbe + `)" data-seite="` + page + `"><a class=name href="` + page + `" style="--n:` +
		strconv.Itoa(utf8.RuneCountInString(k.name)) + `">` + html.EscapeString(k.name) + `</a>`)
	switch a := k.tippen; {
	case k.stufe != nil && k.domain == "cover":
		b.WriteString(`<span class=flaeche><button type=button popovertarget=` + k.stufe.ID() + ` aria-haspopup=dialog aria-label="` + html.EscapeString(k.name) + `"></button></span>`)
	case a != nil && a.Frage != nil: // the ▲'s question, asked once
		b.WriteString(`<span class=flaeche><button type=button popovertarget=` + a.FrageID() + ` aria-label="` + html.EscapeString(a.Label) + `"></button></span>`)
	case a != nil:
		b.WriteString(`<span class=flaeche>` + a.Form() + `</span>`)
	}
	b.WriteString(`<span class=bild aria-hidden=true>` + bildVon(k.domain, k.s) + `</span><span class=zeile>`)
	if k.worte != "" {
		b.WriteString(`<span class=worte>` + html.EscapeString(k.worte) + `</span>`)
	}
	if k.stufe != nil {
		b.WriteString(k.stufe.Button())
	}
	b.WriteString(`<span class=schaltet>` + html.EscapeString(texte.T(texte.GeraeteSchaltet)) + `</span></span>`)
	if len(k.knoepfe) > 0 {
		b.WriteString(`<span class=knoepfe>`)
		for _, a := range k.knoepfe {
			b.WriteString(a.Form())
		}
		b.WriteString(`</span>`)
	}
	if k.stufe != nil {
		b.WriteString(k.stufe.Sheet())
	}
	b.WriteString(`</li>`)
	return b.String()
}

// kachelnHTML is Deine Geräte: its head with „Alle →“, what a tap does, the
// tiles in the person's order: on a phone a lamp or a lock half the row, a
// cover and a tile left alone across it (allein); wider, cells (zelle).
// more did not fit; stand is the time of the values, said without the box.
func kachelnHTML(id string, ks []kachel, more int, stand string) string {
	for i := 0; i < len(ks); i++ {
		switch {
		case len(ks[i].knoepfe) > 0:
		case i+1 < len(ks) && len(ks[i+1].knoepfe) == 0:
			i++
		default:
			ks[i].allein = true
		}
	}
	var b strings.Builder
	b.WriteString(kachelStyle + `<section class="karte geraete" aria-labelledby=` + id + `><p class=stand>` + html.EscapeString(texte.T(texte.BoxWeg)) + `<br>` +
		html.EscapeString(fmt.Sprintf(texte.T(texte.Stand), stand)) + `</p><div class=kopfzeile><h2 id=` + id + `>` + html.EscapeString(texte.T(texte.Geraete)) +
		`</h2><a class=alle href=/raeume>` + html.EscapeString(texte.T(texte.GeraeteAlle)) + `</a></div><p class=hinweis>` + html.EscapeString(texte.T(texte.GeraeteHinweis)) + `</p><ul class=kacheln>`)
	for _, k := range ks {
		b.WriteString(k.html())
	}
	if more > 0 {
		b.WriteString(`<li class=mehr><a href=/raeume>und ` + strconv.Itoa(more) + ` weitere</a></li>`)
	}
	b.WriteString(`</ul></section>`)
	return b.String()
}

// bilder are the app's widget pictures (symbol/geraete.svg), the inside of a 24 × 24 <svg>.
var bilder = func() map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(geraeteSVG, `<symbol id="`)[1:] {
		name, rest, _ := strings.Cut(part, `"`)
		_, rest, _ = strings.Cut(rest, ">")
		out[name], _, _ = strings.Cut(rest, "</symbol>")
	}
	return out
}()

//go:embed symbol/geraete.svg
var geraeteSVG string

// wide lays a tile (sel) out as one line: picture, name over level or words, a cover's buttons.
func wide(sel string) string {
	return sel + `{grid-template-columns:auto minmax(0,1fr) auto;grid-template-rows:1fr auto auto 1fr;grid-template-areas:"bild . knoepfe" "bild name knoepfe" "bild zeile knoepfe" "bild . knoepfe";` +
		`column-gap:8px;min-height:64px;padding:6px 4px 6px 12px;--frei:52px}` + sel + ` .name{margin:0}` + sel + ` .bild{width:36px;height:36px}` + sel + ` .zeile{grid-area:zeile;display:flex;gap:6px;align-items:baseline;min-width:0}` +
		sel + ` .stufe{padding:0;font-size:.88rem}`
}

// zelle is the narrowest a cover's line fits in, px: picture, ▲ ■ ▼ and a
// name of 19 letters (Geist bold is 0.59 em a letter at most; the name's
// size reckons 0.66, 11 px at the least). From two cells on, every tile is
// one cell, one line, in the person's order (owner, 2026-11-04: mixed
// widths were „zu unruhig“); narrower, the app's two phone columns.
const zelle = 320

// kachelStyle: the house palette, no outlines, a wash of the state's colour while on, open
// or moving; grey under „Stand …“ without the box (html.weg, homesScript).
var kachelStyle = `<style>.geraete{--pille:#1e1e25;--unterwegs:#7e9cff}@media (prefers-color-scheme:light){.geraete{--pille:#e6e8ee;--unterwegs:#4f6bd8}}.geraete .kopfzeile{display:flex;justify-content:space-between;align-items:center;gap:12px}.geraete .kopfzeile h2{margin:0}
.geraete .alle{display:inline-flex;align-items:center;min-height:44px;font-weight:700;text-decoration:none}.karte.geraete .hinweis{margin:0 2px 10px}.geraete .stand{display:none;margin:0 0 14px;padding:12px 14px;background:var(--card);border-radius:14px}.weg .geraete .stand{display:block}
.karte.geraete{container:kacheln/inline-size}.karte ul.kacheln{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:8px;background:none;border:0;border-radius:0}
.karte .kachel{display:grid;grid-template-columns:auto minmax(0,1fr);grid-template-areas:"bild stufe" "name name" "worte worte";align-items:center;column-gap:8px;padding:12px;--frei:0px;
border:0;border-radius:20px;background:var(--card);overflow-wrap:normal;container-type:inline-size;-webkit-touch-callout:none;-webkit-user-select:none;user-select:none}
.karte .kachel.aktiv{background:color-mix(in srgb,var(--farbe) 14%,var(--card))}.weg .geraete .kachel{background:var(--card);opacity:.55}.weg .geraete .kachel .bild{color:var(--faint);background:var(--track)}
.kachel .bild{grid-area:bild;display:grid;place-items:center;width:38px;height:38px;border-radius:50%;color:var(--farbe);background:color-mix(in srgb,var(--farbe) 18%,transparent)}.kachel .bild>svg{width:22px;height:22px;fill:currentColor}
.karte .kachel .name{grid-area:name;margin-top:10px;font-weight:700;color:var(--ink);white-space:nowrap;overflow:hidden;text-overflow:ellipsis;font-size:clamp(11px,(100cqi - var(--frei))/(var(--n)*.66),15px)}
.kachel .zeile{display:contents}.karte .kachel .stufe{grid-area:stufe;justify-self:end;font-weight:700;font-variant-numeric:tabular-nums}.kachel .worte,.kachel .schaltet{grid-area:worte;min-width:0;font-size:.82rem;color:var(--muted);white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.kachel .schaltet,.kachel:has([aria-busy=true]) :is(.worte,.stufe){display:none}.kachel:has([aria-busy=true]) .schaltet{display:block}
.karte .kachel:is(.breit,.allein){grid-column:1/-1}` + wide(".karte .kachel:is(.breit,.allein)") + `.karte .kachel.breit{--frei:172px}
@container kacheln (min-width:` + strconv.Itoa(2*zelle+8) + `px){.karte ul.kacheln{grid-template-columns:repeat(auto-fill,minmax(` + strconv.Itoa(zelle) + `px,1fr))}.karte .kachel:is(.breit,.allein){grid-column:auto}` + wide(".karte .kachel") + `}
.kachel .knoepfe{grid-area:knoepfe;position:relative;z-index:2;display:flex;gap:0;border-radius:24px;background:var(--pille)}.kachel .knoepfe>form>button,.kachel .knoepfe>button{width:40px;min-height:44px;padding:0;display:grid;place-items:center;border-radius:22px;background:none;color:var(--ink)}
.kachel :is(.hoch,.stopp,.runter)::before{content:"";width:15px;height:13px;background:currentColor}.kachel .stopp::before{width:11px;height:11px}.kachel .hoch::before{clip-path:polygon(50% 0,100% 100%,0 100%)}.kachel .runter::before{clip-path:polygon(0 0,100% 0,50% 100%)}
.kachel .flaeche,.kachel .flaeche form,.kachel .flaeche button{position:absolute;inset:0;z-index:1;width:100%;height:100%;min-height:0;margin:0;padding:0;border-radius:20px;background:none}
.kachel .flaeche button::before,.kachel .flaeche button::after{display:none}.kachel .flaeche button:focus-visible{outline:2px solid var(--accent);outline-offset:-2px}.karte .kachel button.stufe{z-index:2}.karte .kacheln .mehr{grid-column:1/-1;border:0;color:var(--muted)}</style>`
