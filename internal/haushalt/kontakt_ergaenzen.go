package haushalt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/mail"
	"regexp"
	"sort"
	"strings"

	"github.com/mdopp/haus/internal/caldav"
	"github.com/mdopp/haus/internal/mcpserver"
)

// kontakt_ergaenzen (haus#163): a firm from a document into the family
// address book. Laib's paperless helper calls it; its answer is a German
// line for people and one JSON line for the helper, a status per field.

// the statuses a field is answered with
const (
	stAngelegt   = "angelegt"   // on the new card
	stErgaenzt   = "ergaenzt"   // added to the card
	stVorhanden  = "vorhanden"  // the card has it, or already another one that is kept
	stAbweichend = "abweichend" // a different IBAN: never written, the document wants checking
	stAbgelehnt  = "abgelehnt"  // not written: not a value, or not a card Haus may add to
)

// ergaenzenFields is the answer's field order.
var ergaenzenFields = []string{"firma", "ansprechpartner", "telefon", "email", "iban"}

var fieldLabel = map[string]string{"firma": "Firma", "ansprechpartner": "Ansprechpartner", "telefon": "Telefon", "email": "E-Mail", "iban": "IBAN"}

// ibanLen is the length of the IBANs of the countries the family will meet
// most; another country's must be 15 to 34 characters.
var ibanLen = map[string]int{"DE": 22, "AT": 20, "CH": 21, "LI": 21, "NL": 18, "BE": 16, "LU": 20, "FR": 27, "IT": 27, "ES": 24,
	"PL": 28, "CZ": 24, "DK": 18, "SE": 24, "NO": 15, "FI": 18, "IE": 22, "GB": 22, "PT": 25}

var ibanRE = regexp.MustCompile(`^[A-Z]{2}[0-9]{2}[A-Z0-9]{11,30}$`)

// normIBAN is s as an IBAN without spaces, upper case, if its form and
// check digits (ISO 13616, mod 97) hold.
func normIBAN(s string) (string, bool) {
	s = strings.ToUpper(strings.Join(strings.Fields(s), ""))
	if !ibanRE.MatchString(s) {
		return "", false
	}
	if n, ok := ibanLen[s[:2]]; ok && len(s) != n {
		return "", false
	}
	var d strings.Builder
	for _, r := range s[4:] + s[:4] {
		if r >= 'A' && r <= 'Z' {
			fmt.Fprintf(&d, "%d", r-'A'+10)
		} else {
			d.WriteRune(r)
		}
	}
	n, _ := new(big.Int).SetString(d.String(), 10)
	return s, new(big.Int).Mod(n, big.NewInt(97)).Int64() == 1
}

// groupIBAN shows an IBAN in groups of four.
func groupIBAN(s string) string {
	var parts []string
	for len(s) > 4 {
		parts, s = append(parts, s[:4]), s[4:]
	}
	return strings.Join(append(parts, s), " ")
}

// ibanInText finds the IBANs written in a note (grouped or not).
var ibanInText = regexp.MustCompile(`\b[A-Za-z]{2}[0-9]{2}(?: ?[A-Za-z0-9]{1,4}){3,8}\b`)

// cardIBANs are the valid IBANs a card names: its X-IBAN and its note.
func cardIBANs(ct caldav.Contact) []string {
	var out []string
	seen := map[string]bool{}
	for _, v := range append(append([]string{}, ct.IBANs...), ibanInText.FindAllString(ct.Note, -1)...) {
		if i, ok := normIBAN(v); ok && !seen[i] {
			out, seen[i] = append(out, i), true
		}
	}
	return out
}

// firmKey is a firm's name for comparing: no case, no spaces.
func firmKey(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), "")) }

// noteLine is the value of the note's line "label: …", if any.
func noteLine(note, label string) (string, bool) {
	for _, l := range strings.Split(note, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(l), label+":"); ok {
			return strings.TrimSpace(v), true
		}
	}
	return "", false
}

type ergaenzung struct {
	Status  string            `json:"status"` // angelegt, ergaenzt, unveraendert, mehrdeutig
	Kontakt string            `json:"kontakt,omitempty"`
	Felder  map[string]string `json:"felder"`
	Gruende map[string]string `json:"gruende,omitempty"`
}

