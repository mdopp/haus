package pages

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

// The household's shopping list (haus#118) and the other lists (haus#119):
// EinkaufPath a list (?liste=<id>, the household's Einkauf when none), a
// tap ticks a line off on every phone at once; EinkaufRezeptePath the
// ingredients of one or more recipes, for so many portions, to choose from
// before they go on a list.
const (
	EinkaufPath        = "/einkauf"
	EinkaufRezeptePath = "/einkauf/rezepte"
)

// ListeLive is what Live tells when a list changed: the page carries it,
// so it is fetched again. A token, not the list's name: every listener
// hears it, a person's own list is theirs alone.
func ListeLive(r caldav.ListRef) string {
	sum := sha256.Sum256([]byte("haus-liste:" + r.ID()))
	return "haus.liste." + hex.EncodeToString(sum[:6])
}

const (
	saysEinkaufHow   = "Die Liste des Haushalts. Ein Tipp hakt ab, auf allen Handys zugleich. Auf dem Handy auch als Aufgabenliste „Einkauf“ (über DAVx⁵)."
	saysListeHow     = "Eine Liste für den ganzen Haushalt. Ein Tipp hakt ab, auf allen Handys zugleich; auf dem Handy auch als Aufgabenliste."
	saysListeMine    = "Nur für dich: die anderen sehen sie nicht. Auf deinem Handy als Aufgabenliste in deinem eigenen Konto."
	saysEinkaufEmpty = "Die Liste ist leer. Schreib oben, was fehlt, oder nimm die Zutaten eines Rezepts."
	saysEinkaufDown  = "Die Liste lässt sich gerade nicht lesen: Radicale antwortet nicht."
	saysEinkaufWrite = "Das ließ sich gerade nicht speichern: Radicale antwortet nicht. Versuch es gleich noch einmal."
	saysEinkaufRace  = "Jemand hat die Liste gerade gleichzeitig geändert. Versuch es noch einmal."
	saysNoRecipes    = "Noch kein Rezept gemerkt. Ein Rezept merkst du dir unter Notizen mit „Aus Link merken“."
	saysNoList       = "Diese Liste gibt es nicht."
	saysListMade     = "Die Liste ist angelegt."
	saysListGone     = "Die Liste ist gelöscht."
)

// einkaufWait is how long a change may take before the page says so.
var einkaufWait = 20 * time.Second

// listOf is the list the request names (liste=<id>), the household's
// shopping list without one; another person's own list is no list here.
func listOf(r *http.Request, cfg StartConfig) (*einkauf.Liste, bool) {
	id := r.FormValue("liste")
	if id == "" {
		return cfg.Einkauf.Andere(caldav.Einkauf), true
	}
	ref, ok := caldav.ParseListRef(id)
	if !ok || (ref.Personal() && ref.Person != r.Header.Get("Remote-User")) {
		return nil, false
	}
	return cfg.Einkauf.Andere(ref), true
}

// listPath is a list's page.
func listPath(ref caldav.ListRef) string {
	if ref == caldav.Einkauf {
		return EinkaufPath
	}
	return EinkaufPath + "?liste=" + url.QueryEscape(ref.ID())
}

