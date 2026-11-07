package kontaktantwort

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"strings"
	"time"
	"unicode"

	"github.com/mdopp/homes/internal/caldav"
	"github.com/mdopp/homes/internal/haushalt"
)

// The checks run before the owner sees an answer (owner 2026-11-07): a
// number that does not parse, an e-mail address whose syntax is wrong or
// whose domain takes no mail, a birthday that cannot be, a name changed
// completely, too many changes at once. Each finding is one plain German
// sentence beside its field; nothing is refused, the owner decides.

// Resolver is the DNS the e-mail check asks (net.DefaultResolver; a fake in the tests).
type Resolver interface {
	LookupMX(ctx context.Context, name string) ([]*net.MX, error)
	LookupHost(ctx context.Context, host string) ([]string, error)
}

// DNSWarten is how long the e-mail check waits for the DNS.
var DNSWarten = 3 * time.Second

// VieleAb is how many changes at once are flagged.
const VieleAb = 5

// Pruefung is what the checks need: the house's calling code, the DNS and now.
type Pruefung struct {
	Land string // calling code without +, haushalt.DefaultCountry by default
	DNS  Resolver
	Now  time.Time
}

// Pruefen sets each change's findings and says what concerns the whole
// answer: the name changed completely, too many changes.
func (p Pruefung) Pruefen(ctx context.Context, vorher, nachher Felder, as []Aenderung) []string {
	for i := range as {
		a := &as[i]
		var h string
		switch {
		case a.Nachher == "":
		case a.Art == "telefon":
			h = Nummer(a.Nachher, p.Land)
		case a.Art == "mail":
			h = p.Mail(ctx, a.Nachher)
		case a.Art == "geburtstag":
			h = Geburtstag(a.Nachher, p.Now)
		}
		if h != "" {
			a.Hinweise = append(a.Hinweise, h)
		}
	}
	var out []string
	if h := Name(vorher.Name(), nachher.Name()); h != "" && (vorher.Vorname != nachher.Vorname || vorher.Nachname != nachher.Nachname) {
		out = append(out, h)
	}
	if len(as) >= VieleAb {
		out = append(out, fmt.Sprintf("Viele Änderungen auf einmal (%d): sieh sie dir einzeln an, bevor du alle übernimmst.", len(as)))
	}
	return out
}

// Nummer is what is wrong with a number, "" when nothing: the characters,
// the + only at the start, a country code that exists, and the length of
// the number — German numbers by the rules for German mobile, service and
// landline numbers, others by E.164's 15 digits at most.
func Nummer(s, land string) string {
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "tel:"))
	for i, r := range s {
		if !strings.ContainsRune("0123456789 +-/().", r) {
			return "Die Nummer enthält Zeichen, die in keiner Telefonnummer vorkommen."
		}
		if r == '+' && i > 0 {
			return "Das + gehört nur an den Anfang der Nummer."
		}
	}
	if land == "" {
		land = haushalt.DefaultCountry
	}
	d := ziffern(strings.ReplaceAll(s, "+", ""))
	intl := strings.HasPrefix(s, "+") || strings.HasPrefix(d, "00")
	switch {
	case len(d) < 3:
		return "Das ist keine vollständige Telefonnummer."
	case !intl && !strings.HasPrefix(d, "0"):
		return "Die Nummer hat keine Vorwahl: so ist sie nur im eigenen Ortsnetz erreichbar."
	}
	e164 := haushalt.International(s, land)
	cc := laenderVorwahl(e164)
	if cc == "" {
		return "Diese Ländervorwahl gibt es nicht."
	}
	if cc == "49" {
		return deutsch(strings.TrimPrefix(e164, "49"))
	}
	if n := len(e164); n < 8 || n > 15 {
		return "Die Nummer ist für eine internationale Nummer zu kurz oder zu lang."
	}
	return ""
}

// deutsch checks a German national number (without the 0): mobile 15x, 16x,
// 17x have 10 or 11 digits, a landline 6 to 13, a service number (0800,
// 0180, 0900, …) 7 to 13.
func deutsch(n string) string {
	switch {
	case n == "" || n[0] == '0':
		return "Nach der Vorwahl +49 folgt keine weitere 0."
	case n[0] == '1' && len(n) > 1 && strings.ContainsRune("567", rune(n[1])):
		if len(n) < 10 || len(n) > 11 {
			return "Für eine deutsche Handynummer ist sie zu kurz oder zu lang: nach der 01… kommen insgesamt 10 oder 11 Ziffern."
		}
	case n[0] == '1':
		if len(n) < 6 || len(n) > 13 {
			return "Für eine deutsche Sondernummer ist sie zu kurz oder zu lang."
		}
	case strings.HasPrefix(n, "800"), strings.HasPrefix(n, "900"), strings.HasPrefix(n, "700"):
		if len(n) < 7 || len(n) > 13 {
			return "Für eine deutsche Servicenummer ist sie zu kurz oder zu lang."
		}
	default:
		if len(n) < 6 || len(n) > 13 {
			return "Für eine deutsche Festnetznummer ist sie zu kurz oder zu lang."
		}
	}
	return ""
}

