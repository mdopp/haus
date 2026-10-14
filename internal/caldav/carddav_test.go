package caldav

import (
	"context"
	"fmt"
	"net/http"
	"strings"
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
