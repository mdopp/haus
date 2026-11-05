package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/mdopp/homes/internal/caldav/davfake"
	"github.com/mdopp/homes/internal/cards"
	"github.com/mdopp/homes/internal/napi"
	"github.com/mdopp/homes/internal/pages"
	"github.com/mdopp/homes/internal/samba"
)

// mehrSeiten mounts the Mehr pages the demo had left out (the Mehr audit,
// owner 2026-11-04): Daten sichern, Netzlaufwerk, Box with its pages and
// Adresse, so the browser tests and the pictures cover them. cfg has the
// demo's Laib (laibFake answers Box's verbs from auditLaib) and Reitti.
// Anna has backups, a password and contacts; Ben, Clara none (the cookie
// wer=ben shows another person, in the family only).
func mehrSeiten(mux *http.ServeMux, cfg pages.StartConfig, dav *davfake.Server, dir string) {
	sambaDir := filepath.Join(dir, "samba")
	os.MkdirAll(sambaDir, 0o755)
	setSamba := func(gesetzt bool) { // /demo/samba?gesetzt=1: Anna's password is set
		at := "0"
		if gesetzt {
			at = strconv.FormatInt(time.Now().Add(-9*24*time.Hour).Unix(), 10)
		}
		os.WriteFile(filepath.Join(sambaDir, "status"), []byte("anna "+at+"\nben 0\nclara "+strconv.FormatInt(time.Now().Add(-40*24*time.Hour).Unix(), 10)+"\n"), 0o644)
	}
	setSamba(true)
	mux.HandleFunc("/demo/samba", func(w http.ResponseWriter, r *http.Request) { setSamba(r.URL.Query().Get("gesetzt") == "1") })

	archiv, ledger := filepath.Join(dir, "archiv-eingang"), filepath.Join(dir, "sicherung", "anna")
	os.MkdirAll(filepath.Join(archiv, "anna", "letzte", "signal"), 0o755)
	os.WriteFile(filepath.Join(archiv, "anna", "letzte", "signal", "signal-2026-11-04-03-00-00.backup"), make([]byte, 3<<20), 0o644)
	os.MkdirAll(ledger, 0o755)
	line := func(pfad string, groesse int64, vor time.Duration) string {
		return fmt.Sprintf(`{"pfad":%q,"groesse":%d,"zeit":%q}`+"\n", pfad, groesse, time.Now().Add(-vor).UTC().Format(time.RFC3339))
	}
	var signal, whatsapp string
	for i := range 40 {
		signal += line(fmt.Sprintf("signal-%02d.backup", i), 80<<20, time.Duration(i)*24*time.Hour+9*time.Hour)
	}
	whatsapp = line("Databases/msgstore.db.crypt15", 2<<30, 10*time.Hour) + line("Media/WhatsApp Images/IMG-1.jpg", 3<<20, 10*time.Hour)
	os.WriteFile(filepath.Join(ledger, "signal.jsonl"), []byte(signal), 0o600)
	os.WriteFile(filepath.Join(ledger, "whatsapp.jsonl"), []byte(whatsapp), 0o600)
	os.WriteFile(filepath.Join(ledger, "sms.jsonl"), []byte(line("sms-2026.xml.age", 120000, 3*time.Hour)), 0o600)
	dav.AddAddressBook("/anna/handy-kontakte/", "Handy-Kontakte")
	for i, n := range []string{"Oma Hilde", "Opa Karl", "Kita Sonnenschein"} {
		dav.Put("/anna/handy-kontakte/k"+strconv.Itoa(i)+".vcf", "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:k"+strconv.Itoa(i)+"\r\nFN:"+n+"\r\nEND:VCARD\r\n")
	}

	cfg.Samba, cfg.SambaHost = &samba.Dir{Path: sambaDir}, "box.local"
	cfg.Sicherung, cfg.Contacts, cfg.LaibURL = napi.NewSicherung(archiv, dir), cfg.Calendar, "https://laib.example"
	mux.HandleFunc(pages.DatenPath, pages.Nachrichten(cfg))
	// the household's address book, nine cards as on the box (owner 2026-11-05): the list and each contact's page
	dav.AddAddressBook("/homes/haushalt-kontakte/", "Haushalt")
	for i, k := range [][3]string{{"Anna Beispiel", "+49 151 23456789", "anna@example.org"}, {"Ben Beispiel", "+49 160 9876543", ""},
		{"Clara Beispiel", "", "clara@example.org"}, {"Ärztehaus am Markt", "0711 123450", "praxis@example.org"},
		{"Bäckerei Müller", "0711 556677", ""}, {"Kita Sonnenschein", "0711 909090", "kita@example.org"},
		{"Oma Hilde", "0711 445566", ""}, {"Zahnarzt Dr. Berg", "0711 778899", ""}, {"Hausverwaltung Schmidt", "", "info@hv-schmidt.example"}} {
		card := "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:h" + strconv.Itoa(i) + "\r\nFN:" + k[0] + "\r\n"
		if k[1] != "" {
			card += "TEL;TYPE=CELL:" + k[1] + "\r\n"
		}
		if k[2] != "" {
			card += "EMAIL:" + k[2] + "\r\n"
		}
		dav.Put("/homes/haushalt-kontakte/h"+strconv.Itoa(i)+".vcf", card+"END:VCARD\r\n")
	}
	mux.HandleFunc(pages.KontaktePath, pages.Kontakte(cfg))
	mux.HandleFunc(cards.ContactPages, pages.Kontakt(cfg))
	mux.HandleFunc("/netzlaufwerk", pages.Netzlaufwerk(cfg))
	boxCfg := pages.BoxConfig{Laib: cfg.Laib, LaibURL: cfg.LaibURL, Family: cfg}
	mux.HandleFunc("/box", pages.BoxPage(boxCfg))
	mux.HandleFunc("/box/", pages.BoxPage(boxCfg))
	mux.HandleFunc("/box/einladen", pages.Einladen(boxCfg))
	mux.HandleFunc(pages.AufgabenPath, pages.Aufgaben(boxCfg))
	mux.HandleFunc("/adresse", pages.Adresse(cfg))
}

