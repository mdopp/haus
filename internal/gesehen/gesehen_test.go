package gesehen

import (
	"context"
	"errors"
	"testing"

	"github.com/mdopp/haus/internal/abs"
	"github.com/mdopp/haus/internal/caldav"
	"github.com/mdopp/haus/internal/caldav/davfake"
	"github.com/mdopp/haus/internal/jellyfin"
)

// fakeFilme is Jellyfin for the tests: what each person played, the
// library's names, and an error to answer with.
type fakeFilme struct {
	seen map[string][]jellyfin.Gesehen
	lib  []string
	err  error
}

func (f *fakeFilme) Geschaut(_ context.Context, person string, n int) ([]jellyfin.Gesehen, error) {
	return f.seen[person], f.err
}

func (f *fakeFilme) HatFilm(_ context.Context, name string) (string, bool, error) {
	for _, l := range f.lib {
		if jellyfin.Plain(l) == jellyfin.Plain(name) {
			return l, true, f.err
		}
	}
	return "", false, f.err
}

type fakeBuecher struct{ lib []string }

func (f *fakeBuecher) Gehoerte(context.Context, string, int) ([]abs.Gehoert, error) { return nil, nil }
func (f *fakeBuecher) HatBuch(_ context.Context, t string, plain func(string) string) (string, bool, error) {
	for _, l := range f.lib {
		if plain(l) == plain(t) {
			return l, true, nil
		}
	}
	return "", false, nil
}

// haus#152: „Was als Nächstes?“ — a title onto the household's film and
// book wish list, with who wished it; not when the person has seen it, the
// box has it, or it is on the list already; a ticked-off wish open again;
// a library that does not answer does not stop the wish; no title, no wish.
func TestWuenschen(t *testing.T) {
	dav := davfake.New("haus", "pw")
	defer dav.Close()
	filme := &fakeFilme{seen: map[string][]jellyfin.Gesehen{"anna": {{Name: "Dark", Serie: true}}}, lib: []string{"Dune"}}
	told := 0
	s := &Service{Filme: filme, Buecher: &fakeBuecher{lib: []string{"Momo"}}, DAV: &caldav.Client{URL: dav.URL, User: "haus", Password: "pw"},
		Changed: func(r caldav.ListRef) {
			if r == Liste {
				told++
			}
		}}
	ctx := context.Background()
	want := func(person, titel string, art Art, satz string, dazu bool) {
		t.Helper()
		got, err := s.Wuenschen(ctx, person, titel, art)
		if err != nil || got.Satz != satz || got.Dazu != dazu {
			t.Errorf("%s %q: %+v %v\nwant %q", person, titel, got, err, satz)
		}
	}
	want("anna", "  Arrival ", Film, "Auf der Wunschliste „Filme & Bücher“: „Arrival“.", true)
	want("ben", "arrival", Film, "„arrival“ steht schon auf der Wunschliste „Filme & Bücher“.", false)
	want("anna", "dark", Serie, "Hast du schon gesehen: „dark“.", false)
	want("ben", "Dark", Serie, "Auf der Wunschliste „Filme & Bücher“: „Dark“.", true) // ben has not seen it
	want("anna", "DUNE", Film, "„Dune“ ist schon in Jellyfin – du kannst gleich anfangen.", false)
	want("anna", "Momo", Buch, "„Momo“ gibt es schon als Hörbuch in Audiobookshelf.", false)
	want("anna", "Krabat", Buch, "Auf der Wunschliste „Filme & Bücher“: „Krabat“.", true)
	open, err := s.Wuensche(ctx)
	if err != nil || len(open) != 3 || open[0].Summary != "Arrival (Film)" || open[0].Herkunft != "von anna" || open[2].Summary != "Krabat (Buch)" {
		t.Fatalf("list: %+v %v", open, err)
	}
	if told != 3 {
		t.Errorf("told %d times", told)
	}
	it := open[0]
	it.Done = true
	if err := s.DAV.UpdateItem(ctx, it); err != nil {
		t.Fatal(err)
	}
	want("anna", "Arrival", Film, "„Arrival“ war schon einmal gewünscht und abgehakt; es steht wieder auf der Wunschliste.", true)
	if open, _ := s.Wuensche(ctx); len(open) != 3 {
		t.Errorf("open again: %+v", open)
	}
	filme.err = errors.New("jellyfin 503")
	want("anna", "Dune", Film, "Auf der Wunschliste „Filme & Bücher“: „Dune“.", true) // Jellyfin away: the wish still goes on
	if _, err := s.Wuenschen(ctx, "anna", "   ", Film); !errors.Is(err, ErrNoTitle) {
		t.Errorf("no title: %v", err)
	}
	if _, err := (&Service{}).Wuenschen(ctx, "anna", "Arrival", Film); err == nil {
		t.Error("no list, no wish")
	}
	if ArtVon("Buch") != Buch || ArtVon("x") != Film {
		t.Error("ArtVon")
	}
}
