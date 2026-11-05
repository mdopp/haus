// Package modul is what one area of Homes says about itself (ADR 0001):
// what it is called, what it talks to, what it serves and what it offers
// the app. Homes builds one Liste from them at start and puts a new one in
// its place when what the box runs changes (Setzen). Standard library only
// (scripts/loc.sh: shared).
package modul

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
)

// Ort is where a module's entry sits in the family's selection.
type Ort uint8

const (
	Nirgends Ort = iota // no entry: a field, a card or a part of another page
	Leiste              // a tab of its own in the bar (Geräte, Energie)
	Haushalt            // a pill under the Haushalt tab (Kalender, Einkauf, Notizen)
	Mehr                // an entry under Mehr (Dokumente, Podcasts, …)
)

// Eintrag is one entry of the family's selection.
type Eintrag struct {
	Titel, Pfad, Icon string
	Ort               Ort
	Rang              int                      // its place at Ort among the core's and the modules' entries, lowest first
	Fuer              func(*http.Request) bool // who sees it (signed in, admins); nil: everyone
}

// Route is one path a module serves: a page behind the proxy secret, or a
// /napi path behind the paired device's bearer. Each module's come by its
// name from its surface's groups (napi.Gruppen; homes#243), which Homes
// mounts with the core's. An off module's group is what its paths answer
// without its backend: where a released app relies on that answer (ADR
// 0001 §4) a sentence, else nothing, so the surface's 404.
type Route struct {
	Muster  string // a ServeMux pattern: "GET /napi/standort", "/standort"
	Handler http.Handler
}

// Angebot is what a module offers the app, in the app's keys
// (GET /napi/kann "app"; the keys are listed in ADR 0001 §2).
type Angebot struct {
	Widgets   []string `json:"widgets"`   // "geraet", "energie", "tool:note"
	Teilen    []string `json:"teilen"`    // share targets: "merken", "dokument", "standort-import"
	Konto     []string `json:"konto"`     // the Homes account's parts: "kontakte", "kalender"
	Sicherung []string `json:"sicherung"` // the backup's parts: "whatsapp", "signal", "sms", "kontakte"
	Mehr      []string `json:"mehr"`      // the app's own Mehr rows: "daten-sichern", "standortverlauf"
}

// plus is a with b's keys it lacks added, in order; every list non-nil,
// so the JSON says [] and never null.
func (a Angebot) plus(b Angebot) Angebot {
	add := func(to, from []string) []string {
		to = append([]string{}, to...)
		for _, k := range from {
			if !slices.Contains(to, k) {
				to = append(to, k)
			}
		}
		return to
	}
	return Angebot{add(a.Widgets, b.Widgets), add(a.Teilen, b.Teilen), add(a.Konto, b.Konto), add(a.Sicherung, b.Sicherung), add(a.Mehr, b.Mehr)}
}

// Why a module is off (Modul.Warum).
const (
	Fehlt = "fehlt" // the box does not run its backend
	Aus   = "aus"   // switched off in Laib although the backend runs
)

// Modul is one area of Homes.
type Modul struct {
	Name     string   // the key: /napi/kann, HOMES_MODULE_AUS, the log. Never renamed.
	Titel    string   // German, as the family reads it: "Standortverlauf"
	Backend  string   // the Laib template it talks to: "reitti"; "" for Homes' own folder
	Standard string   // the open protocol, where there is one: "caldav", "carddav"
	Env      []string // the variables it reads (a test holds every HOMES_* to a module or the core)
	Braucht  []string // modules it needs on: energie needs geraete
	Admins   bool     // only admins use it: its trace is on the Box page, not on Mehr (§10)
	// Schaltbar: HOMES_MODULE_AUS can switch it off (ADR 0001 H3–H6); the
	// Box page offers its switch only then
	Schaltbar bool

	Nav   []Eintrag // its entries in the family's selection
	Start []string  // Start's parts it fills: "im-haus", "meine-geraete", "heute"
	App   Angebot
	// Einrichtung is how a person sets it up; nil: nothing to set up
	Einrichtung *Einrichtung

	// Grund is why it is off, one German sentence; "" while it is on.
	Grund string
	// Warum is the machine's word for Grund: Fehlt or Aus.
	Warum string
}

