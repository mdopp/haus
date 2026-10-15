package takeout

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/haus/internal/caldav"
	"github.com/mdopp/haus/internal/caldav/davfake"
	"github.com/mdopp/haus/internal/notes"
)

var bg = context.Background()

// fixture zips testdata/takeout (made-up data only) the way Google ships
// a big export: split over two archives, the calendars and contacts in
// one, the Keep notes in the other — and a note's attachment beside it.
func fixture(t *testing.T) []string {
	t.Helper()
	dir := t.TempDir()
	one, two := filepath.Join(dir, "takeout-20261015T100000Z-001.zip"), filepath.Join(dir, "takeout-20261015T100000Z-002.zip")
	zips := map[string]*zip.Writer{}
	files := map[string]*os.File{}
	for _, p := range []string{one, two} {
		f, err := os.Create(p)
		if err != nil {
			t.Fatal(err)
		}
		files[p], zips[p] = f, zip.NewWriter(f)
	}
	root := filepath.Join("testdata", "takeout")
	err := filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		to := one
		if strings.Contains(rel, "Notizen") {
			to = two
		}
		w, err := zips[to].Create(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		_, err = w.Write(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	for p, z := range zips {
		if err := z.Close(); err != nil {
			t.Fatal(err)
		}
		files[p].Close()
	}
	return []string{one, two}
}

// a box: Haus's Radicale account (no Haushalt calendar or address book
// yet) and the notes folder
func box(t *testing.T) (*davfake.Server, Target, string) {
	t.Helper()
	dav := davfake.New("haus", "pw")
	t.Cleanup(dav.Close)
	dir := t.TempDir()
	d, err := notes.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	return dav, Target{Radicale: &caldav.Client{URL: dav.URL, User: "haus", Password: "pw", Loc: loc}, Notes: d, Loc: loc}, dir
}

func objects(dav *davfake.Server, ext string) map[string]string {
	out := map[string]string{}
	for p, v := range dav.Objects() {
		if strings.HasSuffix(p, ext) {
			out[p] = v
		}
	}
	return out
}

func withText(all map[string]string, text string) []string {
	var out []string
	for p, v := range all {
		if strings.Contains(v, text) {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

func files(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
		if err == nil && !e.IsDir() {
			rel, _ := filepath.Rel(dir, p)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// haus#13, laib ADR 0017 §4: a Takeout archive goes where the family's
// things live. The calendars' entries into the Haushalt calendar (made on
// the first write), one object per UID — a series with its moved date
// together — each with the time zones it names; an entry without a UID
// gets one. The contacts into the Haushalt address book, once each though
// Takeout lists a person in several address books; one without a name
// named by its number, one with nothing to call it by left out. The Keep
// notes as Markdown into the notes folder's "Google Notizen", their
// attachments beside them; trashed notes and Takeout's other files not.
func TestImportWritesEverythingWhereItBelongs(t *testing.T) {
	dav, target, dir := box(t)
	var log bytes.Buffer
	rep, err := Import(bg, fixture(t), target, &log)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(rep.Calendars, ",") != "Familie,Feiertage in Deutschland" || rep.Events != (Count{New: 5}) {
		t.Fatalf("calendars %v, events %+v", rep.Calendars, rep.Events)
	}
	if rep.Contacts != (Count{New: 3, There: 1, Failed: 1}) {
		t.Fatalf("contacts %+v\n%s", rep.Contacts, log.String())
	}
	if rep.Notes != (Count{New: 3}) || rep.Attachments != (Count{New: 1}) {
		t.Fatalf("notes %+v, attachments %+v", rep.Notes, rep.Attachments)
	}

	// the calendar Haus writes to, made because it was missing
	if cals := dav.Calendars(); len(cals) != 1 || dav.DisplayName(cals[0]) != "Haushalt" {
		t.Fatalf("calendars: %v", cals)
	}
	ics := objects(dav, ".ics")
	if len(ics) != 5 {
		t.Fatalf("%d objects: %v", len(ics), ics)
	}
	zahnarzt := ics["/haus/haushalt/zahnarzt-lena-1@google.com.ics"]
	for _, want := range []string{"BEGIN:VCALENDAR\r\n", "BEGIN:VTIMEZONE\r\nTZID:Europe/Berlin\r\n", "SUMMARY:Zahnarzt Lena\r\n", "LOCATION:Praxis am Markt\\, Musterstadt\r\n",
		"BEGIN:VALARM\r\n", "END:VEVENT\r\nEND:VCALENDAR\r\n"} {
		if !strings.Contains(zahnarzt, want) {
			t.Fatalf("Zahnarzt lacks %q:\n%s", want, zahnarzt)
		}
	}
	if strings.Contains(zahnarzt, "America/New_York") || strings.Contains(zahnarzt, "X-WR-CALNAME") {
		t.Fatalf("a time zone it does not name, or the export's own lines:\n%s", zahnarzt)
	}
	if root, err := caldav.Parse(zahnarzt); err != nil || len(caldav.Events(root, time.UTC)) != 1 {
		t.Fatalf("not one readable appointment: %v", err)
	}
	// the series and its moved date: one object
	muell := ics["/haus/haushalt/muellabfuhr-1@google.com.ics"]
	if strings.Count(muell, "BEGIN:VEVENT") != 2 || !strings.Contains(muell, "RECURRENCE-ID;TZID=Europe/Berlin:20260406T070000") {
		t.Fatalf("Müllabfuhr:\n%s", muell)
	}
	// folding kept as it came: the long description is still one property
	if oma := ics["/haus/haushalt/geburtstag-oma@google.com.ics"]; !strings.Contains(oma, "DESCRIPTION:Fotobuch mit den Enkeln besorgen\\, Kuchen backen und die ganze F\r\n amilie") {
		t.Fatalf("Oma:\n%s", oma)
	}
	// no UID: one made from what it is, written into the entry
	grill := withText(ics, "Grillfest im Garten")
	if len(grill) != 1 || !strings.HasPrefix(grill[0], "/haus/haushalt/takeout-") || !strings.Contains(ics[grill[0]], "UID:takeout-") {
		t.Fatalf("Grillfest: %v", grill)
	}

	// contacts: the Haushalt address book, made because it was missing
	if books := dav.AddressBooks(); len(books) != 1 || dav.DisplayName(books[0]) != "Haushalt" {
		t.Fatalf("address books: %v", books)
	}
	vcf := objects(dav, ".vcf")
	if len(vcf) != 3 || len(withText(vcf, "FN:Anna Beispiel")) != 1 || len(withText(vcf, "FN:Bernd Muster")) != 1 {
		t.Fatalf("cards: %v", vcf)
	}
	karl := withText(vcf, "TEL;TYPE=WORK:0170 0000003")
	if len(karl) != 1 || !strings.Contains(vcf[karl[0]], "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:takeout-") || !strings.Contains(vcf[karl[0]], "FN:Karl Klempner\r\n") {
		t.Fatalf("Karl:\n%v", vcf)
	}
	bernd := vcf[withText(vcf, "FN:Bernd Muster")[0]]
	if root, err := caldav.Parse(bernd); err != nil {
		t.Fatal(err)
	} else if cs := caldav.Contacts(root); len(cs) != 1 || cs[0].Emails[0] != "bernd@example.org" || cs[0].Phones[0] != "089 0000002" {
		t.Fatalf("Bernd reads as %+v", cs)
	}
	if len(withText(vcf, "nur eine Notiz")) != 0 || !strings.Contains(log.String(), "ohne Namen, E-Mail und Nummer") {
		t.Fatalf("the card with nothing to call it by: %v\n%s", vcf, log.String())
	}

	// Keep: three notes and the attachment, nothing else
	got := files(t, dir)
	want := []string{"Google Notizen/Anhänge/Pfannkuchen.jpg", "Google Notizen/einkaufsliste-", "Google Notizen/rezept-pfannkuchen-", "Google Notizen/wlan-fuer-gaeste-netz-gast-beispiel-"}
	if len(got) != len(want) {
		t.Fatalf("files: %v", got)
	}
	for i := range want {
		if !strings.HasPrefix(got[i], want[i]) {
			t.Fatalf("files: %v", got)
		}
	}
	liste := read(t, filepath.Join(dir, got[1]))
	if liste != "# Einkaufsliste\n\n- [ ] Milch\n- [x] Brot\n\nAus Google Notizen, angelegt am 15.06.2025, zuletzt geändert am 09.10.2025.\n" {
		t.Fatalf("Einkaufsliste: %q", liste)
	}
	rezept := read(t, filepath.Join(dir, got[2]))
	for _, w := range []string{"# Rezept Pfannkuchen\n\n250 g Mehl, 500 ml Milch, 3 Eier.\nAlles verrühren", "Anhang: Anhänge/Pfannkuchen.jpg\n",
		"Link: Grundrezept – https://example.org/pfannkuchen\n", "Labels: Rezepte. In Keep archiviert.\n"} {
		if !strings.Contains(rezept, w) {
			t.Fatalf("Rezept lacks %q:\n%s", w, rezept)
		}
	}
	if !strings.HasPrefix(read(t, filepath.Join(dir, got[3])), "# WLAN für Gäste: Netz Gast-Beispiel\n\nWLAN für Gäste") {
		t.Fatalf("a note without a title is titled by its first line")
	}
	// where the library finds them: the notes folder's own search reads them
	d, _ := notes.Open(dir)
	if res, err := d.Search(bg, "Pfannkuchen", 10); err != nil || res.Total != 1 {
		t.Fatalf("the notes search: %+v %v", res, err)
	}
}

// Running it twice adds nothing twice: every entry is there already and
// stays as it is — also one the family changed since on a phone or in the
// notes folder.
func TestImportTwiceWritesNothingTwice(t *testing.T) {
	dav, target, dir := box(t)
	zips := fixture(t)
	if _, err := Import(bg, zips, target, io.Discard); err != nil {
		t.Fatal(err)
	}
	// edited since: an appointment on a phone, a note in the folder
	dav.Put("/haus/haushalt/zahnarzt-lena-1@google.com.ics", "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:zahnarzt-lena-1@google.com\r\nSUMMARY:Zahnarzt Lena (verschoben)\r\n"+
		"DTSTART:20260311T080000Z\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")
	before := files(t, dir)
	note := filepath.Join(dir, before[1])
	os.WriteFile(note, []byte("# Einkaufsliste\n\n- [ ] Hafermilch\n"), 0o644)
	objs := dav.Objects()

	rep, err := Import(bg, zips, target, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Events != (Count{There: 5}) || rep.Contacts != (Count{There: 4, Failed: 1}) || rep.Notes != (Count{There: 3}) || rep.Attachments != (Count{There: 1}) {
		t.Fatalf("second run: %+v", rep)
	}
	if after := dav.Objects(); len(after) != len(objs) || after["/haus/haushalt/zahnarzt-lena-1@google.com.ics"] != objs["/haus/haushalt/zahnarzt-lena-1@google.com.ics"] {
		t.Fatalf("objects changed: %d → %d", len(objs), len(after))
	}
	if after := files(t, dir); strings.Join(after, ",") != strings.Join(before, ",") || read(t, note) != "# Einkaufsliste\n\n- [ ] Hafermilch\n" {
		t.Fatalf("files changed: %v → %v", before, after)
	}
	for _, h := range dav.Header {
		if h.Get("If-None-Match") != "*" {
			t.Fatalf("a PUT that may replace: %v", h)
		}
	}
}

// A dry run reads the archive and counts, and writes nothing anywhere;
// a calendar left out by name is not counted and said so.
func TestDryRunAndSkippedCalendars(t *testing.T) {
	dav, target, dir := box(t)
	target.DryRun = true
	target.SkipCalendars = []string{"feiertage in deutschland"}
	rep, err := Import(bg, fixture(t), target, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Events != (Count{New: 4}) || strings.Join(rep.Skipped, ",") != "Feiertage in Deutschland" || rep.Contacts.New != 4 || rep.Notes.New != 3 {
		t.Fatalf("%+v", rep)
	}
	if len(dav.Methods) != 0 || len(files(t, dir)) != 0 {
		t.Fatalf("a dry run wrote: %v %v", dav.Methods, files(t, dir))
	}
	// skipped for real too
	target.DryRun = false
	if rep, _ := Import(bg, fixture(t), target, io.Discard); rep.Events.New != 4 || len(withText(objects(dav, ".ics"), "Einheit")) != 0 {
		t.Fatalf("skipped: %+v", rep)
	}
}

// Radicale not answering, or refusing the account, ends the import with
// that error rather than a line per entry; what is not Haus's to set —
// no Radicale, no notes folder — is left out.
func TestImportStopsWhenRadicaleDoesNot(t *testing.T) {
	dav, target, _ := box(t)
	target.Radicale.Password = "falsch"
	if _, err := Import(bg, fixture(t), target, io.Discard); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("refused: %v", err)
	}
	dav.Close()
	target.Radicale.Password = "pw"
	if _, err := Import(bg, fixture(t), target, io.Discard); err == nil || !strings.Contains(err.Error(), "nicht erreichbar") {
		t.Fatalf("down: %v", err)
	}
	// only the notes
	target.Radicale = nil
	rep, err := Import(bg, fixture(t), target, io.Discard)
	if err != nil || rep.Notes.New != 3 || rep.Events != (Count{}) || rep.Contacts != (Count{}) {
		t.Fatalf("notes only: %+v %v", rep, err)
	}
}

// What is no Takeout archive is said, and nothing is written.
func TestNotAZip(t *testing.T) {
	_, target, dir := box(t)
	p := filepath.Join(t.TempDir(), "kein.zip")
	os.WriteFile(p, []byte("nur Text"), 0o644)
	if _, err := Import(bg, []string{p}, target, io.Discard); err == nil || !strings.Contains(err.Error(), "keine lesbare ZIP-Datei") {
		t.Fatalf("%v", err)
	}
	if len(files(t, dir)) != 0 {
		t.Fatal("wrote")
	}
}
