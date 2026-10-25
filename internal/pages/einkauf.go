package pages

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mdopp/haus/internal/caldav"
	"github.com/mdopp/haus/internal/einkauf"
	"github.com/mdopp/haus/internal/notes"
)

// The household's shopping list (haus#118): EinkaufPath the list, a tap
// ticks a line off on every phone at once; EinkaufRezeptePath the
// ingredients of one or more recipes, for so many portions, to choose from
// before they go on it.
const (
	EinkaufPath        = "/einkauf"
	EinkaufRezeptePath = "/einkauf/rezepte"
	// EinkaufLive is what Live tells when the list changed: the page
	// carries it, so it is fetched again.
	EinkaufLive = "haus.einkauf"
)

const (
	saysEinkaufHow   = "Die Liste des Haushalts. Ein Tipp hakt ab, auf allen Handys zugleich. Auf dem Handy auch als Aufgabenliste „Einkauf“ (über DAVx⁵)."
	saysEinkaufEmpty = "Die Liste ist leer. Schreib oben, was fehlt, oder nimm die Zutaten eines Rezepts."
	saysEinkaufDown  = "Die Einkaufsliste lässt sich gerade nicht lesen: Radicale antwortet nicht."
	saysEinkaufWrite = "Das ließ sich gerade nicht speichern: Radicale antwortet nicht. Versuch es gleich noch einmal."
	saysEinkaufRace  = "Jemand hat die Liste gerade gleichzeitig geändert. Versuch es noch einmal."
	saysNoRecipes    = "Noch kein Rezept gemerkt. Ein Rezept merkst du dir unter Notizen mit „Aus Link merken“."
)

// einkaufWait is how long a change may take before the page says so.
var einkaufWait = 20 * time.Second

// Einkauf is the list and its POSTs: tun=dazu (was: lines, comma or line
// apart), haken (uid, erledigt), aufraeumen, rezepte (z: the lines chosen,
// each "text\torigin").
func Einkauf(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cfg.Einkauf == nil || (r.URL.Path != EinkaufPath && r.URL.Path != EinkaufRezeptePath) {
			missing{http.StatusNotFound, "Diese Seite gibt es nicht.", "Haushalt", "/kalender", "Haushalt"}.write(w, r, cfg)
			return
		}
		if r.URL.Path == EinkaufRezeptePath {
			if showOnly(w, r, "Die Rezeptauswahl") {
				writeRezepte(w, r, cfg)
			}
			return
		}
		if r.Method != http.MethodPost {
			if showOnly(w, r, "Einkauf") {
				writeEinkauf(w, r, cfg, "")
			}
			return
		}
		if !fromHaus(r) {
			http.Error(w, "Das geht nur von Haus' eigener Seite.", http.StatusForbidden)
			return
		}
		r.ParseForm()
		ctx, cancel := context.WithTimeout(r.Context(), einkaufWait)
		defer cancel()
		person := r.Header.Get("Remote-User")
		var said string
		var err error
		switch r.PostForm.Get("tun") {
		case "dazu":
			var zs []einkauf.Zeile
			for _, l := range splitLines(r.PostForm.Get("was")) {
				zs = append(zs, einkauf.Zeile{Posten: einkauf.Parse(l)})
			}
			if len(zs) == 0 {
				http.Error(w, "Schreib, was auf die Liste soll.", http.StatusBadRequest)
				return
			}
			var res einkauf.Ergebnis
			res, err = cfg.Einkauf.Dazu(ctx, zs)
			said = dazuSaid(res)
		case "rezepte":
			var zs []einkauf.Zeile
			for _, v := range r.PostForm["z"] {
				text, origin, _ := strings.Cut(v, "\t")
				if p := einkauf.Parse(text); p.Ding != "" {
					zs = append(zs, einkauf.Zeile{Posten: p, Herkunft: strings.Split(origin, ", ")})
				}
			}
			if len(zs) == 0 {
				http.Error(w, "Keine Zutat gewählt: Hake an, was auf die Liste soll.", http.StatusBadRequest)
				return
			}
			var res einkauf.Ergebnis
			res, err = cfg.Einkauf.Dazu(ctx, zs)
			said = dazuSaid(res)
		case "haken":
			err = cfg.Einkauf.Haken(ctx, r.PostForm.Get("uid"), r.PostForm.Get("erledigt") == "1")
		case "aufraeumen":
			var n int
			n, err = cfg.Einkauf.Aufraeumen(ctx)
			said = count(n, "abgehakte Zeile entfernt.", "abgehakte Zeilen entfernt.")
		default:
			http.Error(w, "Das geht hier nicht.", http.StatusBadRequest)
			return
		}
		if err != nil {
			// a sentence at the bottom, the page as it was (hausScript)
			code, say := http.StatusServiceUnavailable, saysEinkaufWrite
			switch {
			case errors.Is(err, einkauf.ErrNoLine):
				code, say = http.StatusConflict, err.Error()
			case errors.Is(err, caldav.ErrChanged):
				code, say = http.StatusConflict, saysEinkaufRace
			default:
				log.Printf("haus: einkauf: %s %s: %v", person, r.PostForm.Get("tun"), err)
			}
			http.Error(w, say, code)
			return
		}
		writeEinkauf(w, r, cfg, said)
	}
}

