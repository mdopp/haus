package caldav

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mdopp/haus/internal/caldav/davfake"
)

// a card as DAVx⁵ or an iPhone writes it: vCard 3.0, groups, folded
// lines, a tel: URI, escaped commas
const phoneCard = "BEGIN:VCARD\r\nVERSION:3.0\r\nPRODID:-//phone//EN\r\nUID:oma-1\r\n" +
	"FN:Oma Erna\r\nN:Erna;Oma;;;\r\nNICKNAME:Omi\r\nORG:Seniorenstift\\, Haus 2\r\n" +
	"item1.EMAIL;TYPE=INTERNET:erna@example.org\r\nitem1.X-ABLabel:privat\r\n" +
	"TEL;TYPE=CELL;VALUE=uri:tel:+49-170-1234567\r\nTEL;TYPE=HOME:0711 98765\r\n" +
	"BDAY:1941-03-0\r\n 9\r\nPHOTO;ENCODING=b;TYPE=JPEG:" + "QUJD" + "\r\nEND:VCARD\r\n"

func card(uid, fn, extra string) string {
	return "BEGIN:VCARD\r\nVERSION:4.0\r\nUID:" + uid + "\r\nFN:" + fn + "\r\n" + extra + "END:VCARD\r\n"
}

// A contact goes into the address book named "Haushalt", never the first
// by address, and never into the calendar of that name.
func TestPutContactGoesToHaushaltAddressBook(t *testing.T) {
	fake := davfake.New("bote", "pw")
	defer fake.Close()
	fake.AddAddressBook("/bote/a-kontakte/", "Kontakte")
	fake.AddCalendar("/bote/haushalt/", "Haushalt")
	fake.AddAddressBook("/bote/zz/", "Haushalt")
	c := &Client{URL: fake.URL, User: "bote", Password: "pw"}
	col, err := c.PutContact(context.Background(), Contact{Name: "Eva Müller", Phones: []string{"+49 170 1234567"}})
	if err != nil || col.Name != "Haushalt" || col.URL != fake.URL+"/bote/zz/" {
		t.Fatalf("%+v %v", col, err)
	}
	if len(fake.Objects()) != 1 {
		t.Fatalf("objects: %v", fake.Objects())
	}
	for p := range fake.Objects() {
		if !strings.HasPrefix(p, "/bote/zz/") || !strings.HasSuffix(p, ".vcf") {
			t.Fatalf("stray object %s", p)
		}
	}
	if count(fake.Methods, "MKCOL") != 0 || count(fake.Methods, "MKCALENDAR") != 0 {
		t.Fatalf("methods: %v", fake.Methods)
	}
	for _, h := range fake.Header {
		if h.Get("If-None-Match") != "*" || !strings.HasPrefix(h.Get("Content-Type"), "text/vcard") {
			t.Fatalf("a PUT never replaces, and says vCard: %v", h)
		}
	}
	// the calendar writes are not confused by the address book
	ccol, err := c.PutEvent(context.Background(), Event{Summary: "y", Start: time.Now(), End: time.Now().Add(time.Hour)})
	if err != nil || ccol.URL != fake.URL+"/bote/haushalt/" {
		t.Fatalf("event: %+v %v", ccol, err)
	}
}

