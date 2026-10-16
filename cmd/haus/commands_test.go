package main

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mdopp/haus/internal/caldav/davfake"
	"github.com/mdopp/haus/internal/paperless/paperlessfake"
)

// haus#13: `haus export-documents` is the refresh timer's first step. It
// mirrors Paperless into HAUS_DOCUMENTS_DIR and says what it did; unset,
// it does nothing and succeeds. Paperless in trouble does not fail the
// refresh (the notes are refreshed all the same), a folder Haus cannot
// write does; neither says the token.
func TestExportDocumentsCommand(t *testing.T) {
	fake := paperlessfake.New("pl-geheim")
	defer fake.Close()
	fake.Add(paperlessfake.Doc{ID: 7, Title: "Stromvertrag", Content: "Laufzeit bis 2027", Modified: "2026-10-01T12:00:00Z"})
	dir := t.TempDir()
	env := map[string]string{"HAUS_DOCUMENTS_DIR": dir, "HAUS_PAPERLESS_URL": fake.URL, "HAUS_PAPERLESS_TOKEN": "pl-geheim"}
	run := func() (int, string) {
		var out, errw bytes.Buffer
		code := command(context.Background(), []string{"export-documents"}, func(k string) string { return env[k] }, &out, &errw)
		return code, out.String() + errw.String()
	}
	if code, said := run(); code != 0 || !strings.Contains(said, "1 written, 0 unchanged, 0 removed") {
		t.Fatalf("%d %q", code, said)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "7.md")); err != nil || !strings.Contains(string(b), "Laufzeit bis 2027") {
		t.Fatalf("%q %v", b, err)
	}
	fake.Status = 500
	if code, said := run(); code != 0 || !strings.Contains(said, "stay as they were") || strings.Contains(said, "pl-geheim") {
		t.Fatalf("Paperless failing: %d %q", code, said)
	}
	fake.Status = 0
	env["HAUS_DOCUMENTS_DIR"] = filepath.Join(dir, "gibtsnicht")
	if code, said := run(); code != 1 || strings.Contains(said, "pl-geheim") {
		t.Fatalf("folder failing: %d %q", code, said)
	}
	env = map[string]string{}
	if code, said := run(); code != 0 || !strings.Contains(said, "nothing to export") {
		t.Fatalf("unset: %d %q", code, said)
	}
	var errw bytes.Buffer
	if code := command(context.Background(), []string{"bake-cake"}, func(string) string { return "" }, &errw, &errw); code != 2 {
		t.Fatalf("unknown: %d", code)
	}
}

// haus#13: `haus import-takeout` writes with Haus's own Radicale account
// and notes folder and says in German what it did; a dry run writes
// nothing; without an archive it says how to call it.
func TestImportTakeoutCommand(t *testing.T) {
	dav := davfake.New("haus", "pw")
	defer dav.Close()
	dir := t.TempDir()
	zp := filepath.Join(t.TempDir(), "takeout-001.zip")
	f, _ := os.Create(zp)
	z := zip.NewWriter(f)
	w, _ := z.Create("Takeout/Kalender/Familie.ics")
	w.Write([]byte("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nX-WR-CALNAME:Familie\r\nBEGIN:VEVENT\r\nUID:a@google.com\r\nDTSTART:20260101T100000Z\r\nSUMMARY:Neujahrsspaziergang\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"))
	w, _ = z.Create("Takeout/Notizen/Idee.json")
	w.Write([]byte(`{"title":"Idee","textContent":"Baumhaus bauen","createdTimestampUsec":1750000000000000,"userEditedTimestampUsec":1750000000000000,"isTrashed":false}`))
	z.Close()
	f.Close()
	env := map[string]string{"HAUS_RADICALE_URL": dav.URL, "HAUS_RADICALE_USER": "haus", "HAUS_RADICALE_PASSWORD": "pw", "HAUS_NOTES_DIR": dir, "TZ": "Europe/Berlin"}
	run := func(args ...string) (int, string) {
		var out, errw bytes.Buffer
		code := command(context.Background(), append([]string{"import-takeout"}, args...), func(k string) string { return env[k] }, &out, &errw)
		return code, out.String() + errw.String()
	}
	code, said := run("-dry-run", zp)
	if code != 0 || !strings.Contains(said, "Probelauf") || !strings.Contains(said, "Kalender (Familie): 1 Termin neu") || len(dav.Methods) != 0 {
		t.Fatalf("dry run: %d %q %v", code, said, dav.Methods)
	}
	code, said = run(zp)
	if code != 0 || !strings.Contains(said, "Kalender (Familie): 1 Termin neu") || !strings.Contains(said, "Google Notizen (Ordner „Google Notizen“): 1 Notiz neu") {
		t.Fatalf("import: %d %q", code, said)
	}
	code, said = run(zp)
	if code != 0 || !strings.Contains(said, "0 Termine neu, 1 schon da") || !strings.Contains(said, "0 Notizen neu, 1 schon da") {
		t.Fatalf("again: %d %q", code, said)
	}
	if code, said := run(); code != 2 || !strings.Contains(said, "Aufruf: haus import-takeout") {
		t.Fatalf("no archive: %d %q", code, said)
	}
	// Radicale refusing: stopped, said, exit 1, the password never
	env["HAUS_RADICALE_PASSWORD"] = "falsch-geheim"
	if code, said := run(zp); code != 1 || !strings.Contains(said, "Abgebrochen") || strings.Contains(said, "falsch-geheim") {
		t.Fatalf("refused: %d %q", code, said)
	}
	// no Radicale: the notes only, and said so
	env = map[string]string{"HAUS_NOTES_DIR": dir}
	if code, said := run(zp); code != 0 || !strings.Contains(said, "Kein Radicale eingerichtet") {
		t.Fatalf("notes only: %d %q", code, said)
	}
}

