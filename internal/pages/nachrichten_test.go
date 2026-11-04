package pages

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/homes/internal/napi"
)

// haus#207, owner 2026-11-03: „Daten sichern“ shows the person's state
// from Homes' ledger and the inbox, and per source what to do and what then
// happens; no Discord, the location history only as a pointer.
func TestDatenSichernShowsTheStandAndTheGuide(t *testing.T) {
	inbox, data := t.TempDir(), t.TempDir()
	os.MkdirAll(filepath.Join(data, "sicherung", "anna"), 0o700)
	os.WriteFile(filepath.Join(data, "sicherung", "anna", "whatsapp.jsonl"), []byte(
		`{"pfad":"Databases/msgstore.db.crypt15","groesse":3435973837,"zeit":"2026-10-31T02:10:00Z"}`+"\n"+
			`{"pfad":"Media/WhatsApp Images/IMG-1.jpg","groesse":1000,"zeit":"2026-10-31T02:11:00Z"}`+"\n"), 0o600)
	os.MkdirAll(filepath.Join(inbox, "anna", "whatsapp", "Databases"), 0o755)
	os.WriteFile(filepath.Join(inbox, "anna", "whatsapp", "Databases", "msgstore.db.crypt15"), []byte("x"), 0o644)
	os.MkdirAll(filepath.Join(inbox, "anna", ".teil"), 0o755)
	os.WriteFile(filepath.Join(inbox, "anna", ".teil", "halb"), []byte("x"), 0o644) // a part does not wait
	cfg := StartConfig{Sicherung: napi.NewSicherung(inbox, data), Module: modulListe(t, "sicherung")}

	code, body, hdr := familyPage(t, Nachrichten, cfg, "GET", DatenPath, "family")
	if code != 200 || hdr.Get("Cache-Control") != "no-store" {
		t.Fatalf("%d %q", code, hdr.Get("Cache-Control"))
	}
	for _, want := range []string{"<h1>Daten sichern</h1>", "Dein Stand",
		"zuletzt am 31. Oktober 2026 um 03:11 · 2 Dateien, 3,2 GB · 1 Datei wartet auf dein Archiv", // WhatsApp, Berlin time
		"noch nichts angekommen", " frei.", "Vaultwarden",
		"<h2>Signal</h2>", "Chat-Backups", "Backups → Auf dem Gerät", "30-stellige Passphrase", "In deinem Archiv die Passphrase eingeben",
		"<h2>WhatsApp</h2>", "Einstellungen → Chats → Chat-Backup", "Ende-zu-Ende-verschlüsseltes Backup", "64-stelligen Schlüssel verwenden", "nicht Passkey",
		"<h2>SMS und Anrufe</h2>", "„SMS und Anrufe übertragen“ einschalten",
		"<h2>Kontakte</h2>", "„Handy-Kontakte“", "bleibt er im Archiv",
		"<h2>E-Mail</h2>", "App-Passwort", "jede Stunde",
		"<h2>Kalender</h2>", "Google-Kalender", "Gelöschte Termine bleiben im Archiv",
		"Mehr → Daten sichern", "Mehr → Standortverlauf",
		"Was nie ins Archiv kommt", "sich selbst löschen", "einmaligen Ansicht", "je nach App-Version"} {
		if !strings.Contains(body, want) {
			t.Errorf("the page lacks %q", want)
		}
	}
	for _, gone := range []string{"Discord", "Nachrichten sichern", "kommt mit dem Archiv"} {
		if strings.Contains(body, gone) {
			t.Errorf("the page still says %q", gone)
		}
	}
	before(t, body, "Dein Stand", "<h2>Signal</h2>")
	if strings.Contains(body, "<form") || strings.Contains(body, "<button") {
		t.Error("nothing on the page changes anything yet: no form, no button")
	}
	// another person's page shows only theirs
	if _, ben, _ := familyPageAs(t, Nachrichten, cfg, "GET", DatenPath, "ben", "family"); strings.Contains(ben, "3,2 GB") || strings.Contains(ben, "wartet") {
		t.Errorf("Ben sees Anna's backups:\n%s", ben)
	}
	if code, _, _ := familyPageAs(t, Nachrichten, cfg, "GET", DatenPath, "", ""); code != 403 {
		t.Errorf("without a person → %d", code)
	}
	if code, _, _ := familyPage(t, Nachrichten, cfg, "POST", DatenPath, "family"); code != 405 {
		t.Errorf("POST → %d", code)
	}
	// Mehr lists it for a signed-in person on a box with the inbox
	if _, mehr, _ := familyPage(t, Mehr, cfg, "GET", mehrPath, "family"); !strings.Contains(mehr, "href="+DatenPath) || !strings.Contains(mehr, "Daten sichern") {
		t.Error("Mehr does not lead to Daten sichern")
	}
	// the old address still lands
	if code, _, hdr := familyPage(t, Nachrichten, cfg, "GET", NachrichtenPath, "family"); code != 301 || hdr.Get("Location") != DatenPath {
		t.Errorf("the old address → %d %q", code, hdr.Get("Location"))
	}
}

