package merken

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/mdopp/haus/internal/judge"
	"github.com/mdopp/haus/internal/notes"
)

// Gemerkt is what Merken did: the note's file and title, its kind, a line
// to say, whether it was there already, and whether only the link and
// title could be kept.
type Gemerkt struct {
	Name, Titel, Art, Kurz         string
	Schon, Teilweise, Aktualisiert bool
}

// Merken makes the note for a link: hint is what came with it ("für
// Weihnachten"; #words in it are tags). A page that cannot be read is kept
// as link and title. model may be nil (no summary, the page's own
// description instead). A link kept already — shared again, maybe with
// ?si=… or utm_… — is not kept twice (owner 2026-10-26): Haus says so, and
// brings the note up to the page's new state when the page changed and
// nobody edited the note since; a note someone edited is left as it is.
func Merken(ctx context.Context, f Fetcher, model *judge.Client, d *notes.Dir, link, hint string) (Gemerkt, error) {
	u, err := Address(link)
	if err != nil {
		return Gemerkt{}, err
	}
	old, had, lerr := d.ByQuelleWie(ctx, u.String(), Schluessel)
	had = had && lerr == nil
	final, page, err := f.Fetch(ctx, u)
	if err != nil && !errors.Is(err, ErrNoPage) { // no address, at home, or no answer at all: nothing to keep
		if had {
			return schon(old, " Die Seite antwortet gerade nicht, darum weiß ich nicht, ob sie sich geändert hat."), nil
		}
		return Gemerkt{}, err
	}
	var s Seite
	partial := err != nil
	if partial {
		log.Printf("haus: merken: %s: %v", u.Host, err)
		s = Seite{URL: u.String(), Titel: u.Host + u.EscapedPath()}
	} else {
		s = Lesen(final, page)
		s.URL = u.String() // the link as sent; a redirect's target is no better to keep
	}
	if had {
		if partial {
			return schon(old, " Die Seite ließ sich gerade nicht lesen, darum weiß ich nicht, ob sie sich geändert hat."), nil
		}
		return auffrischen(ctx, model, d, old, s, hint)
	}
	g := gist(ctx, model, s, partial)
	title, text, k := s.Notiz(g, hint)
	if partial {
		text += "\n\nDie Seite ließ sich nicht lesen; gemerkt sind Link und Titel."
	} else {
		k.Stand = s.Fingerabdruck()
	}
	name, err := d.CreateWith(title, text, k)
	if err != nil {
		return Gemerkt{}, err
	}
	stempeln(ctx, d, name)
	out := Gemerkt{Name: name, Titel: title, Art: k.Art, Teilweise: partial}
	out.Kurz = kurz(s, g, k, partial)
	return out, nil
}

// gist is the house model's summary of a page that is no recipe (nor an
// album with a short review of its own); none without the model.
func gist(ctx context.Context, model *judge.Client, s Seite, partial bool) judge.Gist {
	if model == nil || partial || s.Rezept != nil || (s.Album != nil && len([]rune(s.Album.Rezension)) <= 600 && s.Album.Rezension != "") {
		return judge.Gist{}
	}
	gctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	g, err := model.Summarize(gctx, s.Titel, first(s.Text, s.Beschreibung))
	if err != nil {
		log.Printf("haus: merken: the house model: %v", err)
		return judge.Gist{}
	}
	return g
}

// schon is a link kept already, said so, with what more there is to say.
func schon(e notes.Entry, more string) Gemerkt {
	return Gemerkt{Name: e.Name, Titel: e.Title, Art: e.Kopf.Art, Schon: true, Kurz: "Das ist schon gemerkt: „" + e.Title + "“." + more}
}

// stempeln keeps the fingerprint of the note's text as Haus wrote it, so a
// later share sees whether someone edited it.
func stempeln(ctx context.Context, d *notes.Dir, name string) {
	n, err := d.Read(ctx, name)
	if err != nil || n.Truncated {
		return
	}
	k, body := notes.SplitKopf(n.Text)
	k.Inhalt = notes.Inhalt(body)
	if err := d.Update(name, k.With(body), n.Changed); err != nil {
		log.Printf("haus: merken: %s: %v", name, err)
	}
}

