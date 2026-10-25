// Package einkauf is the household's shopping list (haus#118; owner
// 2026-10-25): one list for the household in Radicale (haus/einkauf), its
// lines tasks the phones show too. Lines are added by hand, by Joshua or
// from recipes, scaled to the portions wanted; the same thing in the same
// kind of unit is added up (200 g + 300 g Mehl = 500 g), anything else
// stands side by side. The text of a line is all there is: a phone's edit
// is read back the same way.
package einkauf

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Posten is one line read: how much (0: no amount), in what unit ("g" and
// "ml" for weight and volume, kept in those; "" for a count), of what, and
// what else the line said ("fein gehackt").
type Posten struct {
	Menge   float64
	Einheit string
	Ding    string
	Rest    string
}

// unit is one unit as written and what it is.
type unit struct {
	name, plural string
	base         string  // g, ml, or the unit itself
	factor       float64 // to the base
	whole        bool    // bought whole: scaled, it is rounded up
}

var units = map[string]unit{}

func init() {
	add := func(u unit, spellings ...string) {
		if u.base == "" {
			u.base, u.factor = u.name, 1
		}
		for _, s := range append(spellings, u.name, u.plural) {
			units[strings.ToLower(s)] = u
		}
	}
	add(unit{name: "g", plural: "g", base: "g", factor: 1}, "gr", "gr.", "gramm")
	add(unit{name: "kg", plural: "kg", base: "g", factor: 1000}, "kilo", "kilogramm")
	add(unit{name: "ml", plural: "ml", base: "ml", factor: 1}, "milliliter")
	add(unit{name: "cl", plural: "cl", base: "ml", factor: 10})
	add(unit{name: "dl", plural: "dl", base: "ml", factor: 100})
	add(unit{name: "l", plural: "l", base: "ml", factor: 1000}, "liter", "ltr", "ltr.")
	add(unit{name: "EL", plural: "EL"}, "el.", "esslöffel", "essl.", "eßlöffel")
	add(unit{name: "TL", plural: "TL"}, "tl.", "teelöffel", "teel.")
	add(unit{name: "Prise", plural: "Prisen"})
	add(unit{name: "Msp.", plural: "Msp."}, "msp", "messerspitze", "messerspitzen")
	add(unit{name: "Bund", plural: "Bund"}, "bd.", "bund")
	add(unit{name: "Dose", plural: "Dosen", whole: true}, "ds.")
	add(unit{name: "Packung", plural: "Packungen", whole: true}, "pck.", "pck", "pkg.", "pkt.", "päckchen", "pack")
	add(unit{name: "Becher", plural: "Becher", whole: true})
	add(unit{name: "Glas", plural: "Gläser", whole: true})
	add(unit{name: "Flasche", plural: "Flaschen", whole: true})
	add(unit{name: "Tüte", plural: "Tüten", whole: true})
	add(unit{name: "Zehe", plural: "Zehen"})
	add(unit{name: "Scheibe", plural: "Scheiben"})
	add(unit{name: "Tasse", plural: "Tassen"})
	add(unit{name: "Handvoll", plural: "Handvoll"})
	add(unit{name: "Zweig", plural: "Zweige"})
	add(unit{name: "Stange", plural: "Stangen"})
	add(unit{name: "Kopf", plural: "Köpfe"})
	add(unit{name: "Würfel", plural: "Würfel"})
	add(unit{name: "Blatt", plural: "Blätter"})
	add(unit{name: "Knolle", plural: "Knollen"})
	add(unit{name: "Stück", plural: "Stück", base: "", factor: 1}, "stk", "stk.", "st.")
	units["stück"] = unit{name: "", base: "", factor: 1} // a count, as without a unit
	units["stk"], units["stk."], units["st."] = units["stück"], units["stück"], units["stück"]
}

var fractions = map[rune]float64{'½': .5, '¼': .25, '¾': .75, '⅓': 1. / 3, '⅔': 2. / 3, '⅛': .125}

var (
	reAmount = regexp.MustCompile(`^(\d+(?:[.,]\d+)?\s*[½¼¾⅓⅔⅛]|\d+\s+\d+/\d+|\d+/\d+|\d+(?:[.,]\d+)?|[½¼¾⅓⅔⅛])(?:\s*(?:-|–|bis)\s*(\d+(?:[.,]\d+)?|[½¼¾⅓⅔⅛]))?`)
	reVague  = regexp.MustCompile(`(?i)^(ca\.?|circa|etwa|evtl\.?|eventuell|ggf\.?)\s+`)
)

// number reads one amount as written: 2, 1,5, 1.5, 1/2, 1 1/2, ½, 1½.
func number(s string) (float64, bool) {
	s = strings.TrimSpace(strings.ReplaceAll(s, ",", "."))
	if s == "" {
		return 0, false
	}
	if r := []rune(s); fractions[r[len(r)-1]] > 0 {
		whole := 0.0
		if len(r) > 1 {
			w, err := strconv.ParseFloat(strings.TrimSpace(string(r[:len(r)-1])), 64)
			if err != nil {
				return 0, false
			}
			whole = w
		}
		return whole + fractions[r[len(r)-1]], true
	}
	whole, frac, ok := strings.Cut(s, " ")
	if !ok {
		whole, frac = "", s
	}
	v := 0.0
	if whole != "" {
		w, err := strconv.ParseFloat(whole, 64)
		if err != nil {
			return 0, false
		}
		v = w
	}
	if a, b, ok := strings.Cut(frac, "/"); ok {
		n, err1 := strconv.ParseFloat(a, 64)
		d, err2 := strconv.ParseFloat(b, 64)
		if err1 != nil || err2 != nil || d == 0 {
			return 0, false
		}
		return v + n/d, true
	}
	f, err := strconv.ParseFloat(frac, 64)
	return v + f, err == nil
}

// Parse reads one line as a recipe or a person writes it: "200 g Mehl",
// "1/2 TL Salz", "2-3 Zwiebeln, fein gehackt", "1 Prise Salz", "Milch".
// Of a range the larger end counts.
func Parse(s string) Posten {
	s = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(s), "-*•·"))
	s = reVague.ReplaceAllString(s, "")
	var p Posten
	if m := reAmount.FindStringSubmatch(s); m != nil {
		v, ok := number(m[1])
		if m[2] != "" {
			if hi, ok2 := number(m[2]); ok2 && hi > v {
				v = hi
			}
		}
		if ok {
			p.Menge = v
			s = strings.TrimSpace(s[len(m[0]):])
		}
	}
	if p.Menge > 0 {
		word, rest, _ := strings.Cut(s, " ")
		if u, ok := units[strings.ToLower(word)]; ok {
			p.Menge *= u.factor
			p.Einheit = u.base
			s = strings.TrimSpace(rest)
		}
	} else if word, rest, ok := strings.Cut(s, " "); ok && strings.EqualFold(word, "etwas") {
		p.Rest, s = "etwas", strings.TrimSpace(rest)
	}
	p.Ding, p.Rest = splitRest(s, p.Rest)
	return p
}

