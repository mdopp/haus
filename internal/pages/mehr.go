package pages

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/mdopp/homes/internal/cards"
	"github.com/mdopp/homes/internal/modul"
	"github.com/mdopp/homes/internal/texte"
)

// mehrPath is Mehr's page.
const mehrPath = "/mehr"

// What Mehr says of each of its pages, in a line.
var mehrSays = map[string]string{
	"Dokumente":               "Briefe, Rechnungen und Verträge ablegen und wiederfinden",
	"Deine Aufgaben":          "Was du einmal einrichtest, offen und erledigt",
	"Podcasts":                "Podcasts per Link abonnieren; Bescheid bei jeder neuen Folge",
	"Gesehen & Gehört":        "Was du geschaut und gehört hast; was als Nächstes auf die Wunschliste kommt",
	"Platte erfassen":         "Eine Schallplatte fotografieren und in deine Discogs-Sammlung eintragen",
	"Discogs":                 "Dein Discogs-Konto verbinden: Sammlung, Wantlist, Platte erfassen",
	"Musik":                   "Hinzufügen und was beim Einsortieren hängt, Platten erfassen, die Musik-Wunschliste",
	"Netzlaufwerk":            "Dein Passwort für das Netzlaufwerk",
	"Standortverlauf":         "Wo du unterwegs warst, in deiner eigenen Zeitleiste",
	"Daten sichern":           "Nachrichten, Kontakte, Mails und Termine in dein Archiv",
	"Box":                     "Wie es der Box geht, für Admins",
	texte.T("zusammen.titel"): texte.T("zusammen.mehr"),
}

// einmalig are the pages a person sets up once and then rarely opens, and Box.
var einmalig = map[string]bool{StandortPath: true, netzlaufwerkPath: true, DatenPath: true, DiscogsPath: true, aufgabenPath: true, "/box": true}

// mehrStand is where a page stands for the person, from what Homes keeps
// itself (Mehr never waits for a backend); "" where there is nothing cheap to say.
func mehrStand(r *http.Request, cfg StartConfig, pfad string) string {
	user := r.Header.Get("Remote-User")
	now, loc := cfg.clock()
	switch pfad {
	case aufgabenPath:
		if n := offeneZahl(r, cfg); n > 0 { // the counter, in words (owner 2026-11-04)
			return fmt.Sprintf(texte.T(texte.Offen), n) + " · " + mehrSays["Deine Aufgaben"]
		}
	case StandortPath:
		if k, ok := cfg.Reitti.Konto(user); !ok {
			return "Noch nicht verbunden"
		} else if !k.Zuletzt.IsZero() {
			return "Zuletzt aufgezeichnet " + dayWord(k.Zuletzt, now, loc) + " " + k.Zuletzt.In(loc).Format("15:04")
		}
		return "Verbunden, noch kein Punkt angekommen"
	case netzlaufwerkPath:
		if acc, err := cfg.Samba.Accounts(); err == nil && !acc[user].IsZero() {
			return "Passwort seit " + dayWords(acc[user], loc)
		}
		return "Noch kein Passwort"
	case DatenPath:
		var last time.Time
		st, _ := cfg.Sicherung.Stand(user)
		for _, q := range st.Quellen {
			if t := am(q); t.After(last) {
				last = t
			}
		}
		if last.IsZero() {
			return "Noch nichts angekommen"
		}
		return "Zuletzt angekommen " + dayWord(last, now, loc) + " " + last.In(loc).Format("15:04")
	case MusikPath: // the inbox, read here; beets' own word is Laib's, which Mehr does not wait for
		if cfg.MusicInbox == "" {
			return ""
		} else if n := len(zurueck(cfg.MusicInbox, cfg.Loc)); n > 0 {
			return texte.Anzahl(n, "Datei", "Dateien") + " " + texte.T(texte.MusikNicht)
		} else if n := len(musikWartet(cfg.MusicInbox)); n > 0 {
			return texte.Anzahl(n, "Datei", "Dateien") + map[bool]string{true: " wird", false: " werden"}[n == 1] + " einsortiert"
		}
		return "Alles einsortiert"
	case PodcastsPath:
		if cfg.Podcasts == nil { // no podcasts on this box
			return ""
		}
		if mine, _ := cfg.Podcasts.Meine(user); len(mine) > 0 {
			return texte.Anzahl(len(mine), "Podcast", "Podcasts") + " abonniert"
		}
	case DiscogsPath:
		if k, ok := cfg.Discogs.Konto(user); ok {
			return "Verbunden mit „" + k.Name + "“"
		}
	}
	return ""
}