// No "Haushalt" address book: the first contact makes one with an
// extended MKCOL, beside the calendar of that name, once.
func TestNoHaushaltAddressBookMakesOne(t *testing.T) {
	fake := davfake.New("bote", "pw")
	defer fake.Close()
	fake.AddCalendar("/bote/haushalt/", "Haushalt")
	fake.AddAddressBook("/bote/a-kontakte/", "Kontakte")
	c := &Client{URL: fake.URL, User: "bote", Password: "pw"}
	ctx := context.Background()
	if cs, _, err := c.Contacts(ctx); err != nil || len(cs) != 0 {
		t.Fatalf("reading: %v %v", cs, err)
	}
	if got := fake.AddressBooks(); len(got) != 1 {
		t.Fatalf("reading creates nothing: %v", got)
	}
	col, err := c.PutContact(ctx, Contact{Name: "A"})
	if err != nil || col.Name != "Haushalt" {
		t.Fatalf("%+v %v", col, err)
	}
	if _, err := c.PutContact(ctx, Contact{Name: "B"}); err != nil {
		t.Fatal(err)
	}
	books := fake.AddressBooks()
	if len(books) != 2 || count(fake.Methods, "MKCOL") != 1 {
		t.Fatalf("one new address book: %v %v", books, fake.Methods)
	}
	made := ""
	for _, b := range books {
		if b != "/bote/a-kontakte/" {
			made = b
		}
	}
	if !strings.HasPrefix(made, "/bote/") || made == "/bote/haushalt/" || fake.DisplayName(made) != "Haushalt" {
		t.Fatalf("made %q (%q)", made, fake.DisplayName(made))
	}
	if got := fake.Calendars(); len(got) != 1 || got[0] != "/bote/haushalt/" {
		t.Fatalf("the calendar stays: %v", got)
	}
	for p := range fake.Objects() {
		if !strings.HasPrefix(p, made) {
			t.Fatalf("stray object %s", p)
		}
	}
}

// MKCOL refused because another client made it meanwhile (405/409): look
// again and use it; refused for want of rights (403): an error, and no
// card lands elsewhere.
func TestMkcolRace(t *testing.T) {
	for _, code := range []int{http.StatusMethodNotAllowed, http.StatusConflict} {
		fake := davfake.New("bote", "pw")
		fake.ExistsStatus = code
		c := &Client{URL: fake.URL, User: "bote", Password: "pw"}
		where := "/bote/" + bookSlug + "/" // where the client will try to make it
		fake.CreateAddressBookAfterPropfind(where, "Haushalt")
		col, err := c.PutContact(context.Background(), Contact{Name: "x"})
		if err != nil || col.Name != "Haushalt" {
			t.Fatalf("%d: %+v %v (methods %v)", code, col, err, fake.Methods)
		}
		if count(fake.Methods, "PROPFIND") != 2 || count(fake.Methods, "PUT "+where) != 1 {
			t.Fatalf("%d: rediscovered after the refusal: %v", code, fake.Methods)
		}
		fake.Close()
	}

	fake := davfake.New("bote", "pw")
	defer fake.Close()
	fake.AddAddressBook("/bote/a-kontakte/", "Kontakte")
	fake.MkcolStatus = http.StatusForbidden
	c := &Client{URL: fake.URL, User: "bote", Password: "pw"}
	if _, err := c.PutContact(context.Background(), Contact{Name: "x"}); err == nil {
		t.Fatal("want an error")
	}
	if len(fake.Objects()) != 0 {
		t.Fatalf("written elsewhere: %v", fake.Objects())
	}
}

// HAUS_RADICALE_ADDRESSBOOK takes the writes, no MKCOL; it is read along
// with the account's own address books.
func TestConfiguredAddressBook(t *testing.T) {
	fake := davfake.New("bote", "pw")
	defer fake.Close()
	fake.AddAddressBook("/familie/kontakte/", "Kontakte")
	fake.AddAddressBook("/bote/eigene/", "Eigene")
	fake.Put("/bote/eigene/a.vcf", card("a", "Anna", ""))
	c := &Client{URL: fake.URL, User: "bote", Password: "pw", AddressBook: "/familie/kontakte/"}
	col, err := c.PutContact(context.Background(), Contact{Name: "Bert"})
	if err != nil || col.URL != fake.URL+"/familie/kontakte/" {
		t.Fatalf("%+v %v", col, err)
	}
	if count(fake.Methods, "MKCOL") != 0 {
		t.Fatalf("methods: %v", fake.Methods)
	}
	cs, _, err := c.Contacts(context.Background())
	if err != nil || len(cs) != 2 {
		t.Fatalf("reads the configured and the own: %+v %v", cs, err)
	}
	bad := &Client{URL: fake.URL, User: "bote", Password: "pw", AddressBook: "http://evil.invalid/x/"}
	if _, err := bad.PutContact(context.Background(), Contact{Name: "x"}); err == nil {
		t.Fatal("a configured address book on another host is refused")
	}
}

