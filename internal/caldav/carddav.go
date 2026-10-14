package caldav

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
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
	Propstat []struct {
		Prop struct {
			AddressData string `xml:"urn:ietf:params:xml:ns:carddav address-data"`
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
// partial says the answer was cut at maxCardBytes.
func (c *Client) cards(ctx context.Context, col Collection, fn func(Contact) bool) (partial bool, err error) {
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
	cut := func(err error) (bool, error) {
		if cr.n >= maxCardBytes {
			return true, nil
		}
		return false, fmt.Errorf("Kontakte: REPORT unlesbar: %w", err)
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
			for _, ct := range Contacts(root) {
				if !fn(ct) {
					return false, nil
				}
			}
		}
	}
}

// Contacts reads every card in the account's address books and the
// configured one; partial says it stopped at a cap.
func (c *Client) Contacts(ctx context.Context) (out []Contact, partial bool, err error) {
	cols, err := c.readable(ctx, c.books(), "")
	if err != nil {
		return nil, false, err
	}
	for _, col := range cols {
		full := false
		p, err := c.cards(ctx, col, func(ct Contact) bool {
			if len(out) == maxCards {
				full = true
				return false
			}
			out = append(out, ct)
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
