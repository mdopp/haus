// Package gesehen is what each person watched and heard (haus#152): the
// films and series they played in Jellyfin, the audiobooks they listened
// to in Audiobookshelf, and the household's film and book wish list in
// Radicale („Was als Nächstes?“), where a title goes unless the box has it
// already or the person has seen it. Reading the two apps only; the list
// is the one thing written.
package gesehen

import (
	"context"
	"errors"
	"log"
	"strings"

	"github.com/mdopp/haus/internal/abs"
	"github.com/mdopp/haus/internal/caldav"
	"github.com/mdopp/haus/internal/jellyfin"
)

// Liste is the household's film and book wish list, beside the music's.
var Liste = caldav.ListRef{Slug: "liste-filme-buecher"}

// ListName is its name on the phones.
const ListName = "Filme & Bücher"

// Art is what a title is.
type Art string

// The kinds a wish can be.
const (
	Film  Art = "Film"
	Serie Art = "Serie"
	Buch  Art = "Buch"
)

// ArtVon is the kind named in a form, Film when it names none.
func ArtVon(s string) Art {
	switch Art(s) {
	case Serie, Buch:
		return Art(s)
	}
	return Film
}

// Filme is Jellyfin as this needs it (its API key, laib's media template).
type Filme interface {
	Geschaut(ctx context.Context, person string, n int) ([]jellyfin.Gesehen, error)
	HatFilm(ctx context.Context, name string) (string, bool, error)
}

// Buecher is Audiobookshelf as this needs it (Haus's admin key, laib#676).
type Buecher interface {
	Gehoerte(ctx context.Context, person string, n int) ([]abs.Gehoert, error)
	HatBuch(ctx context.Context, title string, plain func(string) string) (string, bool, error)
}

// Service is the page's backends; any may be nil: Filme no films, Buecher
// no books, DAV no wish list.
type Service struct {
	Filme   Filme
	Buecher Buecher
	DAV     *caldav.Client
	Changed func(caldav.ListRef) // told after the list changed (the pages' live stream); nil: nobody
}

// Zeigen is how many entries each card shows.
const Zeigen = 15

// ErrNoTitle is a wish without a title.
var ErrNoTitle = errors.New("Welcher Titel? Schreib den Namen des Films, der Serie oder des Buchs.")

// Zeile is a wish's line on the list: "Dune (Film)".
func Zeile(titel string, art Art) string { return titel + " (" + string(art) + ")" }

// Gewuenscht is what wishing did, in a sentence.
type Gewuenscht struct {
	Satz  string
	Dazu  bool // a new line, or one ticked off before open again
	Schon bool // the box has it, or the person has seen it
}

// Wuenschen puts the title on the wish list for person, unless they have
// seen it, the box has it, or it is on the list already. A library that
// does not answer does not stop the wish: it is said in the log only.
func (s *Service) Wuenschen(ctx context.Context, person, titel string, art Art) (Gewuenscht, error) {
	titel = strings.Join(strings.Fields(titel), " ")
	if titel == "" {
		return Gewuenscht{}, ErrNoTitle
	}
	if r := []rune(titel); len(r) > 200 {
		titel = string(r[:200])
	}
	plain := jellyfin.Plain(titel)
	q := "„" + titel + "“"
	switch {
	case art != Buch && s.Filme != nil:
		if seen, err := s.Filme.Geschaut(ctx, person, 200); err == nil {
			for _, g := range seen {
				if jellyfin.Plain(g.Name) == plain {
					return Gewuenscht{Schon: true, Satz: "Hast du schon gesehen: " + q + "."}, nil
				}
			}
		}
		if name, ok, err := s.Filme.HatFilm(ctx, titel); err != nil {
			log.Printf("haus: gesehen: jellyfin: %v", err)
		} else if ok {
			return Gewuenscht{Schon: true, Satz: "„" + name + "“ ist schon in Jellyfin – du kannst gleich anfangen."}, nil
		}
	case art == Buch && s.Buecher != nil:
		if name, ok, err := s.Buecher.HatBuch(ctx, titel, jellyfin.Plain); err != nil {
			log.Printf("haus: gesehen: audiobookshelf: %v", err)
		} else if ok {
			return Gewuenscht{Schon: true, Satz: "„" + name + "“ gibt es schon als Hörbuch in Audiobookshelf."}, nil
		}
	}
	if s.DAV == nil {
		return Gewuenscht{}, errors.New("Die Wunschliste gibt es auf dieser Box nicht.")
	}
	if err := s.DAV.EnsureList(ctx, Liste, ListName); err != nil {
		return Gewuenscht{}, err
	}
	items, err := s.DAV.ListItems(ctx, Liste)
	if err != nil {
		return Gewuenscht{}, err
	}
	line := Zeile(titel, art)
	for _, it := range items {
		if jellyfin.Plain(it.Summary) != jellyfin.Plain(line) {
			continue
		}
		if !it.Done {
			return Gewuenscht{Satz: q + " steht schon auf der Wunschliste „" + ListName + "“."}, nil
		}
		it.Done = false
		if err := s.DAV.UpdateItem(ctx, it); err != nil {
			return Gewuenscht{}, err
		}
		s.told()
		return Gewuenscht{Dazu: true, Satz: q + " war schon einmal gewünscht und abgehakt; es steht wieder auf der Wunschliste."}, nil
	}
	origin := ""
	if person != "" {
		origin = "von " + person
	}
	if _, err := s.DAV.AddItem(ctx, Liste, line, origin); err != nil {
		return Gewuenscht{}, err
	}
	s.told()
	return Gewuenscht{Dazu: true, Satz: "Auf der Wunschliste „" + ListName + "“: " + q + "."}, nil
}

func (s *Service) told() {
	if s.Changed != nil {
		s.Changed(Liste)
	}
}

// Wuensche are the list's open lines.
func (s *Service) Wuensche(ctx context.Context) ([]caldav.Item, error) {
	if s.DAV == nil {
		return nil, nil
	}
	items, err := s.DAV.ListItems(ctx, Liste)
	var open []caldav.Item
	for _, it := range items {
		if !it.Done {
			open = append(open, it)
		}
	}
	return open, err
}
