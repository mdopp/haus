package caldav

import (
	"context"
	"errors"
	"net/url"
	"strings"
)

// Kontakt prüfen lassen (owner 2026-11-07): the owner accepted a person's
// own corrections of their card, field by field. Each is written into the
// stored card as a change of one line: the line that holds the old value
// gets the new one (its parameters kept), is dropped, or a new line is
// added. Every other line of the card stays as it was. A field whose old
// value is no longer on the card (changed meanwhile) is left alone and
// said so; nothing is ever written over a value the owner did not see.

// Korrektur is one accepted change of a card's field: Prop is N-GIVEN,
// N-FAMILY (FN follows them), TEL, EMAIL, ADR, BDAY, ORG (its first part)
// or TITLE; Alt "" adds, Neu "" removes. For ADR the values are AltAdr
// and NeuAdr; for BDAY JJJJ-MM-TT or --MM-TT; for TEL Art is the new
// number's kind (mobil, privat, geschaeftlich; "" keeps the line's).
type Korrektur struct {
	Prop           string
	Alt, Neu, Art  string
	AltAdr, NeuAdr *Address
}

// ErrNoCard is a card that is not in the shared address books (any more).
var ErrNoCard = errors.New("Adressbuch: diesen Kontakt gibt es im Adressbuch nicht mehr")

// CorrectShared writes ks into the shared card uid over the version it
// read; another write meanwhile is read again, up to three times. The
// answer says, for each of ks, whether it was written.
func (c *Client) CorrectShared(ctx context.Context, uid string, ks []Korrektur) ([]bool, error) {
	for range 3 {
		at, err := c.FamilyCard(ctx, uid)
		if err != nil {
			return nil, err
		}
		if at.Data == "" {
			return nil, ErrNoCard
		}
		text, done := Corrected(at.Data, uid, ks, utc(c.now()))
		if !anyTrue(done) {
			return done, nil
		}
		u, err := c.resolve(at.Collection + url.PathEscape(at.Name))
		if err != nil {
			return nil, err
		}
		if _, err = c.rewrite(ctx, u, "text/vcard", at.ETag, text); !errors.Is(err, ErrChanged) {
			return done, err
		}
	}
	return nil, ErrChanged
}

func anyTrue(bs []bool) bool {
	for _, b := range bs {
		if b {
			return true
		}
	}
	return false
}

// Corrected is data with ks written into the VCARD uid, and which of ks
// were: each against the card as the ones before it left it, and a fresh
// REV when any was.
func Corrected(data, uid string, ks []Korrektur, rev string) (string, []bool) {
	done := make([]bool, len(ks))
	var b strings.Builder
	blocks(unfold(data), "VCARD", func(ls []string, block bool) {
		if own, _ := ownUID(ls); !block || own != uid || len(ls) < 2 {
			for _, l := range ls {
				fold(&b, l)
			}
			return
		}
		body := append([]string(nil), ls[1:len(ls)-1]...)
		for i, k := range ks {
			body, done[i] = korrigiert(body, k)
		}
		if anyTrue(done) {
			body = setLine(body, "REV", "REV:"+rev)
		}
		fold(&b, ls[0])
		for _, l := range body {
			fold(&b, l)
		}
		fold(&b, ls[len(ls)-1])
	})
	return b.String(), done
}

// korrigiert is body with k written, and whether it was.
func korrigiert(body []string, k Korrektur) ([]string, bool) {
	switch k.Prop {
	case "N-GIVEN", "N-FAMILY":
		return korrigiertName(body, k)
	case "BDAY":
		i := find(body, "BDAY", func(Prop) bool { return true })
		cur := ""
		if i >= 0 {
			p, _ := parseLine(body[i])
			cur = birthday(UnescapeText(p.Value), p.Params["X-APPLE-OMIT-YEAR"])
		}
		if cur != k.Alt || (k.Neu != "" && bdayValue(k.Neu) == "") {
			return body, false
		}
		if i >= 0 {
			body = append(body[:i:i], body[i+1:]...)
		}
		if k.Neu != "" {
			body = append(body, "BDAY:"+bdayValue(k.Neu))
		}
		return body, true
	case "ORG", "TITLE":
		return korrigiertEinzeln(body, k)
	case "TEL", "EMAIL":
		return korrigiertWert(body, k)
	case "ADR":
		return korrigiertAdresse(body, k.AltAdr, k.NeuAdr)
	}
	return body, false
}

// korrigiertName changes N's given or family name, and FN with them. A
// card without N gets one from its FN first (the page split it so).
func korrigiertName(body []string, k Korrektur) ([]string, bool) {
	i := find(body, "N", func(Prop) bool { return true })
	if i < 0 {
		fn := find(body, "FN", func(Prop) bool { return true })
		if fn < 0 {
			return body, false
		}
		p, _ := parseLine(body[fn])
		body, i = append(body, "N:"+nameParts(UnescapeText(p.Value))), len(body)
	}
	p, _ := parseLine(body[i])
	n := append(parts(p.Value), "", "", "", "", "")[:max(5, len(parts(p.Value)))]
	at := 1
	if k.Prop == "N-FAMILY" {
		at = 0
	}
	if strings.TrimSpace(n[at]) != strings.TrimSpace(k.Alt) {
		return body, false
	}
	n[at] = strings.TrimSpace(k.Neu)
	esc := make([]string, len(n))
	for j, x := range n {
		esc[j] = EscapeText(x)
	}
	body = withValue(body, i, strings.Join(esc, ";"))
	if fn := find(body, "FN", func(Prop) bool { return true }); fn >= 0 {
		if name := strings.TrimSpace(n[1] + " " + n[0]); name != "" {
			body = withValue(body, fn, EscapeText(name))
		}
	}
	return body, true
}

