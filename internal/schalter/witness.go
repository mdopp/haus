package schalter

import (
	"context"
	"errors"
	"log"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mdopp/haus/internal/laib"
)

// Witness says who switched what (haus#82, haus#89): one line in Haus's
// log, and a note in Laib's journal (kind watch) with HA's context id, so
// HA's logbook ("Haus") and the journal together name the person. It is
// never in the switch's way: the note goes on its own, a failed one is a
// warning, and Laib refusing notes to the haus hand (until the owner
// approves the verb) is said once. A nil Witness only logs.
type Witness struct {
	Laib *laib.Client

	refused atomic.Bool
	notes   sync.WaitGroup // the notes under way, for the tests
}

// noteWait is how long one note may take.
const noteWait = 5 * time.Second

// witnessName is what a person's name may be in the note; anything else
// is "?", so no Remote-User can write into the journal's line.
var witnessName = regexp.MustCompile(`^[A-Za-z0-9._@-]{1,64}$`)

// Saw is one switch done for person: service on entity, HA's context id.
func (w *Witness) Saw(person, service, entity, contextID string) {
	if !witnessName.MatchString(person) {
		person = "?"
	}
	line := "ha person=" + person + " service=" + service + " entity=" + entity + " context=" + contextID
	log.Printf("haus: %s", line)
	if w == nil || w.Laib == nil {
		return
	}
	w.notes.Add(1)
	go func() {
		defer w.notes.Done()
		ctx, cancel := context.WithTimeout(context.Background(), noteWait)
		defer cancel()
		err := w.Laib.Note(ctx, "watch", line)
		switch {
		case err == nil:
		case errors.Is(err, laib.ErrNoTool):
			if !w.refused.Swap(true) {
				log.Printf("haus: laib takes no notes from the haus hand yet (policy note): who switched stays in this log only: %v", err)
			}
		default:
			log.Printf("haus: warning: note for %s on %s: %v", service, entity, err)
		}
	}()
}

// wait is until the notes under way are done (tests).
func (w *Witness) wait() { w.notes.Wait() }
