package einkauf

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"github.com/mdopp/haus/internal/notes"
)

// Rezept is what the list needs of a recipe note: its title, the portions
// it is written for (0: not said), its ingredients as written.
type Rezept struct {
	Name, Titel string
	Portionen   int
	Zutaten     []string
}

var rePortionen = regexp.MustCompile(`(?i)\bfür\s+(\d+)\b`)

// AusNotiz reads a recipe note as Merken writes it (and a person might):
// "*Für 4 Portionen · …*" near the top, the ingredients as a list under a
// "Zutaten" heading. A heading in the list ("Für den Teig:") is left out.
func AusNotiz(n notes.Note) Rezept {
	r := Rezept{Name: n.Name, Titel: n.Title}
	_, body := notes.SplitKopf(n.Text)
	in := false
	for _, l := range strings.Split(body, "\n") {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "#") {
			in = strings.Contains(strings.ToLower(t), "zutaten")
			continue
		}
		if r.Portionen == 0 && !in && len(r.Zutaten) == 0 {
			if m := rePortionen.FindStringSubmatch(t); m != nil {
				r.Portionen, _ = strconv.Atoi(m[1])
			}
		}
		if !in {
			continue
		}
		if z, ok := strings.CutPrefix(t, "- "); ok {
			t = z
		} else if z, ok := strings.CutPrefix(t, "* "); ok {
			t = z
		} else {
			continue
		}
		if t = strings.TrimSpace(t); t != "" && !strings.HasSuffix(t, ":") {
			r.Zutaten = append(r.Zutaten, t)
		}
	}
	return r
}

// Zeile is a line to go on the list and where it came from.
type Zeile struct {
	Posten   Posten
	Herkunft []string
}

// Text is the line as written to the list.
func (z Zeile) Text() string { return z.Posten.String() }

// Merge adds up the same things among lines, in their first order; the
// origins are kept, each once.
func Merge(in []Zeile) []Zeile {
	var out []Zeile
next:
	for _, z := range in {
		if z.Posten.Ding == "" {
			continue
		}
		for i := range out {
			if sum, ok := Add(out[i].Posten, z.Posten); ok {
				out[i].Posten = sum
				out[i].Herkunft = union(out[i].Herkunft, z.Herkunft)
				continue next
			}
		}
		out = append(out, Zeile{Posten: z.Posten, Herkunft: union(nil, z.Herkunft)})
	}
	return out
}

// Fuer is the recipes' ingredients for portionen each (0: as written),
// added up across them, each line with its recipes.
func Fuer(rs []Rezept, portionen int) []Zeile {
	var all []Zeile
	for _, r := range rs {
		f := 1.0
		if portionen > 0 && r.Portionen > 0 {
			f = float64(portionen) / float64(r.Portionen)
		}
		for _, z := range r.Zutaten {
			all = append(all, Zeile{Posten: Parse(z).Mal(f), Herkunft: []string{r.Titel}})
		}
	}
	return Merge(all)
}

func union(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range append(append([]string{}, a...), b...) {
		if s = strings.TrimSpace(s); s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// herkunft is origins as the list keeps them, and back.
func herkunft(h []string) string { return strings.Join(h, ", ") }

func herkunftOf(s string) []string { return union(nil, strings.Split(s, ",")) }

// Rezepte are the notes of kind rezept, newest first.
func Rezepte(ctx context.Context, d *notes.Dir) ([]notes.Entry, error) {
	return d.ByArt(ctx, "rezept", 500)
}

// Finden is the recipes named, as someone says them ("Lasagne", "das
// Curry"): the recipe whose title is the name, else the newest whose title
// holds it. It says which names it did not find.
func Finden(ctx context.Context, d *notes.Dir, names []string) ([]Rezept, []string, error) {
	all, err := Rezepte(ctx, d)
	if err != nil {
		return nil, nil, err
	}
	var out []Rezept
	var missing []string
	for _, name := range names {
		want := strings.ToLower(strings.TrimSpace(name))
		for _, art := range []string{"das ", "die ", "den ", "der ", "ein ", "eine "} {
			want = strings.TrimPrefix(want, art)
		}
		if want == "" {
			continue
		}
		var hit *notes.Entry
		for i, e := range all {
			t := strings.ToLower(e.Title)
			if t == want {
				hit = &all[i]
				break
			}
			if hit == nil && strings.Contains(t, want) {
				hit = &all[i]
			}
		}
		if hit == nil {
			missing = append(missing, strings.TrimSpace(name))
			continue
		}
		n, err := d.Read(ctx, hit.Name)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, AusNotiz(n))
	}
	return out, missing, nil
}