// Cards from phones read back: name, phones (a tel: URI too), emails in a
// group, the birthday in its forms, nickname and organisation; a calendar
// is not read for cards, an address book on another host not at all.
func TestReadContacts(t *testing.T) {
	fake := davfake.New("bote", "pw")
	defer fake.Close()
	fake.AddAddressBook("/bote/kontakte/", "Kontakte")
	fake.AddAddressBook("http://evil.invalid/bote/k/", "Haushalt")
	fake.AddCalendar("/bote/haushalt/", "Haushalt")
	fake.Put("/bote/kontakte/oma.vcf", phoneCard)
	fake.Put("/bote/kontakte/b.vcf", card("b", "Bert", "BDAY:--0229\r\n"))
	fake.Put("/bote/kontakte/c.vcf", card("c", "Carla", "BDAY:19900102T000000Z\r\n"))
	c := &Client{URL: fake.URL, User: "bote", Password: "pw"}
	cs, partial, err := c.Contacts(context.Background())
	if err != nil || partial {
		t.Fatalf("%v %v", err, partial)
	}
	if count(fake.Methods, "REPORT /bote/haushalt/") != 0 {
		t.Fatalf("a calendar holds no cards: %v", fake.Methods)
	}
	byName := map[string]Contact{}
	for _, x := range cs {
		byName[x.Name] = x
	}
	oma := byName["Oma Erna"]
	if len(cs) != 3 || oma.UID != "oma-1" || oma.Nickname != "Omi" || oma.Org != "Seniorenstift, Haus 2" || oma.Birthday != "1941-03-09" {
		t.Fatalf("contacts: %+v", cs)
	}
	if strings.Join(oma.Phones, "|") != "+49-170-1234567|0711 98765" || strings.Join(oma.Emails, "|") != "erna@example.org" {
		t.Fatalf("phones %v emails %v", oma.Phones, oma.Emails)
	}
	if byName["Bert"].Birthday != "--02-29" || byName["Carla"].Birthday != "1990-01-02" {
		t.Fatalf("birthdays: %+v", cs)
	}
	q := fake.Reports[len(fake.Reports)-1]
	for _, want := range []string{"addressbook-query", `xmlns:CR="urn:ietf:params:xml:ns:carddav"`, "address-data", "filter"} {
		if !strings.Contains(q, want) {
			t.Fatalf("REPORT lacks %q:\n%s", want, q)
		}
	}
}

// A big address book is read only so far: maxCards cards, then partial.
func TestContactsCapped(t *testing.T) {
	old := maxCards
	maxCards = 5
	defer func() { maxCards = old }()
	fake := davfake.New("bote", "pw")
	defer fake.Close()
	fake.AddAddressBook("/bote/kontakte/", "Kontakte")
	for i := 0; i < 8; i++ {
		fake.Put(fmt.Sprintf("/bote/kontakte/%d.vcf", i), card(fmt.Sprint(i), fmt.Sprint("Name ", i), ""))
	}
	c := &Client{URL: fake.URL, User: "bote", Password: "pw"}
	cs, partial, err := c.Contacts(context.Background())
	if err != nil || !partial || len(cs) != 5 {
		t.Fatalf("%d cards, partial %v, %v", len(cs), partial, err)
	}
}

// A wrong password is the refusal, not an empty address book.
func TestContactsWrongPassword(t *testing.T) {
	fake := davfake.New("bote", "pw")
	defer fake.Close()
	c := &Client{URL: fake.URL, User: "bote", Password: "nope"}
	if _, _, err := c.Contacts(context.Background()); err == nil || !strings.Contains(err.Error(), "Anmeldung abgelehnt") || strings.Contains(err.Error(), "nope") {
		t.Fatalf("want the refusal, got %v", err)
	}
}

