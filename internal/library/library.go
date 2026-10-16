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
// and the house model is there, it may narrow them (Tidy). With Paperless
// and the documents' collection set, the same search covers the family's
// documents: their OCR text, mirrored into qmd by internal/dokumente, each
// hit checked against Paperless as a note's is against its folder.
package library

import (
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/mdopp/haus/internal/dokumente"
	"github.com/mdopp/haus/internal/judge"
	"github.com/mdopp/haus/internal/notes"
	"github.com/mdopp/haus/internal/paperless"
	"github.com/mdopp/haus/internal/qmd"
)

// Wait is qmd's share of a search, at most two thirds of the caller's
// time: the rest is the word search's, should qmd not answer.
var Wait = 2500 * time.Millisecond

// Found is a search's answer: the notes package's result, the documents
// found, and which search it is. Reduced: the search by meaning could not
// answer, these are the words'. NoMatch: it answered, but nothing is close
// to the question; these are the words' too, notes and documents that
// hold a word of it, not answers.
type Found struct {
	notes.Result
	Reduced bool
	NoMatch bool
	// Docs are the documents found, best first; nil when the library
	// holds no documents (haus#13).
	Docs []Doc
	// DocsMore is how many more documents the word search counted.
	DocsMore int
	// DocsDown is Paperless not answering: the documents are left out,
	// the notes' answer stands.
	DocsDown bool
}

// Doc is a document found: as Paperless has it now (title, date, sender,
// tags, its page), not as the copy qmd indexed, and the lines of the
// passage that matched, when the search by meaning found it.
type Doc struct {
	paperless.Doc
	Lines []string
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
	vecs := make([]float64, len(hits))
	for i, h := range hits {
		vecs[i] = h.Vec
	}
	out := []qmd.Hit{}
	for _, i := range cut(vecs) {
		out = append(out, hits[i])
	}
	return out
}