// splitRest is the thing and what the line adds to it after a comma or in
// brackets.
func splitRest(s, rest string) (string, string) {
	cut := len(s)
	for _, sep := range []string{",", " (", "("} {
		if i := strings.Index(s, sep); i > 0 && i < cut {
			cut = i
		}
	}
	ding := strings.TrimSpace(s[:cut])
	more := strings.TrimSpace(strings.Trim(strings.TrimSpace(s[cut:]), ",()"))
	if more != "" {
		if rest != "" {
			rest += ", "
		}
		rest += more
	}
	return ding, rest
}

// irregular are plurals the simple rule does not get.
var irregular = map[string]string{"eier": "ei", "äpfel": "apfel", "nüsse": "nuss", "blätter": "blatt"}

// Key is what two lines must share to be the same thing: the thing in
// lower case, a plural "-n"/"-en" and a few others made singular.
func (p Posten) Key() string {
	k := strings.ToLower(strings.Join(strings.Fields(p.Ding), " "))
	if s, ok := irregular[k]; ok {
		return s
	}
	if r := []rune(k); len(r) > 4 && r[len(r)-1] == 'n' {
		k = string(r[:len(r)-1])
	}
	return k
}

// Add is a and b as one line, if they are the same thing in the same kind
// of unit; a line without an amount goes into one with.
func Add(a, b Posten) (Posten, bool) {
	if a.Key() != b.Key() || a.Key() == "" {
		return a, false
	}
	switch {
	case b.Menge == 0:
		return keepRest(a, b), true
	case a.Menge == 0:
		return keepRest(b, a), true
	case a.Einheit != b.Einheit:
		return a, false
	}
	out := a
	if b.Menge > a.Menge {
		out.Ding = b.Ding // the larger amount's word: "1 Ei" and "2 Eier" are "3 Eier"
	}
	out.Menge = a.Menge + b.Menge
	out = keepRest(out, b)
	return out, true
}

