package pages

import (
	"encoding/base64"
	"fmt"
	"html"
	"strconv"
	"strings"

	"github.com/mdopp/homes/internal/texte"
)

// „Ist das …?“ on „Dieselbe Person?“ (owner 2026-11-06): a picture of the
// key owner's own contact beside the person Immich gave its face
// (gesichter.Kontaktbilder), and Ja (merge), Nein (never that person) and
// Kein Foto (never that picture). Nothing is merged without the Ja.

// istFragen is the section of questions, before the pairs; "" without.
func istFragen(cfg StartConfig, seite int) string {
	fragen := cfg.Gesichter.Fragen()
	if len(fragen) == 0 {
		return ""
	}
	t := func(id texte.ID) string { return html.EscapeString(texte.T(id)) }
	var b strings.Builder
	b.WriteString("<h2>" + t(texte.IstDas) + "</h2><p class=k>" + t(texte.IstDasHinweis) + "</p>")
	for _, f := range fragen {
		bild := `<span class=ohne aria-hidden=true></span>`
		if f.Bild != nil {
			bild = `<img src="data:image/jpeg;base64,` + base64.StdEncoding.EncodeToString(f.Bild) + `" alt="" width=96 height=96>`
		}
		b.WriteString(`<section class="paar frage"><p><b>` + html.EscapeString(fmt.Sprintf(texte.T(texte.IstDasFrage), f.Name)) + `</b></p><div class=zwei>` +
			`<figure>` + bild + `<figcaption><b>` + html.EscapeString(f.Name) + `</b><span>` + t(texte.IstDasKontaktbild) + `</span></figcaption></figure>` + dieselbeGesicht(f.Person) +
			`</div><form method=post action=` + DieselbePath + `><input type=hidden name=kontakt value="` + html.EscapeString(f.Key) +
			`"><input type=hidden name=b value="` + html.EscapeString(f.Person.ID) + `"><input type=hidden name=seite value=` + strconv.Itoa(seite) +
			`><button name=antwort value=ja data-laeuft="` + t(texte.DieselbeJaLaeuft) + `">` + html.EscapeString(fmt.Sprintf(texte.T(texte.IstDasJa), f.Name)) +
			`</button><button name=antwort value=nein data-laeuft="` + t(texte.DieselbeNeinLaeuft) + `">` + t(texte.DieselbeNein) +
			`</button><button name=antwort value=keinfoto class=leise data-laeuft="` + t(texte.IstDasKeinFotoLaeuft) + `">` + html.EscapeString(fmt.Sprintf(texte.T(texte.IstDasKeinFoto), f.Name)) + `</button></form></section>`)
	}
	return b.String() + "<h2>" + t(texte.Dieselbe) + "</h2>"
}

// istGesagt are the answers about a picture as the page says them.
var istGesagt = map[string]texte.ID{"nein": texte.IstDasNeinGemerkt, "keinfoto": texte.IstDasKeinFotoGemerkt, "stumm": texte.IstDasStumm}
