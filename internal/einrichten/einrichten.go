// Package einrichten is each person's setup tasks (owner 2026-11-04: „die
// Einrichtung gehört zur Funktion, als Aufgabe pro App“): one task per
// module that is on and has its own phone app (modul.Einrichtung), and one
// for the Homes app. A task is done when Homes can tell (the Homes app is
// paired; the phone reports the module's app installed, POST
// /napi/einrichtung/apps; a module's own check, Jellyfin's devices) or
// when the person ticks it by hand. What was found or ticked is kept per
// person in HOMES_DATA/einrichtung.json, so a module switched off and on
// again is still done; a module that is off has no task.
package einrichten

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mdopp/homes/internal/jsonfile"
	"github.com/mdopp/homes/internal/laib"
	"github.com/mdopp/homes/internal/modul"
	"github.com/mdopp/homes/internal/texte"
)

// File is the ticks' file in Homes' data folder.
const File = "einrichtung.json"

// Homes is the Homes app's key among the modules' names; it is no module.
const Homes = "homes"

// How a task was found done (Stand.Wie).
const (
	Gekoppelt = "gekoppelt" // the person paired a Homes app
	Handy     = "app"       // the phone said the module's app is installed
	Erkannt   = "erkannt"   // the module's own service knows a device of theirs
	Hand      = "hand"      // the person ticked it
)

// Stand is a done task: since when, and how Homes knows.
type Stand struct {
	Am  string `json:"am"` // 2026-11-04
	Wie string `json:"wie"`
}

// Haken are the persons' done tasks, {"anna": {"fotos": {am, wie}}}.
type Haken struct {
	path string
	mu   sync.Mutex // one write at a time; a read needs none (a write replaces the file whole)
}

// Open names the file in dir; it need not be there.
func Open(dir string) *Haken { return &Haken{path: filepath.Join(dir, File)} }

func (h *Haken) read() (map[string]map[string]Stand, error) {
	all := map[string]map[string]Stand{}
	b, err := os.ReadFile(h.path)
	if errors.Is(err, fs.ErrNotExist) {
		return all, nil
	}
	if err == nil {
		err = json.Unmarshal(b, &all)
	}
	return all, err
}

// Von is one person's done tasks by module; none when the file does not read.
func (h *Haken) Von(person string) map[string]Stand {
	if h == nil || person == "" {
		return nil
	}
	all, _ := h.read()
	return all[person]
}