func (e *ergaenzung) set(field, status, grund string) {
	e.Felder[field] = status
	if grund != "" {
		e.Gruende[field] = grund
	}
}

// ergaenzenArgs are the values checked: each one valid, else refused for
// itself with its reason.
type ergaenzenArgs struct {
	firma, ansprechpartner, telefon, email, iban, quelle string
}

func readErgaenzen(a map[string]any, e *ergaenzung) (ergaenzenArgs, error) {
	v := ergaenzenArgs{firma: oneLine(mcpserver.Str(a, "firma")), ansprechpartner: oneLine(mcpserver.Str(a, "ansprechpartner")),
		quelle: strings.TrimSpace(mcpserver.Str(a, "quelle"))}
	if v.firma == "" {
		return v, errors.New("firma fehlt")
	}
	if err := tooLong("firma", v.firma, MaxField); err != nil {
		return v, err
	}
	if err := tooLong("ansprechpartner", v.ansprechpartner, MaxField); err != nil {
		return v, err
	}
	if v.quelle != "" && (!httpURL(v.quelle) || len(v.quelle) > 500 || strings.ContainsAny(v.quelle, " \t\r\n")) {
		return v, fmt.Errorf("quelle: %q ist kein Link (http/https)", cut(oneLine(v.quelle), 100))
	}
	if p := strings.TrimSpace(mcpserver.Str(a, "telefon")); p != "" {
		if !phoneRE.MatchString(p) || len(digits(p)) < 3 {
			e.set("telefon", stAbgelehnt, fmt.Sprintf("„%s“ ist keine Telefonnummer", cut(oneLine(p), 40)))
		} else {
			v.telefon = p
		}
	}
	if m := strings.TrimSpace(mcpserver.Str(a, "email")); m != "" {
		if addr, err := mail.ParseAddress(m); err != nil || addr.Address != m || addr.Name != "" || len(m) > 254 {
			e.set("email", stAbgelehnt, fmt.Sprintf("„%s“ ist keine E-Mail-Adresse", cut(oneLine(m), 100)))
		} else {
			v.email = m
		}
	}
	if i := mcpserver.Str(a, "iban"); strings.TrimSpace(i) != "" {
		if n, ok := normIBAN(i); ok {
			v.iban = n
		} else {
			e.set("iban", stAbgelehnt, fmt.Sprintf("„%s“ ist keine gültige IBAN (Prüfziffer oder Länge falsch)", cut(oneLine(i), 50)))
		}
	}
	return v, nil
}

// findFirm is the card the firm is: by ORG or FN, else the email, else the
// IBAN; several at one step are narrowed to the firm cards Haus made.
func findFirm(all []caldav.Card, v ergaenzenArgs) []caldav.Card {
	steps := []func(caldav.Contact) bool{
		func(ct caldav.Contact) bool {
			k := firmKey(v.firma)
			return firmKey(ct.Org) == k || firmKey(ct.Name) == k
		},
		func(ct caldav.Contact) bool {
			for _, m := range ct.Emails {
				if v.email != "" && strings.EqualFold(strings.TrimSpace(m), v.email) {
					return true
				}
			}
			return false
		},
		func(ct caldav.Contact) bool {
			for _, i := range cardIBANs(ct) {
				if v.iban != "" && i == v.iban {
					return true
				}
			}
			return false
		},
	}
	for _, match := range steps {
		var hits, firms []caldav.Card
		for _, cd := range all {
			if match(cd.Contact) {
				hits = append(hits, cd)
				if cd.IsFirm() {
					firms = append(firms, cd)
				}
			}
		}
		if len(hits) > 1 && len(firms) == 1 {
			return firms
		}
		if len(hits) > 0 {
			return hits
		}
	}
	return nil
}

