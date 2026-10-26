package pages

import (
	"net/http"

	"github.com/mdopp/haus/internal/cards"
)

// mehrPath is Mehr's page.
const mehrPath = "/mehr"

// What Mehr says of each of its pages, in a line.
var mehrSays = map[string]string{
	"Apps einrichten":  "So kommen die Apps der Box auf Handy und Computer",
	"Podcasts":         "Podcasts per Link abonnieren; Bescheid bei jeder neuen Folge",
	"Platte erfassen":  "Eine Schallplatte fotografieren und in deine Discogs-Sammlung eintragen",
	"Discogs":          "Dein Discogs-Konto verbinden: Sammlung, Wantlist, Platte erfassen",
	"Musik hinzufügen": "Lieder, Alben oder ZIPs hochladen; die Box sortiert sie ein",
	"Netzlaufwerk":     "Dein Passwort für das Netzlaufwerk",
	"Box":              "Wie es der Box geht, für Admins",
	"Adresse":          "Wo das Haus steht, für Admins",
}

// mehrMusik are Mehr's pages under the heading Musik.
var mehrMusik = map[string]bool{"Musik hinzufügen": true, "Platte erfassen": true, "Discogs": true}

// Mehr is the bar's fourth entry on the phone (haus#67): the pages that
// have no entry of their own — Energie, Medien, Notizen, Dokumente, Netzlaufwerk, Box
// and Adresse for admins, each only when this box has it — and the account: who is
// signed in, and Abmelden. On a wide window the list on the right holds
// the same, open.
func Mehr(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != mehrPath {
			Elsewhere(cfg)(w, r)
			return
		}
		if !showOnly(w, r, "Mehr") {
			return
		}
		show := shows(r, cfg)
		b := familyHead(r, cfg, "Mehr", "Mehr")
		b.WriteString("<h1>Mehr</h1>")
		musik, pages := cards.Card{Title: "Musik"}, cards.Card{Title: "Seiten"} // the music's pages under their own heading (owner 2026-10-26)
		for _, t := range mehr {
			if !show[t.name] {
				continue
			}
			if mehrMusik[t.name] {
				musik.AddLink(t.name, mehrSays[t.name], t.href)
			} else {
				pages.AddLink(t.name, mehrSays[t.name], t.href)
			}
		}
		if len(musik.Rows) > 0 {
			b.WriteString(musik.HTML())
		}
		b.WriteString(pages.HTML())
		konto := cards.Card{Title: "Konto"}
		if u := r.Header.Get("Remote-User"); u != "" {
			konto.Add("Angemeldet als "+u, "")
		}
		konto.AddLink("Abmelden", "Danach meldet sich jemand anderes an", "/abmelden")
		b.WriteString(konto.HTML())
		writePage(w, http.StatusOK, b)
	}
}
