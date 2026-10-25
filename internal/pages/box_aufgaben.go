package pages

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mdopp/haus/internal/laib"
)

// AufgabenPath is where the Box page ticks off a Box-Aufgabe or moves its
// reminder (haus#115, laib#667): the box's own to-dos — a second disk, the
// off-box backup, flashing the Voice PE — that are box topics, not code.
// Admins only; laib records haus:<who> as who did it.
const AufgabenPath = "/box/aufgaben"

// aufgabeWait is how long a tick may take before the page says so.
var aufgabeWait = 20 * time.Second

const (
	saysAufgabenNone   = "Keine offene Box-Aufgabe."
	saysAufgabenNoTool = "Haus darf die Box-Aufgaben noch nicht lesen: Laib gibt dem Haus-Zugang das Verb aufgabe mit dem Vorschlag der Standard-Verben, den der Besitzer in der Inbox freigibt."
	saysAufgabeDown    = "Laib antwortet gerade nicht; die Aufgabe ist unverändert. Versuch es gleich noch einmal."
	aufgabenDone       = 7 // days a ticked-off one is still shown
)

// aufgabeRef is "Box-Aufgabe #N" in a line laib wrote: a link to it.
var aufgabeRef = regexp.MustCompile(`Box-Aufgabe #(\d+)`)

// linkAufgaben makes each "Box-Aufgabe #N" in escaped text a link to its
// place on the page (a finding deferred to it, laib#667).
func linkAufgaben(escaped string) string {
	return aufgabeRef.ReplaceAllString(escaped, `<a href="#aufgabe-$1">Box-Aufgabe #$1</a>`)
}

// Aufgaben answers the card's two POSTs: tun=erledigt (id) and tun=spaeter
// (id, erinnern JJJJ-MM-TT). Done: the Box page with the card saying so;
// refused or failed: laib's sentence, the page as it was (hausScript).
func Aufgaben(cfg BoxConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Redirect(w, r, "/box#box-aufgaben", http.StatusSeeOther)
			return
		}
		if !adminOnly(w, r) {
			return
		}
		if !fromHaus(r) {
			http.Error(w, "Das geht nur von Haus' eigener Seite.", http.StatusForbidden)
			return
		}
		r.ParseForm()
		id, err := strconv.Atoi(r.PostForm.Get("id"))
		if err != nil || id <= 0 {
			http.Error(w, "Welche Box-Aufgabe? Die Nummer fehlt.", http.StatusBadRequest)
			return
		}
		who := r.Header.Get("Remote-User")
		ctx, cancel := context.WithTimeout(r.Context(), aufgabeWait)
		defer cancel()
		var said string
		switch r.PostForm.Get("tun") {
		case "erledigt":
			said, err = cfg.Laib.AufgabeErledigt(ctx, id, who)
		case "spaeter":
			day := r.PostForm.Get("erinnern")
			t, perr := time.ParseInLocation("2006-01-02", day, time.Local)
			if perr != nil {
				http.Error(w, "Wann soll Laib wieder erinnern? Ein Datum fehlt.", http.StatusBadRequest)
				return
			}
			if !t.After(time.Now()) {
				http.Error(w, "Das Datum muss nach heute liegen.", http.StatusBadRequest)
				return
			}
			said, err = cfg.Laib.AufgabeSpaeter(ctx, id, day, who)
		default:
			http.Error(w, "Das geht hier nicht.", http.StatusBadRequest)
			return
		}
		if err != nil {
			http.Error(w, aufgabeProblem(err), aufgabeCode(err))
			return
		}
		log.Printf("haus: box-aufgabe #%d %s by %s", id, r.PostForm.Get("tun"), who)
		card := aufgabenCard(r.Context(), cfg.Laib, time.Now(), said, "")
		writeBoxWith(w, r, cfg, http.StatusOK, invitation{}, map[string]string{"box-aufgaben": card})
	}
}

// aufgabeProblem is laib's refusal as laib says it ("Box-Aufgabe #9 gibt es
// nicht"), else a sentence for what did not answer.
func aufgabeProblem(err error) string {
	switch {
	case errors.Is(err, laib.ErrNoTool):
		return saysAufgabenNoTool
	case strings.HasPrefix(err.Error(), "laib aufgabe: "):
		return strings.TrimPrefix(err.Error(), "laib aufgabe: ")
	}
	log.Printf("haus: box-aufgabe: %v", err)
	return saysAufgabeDown
}

func aufgabeCode(err error) int {
	switch {
	case errors.Is(err, laib.ErrNoTool):
		return http.StatusForbidden
	case strings.HasPrefix(err.Error(), "laib aufgabe: "):
		return http.StatusConflict
	}
	return http.StatusBadGateway
}

// day is a JJJJ-MM-TT date as the page says it: "25.10.".
func day(s string) string {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return s
	}
	return t.Format("02.01.")
}