func kontaktErgaenzen(ctx context.Context, c *caldav.Client, cc string, a map[string]any) (string, error) {
	for try := 0; ; try++ {
		out, err := ergaenzenOnce(ctx, c, cc, a)
		if errors.Is(err, caldav.ErrChanged) && try == 0 {
			continue // a phone saved the card meanwhile: read it again
		}
		if errors.Is(err, caldav.ErrChanged) {
			return "", errors.New("der Kontakt wurde gerade woanders geändert; später noch einmal versuchen")
		}
		return out, err
	}
}

func ergaenzenOnce(ctx context.Context, c *caldav.Client, cc string, a map[string]any) (string, error) {
	e := ergaenzung{Felder: map[string]string{}, Gruende: map[string]string{}}
	v, err := readErgaenzen(a, &e)
	if err != nil {
		return "", err
	}
	all, partial, err := c.Cards(ctx)
	if err != nil {
		return "", err
	}
	hits := findFirm(all, v)
	switch {
	case len(hits) == 0 && partial:
		return "", errors.New("das Adressbuch ist zu groß, um sicher zu sein, dass die Firma fehlt; nichts angelegt")
	case len(hits) == 0:
		return ergaenzenNew(ctx, c, v, e)
	case len(hits) > 1:
		e.Status = "mehrdeutig"
		for _, f := range ergaenzenFields {
			if f == "firma" {
				e.set(f, stVorhanden, "")
			} else if given(v, f) {
				e.set(f, stAbgelehnt, "mehrere Kontakte passen")
			}
		}
		names := make([]string, 0, len(hits))
		for _, h := range hits {
			names = append(names, contactLine(h.Contact)+" · Kennung "+cut(oneLine(h.UID), MaxField))
		}
		sort.Strings(names)
		return answer("Mehrere Kontakte passen zu „"+cut(v.firma, MaxField)+"“; nichts geschrieben, bitte im Adressbuch zusammenführen:\n"+
			strings.Join(names, "\n"), e), nil
	}
	return ergaenzenOne(ctx, c, cc, v, hits[0], e)
}

// given says whether the field came with a valid value.
func given(v ergaenzenArgs, f string) bool {
	switch f {
	case "ansprechpartner":
		return v.ansprechpartner != ""
	case "telefon":
		return v.telefon != ""
	case "email":
		return v.email != ""
	case "iban":
		return v.iban != ""
	}
	return true
}

func ergaenzenNew(ctx context.Context, c *caldav.Client, v ergaenzenArgs, e ergaenzung) (string, error) {
	ct := caldav.Contact{UID: caldav.NewUUID(), Name: v.firma, Org: v.firma, Categories: []string{caldav.FirmCategory}}
	var note []string
	e.set("firma", stAngelegt, "")
	if v.ansprechpartner != "" {
		note = append(note, "Ansprechpartner: "+v.ansprechpartner)
		e.set("ansprechpartner", stAngelegt, "")
	}
	if v.telefon != "" {
		ct.Phones = []string{v.telefon}
		e.set("telefon", stAngelegt, "")
	}
	if v.email != "" {
		ct.Emails = []string{v.email}
		e.set("email", stAngelegt, "")
	}
	if v.iban != "" {
		ct.IBANs = []string{v.iban}
		note = append(note, "IBAN: "+groupIBAN(v.iban))
		e.set("iban", stAngelegt, "")
	}
	if v.quelle != "" {
		note = append(note, "Quelle: "+v.quelle)
	}
	ct.Note = strings.Join(note, "\n")
	col, err := c.PutContact(ctx, ct)
	if err != nil {
		return "", err
	}
	e.Status, e.Kontakt = stAngelegt, ct.UID
	return answer("Kontakt „"+cut(v.firma, MaxField)+"“ angelegt im Adressbuch „"+col.Name+"“"+said(e)+".", e), nil
}