// Einkauf is a list and its POSTs: tun=dazu (was: lines, comma or line
// apart), haken (uid, erledigt), aufraeumen, rezepte (z: the lines chosen,
// each "text\torigin"), neu (name, fuer=haushalt|mich), loeschen.
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
			if !showOnly(w, r, "Einkauf") {
				return
			}
			l, ok := listOf(r, cfg)
			if !ok {
				missing{http.StatusNotFound, saysNoList, "Einkauf", EinkaufPath, "Zur Einkaufsliste"}.write(w, r, cfg)
				return
			}
			said := ""
			switch {
			case r.URL.Query().Get("neu") == "1":
				said = saysListMade
			case r.URL.Query().Get("geloescht") == "1":
				said = saysListGone
			}
			writeEinkauf(w, r, cfg, l, said)
			return
		}
		if !fromHaus(r) {
			http.Error(w, "Das geht nur von Haus' eigener Seite.", http.StatusForbidden)
			return
		}
		r.ParseForm()
		l, ok := listOf(r, cfg)
		if !ok {
			http.Error(w, saysNoList, http.StatusNotFound)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), einkaufWait)
		defer cancel()
		person := r.Header.Get("Remote-User")
		var said string
		var err error
		switch r.PostForm.Get("tun") {
		case "dazu":
			var zs []einkauf.Zeile
			for _, line := range splitLines(r.PostForm.Get("was")) {
				zs = append(zs, einkauf.Zeile{Posten: einkauf.Parse(line)})
			}
			if len(zs) == 0 {
				http.Error(w, "Schreib, was auf die Liste soll.", http.StatusBadRequest)
				return
			}
			var res einkauf.Ergebnis
			res, err = l.Dazu(ctx, zs)
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
			res, err = l.Dazu(ctx, zs)
			said = dazuSaid(res)
		case "haken":
			err = l.Haken(ctx, r.PostForm.Get("uid"), r.PostForm.Get("erledigt") == "1")
		case "aufraeumen":
			var n int
			n, err = l.Aufraeumen(ctx)
			said = count(n, "abgehakte Zeile entfernt.", "abgehakte Zeilen entfernt.")
		case "neu":
			name := strings.Join(strings.Fields(r.PostForm.Get("name")), " ")
			if name == "" || len([]rune(name)) > 60 {
				http.Error(w, "Wie soll die Liste heißen? Ein Name mit höchstens 60 Zeichen.", http.StatusBadRequest)
				return
			}
			who := ""
			if r.PostForm.Get("fuer") == "mich" {
				who = person
			}
			var ref caldav.ListRef
			ref, err = l.DAV.MakeList(ctx, who, notes.Slug(name), name)
			if errors.Is(err, caldav.ErrListExists) {
				http.Error(w, "Eine Liste mit diesem Namen gibt es schon.", http.StatusConflict)
				return
			}
			if err == nil {
				log.Printf("haus: einkauf: %s made the list %s", person, ref.ID())
				http.Redirect(w, r, listPath(ref)+sep(listPath(ref))+"neu=1", http.StatusSeeOther)
				return
			}
		case "loeschen":
			if l.Ref == caldav.Einkauf {
				http.Error(w, "Die Einkaufsliste des Haushalts bleibt.", http.StatusBadRequest)
				return
			}
			if err = l.DAV.DeleteList(ctx, l.Ref); err == nil {
				log.Printf("haus: einkauf: %s deleted the list %s", person, l.Ref.ID())
				http.Redirect(w, r, EinkaufPath+"?geloescht=1", http.StatusSeeOther)
				return
			}
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
				log.Printf("haus: einkauf: %s %s %s: %v", person, l.Ref.ID(), r.PostForm.Get("tun"), err)
			}
			http.Error(w, say, code)
			return
		}
		writeEinkauf(w, r, cfg, l, said)
	}
}

