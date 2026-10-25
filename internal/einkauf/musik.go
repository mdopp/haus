package einkauf

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/mdopp/haus/internal/caldav"
	"github.com/mdopp/haus/internal/jellyfin"
	"github.com/mdopp/haus/internal/merken"
)

// The household's music wish list (owner 2026-10-25: "nur noch eigene
// Musik", "so eine Wunschliste wäre cool"): a song or an album someone
// shared goes on the list "Musik", unless it is in the music already; a
// wish that has arrived (bought, uploaded, sorted in by beets) is ticked
// off by itself and the one who wished it is told. A line is "Künstler –
// Titel"; its origin "von <person>, <link>". Its lines are never read as
// amounts ("2Pac – Changes" is no 2).

// MusikListe is the household's music wish list.
var MusikListe = caldav.ListRef{Slug: "liste-musik"}

// MusikName is its name on the phones.
const MusikName = "Musik"

// Bibliothek is the household's own music: where a song is, if it is.
type Bibliothek interface {
	Find(ctx context.Context, artist, title string) (jellyfin.Treffer, bool, error)
}

// Wuensche is the music wish list on l's Radicale; Bib may be nil (no
// "hast du schon", no ticking off by itself).
type Wuensche struct {
	Listen *Liste
	Bib    Bibliothek
}

// Gewuenscht is what wishing did, in a sentence to say.
type Gewuenscht struct {
	Satz  string
	Schon bool // in the music already
	Steht bool // on the list already
}

func (w *Wuensche) liste() *Liste { return w.Listen.Andere(MusikListe) }

// Ensure makes the list when it is not there yet.
func (w *Wuensche) Ensure(ctx context.Context) error {
	return w.Listen.DAV.EnsureList(ctx, MusikListe, MusikName)
}

// Wuenschen puts the song on the list for person, unless it is in the
// music or on the list already.
func (w *Wuensche) Wuenschen(ctx context.Context, lied merken.Lied, person string) (Gewuenscht, error) {
	if w.Bib != nil {
		if t, ok, err := w.Bib.Find(ctx, lied.Kuenstler, lied.Titel); err == nil && ok {
			where := "in deiner Musik"
			if t.Album != "" && !lied.Album {
				where = "auf dem Album „" + t.Album + "“"
			}
			return Gewuenscht{Schon: true, Satz: "Hast du schon: „" + lied.Name() + "“ " + where + "."}, nil
		}
	}
	if err := w.Ensure(ctx); err != nil {
		return Gewuenscht{}, err
	}
	l := w.liste()
	var out Gewuenscht
	err := l.retry(ctx, func(items []caldav.Item) error {
		for _, it := range items {
			if !it.Done && jellyfin.Plain(it.Summary) == jellyfin.Plain(lied.Name()) {
				out = Gewuenscht{Steht: true, Satz: "„" + lied.Name() + "“ steht schon auf der Musik-Wunschliste."}
				return nil
			}
		}
		origin := []string{}
		if person != "" {
			origin = append(origin, "von "+person)
		}
		if lied.Link != "" {
			origin = append(origin, lied.Link)
		}
		if err := l.DAV.AddItem(ctx, MusikListe, lied.Name(), herkunft(origin)); err != nil {
			return err
		}
		out = Gewuenscht{Satz: "Auf der Musik-Wunschliste: „" + lied.Name() + "“. Sobald es in deiner Musik ist, hake ich es ab und sage dir Bescheid."}
		if w.Bib == nil {
			out.Satz = "Auf der Musik-Wunschliste: „" + lied.Name() + "“."
		}
		return nil
	})
	return out, err
}

// Erfuellt is a wish that has arrived: who wished it (or ""), and the
// sentence to tell them.
type Erfuellt struct {
	Person, Satz string
}

// Pruefen ticks off the open wishes the music has now, and says whom to
// tell.
func (w *Wuensche) Pruefen(ctx context.Context) ([]Erfuellt, error) {
	if w.Bib == nil {
		return nil, nil
	}
	l := w.liste()
	var out []Erfuellt
	err := l.retry(ctx, func(items []caldav.Item) error {
		out = nil
		for _, it := range items {
			if it.Done {
				continue
			}
			artist, title := Teile(it.Summary)
			t, ok, err := w.Bib.Find(ctx, artist, title)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			it.Done = true
			if err := l.DAV.UpdateItem(ctx, it); err != nil {
				return err
			}
			satz := "„" + it.Summary + "“ ist jetzt in deiner Musik"
			if t.Album != "" && t.Titel != "" {
				satz += ", auf dem Album „" + t.Album + "“"
			}
			out = append(out, Erfuellt{Person: Wer(it.Herkunft), Satz: satz + ". Ich habe es auf der Wunschliste abgehakt."})
		}
		return nil
	})
	return out, err
}

// Teile is a wish's artist and title: "Wanda – Salbei" is Wanda, Salbei;
// a line without a dash is a title alone.
func Teile(s string) (string, string) {
	for _, sep := range []string{" – ", " - ", " — "} {
		if a, t, ok := strings.Cut(s, sep); ok {
			return strings.TrimSpace(a), strings.TrimSpace(t)
		}
	}
	return "", strings.TrimSpace(s)
}

// Wer is who wished it, from the origin "von anna, https://…".
func Wer(herkunft string) string {
	for _, p := range herkunftOf(herkunft) {
		if w, ok := strings.CutPrefix(p, "von "); ok {
			return strings.TrimSpace(w)
		}
	}
	return ""
}

// Kaufen are shops that sell music as files without copy protection, each
// searched for the wish: the way to put it in the music (Musik hinzufügen).
func Kaufen(wish string) [][2]string {
	q := strings.ReplaceAll(strings.ReplaceAll(wish, " – ", " "), " - ", " ")
	e := url.QueryEscape(q)
	return [][2]string{
		{"Bandcamp", "https://bandcamp.com/search?q=" + e},
		{"Qobuz", "https://www.qobuz.com/de-de/search?q=" + e},
		{"7digital", "https://de.7digital.com/search?q=" + e},
	}
}

// AusLink is a shared link as a wish, when it is a song or an album: it
// answers false for any other link (a page to keep as a note), and for
// none on a box without the list (w nil).
func (w *Wuensche) AusLink(ctx context.Context, f merken.Fetcher, link, person string) (Gewuenscht, bool, error) {
	if w == nil {
		return Gewuenscht{}, false, nil
	}
	u, err := merken.Address(link)
	if err != nil || !merken.MusikLink(u) {
		return Gewuenscht{}, false, nil
	}
	lied, err := f.Lied(ctx, u.String())
	if errors.Is(err, merken.ErrKeinLied) {
		return Gewuenscht{}, false, nil
	}
	if err != nil {
		return Gewuenscht{}, true, err
	}
	g, err := w.Wuenschen(ctx, lied, person)
	return g, true, err
}
