package haushalt

import (
	"encoding/json"
	"strings"
	"testing"
)

// the invented firm and the standard example IBANs (valid check digits)
const (
	ibanDE  = "DE89 3704 0044 0532 0130 00"
	ibanGB  = "GB82 WEST 1234 5698 7654 32"
	quelle7 = "https://paperless.example/documents/7/"
)

type ergaenzt struct {
	Status  string            `json:"status"`
	Kontakt string            `json:"kontakt"`
	Felder  map[string]string `json:"felder"`
	Gruende map[string]string `json:"gruende"`
}

// ergaenzen calls kontakt_ergaenzen and reads its answer: German lines,
// then one line of JSON.
func ergaenzen(t *testing.T, s setup, args map[string]any) (string, ergaenzt) {
	t.Helper()
	text, isErr := call(t, s.h, "kontakt_ergaenzen", args)
	if isErr {
		t.Fatalf("kontakt_ergaenzen %v: %q", args, text)
	}
	i := strings.LastIndex(text, "\n")
	if i < 0 {
		t.Fatalf("no JSON line: %q", text)
	}
	var e ergaenzt
	if err := json.Unmarshal([]byte(text[i+1:]), &e); err != nil {
		t.Fatalf("JSON line %q: %v", text[i+1:], err)
	}
	return text[:i], e
}

func onlyCard(t *testing.T, s setup) (string, string) {
	t.Helper()
	objs := s.fake.Objects()
	if len(objs) != 1 {
		t.Fatalf("want one card, have %d: %v", len(objs), objs)
	}
	for p, v := range objs {
		return p, v
	}
	return "", ""
}

func felder(t *testing.T, e ergaenzt, want map[string]string) {
	t.Helper()
	if len(e.Felder) != len(want) {
		t.Fatalf("felder %v, want %v", e.Felder, want)
	}
	for k, v := range want {
		if e.Felder[k] != v {
			t.Fatalf("%s: %q, want %q (%v, %v)", k, e.Felder[k], v, e.Felder, e.Gruende)
		}
	}
}

// No card matches: one is made, a firm's (ORG, FN, CATEGORIES Firma), with
// everything given; the IBAN without spaces as X-IBAN and grouped in the note.
func TestKontaktErgaenzenCreates(t *testing.T) {
	s := newSetup(t, want{contacts: true})
	line, e := ergaenzen(t, s, map[string]any{"firma": "Beispiel Stadtwerke GmbH", "ansprechpartner": "Frau Erika Muster",
		"telefon": "+49 30 1234567", "email": "service@stadtwerke.example", "iban": "de89370400440532013000", "quelle": quelle7})
	if e.Status != "angelegt" || !strings.HasPrefix(e.Kontakt, "urn:uuid:") || !strings.Contains(line, "Beispiel Stadtwerke GmbH") || !strings.Contains(line, "angelegt") {
		t.Fatalf("%q %+v", line, e)
	}
	felder(t, e, map[string]string{"firma": "angelegt", "ansprechpartner": "angelegt", "telefon": "angelegt", "email": "angelegt", "iban": "angelegt"})
	p, vcf := onlyCard(t, s)
	if !strings.HasPrefix(p, "/bote/kontakte/") {
		t.Fatalf("stray card %s", p)
	}
	for _, want := range []string{"FN:Beispiel Stadtwerke GmbH\r\n", "ORG:Beispiel Stadtwerke GmbH\r\n", "CATEGORIES:Firma\r\n", "TEL:+49 30 1234567\r\n",
		"EMAIL:service@stadtwerke.example\r\n", "X-IBAN:DE89370400440532013000\r\n", "UID:" + e.Kontakt + "\r\n",
		"Ansprechpartner: Frau Erika Muster", "IBAN: " + ibanDE, "Quelle: " + quelle7} {
		if !strings.Contains(strings.ReplaceAll(vcf, "\r\n ", ""), want) {
			t.Fatalf("card lacks %q:\n%s", want, vcf)
		}
	}
	// only the firm: nothing else is reported or written
	s2 := newSetup(t, want{contacts: true})
	_, e = ergaenzen(t, s2, map[string]any{"firma": "Nur Name AG"})
	felder(t, e, map[string]string{"firma": "angelegt"})
	if _, vcf := onlyCard(t, s2); strings.Contains(vcf, "NOTE") || strings.Contains(vcf, "X-IBAN") {
		t.Fatalf("an empty note or IBAN: %s", vcf)
	}
}