// Mehr is the bar's last entry on the phone (haus#67): the pages that
// have no entry of their own — Dokumente (owner 2026-10-27: Dokumente
// unter Mehr), the music, Aufgaben, Netzlaufwerk, Box for admins,
// each only when this box has it — and the account: who is
// signed in, and Abmelden. On a wide window the list on the right holds
// the same, open.
func Mehr(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case mehrPath:
		case MehrAufgabenPath: // the Einrichtung card, after the page
			mehrAufgaben(w, r, cfg)
			return
		case SpurenPath: // the traces with Laib's wishes, after the page (ADR 0001 §10)
			spuren(w, r, cfg)
			return
		default:
			Elsewhere(cfg)(w, r)
			return
		}
		if !showOnly(w, r, "Mehr") {
			return
		}
		b := familyHead(r, cfg, "Mehr", "Mehr")
		b.WriteString("<h1>Mehr</h1>")
		b.WriteString(einrichtungKarte(r, cfg)) // haus#188: the open setup first; gone once done
		// the Mehr audit (owner 2026-11-04): what a person uses day to day first; what is set up
		// once, and the admins' Box, lower and quieter, each saying where it stands
		pages, einmal := cards.Card{Title: "Seiten"}, cards.Card{Title: "Einrichten", Look: "leise"}
		for _, t := range eintraege(r, cfg, modul.Mehr) {
			says := mehrSays[t.Titel]
			if stand := mehrStand(r, cfg, t.Pfad); stand != "" {
				says = stand
			}
			if einmalig[t.Pfad] {
				einmal.AddLink(t.Titel, says, t.Pfad)
			} else {
				pages.AddLink(t.Titel, says, t.Pfad)
			}
		}
		konto := cards.Card{Title: "Konto"}
		konto.AddLink("Abmelden", strings.TrimSpace("Angemeldet als "+r.Header.Get("Remote-User")), "/abmelden")
		b.WriteString(pages.HTML() + gesundheitKarte(cfg) + einmal.HTML() + konto.HTML() + `<style>.karte.leise li a{color:var(--muted)}</style>`)
		b.WriteString(spurenKarte(r, cfg)) // what this box lacks, last and quiet (ADR 0001 §10)
		writePage(w, http.StatusOK, b)
	}
}

// What the Gesundheit card says (haus#192): its pages, each a link, and
// honestly who sees what in one quiet line — Homes links and holds nothing of it.
// Owner, 2026-10-30: the first row was text that looked like a link, and one
// „Weiter in Gesundheit“ hid that Gesundheit has several pages.
const saysGesundheitWer = "Nur du siehst deine Werte; wer die Box verwaltet (Admin), sieht die Dokumente in Paperless. Nur zu Hause im WLAN."

// gesundheitSeiten are Gesundheit's pages (mdopp/gesundheit#10), in the order a person uses them.
var gesundheitSeiten = []struct{ text, detail, path string }{
	{"Werte", "Blutwerte und Messungen: im Normalbereich?, was hat sich verändert", "/werte"},
	{"Befunde", "Arztbriefe und Laborblätter, die neuesten zuerst", "/befunde"},
	{"Wie es mir ging", "Deine Tagesnotizen im Verlauf", "/befinden"},
	{"Für den Arzt", "Fragen zu deinen Werten, zum Ausdrucken", "/arzt"},
}

// gesundheitKarte is Mehr's card for the Gesundheit program (haus#192,
// laib#893), only on a box that has it (HOMES_GESUNDHEIT_URL): an internal
// route, so the links open at home only, as Paperless does.
func gesundheitKarte(cfg StartConfig) string {
	if cfg.GesundheitURL == "" {
		return ""
	}
	c := cards.Card{Title: "Gesundheit", Note: saysGesundheitWer}
	base := strings.TrimSuffix(cfg.GesundheitURL, "/")
	for _, s := range gesundheitSeiten {
		c.AddLink(s.text, s.detail, base+s.path)
	}
	return c.HTML()
}