// An answer bigger than maxCardBytes is read only so far: the cards
// before the cut, partial, no error.
func TestContactsByteCap(t *testing.T) {
	old := maxCardBytes
	maxCardBytes = 3000
	defer func() { maxCardBytes = old }()
	fake := davfake.New("bote", "pw")
	defer fake.Close()
	fake.AddAddressBook("/bote/kontakte/", "Kontakte")
	for i := 0; i < 40; i++ {
		fake.Put(fmt.Sprintf("/bote/kontakte/%02d.vcf", i), card(fmt.Sprint(i), fmt.Sprint("Name ", i), "NOTE:"+strings.Repeat("n", 100)+"\r\n"))
	}
	c := &Client{URL: fake.URL, User: "bote", Password: "pw"}
	cs, partial, err := c.Contacts(context.Background())
	if err != nil || !partial || len(cs) == 0 || len(cs) >= 40 {
		t.Fatalf("%d cards, partial %v, %v", len(cs), partial, err)
	}
}

// One broken card (BEGIN without END) does not hide the address book.
func TestBrokenCardSkipped(t *testing.T) {
	fake := davfake.New("bote", "pw")
	defer fake.Close()
	fake.AddAddressBook("/bote/kontakte/", "Kontakte")
	fake.Put("/bote/kontakte/a.vcf", "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:Kaputt\r\n")
	fake.Put("/bote/kontakte/b.vcf", card("b", "Bert", ""))
	c := &Client{URL: fake.URL, User: "bote", Password: "pw"}
	cs, partial, err := c.Contacts(context.Background())
	if err != nil || partial || len(cs) != 1 || cs[0].Name != "Bert" {
		t.Fatalf("%+v %v %v", cs, partial, err)
	}
}

// A configured address book that is not there (404) is skipped; the
// account's own are still read.
func TestGoneAddressBookSkipped(t *testing.T) {
	fake := davfake.New("bote", "pw")
	defer fake.Close()
	fake.AddAddressBook("/bote/kontakte/", "Kontakte")
	fake.Put("/bote/kontakte/b.vcf", card("b", "Bert", ""))
	c := &Client{URL: fake.URL, User: "bote", Password: "pw", AddressBook: "/familie/fehlt/"}
	cs, _, err := c.Contacts(context.Background())
	if err != nil || len(cs) != 1 {
		t.Fatalf("%+v %v", cs, err)
	}
}

// The address book Haus made earlier is found by its path, whatever
// someone renamed it to: no second one.
func TestHaushaltBookFoundByPath(t *testing.T) {
	fake := davfake.New("bote", "pw")
	defer fake.Close()
	fake.AddAddressBook("/bote/a-kontakte/", "Kontakte")
	fake.AddAddressBook("/bote/"+bookSlug+"/", "Familie (umbenannt)")
	c := &Client{URL: fake.URL, User: "bote", Password: "pw"}
	col, err := c.PutContact(context.Background(), Contact{Name: "x"})
	if err != nil || col.URL != fake.URL+"/bote/"+bookSlug+"/" || count(fake.Methods, "MKCOL") != 0 {
		t.Fatalf("%+v %v %v", col, err, fake.Methods)
	}
}

// A Radicale that stalls mid-answer: the cards read so far, marked
// partial, not an error that hides them all.
func TestContactsTimeoutMidBody(t *testing.T) {
	fake := davfake.New("bote", "pw")
	defer fake.Close()
	fake.AddAddressBook("/bote/kontakte/", "Kontakte")
	for i := 0; i < 5; i++ {
		fake.Put(fmt.Sprintf("/bote/kontakte/%d.vcf", i), card(fmt.Sprint(i), fmt.Sprint("Name ", i), ""))
	}
	fake.StallAfter = 3
	c := &Client{URL: fake.URL, User: "bote", Password: "pw", HTTP: &http.Client{Timeout: 300 * time.Millisecond}}
	cs, partial, err := c.Contacts(context.Background())
	if err != nil || !partial || len(cs) != 3 {
		t.Fatalf("%d cards, partial %v, %v", len(cs), partial, err)
	}
}

