// Package modul is what one area of Homes says about itself (ADR 0001):
// what it is called, what it talks to, what it serves and what it offers
// the app. Homes builds one Liste from them at start; it never changes
// while Homes runs. Standard library only (scripts/loc.sh: shared).
package modul

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
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
	Fuer              func(*http.Request) bool // who sees it (signed in, admins); nil: everyone
}

// Route is one path a module serves: a page behind the proxy secret, or a
// /napi path the Liste puts behind the paired device's bearer.
type Route struct {
	Muster  string // a ServeMux pattern: "GET /napi/standort", "/standort"
	Handler http.Handler
}

// Angebot is what a module offers the app, in the app's keys
// (GET /napi/kann "app"; the keys are listed in ADR 0001 §2).
type Angebot struct {
	Widgets   []string // "geraet", "energie", "tool:note"
	Teilen    []string // share targets: "merken", "dokument", "standort-import"
	Konto     []string // the Homes account's parts: "kontakte", "kalender"
	Sicherung []string // the backup's parts: "whatsapp", "signal", "sms", "kontakte"
	Mehr      []string // the app's own Mehr rows: "daten-sichern", "standortverlauf"
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

	Seiten []Route   // pages, behind the proxy secret
	Napi   []Route   // /napi paths, behind the device's bearer
	Nav    []Eintrag // its entries in the family's selection
	Start  []string  // Start's parts it fills: "im-haus", "meine-geraete", "heute"
	App    Angebot
	Laeuft []func(context.Context) // its background work, started only while it is on

	// Grund is why it is off, one German sentence; "" while it is on.
	Grund string
	// Warum is the machine's word for Grund: Fehlt or Aus.
	Warum string
	// Aus is what its paths answer while it is off, only where a released
	// app relies on that answer (ADR 0001 §4).
	Aus []Route
}

// An: is the module on?
func (m Modul) An() bool { return m.Warum == "" }

// Liste is the box's modules in catalog order, on and off.
type Liste struct{ alle []Modul }

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
	l := &Liste{alle: slices.Clone(ms)}
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
	i := slices.IndexFunc(l.alle, func(m Modul) bool { return m.Name == name })
	return i >= 0 && l.alle[i].An()
}

// String is the start's log line: "Module an: geraete (home-assistant), …;
// aus: standort (fehlt: Auf dieser Box läuft kein Reitti.), …".
func (l *Liste) String() string {
	var an, aus []string
	for _, m := range l.alle {
		if m.An() {
			an = append(an, strings.TrimSuffix(m.Name+" ("+m.Backend+")", " ()"))
		} else {
			aus = append(aus, m.Name+" ("+m.Warum+": "+m.Grund+")")
		}
	}
	return "Module an: " + strings.Join(an, ", ") + "; aus: " + strings.Join(aus, ", ")
}