// A firm's card Haus made is filled where empty and gets a second phone or
// email; a value there is never changed, and a different IBAN is never
// written but answered abweichend.
func TestKontaktErgaenzenFills(t *testing.T) {
	s := newSetup(t, want{contacts: true})
	_, made := ergaenzen(t, s, map[string]any{"firma": "Beispiel Stadtwerke GmbH", "telefon": "030 1234567", "iban": ibanDE})
	line, e := ergaenzen(t, s, map[string]any{"firma": "  beispiel   STADTWERKE gmbh ", "ansprechpartner": "Herr Max Beispiel",
		"telefon": "+49 30 7654321", "email": "rechnung@stadtwerke.example", "iban": ibanGB, "quelle": quelle7})
	if e.Status != "ergaenzt" || e.Kontakt != made.Kontakt {
		t.Fatalf("%q %+v", line, e)
	}
	felder(t, e, map[string]string{"firma": "vorhanden", "ansprechpartner": "ergaenzt", "telefon": "ergaenzt", "email": "ergaenzt", "iban": "abweichend"})
	if !strings.Contains(line, "prüfen") || strings.Count(line, ibanDE) != 1 || !strings.Contains(line, ibanGB) {
		t.Fatalf("the different IBAN is said: %q %v", line, e.Gruende)
	}
	_, vcf := onlyCard(t, s)
	vcf = strings.ReplaceAll(vcf, "\r\n ", "")
	for _, want := range []string{"TEL:030 1234567\r\n", "TEL:+49 30 7654321\r\n", "EMAIL:rechnung@stadtwerke.example\r\n", "X-IBAN:DE89370400440532013000\r\n",
		"Ansprechpartner: Herr Max Beispiel", "Quelle: " + quelle7} {
		if !strings.Contains(vcf, want) {
			t.Fatalf("card lacks %q:\n%s", want, vcf)
		}
	}
	if strings.Contains(vcf, "GB82") || strings.Contains(vcf, "WEST") {
		t.Fatalf("the different IBAN was written:\n%s", vcf)
	}
	// the same again: all there, nothing written
	before := vcf
	_, e = ergaenzen(t, s, map[string]any{"firma": "Beispiel Stadtwerke GmbH", "ansprechpartner": "Frau Andere",
		"telefon": "+49 (0)30 1234567", "email": "RECHNUNG@stadtwerke.example", "iban": ibanDE, "quelle": quelle7})
	if e.Status != "unveraendert" {
		t.Fatalf("%+v", e)
	}
	felder(t, e, map[string]string{"firma": "vorhanden", "ansprechpartner": "vorhanden", "telefon": "vorhanden", "email": "vorhanden", "iban": "vorhanden"})
	if _, vcf := onlyCard(t, s); strings.ReplaceAll(vcf, "\r\n ", "") != before {
		t.Fatalf("a card with nothing new was written:\n%s", vcf)
	}
}

// A firm's card is found by its email, else its IBAN, when the name in the
// document is another.
func TestKontaktErgaenzenMatchesByEmailThenIBAN(t *testing.T) {
	s := newSetup(t, want{contacts: true})
	_, made := ergaenzen(t, s, map[string]any{"firma": "Beispiel Versicherung AG", "email": "kunde@versicherung.example", "iban": ibanDE})
	_, e := ergaenzen(t, s, map[string]any{"firma": "BV Versicherungsgruppe", "email": "Kunde@Versicherung.example", "telefon": "0800 111222"})
	if e.Kontakt != made.Kontakt || e.Felder["telefon"] != "ergaenzt" || e.Felder["firma"] != "vorhanden" {
		t.Fatalf("by email: %+v", e)
	}
	_, e = ergaenzen(t, s, map[string]any{"firma": "BV Gruppe", "iban": "DE89370400440532013000", "email": "neu@versicherung.example"})
	if e.Kontakt != made.Kontakt || e.Felder["email"] != "ergaenzt" || e.Felder["iban"] != "vorhanden" {
		t.Fatalf("by IBAN: %+v", e)
	}
	if n := len(s.fake.Objects()); n != 1 {
		t.Fatalf("one card, have %d", n)
	}
}

// A card the family made (no CATEGORIES Firma) gets nothing added; a
// different IBAN in it, even only in its note, is still abweichend.
func TestKontaktErgaenzenLeavesFamilyCards(t *testing.T) {
	s := newSetup(t, want{contacts: true})
	card := "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:paul\r\nFN:Paul Plan\r\nORG:Plan Bau\r\nTEL:0170 5555555\r\nNOTE:Konto: GB82 WEST 1234 5698 7654 32\r\nEND:VCARD\r\n"
	s.fake.Put("/bote/kontakte/paul.vcf", card)
	line, e := ergaenzen(t, s, map[string]any{"firma": "Plan Bau", "ansprechpartner": "Paul Plan", "telefon": "0170 6666666",
		"email": "paul@planbau.example", "iban": ibanDE})
	if e.Status != "unveraendert" || e.Kontakt != "paul" {
		t.Fatalf("%q %+v", line, e)
	}
	felder(t, e, map[string]string{"firma": "vorhanden", "ansprechpartner": "abgelehnt", "telefon": "abgelehnt", "email": "abgelehnt", "iban": "abweichend"})
	if _, got := onlyCard(t, s); got != card {
		t.Fatalf("a family card was written:\n%s", got)
	}
	_, e = ergaenzen(t, s, map[string]any{"firma": "Plan Bau", "telefon": "+49 170 5555555"})
	if e.Felder["telefon"] != "vorhanden" {
		t.Fatalf("the same number: %+v", e)
	}
}