// boxAntwort is the demo Laib's answer to one of Box's verbs; laib_status for one
// service (Musik's beets) is the wish fake's.
func boxAntwort(name string, args map[string]any) (string, bool) {
	if _, one := args["service"]; one {
		return "", false
	}
	text, ok := auditLaib[name]
	return text, ok
}

// auditLaib is what the demo's Laib answers for Box: a box with one red and
// two yellow services, a card waiting, last night's report, one skill, one
// person invited, the household's address.
var auditLaib = map[string]string{
	"laib_status": `{"ready":{"state":"bereit","services":23,"green":20,"yellow":2,"red":1,"operator":"läuft","drift":0,"aufgaben":0},"services":[` +
		`{"name":"paperless","state":"gelb","title":"Paperless","beschreibung":"Dokumente","detail":"Redis antwortet langsam"},` +
		`{"name":"immich","state":"rot","title":"Immich","beschreibung":"Fotos","detail":"Machine-Learning-Container startet nicht · seit 2 h"},` +
		`{"name":"media","state":"gelb","title":"Jellyfin","beschreibung":"Filme, Serien, Musik","detail":"Zertifikat läuft in 9 Tagen ab"},` +
		`{"name":"adguard","state":"grün","title":"AdGuard"}]}`,
	"laib_inbox":          `[{"id":412,"actor":"claude","reason":"Gesundheit: neue Version 0.9.2","tier":"apply","status":"open","created":"2026-11-04T09:00:00Z"}]`,
	"laib_journal":        `[{"t":"2026-11-04T06:00:00Z","actor":"operator","verb":"note","reason":"report","result":"ok","detail":"Nachts lief alles. Immich-ML startet nicht; Neustart zweimal versucht, wartet auf dich."}]`,
	"laib_skill":          `[{"name":"morgenbericht","source":"builtin","description":"Schreibt jeden Morgen, wie die Nacht war","trigger":{"daily":"06:00"},"tools":["laib_status","laib_note"],"modell":"frei","status":"active"}]`,
	"laib_person_pending": `[{"user":"oma","name":"Oma Hilde","email":"hilde@example.org","invited_at":"2026-11-01T10:00:00Z"}]`,
	"laib_zuhause":        `{"street":"Musterweg 1","postcode":"12345","city":"Musterstadt","country":"DE","latitude":50.1,"longitude":8.6}`,
}
