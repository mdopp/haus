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

// bdayValue is a JJJJ-MM-TT or --MM-TT birthday as vCard 4.0 writes it
// (19850412, --0412); anything else is a text birthday.
func bdayValue(s string) string {
	if s == "" {
		return ""
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.Format("20060102")
	}
	if md, ok := strings.CutPrefix(s, "--"); ok {
		if t, err := time.Parse("2006-01-02", "2000-"+md); err == nil {
			return "--" + t.Format("0102")
		}
	}
	return "" // VALUE=text is legal but the tools only write a date
}

// birthday reads a BDAY value: 19850412, 1985-04-12, either with a time,
// --0412, --04-12; another value is kept as it is.
func birthday(v string) string {
	v = strings.TrimSpace(v)
	d, _, _ := strings.Cut(v, "T")
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
				ct.Birthday = birthday(v)
			}
		}
		out = append(out, ct)
	})
	return out
}

// orgParts reads a structured ORG (name;unit;…) as "name, unit".
func orgParts(v string) string {
	var parts []string
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
			parts = append(parts, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(v[i])
		}
	}
	parts = append(parts, cur.String())
	var keep []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			keep = append(keep, p)
		}
	}
	return strings.Join(keep, ", ")
}
