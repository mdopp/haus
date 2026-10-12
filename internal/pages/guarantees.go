package pages

import (
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/mdopp/haus/internal/laib"
)

// familyNames: what a service is called at the kitchen table.
var familyNames = map[string]string{
	"immich": "Fotos", "media": "Musik und Filme", "audiobookshelf": "Hörbücher", "paperless": "Dokumente",
	"vaultwarden": "Passwörter", "radicale": "Kalender und Kontakte", "home-assistant": "das Haus (Licht, Heizung)",
	"file-share": "Dateien", "bote": "der Bote", "auth": "die Anmeldung", "caddy": "der Zugang", "adguard": "das Internet im Haus",
	"frage": "Frage", "stimme": "die Stimme", "music-assistant": "Musik im Haus", "haus": "Haus", "mosquitto": "die Geräte",
}

func familyName(svc string) string {
	if n, ok := familyNames[svc]; ok {
		return n
	}
	return svc
}

// guarantees renders the three lines the family sees first (haus#2):
// everything runs (or what does not, and that someone is on it), the last
// backup and where it is, the last change in plain words — Rückgängig only
// for admins, and on Laib's page (Haus decides nothing).
func guarantees(ready laib.Readiness, rows []laib.Service, admin bool, laibURL string) string {
	var b strings.Builder
	b.WriteString("<section class=guarantees>")
	// 1. Alles läuft
	var down []string
	for _, s := range rows {
		if s.State == "rot" {
			down = append(down, familyName(s.Name))
		}
	}
	if len(down) == 0 {
		b.WriteString("<p class=gruen><b>Alles läuft.</b></p>")
	} else {
		who := "Der Operator schaut nach."
		if ready.Operator != "läuft" && ready.Operator != "startet" {
			who = "Der Admin ist informiert."
		}
		fmt.Fprintf(&b, "<p class=rot><b>%s %s gerade nicht erreichbar.</b> %s</p>", html.EscapeString(strings.Join(down, ", ")), map[bool]string{true: "sind", false: "ist"}[len(down) > 1], who)
	}
	// 2. Letzte Sicherung
	switch {
	case !ready.LastBackup.OK:
		b.WriteString("<p class=rot>Letzte Sicherung: <b>noch keine</b>.</p>")
	default:
		at, _ := time.Parse(time.RFC3339, ready.LastBackup.At)
		age := time.Since(at)
		cls := "gruen"
		if age > 26*time.Hour || ready.LastBackup.Repos < 2 {
			cls = "gelb"
		}
		if age > 72*time.Hour || ready.LastBackup.Repos < 1 {
			cls = "rot"
		}
		places := fmt.Sprintf("an %d Orten", ready.LastBackup.Repos)
		if ready.LastBackup.Repos == 1 {
			places = "an einem Ort"
		}
		fmt.Fprintf(&b, "<p class=%s>Letzte Sicherung: <b>%s</b>, %s.</p>", cls, when(at), places)
	}
	// 3. Letzte Änderung
	if ready.LastChange.Summary != "" {
		at, _ := time.Parse(time.RFC3339, ready.LastChange.At)
		fmt.Fprintf(&b, "<p>Letzte Änderung: <b>%s</b> (%s)", html.EscapeString(ready.LastChange.Summary), when(at))
		if admin && ready.LastChange.Reversible {
			fmt.Fprintf(&b, " — <a href=\"%s/inbox\">Rückgängig</a> <span class=k>(auf der Laib-Seite, mit der Folge davor)</span>", html.EscapeString(strings.TrimRight(laibURL, "/")))
		}
		b.WriteString("</p>")
	}
	b.WriteString("</section>")
	return b.String()
}

// when says a time the family way: heute 03:12, gestern 18:40, 09.10. 07:00.
func when(t time.Time) string {
	if t.IsZero() {
		return "unbekannt"
	}
	t = t.Local()
	now := time.Now()
	y, m, d := now.Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	switch {
	case !t.Before(today):
		return "heute " + t.Format("15:04")
	case !t.Before(today.Add(-24 * time.Hour)):
		return "gestern " + t.Format("15:04")
	}
	return t.Format("02.01. 15:04")
}
