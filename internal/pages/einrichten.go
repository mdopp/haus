package pages

import (
	"context"
	"fmt"
	"html"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/mdopp/homes/internal/assistent"
	"github.com/mdopp/homes/internal/einrichten"
	"github.com/mdopp/homes/internal/texte"
)

// EinrichtenPages is each module's setup (owner 2026-11-04: „die
// Einrichtung gehört zur Funktion“): /einrichten/<name> shows its block
// for a module without a page of its own, POST ticks it by hand.
const EinrichtenPages = "/einrichten/"

// mitSeite are the modules whose own page carries their setup block, and
// where the task leads: the block leads that page while it is open.
var mitSeite = map[string]string{"podcasts": PodcastsPath, "sprechen": "/sprechen"}

// einrichtenHref is where a module's task is done.
func einrichtenHref(name string) string {
	if p, ok := mitSeite[name]; ok {
		return p + "#einrichten-" + name
	}
	return einrichten.Seite(name)
}

// einrichtenHier is a module's setup on its own page: the whole block on
// top while the person's task is open, one quiet link at the bottom once
// it is done (to set up another device); none for a module without a task.
func einrichtenHier(r *http.Request, cfg StartConfig, name string) (oben, unten string) {
	a, ok := cfg.Einrichtung.Eine(r.Header.Get("Remote-User"), name)
	switch {
	case !ok:
		return "", ""
	case a.Erledigt == nil:
		return einrichtenBlock(r, a), ""
	}
	return "", `<p class=leise-weg id=einrichten-` + name + `><a href=` + einrichten.Seite(name) + `>` + html.EscapeString(texte.T(texte.Weiteres)) + `</a></p>` +
		`<style>.leise-weg{margin:40px 0 0;text-align:center;font-size:.9rem}.leise-weg a{color:var(--muted)}</style>`
}

// einrichtenBlock is the task's steps, its addresses, and while it is open
// the person's tick.
func einrichtenBlock(r *http.Request, a einrichten.Aufgabe) string {
	name := a.Modul
	e := a.Einrichtung
	var b strings.Builder
	b.WriteString(`<section class="karte einrichten" id=einrichten-` + name + `><h2>` + html.EscapeString(a.Titel) + `</h2><ol>`)
	b.WriteString(`<li>` + html.EscapeString(fmt.Sprintf(texte.T(texte.AppLaden), e.App)) + ` <a class=knopf href="` + html.EscapeString(e.Store) + `">` + html.EscapeString(texte.T(texte.Laden)) + `</a></li>`)
	addr := `<span class=adresse>` + html.EscapeString(e.Web) + `</span>`
	for _, s := range e.Schritte {
		b.WriteString(`<li>` + strings.NewReplacer("{adresse}", addr, "{name}", html.EscapeString(assistent.Name())).Replace(html.EscapeString(texte.T(texte.ID(s)))) + `</li>`)
	}
	b.WriteString(`</ol><p class=wege>`)
	if e.Web != "" && name != einrichten.Homes {
		b.WriteString(`<a class=knopf href="` + html.EscapeString(e.Web) + `">` + html.EscapeString(texte.T(texte.Oeffnen)) + `</a> `)
	}
	b.WriteString(`<a href="` + html.EscapeString(e.Anleitung) + `">` + html.EscapeString(texte.T(texte.Anleitung)) + `</a></p>`)
	if a.Erledigt == nil {
		b.WriteString(`<form method=post action=` + einrichten.Seite(name) + `><input type=hidden name=zurueck value="` + html.EscapeString(r.URL.Path) + `">` +
			`<button data-laeuft="` + html.EscapeString(texte.T(texte.Hakt)) + `">` + html.EscapeString(texte.T(texte.Eingerichtet)) + `</button></form>`)
	}
	return b.String() + `</section>` + einrichtenStyle
}

const einrichtenStyle = `<style>.einrichten ol{margin:0;padding:0 0 0 1.4em;background:var(--card);border:1px solid var(--edge);border-radius:14px}
.einrichten ol li{padding:10px 14px 10px 4px;border-top:1px solid var(--line);overflow-wrap:anywhere}.einrichten ol li:first-child{border-top:0}.einrichten li .knopf{margin:8px 0 0}
.einrichten .wege{display:flex;flex-wrap:wrap;align-items:center;gap:8px 16px}.einrichten .knopf{display:inline-flex;align-items:center;min-height:44px;padding:0 18px;border-radius:999px;background:var(--accent-soft);color:var(--accent);font-weight:600;text-decoration:none}
.einrichten .adresse{font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;font-size:.9rem}.einrichten form{margin:12px 0 0}</style>`

// Einrichten is /einrichten/<name>: the module's setup block, whole, and
// POST, the person's tick where nothing can tell; then back where it was
// pressed. A module that is off, or without an app, is a 404.
func Einrichten(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name, user := strings.TrimPrefix(r.URL.Path, EinrichtenPages), r.Header.Get("Remote-User")
		a, ok := cfg.Einrichtung.Eine(user, name)
		if !ok {
			missing{http.StatusNotFound, "Diese Seite gibt es nicht.", "Aufgaben", aufgabenPath, "Deine Aufgaben"}.write(w, r, cfg)
			return
		}
		if r.Method == http.MethodPost {
			if !fromHomes(r) {
				http.Error(w, "Das geht nur von Homes' eigener Seite.", http.StatusForbidden)
				return
			}
			now, _ := cfg.clock()
			if err := cfg.Einrichtung.Haken.Setzen(user, name, einrichten.Hand, now); err != nil {
				log.Printf("homes: einrichten: %s ticks %s: %v", user, name, err)
				http.Error(w, texte.T(texte.HakenNicht), http.StatusInternalServerError)
				return
			}
			back := r.PostFormValue("zurueck")
			if !strings.HasPrefix(back, "/") || strings.HasPrefix(back, "//") {
				back = einrichten.Seite(name)
			}
			http.Redirect(w, r, back, http.StatusSeeOther)
			return
		}
		if !showOnly(w, r, a.Titel) {
			return
		}
		fetch(r, func(ctx context.Context) { cfg.Einrichtung.Pruefen(ctx, user) })
		b := familyHead(r, cfg, a.Titel, "Aufgaben")
		b.WriteString("<h1>" + html.EscapeString(a.Titel) + "</h1>" + einrichtenBlock(r, a))
		writePage(w, http.StatusOK, b)
	}
}

// offeneZahl is how many tasks Deine Aufgaben lists open for the person:
// Homes' own, and Laib's for them when a page has read Laib's list in the
// last while (laib.Gemerkt: every page's nav shows it, Laib is not asked).
func offeneZahl(r *http.Request, cfg StartConfig) int {
	as, user := einrichtung(r, cfg), r.Header.Get("Remote-User")
	if cfg.Laib != nil && user != "" {
		if la, ok := cfg.Laib.Gemerkt(user); ok {
			as = mitLaibs(r, cfg, as, la)
		}
	}
	return len(offen(as))
}

// zahl is the counter beside an entry: the open tasks, none at 0.
func zahl(n int) string {
	if n == 0 {
		return ""
	}
	return ` <span class=anzahl aria-label="` + html.EscapeString(fmt.Sprintf(texte.T(texte.Offen), n)) + `">` + strconv.Itoa(n) + `</span>`
}
