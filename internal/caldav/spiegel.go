package caldav

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
)

// The phone's contacts, mirrored (laib ADR 0029): the Homes app sends all
// of a person's phone contacts as one text of vCards, and Homes keeps an
// address book of the person's (/<person>/handy-kontakte/) the same, as
// its own account: each card at a place its UID names, written only when
// its text changed, and every card the phone no longer has removed. What
// Homes wrote is noted on the card itself (X-HAUS-INHALT, a hash of the
// phone's text), so "unchanged" holds whatever way Radicale stores it.

// inhaltProp carries the hash of the phone's text of a mirrored card. It
// keeps the old name: stored data, on every mirrored card (laib#1021).
const inhaltProp = "X-HAUS-INHALT"

// maxMirrorBytes caps the read of a mirrored address book: past it the
// read fails, as a mirror must not judge by half its cards.
var maxMirrorBytes int64 = 256 << 20

// RawCard is one card of a phone's upload: its UID and its text, CRLF.
type RawCard struct {
	UID  string
	Name string // FN, for a sentence about the card
	Text string
}

// ErrNoUID is a card of an upload without a UID.
type ErrNoUID struct {
	N    int    // the card's place in the upload, from 1
	Name string // its FN, or ""
}

func (e *ErrNoUID) Error() string {
	return fmt.Sprintf("card %d (%q) has no UID", e.N, e.Name)
}

// ErrBrokenCards is an upload that is no sequence of whole vCards.
var ErrBrokenCards = errors.New("vCards: BEGIN:VCARD and END:VCARD do not pair up")

// SplitCards cuts a text of concatenated vCards into its cards, each line
// kept as it came (folding and all; a nested card stays inside its own).
// A card without a UID is an ErrNoUID; text outside the cards is ignored.
func SplitCards(text string) ([]RawCard, error) {
	var out []RawCard
	var cur []string
	depth := 0
	for _, l := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		l = strings.TrimRight(l, "\r")
		t := strings.ToUpper(strings.TrimSpace(l))
		if depth == 0 {
			if t == "BEGIN:VCARD" {
				depth, cur = 1, []string{l}
			} else if t == "END:VCARD" {
				return nil, ErrBrokenCards
			}
			continue
		}
		if l == "" || depth == 1 && (strings.HasPrefix(t, inhaltProp+":") || strings.HasPrefix(t, inhaltProp+";")) {
			continue // Homes' own note is never the phone's
		}
		cur = append(cur, l)
		switch t {
		case "BEGIN:VCARD":
			depth++
		case "END:VCARD":
			depth--
		}
		if depth > 0 {
			continue
		}
		cd := RawCard{Text: strings.Join(cur, "\r\n") + "\r\n"}
		cd.UID, cd.Name = top(cur, "UID"), top(cur, "FN")
		if cd.UID == "" {
			return nil, &ErrNoUID{N: len(out) + 1, Name: cd.Name}
		}
		out = append(out, cd)
	}
	if depth != 0 {
		return nil, ErrBrokenCards
	}
	return out, nil
}

// top is the value of the card's own property name (not a nested card's),
// unfolded and trimmed; "" when it has none.
func top(lines []string, name string) string {
	depth := 0
	for _, l := range unfold(strings.Join(lines, "\n")) {
		t := strings.ToUpper(strings.TrimSpace(l))
		switch t {
		case "BEGIN:VCARD":
			depth++
			continue
		case "END:VCARD":
			depth--
			continue
		}
		if depth != 1 {
			continue
		}
		i := strings.IndexByte(l, ':')
		if i < 0 {
			continue
		}
		n := l[:i]
		if j := strings.IndexByte(n, ';'); j >= 0 {
			n = n[:j]
		}
		if strings.EqualFold(propName(n), name) {
			return strings.TrimSpace(l[i+1:])
		}
	}
	return ""
}

// inhalt is the hash of a phone's card: what tells a changed card.
func (cd RawCard) inhalt() string {
	h := sha256.Sum256([]byte(cd.Text))
	return hex.EncodeToString(h[:])
}

// stored is the card as Homes writes it: the phone's text, with the hash
// before its end.
func (cd RawCard) stored() string {
	i := strings.LastIndex(strings.ToUpper(cd.Text), "END:VCARD")
	return cd.Text[:i] + inhaltProp + ":" + cd.inhalt() + "\r\n" + cd.Text[i:]
}

// CardHref is a card's place in a mirrored book, from its UID: hex only,
// so any UID (an Android lookup key has dots and percent signs) is safe.
func CardHref(uid string) string {
	h := sha256.Sum256([]byte(uid))
	return hex.EncodeToString(h[:16]) + ".vcf"
}

// BookCard is one card in an address book: its place, ETag and text.
type BookCard struct {
	URL, ETag, Data string
}

// every card with a UID: a mirrored card always has one
const mirrorQuery = `<?xml version="1.0" encoding="utf-8"?>
<CR:addressbook-query xmlns:D="DAV:" xmlns:CR="urn:ietf:params:xml:ns:carddav">
  <D:prop><D:getetag/><CR:address-data/></D:prop>
  <CR:filter><CR:prop-filter name="UID"/></CR:filter>
</CR:addressbook-query>`

