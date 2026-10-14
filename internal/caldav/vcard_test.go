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
	if !strings.HasPrefix(vcf, "BEGIN:VCARD\r\n") || strings.Count(vcf, "\r\nBEGIN:VCARD\r\n") != 0 || strings.Count(vcf, "\r\nEND:VCARD\r\n") != 1 || !strings.HasSuffix(vcf, "\r\nEND:VCARD\r\n") {
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

// Every value is escaped, not only the name: a newline in a phone, an
// email, an organisation or a nickname cannot add a property.
func TestVCardEscapesEveryValue(t *testing.T) {
	ct := Contact{Name: "x", Phones: []string{"1\nEND:VCARD"}, Emails: []string{"a;b\nX-TEL:y"}, Org: "A\nORG2:c", Nickname: "n\nNOTE:z"}
	vcf := ct.VCF(time.Now())
	lines := strings.Split(strings.TrimSuffix(vcf, "\r\n"), "\r\n")
	for i, l := range lines {
		for _, bad := range []string{"END:VCARD", "X-TEL:", "ORG2:", "NOTE:"} {
			if strings.HasPrefix(l, bad) && !(bad == "END:VCARD" && i == len(lines)-1) {
				t.Fatalf("injected %q:\n%s", l, vcf)
			}
		}
	}
	root, err := Parse(vcf)
	if err != nil {
		t.Fatal(err)
	}
	got := Contacts(root)[0]
	if got.Phones[0] != "1\nEND:VCARD" || got.Emails[0] != "a;b\nX-TEL:y" || got.Nickname != "n\nNOTE:z" {
		t.Fatalf("read back: %+v", got)
	}
}

// iOS writes a birthday without a year as 1604 (with X-APPLE-OMIT-YEAR);
// that is --MM-TT, not a 422-year-old.
func TestAppleOmitYear(t *testing.T) {
	for _, line := range []string{"BDAY;X-APPLE-OMIT-YEAR=1604:1604-04-12", "BDAY;VALUE=date:1604-04-12", "BDAY:16040412"} {
		root, err := Parse("BEGIN:VCARD\r\nFN:x\r\n" + line + "\r\nEND:VCARD\r\n")
		if err != nil {
			t.Fatal(err)
		}
		if got := Contacts(root)[0].Birthday; got != "--04-12" {
			t.Fatalf("%q: %q", line, got)
		}
	}
}

// vCard 2.1 parameters without a name (TEL;CELL:) are TYPEs; the line is
// not dropped.
func TestBareParams(t *testing.T) {
	root, err := Parse("BEGIN:VCARD\r\nVERSION:2.1\r\nFN:Alt\r\nTEL;CELL:0170 1\r\nTEL;HOME;VOICE:0711 2\r\nEND:VCARD\r\n")
	if err != nil {
		t.Fatal(err)
	}
	ct := Contacts(root)[0]
	if strings.Join(ct.Phones, "|") != "0170 1|0711 2" {
		t.Fatalf("phones: %v", ct.Phones)
	}
	var types []string
	root.Walk(func(c *Component) {
		for _, p := range c.Props {
			if p.Name == "TEL" {
				types = append(types, p.Params["TYPE"])
			}
		}
	})
	if strings.Join(types, "|") != "CELL|HOME,VOICE" {
		t.Fatalf("types: %v", types)
	}
}

// A birthday in the year 0000 is no date: no BDAY is written.
func TestBirthdayYearZero(t *testing.T) {
	if ValidBirthday("0000-04-12") || !ValidBirthday("1985-04-12") || !ValidBirthday("--02-29") || ValidBirthday("--02-30") || ValidBirthday("12.04.") {
		t.Fatal("ValidBirthday")
	}
	if strings.Contains((Contact{Name: "x", Birthday: "0000-04-12"}).VCF(time.Now()), "BDAY") {
		t.Fatal("year 0000 written")
	}
}