// auffrischen is a link shared again: the note as it is when the page did
// not change, the note made again when it did and nobody edited it, the
// note left alone when someone did; new #tags from the hint are added
// either way.
func auffrischen(ctx context.Context, model *judge.Client, d *notes.Dir, e notes.Entry, s Seite, hint string) (Gemerkt, error) {
	n, err := d.Read(ctx, e.Name)
	if err != nil || n.Truncated {
		return schon(e, ""), nil
	}
	k, body := notes.SplitKopf(n.Text)
	_, _, hk := Seite{}.Notiz(judge.Gist{}, hint)
	var added, said []string
	for _, t := range hk.Tags {
		if !k.Has([]string{t}) {
			added, said = append(added, t), append(said, "#"+t)
		}
	}
	tagsSaid := ""
	if len(added) > 0 {
		k.Tags = notes.Tags(append(k.Tags, added...))
		tagsSaid = " Tags dazu: " + strings.Join(said, " ") + "."
	}
	fp := s.Fingerabdruck()
	edited := k.Inhalt == "" || notes.Inhalt(body) != k.Inhalt
	var more string
	switch {
	case k.Stand == fp:
		more = " Die Seite hat sich seither nicht geändert."
	case k.Stand == "": // kept before Haus kept a fingerprint: from now on
		k.Stand = fp
	case edited:
		more = " Die Seite hat sich seither geändert; die Notiz hast du selbst bearbeitet, darum lasse ich sie, wie sie ist."
	default:
		g := gist(ctx, model, s, false)
		title, text, nk := s.Notiz(g, hint)
		nk.Tags = notes.Tags(append(append([]string{}, k.Tags...), nk.Tags...))
		nk.Stand = fp
		newBody := "# " + title + "\n\n" + strings.TrimSpace(text) + "\n"
		if err := d.Update(e.Name, nk.With(newBody), n.Changed); err != nil {
			return Gemerkt{}, err
		}
		stempeln(ctx, d, e.Name)
		out := schon(e, "")
		out.Aktualisiert, out.Art = true, nk.Art
		out.Kurz = "Das hattest du schon gemerkt: „" + e.Title + "“. Die Seite hat sich geändert, ich habe die Notiz auf den neuen Stand gebracht" + was(body, newBody) + "." + tagsSaid
		return out, nil
	}
	if k.Stand != fp || len(added) > 0 { // the head only: the text stays, and so does its fingerprint
		if k.Stand == "" {
			k.Stand = fp
		}
		if err := d.Update(e.Name, k.With(body), n.Changed); err != nil {
			log.Printf("haus: merken: %s: %v", e.Name, err)
		}
	}
	return schon(e, more+tagsSaid), nil
}

// was is what changed between a note's old and new text, in a few words:
// a recipe's ingredients counted, else nothing more.
func was(old, neu string) string {
	a, b := zutaten(old), zutaten(neu)
	if a == nil && b == nil {
		return ""
	}
	in := map[string]bool{}
	for _, z := range a {
		in[z] = true
	}
	plus, minus := 0, 0
	seen := map[string]bool{}
	for _, z := range b {
		seen[z] = true
		if !in[z] {
			plus++
		}
	}
	for _, z := range a {
		if !seen[z] {
			minus++
		}
	}
	switch {
	case plus == 0 && minus == 0:
		return ""
	case minus == 0:
		return fmt.Sprintf(": %d Zutaten neu", plus)
	case plus == 0:
		return fmt.Sprintf(": %d Zutaten weg", minus)
	}
	return fmt.Sprintf(": %d Zutaten neu, %d weg", plus, minus)
}

// zutaten are the "- " lines under a note's "## Zutaten".
func zutaten(body string) []string {
	var out []string
	in := false
	for _, l := range strings.Split(body, "\n") {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "#") {
			in = strings.Contains(strings.ToLower(t), "zutaten")
			continue
		}
		if z, ok := strings.CutPrefix(t, "- "); ok && in {
			out = append(out, z)
		}
	}
	return out
}

// kurz is the line Haus says after keeping: what it is, in a few words.
func kurz(s Seite, g judge.Gist, k notes.Kopf, partial bool) string {
	switch {
	case partial:
		return "Gemerkt sind nur Link und Titel: Die Seite ließ sich nicht lesen. Schick mir den Text, wenn du ihn willst."
	case s.Rezept != nil:
		r := s.Rezept
		return fmt.Sprintf("Rezept gemerkt: %s, %s, %s.", first(r.Name, s.Titel), count(len(r.Zutaten), "Zutat", "Zutaten"), count(len(r.Schritte), "Schritt", "Schritte"))
	case s.Album != nil && (s.Album.Band != "" || s.Album.Name != ""):
		a := s.Album
		line := "Album gemerkt: " + strings.TrimSpace(strings.Trim(a.Band+" – "+a.Name, " –"))
		if a.Wertung != "" {
			line += ", Wertung " + a.Wertung
		}
		return line + "."
	case g.Kern != "":
		return notes.ArtName(k.Art) + " gemerkt: " + firstSentence(g.Kern)
	}
	return "Gemerkt: " + s.Titel + "."
}

func firstSentence(s string) string {
	for i, r := range s {
		if (r == '.' || r == '!' || r == '?') && i > 20 {
			return s[:i+1]
		}
	}
	return s
}

