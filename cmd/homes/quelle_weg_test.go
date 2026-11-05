package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mdopp/homes/internal/caldav"
	"github.com/mdopp/homes/internal/caldav/davfake"
)

// the cards of the household's address book before the run
var quelleCards = map[string]string{
	// a firm card kontakt_ergaenzen wrote: two links among the family's lines, a photo
	"stadtwerke.vcf": "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:stadtwerke\r\nFN:Beispiel Stadtwerke\r\nORG:Beispiel Stadtwerke\r\nCATEGORIES:Firma\r\n" +
		"TEL:030 1234567\r\nPHOTO;ENCODING=b;TYPE=JPEG:QUJD\r\nX-IBAN:DE89370400440532013000\r\n" +
		"NOTE:Ansprechpartner: Frau Muster\\nIBAN: DE89 3704 0044 0532 0130 00\\nQuelle: https://paperless.example/documents/139/details\\n" +
		"Kundennummer 4711\\nQuelle: https://paperless.example/documents/140/details\\nQuelle: Empfehlung vom Nachbarn\r\nEND:VCARD\r\n",
	// a firm card with nothing but a link
	"nur.vcf": "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:nur\r\nFN:Nur Link AG\r\nORG:Nur Link AG\r\nCATEGORIES:Firma\r\n" +
		"NOTE:Quelle: https://paperless.example/documents/7/\r\nEND:VCARD\r\n",
	// a card the family made: never touched, whatever its note says
	"paul.vcf": "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:paul\r\nFN:Paul Plan\r\n" +
		"NOTE:Quelle: https://paperless.example/documents/9/details\r\nEND:VCARD\r\n",
	// a firm card without a link
	"bau.vcf": "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:bau\r\nFN:Bau GmbH\r\nORG:Bau GmbH\r\nCATEGORIES:Firma\r\n" +
		"NOTE:Ansprechpartner: Herr Beispiel\r\nEND:VCARD\r\n",
}

func quelleSetup(t *testing.T) (*davfake.Server, *caldav.Client) {
	t.Helper()
	dav := davfake.New("homes", "pw")
	t.Cleanup(dav.Close)
	dav.AddAddressBook("/homes/haushalt-kontakte/", "Haushalt")
	for name, card := range quelleCards {
		dav.Put("/homes/haushalt-kontakte/"+name, card)
	}
	return dav, &caldav.Client{URL: dav.URL, User: "homes", Password: "pw"}
}

// withoutNoteRev is a card's lines but its NOTE and REV: what the run
// must keep as it was.
func withoutNoteRev(card string) string {
	var keep []string
	for _, l := range strings.Split(card, "\r\n") {
		if !strings.HasPrefix(l, "NOTE:") && !strings.HasPrefix(l, "REV:") {
			keep = append(keep, l)
		}
	}
	return strings.Join(keep, "\r\n")
}

// The run takes only the „Quelle: <document link>“ lines out of the firm
// cards, keeps every other line, copies each changed card as it was into
// HOMES_DATA first, and marks itself done; a second run changes nothing
// and keeps the first copies.
func TestQuellenWeg(t *testing.T) {
	ctx := context.Background()
	dav, c := quelleSetup(t)
	data := t.TempDir()
	changed, left, done, err := quellenWeg(ctx, c, data)
	if err != nil || changed != 2 || left != 0 || done {
		t.Fatalf("changed %d, left %d, done %v, %v", changed, left, done, err)
	}
	objs := dav.Objects()
	at := func(name string) string {
		return strings.ReplaceAll(objs["/homes/haushalt-kontakte/"+name], "\r\n ", "")
	}

	sw := at("stadtwerke.vcf")
	if !strings.Contains(sw, "NOTE:Ansprechpartner: Frau Muster\\nIBAN: DE89 3704 0044 0532 0130 00\\nKundennummer 4711\\nQuelle: Empfehlung vom Nachbarn\r\n") || strings.Contains(sw, "paperless") {
		t.Fatalf("stadtwerke:\n%s", sw)
	}
	if withoutNoteRev(sw) != withoutNoteRev(quelleCards["stadtwerke.vcf"]) {
		t.Fatalf("another line changed:\n%s\nwas\n%s", sw, quelleCards["stadtwerke.vcf"])
	}
	if nur := at("nur.vcf"); strings.Contains(nur, "NOTE") || withoutNoteRev(nur) != withoutNoteRev(quelleCards["nur.vcf"]) {
		t.Fatalf("nur:\n%s", nur)
	}
	for _, same := range []string{"paul.vcf", "bau.vcf"} {
		if at(same) != quelleCards[same] {
			t.Fatalf("%s written:\n%s", same, at(same))
		}
	}

	// each changed card as it was, and only those
	dir := filepath.Join(data, quelleDir)
	for uid, name := range map[string]string{"stadtwerke": "stadtwerke.vcf", "nur": "nur.vcf"} {
		b, err := os.ReadFile(filepath.Join(dir, uid+".vcf"))
		if err != nil || string(b) != quelleCards[name] {
			t.Fatalf("copy of %s: %q, %v", uid, b, err)
		}
		if fi, _ := os.Stat(filepath.Join(dir, uid+".vcf")); fi.Mode().Perm() != 0o600 {
			t.Fatalf("copy of %s: mode %v", uid, fi.Mode())
		}
	}
	for _, none := range []string{"paul.vcf", "bau.vcf"} {
		if _, err := os.Stat(filepath.Join(dir, none)); err == nil {
			t.Fatalf("a copy of the unchanged %s", none)
		}
	}

	// once: the marker ends it
	before := dav.Objects()
	if changed, _, done, err = quellenWeg(ctx, c, data); err != nil || changed != 0 || !done {
		t.Fatalf("second run: changed %d, done %v, %v", changed, done, err)
	}
	// and without the marker it finds nothing more and keeps the first copies
	os.Remove(filepath.Join(dir, "fertig"))
	if changed, _, _, err = quellenWeg(ctx, c, data); err != nil || changed != 0 {
		t.Fatalf("run without the marker: changed %d, %v", changed, err)
	}
	for p, v := range dav.Objects() {
		if before[p] != v {
			t.Fatalf("%s written again", p)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "stadtwerke.vcf")); string(b) != quelleCards["stadtwerke.vcf"] {
		t.Fatalf("the first copy was written over: %q", b)
	}

	// the copy puts the card back as it was
	dav.Put("/homes/haushalt-kontakte/stadtwerke.vcf", mustRead(t, filepath.Join(dir, "stadtwerke.vcf")))
	if got := dav.Objects()["/homes/haushalt-kontakte/stadtwerke.vcf"]; got != quelleCards["stadtwerke.vcf"] {
		t.Fatalf("restored:\n%s", got)
	}
}

// A Radicale that does not answer leaves no marker: the next start tries again.
func TestQuellenWegFailsWithoutMarker(t *testing.T) {
	dav, c := quelleSetup(t)
	dav.Close()
	data := t.TempDir()
	if _, _, _, err := quellenWeg(context.Background(), c, data); err == nil {
		t.Fatal("no error from a Radicale that is gone")
	}
	if _, err := os.Stat(filepath.Join(data, quelleDir, "fertig")); err == nil {
		t.Fatal("marked done after a failure")
	}
}

func mustRead(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