// splitLines is what someone wrote, a line per comma or line break.
func splitLines(s string) []string {
	var out []string
	for _, l := range strings.FieldsFunc(s, func(r rune) bool { return r == '\n' || r == ',' || r == ';' }) {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// dazuSaid is what adding did, in a sentence.
func dazuSaid(res einkauf.Ergebnis) string {
	var parts []string
	if res.Neu > 0 {
		parts = append(parts, count(res.Neu, "Zeile neu auf der Liste", "Zeilen neu auf der Liste"))
	}
	if res.Addiert > 0 {
		parts = append(parts, count(res.Addiert, "zu einer Zeile dazugezählt", "zu Zeilen dazugezählt"))
	}
	if len(parts) == 0 {
		return "Steht schon auf der Liste."
	}
	return strings.Join(parts, ", ") + "."
}

func count(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

const einkaufStyle = `<style>.einkauf{list-style:none;margin:16px 0 0;padding:0;display:grid;gap:8px}
.einkauf form{margin:0}.einkauf button.posten{width:100%;display:flex;align-items:center;gap:14px;min-height:56px;padding:10px 16px;text-align:left;font:inherit;font-size:1.1rem;color:var(--ink);background:var(--card);border:1px solid var(--edge);border-radius:14px;cursor:pointer}
.einkauf .haken{flex:none;width:26px;height:26px;border-radius:8px;border:2px solid var(--edge);display:grid;place-items:center}
.einkauf button[aria-checked=true] .haken{background:var(--ok);border-color:var(--ok)}.einkauf button[aria-checked=true] .haken::after{content:"";width:7px;height:13px;border:solid var(--card);border-width:0 3px 3px 0;transform:translateY(-2px) rotate(45deg)}
.einkauf button[aria-checked=true] .was{text-decoration:line-through;color:var(--muted)}.einkauf .was{overflow-wrap:anywhere}.einkauf small{display:block;font-size:.8rem;color:var(--muted)}
.einkauf-dazu{display:flex;gap:8px;margin:16px 0 0}.einkauf-dazu input{flex:1;min-width:0;min-height:48px;font:inherit;color:var(--ink);background:var(--card);border:1px solid var(--edge);border-radius:12px;padding:0 14px}
.einkauf-dazu button,.einkauf-weg button,.rezeptwahl button{min-height:48px;padding:0 18px;border-radius:999px;border:0;background:var(--accent);color:var(--card);font:inherit;font-weight:600;cursor:pointer}
.einkauf-weg{margin:16px 0 0}.einkauf-weg button{background:var(--accent-soft);color:var(--accent)}.einkauf-zwischen{margin:22px 2px 0;font-size:.85rem;color:var(--muted)}
.rezeptwahl{margin:12px 0 0}.rezeptwahl label{display:flex;align-items:center;gap:12px;min-height:48px;padding:6px 0;border-bottom:1px solid var(--line)}.rezeptwahl label small{color:var(--muted)}
.rezeptwahl input[type=checkbox]{width:22px;height:22px;flex:none}.rezeptwahl .portionen{display:flex;align-items:center;gap:10px;margin:14px 0}.rezeptwahl .portionen input{width:8rem;min-height:44px;font:inherit;padding:0 10px;border:1px solid var(--edge);border-radius:12px;background:var(--card);color:var(--ink)}
.rezeptwahl button{margin:12px 0 0}</style>`

// writeEinkauf is the list page: what was just done, the field to add,
// the open lines, the ticked ones, and the way to the recipes.
func writeEinkauf(w http.ResponseWriter, r *http.Request, cfg StartConfig, said string) {
	var items []caldav.Item
	var err error
	fetch(r, func(ctx context.Context) { items, err = cfg.Einkauf.Lesen(ctx) })
	b := familyHead(r, cfg, "Einkauf", "Einkauf")
	b.WriteString(einkaufStyle + liveRefresh("seite", 60) + `<span hidden>` + EinkaufLive + `</span><h1>Einkauf</h1><p class=k>` + html.EscapeString(saysEinkaufHow) + `</p>`)
	if said != "" {
		b.WriteString("<div class=gut role=status><span class=punkt></span><p>" + html.EscapeString(said) + "</p></div>")
	}
	b.WriteString(`<form class=einkauf-dazu method=post action=` + EinkaufPath + `><input type=hidden name=tun value=dazu>` +
		`<input name=was aria-label="Was fehlt?" placeholder="Was fehlt? z. B. Milch, 500 g Mehl" autocomplete=off enterkeyhint=done required>` +
		`<button data-laeuft="Schreibt …">Dazu</button></form>`)
	if err != nil {
		log.Printf("haus: einkauf: %v", err)
		b.WriteString("<div class=achtung role=alert><p>" + html.EscapeString(saysEinkaufDown) + "</p><p>Die Seite versucht es von selbst wieder.</p></div>")
		writePage(w, http.StatusOK, b)
		return
	}
	done := 0
	b.WriteString("<ul class=einkauf>")
	for i, it := range items {
		if it.Done {
			if done == 0 && i > 0 {
				b.WriteString("</ul><p class=einkauf-zwischen>Abgehakt</p><ul class=einkauf>")
			}
			done++
		}
		next := "1"
		if it.Done {
			next = "0"
		}
		b.WriteString(`<li><form method=post action=` + EinkaufPath + `><input type=hidden name=tun value=haken><input type=hidden name=uid value="` + html.EscapeString(it.UID) +
			`"><input type=hidden name=erledigt value=` + next + `><button class=posten role=switch aria-checked=` + strconv.FormatBool(it.Done) + `><span class=haken></span><span class=was>` + html.EscapeString(it.Summary))
		if it.Herkunft != "" {
			b.WriteString("<small>" + html.EscapeString(it.Herkunft) + "</small>")
		}
		b.WriteString("</span></button></form></li>")
	}
	b.WriteString("</ul>")
	if len(items) == 0 {
		b.WriteString("<p class=k>" + html.EscapeString(saysEinkaufEmpty) + "</p>")
	}
	if done > 0 {
		b.WriteString(`<form class=einkauf-weg method=post action=` + EinkaufPath + ` data-frage="` + html.EscapeString(count(done, "abgehakte Zeile", "abgehakte Zeilen")) + ` von der Liste nehmen?">` +
			`<input type=hidden name=tun value=aufraeumen><button data-laeuft="Räumt auf …">Abgehakte entfernen</button></form>`)
	}
	if cfg.Notes != nil {
		b.WriteString(`<p class=wege><a class=knopf href=` + EinkaufRezeptePath + `>Zutaten aus Rezepten</a></p>`)
	}
	writePage(w, http.StatusOK, b)
}

// writeRezepte is choosing recipes and portions (n: the notes chosen,
// portionen), and with some chosen their ingredients, added up and
// scaled, each to tick off as "habe ich" before they go on the list.
func writeRezepte(w http.ResponseWriter, r *http.Request, cfg StartConfig) {
	q := r.URL.Query()
	chosen := map[string]bool{}
	for _, n := range q["n"] {
		chosen[n] = true
	}
	picked := map[string]bool{}
	for n := range chosen {
		picked[n] = true
	}
	portionen, _ := strconv.Atoi(q.Get("portionen"))
	if portionen < 0 || portionen > 99 {
		portionen = 0
	}
	b := familyHead(r, cfg, "Zutaten aus Rezepten", "Einkauf")
	b.WriteString(noteStyle + einkaufStyle + `<h1>Zutaten aus Rezepten</h1>`)
	if cfg.Notes == nil {
		b.WriteString("<p class=k>" + html.EscapeString(saysNoRecipes) + "</p>")
		writePage(w, http.StatusOK, b)
		return
	}
	var all []notes.Entry
	var rs []einkauf.Rezept
	var err error
	fetch(r, func(ctx context.Context) {
		if all, err = einkauf.Rezepte(ctx, cfg.Notes); err != nil {
			return
		}
		listed := map[string]bool{}
		for _, e := range all {
			listed[e.Name] = true
		}
		for _, name := range q["n"] { // in the order chosen: the first recipe's portions are the default
			if listed[name] && chosen[name] {
				delete(chosen, name)
				if n, rerr := cfg.Notes.Read(ctx, name); rerr == nil {
					rs = append(rs, einkauf.AusNotiz(n))
				}
			}
		}
	})
	if err != nil {
		log.Printf("haus: einkauf: recipes: %v", err)
		b.WriteString("<div class=achtung role=alert><p>" + html.EscapeString(saysNoNotes) + "</p></div>")
		writePage(w, http.StatusOK, b)
		return
	}
	if len(all) == 0 {
		b.WriteString("<p class=k>" + html.EscapeString(saysNoRecipes) + "</p><p class=wege><a href=" + EinkaufPath + ">Zur Einkaufsliste</a></p>")
		writePage(w, http.StatusOK, b)
		return
	}
	if portionen == 0 && len(rs) > 0 {
		portionen = rs[0].Portionen
	}
	// the choice: recipes and portions, a GET to this page
	b.WriteString(`<form class=rezeptwahl method=get action=` + EinkaufRezeptePath + `><fieldset style="border:0;padding:0;margin:0"><legend class=k>Welche Rezepte?</legend>`)
	for _, e := range all {
		check := ""
		if picked[e.Name] {
			check = " checked"
		}
		b.WriteString(`<label><input type=checkbox name=n value="` + html.EscapeString(e.Name) + `"` + check + `><span>` + html.EscapeString(e.Title) + `</span></label>`)
	}
	pv := ""
	if portionen > 0 {
		pv = strconv.Itoa(portionen)
	}
	b.WriteString(`</fieldset><p class=portionen><label for=portionen style="border:0;min-height:0;padding:0">Für</label><input id=portionen type=number name=portionen min=1 max=99 inputmode=numeric value="` + pv + `" placeholder="wie im Rezept"><span>Portionen</span></p>` +
		`<button data-laeuft="Rechnet …">Zutaten zeigen</button></form>`)
	if len(rs) > 0 {
		lines := einkauf.Fuer(rs, portionen)
		var missingCount []string
		for _, rz := range rs {
			if portionen > 0 && rz.Portionen == 0 {
				missingCount = append(missingCount, rz.Titel)
			}
		}
		b.WriteString(`<section class=karte aria-labelledby=k-zutaten><h2 id=k-zutaten>Zutaten</h2>`)
		if len(missingCount) > 0 {
			b.WriteString("<p class=k>" + html.EscapeString(fmt.Sprintf("%s sagt nicht, für wie viele es ist: Diese Zutaten stehen da wie im Rezept.", strings.Join(missingCount, ", "))) + "</p>")
		}
		if len(lines) == 0 {
			b.WriteString("<p class=k>In diesen Rezepten steht keine Zutatenliste („## Zutaten“ mit „- …“-Zeilen).</p></section>")
		} else {
			b.WriteString(`<p class=k>Nimm weg, was du schon hast.</p><form class=rezeptwahl method=post action=` + EinkaufPath + `><input type=hidden name=tun value=rezepte>`)
			for _, z := range lines {
				origin := strings.Join(z.Herkunft, ", ")
				b.WriteString(`<label><input type=checkbox name=z checked value="` + html.EscapeString(z.Text()+"\t"+origin) + `"><span>` + html.EscapeString(z.Text()))
				if len(rs) > 1 {
					b.WriteString(" <small>" + html.EscapeString(origin) + "</small>")
				}
				b.WriteString("</span></label>")
			}
			b.WriteString(`<button data-laeuft="Schreibt …">Auf die Einkaufsliste</button></form></section>`)
		}
	}
	b.WriteString("<p class=wege><a href=" + EinkaufPath + ">Zur Einkaufsliste</a></p>")
	writePage(w, http.StatusOK, b)
}

// RezeptAufDieListe is a recipe note's way to the list.
func RezeptAufDieListe(name string) string {
	return EinkaufRezeptePath + "?n=" + url.QueryEscape(name)
}
