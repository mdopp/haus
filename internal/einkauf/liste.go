package einkauf

import (
	"context"
	"errors"
	"sync"

	"github.com/mdopp/haus/internal/caldav"
)

// Name is the list's name on the phones.
const Name = "Einkauf"

// Liste is one list in Radicale: the household's shopping list, or
// another (haus#119) through Andere.
type Liste struct {
	DAV *caldav.Client
	Ref caldav.ListRef
	// Changed is told after every change Haus makes, with the list (the
	// pages' live stream); nil: nobody listens.
	Changed func(caldav.ListRef)

	mu *sync.Mutex
}

// Open is the household's shopping list on c.
func Open(c *caldav.Client) *Liste {
	return &Liste{DAV: c, Ref: caldav.Einkauf, mu: &sync.Mutex{}}
}

// Andere is another list on the same Radicale, told the same way.
func (l *Liste) Andere(r caldav.ListRef) *Liste {
	return &Liste{DAV: l.DAV, Ref: r, Changed: l.Changed, mu: l.mu}
}

// Ensure makes the list when it is not there yet (at start, so a phone
// can take it before the first line).
func (l *Liste) Ensure(ctx context.Context) error {
	return l.DAV.EnsureList(ctx, l.Ref, Name)
}

// Lesen is the list: the open lines first, the ticked-off ones after.
func (l *Liste) Lesen(ctx context.Context) ([]caldav.Item, error) {
	return l.DAV.ListItems(ctx, l.Ref)
}

// Ergebnis is what adding did: lines new, lines added to.
type Ergebnis struct {
	Neu, Addiert int
}

// tries is how often a write is tried again after a phone changed the list
// in between.
const tries = 3

// retry runs fn on a fresh read until it does not meet a line changed in
// between, at most tries times.
func (l *Liste) retry(ctx context.Context, fn func([]caldav.Item) error) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	var err error
	for range tries {
		var items []caldav.Item
		if items, err = l.Lesen(ctx); err != nil {
			return err
		}
		if err = fn(items); !errors.Is(err, caldav.ErrChanged) {
			break
		}
	}
	if l.Changed != nil {
		l.Changed(l.Ref)
	}
	return err
}

// Dazu puts lines on the list: one the same as an open line (same thing,
// same kind of unit) is added to it, its origin with it; any other is a
// new line. Lines among themselves are added up first.
func (l *Liste) Dazu(ctx context.Context, zeilen []Zeile) (Ergebnis, error) {
	zeilen = Merge(zeilen)
	var res Ergebnis
	done := 0 // the lines written already, when a retry reads again
	err := l.retry(ctx, func(items []caldav.Item) error {
		for ; done < len(zeilen); done++ {
			z := zeilen[done]
			merged := false
			for _, it := range items {
				if it.Done {
					continue
				}
				sum, ok := Add(Parse(it.Summary), z.Posten)
				if !ok {
					continue
				}
				it.Summary = sum.String()
				it.Herkunft = herkunft(union(herkunftOf(it.Herkunft), z.Herkunft))
				if err := l.DAV.UpdateItem(ctx, it); err != nil {
					return err
				}
				res.Addiert++
				merged = true
				break
			}
			if !merged {
				if err := l.DAV.AddItem(ctx, l.Ref, z.Text(), herkunft(z.Herkunft)); err != nil {
					return err
				}
				res.Neu++
			}
		}
		return nil
	})
	return res, err
}

// ErrNoLine is a line that is not on the list (any more).
var ErrNoLine = errors.New("Diese Zeile steht nicht mehr auf der Liste.")

// Haken ticks the line off, or back on.
func (l *Liste) Haken(ctx context.Context, uid string, done bool) error {
	return l.retry(ctx, func(items []caldav.Item) error {
		for _, it := range items {
			if it.UID != uid {
				continue
			}
			if it.Done == done {
				return nil
			}
			it.Done = done
			return l.DAV.UpdateItem(ctx, it)
		}
		return ErrNoLine
	})
}

// Aufraeumen removes the ticked-off lines; it says how many.
func (l *Liste) Aufraeumen(ctx context.Context) (int, error) {
	n := 0
	err := l.retry(ctx, func(items []caldav.Item) error {
		for _, it := range items {
			if !it.Done {
				continue
			}
			if err := l.DAV.DeleteItem(ctx, it); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	return n, err
}
