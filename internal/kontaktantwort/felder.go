package kontaktantwort

import (
	"slices"
	"strings"

	"github.com/mdopp/homes/internal/caldav"
)

// Adresse is a postal address as the page asks for it.
type Adresse struct {
	Strasse string `json:"strasse,omitempty"`
	PLZ     string `json:"plz,omitempty"`
	Ort     string `json:"ort,omitempty"`
	Land    string `json:"land,omitempty"`
}

// Leer says whether every part is blank.
func (a Adresse) Leer() bool { return a == Adresse{} }

// Zeile is the address on one line, as a letter has it.
func (a Adresse) Zeile() string {
	var ps []string
	for _, p := range []string{a.Strasse, strings.TrimSpace(a.PLZ + " " + a.Ort), a.Land} {
		if p != "" {
			ps = append(ps, p)
		}
	}
	return strings.Join(ps, ", ")
}

func (a Adresse) dav() *caldav.Address {
	return &caldav.Address{Street: a.Strasse, Code: a.PLZ, Locality: a.Ort, Country: a.Land}
}

// Telefon is a number and its kind: mobil, privat, geschaeftlich, or "".
type Telefon struct {
	Nummer string `json:"nummer"`
	Art    string `json:"art,omitempty"`
}

// Arten are the kinds of numbers the page offers (owner 2026-11-07).
var Arten = []string{"mobil", "privat", "geschaeftlich"}

// Felder are the fields the page shows and the contact may check and add
// (owner 2026-11-07): first and last name, numbers with their kind, e-mail
// addresses, postal addresses, the birthday (day, month, the year if they
// like), company and position. Nothing else. In an answer each list keeps
// the places of the fields shown: an empty one at a place is that field
// taken away; what follows is new.
type Felder struct {
	Vorname    string    `json:"vorname,omitempty"`
	Nachname   string    `json:"nachname,omitempty"`
	Telefone   []Telefon `json:"telefone,omitempty"`
	Mails      []string  `json:"mails,omitempty"`
	Adressen   []Adresse `json:"adressen,omitempty"`
	Geburtstag string    `json:"geburtstag,omitempty"` // JJJJ-MM-TT, or --MM-TT without a year
	Firma      string    `json:"firma,omitempty"`
	Position   string    `json:"position,omitempty"`
}

// Name is the name as a list shows it: first and last, else the company.
func (f Felder) Name() string {
	if n := strings.TrimSpace(f.Vorname + " " + f.Nachname); n != "" {
		return n
	}
	return f.Firma
}

// Grenze is how many of each kind an answer may hold, and how long a value.
const (
	GrenzeJeArt = 12
	GrenzeWert  = 200
)

// FelderVon are a card's fields. A card without N (Homes' own write FN
// only) is split: the last word the last name.
func FelderVon(ct caldav.Contact) Felder {
	f := Felder{Vorname: eineZeile(ct.Given), Nachname: eineZeile(ct.Family), Mails: kopie(ct.Emails), Firma: eineZeile(ct.Company), Position: eineZeile(ct.Title)}
	if f.Vorname == "" && f.Nachname == "" && eineZeile(ct.Name) != eineZeile(ct.Company) {
		if ws := strings.Fields(eineZeile(ct.Name)); len(ws) > 0 {
			f.Vorname, f.Nachname = strings.Join(ws[:len(ws)-1], " "), ws[len(ws)-1]
		}
	}
	for i, p := range ct.Phones {
		if p = eineZeile(p); p != "" {
			t := Telefon{Nummer: p}
			if i < len(ct.PhoneKinds) {
				t.Art = ct.PhoneKinds[i]
			}
			f.Telefone = append(f.Telefone, t)
		}
	}
	if caldav.ValidBirthday(ct.Birthday) {
		f.Geburtstag = ct.Birthday
	}
	for _, a := range ct.Postal {
		f.Adressen = append(f.Adressen, Adresse{Strasse: a.Street, PLZ: a.Code, Ort: a.Locality, Land: a.Country})
	}
	return f
}