// Einrichtung is a module's setup (owner 2026-11-04: „die Einrichtung
// gehört zur Funktion“): the address to open, its own phone app if it has
// one, short steps and the vendor's guide for the rest. A module that is
// on and has an App is a task for each person (internal/einrichten).
type Einrichtung struct {
	Web       string   // the address to open, https://photos.<domain>; "" for none
	App       string   // its phone app's name, „Immich“; "" when it is web only
	Paket     string   // the app's Android package: the phone says it is installed
	Store     string   // where the app comes from
	Anleitung string   // the vendor's own guide
	Schritte  []string // after „Lade die App“, as texte ids: {adresse} is Web, {name} the assistant's
}

// An: is the module on?
func (m Modul) An() bool { return m.Warum == "" }

// Liste is the box's modules in catalog order, on and off. Every holder
// of one Liste (the pages, /napi, the setup tasks) sees a Setzen at once.
type Liste struct{ p atomic.Pointer[[]Modul] }

// alle is the modules now; nil for an empty Liste.
func (l *Liste) alle() []Modul {
	if a := l.p.Load(); a != nil {
		return *a
	}
	return nil
}

// Setzen puts n's modules in l's place, for every holder of l.
func (l *Liste) Setzen(n *Liste) { l.p.Store(n.p.Load()) }

// Bauen checks the catalog and makes the Liste: every name once, a name
// in lower case, every off module with its reason, and every module it
// needs on while it is on.
func Bauen(ms []Modul) (*Liste, error) {
	seen := map[string]bool{}
	for _, m := range ms {
		switch {
		case m.Name == "" || strings.ToLower(m.Name) != m.Name || strings.ContainsAny(m.Name, " ,"):
			return nil, fmt.Errorf("modul %q: a name is one lower-case word", m.Name)
		case seen[m.Name]:
			return nil, fmt.Errorf("modul %q: twice in the catalog", m.Name)
		case m.Warum != "" && m.Warum != Fehlt && m.Warum != Aus:
			return nil, fmt.Errorf("modul %s: Warum is %q, want %q or %q", m.Name, m.Warum, Fehlt, Aus)
		case !m.An() && m.Grund == "":
			return nil, fmt.Errorf("modul %s: off without a reason", m.Name)
		}
		seen[m.Name] = true
	}
	l, alle := &Liste{}, slices.Clone(ms)
	l.p.Store(&alle)
	for _, m := range ms {
		for _, b := range m.Braucht {
			if !seen[b] {
				return nil, fmt.Errorf("modul %s needs %s, which is not in the catalog", m.Name, b)
			}
			if m.An() && !l.An(b) {
				return nil, fmt.Errorf("modul %s is on, but %s, which it needs, is off", m.Name, b)
			}
		}
	}
	return l, nil
}

// An: is the module of that name on? An unknown name is off.
func (l *Liste) An(name string) bool {
	alle := l.alle()
	i := slices.IndexFunc(alle, func(m Modul) bool { return m.Name == name })
	return i >= 0 && alle[i].An()
}

// String is the start's log line: "Module an: geraete (home-assistant), …;
// aus: standort (fehlt: Auf dieser Box läuft kein Reitti.), …".
func (l *Liste) String() string {
	var an, aus []string
	for _, m := range l.alle() {
		if m.An() {
			an = append(an, strings.TrimSuffix(m.Name+" ("+m.Backend+")", " ()"))
		} else {
			aus = append(aus, m.Name+" ("+m.Warum+": "+m.Grund+")")
		}
	}
	return "Module an: " + strings.Join(an, ", ") + "; aus: " + strings.Join(aus, ", ")
}

// Eintraege are the entries at o of the modules that are on, with kern
// (the core's own) among them: those r may see, by Rang, in catalog order
// where two share one. A nil Liste has the core's only.
func (l *Liste) Eintraege(r *http.Request, o Ort, kern ...Eintrag) []Eintrag {
	var es []Eintrag
	for _, e := range kern {
		if e.Ort == o {
			es = append(es, e)
		}
	}
	if l != nil {
		for _, m := range l.alle() {
			for _, e := range m.Nav {
				if m.An() && e.Ort == o {
					es = append(es, e)
				}
			}
		}
	}
	es = slices.DeleteFunc(es, func(e Eintrag) bool { return e.Fuer != nil && !e.Fuer(r) })
	slices.SortStableFunc(es, func(a, b Eintrag) int { return a.Rang - b.Rang })
	return es
}

