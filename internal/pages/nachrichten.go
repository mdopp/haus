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

// DatenPath is the page „Daten sichern“ (haus#207, laib ADR 0029; owner
// 2026-11-03: more than messages go into the archive now).
const DatenPath = "/daten-sichern"

// NachrichtenPath is the page's old address, „Nachrichten sichern“: it
// leads to DatenPath, so a bookmark or an older app still lands.
const NachrichtenPath = "/nachrichten-sichern"

// What the page says, in the family's words.
const (
	saysDatenLead     = "Deine Nachrichten, Kontakte, Mails und Termine kommen in dein eigenes Archiv auf der Box. Öffnen kannst nur du es, mit deinem Archiv-Passwort."
	saysWieSieLiegen  = "Auf der Box liegen die Sicherungen so verschlüsselt, wie WhatsApp und Signal sie schreiben. Lesbar werden sie erst in deinem Archiv, mit deinen Schlüsseln."
	saysSchluessel    = "Archiv-Passwort, Signal-Passphrase und WhatsApp-Schlüssel gehören in Vaultwarden. Wer einen davon verliert, kann diese Sicherungen nicht mehr lesen."
	saysStandFehlt    = "Der Stand lässt sich gerade nicht lesen. Später noch einmal versuchen."
	saysStandortNicht = "Dein Standortverlauf gehört nicht dazu: er geht in deinen Reitti-Zeitstrahl (Mehr → Standortverlauf)."
	saysMenues        = "Die Menüs heißen je nach App-Version etwas anders. Findest du einen Punkt nicht, such in den Einstellungen der App nach „Backup“."
)

var notSetArchiv = notSet{"Das Archiv ist auf dieser Box nicht eingerichtet.",
	"Für Admins: Haus hat keinen Archiv-Eingang (HAUS_ARCHIV_DIR; laibs haus-Template hängt ${DATA_DIR}/archiv-eingang als /archiv-eingang ein)."}

// quelleName is how the page names a quelle.
var quelleName = map[string]string{"whatsapp": "WhatsApp", "signal": "Signal", "sms": "SMS und Anrufe"}

// the guide, per source: what the person does, then what happens
var datenAnleitung = []struct {
	title string
	steps []string
	note  string
}{
	{"Signal", []string{
		"In Signal: Einstellungen → Chats → Chat-Backups (je nach Version: Backups → Auf dem Gerät) einschalten und einen Ordner wählen, zum Beispiel „Signal-Backups“.",
		"Die 30-stellige Passphrase in Vaultwarden ablegen. Signal zeigt sie nur dieses eine Mal.",
		"In der Haus-App: Mehr → Daten sichern → bei Signal den Ordner freigeben, den du gewählt hast.",
		"In deinem Archiv die Passphrase eingeben.",
	}, "Signal legt jede Nacht eine Sicherung an, die App bringt sie auf die Box. Gelesen wird sie, während dein Archiv offen ist."},
	{"WhatsApp", []string{
		"In WhatsApp: Einstellungen → Chats → Chat-Backup → „Ende-zu-Ende-verschlüsseltes Backup“ einschalten.",
		"„64-stelligen Schlüssel verwenden“ wählen, nicht Passwort und nicht Passkey: aus einem Passkey lässt sich der Schlüssel nicht auslesen. Nutzt WhatsApp schon einen Passkey: das Backup aus- und gleich wieder einschalten, diesmal mit dem 64-stelligen Schlüssel.",
		"Den Schlüssel in Vaultwarden ablegen.",
		"In der Haus-App: Mehr → Daten sichern → bei WhatsApp den Ordner freigeben (Android → media → com.whatsapp → WhatsApp).",
		"In deinem Archiv den Schlüssel eingeben.",
	}, "Die Nachrichten kommen ins Archiv, sobald es offen ist. Fotos und Videos bringt die App verschlüsselt mit auf die Box; durchsuchen kannst du im Archiv vorerst die Nachrichten."},
	{"SMS und Anrufe", []string{
		"In der Haus-App: Mehr → Daten sichern → „SMS und Anrufe übertragen“ einschalten.",
	}, "Die App verschlüsselt sie schon auf dem Handy. Ins Archiv kommen sie, während es offen ist."},
	{"Kontakte", []string{
		"In der Haus-App: Mehr → Daten sichern → „Kontakte übertragen“ einschalten.",
	}, "Die Kontakte kommen in dein Adressbuch „Handy-Kontakte“ auf der Box und von dort ins Archiv, während es offen ist. Löschst du einen Kontakt auf dem Handy, bleibt er im Archiv."},
	{"E-Mail", []string{
		"In deinem Archiv den Zugang zu deinem Postfach eintragen: Server, Benutzername und ein App-Passwort.",
	}, "Das Archiv holt jede Stunde neue Mails ab, auch wenn es zu ist. Im Postfach ändert es nichts."},
	{"Kalender", []string{
		"In Haus unter Kalender deine Google-Kalender verbinden; eigene private Kalender legst du dort auch an.",
	}, "Das Archiv übernimmt jeden Termin, während es offen ist. Gelöschte Termine bleiben im Archiv."},
}