// A redirect is not followed: Basic auth never goes to another address;
// the 3xx is an error.
func TestNoRedirectDAV(t *testing.T) {
	var hits atomic.Int32
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer evil.Close()
	fake := davfake.New("bote", "pw")
	defer fake.Close()
	fake.Redirect = evil.URL + "/bote/"
	for name, c := range map[string]*Client{
		"default": {URL: fake.URL, User: "bote", Password: "pw"},
		"own":     {URL: fake.URL, User: "bote", Password: "pw", HTTP: &http.Client{}},
	} {
		if _, err := c.Events(context.Background(), time.Now(), time.Now().Add(time.Hour)); err == nil {
			t.Fatalf("%s: events: want an error", name)
		}
		if _, _, err := c.Contacts(context.Background()); err == nil {
			t.Fatalf("%s: contacts: want an error", name)
		}
		if _, err := c.PutContact(context.Background(), Contact{Name: "x"}); err == nil {
			t.Fatalf("%s: put: want an error", name)
		}
	}
	if hits.Load() != 0 {
		t.Fatalf("followed a redirect %d times", hits.Load())
	}
}

// A contact's error speaks of Radicale or contacts, not of the calendar.
func TestContactErrorsNotKalender(t *testing.T) {
	fake := davfake.New("bote", "pw")
	defer fake.Close()
	c := &Client{URL: fake.URL, User: "bote", Password: "nope"}
	_, _, err := c.Contacts(context.Background())
	if err == nil || strings.Contains(err.Error(), "Kalender") {
		t.Fatalf("%v", err)
	}
	if _, err := c.PutContact(context.Background(), Contact{Name: "x"}); err == nil || strings.Contains(err.Error(), "Kalender") {
		t.Fatalf("%v", err)
	}
}