func sep(u string) string {
	if strings.Contains(u, "?") {
		return "&"
	}
	return "?"
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
.rezeptwahl button{margin:12px 0 0}.rezeptwahl select{min-height:44px;min-width:10rem;font:inherit;padding:0 10px;border:1px solid var(--edge);border-radius:12px;background:var(--card);color:var(--ink)}
.listen{display:flex;gap:8px;margin:22px 0 -8px;overflow-x:auto}.listen a{display:inline-flex;align-items:center;gap:6px;min-height:44px;padding:0 16px;border-radius:999px;background:var(--card);border:1px solid var(--edge);color:var(--ink);text-decoration:none;font-size:.9rem;white-space:nowrap}
.listen a[aria-current]{color:var(--accent);border-color:var(--accent);font-weight:600}.einkauf .kaufen{margin:6px 4px 0;font-size:.85rem;color:var(--muted)}.einkauf .kaufen a{display:inline-block;padding:10px 2px}.listen .meine{font-size:.7rem;color:var(--muted);font-weight:400}</style>`

// listName is the list's name among lists; the household's shopping list
// is "Einkauf" before it is made.
func listName(lists []caldav.List, ref caldav.ListRef) (string, bool) {
	for _, x := range lists {
		if x.Ref == ref {
			return x.Name, true
		}
	}
	return einkauf.Name, ref == caldav.Einkauf
}

// listPills are the lists to switch between, the current one marked.
func listPills(lists []caldav.List, cur caldav.ListRef) string {
	if len(lists) < 2 {
		return ""
	}
	var b strings.Builder
	b.WriteString(`<nav class=listen aria-label=Listen>`)
	for _, x := range lists {
		name := html.EscapeString(x.Name)
		if x.Ref.Personal() {
			name += ` <span class=meine>nur für dich</span>`
		}
		b.WriteString(`<a href="` + html.EscapeString(listPath(x.Ref)) + `"`)
		if x.Ref == cur {
			b.WriteString(" aria-current=page")
		}
		b.WriteString(">" + name + "</a>")
	}
	b.WriteString("</nav>")
	return b.String()
}

// writeEinkauf is a list's page: the lists to switch between, what was
// just done, the field to add, the open lines, the ticked ones, the way to
// the recipes, and the lists to make and delete.
func writeEinkauf(w http.ResponseWriter, r *http.Request, cfg StartConfig, l *einkauf.Liste, said string) {
	var items []caldav.Item
	var lists []caldav.List
	var err, lerr error
	person := r.Header.Get("Remote-User")
	fetch(r, func(ctx context.Context) { items, err = l.Lesen(ctx) }, func(ctx context.Context) { lists, lerr = l.DAV.Lists(ctx, person) })
	if lerr != nil {
		log.Printf("haus: einkauf: lists: %v", lerr)
	}
	name, known := listName(lists, l.Ref)
	if lerr == nil && !known {
		missing{http.StatusNotFound, saysNoList, "Einkauf", EinkaufPath, "Zur Einkaufsliste"}.write(w, r, cfg)
		return
	}
	act := html.EscapeString(listPath(l.Ref))
	b := familyHead(r, cfg, name, "Einkauf")
	how := saysEinkaufHow
	switch {
	case l.Ref.Personal():
		how = saysListeMine
	case l.Ref != caldav.Einkauf:
		how = saysListeHow
	}
	b.WriteString(noteStyle + einkaufStyle + liveRefresh("seite", 60) + `<span hidden>` + ListeLive(l.Ref) + `</span>` + listPills(lists, l.Ref) +
		`<h1>` + html.EscapeString(name) + `</h1><p class=k>` + html.EscapeString(how) + `</p>`)
	if said != "" {
		b.WriteString("<div class=gut role=status><span class=punkt></span><p>" + html.EscapeString(said) + "</p></div>")
	}
	b.WriteString(`<form class=einkauf-dazu method=post action="` + act + `"><input type=hidden name=tun value=dazu>` +
		`<input name=was aria-label="Was fehlt?" placeholder="Was fehlt? z. B. Milch, 500 g Mehl" autocomplete=off enterkeyhint=done required>` +
		`<button data-laeuft="Schreibt …">Dazu</button></form>`)
	if err != nil {
		log.Printf("haus: einkauf: %s: %v", l.Ref.ID(), err)
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
		b.WriteString(`<li><form method=post action="` + act + `"><input type=hidden name=tun value=haken><input type=hidden name=uid value="` + html.EscapeString(it.UID) +
			`"><input type=hidden name=erledigt value=` + next + `><button class=posten role=switch aria-checked=` + strconv.FormatBool(it.Done) + `><span class=haken></span><span class=was>` + html.EscapeString(it.Summary))
		music := l.Ref == einkauf.MusikListe
		if it.Herkunft != "" && !music {
			b.WriteString("<small>" + html.EscapeString(it.Herkunft) + "</small>")
		}
		if music {
			if who := einkauf.Wer(it.Herkunft); who != "" {
				b.WriteString("<small>gewünscht von " + html.EscapeString(who) + "</small>")
			}
		}
		b.WriteString("</span></button></form>")
		if music && !it.Done { // where to buy it as a file, and the link it came from
			b.WriteString("<p class=kaufen>Kaufen: ")
			for i, s := range einkauf.Kaufen(it.Summary) {
				if i > 0 {
					b.WriteString(" · ")
				}
				b.WriteString(`<a href="` + html.EscapeString(s[1]) + `" rel="noopener noreferrer" target=_blank>` + s[0] + `</a>`)
			}
			for _, p := range strings.Split(it.Herkunft, ", ") {
				if strings.HasPrefix(p, "https://") {
					b.WriteString(` · <a href="` + html.EscapeString(p) + `" rel="noopener noreferrer" target=_blank>Anhören</a>`)
					break
				}
			}
			b.WriteString("</p>")
		}
		b.WriteString("</li>")
	}
	b.WriteString("</ul>")
	if len(items) == 0 {
		b.WriteString("<p class=k>" + html.EscapeString(saysEinkaufEmpty) + "</p>")
	}
	if done > 0 {
		b.WriteString(`<form class=einkauf-weg method=post action="` + act + `" data-frage="` + html.EscapeString(count(done, "abgehakte Zeile", "abgehakte Zeilen")) + ` von der Liste nehmen?">` +
			`<input type=hidden name=tun value=aufraeumen><button data-laeuft="Räumt auf …">Abgehakte entfernen</button></form>`)
	}
	if cfg.Notes != nil {
		rez := EinkaufRezeptePath
		if l.Ref != caldav.Einkauf {
			rez += "?liste=" + url.QueryEscape(l.Ref.ID())
		}
		b.WriteString(`<p class=wege><a class=knopf href="` + html.EscapeString(rez) + `">Zutaten aus Rezepten</a></p>`)
	}
	// make a list, delete this one (not the household's shopping list)
	b.WriteString(`<details class=neu><summary>Listen verwalten</summary><form class=notizform method=post action=` + EinkaufPath + `><input type=hidden name=tun value=neu>` +
		`<label for=liste-name>Neue Liste</label><input type=text id=liste-name name=name maxlength=60 required placeholder="z. B. Baumarkt, Urlaub packen" autocomplete=off>` +
		`<fieldset class=tagwahl><legend>Für wen?</legend><label class=tag><input type=radio name=fuer value=haushalt checked> Für den Haushalt</label>` +
		`<label class=tag><input type=radio name=fuer value=mich> Nur für mich</label></fieldset><button data-laeuft="Legt an …">Anlegen</button></form>`)
	if l.Ref != caldav.Einkauf {
		b.WriteString(`<form class=einkauf-weg method=post action="` + act + `" data-frage="Liste „` + html.EscapeString(name) + `“ mit allen Zeilen löschen?">` +
			`<input type=hidden name=tun value=loeschen><button data-laeuft="Löscht …">Diese Liste löschen</button></form>`)
	}
	b.WriteString("</details>")
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
	var lists []caldav.List
	var err error
	target := caldav.Einkauf.ID()
	if ref, ok := caldav.ParseListRef(q.Get("liste")); ok && (!ref.Personal() || ref.Person == r.Header.Get("Remote-User")) {
		target = ref.ID()
	}
	fetch(r, func(ctx context.Context) {
		var lerr error
		if lists, lerr = cfg.Einkauf.DAV.Lists(ctx, r.Header.Get("Remote-User")); lerr != nil {
			log.Printf("haus: einkauf: lists: %v", lerr)
		}
	}, func(ctx context.Context) {
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
	if target != caldav.Einkauf.ID() {
		b.WriteString(`<input type=hidden name=liste value="` + html.EscapeString(target) + `">`)
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
			if len(lists) > 1 {
				b.WriteString(`<p class=portionen><label for=ziel style="border:0;min-height:0;padding:0">Auf</label><select id=ziel name=liste>`)
				for _, x := range lists {
					sel := ""
					if x.Ref.ID() == target {
						sel = " selected"
					}
					b.WriteString(`<option value="` + html.EscapeString(x.Ref.ID()) + `"` + sel + `>` + html.EscapeString(x.Name) + `</option>`)
				}
				b.WriteString(`</select></p>`)
			} else if target != caldav.Einkauf.ID() {
				b.WriteString(`<input type=hidden name=liste value="` + html.EscapeString(target) + `">`)
			}
			for _, z := range lines {
				origin := strings.Join(z.Herkunft, ", ")
				b.WriteString(`<label><input type=checkbox name=z checked value="` + html.EscapeString(z.Text()+"\t"+origin) + `"><span>` + html.EscapeString(z.Text()))
				if len(rs) > 1 {
					b.WriteString(" <small>" + html.EscapeString(origin) + "</small>")
				}
				b.WriteString("</span></label>")
			}
			b.WriteString(`<button data-laeuft="Schreibt …">Auf die Liste</button></form></section>`)
		}
	}
	b.WriteString("<p class=wege><a href=" + EinkaufPath + ">Zur Einkaufsliste</a></p>")
	writePage(w, http.StatusOK, b)
}

// RezeptAufDieListe is a recipe note's way to the list.
func RezeptAufDieListe(name string) string {
	return EinkaufRezeptePath + "?n=" + url.QueryEscape(name)
}
