package pages

import (
	"html"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mdopp/haus/internal/napi"
)

// NachrichtenPath is the page „Nachrichten sichern“ (haus#207, laib ADR 0029).
const NachrichtenPath = "/nachrichten-sichern"

// What the page says, in the family's words.
const (
	saysNachrichtenLead = "Die Haus-App bringt die Sicherungen von WhatsApp und Signal von deinem Handy auf die Box. Dort kommen sie in dein eigenes Nachrichten-Archiv, das nur du öffnest."
	saysWieSieLiegen    = "Auf der Box liegen die Sicherungen so verschlüsselt, wie WhatsApp und Signal sie schreiben. Lesbar werden sie erst in deinem Archiv, mit deinen Schlüsseln."
	saysSchluesselSpaet = "Schlüssel eingeben kommt mit dem Archiv. Bis dahin: Lege beide Schlüssel in Vaultwarden ab, wie unten beschrieben."
	saysStandFehlt      = "Der Stand lässt sich gerade nicht lesen. Später noch einmal versuchen."
	saysMenues          = "Die Menüs heißen je nach App-Version etwas anders. Findest du einen Punkt nicht, such in den Einstellungen der App nach „Backup“."
)

var notSetArchiv = notSet{"Das Nachrichten-Archiv ist auf dieser Box nicht eingerichtet.",
	"Für Admins: Haus hat keinen Archiv-Eingang (HAUS_ARCHIV_DIR; laibs haus-Template hängt ${DATA_DIR}/archiv-eingang als /archiv-eingang ein)."}

// quelleName is how the page names a quelle.
var quelleName = map[string]string{"whatsapp": "WhatsApp", "signal": "Signal", "discord": "Discord"}

// the guide, step by step: each part a heading and its steps
var nachrichtenAnleitung = []struct {
	title string
	steps []string
	note  string
}{
	{"WhatsApp, auf dem Handy", []string{
		"WhatsApp öffnen: Einstellungen → Chats → Chat-Backup.",
		"„Ende-zu-Ende-verschlüsseltes Backup“ antippen und einschalten.",
		"„64-stelligen Schlüssel verwenden“ wählen, nicht ein Passwort.",
		"Den Schlüssel in Vaultwarden ablegen, als eigenen Eintrag „WhatsApp-Schlüssel“. Wer ihn verliert, kann keine dieser Sicherungen mehr lesen.",
	}, "WhatsApp legt dann jede Nacht eine Sicherung auf dem Handy an."},
	{"Signal, auf dem Handy", []string{
		"Signal öffnen: Einstellungen → Chats → Chat-Backups (je nach Version: Backups → Auf dem Gerät).",
		"Einschalten und einen Ordner auf dem Handy wählen, zum Beispiel „Signal-Backups“.",
		"Die 30-stellige Passphrase in Vaultwarden ablegen, als eigenen Eintrag „Signal-Passphrase“. Signal zeigt sie nur dieses eine Mal.",
	}, ""},
	{"In der Haus-App", []string{
		"Mehr → Nachrichten sichern.",
		"Den WhatsApp-Ordner freigeben (Android → media → com.whatsapp → WhatsApp).",
		"Den Signal-Ordner freigeben, den du oben gewählt hast.",
	}, "Danach bringt die App neue Sicherungen auf die Box. Bricht die Verbindung ab, macht sie später dort weiter, wo sie war."},
	{"Discord", []string{
		"Server-Kanäle kommen später: über einen Bot, den du in deine Server einlädst.",
		"Direktnachrichten gibt es nur über dein Datenpaket: Discord → Einstellungen → Daten & Datenschutz → Daten anfordern. Discord schickt dir nach einigen Tagen eine Mail mit einem Zip.",
		"Das Zip teilst du später per „Teilen“ an Haus. Bis dahin heb es auf.",
	}, ""},
}

// nachrichtenNie is what never reaches an archive, whatever the phone does.
var nachrichtenNie = []string{
	"Nachrichten, die sich selbst löschen (verschwindende Nachrichten): sie stehen in keiner Sicherung.",
	"Fotos und Videos zur einmaligen Ansicht.",
}

const nachrichtenStyle = `<style>.lead{color:var(--muted)}.schritte ol{margin:0;padding:10px 14px 10px 34px;background:var(--card);border:1px solid var(--edge);border-radius:14px}
.schritte ol li{margin:6px 0;overflow-wrap:anywhere}.schritte p.k{font-size:.85rem;margin:8px 2px 0}</style>`

