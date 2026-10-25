package discogs

import (
	"context"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Regal is the collection as Haus looks into it: read whole at most every
// ten minutes (Discogs's rate), asked by artist and title.
type Regal struct {
	C   *Client
	Now func() time.Time

	mu     sync.Mutex
	plates []Platte
	at     time.Time
}

const regalEvery = 10 * time.Minute

// Vergessen makes the next look read the collection again (after Haus
// added a record).
func (r *Regal) Vergessen() {
	r.mu.Lock()
	r.at = time.Time{}
	r.mu.Unlock()
}

func (r *Regal) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// Hat is the record by artist named title on the shelf, if it is.
func (r *Regal) Hat(ctx context.Context, artist, title string) (Platte, bool, error) {
	r.mu.Lock()
	stale := r.now().Sub(r.at) > regalEvery
	r.mu.Unlock()
	if stale {
		ps, err := r.C.Sammlung(ctx)
		if err != nil {
			return Platte{}, false, err
		}
		r.mu.Lock()
		r.plates, r.at = ps, r.now()
		r.mu.Unlock()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	a, t := plain(artist), plain(title)
	for _, p := range r.plates {
		if plain(p.Titel) == t && (a == "" || plain(p.Kuenstler) == a) {
			return p, true, nil
		}
	}
	return Platte{}, false, nil
}

// Platten is the shelf as last read.
func (r *Regal) Platten(ctx context.Context) ([]Platte, error) {
	if _, _, err := r.Hat(ctx, "", "\x00"); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Platte(nil), r.plates...), nil
}

func plain(s string) string {
	s = strings.NewReplacer("ä", "a", "ö", "o", "ü", "u", "ß", "ss", "&", "und").Replace(strings.ToLower(s))
	var b strings.Builder
	for _, c := range s {
		if unicode.IsLetter(c) || unicode.IsDigit(c) {
			b.WriteRune(c)
		}
	}
	return b.String()
}
