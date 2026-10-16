package takeout

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
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
	if rep.Events != (Count{New: 4}) || strings.Join(rep.Skipped, ",") != "Feiertage in Deutschland" || rep.Contacts != (Count{New: 3, There: 1, Failed: 1}) || rep.Notes.New != 3 {
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

// zipOf is one archive of these members, in name order.
func zipOf(t *testing.T, members map[string][]byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "takeout-001.zip")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	var names []string
	for n := range members {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		w, err := z.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		w.Write(members[n])
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return p
}

func keepJSON(title, text string, atts ...string) []byte {
	var a []string
	for _, x := range atts {
		a = append(a, `{"filePath":"`+x+`","mimetype":"image/jpeg"}`)
	}
	return []byte(`{"title":"` + title + `","textContent":"` + text + `","attachments":[` + strings.Join(a, ",") +
		`],"createdTimestampUsec":1750000000000000,"userEditedTimestampUsec":1750000000000000,"isTrashed":false}`)
}

// An attachment past its cap — a zip bomb's shape, a few KB packed and
// far more unpacked — is not unpacked whole and not written; the note is.
func TestAnAttachmentTooBigIsLeftOut(t *testing.T) {
	_, target, dir := box(t)
	target.Radicale = nil
	zp := zipOf(t, map[string][]byte{
		"Takeout/Notizen/Riesig.json": keepJSON("Riesig", "mit Bild", "riesig.jpg"),
		"Takeout/Notizen/riesig.jpg":  make([]byte, maxAttachment+1),
	})
	var log bytes.Buffer
	rep, err := Import(bg, []string{zp}, target, &log)
	if err != nil || rep.Notes != (Count{New: 1}) || rep.Attachments != (Count{Failed: 1}) || !strings.Contains(log.String(), "größer als 25 MB") {
		t.Fatalf("%+v %v\n%s", rep, err, log.String())
	}
	for _, f := range files(t, dir) {
		if strings.Contains(f, "riesig.jpg") {
			t.Fatalf("written: %v", files(t, dir))
		}
	}
}

// An attachment's name is only ever its file's name inside "Google
// Notizen/Anhänge": one that tries to step out, or is hidden, writes
// nothing elsewhere and does not stop the import. Of two files of that
// name, the one beside the note is the note's.
func TestAttachmentNamesStayInTheirFolder(t *testing.T) {
	_, target, dir := box(t)
	target.Radicale = nil
	zp := zipOf(t, map[string][]byte{
		"Takeout/Fotos/Bild.jpg":      []byte("ein anderes Bild"),
		"Takeout/Notizen/Bild.jpg":    []byte("das Bild der Notiz"),
		"Takeout/Notizen/.geheim.jpg": []byte("versteckt"),
		"Takeout/Notizen/passwd":      []byte("root:x"),
		"Takeout/Notizen/Notiz.json":  keepJSON("Notiz", "Text", ".geheim.jpg", "../../../../etc/passwd", "Bild.jpg"),
		"Takeout/Notizen/Zweite.json": keepJSON("Zweite", "danach"),
	})
	rep, err := Import(bg, []string{zp}, target, io.Discard)
	if err != nil || rep.Notes.New != 2 {
		t.Fatalf("%+v %v", rep, err)
	}
	for _, f := range files(t, dir) {
		if !strings.HasPrefix(f, "Google Notizen/") || strings.Contains(f, "/.") {
			t.Fatalf("written outside or hidden: %v", files(t, dir))
		}
	}
	if got := read(t, filepath.Join(dir, "Google Notizen", "Anhänge", "Bild.jpg")); got != "das Bild der Notiz" {
		t.Fatalf("Bild.jpg: %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "Google Notizen", "Anhänge", "passwd")); err != nil {
		t.Fatalf("the step out is the file's own name or nothing: %v", err)
	}
}

// Radicale refusing the account (403), sending elsewhere (302), or a
// Haushalt calendar that takes no appointments stops the import with that
// error, also when the archive holds calendars only — never a line per
// entry and a success.
func TestImportStopsOnWhatEveryEntryWouldHit(t *testing.T) {
	ics := zipOf(t, map[string][]byte{"Takeout/Kalender/Familie.ics": []byte("BEGIN:VCALENDAR\r\nVERSION:2.0\r\n" +
		"BEGIN:VEVENT\r\nUID:a@google.com\r\nDTSTART:20260101T100000Z\r\nSUMMARY:A\r\nEND:VEVENT\r\n" +
		"BEGIN:VEVENT\r\nUID:b@google.com\r\nDTSTART:20260102T100000Z\r\nSUMMARY:B\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")})
	for _, code := range []int{http.StatusForbidden, http.StatusFound} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if code == http.StatusFound {
				w.Header().Set("Location", "http://example.invalid/")
			}
			w.WriteHeader(code)
		}))
		_, target, _ := box(t)
		target.Radicale.URL = srv.URL
		var log bytes.Buffer
		if _, err := Import(bg, []string{ics}, target, &log); err == nil || strings.Contains(log.String(), "Termin") {
			t.Fatalf("%d: %v\n%s", code, err, log.String())
		}
		srv.Close()
	}
	dav, target, _ := box(t)
	dav.AddCalendar("/haus/haushalt/", "Haushalt", "VTODO")
	if _, err := Import(bg, []string{ics}, target, io.Discard); err == nil || !strings.Contains(err.Error(), "nimmt kein") {
		t.Fatalf("a calendar that takes no appointments: %v", err)
	}
}