// the phone change (haus#209, haus-android#197): three steps, the app's words
var neuesHandy = []string{
	"Auf dem neuen Handy die Haus-App installieren und mit deinem Namen koppeln.",
	"In der Haus-App: Mehr → Daten sichern → „Sicherung zurückholen“. Für Signal einen Ordner wählen, etwa „Dokumente/Signal“. WhatsApp vorher installieren, aber noch nicht öffnen, und den Ordner „Android/media/com.whatsapp/WhatsApp“ freigeben (gibt es ihn noch nicht: „Android/media“).",
	"Signal neu installieren, bei der Einrichtung „Aus Sicherung wiederherstellen“ wählen, die Datei auswählen und die 30-stellige Passphrase aus Vaultwarden eingeben. WhatsApp öffnen, mit derselben Nummer anmelden und „Wiederherstellen“ wählen.",
}

const saysNeuesHandyMedien = "Fotos und Sprachnachrichten kommen nur zurück, während dein Archiv offen ist. Die App überschreibt nichts, ohne zu fragen."

// datenNie is what never reaches an archive, whatever the phone does.
var datenNie = []string{
	"Nachrichten, die sich selbst löschen (verschwindende Nachrichten): sie stehen in keiner Sicherung.",
	"Fotos und Videos zur einmaligen Ansicht.",
}

const nachrichtenStyle = `<style>.lead{color:var(--muted)}.schritte ol{margin:0;padding:10px 14px 10px 34px;background:var(--card);border:1px solid var(--edge);border-radius:14px}
.schritte ol li{margin:6px 0;overflow-wrap:anywhere}.schritte p.k{font-size:.85rem;margin:8px 2px 0}</style>`

// Nachrichten is the page „Daten sichern“: the person's state — what
// arrived from each phone source, what waits for their archive, the room
// left — from the same ledger the app's /napi/sicherung/stand reads, and
// per source what to do and what then happens. Its old address
// (NachrichtenPath) leads here.
func Nachrichten(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == NachrichtenPath {
			http.Redirect(w, r, DatenPath, http.StatusMovedPermanently)
			return
		}
		if !showOnly(w, r, "Daten sichern") {
			return
		}
		if cfg.Sicherung == nil {
			notSetArchiv.write(w, r, cfg, "Daten sichern")
			return
		}
		person := r.Header.Get("Remote-User")
		if person == "" {
			http.Error(w, "kein Remote-User: diese Seite gehört hinter Authelia", http.StatusForbidden)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		b := familyHead(r, cfg, "Daten sichern", "Daten sichern")
		b.WriteString(nachrichtenStyle)
		b.WriteString("<h1>Daten sichern</h1><p class=lead>" + html.EscapeString(saysDatenLead) + "</p>")
		b.WriteString(nachrichtenStand(cfg, person))
		b.WriteString(nachrichtenZurueck(cfg, person))
		keys := cardsList("Deine Schlüssel")
		keys.row(saysSchluessel, "", false)
		b.WriteString(keys.html())
		for _, a := range datenAnleitung {
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
		b.WriteString("<p class=k>" + html.EscapeString(saysStandortNicht) + "</p>")
		nie := cardsList("Was nie ins Archiv kommt")
		for _, n := range datenNie {
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

// nachrichtenZurueck is the card „Für ein neues Handy“: what the archive
// keeps of the newest backup (napi's Letzte, the same as the app's
// /napi/sicherung/letzte), with date and size, and the phone change in
// three steps.
func nachrichtenZurueck(cfg StartConfig, person string) string {
	c := cardsList("Für ein neues Handy")
	letzte, err := cfg.Sicherung.Letzte(person)
	if err != nil {
		log.Printf("haus: nachrichten: letzte of %s: %v", person, err)
		c.row(saysStandFehlt, "", false)
	} else {
		_, loc := cfg.clock()
		am := func(ms int64) string {
			t := time.UnixMilli(ms)
			return "vom " + dayWords(t, loc) + ", " + t.In(loc).Format("15:04")
		}
		var signal *napi.LetzteDatei
		for i, d := range letzte["signal"] {
			if signal == nil || d.Geaendert > signal.Geaendert {
				signal = &letzte["signal"][i]
			}
		}
		if signal != nil {
			c.row("Signal", "Sicherung "+am(signal.Geaendert)+" · "+groesseWorte(signal.Groesse), true)
		} else {
			c.row("Signal", "noch keine Sicherung zum Zurückholen", true)
		}
		var says []string
		var medien int
		var medienBytes int64
		for _, d := range letzte["whatsapp"] {
			if strings.HasPrefix(d.Pfad, "Media/") {
				medien++
				medienBytes += d.Groesse
			} else if d.Pfad == "Databases/msgstore.db.crypt15" {
				says = append(says, "Chats "+am(d.Geaendert)+" · "+groesseWorte(d.Groesse))
			}
		}
		if medien > 0 {
			says = append(says, anzahl(medien, "Foto oder Sprachnachricht", "Fotos und Sprachnachrichten")+", "+groesseWorte(medienBytes))
		}
		if len(says) == 0 {
			says = append(says, "noch nichts zum Zurückholen")
		}
		c.row("WhatsApp", strings.Join(says, " · "), true)
	}
	var b strings.Builder
	b.WriteString(c.html())
	b.WriteString("<section class=\"karte schritte\"><h2>Handy wechseln</h2><ol>")
	for _, s := range neuesHandy {
		b.WriteString("<li>" + html.EscapeString(s) + "</li>")
	}
	b.WriteString("</ol><p class=k>" + html.EscapeString(saysNeuesHandyMedien) + "</p></section>")
	return b.String()
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
