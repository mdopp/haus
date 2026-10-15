package caldav

import (
	"crypto/rand"
	"fmt"
	"strings"
	"time"
)

// Just enough vCard (RFC 6350, and the 3.0 cards phones still write) for
// the family's contacts. The content-line grammar, folding and TEXT
// escaping are iCalendar's (ical.go); a property may carry a group prefix
// (item1.EMAIL), which is dropped.

// Contact is one card as the tools see it.
type Contact struct {
	UID      string
	Name     string // FN
	Nickname string
	Org      string
	Phones   []string
	Emails   []string
	// Birthday is JJJJ-MM-TT, or --MM-TT without a year; a card's
	// birthday in another form is kept as its text.
	Birthday string
	// Addresses are the postal addresses (ADR), each one line.
	Addresses []string
}

// NewUUID is a random urn:uuid (RFC 4122 version 4), a card's UID.
func NewUUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("urn:uuid:%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// VCF renders the contact as a vCard 4.0. Every value is TEXT-escaped, so
// a newline, a semicolon or a comma in it stays inside its property.
func (ct Contact) VCF(now time.Time) string {
	uid := ct.UID
	if uid == "" {
		uid = NewUUID()
	}
	var b strings.Builder
	for _, l := range []string{"BEGIN:VCARD", "VERSION:4.0", "PRODID:" + prodID, "UID:" + EscapeText(uid), "FN:" + EscapeText(ct.Name)} {
		fold(&b, l)
	}
	if ct.Nickname != "" {
		fold(&b, "NICKNAME:"+EscapeText(ct.Nickname))
	}
	if ct.Org != "" {
		fold(&b, "ORG:"+EscapeText(ct.Org))
	}
	for _, p := range ct.Phones {
		fold(&b, "TEL:"+EscapeText(p))
	}
	for _, e := range ct.Emails {
		fold(&b, "EMAIL:"+EscapeText(e))
	}
	if bd := bdayValue(ct.Birthday); bd != "" {
		fold(&b, "BDAY:"+bd)
	}
	fold(&b, "REV:"+utc(now))
	fold(&b, "END:VCARD")
	return b.String()
}

// ValidBirthday says whether s is a birthday the tools write: JJJJ-MM-TT
// (a real day, not in the year 0000) or --MM-TT without a year.
func ValidBirthday(s string) bool {
	if md, ok := strings.CutPrefix(s, "--"); ok {
		_, err := time.Parse("2006-01-02", "2000-"+md) // a leap year: --02-29 is a birthday
		return err == nil && len(md) == 5
	}
	t, err := time.Parse("2006-01-02", s)
	return err == nil && t.Year() > 0
}

// bdayValue is a valid birthday as vCard 4.0 writes it (19850412, --0412);
// anything else writes none (VALUE=text is legal, but the tools only
// write a date).
func bdayValue(s string) string {
	if !ValidBirthday(s) {
		return ""
	}
	if md, ok := strings.CutPrefix(s, "--"); ok {
		return "--" + strings.ReplaceAll(md, "-", "")
	}
	return strings.ReplaceAll(s, "-", "")
}

// appleNoYear is the year iOS writes for a birthday without one.
const appleNoYear = "1604"

// birthday reads a BDAY value: 19850412, 1985-04-12, either with a time,
// --0412, --04-12, and iOS's 1604-04-12 for no year; another value is
// kept as it is.
func birthday(v string, omitYear string) string {
	v = strings.TrimSpace(v)
	d, _, _ := strings.Cut(v, "T")
	if len(d) > 4 && ((len(omitYear) == 4 && strings.HasPrefix(d, omitYear)) || strings.HasPrefix(d, appleNoYear)) {
		d = "--" + strings.TrimPrefix(d[4:], "-")
	}
	if md, ok := strings.CutPrefix(d, "--"); ok {
		md = strings.ReplaceAll(md, "-", "")
		if t, err := time.Parse("20060102", "2000"+md); err == nil && len(md) == 4 {
			return "--" + t.Format("01-02")
		}
		return v
	}
	for _, l := range []string{"20060102", "2006-01-02"} {
		if t, err := time.Parse(l, d); err == nil {
			return t.Format("2006-01-02")
		}
	}
	return v
}

// propName is a property's name without its group.
func propName(n string) string {
	if i := strings.LastIndexByte(n, '.'); i >= 0 {
		return n[i+1:]
	}
	return n
}

// Contacts reads every VCARD in a parsed stream.
func Contacts(root *Component) []Contact {
	var out []Contact
	root.Walk(func(c *Component) {
		if c.Name != "VCARD" {
			return
		}
		var ct Contact
		for _, p := range c.Props {
			v := UnescapeText(p.Value)
			switch propName(p.Name) {
			case "UID":
				ct.UID = v
			case "FN":
				ct.Name = v
			case "NICKNAME":
				ct.Nickname = v
			case "ORG":
				ct.Org = orgParts(p.Value)
			case "TEL":
				if v = strings.TrimSpace(strings.TrimPrefix(v, "tel:")); v != "" {
					ct.Phones = append(ct.Phones, v)
				}
			case "EMAIL":
				if v = strings.TrimSpace(strings.TrimPrefix(v, "mailto:")); v != "" {
					ct.Emails = append(ct.Emails, v)
				}
			case "BDAY":
				ct.Birthday = birthday(v, p.Params["X-APPLE-OMIT-YEAR"])
			case "ADR":
				if a := address(p.Value); a != "" {
					ct.Addresses = append(ct.Addresses, a)
				}
			}
		}
		out = append(out, ct)
	})
	return out
}

// parts splits a structured value (ORG, ADR) at its unescaped semicolons,
// empty parts kept in their place; an escaped newline is a space, any
// other escaped character itself.
func parts(v string) []string {
	var out []string
	var cur strings.Builder
	for i := 0; i < len(v); i++ {
		switch {
		case v[i] == '\\' && i+1 < len(v):
			i++
			if v[i] == 'n' || v[i] == 'N' {
				cur.WriteByte(' ')
			} else {
				cur.WriteByte(v[i])
			}
		case v[i] == ';':
			out = append(out, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(v[i])
		}
	}
	return append(out, cur.String())
}

// joinSome joins the parts that are not blank, each on one line.
func joinSome(sep string, ps ...string) string {
	var keep []string
	for _, p := range ps {
		if p = strings.Join(strings.Fields(p), " "); p != "" {
			keep = append(keep, p)
		}
	}
	return strings.Join(keep, sep)
}

// orgParts reads a structured ORG (name;unit;…) as "name, unit".
func orgParts(v string) string { return joinSome(", ", parts(v)...) }

// address reads an ADR (post box;extended;street;locality;region;code;
// country) as a letter has it: "street, code locality, region, country";
// "" when it is all blank.
func address(v string) string {
	p := append(parts(v), make([]string, 7)...)
	return joinSome(", ", p[0], p[1], p[2], joinSome(" ", p[5], p[3]), p[4], p[6])
}
