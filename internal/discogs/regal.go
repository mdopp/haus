package discogs

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Regal is the collection as Homes looks into it: read whole at most every
// ten minutes (Discogs's rate), asked by artist and title.
type Regal struct {
	C   *Client
	Now func() time.Time

	mu     sync.Mutex
	plates []Platte
	at     time.Time
}

const regalEvery = 10 * time.Minute

// Vergessen makes the next look read the collection again (after Homes
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

// Anzahl is how many records the shelf holds: the shelf read last when it
// is fresh, else Discogs's count of the collection, one call.
func (r *Regal) Anzahl(ctx context.Context) (int, error) {
	r.mu.Lock()
	n, fresh := len(r.plates), r.now().Sub(r.at) <= regalEvery
	r.mu.Unlock()
	if fresh {
		return n, nil
	}
	u, err := r.C.User(ctx)
	if err != nil {
		return 0, err
	}
	var out struct {
		Count int `json:"count"`
	}
	err = r.C.do(ctx, http.MethodGet, "/users/"+url.PathEscape(u)+"/collection/folders/0", nil, nil, &out)
	return out.Count, err
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