// keepRest keeps what the line adds only if both said the same.
func keepRest(a, b Posten) Posten {
	if a.Rest != b.Rest {
		a.Rest = ""
	}
	return a
}

// Mal is the line for f times the portions, rounded as one buys and
// measures: grams and millilitres to 5 (from 50), spoons and pinches to a
// half, what is bought whole up to whole ones, a count up to a half.
func (p Posten) Mal(f float64) Posten {
	if p.Menge == 0 || f <= 0 || f == 1 {
		return p
	}
	v := p.Menge * f
	switch u := units[strings.ToLower(p.Einheit)]; {
	case p.Einheit == "g" || p.Einheit == "ml":
		if v >= 50 {
			v = math.Round(v/5) * 5
		} else {
			v = math.Max(1, math.Round(v))
		}
	case u.whole:
		v = math.Ceil(v - 1e-9)
	case p.Einheit == "":
		v = math.Ceil(v*2-1e-9) / 2
	default:
		v = math.Max(.5, math.Round(v*2)/2)
	}
	p.Menge = v
	return p
}

// String is the line as the list shows it: "500 g Mehl", "1,5 kg
// Kartoffeln", "½ TL Salz", "3 Eier", "Milch".
func (p Posten) String() string {
	var parts []string
	if p.Menge > 0 {
		v, name := p.Menge, p.Einheit
		switch p.Einheit {
		case "g":
			if v >= 1000 {
				v, name = v/1000, "kg"
			}
		case "ml":
			if v >= 1000 {
				v, name = v/1000, "l"
			}
		default:
			if u, ok := units[strings.ToLower(p.Einheit)]; ok && v > 1 {
				name = u.plural
			}
		}
		parts = append(parts, amount(v, name == "g" || name == "ml" || name == "kg" || name == "l"))
		if name != "" {
			parts = append(parts, name)
		}
	} else if p.Rest == "etwas" {
		parts = append(parts, "etwas")
	}
	parts = append(parts, p.Ding)
	s := strings.Join(parts, " ")
	if p.Rest != "" && p.Rest != "etwas" {
		s += ", " + p.Rest
	}
	return s
}

// amount is a number the German way: whole, a common fraction for spoons
// and counts, else with a decimal comma.
func amount(v float64, metric bool) string {
	whole, frac := math.Modf(v)
	if !metric {
		for r, f := range map[string]float64{"½": .5, "¼": .25, "¾": .75, "⅓": 1. / 3, "⅔": 2. / 3} {
			if math.Abs(frac-f) < .01 {
				if whole == 0 {
					return r
				}
				return strconv.Itoa(int(whole)) + r
			}
		}
	}
	if math.Abs(frac) < .005 {
		return strconv.Itoa(int(math.Round(v)))
	}
	return strings.ReplaceAll(strconv.FormatFloat(math.Round(v*100)/100, 'f', -1, 64), ".", ",")
}