// A -dry-run put after the archive is no archive: the command stops before
// it writes anything, rather than taking it as a file and importing. Only
// a contact left out is reason enough for exit 1; a Keep note's day is
// the box's (TZ), not UTC's.
func TestImportTakeoutEdges(t *testing.T) {
	dav := davfake.New("haus", "pw")
	defer dav.Close()
	dir := t.TempDir()
	zp := filepath.Join(t.TempDir(), "takeout-001.zip")
	f, _ := os.Create(zp)
	z := zip.NewWriter(f)
	w, _ := z.Create("Takeout/Kalender/Familie.ics")
	w.Write([]byte("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:a@google.com\r\nDTSTART:20260101T100000Z\r\nSUMMARY:Spaziergang\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"))
	w, _ = z.Create("Takeout/Kontakte/Alle Kontakte/Alle Kontakte.vcf")
	w.Write([]byte("BEGIN:VCARD\r\nVERSION:3.0\r\nNOTE:nichts\r\nEND:VCARD\r\n"))
	w, _ = z.Create("Takeout/Notizen/Spaet.json")
	// 2025-06-15 22:30 UTC: the 16th in Berlin
	w.Write([]byte(`{"title":"Spät","textContent":"Licht aus","createdTimestampUsec":1750026600000000,"userEditedTimestampUsec":1750026600000000,"isTrashed":false}`))
	z.Close()
	f.Close()
	env := map[string]string{"HAUS_RADICALE_URL": dav.URL, "HAUS_RADICALE_USER": "haus", "HAUS_RADICALE_PASSWORD": "pw", "HAUS_NOTES_DIR": dir, "TZ": "Europe/Berlin"}
	run := func(args ...string) (int, string) {
		var out, errw bytes.Buffer
		code := command(context.Background(), append([]string{"import-takeout"}, args...), func(k string) string { return env[k] }, &out, &errw)
		return code, out.String() + errw.String()
	}
	if code, said := run(zp, "-dry-run"); code == 0 || len(dav.Methods) != 0 {
		t.Fatalf("a misplaced -dry-run: %d %q %v", code, said, dav.Methods)
	}
	if es, _ := os.ReadDir(dir); len(es) != 0 {
		t.Fatalf("a misplaced -dry-run wrote %v", es)
	}
	code, said := run(zp)
	if code != 1 || !strings.Contains(said, "1 Termin neu") || !strings.Contains(said, "1 nicht übernommen") {
		t.Fatalf("a contact left out: %d %q", code, said)
	}
	es, _ := os.ReadDir(filepath.Join(dir, "Google Notizen"))
	if len(es) != 1 {
		t.Fatalf("notes: %v", es)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "Google Notizen", es[0].Name()))
	if !strings.Contains(string(b), "angelegt am 16.06.2025") {
		t.Fatalf("the note's day: %q", b)
	}
}
