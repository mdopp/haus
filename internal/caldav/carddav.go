package caldav

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
)

// A read of the address books stops after maxCards cards or maxCardBytes
// of answer per address book (photos make cards big) and says it did.
var (
	maxCards           = 5000
	maxCardBytes int64 = 64 << 20
)

// every card that has a name (FN is required in vCard 3.0 and 4.0): a
// prop-filter without a test is "the property is there" (RFC 6352 §10.5.1)
const addressbookQuery = `<?xml version="1.0" encoding="utf-8"?>
<CR:addressbook-query xmlns:D="DAV:" xmlns:CR="urn:ietf:params:xml:ns:carddav">
  <D:prop><D:getetag/><CR:address-data/></D:prop>
  <CR:filter><CR:prop-filter name="FN"/></CR:filter>
</CR:addressbook-query>`

type cardResponse struct {
	Href     string `xml:"DAV: href"`
	Propstat []struct {
		Prop struct {
			AddressData string `xml:"urn:ietf:params:xml:ns:carddav address-data"`
			ETag        string `xml:"DAV: getetag"`
		} `xml:"DAV: prop"`
	} `xml:"DAV: propstat"`
}

// countingReader counts what was read through it.
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// cards streams one address book's cards to fn, response by response, so a
// big address book is never held whole; fn returning false stops early.
// partial says the answer was cut at maxCardBytes, or by the timeout
// after at least one card (a slow Radicale with many photos).

func (c *Client) cards(ctx context.Context, col Collection, fn func(Card) bool) (partial bool, err error) {
	resp, err := c.send(ctx, "REPORT", col.URL, "1", addressbookQuery, nil)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMultiStatus {
		return false, &ErrStatus{"REPORT", resp.StatusCode}
	}
	cr := &countingReader{r: io.LimitReader(resp.Body, maxCardBytes)}
	dec := xml.NewDecoder(cr)
	read := 0
	cut := func(err error) (bool, error) {
		var ne net.Error
		if cr.n >= maxCardBytes || (read > 0 && (errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()))) {
			return true, nil
		}
		return false, fmt.Errorf("Radicale: Adressbuch unlesbar: %w", err)
	}
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return false, nil
		}
		if err != nil {
			return cut(err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Space != "DAV:" || se.Name.Local != "response" {
			continue
		}
		var r cardResponse
		if err := dec.DecodeElement(&r, &se); err != nil {
			return cut(err)
		}
		for _, ps := range r.Propstat {
			if ps.Prop.AddressData == "" {
				continue
			}
			root, err := Parse(ps.Prop.AddressData)
			if err != nil {
				continue // one broken card must not hide the address book
			}
			cts := Contacts(root)
			for _, ct := range cts {
				read++
				cd := Card{Contact: ct}
				if len(cts) == 1 { // a file of several cards is never written back
					cd.href, cd.etag, cd.data = r.Href, ps.Prop.ETag, ps.Prop.AddressData
				}
				if !fn(cd) {
					return false, nil
				}
			}
		}
	}
}

// Contacts reads every card in the account's address books and the
// configured one; partial says it stopped at a cap.
func (c *Client) Contacts(ctx context.Context) (out []Contact, partial bool, err error) {
	cards, partial, err := c.Cards(ctx)
	for _, cd := range cards {
		out = append(out, cd.Contact)
	}
	return out, partial, err
}

// Card is a contact with where it is, its ETag and its text, so it can be
// written back (AmendCard).
type Card struct {
	Contact
	href, etag, data string
}

// Cards is Contacts with each card's place, for a write back.
func (c *Client) Cards(ctx context.Context) (out []Card, partial bool, err error) {
	cols, err := c.readable(ctx, c.books(), "")
	if err != nil {
		return nil, false, err
	}
	for _, col := range cols {
		full := false
		p, err := c.cards(ctx, col, func(cd Card) bool {
			if len(out) == maxCards {
				full = true
				return false
			}
			out = append(out, cd)
			return true
		})
		var st *ErrStatus
		if errors.As(err, &st) && st.Code == http.StatusNotFound {
			continue // gone since the PROPFIND
		}
		if err != nil {
			return nil, false, err
		}
		if p || full {
			return out, true, nil
		}
	}
	return out, false, nil
}

// PutContact writes a new card into the configured address book, else the
// one named "Haushalt", made when missing; it never replaces one.
func (c *Client) PutContact(ctx context.Context, ct Contact) (Collection, error) {
	if ct.UID == "" {
		ct.UID = NewUUID()
	}
	return c.put(ctx, c.books(), "", strings.TrimPrefix(ct.UID, "urn:uuid:")+".vcf", "text/vcard", ct.VCF(c.now()))
}

// what AmendCard replaces: the note it carries on, and the revision
var amended = map[string]bool{"NOTE": true, "REV": true}

// amended is the card's text as it was read, every line kept, with add
// before its end and, when note is set, one NOTE: the card's own note,
// then note on a new line; and a fresh REV.
func (cd Card) amended(add []string, note string, now string) string {
	lines := unfold(cd.data)
	var b strings.Builder
	for _, l := range lines {
		name := l
		if i := strings.IndexAny(l, ";:"); i >= 0 {
			name = l[:i]
		}
		name = strings.ToUpper(propName(name))
		if strings.EqualFold(l, "END:VCARD") {
			for _, a := range add {
				fold(&b, a)
			}
			if note != "" {
				text := cd.Note
				if text != "" {
					text += "\n"
				}
				fold(&b, "NOTE:"+EscapeText(text+note))
			} else if cd.Note != "" {
				fold(&b, "NOTE:"+EscapeText(cd.Note))
			}
			fold(&b, "REV:"+now)
			fold(&b, l)
			continue
		}
		if amended[name] {
			continue
		}
		fold(&b, l)
	}
	return b.String()
}

// AmendCard writes the card back with add (whole content lines, their
// values escaped) and note appended (amended), only over the ETag it read
// (If-Match): a phone's edit in between is ErrChanged, and the caller
// reads again. It changes no value that is there; deciding what to add is
// the caller's (haus#163).
func (c *Client) AmendCard(ctx context.Context, cd Card, add []string, note string) error {
	if cd.etag == "" || cd.href == "" || cd.data == "" {
		return errors.New("Adressbuch: Radicale nennt für die Karte kein ETag; ohne schreibt Haus nicht zurück")
	}
	u, err := c.resolve(cd.href)
	if err != nil {
		return err
	}
	code, _, err := c.do(ctx, http.MethodPut, u, "", cd.amended(add, note, utc(c.now())),
		map[string]string{"Content-Type": "text/vcard; charset=utf-8", "If-Match": cd.etag})
	if err != nil {
		return err
	}
	switch code {
	case http.StatusOK, http.StatusCreated, http.StatusNoContent:
		return nil
	case http.StatusPreconditionFailed:
		return ErrChanged
	}
	return &ErrStatus{"PUT", code}
}
