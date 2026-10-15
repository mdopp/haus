// Package library is Haus's memory library (haus#13, laib ADR 0017 §3):
// the one search over the household's notes, by meaning, that Haus's
// pages and the Haushalt both use. qmd holds the index (laib#195); Haus
// holds none. What qmd names is checked against the notes folder, so a
// hit is a note the family can open, nothing else.
//
// A hit by meaning is shown only when it is close enough to the question
// (Cut). When qmd is not set, does not answer in time, or is still
// indexing (no vectors yet), the search is the notes package's word
// search, and says it is reduced; when it answered but nothing is close
// enough, it is the word search too, and says that instead (NoMatch):
// never empty while words match, never an error of its own. When more than one hit is left
// and the house model is there, it may narrow them (Tidy). Paperless
// documents are a later step.
package library

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/mdopp/haus/internal/judge"
	"github.com/mdopp/haus/internal/notes"
	"github.com/mdopp/haus/internal/qmd"
)

// Wait is qmd's share of a search, at most two thirds of the caller's
// time: the rest is the word search's, should qmd not answer.
var Wait = 2500 * time.Millisecond

// Found is a search's answer: the notes package's result, and which
// search it is. Reduced: the search by meaning could not answer, these are
// the words'. NoMatch: it answered, but no note is close to the question;
// these are the words' too, notes that hold a word of it, not answers.
type Found struct {
	notes.Result
	Reduced bool
	NoMatch bool
}

// The cut (haus#13, measured on 44 questions and 10 nonsense ones): a hit
// by meaning is shown when its cosine is at least Floor and at least Share
// of the best hit's. qmd's rank alone cannot say it: the best hit of any
// question, nonsense too, ranks first.
const (
	Floor = 0.30
	Share = 0.75
	ulp   = 1e-9 // far below the Vermittler's 0.001
)

// Cut keeps the hits close enough to the question, in qmd's order. The
// Vermittler rounds a cosine to three places, so a hit can sit exactly on
// the bar; it stays, whatever the float arithmetic makes of 0.75 × 0.4.
func Cut(hits []qmd.Hit) []qmd.Hit {
	best := 0.0
	for _, h := range hits {
		best = max(best, h.Vec)
	}
	out := []qmd.Hit{}
	for _, h := range hits {
		if h.Vec >= Floor-ulp && h.Vec >= Share*best-ulp {
			out = append(out, h)
		}
	}
	return out
}

// Tidy is the house model's tidy-up for one search, and how long it may
// take; the zero Tidy is none.
type Tidy struct {
	Judge *judge.Client
	Wait  time.Duration
}

// The tidy-up's budgets: a page shows its hits without waiting for it
// longer than PageTidy; the Bote waits for its answer anyway, a little
// longer.
const (
	PageTidy = 1200 * time.Millisecond
	BoteTidy = 3 * time.Second
)

// passageChars is how much of a note the house model reads per hit: the
// eval's 220-character snippets often missed the answer.
const passageChars = 700

// errNothingClose is a search by meaning that found nothing close enough:
// the words get their turn.
var errNothingClose = errors.New("no note is close enough to the question")

// Find searches the notes for q, at most max hits, best first.
func Find(ctx context.Context, c *qmd.Client, t Tidy, d *notes.Dir, q string, max int) (Found, error) {
	if c != nil && strings.TrimSpace(q) != "" {
		f, err := byMeaning(ctx, c, t, d, q, max)
		if err == nil {
			return f, nil
		}
		var folder folderError
		if errors.As(err, &folder) {
			return Found{}, folder.error
		}
		if !errors.Is(err, errNothingClose) {
			log.Printf("haus: library: search by words only: %v", err)
		}
		res, werr := d.Search(ctx, q, max)
		if werr != nil {
			return Found{}, werr
		}
		return Found{Result: res, Reduced: !errors.Is(err, errNothingClose), NoMatch: errors.Is(err, errNothingClose)}, nil
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

func byMeaning(ctx context.Context, c *qmd.Client, t Tidy, d *notes.Dir, q string, max int) (Found, error) {
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
	// the folder first: only a note the family can open counts, also as
	// the best hit the cut measures by
	var inFolder []qmd.Hit
	read := map[string]notes.Note{}
	for _, h := range hits {
		e, err := d.Lookup(ctx, h.Path)
		if errors.Is(err, notes.ErrNotFound) || errors.Is(err, notes.ErrOutside) || errors.Is(err, notes.ErrHidden) {
			continue // deleted since qmd's last refresh, or never a note
		}
		if err != nil {
			return Found{}, folderError{err}
		}
		inFolder, read[h.Path] = append(inFolder, h), e
	}
	out := Found{Result: notes.Result{Hits: []notes.Hit{}}}
	var items []judge.Item
	for _, h := range Cut(inFolder) {
		e := read[h.Path]
		out.Hits = append(out.Hits, notes.Hit{Name: e.Name, Title: e.Title, Lines: notes.Passage(h.Lines, e.Text, passageLines)})
		items = append(items, judge.Item{Title: e.Title, Text: notes.Excerpt(e.Text, h.Start, passageChars)})
		if len(out.Hits) == max {
			break
		}
	}
	if len(out.Hits) == 0 {
		return Found{}, errNothingClose
	}
	if len(out.Hits) > 1 && t.Judge != nil && t.Wait > 0 {
		out.Hits = tidy(ctx, t, q, out.Hits, items)
	}
	out.Total = len(out.Hits)
	return out, nil
}

// tidy asks the house model which hits answer q, within t.Wait (and the
// caller's time: the context carries both), and keeps those, in qmd's order. Whatever goes wrong —
// the model not loaded, a refusal, no answer in time, an answer that is
// not one — keeps the hits as the cut left them. So does an empty answer:
// the cut has already let only close notes through, and in the eval the
// model said "none" when a note's passage missed the answer, not when the
// note did. Emptying the list is the cut's to do (nonsense finds nothing
// close), never the model's.
func tidy(ctx context.Context, t Tidy, q string, hits []notes.Hit, items []judge.Item) []notes.Hit {
	jctx, cancel := context.WithTimeout(ctx, t.Wait) // and never past the caller's own deadline
	defer cancel()
	ids, err := t.Judge.Pick(jctx, q, items)
	if err != nil {
		log.Printf("haus: library: the hits as the cut left them: %v", err)
		return hits
	}
	if len(ids) == 0 {
		log.Printf("haus: library: the house model kept none of %d hits; the cut's stand", len(hits))
		return hits
	}
	out := make([]notes.Hit, 0, len(ids))
	for _, i := range ids {
		out = append(out, hits[i])
	}
	return out
}