// BookCards is every card in the address book at book (a URL or a path on
// the server); found is false when there is no such book. A read cut short
// is an error, never fewer cards.
func (c *Client) BookCards(ctx context.Context, book string) (cards []BookCard, found bool, err error) {
	if c == nil {
		return nil, false, ErrNoClient
	}
	u, err := c.resolve(strings.TrimRight(book, "/") + "/")
	if err != nil {
		return nil, false, err
	}
	resp, err := c.send(ctx, "REPORT", u, "1", mirrorQuery, nil)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, false, nil
	}
	if resp.StatusCode != http.StatusMultiStatus {
		return nil, false, &ErrStatus{"REPORT", resp.StatusCode}
	}
	cr := &countingReader{r: io.LimitReader(resp.Body, maxMirrorBytes+1)}
	dec := xml.NewDecoder(cr)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return cards, true, nil
		}
		if err != nil {
			if cr.n > maxMirrorBytes {
				return nil, true, fmt.Errorf("Radicale: Adressbuch größer als %d MB", maxMirrorBytes>>20)
			}
			return nil, true, fmt.Errorf("Radicale: Adressbuch unlesbar: %w", err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Space != "DAV:" || se.Name.Local != "response" {
			continue
		}
		var r cardResponse
		if err := dec.DecodeElement(&r, &se); err != nil {
			return nil, true, fmt.Errorf("Radicale: Adressbuch unlesbar: %w", err)
		}
		cu, err := c.resolve(r.Href)
		if err != nil || strings.HasSuffix(cu, "/") {
			continue // another host, or the book itself
		}
		for _, ps := range r.Propstat {
			if ps.Prop.AddressData != "" {
				cards = append(cards, BookCard{URL: cu, ETag: ps.Prop.ETag, Data: ps.Prop.AddressData})
				break
			}
		}
	}
}

// MakeBook makes an address book at book named name (an extended MKCOL,
// RFC 5689). One made meanwhile (405, 409 from Radicale) is no error.
func (c *Client) MakeBook(ctx context.Context, book, name string) error {
	u, err := c.resolve(strings.TrimRight(book, "/") + "/")
	if err != nil {
		return err
	}
	var b strings.Builder
	xml.EscapeText(&b, []byte(name))
	body := strings.Replace(mkcolBookBody, "<D:displayname>Haushalt</D:displayname>", "<D:displayname>"+b.String()+"</D:displayname>", 1)
	code, _, err := c.do(ctx, "MKCOL", u, "", body, nil)
	if err != nil {
		return err
	}
	if code == http.StatusCreated {
		return nil
	}
	if code == http.StatusMethodNotAllowed { // there already
		return nil
	}
	return &ErrStatus{"MKCOL", code}
}

// PutCard writes a card at u: a new one only where none is (If-None-Match),
// a changed one only over the ETag it read (If-Match), else ErrChanged.
func (c *Client) PutCard(ctx context.Context, u, etag, data string) error {
	hdr := map[string]string{"Content-Type": "text/vcard; charset=utf-8", "If-None-Match": "*"}
	if etag != "" {
		hdr = map[string]string{"Content-Type": "text/vcard; charset=utf-8", "If-Match": etag}
	}
	code, _, err := c.do(ctx, http.MethodPut, u, "", data, hdr)
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

// DeleteCard removes the card at u, over its ETag when it has one; one
// gone already is no error.
func (c *Client) DeleteCard(ctx context.Context, u, etag string) error {
	var hdr map[string]string
	if etag != "" {
		hdr = map[string]string{"If-Match": etag}
	}
	code, _, err := c.do(ctx, http.MethodDelete, u, "", "", hdr)
	if err != nil {
		return err
	}
	switch code {
	case http.StatusOK, http.StatusNoContent, http.StatusNotFound:
		return nil
	case http.StatusPreconditionFailed:
		return ErrChanged
	}
	return &ErrStatus{"DELETE", code}
}

// Mirrored is what a mirror did: the cards it holds now, and how many it
// wrote new, wrote over, and removed.
type Mirrored struct {
	Kontakte  int `json:"kontakte"`
	Neu       int `json:"neu"`
	Geaendert int `json:"geaendert"`
	Entfernt  int `json:"entfernt"`
}

// Mirror makes the address book at book (made, named name, when missing)
// hold exactly cards: each at CardHref(UID), the last of a UID winning;
// unchanged ones are left alone, and cards not among them are removed
// after every write went through.
func (c *Client) Mirror(ctx context.Context, book, name string, cards []RawCard) (Mirrored, error) {
	var m Mirrored
	bookURL, err := c.resolve(strings.TrimRight(book, "/") + "/")
	if err != nil {
		return m, err
	}
	have, found, err := c.BookCards(ctx, bookURL)
	if err != nil {
		return m, err
	}
	if !found {
		if err := c.MakeBook(ctx, bookURL, name); err != nil {
			return m, err
		}
	}
	byURL := map[string]BookCard{}
	for _, bc := range have {
		byURL[bc.URL] = bc
	}
	want := map[string]RawCard{}
	var order []string
	for _, cd := range cards {
		u := bookURL + CardHref(cd.UID)
		if _, ok := want[u]; !ok {
			order = append(order, u)
		}
		want[u] = cd
	}
	m.Kontakte = len(order)
	for _, u := range order {
		cd := want[u]
		old, ok := byURL[u]
		if ok && storedInhalt(old.Data) == cd.inhalt() {
			continue
		}
		if err := c.PutCard(ctx, u, old.ETag, cd.stored()); err != nil {
			return m, fmt.Errorf("%s: %w", path.Base(u), err)
		}
		if ok {
			m.Geaendert++
		} else {
			m.Neu++
		}
	}
	for _, bc := range have {
		if _, ok := want[bc.URL]; ok {
			continue
		}
		if err := c.DeleteCard(ctx, bc.URL, bc.ETag); err != nil {
			return m, fmt.Errorf("%s: %w", path.Base(bc.URL), err)
		}
		m.Entfernt++
	}
	return m, nil
}

// storedInhalt is the hash Homes noted on a card it wrote, or "".
func storedInhalt(data string) string {
	for _, l := range unfold(data) {
		if n, v, ok := strings.Cut(l, ":"); ok && strings.EqualFold(strings.TrimSpace(n), inhaltProp) {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