// cut is Cut on the cosines alone: the places of those that stay.
func cut(vecs []float64) []int {
	best := 0.0
	for _, v := range vecs {
		best = max(best, v)
	}
	out := []int{}
	for i, v := range vecs {
		if v >= Floor-ulp && v >= Share*best-ulp {
			out = append(out, i)
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

// Find searches the notes for q, at most max hits, best first; with
// Paperless and the documents' collection (c.Documents) set, the
// documents too (haus#13): one search, max hits in all.
func Find(ctx context.Context, c *qmd.Client, t Tidy, d *notes.Dir, pl *paperless.Client, q string, max int) (Found, error) {
	docs := pl != nil && c != nil && c.Documents != ""
	if c != nil && strings.TrimSpace(q) != "" {
		f, err := byMeaning(ctx, c, t, d, pl, docs, q, max)
		if err == nil {
			return f, nil
		}
		var folder folderError
		if errors.As(err, &folder) {
			return Found{}, folder.error
		}
		nothingClose := errors.Is(err, errNothingClose)
		if !nothingClose {
			log.Printf("haus: library: search by words only: %v", err)
		}
		f, err = byWords(ctx, d, pl, docs, q, max)
		if err != nil {
			return Found{}, err
		}
		f.Reduced, f.NoMatch = !nothingClose, nothingClose
		return f, nil
	}
	f, err := byWords(ctx, d, pl, docs, q, max)
	if err != nil {
		return Found{}, err
	}
	f.Reduced = true
	return f, nil
}

// byWords is the search by words: the notes package's, and Paperless's
// own full text search for the documents, side by side. Paperless failing
// leaves the notes' answer as it is and says so (DocsDown).
func byWords(ctx context.Context, d *notes.Dir, pl *paperless.Client, docs bool, q string, max int) (Found, error) {
	var found []paperless.Doc
	var total int
	var derr error
	var wg sync.WaitGroup
	docs = docs && strings.TrimSpace(q) != ""
	if docs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			found, total, derr = pl.Search(ctx, AnyWord(q), (max+1)/2)
		}()
	}
	res, err := d.Search(ctx, q, max)
	wg.Wait()
	if err != nil {
		return Found{}, err
	}
	f := Found{Result: res}
	if docs {
		if derr != nil {
			log.Printf("haus: library: the documents by words: %v", derr)
			f.DocsDown = true
		} else {
			f.Docs = make([]Doc, 0, len(found))
			for _, x := range found {
				f.Docs = append(f.Docs, Doc{Doc: x})
			}
			if total > len(found) {
				f.DocsMore = total - len(found)
			}
		}
	}
	return f, nil
}

// AnyWord is q as Paperless's full text search finds a document holding
// any of its meaningful words (Whoosh's OR): a question, as people ask it,
// is not a phrase every word of which a document holds. The words go in
// lower case (Whoosh finds them in any case): a word that is one of its
// operators (NOT, AND) stays a word. A q of only common words is asked as
// written.
func AnyWord(q string) string {
	ws := notes.Words(q)
	if len(ws) == 0 {
		return strings.TrimSpace(q)
	}
	for i, w := range ws {
		ws[i] = strings.ToLower(w)
	}
	return strings.Join(ws, " OR ")
}

// passageLines is how many lines of its passage a hit shows.
const passageLines = 3

// folderError is the notes folder failing, not qmd: no word search
// would answer either.
type folderError struct{ error }

// DocsWait is the most Paperless gets to confirm the documents qmd found;
// at most half the caller's time left.
var DocsWait = 1500 * time.Millisecond

// a hit qmd found, and what it is: a note read from the folder, or a
// document Paperless knows
type found struct {
	hit   qmd.Hit
	note  notes.Note
	doc   paperless.Doc
	isDoc bool
}

func byMeaning(ctx context.Context, c *qmd.Client, t Tidy, d *notes.Dir, pl *paperless.Client, docs bool, q string, max int) (Found, error) {
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
	// what is there first: only a note the family can open and a document
	// Paperless still has count, also as the best hit the cut measures by
	var all []found
	var ids []int
	for _, h := range hits {
		if docs && h.Collection == c.Documents {
			if id, ok := dokumente.ID(h.Path); ok {
				all, ids = append(all, found{hit: h, isDoc: true}), append(ids, id)
			}
			continue
		}
		if h.Collection != c.Collection {
			continue
		}
		e, err := d.Lookup(ctx, h.Path)
		if errors.Is(err, notes.ErrNotFound) || errors.Is(err, notes.ErrOutside) || errors.Is(err, notes.ErrHidden) {
			continue // deleted since qmd's last refresh, or never a note
		}
		if err != nil {
			return Found{}, folderError{err}
		}
		all = append(all, found{hit: h, note: e})
	}
	out := Found{Result: notes.Result{Hits: []notes.Hit{}}}
	if docs {
		out.Docs = []Doc{}
	}
	known := map[int]paperless.Doc{}
	if len(ids) > 0 {
		pwait := DocsWait
		if dl, ok := ctx.Deadline(); ok {
			pwait = min(pwait, time.Until(dl)/2)
		}
		pctx, pcancel := context.WithTimeout(ctx, pwait)
		got, err := pl.ByIDs(pctx, ids)
		pcancel()
		if err != nil {
			log.Printf("haus: library: the documents left out: %v", err)
			out.DocsDown = true
		}
		for _, x := range got {
			known[x.ID] = x
		}
	}
	var there []found
	for _, f := range all {
		if f.isDoc {
			id, _ := dokumente.ID(f.hit.Path)
			x, ok := known[id]
			if !ok {
				continue // deleted in Paperless since the last refresh, or not Haus's to see
			}
			f.doc = x
		}
		there = append(there, f)
	}
	vecs := make([]float64, len(there))
	for i, f := range there {
		vecs[i] = f.hit.Vec
	}
	var kept []found
	var items []judge.Item
	for _, i := range cut(vecs) {
		f := there[i]
		if f.isDoc {
			items = append(items, judge.Item{Title: f.doc.Title, Text: strings.Join(docLines(f.hit.Lines, f.doc.Start), " ")})
		} else {
			items = append(items, judge.Item{Title: f.note.Title, Text: notes.Excerpt(f.note.Text, f.hit.Start, passageChars)})
		}
		kept = append(kept, f)
		if len(kept) == max {
			break
		}
	}
	if len(kept) == 0 {
		return Found{}, errNothingClose
	}
	if len(kept) > 1 && t.Judge != nil && t.Wait > 0 {
		kept = keep(kept, tidy(ctx, t, q, len(kept), items))
	}
	for _, f := range kept {
		if f.isDoc {
			out.Docs = append(out.Docs, Doc{Doc: f.doc, Lines: docLines(f.hit.Lines, f.doc.Start)})
			continue
		}
		out.Hits = append(out.Hits, notes.Hit{Name: f.note.Name, Title: f.note.Title, Lines: notes.Passage(f.hit.Lines, f.note.Text, passageLines)})
	}
	out.Total = len(out.Hits)
	return out, nil
}

// docLines is what a document's hit shows of qmd's passage: its text,
// the lines above it that the file puts there (title, date, sender, tags)
// left out — a page names those itself, from Paperless. A passage that
// was only those lines (the question matched the title or the sender) is
// the start of the document's text instead.
func docLines(raw []string, start string) []string {
	var text []string
	for _, l := range raw {
		if !dokumente.HeadLine(l) {
			text = append(text, l)
		}
	}
	if out := notes.Passage(text, "", passageLines); len(out) > 0 {
		return out
	}
	return notes.Passage(nil, start, passageLines)
}

// keep is the found ones at these places, in that order.
func keep(fs []found, at []int) []found {
	out := make([]found, 0, len(at))
	for _, i := range at {
		out = append(out, fs[i])
	}
	return out
}

// tidy asks the house model which hits answer q, within t.Wait (and the
// caller's time: the context carries both), and keeps those, in qmd's order. Whatever goes wrong —
// the model not loaded, a refusal, no answer in time, an answer that is
// not one — keeps the hits as the cut left them. So does an empty answer:
// the cut has already let only close notes through, and in the eval the
// model said "none" when a note's passage missed the answer, not when the
// note did. Emptying the list is the cut's to do (nonsense finds nothing
// close), never the model's.
func tidy(ctx context.Context, t Tidy, q string, n int, items []judge.Item) []int {
	all := make([]int, n)
	for i := range all {
		all[i] = i
	}
	jctx, cancel := context.WithTimeout(ctx, t.Wait) // and never past the caller's own deadline
	defer cancel()
	ids, err := t.Judge.Pick(jctx, q, items)
	if err != nil {
		log.Printf("haus: library: the hits as the cut left them: %v", err)
		return all
	}
	if len(ids) == 0 {
		log.Printf("haus: library: the house model kept none of %d hits; the cut's stand", n)
		return all
	}
	return ids
}
