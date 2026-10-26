package einkauf

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mdopp/haus/internal/caldav"
	"github.com/mdopp/haus/internal/discogs"
	"github.com/mdopp/haus/internal/jellyfin"
	"github.com/mdopp/haus/internal/merken"
	"github.com/mdopp/haus/internal/musicbrainz"
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
// "hast du schon", no ticking off by itself), MB nil (no "wo gibt es das"),
// Dir "" (no stages: a wish is open or ticked off).
type Wuensche struct {
	Listen *Liste
	Bib    Bibliothek
	MB     Suche
	Regal  *discogs.Regal // the household's records on Discogs; nil: none
	Dir    string         // HAUS_DATA: musikwuensche.json, each wish's stage and where it is
	Now    func() time.Time

	mu        sync.Mutex
	searching map[string]bool // the wishes being looked up now
}

// Suche is where a wish is to be had (MusicBrainz).
type Suche interface {
	Suchen(ctx context.Context, artist, title string, album bool, now time.Time) (musicbrainz.Fund, error)
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
	if w.Regal != nil {
		if p, ok, err := w.Regal.Hat(ctx, lied.Kuenstler, lied.Titel); err == nil && ok {
			year := ""
			if p.Jahr > 0 {
				year = " (" + strconv.Itoa(p.Jahr) + ")"
			}
			return Gewuenscht{Schon: true, Satz: "Hast du schon auf Vinyl: „" + p.Titel + "“" + year + "."}, nil
		}
	}
	if err := w.Ensure(ctx); err != nil {
		return Gewuenscht{}, err
	}
	l := w.liste()
	var out Gewuenscht
	err := l.retry(ctx, func(items []caldav.Item) error {
		for _, it := range items {
			if jellyfin.Plain(it.Summary) != jellyfin.Plain(lied.Name()) {
				continue
			}
			if !it.Done {
				out = Gewuenscht{Steht: true, Satz: "„" + lied.Name() + "“ steht schon auf der Musik-Wunschliste."}
				return nil
			}
			it.Done = false // wished before and ticked off: the same line again, not a second
			if err := l.DAV.UpdateItem(ctx, it); err != nil {
				return err
			}
			out = Gewuenscht{Steht: true, Satz: "„" + lied.Name() + "“ hattest du schon einmal gewünscht und abgehakt; der Wunsch steht wieder auf der Liste."}
			return nil
		}
		origin := []string{}
		if person != "" {
			origin = append(origin, "von "+person)
		}
		if lied.Link != "" {
			origin = append(origin, lied.Link)
		}
		uid, err := l.DAV.AddItem(ctx, MusikListe, lied.Name(), herkunft(origin))
		if err != nil {
			return err
		}
		if lied.Album {
			w.setzen(uid, func(st *Stand) { st.Album = true })
		}
		if w.MB != nil { // where it is to be had, while the answer is on its way
			go w.suchen(uid, lied.Kuenstler, lied.Titel, lied.Album)
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
	if w.Bib == nil && w.Regal == nil {
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
			st := w.Stand(it.UID)
			if album := st.AlbumName(title); w.Regal != nil && album != "" { // the record on the shelf: all there
				if p, ok, err := w.Regal.Hat(ctx, artist, album); err == nil && ok {
					it.Done = true
					if err := l.DAV.UpdateItem(ctx, it); err != nil {
						return err
					}
					if st.Wantlist != 0 {
						if err := w.Regal.C.Entwuenschen(ctx, st.Wantlist); err != nil {
							log.Printf("haus: musikwunsch: wantlist: %v", err)
						}
					}
					out = append(out, Erfuellt{Person: Wer(it.Herkunft), Satz: "Die Platte „" + p.Titel + "“ ist in deiner Sammlung; ich habe „" + it.Summary + "“ auf der Wunschliste abgehakt."})
					continue
				}
			}
			if w.Bib == nil {
				continue
			}
			t, ok, err := w.Bib.Find(ctx, artist, title)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			if st.Stufe == StufeDatei {
				continue // the file came already; the record is ticked off by hand
			}
			satz := "„" + it.Summary + "“ ist jetzt in deiner Musik"
			if t.Album != "" && t.Titel != "" {
				satz += ", auf dem Album „" + t.Album + "“"
			}
			if st.Stufe == StufeBestellt && st.LP { // the download is there, the record on its way
				w.setzen(it.UID, func(s *Stand) { s.Stufe, s.DateiAm = StufeDatei, w.now() })
				out = append(out, Erfuellt{Person: Wer(it.Herkunft), Satz: satz + ". Die Platte ist noch unterwegs; wenn sie da ist, hake sie auf der Wunschliste ab."})
				continue
			}
			it.Done = true
			if err := l.DAV.UpdateItem(ctx, it); err != nil {
				return err
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

// The stages of a wish (owner 2026-10-25): wished, ordered (a record with
// its download, or the file alone), the file there (the record on its
// way), and ticked off when all of it is there.
const (
	StufeWunsch   = ""
	StufeBestellt = "bestellt"
	StufeDatei    = "datei"
)

// Stand is a wish's stage and where it is to be had.
type Stand struct {
	Stufe    string            `json:"stufe,omitempty"`
	LP       bool              `json:"lp,omitempty"` // a record was ordered, not the file alone
	Album    bool              `json:"album,omitempty"`
	Bestellt time.Time         `json:"bestellt,omitzero"`
	DateiAm  time.Time         `json:"datei_am,omitzero"`
	Fund     *musicbrainz.Fund `json:"fund,omitempty"`
	Wantlist int               `json:"wantlist,omitempty"` // the release on the Discogs wantlist
	Versucht time.Time         `json:"versucht,omitzero"`  // the last search, found or not
	Sucht    bool              `json:"-"`
}

const wishFile = "musikwuensche.json"

func (w *Wuensche) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}

func (w *Wuensche) load() map[string]Stand {
	out := map[string]Stand{}
	if w.Dir == "" {
		return out
	}
	if b, err := os.ReadFile(filepath.Join(w.Dir, wishFile)); err == nil {
		if err := json.Unmarshal(b, &out); err != nil {
			log.Printf("haus: musikwünsche: %s: %v", wishFile, err)
		}
	}
	return out
}

func (w *Wuensche) save(all map[string]Stand) {
	if w.Dir == "" {
		return
	}
	b, _ := json.MarshalIndent(all, "", "  ")
	p := filepath.Join(w.Dir, wishFile)
	if err := os.WriteFile(p+".tmp", b, 0o600); err == nil {
		err = os.Rename(p+".tmp", p)
		if err != nil {
			log.Printf("haus: musikwünsche: %v", err)
		}
	}
}

// Stand is a wish's stage now.
func (w *Wuensche) Stand(uid string) Stand {
	w.mu.Lock()
	defer w.mu.Unlock()
	st := w.load()[uid]
	st.Sucht = w.searching[uid]
	return st
}

func (w *Wuensche) setzen(uid string, fn func(*Stand)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	all := w.load()
	st := all[uid]
	fn(&st)
	all[uid] = st
	w.save(all)
}

// suchen looks the wish up and keeps what was found; the list's pages are
// told.
func (w *Wuensche) suchen(uid, artist, title string, album bool) {
	w.mu.Lock()
	if w.searching[uid] {
		w.mu.Unlock()
		return
	}
	if w.searching == nil {
		w.searching = map[string]bool{}
	}
	w.searching[uid] = true
	w.mu.Unlock()
	defer func() {
		w.mu.Lock()
		delete(w.searching, uid)
		w.mu.Unlock()
		if w.Listen.Changed != nil {
			w.Listen.Changed(MusikListe)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	f, err := w.MB.Suchen(ctx, artist, title, album, w.now())
	if err != nil && !errors.Is(err, musicbrainz.ErrNichts) {
		log.Printf("haus: musikwunsch %q: %v", artist+" – "+title, err)
	}
	w.setzen(uid, func(st *Stand) {
		st.Versucht = w.now()
		if f.Gefunden || errors.Is(err, musicbrainz.ErrNichts) {
			st.Fund = &f
		}
	})
}

// Nachsuchen looks up again the open wishes whose search did not finish
// (MusicBrainz slow or away) at most once an hour, and those found as
// still to come once their day has come.
func (w *Wuensche) Nachsuchen(ctx context.Context) error {
	if w.MB == nil {
		return nil
	}
	items, err := w.liste().Lesen(ctx)
	if err != nil {
		return err
	}
	now := w.now()
	for _, it := range items {
		if it.Done {
			continue
		}
		st := w.Stand(it.UID)
		stale := st.Fund == nil && now.Sub(st.Versucht) > time.Hour
		due := st.Fund != nil && st.Fund.Kommt && st.Fund.Datum <= now.Format("2006-01-02")
		if stale || due {
			artist, title := Teile(it.Summary)
			w.suchen(it.UID, artist, title, st.Album)
		}
	}
	return nil
}

// NeuSuchen looks a wish up again now (the page's "Neu suchen").
func (w *Wuensche) NeuSuchen(ctx context.Context, uid string) error {
	items, err := w.liste().Lesen(ctx)
	if err != nil {
		return err
	}
	for _, it := range items {
		if it.UID == uid {
			artist, title := Teile(it.Summary)
			go w.suchen(uid, artist, title, w.Stand(uid).Album)
			return nil
		}
	}
	return ErrNoLine
}

// Stufe moves a wish on: "bestellt-lp" (a record with its download),
// "bestellt-datei" (the file alone), "datei" (the file is there), "platte"
// (the record is there). What is all there is ticked off. It answers the
// sentence to say.
func (w *Wuensche) Stufe(ctx context.Context, uid, step string) (string, error) {
	l := w.liste()
	var said string
	err := l.retry(ctx, func(items []caldav.Item) error {
		for _, it := range items {
			if it.UID != uid {
				continue
			}
			st := w.Stand(uid)
			tick := false
			switch step {
			case "bestellt-lp":
				w.setzen(uid, func(s *Stand) { s.Stufe, s.LP, s.Bestellt = StufeBestellt, true, w.now() })
				said = "„" + it.Summary + "“ ist bestellt, als Platte mit Download. Sobald die Datei in deiner Musik ist, sage ich Bescheid."
			case "bestellt-datei":
				w.setzen(uid, func(s *Stand) { s.Stufe, s.LP, s.Bestellt = StufeBestellt, false, w.now() })
				said = "„" + it.Summary + "“ ist bestellt, als Datei."
			case "datei":
				if st.LP {
					w.setzen(uid, func(s *Stand) { s.Stufe, s.DateiAm = StufeDatei, w.now() })
					said = "Die Datei von „" + it.Summary + "“ ist da; die Platte ist noch unterwegs."
				} else {
					tick, said = true, "„"+it.Summary+"“ ist da und abgehakt."
				}
			case "platte":
				tick, said = true, "Die Platte von „"+it.Summary+"“ ist da; abgehakt."
			default:
				return errors.New("Diesen Schritt gibt es nicht.")
			}
			if tick {
				it.Done = true
				return l.DAV.UpdateItem(ctx, it)
			}
			return nil
		}
		return ErrNoLine
	})
	return said, err
}

// AlbumName is the record a wish is: the album MusicBrainz found it on,
// or the wish itself when it is an album; "" before either is known.
func (st Stand) AlbumName(title string) string {
	switch {
	case st.Album:
		return title
	case st.Fund != nil && st.Fund.Gefunden:
		return st.Fund.Album
	}
	return ""
}

// AufWantlist puts the wish's record on the Discogs wantlist: its album's
// main release on vinyl.
func (w *Wuensche) AufWantlist(ctx context.Context, uid string) (string, error) {
	if w.Regal == nil {
		return "", errors.New("Discogs ist nicht verbunden.")
	}
	items, err := w.liste().Lesen(ctx)
	if err != nil {
		return "", err
	}
	for _, it := range items {
		if it.UID != uid {
			continue
		}
		artist, title := Teile(it.Summary)
		album := w.Stand(uid).AlbumName(title)
		if album == "" {
			return "", errors.New("Auf welchem Album das ist, weiß Haus noch nicht; erst wenn es gefunden ist, geht es auf die Wantlist.")
		}
		t, ok, err := w.Regal.C.Album(ctx, artist, album)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", errors.New("Discogs kennt „" + album + "“ von " + artist + " nicht als Platte.")
		}
		if err := w.Regal.C.Wuenschen(ctx, t.ID); err != nil {
			return "", err
		}
		w.setzen(uid, func(st *Stand) { st.Wantlist = t.ID })
		if w.Listen.Changed != nil {
			w.Listen.Changed(MusikListe)
		}
		return "„" + album + "“ steht auf deiner Discogs-Wantlist; Discogs meldet sich, wenn jemand sie anbietet.", nil
	}
	return "", ErrNoLine
}
