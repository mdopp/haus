package pages

import (
	"net/http"

	"github.com/mdopp/haus/internal/cards"
)

// mehrPath is Mehr's page.
const mehrPath = "/mehr"

// What Mehr says of each of its pages, in a line.
var mehrSays = map[string]string{
	"Dokumente":       "Briefe, Rechnungen und Verträge ablegen und wiederfinden",
	"Apps einrichten": "So kommen die Apps der Box auf Handy und Computer",
	"Aufgaben":        "Was du einmal einrichtest, offen und erledigt",
	"Podcasts":        "Podcasts per Link abonnieren; Bescheid bei jeder neuen Folge",
	"Platte erfassen": "Eine Schallplatte fotografieren und in deine Discogs-Sammlung eintragen",
	"Discogs":         "Dein Discogs-Konto verbinden: Sammlung, Wantlist, Platte erfassen",
	"Musik":           "Hochladen, Platten erfassen, Discogs und die Musik-Wunschliste",
	"Netzlaufwerk":    "Dein Passwort für das Netzlaufwerk",
	"Box":             "Wie es der Box geht, für Admins",
}

// Mehr is the bar's last entry on the phone (haus#67): the pages that
// have no entry of their own — Dokumente (owner 2026-10-27: Dokumente
// unter Mehr), the music, Apps einrichten, Netzlaufwerk, Box for admins,
// each only when this box has it — and the account: who is
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
		b.WriteString(einrichtungKarte(r, cfg)) // haus#188: the open setup first; gone once done
		pages := cards.Card{Title: "Seiten"}
		for _, t := range mehr {
			if show[t.name] {
				pages.AddLink(t.name, mehrSays[t.name], t.href)
			}
		}
		b.WriteString(pages.HTML())
		b.WriteString(gesundheitKarte(cfg))
		konto := cards.Card{Title: "Konto"}
		if u := r.Header.Get("Remote-User"); u != "" {
			konto.Add("Angemeldet als "+u, "")
		}
		konto.AddLink("Abmelden", "Danach meldet sich jemand anderes an", "/abmelden")
		b.WriteString(konto.HTML())
		writePage(w, http.StatusOK, b)
	}
}

// What the Gesundheit card says (haus#192): what it is, and honestly who
// sees what — Haus links and holds nothing of it.
const (
	saysGesundheit     = "Deine Blutwerte, Befunde und wie es dir ging, als Verlauf."
	saysGesundheitWer  = "Nur du siehst deinen Verlauf. Wer die Box verwaltet (Admin), sieht die Dokumente in Paperless trotzdem."
	saysGesundheitLink = "Weiter in Gesundheit"
)

// gesundheitKarte is Mehr's card for the Gesundheit program (haus#192,
// laib#893), only on a box that has it (HAUS_GESUNDHEIT_URL): an internal
// route, so the link opens at home only, as Paperless does.
func gesundheitKarte(cfg StartConfig) string {
	if cfg.GesundheitURL == "" {
		return ""
	}
	c := cards.Card{Title: "Gesundheit"}
	c.Add(saysGesundheit, saysGesundheitWer)
	c.AddLink(saysGesundheitLink, saysOnlyAtHome, cfg.GesundheitURL)
	return c.HTML()
}