func ergaenzenOne(ctx context.Context, c *caldav.Client, cc string, v ergaenzenArgs, cd caldav.Card, e ergaenzung) (string, error) {
	firm := cd.IsFirm()
	const notOurs = "kein Firmenkontakt von Haus (ohne Kategorie Firma): Haus ergänzt ihn nicht"
	var add, note []string
	e.set("firma", stVorhanden, "")
	if v.ansprechpartner != "" {
		have, ok := noteLine(cd.Note, "Ansprechpartner")
		switch {
		case ok && strings.EqualFold(have, v.ansprechpartner):
			e.set("ansprechpartner", stVorhanden, "")
		case ok:
			e.set("ansprechpartner", stVorhanden, "eingetragen ist „"+cut(have, MaxField)+"“; bleibt")
		case !firm:
			e.set("ansprechpartner", stAbgelehnt, notOurs)
		default:
			note = append(note, "Ansprechpartner: "+v.ansprechpartner)
			e.set("ansprechpartner", stErgaenzt, "")
		}
	}
	if v.telefon != "" {
		have := false
		for _, p := range cd.Phones {
			have = have || international(p, cc) == international(v.telefon, cc)
		}
		switch {
		case have:
			e.set("telefon", stVorhanden, "")
		case !firm:
			e.set("telefon", stAbgelehnt, notOurs)
		default:
			add = append(add, "TEL:"+caldav.EscapeText(v.telefon))
			e.set("telefon", stErgaenzt, "")
		}
	}
	if v.email != "" {
		have := false
		for _, m := range cd.Emails {
			have = have || strings.EqualFold(strings.TrimSpace(m), v.email)
		}
		switch {
		case have:
			e.set("email", stVorhanden, "")
		case !firm:
			e.set("email", stAbgelehnt, notOurs)
		default:
			add = append(add, "EMAIL:"+caldav.EscapeText(v.email))
			e.set("email", stErgaenzt, "")
		}
	}
	if v.iban != "" {
		known := cardIBANs(cd.Contact)
		has := false
		for _, i := range known {
			has = has || i == v.iban
		}
		switch {
		case has:
			e.set("iban", stVorhanden, "")
		case len(known) > 0:
			shown := make([]string, len(known))
			for i, k := range known {
				shown[i] = groupIBAN(k)
			}
			e.set("iban", stAbweichend, "im Adressbuch steht "+strings.Join(shown, ", ")+", im Dokument "+groupIBAN(v.iban)+"; nicht übernommen")
		case !firm:
			e.set("iban", stAbgelehnt, notOurs)
		default:
			add = append(add, "X-IBAN:"+v.iban)
			note = append(note, "IBAN: "+groupIBAN(v.iban))
			e.set("iban", stErgaenzt, "")
		}
	}
	e.Kontakt = cd.UID
	name := cut(orDash(cd.Name), MaxField)
	if len(add) == 0 && len(note) == 0 {
		e.Status = "unveraendert"
		return answer("Kontakt „"+name+"“ ist schon im Adressbuch; nichts geändert"+said(e)+".", e), nil
	}
	if v.quelle != "" && !strings.Contains(cd.Note, v.quelle) {
		note = append(note, "Quelle: "+v.quelle)
	}
	if err := c.AmendCard(ctx, cd, add, strings.Join(note, "\n")); err != nil {
		return "", err
	}
	e.Status = stErgaenzt
	return answer("Kontakt „"+name+"“ ergänzt"+said(e)+".", e), nil
}

// said is the fields by status, in German: ": Telefon ergänzt, IBAN weicht ab (…)".
func said(e ergaenzung) string {
	var parts []string
	for _, f := range ergaenzenFields {
		st, ok := e.Felder[f]
		if !ok || f == "firma" {
			continue
		}
		p := fieldLabel[f] + " "
		switch st {
		case stAngelegt:
			p += "eingetragen"
		case stErgaenzt:
			p += "ergänzt"
		case stVorhanden:
			p += "schon da"
		case stAbweichend:
			p += "weicht ab, bitte das Dokument prüfen"
		case stAbgelehnt:
			p += "nicht übernommen"
		}
		if g := e.Gruende[f]; g != "" {
			p += " (" + g + ")"
		}
		parts = append(parts, p)
	}
	if len(parts) == 0 {
		return ""
	}
	return ": " + strings.Join(parts, "; ")
}

// answer is the German line(s), then the JSON line the caller reads.
func answer(text string, e ergaenzung) string {
	b, _ := json.Marshal(e)
	return text + "\n" + string(b)
}