// aufgabenCard is the card: open ones, the due first, then those without
// a reminder, then the deferred by date; each with Erledigt and Später;
// below, those ticked off in the last week, a person's tick apart from
// Laib's own. said is a POST's outcome on top.
func aufgabenCard(ctx context.Context, c *laib.Client, now time.Time, said, problem string) string {
	var b strings.Builder
	all, err := c.BoxAufgaben(ctx)
	head := func(extra string) { b.WriteString("<h2>Box-Aufgaben" + extra + "</h2>") }
	if err != nil {
		head("")
		if errors.Is(err, laib.ErrNoTool) {
			b.WriteString("<p class=k>" + html.EscapeString(saysAufgabenNoTool) + "</p>")
		} else {
			log.Printf("haus: box-aufgaben: %v", err)
			b.WriteString("<p class=rot>" + html.EscapeString(saysAufgabeDown) + "</p>")
		}
		return b.String()
	}
	today := now.Format("2006-01-02")
	var open, done []laib.Aufgabe
	for _, a := range all.Aufgaben {
		if a.Erledigt == nil {
			open = append(open, a)
		} else if t, err := time.Parse("2006-01-02", a.Erledigt.Am); err == nil && now.Sub(t) < aufgabenDone*24*time.Hour {
			done = append(done, a)
		}
	}
	rank := func(a laib.Aufgabe) int {
		switch {
		case a.Erinnern != "" && a.Erinnern <= today:
			return 0
		case a.Erinnern == "":
			return 1
		}
		return 2
	}
	sort.SliceStable(open, func(i, j int) bool {
		ri, rj := rank(open[i]), rank(open[j])
		if ri != rj {
			return ri < rj
		}
		if open[i].Erinnern != open[j].Erinnern {
			return open[i].Erinnern < open[j].Erinnern
		}
		return open[i].ID < open[j].ID
	})
	due := 0
	for _, a := range open {
		if rank(a) == 0 {
			due++
		}
	}
	extra := fmt.Sprintf(" <span class=k>· %d offen", len(open))
	if due > 0 {
		extra += fmt.Sprintf(" · <span class=gelb>%d fällig</span>", due)
	}
	head(extra + "</span>")
	if said != "" {
		b.WriteString("<div class=gut role=status><span class=punkt></span><p>" + html.EscapeString(said) + "</p></div>")
	}
	if problem != "" {
		b.WriteString("<div class=achtung role=alert><p>" + html.EscapeString(problem) + "</p></div>")
	}
	if len(open) == 0 {
		b.WriteString("<p class=k>" + saysAufgabenNone + "</p>")
	}
	later := now.AddDate(0, 0, 7).Format("2006-01-02")
	tomorrow := now.AddDate(0, 0, 1).Format("2006-01-02")
	b.WriteString("<ul class=aufgaben>")
	for _, a := range open {
		id := strconv.Itoa(a.ID)
		fmt.Fprintf(&b, "<li id=aufgabe-%s><b>%s</b>", id, html.EscapeString(a.Titel))
		if a.Warum != "" {
			b.WriteString("<span class=text>" + html.EscapeString(a.Warum) + "</span>")
		}
		meta := "#" + id + " · seit " + day(a.Seit)
		switch rank(a) {
		case 0:
			meta += " · <span class=gelb>fällig seit " + day(a.Erinnern) + "</span>"
		case 2:
			meta += " · erinnert am " + day(a.Erinnern)
		}
		if a.Befund != "" {
			meta += " · Laib hakt sie selbst ab, wenn der Befund (" + html.EscapeString(a.Befund) + ") weg ist"
		}
		b.WriteString("<span class=wann>" + meta + "</span><div class=aufgabe-tun>")
		b.WriteString(`<form method=post action=` + AufgabenPath + `><input type=hidden name=tun value=erledigt><input type=hidden name=id value=` + id +
			`><button data-laeuft="Hakt ab …">Erledigt</button></form>`)
		b.WriteString(`<form method=post action=` + AufgabenPath + ` class=spaeter><input type=hidden name=tun value=spaeter><input type=hidden name=id value=` + id +
			`><label>Erinnern am <input type=date name=erinnern value=` + later + ` min=` + tomorrow + ` required></label><button data-laeuft="Stellt zurück …">Später</button></form>`)
		b.WriteString("</div></li>")
	}
	b.WriteString("</ul>")
	if len(done) > 0 {
		b.WriteString("<details><summary>Zuletzt erledigt (" + strconv.Itoa(len(done)) + ")</summary><ul class=zeilen>")
		for _, a := range done {
			who := "von " + html.EscapeString(strings.TrimPrefix(a.Erledigt.Von, "haus:"))
			if a.Erledigt.Von == "laib" {
				who = "Laib: erledigt, der Befund ist weg"
			}
			fmt.Fprintf(&b, "<li id=aufgabe-%d><span class=was><b>%s</b><span class=wann>#%d · %s · %s</span></span></li>", a.ID, html.EscapeString(a.Titel), a.ID, day(a.Erledigt.Am), who)
		}
		b.WriteString("</ul></details>")
	}
	return aufgabenStyle + b.String()
}

const aufgabenStyle = `<style>.aufgaben{list-style:none;margin:10px 0 0;padding:0}.aufgaben>li{padding:12px 0;border-top:1px solid var(--line)}.aufgaben b{display:block}
.aufgaben .text{display:block;font-size:.85rem;color:var(--muted);margin:2px 0 0;overflow-wrap:anywhere}.aufgaben .wann{display:block;font-size:.75rem;color:var(--muted);margin:4px 0 0}
.aufgabe-tun{display:flex;flex-wrap:wrap;gap:8px;align-items:flex-end;margin:8px 0 0}.aufgabe-tun form{display:flex;gap:8px;align-items:flex-end;flex-wrap:wrap;margin:0}
.aufgabe-tun label{display:flex;flex-direction:column;font-size:.75rem;color:var(--muted)}.aufgabe-tun input[type=date]{min-height:44px;min-width:9.5rem;font:inherit;color:var(--ink);background:var(--card);border:1px solid var(--edge);border-radius:12px;padding:0 10px}</style>`