// An IBAN with a wrong check sum, a phone or email that is none, is
// refused for itself; the rest is still stored. A missing firm or a source
// that is no link is the caller's mistake.
func TestKontaktErgaenzenRefusesBadValues(t *testing.T) {
	s := newSetup(t, want{contacts: true})
	_, e := ergaenzen(t, s, map[string]any{"firma": "Beispiel Handwerk KG", "iban": "DE89 3704 0044 0532 0130 01",
		"telefon": "ruf an\nX-EVIL:1", "email": "keine-adresse", "ansprechpartner": "Frau Muster"})
	felder(t, e, map[string]string{"firma": "angelegt", "iban": "abgelehnt", "telefon": "abgelehnt", "email": "abgelehnt", "ansprechpartner": "angelegt"})
	if e.Gruende["iban"] == "" || e.Gruende["telefon"] == "" || e.Gruende["email"] == "" {
		t.Fatalf("reasons: %v", e.Gruende)
	}
	if _, vcf := onlyCard(t, s); strings.Contains(vcf, "IBAN") || strings.Contains(vcf, "EVIL") || strings.Contains(vcf, "TEL") {
		t.Fatalf("a refused value was written:\n%s", vcf)
	}
	for _, bad := range []map[string]any{{}, {"firma": "  "}, {"firma": strings.Repeat("x", MaxField+1)},
		{"firma": "X", "quelle": "javascript:alert(1)"}, {"firma": "X", "ansprechpartner": strings.Repeat("y", MaxField+1)}} {
		if text, isErr := call(t, s.h, "kontakt_ergaenzen", bad); !isErr {
			t.Fatalf("%v: want a tool error, got %q", bad, text)
		}
	}
}

// Two cards of the firm: Haus does not guess which, and writes nothing.
func TestKontaktErgaenzenAmbiguous(t *testing.T) {
	s := newSetup(t, want{contacts: true})
	for _, u := range []string{"a", "b"} {
		s.fake.Put("/bote/kontakte/"+u+".vcf", "BEGIN:VCARD\r\nVERSION:4.0\r\nUID:"+u+"\r\nFN:Beispiel GmbH\r\nORG:Beispiel GmbH\r\nCATEGORIES:Firma\r\nEND:VCARD\r\n")
	}
	before := s.fake.Objects()
	line, e := ergaenzen(t, s, map[string]any{"firma": "Beispiel GmbH", "telefon": "030 1"})
	if e.Status != "mehrdeutig" || e.Felder["telefon"] != "abgelehnt" || !strings.Contains(line, "Mehrere") {
		t.Fatalf("%q %+v", line, e)
	}
	for p, v := range s.fake.Objects() {
		if before[p] != v {
			t.Fatalf("%s written", p)
		}
	}
}

// A phone saving the card between Haus's read and write wins: Haus reads
// again and adds to what the phone wrote.
func TestKontaktErgaenzenRace(t *testing.T) {
	s := newSetup(t, want{contacts: true})
	ergaenzen(t, s, map[string]any{"firma": "Beispiel GmbH"})
	p, _ := onlyCard(t, s)
	s.fake.RaceOnPut = p
	_, e := ergaenzen(t, s, map[string]any{"firma": "Beispiel GmbH", "telefon": "030 1234567"})
	if e.Status != "ergaenzt" || e.Felder["telefon"] != "ergaenzt" || s.fake.RaceOnPut != "" {
		t.Fatalf("%+v, race left %q", e, s.fake.RaceOnPut)
	}
	if _, vcf := onlyCard(t, s); !strings.Contains(vcf, "TEL:030 1234567") {
		t.Fatalf("not written after the race:\n%s", vcf)
	}
}

func TestIBAN(t *testing.T) {
	for in, want := range map[string]string{"DE89370400440532013000": ibanDE, " de89 3704 0044 0532 0130 00 ": ibanDE, "GB82WEST12345698765432": ibanGB} {
		got, ok := normIBAN(in)
		if !ok || groupIBAN(got) != want {
			t.Fatalf("%q: %q %v", in, groupIBAN(got), ok)
		}
	}
	for _, bad := range []string{"", "DE89370400440532013001", "DE8937040044053201300", "XX00", "DE89-3704-0044-0532-0130-00x", "DE89 3704 0044 0532 0130 00 00"} {
		if _, ok := normIBAN(bad); ok {
			t.Fatalf("%q passed", bad)
		}
	}
}