// The country codes of the ITU's list (E.164), by length.
var (
	vorwahl2 = strings.Fields("20 27 30 31 32 33 34 36 39 40 41 43 44 45 46 47 48 49 51 52 53 54 55 56 57 58 60 61 62 63 64 65 66 81 82 84 86 90 91 92 93 94 95 98")
	vorwahl3 = strings.Fields("211 212 213 216 218 220 221 222 223 224 225 226 227 228 229 230 231 232 233 234 235 236 237 238 239 240 241 242 243 244 245 246 247 248 249 250 251 252 253 254 255 256 257 258 260 261 262 263 264 265 266 267 268 269 290 291 297 298 299 " +
		"350 351 352 353 354 355 356 357 358 359 370 371 372 373 374 375 376 377 378 379 380 381 382 383 385 386 387 389 420 421 423 " +
		"500 501 502 503 504 505 506 507 508 509 590 591 592 593 594 595 596 597 598 599 670 672 673 674 675 676 677 678 679 680 681 682 683 685 686 687 688 689 690 691 692 " +
		"850 852 853 855 856 880 886 960 961 962 963 964 965 966 967 968 970 971 972 973 974 975 976 977 992 993 994 995 996 998")
)

// laenderVorwahl is the country code an international number starts with, "" none.
func laenderVorwahl(e164 string) string {
	if strings.HasPrefix(e164, "1") || strings.HasPrefix(e164, "7") {
		return e164[:1]
	}
	for _, list := range [][]string{vorwahl2, vorwahl3} {
		for _, cc := range list {
			if strings.HasPrefix(e164, cc) {
				return cc
			}
		}
	}
	return ""
}

// Mail is what is wrong with an e-mail address, "" when nothing: its
// syntax, then whether its domain takes mail (an MX record, else an A or
// AAAA), asked within DNSWarten.
func (p Pruefung) Mail(ctx context.Context, s string) string {
	s = strings.TrimSpace(strings.TrimPrefix(s, "mailto:"))
	at := strings.LastIndexByte(s, '@')
	a, err := mail.ParseAddress(s)
	if err != nil || a.Address != s || a.Name != "" || at < 1 || at > 64 || len(s) > 254 || !domainForm(s[at+1:]) {
		return "So ist die E-Mail-Adresse nicht gültig."
	}
	if p.DNS == nil {
		return ""
	}
	domain := strings.ToLower(s[at+1:])
	ctx, cancel := context.WithTimeout(ctx, DNSWarten)
	defer cancel()
	mx, err := p.DNS.LookupMX(ctx, domain)
	var dnsErr *net.DNSError
	switch {
	case err == nil && len(mx) == 1 && (mx[0].Host == "." || mx[0].Host == ""):
		return "Die Domain " + domain + " nimmt ausdrücklich keine E-Mails an."
	case err == nil && len(mx) > 0:
		return ""
	case err != nil && !(errors.As(err, &dnsErr) && dnsErr.IsNotFound):
		return "Ob " + domain + " E-Mails annimmt, ließ sich gerade nicht prüfen."
	}
	if hosts, err := p.DNS.LookupHost(ctx, domain); err == nil && len(hosts) > 0 {
		return ""
	} else if err != nil && !(errors.As(err, &dnsErr) && dnsErr.IsNotFound) {
		return "Ob " + domain + " E-Mails annimmt, ließ sich gerade nicht prüfen."
	}
	return "Die Domain " + domain + " gibt es nicht, oder sie nimmt keine E-Mails an."
}

// domainForm: labels of letters, digits and hyphens, at least two, the last of letters.
func domainForm(d string) bool {
	ls := strings.Split(d, ".")
	if len(ls) < 2 || len(d) > 253 {
		return false
	}
	for i, l := range ls {
		if l == "" || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
			return false
		}
		for _, r := range l {
			if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-') || (i == len(ls)-1 && unicode.IsDigit(r)) {
				return false
			}
		}
	}
	return len([]rune(ls[len(ls)-1])) >= 2
}

// Geburtstag is what is wrong with a birthday, "" when nothing: a real
// day, not in the future, not more than 120 years ago.
func Geburtstag(s string, now time.Time) string {
	if !caldav.ValidBirthday(s) {
		return "Das ist kein gültiges Datum."
	}
	if strings.HasPrefix(s, "--") {
		return ""
	}
	t, _ := time.Parse("2006-01-02", s)
	switch {
	case t.After(now):
		return "Der Geburtstag liegt in der Zukunft."
	case t.Before(now.AddDate(-120, 0, 0)):
		return "Der Geburtstag liegt mehr als 120 Jahre zurück."
	}
	return ""
}

// Name is a finding when the name changed completely: no word in common
// and most letters different.
func Name(alt, neu string) string {
	a, n := strings.Fields(strings.ToLower(alt)), strings.Fields(strings.ToLower(neu))
	if len(a) == 0 {
		return ""
	}
	for _, x := range a {
		for _, y := range n {
			if len([]rune(x)) > 1 && x == y {
				return ""
			}
		}
	}
	x, y := []rune(strings.Join(a, " ")), []rune(strings.Join(n, " "))
	if abstand(x, y)*2 < max(len(x), len(y)) {
		return ""
	}
	return "Der Name ist ganz anders als vorher. Ist das noch dieselbe Person?"
}

// abstand is the Levenshtein distance.
func abstand(a, b []rune) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := range a {
		cur := make([]int, len(b)+1)
		cur[0] = i + 1
		for j := range b {
			c := prev[j]
			if a[i] != b[j] {
				c++
			}
			cur[j+1] = min(c, prev[j+1]+1, cur[j]+1)
		}
		prev = cur
	}
	return prev[len(b)]
}