// haus#209: the page shows what lies ready for a new phone (date, size)
// and the phone change in three steps; only the person's own.
func TestDatenSichernShowsWhatANewPhoneGetsBack(t *testing.T) {
	inbox, data := t.TempDir(), t.TempDir()
	lege := func(rel string, b []byte, at time.Time) {
		p := filepath.Join(inbox, "anna", "letzte", filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, b, 0o644)
		os.Chtimes(p, at, at)
	}
	nacht := time.Date(2026, 11, 2, 2, 0, 0, 0, time.UTC)
	lege("signal/signal-2026-11-02-03-00-00.backup", make([]byte, 3<<20), nacht)
	lege("whatsapp/Databases/msgstore.db.crypt15", make([]byte, 2<<20), nacht.Add(time.Hour))
	// two media, age-shaped: header, nonce, one chunk with its tag
	for _, m := range []string{"WhatsApp Images/IMG-1.jpg", "WhatsApp Voice Notes/PTT-1.opus"} {
		age := []byte("age-encryption.org/v1\n-> X25519 abc\nxyz\n--- mac\n")
		age = append(age, make([]byte, 16+1024+16)...)
		lege("whatsapp/Media/"+m+".age", age, nacht)
	}
	cfg := StartConfig{Sicherung: napi.NewSicherung(inbox, data)}
	_, body, _ := familyPage(t, Nachrichten, cfg, "GET", DatenPath, "family")
	for _, want := range []string{"Für ein neues Handy",
		"Sicherung vom 2. November 2026, 03:00 · 3,0 MB", // Berlin time
		"Chats vom 2. November 2026, 04:00 · 2,0 MB · 2 Fotos und Sprachnachrichten, 2,0 KB",
		"<h2>Handy wechseln</h2>", "„Sicherung zurückholen“", "noch nicht öffnen", "Android/media/com.whatsapp/WhatsApp",
		"„Aus Sicherung wiederherstellen“", "30-stellige Passphrase aus Vaultwarden", "„Wiederherstellen“ wählen",
		"während dein Archiv offen ist"} {
		if !strings.Contains(body, want) {
			t.Errorf("the page lacks %q", want)
		}
	}
	if strings.Count(body, "<h2>Handy wechseln</h2>") != 1 || strings.Count(body[strings.Index(body, "Handy wechseln"):], "<li>") < 3 {
		t.Error("not three steps")
	}
	before(t, body, "Dein Stand", "Für ein neues Handy")
	_, ben, _ := familyPageAs(t, Nachrichten, cfg, "GET", DatenPath, "ben", "family")
	if strings.Contains(ben, "3,0 MB") || !strings.Contains(ben, "noch keine Sicherung zum Zurückholen") || !strings.Contains(ben, "noch nichts zum Zurückholen") {
		t.Errorf("Ben's page:\n%s", ben)
	}
}

func TestDatenSichernWithoutTheArchiv(t *testing.T) {
	code, body, _ := familyPage(t, Nachrichten, StartConfig{}, "GET", DatenPath, "family")
	if code != 200 || !strings.Contains(body, "Das Archiv ist auf dieser Box nicht eingerichtet.") || strings.Contains(body, "Dein Stand") {
		t.Fatalf("%d %s", code, body)
	}
	if _, admin, _ := familyPage(t, Nachrichten, StartConfig{}, "GET", DatenPath, "admins"); !strings.Contains(admin, "HOMES_ARCHIV_DIR") {
		t.Error("an admin hears what is missing")
	}
	if _, mehr, _ := familyPage(t, Mehr, StartConfig{}, "GET", mehrPath, "family"); strings.Contains(mehr, DatenPath) {
		t.Error("Mehr lists a page the box does not have")
	}
	// a stand that cannot be read is one plain line
	cfg := StartConfig{Sicherung: napi.NewSicherung(t.TempDir(), "/dev/null/kein")}
	if _, body, _ := familyPage(t, Nachrichten, cfg, "GET", DatenPath, "family"); !strings.Contains(body, saysStandFehlt) {
		t.Errorf("an unreadable ledger:\n%s", body)
	}
}

func TestGroesseWorte(t *testing.T) {
	for n, want := range map[int64]string{0: "0 Bytes", 999: "999 Bytes", 1536: "1,5 KB", 450 << 20: "450 MB", 3435973837: "3,2 GB", 12 << 40: "12 TB", 1500 << 30: "1,5 TB"} {
		if got := groesseWorte(n); got != want {
			t.Errorf("groesseWorte(%d) = %q, want %q", n, got, want)
		}
	}
	if got := anzahl(12345, "Datei", "Dateien"); got != "12.345 Dateien" {
		t.Errorf("anzahl = %q", got)
	}
}