// korrigiertEinzeln changes TITLE, or ORG's first part (its units kept).
func korrigiertEinzeln(body []string, k Korrektur) ([]string, bool) {
	i := find(body, k.Prop, func(Prop) bool { return true })
	var cur []string
	if i >= 0 {
		p, _ := parseLine(body[i])
		cur = []string{UnescapeText(p.Value)}
		if k.Prop == "ORG" {
			cur = parts(p.Value)
		}
	}
	if strings.TrimSpace(append(cur, "")[0]) != strings.TrimSpace(k.Alt) {
		return body, false
	}
	neu := strings.TrimSpace(k.Neu)
	if k.Prop == "ORG" && len(cur) > 1 {
		rest := make([]string, len(cur)-1)
		for j, x := range cur[1:] {
			rest[j] = EscapeText(x)
		}
		return withValue(body, i, EscapeText(neu)+";"+strings.Join(rest, ";")), true
	}
	switch {
	case neu == "" && i >= 0:
		return append(body[:i:i], body[i+1:]...), true
	case neu == "":
		return body, false
	case i >= 0:
		return withValue(body, i, EscapeText(neu)), true
	}
	return append(body, k.Prop+":"+EscapeText(neu)), true
}

// korrigiertWert changes one TEL or EMAIL: alt's line, or a new one; a
// number's kind with it when k.Art names one.
func korrigiertWert(body []string, k Korrektur) ([]string, bool) {
	prop := k.Prop
	same := func(v string) func(Prop) bool {
		want := gleichWert(prop, v)
		return func(p Prop) bool { return gleichWert(prop, UnescapeText(p.Value)) == want }
	}
	neu := strings.TrimSpace(k.Neu)
	if k.Alt == "" {
		if neu == "" || find(body, prop, same(neu)) >= 0 {
			return body, neu != "" // there already: nothing to add, and nothing wrong
		}
		return append(body, prop+phoneType(k.Art)+":"+EscapeText(neu)), true
	}
	i := find(body, prop, same(k.Alt))
	if i < 0 {
		return body, false
	}
	if neu == "" {
		return append(body[:i:i], body[i+1:]...), true
	}
	if prop == "TEL" && k.Art != "" {
		p, _ := parseLine(body[i])
		group := ""
		if g := strings.LastIndexByte(p.Name, '.'); g >= 0 {
			group = body[i][:g+1]
		}
		out := append([]string(nil), body...)
		out[i] = group + "TEL" + phoneType(k.Art) + ":" + EscapeText(neu)
		return out, true
	}
	return withValue(body, i, EscapeText(neu)), true
}

// gleichWert is a value as two are compared: a number by its digits and a
// leading +, an address by its letters in one case.
func gleichWert(prop, v string) string {
	v = strings.TrimSpace(v)
	if prop == "TEL" {
		v = strings.TrimPrefix(v, "tel:")
		var d strings.Builder
		for i, r := range v {
			if (r >= '0' && r <= '9') || (r == '+' && i == 0) {
				d.WriteRune(r)
			}
		}
		return d.String()
	}
	return strings.ToLower(strings.TrimPrefix(v, "mailto:"))
}

// korrigiertAdresse changes one ADR, matched by street, code, place and country.
func korrigiertAdresse(body []string, alt, neu *Address) ([]string, bool) {
	if neu != nil && *neu == (Address{}) {
		neu = nil
	}
	if alt == nil {
		if neu == nil {
			return body, false
		}
		return append(body, "ADR:"+neu.value()), true
	}
	i := find(body, "ADR", func(p Prop) bool { return gleicheAdresse(AddressOf(p.Value), *alt) })
	if i < 0 {
		return body, false
	}
	if neu == nil {
		return append(body[:i:i], body[i+1:]...), true
	}
	return withValue(body, i, neu.value()), true
}

func gleicheAdresse(a, b Address) bool {
	n := func(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }
	return n(a.Street) == n(b.Street) && n(a.Code) == n(b.Code) && n(a.Locality) == n(b.Locality) && n(a.Country) == n(b.Country)
}

// AddressOf is an ADR value's street, code, place and country.
func AddressOf(v string) Address {
	p := append(parts(v), make([]string, 7)...)
	t := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	return Address{Street: t(p[2]), Locality: t(p[3]), Code: t(p[5]), Country: t(p[6])}
}

// nameParts is N for a name: the last word the family name, the rest given.
func nameParts(name string) string {
	ws := strings.Fields(name)
	if len(ws) < 2 {
		return EscapeText(name) + ";;;;"
	}
	return EscapeText(ws[len(ws)-1]) + ";" + EscapeText(strings.Join(ws[:len(ws)-1], " ")) + ";;;"
}

// find is the first top-level line of prop (any group) that ok accepts; -1 none.
func find(body []string, prop string, ok func(Prop) bool) int {
	depth := 0
	for i, l := range body {
		p, parsed := parseLine(l)
		switch {
		case !parsed:
		case p.Name == "BEGIN":
			depth++
		case p.Name == "END":
			depth--
		case depth == 0 && propName(p.Name) == prop && ok(p):
			return i
		}
	}
	return -1
}

// withValue is body with line i's value replaced, its name and parameters kept.
func withValue(body []string, i int, value string) []string {
	p, _ := parseLine(body[i])
	out := append([]string(nil), body...)
	out[i] = body[i][:len(body[i])-len(p.Value)] + value
	return out
}

// setLine is body with prop's line(s) replaced by line.
func setLine(body []string, prop, line string) []string {
	var out []string
	for _, l := range body {
		if p, ok := parseLine(l); ok && propName(p.Name) == prop {
			continue
		}
		out = append(out, l)
	}
	return append(out, line)
}
