package pages

import (
	"context"
	"fmt"
	"strings"

	"github.com/mdopp/homes/internal/cards"
	"github.com/mdopp/homes/internal/texte"
)

// moduleBoxPath is Box's page „Was Homes kann“ (homes ADR 0001 H7): every
// module, its backend, on or off and why, and its wish; each leads to its
// own page (/module/<name>) with its switch.
const moduleBoxPath = "/box/module"

// moduleCard is the page's card: one row per module in catalog order. The
// wishes are Laib's (laib#1038), read within the part's time; without
// them the rows say what Homes knows itself.
func moduleCard(ctx context.Context, cfg StartConfig) string {
	if cfg.Module == nil {
		return ""
	}
	var je map[string]wunschZeile
	if cfg.Laib != nil {
		if geht, _ := cfg.Laib.NimmtWuensche(ctx); geht {
			if raw, err := cfg.Laib.Wuensche(ctx, cfg.Module.Frage()); err == nil {
				je = map[string]wunschZeile{}
				for name, w := range raw {
					je[name] = wunschZeile{w.Stand, strings.TrimPrefix(strings.TrimPrefix(w.Von, "homes:"), "homes"), w.Seit}
				}
			}
		}
	}
	c := cards.Card{Title: texte.T(texte.ModuleSeite)} // the page's h1 says „Was Homes kann“
	for _, m := range cfg.Module.Module() {
		parts := []string{}
		if m.Backend != "" {
			parts = append(parts, m.Backend)
		}
		if m.An() {
			parts = append(parts, texte.T(texte.ModulAn))
		} else {
			parts = append(parts, texte.T(texte.ID("spur."+m.Warum))+": "+m.Grund)
		}
		if w, ok := je[m.Name]; ok && w.stand != "umgesetzt" {
			parts = append(parts, fmt.Sprintf(texte.T(texte.WunschVon), texte.T(texte.ID("spur."+w.stand)), w.von, w.seit))
		}
		c.AddLink(m.Titel, strings.Join(parts, " · "), ModulPages+m.Name)
	}
	return c.HTML()
}

// wunschZeile is one wish as the card says it.
type wunschZeile struct{ stand, von, seit string }