// Module are the box's modules in catalog order, on and off.
func (l *Liste) Module() []Modul { return slices.Clone(l.alle()) }

// Eins is the module of that name.
func (l *Liste) Eins(name string) (Modul, bool) {
	if l == nil { // a box without a module list
		return Modul{}, false
	}
	alle := l.alle()
	i := slices.IndexFunc(alle, func(m Modul) bool { return m.Name == name })
	if i < 0 {
		return Modul{}, false
	}
	return alle[i], true
}

// Frage is what Laib's wish stand is asked (laib#1038): every module with
// its backend, "geraete=home-assistant … werkstatt=".
func (l *Liste) Frage() string {
	var b strings.Builder
	for i, m := range l.alle() {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(m.Name + "=" + m.Backend)
	}
	return b.String()
}

// Wunsch is the state of a module's latest wish, as Laib answers it
// (laib#1038): angefordert, wartet, abgelehnt, eingerichtet, umgesetzt.
type Wunsch struct {
	Stand string `json:"stand"`
	Seit  string `json:"seit,omitempty"` // 2026-11-04
}

// Offen: a wish still under way, so its trace shows its state, no button.
func (w *Wunsch) Offen() bool {
	return w != nil && (w.Stand == "angefordert" || w.Stand == "wartet" || w.Stand == "eingerichtet")
}

// Wuensche are the box's wishes by module; Geht: Laib takes a wish.
type Wuensche struct {
	Je   map[string]Wunsch
	Geht bool
}

func (w Wuensche) von(name string) *Wunsch {
	if x, ok := w.Je[name]; ok {
		return &x
	}
	return nil
}

// Spur is the key of an off module's trace (ADR 0001 §10): its wish's
// state while there is one, else fehlt or aus; "" while it is on. A wish
// umgesetzt while the module is off again is history, not its state.
func (m Modul) Spur(w *Wunsch) string {
	switch {
	case m.An():
		return ""
	case w != nil && w.Stand != "umgesetzt" && w.Stand != "":
		return w.Stand
	}
	return m.Warum
}

// Anfordern: does an off module's trace offer „Hier anfordern“?
func (m Modul) Anfordern(w Wuensche) bool { return !m.An() && w.Geht && !w.von(m.Name).Offen() }

// Kann is GET /napi/kann (ADR 0001 §2, §10): the box's modules for words,
// and what the app may offer, the union of the offers of those on.
type Kann struct {
	Version int        `json:"version"`
	Module  []KannEins `json:"module"`
	App     Angebot    `json:"app"`
}

// KannEins is one module as the app reads it. Bietet is its offer, on or
// off, so the app knows where an off module leaves its trace (§10).
//
// SpurID is the trace's stable key (fehlt, aus, or the wish's state), so
// an app in another language says it in its own words; Spur is the
// German the caller fills in.
type KannEins struct {
	Name      string  `json:"name"`
	Titel     string  `json:"titel"`
	An        bool    `json:"an"`
	Backend   string  `json:"backend,omitempty"`
	Standard  string  `json:"standard,omitempty"`
	Warum     string  `json:"warum,omitempty"`
	Grund     string  `json:"grund,omitempty"`
	Bietet    Angebot `json:"bietet"`
	Admins    bool    `json:"admins,omitempty"`
	SpurID    string  `json:"spur_id,omitempty"`
	Spur      string  `json:"spur,omitempty"`
	Wunsch    *Wunsch `json:"wunsch,omitempty"`
	Anfordern bool    `json:"anfordern,omitempty"`
}

// Kann is the list as the app reads it, with the wishes Laib knows.
func (l *Liste) Kann(w Wuensche) Kann {
	k := Kann{Version: 1, Module: []KannEins{}, App: Angebot{}.plus(Angebot{})}
	for _, m := range l.alle() {
		e := KannEins{Name: m.Name, Titel: m.Titel, An: m.An(), Backend: m.Backend, Standard: m.Standard,
			Warum: m.Warum, Grund: m.Grund, Bietet: Angebot{}.plus(m.App), Admins: m.Admins, SpurID: m.Spur(w.von(m.Name)), Anfordern: m.Anfordern(w)}
		if !m.An() {
			e.Wunsch = w.von(m.Name)
		}
		k.Module = append(k.Module, e)
		if m.An() {
			k.App = k.App.plus(m.App)
		}
	}
	return k
}
