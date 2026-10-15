// Package library is Haus's memory library (haus#13, laib ADR 0017 §3):
// the one search over the household's notes, by meaning, that Haus's
// pages and the Haushalt both use. qmd holds the index (laib#195); Haus
// holds none. What qmd names is checked against the notes folder, so a
// hit is a note the family can open, nothing else.
//
// When qmd is not set, does not answer in time, or is still indexing (no
// vectors yet), the search is the notes package's word search, and says
// it is reduced: never empty while words match, never an error of its
// own. Paperless documents are a later step.
package library

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/mdopp/haus/internal/notes"
	"github.com/mdopp/haus/internal/qmd"
)

// Wait is qmd's share of a search, at most two thirds of the caller's
// time: the rest is the word search's, should qmd not answer.
var Wait = 2500 * time.Millisecond

// Found is a search's answer: the notes package's result, and whether it
// is the reduced search by words.
type Found struct {
	notes.Result
	Reduced bool
}

// Find searches the notes for q, at most max hits, best first.
func Find(ctx context.Context, c *qmd.Client, d *notes.Dir, q string, max int) (Found, error) {
	if c != nil && strings.TrimSpace(q) != "" {
		f, err := byMeaning(ctx, c, d, q, max)
		if err == nil {
			return f, nil
		}
		var folder folderError
		if errors.As(err, &folder) {
			return Found{}, folder.error
		}
		log.Printf("haus: library: search by words only: %v", err)
	}
	res, err := d.Search(ctx, q, max)
	if err != nil {
		return Found{}, err
	}
	return Found{Result: res, Reduced: true}, nil
}

// passageLines is how many lines of its passage a hit shows.
const passageLines = 3

// folderError is the notes folder failing, not qmd: no word search
// would answer either.
type folderError struct{ error }

func byMeaning(ctx context.Context, c *qmd.Client, d *notes.Dir, q string, max int) (Found, error) {
	wait := Wait
	if dl, ok := ctx.Deadline(); ok {
		wait = min(wait, time.Until(dl)*2/3) // a third left for the words
	}
	qctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	st, err := c.Status(qctx)
	if err != nil {
		return Found{}, err
	}
	if !st.Ready() {
		return Found{}, errors.New("qmd is still indexing the notes")
	}
	hits, err := c.Search(qctx, q, max)
	if err != nil {
		return Found{}, err
	}
	out := Found{Result: notes.Result{Hits: []notes.Hit{}}}
	for _, h := range hits {
		e, err := d.Lookup(ctx, h.Path)
		if errors.Is(err, notes.ErrNotFound) || errors.Is(err, notes.ErrOutside) || errors.Is(err, notes.ErrHidden) {
			continue // deleted since qmd's last refresh, or never a note
		}
		if err != nil {
			return Found{}, folderError{err}
		}
		out.Hits = append(out.Hits, notes.Hit{Name: e.Name, Title: e.Title, Lines: notes.Passage(h.Lines, e.Text, passageLines)})
		if len(out.Hits) == max {
			break
		}
	}
	out.Total = len(out.Hits)
	return out, nil
}