var anschrift = map[string]any{"strasse": "Musterweg 1", "plz": "12345", "ort": "Beispielstadt"}

// An address is set on a new card (land DE when not said), filled on a
// firm's card without one, never written over one that is there, and
// never added to a family card; a German PLZ is five digits.
func TestKontaktErgaenzenAnschrift(t *testing.T) {
	s := newSetup(t, want{contacts: true})
	_, e := ergaenzen(t, s, map[string]any{"firma": "Beispiel Bau GmbH", "anschrift": anschrift})
	felder(t, e, map[string]string{"firma": "angelegt", "anschrift": "angelegt"})
	if _, vcf := onlyCard(t, s); !strings.Contains(vcf, "ADR;TYPE=WORK:;;Musterweg 1;Beispielstadt;;12345;DE\r\n") {
		t.Fatalf("no work address:\n%s", vcf)
	}
	// another address: kept, the stored one said, nothing written
	_, before := onlyCard(t, s)
	_, e = ergaenzen(t, s, map[string]any{"firma": "Beispiel Bau GmbH", "anschrift": map[string]any{"strasse": "Andere Str. 9", "plz": "54321", "ort": "Anderswo"}})
	if e.Felder["anschrift"] != "vorhanden" || !strings.Contains(e.Gruende["anschrift"], "Musterweg 1, 12345 Beispielstadt") || e.Status != "unveraendert" {
		t.Fatalf("%+v", e)
	}
	if _, vcf := onlyCard(t, s); vcf != before || strings.Contains(vcf, "Anderswo") {
		t.Fatalf("an address was written over:\n%s", vcf)
	}
	_, e = ergaenzen(t, s, map[string]any{"firma": "Beispiel Bau GmbH", "anschrift": anschrift})
	if e.Felder["anschrift"] != "vorhanden" || e.Gruende["anschrift"] != "" {
		t.Fatalf("the same address: %+v", e)
	}

	// a firm's card without an address is filled, a foreign country kept
	s = newSetup(t, want{contacts: true})
	ergaenzen(t, s, map[string]any{"firma": "Beispiel Logistik AG"})
	_, e = ergaenzen(t, s, map[string]any{"firma": "Beispiel Logistik AG", "anschrift": map[string]any{"strasse": "Beispielgasse 3", "plz": "1010", "ort": "Wien", "land": "AT"}})
	if e.Status != "ergaenzt" || e.Felder["anschrift"] != "ergaenzt" {
		t.Fatalf("%+v", e)
	}
	if _, vcf := onlyCard(t, s); !strings.Contains(vcf, "ADR;TYPE=WORK:;;Beispielgasse 3;Wien;;1010;AT\r\n") {
		t.Fatalf("not filled:\n%s", vcf)
	}

	// a family card gets none
	s = newSetup(t, want{contacts: true})
	card := "BEGIN:VCARD\r\nVERSION:4.0\r\nUID:paul\r\nFN:Paul Plan\r\nORG:Plan Bau\r\nEND:VCARD\r\n"
	s.fake.Put("/bote/kontakte/paul.vcf", card)
	_, e = ergaenzen(t, s, map[string]any{"firma": "Plan Bau", "anschrift": anschrift})
	if e.Felder["anschrift"] != "abgelehnt" {
		t.Fatalf("%+v", e)
	}
	if _, got := onlyCard(t, s); got != card {
		t.Fatalf("a family card was written:\n%s", got)
	}

	// a German PLZ is five digits; an address lacking a part is none
	for _, bad := range []any{
		map[string]any{"strasse": "Musterweg 1", "plz": "1234", "ort": "Beispielstadt"},
		map[string]any{"strasse": "Musterweg 1", "plz": "1234a", "ort": "Beispielstadt", "land": "de"},
		map[string]any{"strasse": "Musterweg 1", "plz": "12345"},
		"Musterweg 1, 12345 Beispielstadt",
	} {
		s = newSetup(t, want{contacts: true})
		_, e = ergaenzen(t, s, map[string]any{"firma": "Beispiel Bau GmbH", "anschrift": bad})
		if e.Felder["anschrift"] != "abgelehnt" || e.Gruende["anschrift"] == "" {
			t.Fatalf("%v: %+v", bad, e)
		}
		if _, vcf := onlyCard(t, s); strings.Contains(vcf, "ADR") {
			t.Fatalf("%v written:\n%s", bad, vcf)
		}
	}
}
