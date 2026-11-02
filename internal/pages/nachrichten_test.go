package pages

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mdopp/haus/internal/napi"
)

// haus#207: „Nachrichten sichern“ shows the person's state from Haus's
// ledger and the inbox, and how to switch the phone backups on; the keys
// come with the archive (laib#986).
func TestNachrichtenSichernShowsTheStandAndTheGuide(t *testing.T) {
	inbox, data := t.TempDir(), t.TempDir()
	os.MkdirAll(filepath.Join(data, "sicherung", "anna"), 0o700)
	os.WriteFile(filepath.Join(data, "sicherung", "anna", "whatsapp.jsonl"), []byte(
		`{"pfad":"Databases/msgstore.db.crypt15","groesse":3435973837,"zeit":"2026-10-31T02:10:00Z"}`+"\n"+
			`{"pfad":"Media/WhatsApp Images/IMG-1.jpg","groesse":1000,"zeit":"2026-10-31T02:11:00Z"}`+"\n"), 0o600)
	os.MkdirAll(filepath.Join(inbox, "anna", "whatsapp", "Databases"), 0o755)
	os.WriteFile(filepath.Join(inbox, "anna", "whatsapp", "Databases", "msgstore.db.crypt15"), []byte("x"), 0o644)
	os.MkdirAll(filepath.Join(inbox, "anna", ".teil"), 0o755)
	os.WriteFile(filepath.Join(inbox, "anna", ".teil", "halb"), []byte("x"), 0o644) // a part does not wait
	cfg := StartConfig{Sicherung: napi.NewSicherung(inbox, data)}

	code, body, hdr := familyPage(t, Nachrichten, cfg, "GET", NachrichtenPath, "family")
	if code != 200 || hdr.Get("Cache-Control") != "no-store" {
		t.Fatalf("%d %q", code, hdr.Get("Cache-Control"))
	}
	for _, want := range []string{"<h1>Nachrichten sichern</h1>", "Dein Stand",
		"zuletzt am 31. Oktober 2026 um 03:11 · 2 Dateien, 3,2 GB · 1 Datei wartet auf dein Archiv", // WhatsApp, Berlin time
		"noch nichts angekommen", " frei.", "Schlüssel eingeben kommt mit dem Archiv",
		"Einstellungen → Chats → Chat-Backup", "Ende-zu-Ende-verschlüsseltes Backup", "64-stelligen Schlüssel verwenden", "nicht Passkey", "Vaultwarden",
		"Chat-Backups", "Backups → Auf dem Gerät", "30-stellige Passphrase",
		"Mehr → Nachrichten sichern", "Ordner freigeben",
		"Daten &amp; Datenschutz → Daten anfordern", "Server-Kanäle kommen später",
		"Was nie ins Archiv kommt", "sich selbst löschen", "einmaligen Ansicht", "je nach App-Version"} {
		if !strings.Contains(body, want) {
			t.Errorf("the page lacks %q", want)
		}
	}
	before(t, body, "Dein Stand", "WhatsApp, auf dem Handy")
	if strings.Contains(body, "<form") || strings.Contains(body, "<button") {
		t.Error("nothing on the page changes anything yet: no form, no button")
	}
	// another person's page shows only theirs
	if _, ben, _ := familyPageAs(t, Nachrichten, cfg, "GET", NachrichtenPath, "ben", "family"); strings.Contains(ben, "3,2 GB") || strings.Contains(ben, "wartet") {
		t.Errorf("Ben sees Anna's backups:\n%s", ben)
	}
	if code, _, _ := familyPageAs(t, Nachrichten, cfg, "GET", NachrichtenPath, "", ""); code != 403 {
		t.Errorf("without a person → %d", code)
	}
	if code, _, _ := familyPage(t, Nachrichten, cfg, "POST", NachrichtenPath, "family"); code != 405 {
		t.Errorf("POST → %d", code)
	}
	// Mehr lists it for a signed-in person on a box with the inbox
	if _, mehr, _ := familyPage(t, Mehr, cfg, "GET", mehrPath, "family"); !strings.Contains(mehr, "href="+NachrichtenPath) {
		t.Error("Mehr does not lead to Nachrichten sichern")
	}
}

func TestNachrichtenSichernWithoutTheArchiv(t *testing.T) {
	code, body, _ := familyPage(t, Nachrichten, StartConfig{}, "GET", NachrichtenPath, "family")
	if code != 200 || !strings.Contains(body, "Das Nachrichten-Archiv ist auf dieser Box nicht eingerichtet.") || strings.Contains(body, "Dein Stand") {
		t.Fatalf("%d %s", code, body)
	}
	if _, admin, _ := familyPage(t, Nachrichten, StartConfig{}, "GET", NachrichtenPath, "admins"); !strings.Contains(admin, "HAUS_ARCHIV_DIR") {
		t.Error("an admin hears what is missing")
	}
	if _, mehr, _ := familyPage(t, Mehr, StartConfig{}, "GET", mehrPath, "family"); strings.Contains(mehr, NachrichtenPath) {
		t.Error("Mehr lists a page the box does not have")
	}
	// a stand that cannot be read is one plain line
	cfg := StartConfig{Sicherung: napi.NewSicherung(t.TempDir(), "/dev/null/kein")}
	if _, body, _ := familyPage(t, Nachrichten, cfg, "GET", NachrichtenPath, "family"); !strings.Contains(body, saysStandFehlt) {
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