// Notiz is the note's title, text and head for the page: by what the page
// is — a recipe, an album, a trip — else the model's gist or the page's
// own description; always the link; the page's text kept at the end.
func (s Seite) Notiz(g judge.Gist, hint string) (string, string, notes.Kopf) {
	k := notes.Kopf{Quelle: s.URL}
	var tags []string
	for _, w := range strings.Fields(hint) {
		if strings.HasPrefix(w, "#") {
			tags = append(tags, w[1:])
		}
	}
	host := s.Site
	if host == "" {
		if u, err := url.Parse(s.URL); err == nil {
			host = strings.TrimPrefix(u.Hostname(), "www.")
		}
	}
	var b strings.Builder
	title := s.Titel
	switch {
	case s.Rezept != nil:
		r := s.Rezept
		title, k.Art = first(r.Name, s.Titel), "rezept"
		tags = append(tags, "kochen")
		var info []string
		if r.Portionen != "" {
			info = append(info, "Für "+r.Portionen)
		}
		if r.Zeit != "" {
			info = append(info, r.Zeit)
		}
		if len(info) > 0 {
			b.WriteString("*" + strings.Join(info, " · ") + "*\n\n")
		}
		if r.Beschreibung != "" {
			b.WriteString(r.Beschreibung + "\n\n")
		}
		b.WriteString("## Zutaten\n\n")
		for _, z := range r.Zutaten {
			b.WriteString("- " + z + "\n")
		}
		b.WriteString("\n## Zubereitung\n\n")
		for i, st := range r.Schritte {
			fmt.Fprintf(&b, "%d. %s\n", i+1, st)
		}
	case s.Album != nil && (s.Album.Band != "" || s.Album.Name != "") || g.Art == "album":
		k.Art = "album"
		tags = append(tags, "musik")
		a := Album{}
		if s.Album != nil {
			a = *s.Album
		}
		if a.Band != "" || a.Name != "" {
			title = strings.Trim(a.Band+" – "+a.Name, " –")
		}
		var info []string
		if a.Jahr != "" {
			info = append(info, a.Jahr)
		}
		if a.Wertung != "" {
			info = append(info, "Wertung "+a.Wertung+" ("+host+")")
		}
		if len(info) > 0 {
			b.WriteString("*" + strings.Join(info, " · ") + "*\n\n")
		}
		switch {
		case g.Kern != "":
			b.WriteString(g.Kern + "\n")
		case a.Rezension != "":
			b.WriteString(a.Rezension + "\n")
		case s.Beschreibung != "":
			b.WriteString(s.Beschreibung + "\n")
		}
	case s.Reise != nil || g.Art == "reise":
		k.Art = "reise"
		tags = append(tags, "reisen")
		if r := s.Reise; r != nil {
			title = first(r.Name, s.Titel)
			for _, kv := range [][2]string{{"Ort", r.Ort}, {"Anbieter", first(r.Anbieter, host)}, {"Preis", r.Preis}} {
				if kv[1] != "" {
					b.WriteString("- " + kv[0] + ": " + kv[1] + "\n")
				}
			}
			b.WriteString("\n")
		}
		b.WriteString(first(g.Kern, descOf(s)) + "\n")
	default:
		if g.Art == "rezept" {
			k.Art = "rezept" // a recipe the page does not mark up: the gist is all there is
			tags = append(tags, "kochen")
		}
		if d := first(g.Kern, s.Beschreibung); d != "" {
			b.WriteString(d + "\n")
		}
	}
	tags = append(tags, g.Tags...)
	k.Tags = notes.Tags(tags)
	if rest := strings.TrimSpace(strings.Join(strings.Fields(strings.ReplaceAll(hint, "#", " #")), " ")); rest != "" {
		var words []string
		for _, w := range strings.Fields(rest) {
			if !strings.HasPrefix(w, "#") {
				words = append(words, w)
			}
		}
		// a phone's share sheet sends the page's own title as the text: that is no hint
		if said := strings.Join(words, " "); strings.EqualFold(said, strings.TrimSpace(s.Titel)) || strings.EqualFold(said, strings.TrimSpace(title)) {
			words = nil
		}
		if len(words) > 0 {
			b.WriteString("\nDazu: " + strings.Join(words, " ") + "\n")
		}
	}
	b.WriteString("\nQuelle: " + s.URL + "\n")
	if s.Text != "" && s.Rezept == nil {
		b.WriteString("\n## Von der Seite\n\n" + s.Text + "\n")
	}
	if title == "" {
		title = host
	}
	return cutTitle(title), b.String(), k
}

func descOf(s Seite) string {
	if s.Reise != nil && s.Reise.Beschreibung != "" {
		return s.Reise.Beschreibung
	}
	return s.Beschreibung
}

func cutTitle(t string) string {
	t = strings.Join(strings.Fields(t), " ")
	if r := []rune(t); len(r) > notes.MaxTitle {
		t = string(r[:notes.MaxTitle])
	}
	return t
}

// count is a number with its noun, one or more.
func count(n int, one, more string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, more)
}
