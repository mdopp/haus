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
	Name, Titel, Art, Kurz string
	Schon, Teilweise       bool
}

// Merken makes the note for a link: hint is what came with it ("für
// Weihnachten"; #words in it are tags). A link already kept is not kept
// twice; a page that cannot be read is kept as link and title. model may
// be nil (no summary, the page's own description instead).
func Merken(ctx context.Context, f Fetcher, model *judge.Client, d *notes.Dir, link, hint string) (Gemerkt, error) {
	u, err := Address(link)
	if err != nil {
		return Gemerkt{}, err
	}
	if e, ok, err := d.ByQuelle(ctx, u.String()); err == nil && ok {
		return Gemerkt{Name: e.Name, Titel: e.Title, Art: e.Kopf.Art, Schon: true, Kurz: "Das ist schon gemerkt: „" + e.Title + "“."}, nil
	}
	final, page, err := f.Fetch(ctx, u)
	if err != nil && !errors.Is(err, ErrNoPage) { // no address, at home, or no answer at all: nothing to keep
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
	var g judge.Gist
	if model != nil && !partial && s.Rezept == nil && (s.Album == nil || len([]rune(s.Album.Rezension)) > 600 || s.Album.Rezension == "") {
		gctx, cancel := context.WithTimeout(ctx, 45*time.Second)
		g, err = model.Summarize(gctx, s.Titel, first(s.Text, s.Beschreibung))
		cancel()
		if err != nil {
			log.Printf("haus: merken: the house model: %v", err)
			g = judge.Gist{}
		}
	}
	title, text, k := s.Notiz(g, hint)
	if partial {
		text += "\n\nDie Seite ließ sich nicht lesen; gemerkt sind Link und Titel."
	}
	name, err := d.CreateWith(title, text, k)
	if err != nil {
		return Gemerkt{}, err
	}
	out := Gemerkt{Name: name, Titel: title, Art: k.Art, Teilweise: partial}
	out.Kurz = kurz(s, g, k, partial)
	return out, nil
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