// One object carries each time zone it names once, though it names it on
// DTSTART and DTEND.
func TestObjectNamesEachZoneOnce(t *testing.T) {
	dav, target, _ := box(t)
	if _, err := Import(bg, fixture(t), target, io.Discard); err != nil {
		t.Fatal(err)
	}
	z := objects(dav, ".ics")["/haus/haushalt/zahnarzt-lena-1@google.com.ics"]
	if !strings.Contains(z, "DTEND;TZID=Europe/Berlin") || strings.Count(z, "BEGIN:VTIMEZONE") != 1 {
		t.Fatalf("%s", z)
	}
}

// What an entry without a UID is named by is its own: its summary and its
// times, not a reminder's inside it; folded lines are read unfolded. Two
// such entries on different days are two.
func TestEntriesAreKnownByThemselves(t *testing.T) {
	dav, target, _ := box(t)
	alarm := "BEGIN:VALARM\r\nACTION:EMAIL\r\nSUMMARY:Erinnerung\r\nTRIGGER:-PT1H\r\nEND:VALARM\r\n"
	zp := zipOf(t, map[string][]byte{"Takeout/Kalender/Familie.ics": []byte("BEGIN:VCALENDAR\r\nVERSION:2.0\r\n" +
		"BEGIN:VEVENT\r\n" + alarm + "SUMMARY:Turnen\r\nDTSTART:20260105T160000Z\r\nEND:VEVENT\r\n" +
		"BEGIN:VEVENT\r\n" + alarm + "SUMMARY:Schwimmen\r\nDTSTART:20260105T160000Z\r\nEND:VEVENT\r\n" +
		"BEGIN:VEVENT\r\nSUMMARY:Turnen\r\nDTSTART:20260112T160000Z\r\nEND:VEVENT\r\n" +
		"BEGIN:VEVENT\r\nUID:sehr-lange-kennung-die-google-\r\n umbricht@google.com\r\nSUMMARY:Gefaltet\r\nDTSTART:20260201T100000Z\r\nEND:VEVENT\r\n" +
		"END:VCALENDAR\r\n")})
	rep, err := Import(bg, []string{zp}, target, io.Discard)
	if err != nil || rep.Events != (Count{New: 4}) {
		t.Fatalf("%+v %v", rep, err)
	}
	if _, ok := objects(dav, ".ics")["/haus/haushalt/sehr-lange-kennung-die-google-umbricht@google.com.ics"]; !ok {
		t.Fatalf("folded UID: %v", dav.Objects())
	}
}

// A contact is known by its name and its first email address in order,
// however Google listed them: the same person twice, addresses in another
// order, is one card; two people of one name with their own addresses
// are two.
func TestContactsAreKnownByNameAndAddress(t *testing.T) {
	dav, target, _ := box(t)
	card := func(fn string, mails ...string) string {
		s := "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:" + fn + "\r\n"
		for _, m := range mails {
			s += "EMAIL;TYPE=INTERNET:" + m + "\r\n"
		}
		return s + "END:VCARD\r\n"
	}
	zp := zipOf(t, map[string][]byte{
		"Takeout/Kontakte/Alle Kontakte/Alle Kontakte.vcf":   []byte(card("Anna Beispiel", "b@example.org", "a@example.org") + card("Anna Beispiel", "anna2@example.net")),
		"Takeout/Kontakte/Meine Kontakte/Meine Kontakte.vcf": []byte(card("Anna Beispiel", "a@example.org", "b@example.org")),
	})
	rep, err := Import(bg, []string{zp}, target, io.Discard)
	if err != nil || rep.Contacts != (Count{New: 2, There: 1}) || len(objects(dav, ".vcf")) != 2 {
		t.Fatalf("%+v %v %v", rep, err, objects(dav, ".vcf"))
	}
}

// A dry run says what a real run would: an entry the archive lists twice
// (a person in two of Takeout's address books) is counted once as new.
func TestDryRunCountsAsTheRunWould(t *testing.T) {
	_, target, _ := box(t)
	target.DryRun = true
	rep, err := Import(bg, fixture(t), target, io.Discard)
	if err != nil || rep.Contacts != (Count{New: 3, There: 1, Failed: 1}) || rep.Events != (Count{New: 5}) || rep.Notes != (Count{New: 3}) {
		t.Fatalf("%+v %v", rep, err)
	}
}
