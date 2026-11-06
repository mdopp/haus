package pages

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/mdopp/homes/internal/cards"
	"github.com/mdopp/homes/internal/laib"
	"github.com/mdopp/homes/internal/modul"
	"github.com/mdopp/homes/internal/texte"
)

// haushaltPath is Box's tab Haushalt (owner 2026-11-06): one row each for
// Personen, Geräte, Adresse, Module and Nutzung with its state, each
// leading to its page — those pages are not in the tab bar.
const haushaltPath = "/box/haushalt"

// haushaltCard is the tab's list; what Laib does not answer is said in
// its row.
func haushaltCard(ctx context.Context, cfg BoxConfig) string {
	var pending []laib.Pending
	var z laib.Zuhause
	var perr, zerr error
	fetchCtx(ctx, func(ctx context.Context) { pending, perr = cfg.Laib.PersonPending(ctx) },
		func(ctx context.Context) { z, zerr = cfg.Laib.ZuhauseShow(ctx) })
	c := cards.Card{Title: texte.T("haushalt.karte")}
	personen := texte.T("haushalt.einladen")
	if n := len(pending); n > 0 {
		personen = texte.Anzahl(n, texte.T("haushalt.einladung"), texte.T("haushalt.einladung.n"))
	} else if perr != nil && !errors.Is(perr, laib.ErrNoTool) {
		personen = texte.T("haushalt.stumm")
	}
	c.AddLink("Personen", personen, "/box/personen")
	if cfg.Devices != nil {
		c.AddLink("Geräte", texte.Anzahl(len(cfg.Devices.List()), texte.T("haushalt.telefon"), texte.T("haushalt.telefon.n")), "/box/geraete")
	}
	adresse := texte.T("haushalt.adresse")
	switch {
	case zerr != nil:
		adresse = texte.T("haushalt.stumm")
	case z.Street == "" && z.Latitude == nil:
		adresse = texte.T("haushalt.keine-adresse")
	}
	c.AddLink("Adresse", adresse, adressePath)
	if m := cfg.Family.Module; m != nil {
		alle := len(m.Module())
		an := len(slices.DeleteFunc(m.Module(), func(x modul.Modul) bool { return !x.An() }))
		c.AddLink("Module", fmt.Sprintf(texte.T("haushalt.module"), an, alle), moduleBoxPath)
	}
	if u := cfg.Family.Usage; u != nil {
		c.AddLink("Nutzung", fmt.Sprintf(texte.T("haushalt.nutzung"), u.Started.In(familyLoc(cfg.Family)).Format("02.01.")), "/box/nutzung")
	}
	return c.HTML()
}