// Setzen keeps a module's task as done for the person; one done already
// keeps its first Stand.
func (h *Haken) Setzen(person, name, wie string, now time.Time) error {
	if h == nil || person == "" {
		return errors.New("einrichten: no ticks kept")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	all, err := h.read()
	if err != nil {
		return err
	}
	if _, ok := all[person][name]; ok {
		return nil
	}
	if all[person] == nil {
		all[person] = map[string]Stand{}
	}
	all[person][name] = Stand{Am: now.Format("2006-01-02"), Wie: wie}
	return jsonfile.Save(h.path, all)
}

// Quellen is what tells a person's tasks.
type Quellen struct {
	Module    *modul.Liste
	Haken     *Haken
	HomesApp  *modul.Einrichtung       // the Homes app's own setup; nil: no task for it
	Gekoppelt func(person string) bool // a Homes app is paired for the person; nil: never
	// Erkennen is a module's own check, where Homes reads its service
	// already (Jellyfin's devices): true is a device of the person's
	Erkennen map[string]func(ctx context.Context, person string) (bool, error)
	Adresse  string            // Homes' public address, https://homes.<domain>
	Ziele    map[string]string // the app's launcher shortcuts that this box has (Antwort.Ziele)
	Now      func() time.Time  // nil: time.Now
	// Immich is how many of the Immich key owner's questions are open
	// („Dieselbe Person?“, „Ist das …?“) for person: 0 for anyone else; nil none
	Immich func(person string) int
}

// ImmichPruefen is the key of the task of the open questions in Immich; it
// is no module.
const ImmichPruefen = "immich-pruefen"

// ImmichPfad is where those questions are answered (pages.DieselbePath).
const ImmichPfad = "/kontakte/dieselbe"

// Aufgabe is one module's setup for one person.
type Aufgabe struct {
	Modul, Titel, Warum string
	Einrichtung         *modul.Einrichtung
	Erledigt            *Stand // nil while open
}

// Fragen are the person's questions to answer, each one task while it is
// open and none once nothing is: today those in Immich, for the key's
// owner only (owner 2026-11-07), the count as it is now. The task's
// Modul is ImmichPruefen, done at ImmichPfad.
func (q *Quellen) Fragen(person string) []Aufgabe {
	if q == nil || q.Immich == nil || person == "" || !q.Module.An("fotos") {
		return nil
	}
	n := q.Immich(person)
	if n <= 0 {
		return nil
	}
	return []Aufgabe{{Modul: ImmichPruefen, Titel: fmt.Sprintf(texte.T(texte.DieselbeAufgabe), texte.Anzahl(n, "Person", "Personen")), Warum: texte.T(texte.DieselbeAufgabeWarum)}}
}

// Seite is where a task is done when its module has no page of its own.
func Seite(name string) string { return "/einrichten/" + name }

// Fuer are the person's tasks: the Homes app first, then each module that
// is on and has an app, in catalog order. Nothing is asked of a backend.
func (q *Quellen) Fuer(person string) []Aufgabe {
	if q == nil || person == "" {
		return nil
	}
	done := q.Haken.Von(person)
	var out []Aufgabe
	if e := q.HomesApp; e != nil {
		a := Aufgabe{Modul: Homes, Titel: fmt.Sprintf(texte.T(texte.Aufgabe), texte.T(texte.HomesApp)), Warum: fmt.Sprintf(texte.T(texte.MitApp), e.App), Einrichtung: e}
		if s, ok := done[Homes]; ok {
			a.Erledigt = &s
		} else if q.Gekoppelt != nil && q.Gekoppelt(person) {
			a.Erledigt = &Stand{Wie: Gekoppelt}
		}
		out = append(out, a)
	}
	for _, m := range q.Module.Module() {
		if !m.An() || m.Einrichtung == nil || m.Einrichtung.App == "" {
			continue
		}
		a := Aufgabe{Modul: m.Name, Titel: fmt.Sprintf(texte.T(texte.Aufgabe), m.Titel), Warum: fmt.Sprintf(texte.T(texte.MitApp), m.Einrichtung.App), Einrichtung: m.Einrichtung}
		if s, ok := done[m.Name]; ok {
			a.Erledigt = &s
		}
		out = append(out, a)
	}
	return out
}

// Eine is the person's task for the module of that name.
func (q *Quellen) Eine(person, name string) (Aufgabe, bool) {
	for _, a := range q.Fuer(person) {
		if a.Modul == name {
			return a, true
		}
	}
	return Aufgabe{}, false
}

func (q *Quellen) now() time.Time {
	if q.Now != nil {
		return q.Now()
	}
	return time.Now()
}

// Pruefen asks each open task's own check (within ctx) and keeps what it
// finds; a check that fails leaves its task open.
func (q *Quellen) Pruefen(ctx context.Context, person string) {
	for _, a := range q.Fuer(person) {
		if check := q.Erkennen[a.Modul]; a.Erledigt == nil && check != nil {
			if ok, err := check(ctx, person); err == nil && ok {
				q.Haken.Setzen(person, a.Modul, Erkannt, q.now())
			}
		}
	}
}

// Installiert ticks each open task whose app is among the phone's
// packages; how many it ticked.
func (q *Quellen) Installiert(person string, pakete []string) (int, error) {
	n := 0
	for _, a := range q.Fuer(person) {
		if a.Erledigt == nil && a.Einrichtung.Paket != "" && slices.Contains(pakete, a.Einrichtung.Paket) {
			if err := q.Haken.Setzen(person, a.Modul, Handy, q.now()); err != nil {
				return n, err
			}
			n++
		}
	}
	return n, nil
}

// Antwort is GET /napi/einrichtung (docs/REFERENCE.md): the person's
// modules as the app shows them, each with its task, how many are open,
// and where the launcher's shortcuts lead on this box.
type Antwort struct {
	Version int               `json:"version"`
	Offen   int               `json:"offen"`
	Module  []NapiModul       `json:"module"`
	Ziele   map[string]string `json:"ziele"`
}

// NapiModul is one module that is on, for everyone, with an address or an app.
type NapiModul struct {
	Name    string       `json:"name"`
	Titel   string       `json:"titel"`
	Icon    string       `json:"icon"`    // the app's key for its picture (docs/REFERENCE.md); the module's name unless it has its own
	Oeffnen bool         `json:"oeffnen"` // the app lists it under „Öffnen“, in this list's order
	Web     string       `json:"web,omitempty"`
	App     *NapiApp     `json:"app,omitempty"`
	NurWeb  bool         `json:"nur_web"`
	Aufgabe *NapiAufgabe `json:"aufgabe,omitempty"`
}

// NapiApp is a module's own phone app.
type NapiApp struct {
	Name  string `json:"name"`
	Paket string `json:"paket"`
	Store string `json:"store"`
}

// NapiAufgabe is the person's task for a module with an app.
type NapiAufgabe struct {
	Titel      string `json:"titel"`
	Erledigt   bool   `json:"erledigt"`
	Am         string `json:"am,omitempty"`
	Wie        string `json:"wie,omitempty"`
	Einrichten string `json:"einrichten"` // the setup's page, the whole address
}

// Napi is the person's Antwort.
func (q *Quellen) Napi(person string) Antwort {
	out := Antwort{Version: 1, Module: []NapiModul{}, Ziele: map[string]string{}}
	maps.Copy(out.Ziele, q.Ziele)
	tasks := q.Fuer(person)
	for _, m := range q.Module.Module() {
		e := m.Einrichtung
		if e == nil {
			e = &modul.Einrichtung{}
		}
		n := NapiModul{Name: m.Name, Titel: m.Titel, Icon: cmp.Or(m.Icon, m.Name), Oeffnen: m.Oeffnen, Web: e.Web, NurWeb: e.App == ""}
		if len(m.Nav) > 0 { // its page in Homes before its service's own
			n.Web = q.Adresse + m.Nav[0].Pfad
		}
		if !m.An() || m.Admins || (n.Web == "" && n.NurWeb) {
			continue
		}
		if !n.NurWeb {
			n.App = &NapiApp{e.App, e.Paket, e.Store}
		}
		if i := slices.IndexFunc(tasks, func(a Aufgabe) bool { return a.Modul == m.Name }); i >= 0 {
			a := tasks[i]
			n.Aufgabe = &NapiAufgabe{Titel: a.Titel, Erledigt: a.Erledigt != nil, Einrichten: q.Adresse + Seite(m.Name)}
			if a.Erledigt != nil {
				n.Aufgabe.Am, n.Aufgabe.Wie = a.Erledigt.Am, a.Erledigt.Wie
			} else {
				out.Offen++
			}
		}
		out.Module = append(out.Module, n)
	}
	for _, a := range q.Fragen(person) { // a question is no module: an entry of its own, after them, only while open
		web := q.Adresse + ImmichPfad
		out.Module = append(out.Module, NapiModul{Name: a.Modul, Titel: a.Titel, Icon: "fotos", Web: web, NurWeb: true,
			Aufgabe: &NapiAufgabe{Titel: a.Titel, Einrichten: web}})
		out.Offen++
	}
	return out
}

// Domain is the box's domain in Homes' address: example.org of
// https://homes.example.org; "" without one.
func Domain(publicURL string) string {
	u, err := url.Parse(publicURL)
	if err != nil {
		return ""
	}
	_, dom, _ := strings.Cut(u.Hostname(), ".")
	return dom
}

// Dienste are the services Laib runs, asked once within wait: the link
// modules are on with theirs; nil when Laib did not answer.
func Dienste(ctx context.Context, l *laib.Client, wait time.Duration) map[string]bool {
	c, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	_, rows, err := l.Status(c)
	if err != nil {
		log.Printf("homes: the services Laib runs, for the link modules: %v", err)
		return nil
	}
	out := map[string]bool{}
	for _, r := range rows {
		out[r.Name] = true
	}
	return out
}

// App is a module's own phone app from Google Play (owner 2026-11-04): the
// address it reaches ("": nothing to set up, nil), the vendor's guide, and
// the steps after „Lade die App“ (texte ids, the catalog's).
func App(name, paket, web, anleitung string, schritte ...string) *modul.Einrichtung {
	if web == "" {
		return nil
	}
	return &modul.Einrichtung{Web: web, App: name, Paket: paket, Store: "https://play.google.com/store/apps/details?id=" + paket, Anleitung: anleitung, Schritte: schritte}
}

// Web is a service's address on the box's domain; "" without one.
func Web(host, domain string) string {
	if domain == "" {
		return ""
	}
	return "https://" + host + "." + domain
}

// Signal is the chat with the house's number; "" without one.
func Signal(number string) string {
	if number == "" {
		return ""
	}
	return "https://signal.me/#p/" + url.PathEscape(number)
}
