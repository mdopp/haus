package caldav

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

const korrekturCard = "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:k1\r\nFN:Anna Beispiel\r\nN:Beispiel;Anna;;;\r\n" +
	"item1.TEL;TYPE=CELL:+49 151 2345 6789\r\nTEL;TYPE=HOME:0711 123\r\nEMAIL;TYPE=INTERNET:anna@example.org\r\n" +
	"ADR;TYPE=HOME:;;Weg 1;Stadt;BW;12345;DE\r\nORG:Firma A;Einkauf\r\nBDAY:1980-01-02\r\nX-IBAN:DE00\r\nNOTE:bleibt\r\nREV:20200101T000000Z\r\nEND:VCARD\r\n"

func TestCorrectedChangesOnlyTheLinesOfTheTakenFields(t *testing.T) {
	ks := []Korrektur{
		{Prop: "N-FAMILY", Alt: "Beispiel", Neu: "Muster"},
		{Prop: "TEL", Alt: "+49 151 23456789", Neu: "+49 151 99999999", Art: "geschaeftlich"}, // the stored one written with spaces
		{Prop: "TEL", Alt: "0711 123", Neu: ""},
		{Prop: "EMAIL", Alt: "", Neu: "anna@muster.example"},
		{Prop: "ADR", AltAdr: &Address{Street: "Weg 1", Locality: "Stadt", Code: "12345", Country: "DE"}, NeuAdr: &Address{Street: "Weg 2", Locality: "Stadt", Code: "12345", Country: "DE"}},
		{Prop: "BDAY", Alt: "1980-01-02", Neu: "1980-01-03"},
		{Prop: "TEL", Alt: "0000 gone", Neu: "0171 1"}, // changed meanwhile: left alone
		{Prop: "ORG", Alt: "Firma A", Neu: "Firma B"},
		{Prop: "TITLE", Alt: "", Neu: "Leiterin"},
		{Prop: "TEL", Alt: "", Neu: "0711 5", Art: "privat"},
	}
	got, done := Corrected(korrekturCard, "k1", ks, "20261107T100000Z")
	if want := []bool{true, true, true, true, true, true, false, true, true, true}; !slices.Equal(done, want) {
		t.Errorf("done %v, want %v", done, want)
	}
	for _, want := range []string{"FN:Anna Muster\r\n", "N:Muster;Anna;;;\r\n", "item1.TEL;TYPE=WORK:+49 151 99999999\r\n", "ORG:Firma B;Einkauf\r\n", "TITLE:Leiterin\r\n", "TEL;TYPE=HOME:0711 5\r\n", "EMAIL;TYPE=INTERNET:anna@example.org\r\n",
		"EMAIL:anna@muster.example\r\n", "ADR;TYPE=HOME:;;Weg 2;Stadt;;12345;DE\r\n", "BDAY:19800103\r\n", "X-IBAN:DE00\r\n", "NOTE:bleibt\r\n", "REV:20261107T100000Z\r\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	for _, gone := range []string{"0711 123", "Weg 1", "1980-01-02", "20200101T000000Z", "0171 1"} {
		if strings.Contains(got, gone) {
			t.Errorf("%q still in\n%s", gone, got)
		}
	}
	if _, done := Corrected(korrekturCard, "other", ks, "x"); slices.Contains(done, true) {
		t.Error("another card's UID was written")
	}
	if same, done := Corrected(korrekturCard, "k1", ks[6:7], "x"); done[0] || same != korrekturCard {
		t.Errorf("nothing taken, yet the card changed:\n%s", same)
	}
}

func TestCorrectSharedWritesOverTheVersionItRead(t *testing.T) {
	fake, c := schreibFake(t)
	ctx := context.Background()
	done, err := c.CorrectShared(ctx, "oma", []Korrektur{{Prop: "TEL", Alt: "+49 170 1", Neu: "+49 170 2"}, {Prop: "N-GIVEN", Alt: "Opa", Neu: "X"}, {Prop: "N-GIVEN", Alt: "Oma", Neu: "Omi"}})
	if err != nil || !slices.Equal(done, []bool{true, false, true}) {
		t.Fatalf("CorrectShared = %v, %v", done, err)
	}
	card := fake.Objects()["/homes/haushalt-kontakte/oma.vcf"]
	if !strings.Contains(card, "TEL;TYPE=cell:+49 170 2") || !strings.Contains(card, "FN:Omi Erna") || !strings.Contains(card, "N:Erna;Omi;;;") || !strings.Contains(card, "X-IBAN:DE00") || !strings.Contains(card, "PHOTO:") {
		t.Errorf("card after the write:\n%s", card)
	}
	if _, err := c.CorrectShared(ctx, "nobody", []Korrektur{{Prop: "TITLE", Alt: "", Neu: "b"}}); !errors.Is(err, ErrNoCard) {
		t.Errorf("a card that is not there: %v", err)
	}
}