func kopie(vs []string) []string {
	var out []string
	for _, v := range vs {
		if v = eineZeile(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// eineZeile is s on one line, trimmed, without control characters, at
// most GrenzeWert characters.
func eineZeile(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > GrenzeWert {
		s = string(r[:GrenzeWert])
	}
	return s
}

// Sauber is an answer's fields as they are kept: each value on one line
// and cut to its length, each list cut to GrenzeJeArt, a kind one of Arten.
func Sauber(f Felder) Felder {
	out := Felder{Vorname: eineZeile(f.Vorname), Nachname: eineZeile(f.Nachname), Geburtstag: strings.TrimSpace(f.Geburtstag),
		Firma: eineZeile(f.Firma), Position: eineZeile(f.Position)}
	for i, t := range f.Telefone {
		if i < GrenzeJeArt {
			if !slices.Contains(Arten, t.Art) {
				t.Art = ""
			}
			out.Telefone = append(out.Telefone, Telefon{eineZeile(t.Nummer), t.Art})
		}
	}
	for i, m := range f.Mails {
		if i < GrenzeJeArt {
			out.Mails = append(out.Mails, eineZeile(m))
		}
	}
	for i, a := range f.Adressen {
		if i < GrenzeJeArt {
			out.Adressen = append(out.Adressen, Adresse{eineZeile(a.Strasse), eineZeile(a.PLZ), eineZeile(a.Ort), eineZeile(a.Land)})
		}
	}
	return out
}

// Aenderung is one field, before → after; Vorher "" is new, Nachher ""
// taken away. A number carries its kind before and after.
type Aenderung struct {
	Art            string   `json:"art"` // vorname | nachname | telefon | mail | adresse | geburtstag | firma | position
	Vorher         string   `json:"vorher,omitempty"`
	Nachher        string   `json:"nachher,omitempty"`
	VorherArt      string   `json:"vorher_art,omitempty"`
	NachherArt     string   `json:"nachher_art,omitempty"`
	VorherAdresse  *Adresse `json:"vorher_adresse,omitempty"`
	NachherAdresse *Adresse `json:"nachher_adresse,omitempty"`
	Hinweise       []string `json:"hinweise,omitempty"` // what the checks found
	Stand          string   `json:"stand,omitempty"`
}

// Vergleich is every field the answer changes, in the page's order. A
// number written another way (spaces, dashes) with the same kind is no
// change; a name or company cleared is none either (the card keeps it).
func Vergleich(vorher, nachher Felder) []Aenderung {
	var out []Aenderung
	einzeln := func(art, v, n string, leerErlaubt bool) {
		if v != n && (n != "" || leerErlaubt) {
			out = append(out, Aenderung{Art: art, Vorher: v, Nachher: n})
		}
	}
	einzeln("vorname", vorher.Vorname, nachher.Vorname, vorher.Nachname != "" || nachher.Nachname != "")
	einzeln("nachname", vorher.Nachname, nachher.Nachname, vorher.Vorname != "" || nachher.Vorname != "")
	out = append(out, telefone(vorher.Telefone, nachher.Telefone)...)
	out = append(out, mails(vorher.Mails, nachher.Mails)...)
	out = append(out, adressen(vorher.Adressen, nachher.Adressen)...)
	einzeln("geburtstag", vorher.Geburtstag, nachher.Geburtstag, true)
	einzeln("firma", vorher.Firma, nachher.Firma, true)
	einzeln("position", vorher.Position, nachher.Position, true)
	return out
}

// at is the place i of vs, the zero value past its end.
func at[T any](vs []T, i int) T {
	var zero T
	if i < len(vs) {
		return vs[i]
	}
	return zero
}

func telefone(vorher, nachher []Telefon) []Aenderung {
	var out []Aenderung
	for i := range max(len(vorher), len(nachher)) {
		v, n := at(vorher, i), at(nachher, i)
		if ziffern(v.Nummer) == ziffern(n.Nummer) && (v.Art == n.Art || n.Nummer == "") {
			continue
		}
		a := Aenderung{Art: "telefon", Vorher: v.Nummer, Nachher: n.Nummer, VorherArt: v.Art, NachherArt: n.Art}
		if n.Nummer == "" {
			a.NachherArt = ""
		}
		out = append(out, a)
	}
	return out
}

func mails(vorher, nachher []string) []Aenderung {
	var out []Aenderung
	for i := range max(len(vorher), len(nachher)) {
		if v, n := at(vorher, i), at(nachher, i); !strings.EqualFold(v, n) {
			out = append(out, Aenderung{Art: "mail", Vorher: v, Nachher: n})
		}
	}
	return out
}

func adressen(vorher, nachher []Adresse) []Aenderung {
	var out []Aenderung
	for i := range max(len(vorher), len(nachher)) {
		v, n := at(vorher, i), at(nachher, i)
		if v == n || (i >= len(vorher) && n.Leer()) {
			continue
		}
		a := Aenderung{Art: "adresse", Vorher: v.Zeile(), Nachher: n.Zeile()}
		if !v.Leer() {
			a.VorherAdresse = &v
		}
		if !n.Leer() {
			a.NachherAdresse = &n
		}
		out = append(out, a)
	}
	return out
}

func ziffern(s string) string {
	var b strings.Builder
	for i, r := range strings.TrimSpace(s) {
		if (r >= '0' && r <= '9') || (r == '+' && i == 0) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// davProp is each kind's vCard property.
var davProp = map[string]string{"vorname": "N-GIVEN", "nachname": "N-FAMILY", "telefon": "TEL", "mail": "EMAIL", "adresse": "ADR",
	"geburtstag": "BDAY", "firma": "ORG", "position": "TITLE"}

// Korrekturen are the accepted changes as Radicale writes them, and the
// number of each among f's changes.
func Korrekturen(f Frage) ([]caldav.Korrektur, []int) {
	var ks []caldav.Korrektur
	var nr []int
	for i, a := range f.Aenderungen {
		if a.Stand != Angenommen {
			continue
		}
		k := caldav.Korrektur{Prop: davProp[a.Art], Alt: a.Vorher, Neu: a.Nachher, Art: a.NachherArt}
		if a.Art == "adresse" {
			k.Alt, k.Neu = "", ""
			if a.VorherAdresse != nil {
				k.AltAdr = a.VorherAdresse.dav()
			}
			if a.NachherAdresse != nil {
				k.NeuAdr = a.NachherAdresse.dav()
			}
		}
		ks = append(ks, k)
		nr = append(nr, i)
	}
	return ks, nr
}
