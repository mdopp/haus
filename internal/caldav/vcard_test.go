package caldav

import (
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// A contact is a vCard 4.0 with a random urn:uuid UID; a newline or a
// semicolon in a value cannot add a property or end the card (RFC 6350
// §3.4 escaping), and what goes in reads back.
func TestVCardEscapingAndRoundTrip(t *testing.T) {
	hostile := "Eva;Maria, Dr.\r\nTEL:+49666\nEND:VCARD\nBEGIN:VCARD"
	ct := Contact{Name: hostile, Phones: []string{"+49 170 1234567"}, Emails: []string{"eva@example.org"}, Birthday: "1985-04-12"}
	vcf := ct.VCF(time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC))
	for _, want := range []string{"BEGIN:VCARD\r\nVERSION:4.0\r\n", `FN:Eva\;Maria\, Dr.\nTEL:+49666\nEND:VCARD\nBEGIN:VCARD` + "\r\n",
		"TEL:+49 170 1234567\r\n", "EMAIL:eva@example.org\r\n", "BDAY:19850412\r\n", "REV:20261014T120000Z\r\n"} {
		if !strings.Contains(vcf, want) {
			t.Fatalf("vCard lacks %q:\n%s", want, vcf)
		}
	}
	if strings.Count(vcf, "BEGIN:VCARD") != 1 || strings.Count(vcf, "\r\nEND:VCARD\r\n") != 1 || !strings.HasSuffix(vcf, "END:VCARD\r\n") {
		t.Fatalf("one card:\n%s", vcf)
	}
	for _, l := range strings.Split(strings.TrimSuffix(vcf, "\r\n"), "\r\n") {
		if len(l) > 75 || !utf8.ValidString(l) {
			t.Fatalf("folding: %q", l)
		}
	}
	root, err := Parse(vcf)
	if err != nil {
		t.Fatal(err)
	}
	cs := Contacts(root)
	if len(cs) != 1 {
		t.Fatalf("cards: %+v", cs)
	}
	got := cs[0]
	if got.Name != strings.ReplaceAll(hostile, "\r\n", "\n") || strings.Join(got.Phones, "|") != "+49 170 1234567" || got.Birthday != "1985-04-12" {
		t.Fatalf("read back: %+v", got)
	}
	if !regexp.MustCompile(`^urn:uuid:[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(got.UID) {
		t.Fatalf("UID: %q", got.UID)
	}
	if other := (Contact{Name: "x"}).VCF(time.Now()); strings.Contains(other, got.UID) {
		t.Fatal("UIDs are random")
	}
	if strings.Contains((Contact{Name: "x"}).VCF(time.Now()), "BDAY") {
		t.Fatal("no birthday, no BDAY")
	}
	if !strings.Contains((Contact{Name: "x", Birthday: "--02-29"}).VCF(time.Now()), "BDAY:--0229\r\n") {
		t.Fatal("a birthday without a year")
	}
}

// The birthday forms phones write, as JJJJ-MM-TT or --MM-TT; anything
// else is kept as text.
func TestBirthdayForms(t *testing.T) {
	for in, want := range map[string]string{
		"19850412": "1985-04-12", "1985-04-12": "1985-04-12", "1985-04-12T00:00:00Z": "1985-04-12",
		"19850412T000000": "1985-04-12", "--0412": "--04-12", "--04-12": "--04-12", "zu Ostern": "zu Ostern",
	} {
		root, err := Parse("BEGIN:VCARD\r\nFN:x\r\nBDAY:" + in + "\r\nEND:VCARD\r\n")
		if err != nil {
			t.Fatal(err)
		}
		if got := Contacts(root)[0].Birthday; got != want {
			t.Fatalf("%q: %q, want %q", in, got, want)
		}
	}
}
