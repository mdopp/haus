package pages

import (
	"fmt"
	"html"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/mdopp/homes/internal/cards"
	"github.com/mdopp/homes/internal/texte"
)

// „So hängt alles zusammen“ (owner 2026-11-05): for the family, what links
// their data and what that opens up (docs/DOMAIN.md). A web drawn in SVG
// with the Haushalts-Kontakt at its centre, and a few „Das geht“ moments,
// each a tap to the real place. Only what this box has: a node or a moment
// shows while its modules are on. The words are internal/texte's.
const ZusammenPath = "/zusammen"

// z is one of the page's words.
func z(key string) string { return texte.T(texte.ID("zusammen." + key)) }

// knoten is one node of the web: its words, where it sits, and the module
// it needs ("" always). The inner ones hang on the contact; the outer ones
// sit on the orbit around it. Fotos hangs on it too (owner 2026-11-05: a
// contact's „Fotos“, its face from Immich), at the orbit's foot.
var knoten = []struct {
	key, modul string
	x, y       int
	aussen     bool
}{
	{"termine", "kalender", 180, 80, false}, {"bank", "dokumente", 276, 140, false}, {"telefonbuch", "konto", 236, 245, false},
	{"nachrichten", "sicherung", 124, 245, false}, {"dokumente", "dokumente", 88, 140, false},
	{"zeitleiste", "standort", 302, 58, true}, {"musik", "musik", 302, 292, true}, {"fotos", "fotos", 180, 316, false}, {"geraete", "geraete", 58, 58, true},
}

// momente are the „Das geht“ moments: the modules each needs, and where its tap leads.
var momente = []struct {
	key, href string
	braucht   []string
}{{"rechnung", KontaktePath, []string{"dokumente", "kontakte", "konto"}}, {"tipp", KontaktePath, []string{"dokumente", "kontakte"}},
	{"gesichter", KontaktePath, []string{"fotos", "kontakte"}},
	{"termin", "/kalender", []string{"kalender", "konto"}}, {"zeitachse", StandortPath, []string{"standort"}}, {"waschmaschine", "/raeume", []string{"geraete", "sprechen"}}}

const zusammenStyle = `<style>.netz{display:block;width:100%;height:auto;margin:8px 0 4px;font-family:inherit}.netz .bahn{fill:none;stroke:var(--edge);stroke-dasharray:3 5}
.netz line{stroke:var(--accent);stroke-opacity:.45;stroke-width:1.5}.netz rect{fill:var(--card);stroke:var(--edge)}.netz .aussen rect{fill:var(--bg);stroke-dasharray:3 3}
.netz text{fill:var(--ink);font-size:11.5px;font-weight:600;text-anchor:middle}.netz .unter{fill:var(--muted);font-size:9.5px;font-weight:400}
.netz .mitte{fill:var(--accent)}.netz .mitte+text,.netz .mitte+text+text{fill:#fff}.netz .puls{fill:none;stroke:var(--accent);transform-box:fill-box;transform-origin:center;animation:puls 3.2s ease-out 3}
@keyframes puls{from{transform:scale(1);opacity:.7}to{transform:scale(1.5);opacity:0}}@media (prefers-reduced-motion:reduce){.netz .puls{animation:none;opacity:0}}</style>`

// Zusammen is ZusammenPath, for everyone.
func Zusammen(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != ZusammenPath {
			Elsewhere(cfg)(w, r)
			return
		}
		if !showOnly(w, r, z("titel")) {
			return
		}
		b := familyHead(r, cfg, z("titel"), z("titel"))
		b.WriteString(zusammenStyle + "<h1>" + html.EscapeString(z("titel")) + "</h1><p class=k>" + html.EscapeString(z("kurz")) + "</p>" + netz(cfg))
		c := cards.Card{Title: z("geht")}
		for _, m := range momente {
			if alleAn(cfg, m.braucht) && (m.key != "gesichter" || cfg.fotosAn()) { // a contact's „Fotos“ needs Immich's address
				c.AddLink(z(m.key), z(m.key+".so"), m.href)
			}
		}
		if f, ok := cfg.Module.Eins("fotos"); ok && f.An() && f.Einrichtung != nil && f.Einrichtung.Web != "" { // Immich, at the address its setup knows
			c.AddLink(z("fotos"), z("fotos.so"), f.Einrichtung.Web)
		}
		if len(spurModule(cfg)) > 0 {
			c.AddLink(z("fehlt"), z("fehlt.so"), mehrPath+"#spuren")
		}
		b.WriteString(c.HTML())
		writePage(w, http.StatusOK, b)
	}
}

// alleAn: are all these modules on? Without a module list, all are.
func alleAn(cfg StartConfig, names []string) bool {
	for _, n := range names {
		if cfg.Module != nil && !cfg.Module.An(n) {
			return false
		}
	}
	return true
}

// netz is the web: the orbit, the lines from the contact to its inner
// nodes, the nodes, and the contact itself on top.
func netz(cfg StartConfig) string {
	var lines, nodes strings.Builder
	for _, k := range knoten {
		if !alleAn(cfg, []string{k.modul}) {
			continue
		}
		label, unter := z("knoten."+k.key), z("knoten."+k.key+".unter")
		wd := max(utf8.RuneCountInString(label)*7, utf8.RuneCountInString(unter)*6) + 18
		if !k.aussen {
			fmt.Fprintf(&lines, `<line x1=180 y1=175 x2=%d y2=%d />`, k.x, k.y)
		}
		fmt.Fprintf(&nodes, `<g%s><rect x=%d y=%d width=%d height=36 rx=12 /><text x=%d y=%d>%s</text><text class=unter x=%d y=%d>%s</text></g>`,
			map[bool]string{true: " class=aussen"}[k.aussen], k.x-wd/2, k.y-18, wd, k.x, k.y-3, html.EscapeString(label), k.x, k.y+11, html.EscapeString(unter))
	}
	return `<svg class=netz viewBox="0 0 360 350" role=img aria-label="` + html.EscapeString(z("bild")) + `"><ellipse class=bahn cx=180 cy=175 rx=165 ry=150 />` +
		lines.String() + nodes.String() + `<circle class=puls cx=180 cy=175 r=38 /><circle class=mitte cx=180 cy=175 r=38 /><text x=180 y=172>` +
		html.EscapeString(z("mitte")) + `</text><text x=180 y=186>` + html.EscapeString(z("mitte.unter")) + `</text></svg>`
}