// Nachrichten is the page „Nachrichten sichern“: the person's state —
// what arrived from each messenger, what waits for their archive, the
// room left — from the same ledger the app's /napi/sicherung/stand reads,
// and how to switch the phone backups on.
func Nachrichten(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !showOnly(w, r, "Nachrichten sichern") {
			return
		}
		if cfg.Sicherung == nil {
			notSetArchiv.write(w, r, cfg, "Nachrichten sichern")
			return
		}
		person := r.Header.Get("Remote-User")
		if person == "" {
			http.Error(w, "kein Remote-User: diese Seite gehört hinter Authelia", http.StatusForbidden)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		b := familyHead(r, cfg, "Nachrichten sichern", "Nachrichten sichern")
		b.WriteString(nachrichtenStyle)
		b.WriteString("<h1>Nachrichten sichern</h1><p class=lead>" + html.EscapeString(saysNachrichtenLead) + "</p>")
		b.WriteString(nachrichtenStand(cfg, person))
		keys := cardsList("Deine Schlüssel")
		keys.row(saysSchluesselSpaet, "", false)
		b.WriteString(keys.html())
		for _, a := range nachrichtenAnleitung {
			b.WriteString("<section class=\"karte schritte\"><h2>" + html.EscapeString(a.title) + "</h2><ol>")
			for _, s := range a.steps {
				b.WriteString("<li>" + html.EscapeString(s) + "</li>")
			}
			b.WriteString("</ol>")
			if a.note != "" {
				b.WriteString("<p class=k>" + html.EscapeString(a.note) + "</p>")
			}
			b.WriteString("</section>")
		}
		nie := cardsList("Was nie ins Archiv kommt")
		for _, n := range nachrichtenNie {
			nie.row(n, "", false)
		}
		b.WriteString(nie.html())
		b.WriteString("<p class=k>" + html.EscapeString(saysMenues) + "</p>")
		writePage(w, http.StatusOK, b)
	}
}

// nachrichtenStand is the card „Dein Stand“: per messenger what arrived and
// what waits, then the room on the box; one plain line when it cannot be read.
func nachrichtenStand(cfg StartConfig, person string) string {
	c := cardsList("Dein Stand")
	st, err := cfg.Sicherung.Stand(person)
	if err != nil {
		log.Printf("haus: nachrichten: stand of %s: %v", person, err)
		c.row(saysStandFehlt, "", false)
		return c.html()
	}
	_, loc := cfg.clock()
	for _, q := range napi.Quellen {
		qs := st.Quellen[q]
		var says []string
		if qs.Zuletzt != nil {
			if t, err := time.Parse(time.RFC3339, *qs.Zuletzt); err == nil {
				says = append(says, "zuletzt am "+dayWords(t, loc)+" um "+t.In(loc).Format("15:04"))
			}
			says = append(says, anzahl(qs.Dateien, "Datei", "Dateien")+", "+groesseWorte(qs.Bytes))
		} else {
			says = append(says, "noch nichts angekommen")
		}
		if qs.Wartend > 0 {
			verb := " warten"
			if qs.Wartend == 1 {
				verb = " wartet"
			}
			says = append(says, anzahl(qs.Wartend, "Datei", "Dateien")+verb+" auf dein Archiv")
		}
		c.row(quelleName[q], strings.Join(says, " · "), true)
	}
	c.row("Auf der Box sind noch "+groesseWorte(st.FreiBytes)+" frei.", "", false)
	c.row(saysWieSieLiegen, "", false)
	return c.html()
}

// anzahl is "1 Datei", "12 Dateien".
func anzahl(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return tausender(int64(n)) + " " + many
}

// tausender is n with the German thousands point: 12.345.
func tausender(n int64) string {
	s := strconv.FormatInt(n, 10)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "." + s[i:]
	}
	return s
}

// groesseWorte is a size as the family reads it: "3,2 GB", "450 MB", "12 KB".
func groesseWorte(n int64) string {
	units := []struct {
		size int64
		name string
	}{{1 << 40, "TB"}, {1 << 30, "GB"}, {1 << 20, "MB"}, {1 << 10, "KB"}}
	for _, u := range units {
		if n >= u.size {
			v := float64(n) / float64(u.size)
			if v >= 10 {
				return tausender(int64(v+0.5)) + " " + u.name
			}
			return strings.Replace(strconv.FormatFloat(v, 'f', 1, 64), ".", ",", 1) + " " + u.name
		}
	}
	return tausender(n) + " Bytes"
}