// A firm's card carries its categories, IBANs and note, and reads back so.
func TestFirmCardRoundTrip(t *testing.T) {
	ct := Contact{Name: "Beispiel GmbH", Org: "Beispiel GmbH", Categories: []string{"Firma"}, IBANs: []string{"DE89370400440532013000"},
		Note: "IBAN: DE89 3704 0044 0532 0130 00\nQuelle: https://paperless.example/documents/7/"}
	root, err := Parse(ct.VCF(time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	got := Contacts(root)
	if len(got) != 1 || got[0].Org != "Beispiel GmbH" || len(got[0].Categories) != 1 || got[0].Categories[0] != "Firma" ||
		len(got[0].IBANs) != 1 || got[0].IBANs[0] != "DE89370400440532013000" || got[0].Note != ct.Note {
		t.Fatalf("%+v", got)
	}
	if !strings.Contains(ct.VCF(time.Now()), "X-IBAN:DE89370400440532013000\r\n") {
		t.Fatal("no X-IBAN line")
	}
	root, _ = Parse(card("p", "Paul", "CATEGORIES:Familie,Firma\r\nNOTE:eins\r\nNOTE:zwei\r\n"))
	if p := Contacts(root)[0]; len(p.Categories) != 2 || p.Categories[1] != "Firma" || p.Note != "eins\nzwei" {
		t.Fatalf("%+v", p)
	}
}

// AmendCard keeps every line of the card (the phone's photo, its groups),
// adds the new ones, carries the note on, and writes only over the ETag it
// read; a phone's edit in between is ErrChanged.
func TestAmendCard(t *testing.T) {
	fake := davfake.New("bote", "pw")
	defer fake.Close()
	fake.AddAddressBook("/bote/kontakte/", "Haushalt")
	fake.Put("/bote/kontakte/oma.vcf", phoneCard)
	fake.Put("/bote/kontakte/b.vcf", card("b", "Beispiel GmbH", "CATEGORIES:Firma\r\nNOTE:Ansprechpartner: Frau Muster\r\nREV:20200101T000000Z\r\n"))
	c := &Client{URL: fake.URL, User: "bote", Password: "pw"}
	ctx := context.Background()
	cards, partial, err := c.Cards(ctx)
	if err != nil || partial || len(cards) != 2 {
		t.Fatalf("%v %v %v", cards, partial, err)
	}
	var b, oma Card
	for _, cd := range cards {
		switch cd.UID {
		case "b":
			b = cd
		case "oma-1":
			oma = cd
		}
	}
	if err := c.AmendCard(ctx, b, []string{"TEL:+49 30 1234567", "X-IBAN:DE89370400440532013000"}, "IBAN: DE89 3704 0044 0532 0130 00"); err != nil {
		t.Fatal(err)
	}
	got := fake.Objects()["/bote/kontakte/b.vcf"]
	for _, want := range []string{"UID:b\r\n", "FN:Beispiel GmbH\r\n", "CATEGORIES:Firma\r\n", "TEL:+49 30 1234567\r\n", "X-IBAN:DE89370400440532013000\r\n",
		`NOTE:Ansprechpartner: Frau Muster\nIBAN: DE89 3704 0044 0532 0130 00` + "\r\n", "END:VCARD\r\n"} {
		if !strings.Contains(got, want) {
			t.Fatalf("lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "REV:2020") || strings.Count(got, "NOTE") != 1 || !strings.HasSuffix(got, "END:VCARD\r\n") {
		t.Fatalf("REV renewed, one NOTE, END last:\n%s", got)
	}
	if h := fake.Header[len(fake.Header)-1]; h.Get("If-Match") == "" || !strings.HasPrefix(h.Get("Content-Type"), "text/vcard") {
		t.Fatalf("a write over a card says its ETag: %v", h)
	}
	if err := c.AmendCard(ctx, oma, []string{"TEL:0711 1"}, ""); err != nil {
		t.Fatal(err)
	}
	got = fake.Objects()["/bote/kontakte/oma.vcf"]
	for _, want := range []string{"item1.EMAIL;TYPE=INTERNET:erna@example.org", "PHOTO;ENCODING=b;TYPE=JPEG:QUJD", "TEL:0711 1", "BDAY:1941-03-09"} {
		if !strings.Contains(got, want) {
			t.Fatalf("lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "NOTE") {
		t.Fatalf("no note given, none written:\n%s", got)
	}
	// b was written since it was read: its old ETag no longer holds
	if err := c.AmendCard(ctx, b, []string{"TEL:1"}, ""); err != ErrChanged {
		t.Fatalf("stale ETag: %v, want ErrChanged", err)
	}
	if err := c.AmendCard(ctx, Card{Contact: Contact{UID: "x"}}, []string{"TEL:1"}, ""); err == nil {
		t.Fatal("a card without its href and ETag is not written")
	}
}

// A firm's work address is one ADR;TYPE=WORK, every part escaped, and
// reads back as a letter has it.
func TestWorkAddress(t *testing.T) {
	a := Address{Street: "Musterweg 1; Hinterhaus", Code: "12345", Locality: "Beispielstadt", Country: "DE"}
	if got := a.Line(); got != `ADR;TYPE=WORK:;;Musterweg 1\; Hinterhaus;Beispielstadt;;12345;DE` {
		t.Fatalf("line %q", got)
	}
	if a.String() != "Musterweg 1; Hinterhaus, 12345 Beispielstadt, DE" {
		t.Fatalf("string %q", a.String())
	}
	root, _ := Parse(Contact{Name: "Beispiel GmbH", WorkAddress: &a}.VCF(time.Now()))
	if got := Contacts(root)[0].Addresses; len(got) != 1 || got[0] != a.String() {
		t.Fatalf("read back %v", got)
	}
}
