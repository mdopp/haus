// Package persona is the house persona as Laib renders it (laib ADR 0020):
// /persona/<name>/persona.json and icon.svg, mounted read-only. The family
// sees its name, greeting, icon and colours; internally everything stays Haus.
// A persona change restarts Haus (PERSONA_REV), so it is read once.
package persona

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
)

// Persona is the part of persona.json Haus shows.
type Persona struct {
	Name      string            `json:"name"`
	Display   string            `json:"display"`
	Languages []string          `json:"languages"`
	Greeting  map[string]string `json:"greeting"`
	Colors    map[string]string `json:"colors"`
	Icon      []byte            `json:"-"`
}

// Default is plain Haus: no persona mounted, or one that does not read.
func Default() Persona {
	return Persona{Display: "Haus", Colors: map[string]string{"accent": "#faf9f6", "ground": "#1d1d1b"}}
}

var nameRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

// Load reads dir/<name>/persona.json and icon.svg; Default on any failure, so
// a missing persona costs a costume, never the page.
func Load(dir, name string) Persona {
	if !nameRe.MatchString(name) {
		return Default()
	}
	b, err := os.ReadFile(filepath.Join(dir, name, "persona.json"))
	if err != nil {
		return Default()
	}
	var p Persona
	if json.Unmarshal(b, &p) != nil || p.Display == "" {
		return Default()
	}
	p.Icon, _ = os.ReadFile(filepath.Join(dir, name, "icon.svg"))
	return p
}

// Greet is the greeting in a language, else in the persona's first.
func (p Persona) Greet(lang string) string {
	if g := p.Greeting[lang]; g != "" {
		return g
	}
	if len(p.Languages) > 0 {
		return p.Greeting[p.Languages[0]]
	}
	return ""
}

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{3,8}$`)

func (p Persona) color(k, def string) string {
	if c := p.Colors[k]; hexColor.MatchString(c) {
		return c
	}
	return def
}

// Accent and Ground are the header's colours: accent on ground.
func (p Persona) Accent() string { return p.color("accent", "#faf9f6") }
func (p Persona) Ground() string { return p.color("ground", "#1d1d1b") }
