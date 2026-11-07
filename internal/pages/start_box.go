package pages

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mdopp/homes/internal/cards"
	"github.com/mdopp/homes/internal/laib"
	"github.com/mdopp/homes/internal/texte"
)

// Start does not wait for laib (haus#67; owner 2026-11-03: not even
// briefly): laib's status takes a second, the rest of Start a few
// milliseconds. Homes keeps the status statusFresh, and while it is older
// but not past statusKeep uses it at once and reads it anew behind the
// page; Braucht dich comes whole from StartBoxPath once laib has answered.
// Variables for the tests.
var (
	statusFresh = 45 * time.Second
	statusKeep  = 5 * time.Minute
	boxInline   = time.Duration(0)
)

// boxStatuses are the kept statuses, one per Laib: Start and Willkommen
// share one read.
var (
	boxStatusesMu sync.Mutex
	boxStatuses   = map[*laib.Client]*boxStatus{}
)

// boxStatusFor is c's kept status.
func boxStatusFor(c *laib.Client) *boxStatus {
	boxStatusesMu.Lock()
	defer boxStatusesMu.Unlock()
	s := boxStatuses[c]
	if s == nil {
		s = &boxStatus{laib: c}
		boxStatuses[c] = s
	}
	return s
}

// StartBoxPath is where Start fetches Braucht dich with Laib's part.
const StartBoxPath = "/start/box"

// saysBoxSilent is an admin's row in Braucht dich while laib does not answer.
const saysBoxSilent = "Wie es der Box geht, lässt sich gerade nicht sagen."

// maxBraucht are the tasks in Braucht dich; more is one row to Deine Aufgaben.
const maxBraucht = 3

// boxStatus is laib's status, kept for Start: one read at a time, which
// every page asking meanwhile shares; a failed read is not kept.
type boxStatus struct {
	laib *laib.Client

	mu       sync.Mutex
	ready    laib.Readiness
	services []laib.Service
	at       time.Time     // when the kept status was read; zero: none
	err      error         // the last read's
	running  chan struct{} // the read under way, closed when it is done
}

// read is the status if one is fresh; else it starts a read (or joins the
// one under way), answers at once with a status kept less than statusKeep,
// and without one waits for the read at most wait. ok is false when it did
// not come within wait; the read goes on and is kept for the next page.
func (s *boxStatus) read(wait time.Duration) (ready laib.Readiness, services []laib.Service, err error, ok bool) {
	s.mu.Lock()
	if !s.at.IsZero() && time.Since(s.at) < statusFresh {
		defer s.mu.Unlock()
		return s.ready, s.services, nil, true
	}
	done := s.running
	if done == nil {
		done = make(chan struct{})
		s.running = done
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), backendWait)
			defer cancel()
			r, sv, err := s.laib.Status(ctx)
			s.mu.Lock()
			if err == nil {
				s.ready, s.services, s.at = r, sv, time.Now()
			}
			s.err, s.running = err, nil
			s.mu.Unlock()
			close(done)
		}()
	}
	if !s.at.IsZero() && time.Since(s.at) < statusKeep {
		defer s.mu.Unlock()
		return s.ready, s.services, nil, true
	}
	s.mu.Unlock()
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case <-done:
	case <-t.C:
		return laib.Readiness{}, nil, nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return laib.Readiness{}, nil, s.err, true
	}
	return s.ready, s.services, nil, true
}

// boxRead is laib's status as Start read it.
type boxRead struct {
	ready    laib.Readiness
	services []laib.Service
}

// laibTeil is what Laib adds to Braucht dich; nil is not read (yet).
type laibTeil struct {
	status    *boxRead
	statusErr error           // laib did not answer: an admin is told
	aufgaben  *laib.Aufgaben  // the person's own Box-Aufgaben (laib#878)
	befunde   laib.Befunde    // laib diag's, for an admin
	wartet    []laib.Proposal // decisions waiting, for an admin
}

// brauchtDichSpaeter is Braucht dich's place on Start: what Homes knows at
// once (its own tasks, the status as kept), the whole card from
// StartBoxPath once Laib has answered; that failing leaves the first.
func brauchtDichSpaeter(r *http.Request, cfg StartConfig, kept *boxRead) string {
	own := brauchtDich(r, cfg, laibTeil{status: kept})
	if cfg.Laib == nil {
		return own
	}
	return nachladen(StartBoxPath, own, own)
}

// brauchtDich answers "what needs me?" (owner 2026-11-07; the UX review's
// one place for it): the person's important tasks and Immich's questions,
// for an admin the box's red and yellow and the decisions waiting — never
// an optional app; nothing open, no card. For the family a service down is
// the box's banner above it (boxAchtung).
func brauchtDich(r *http.Request, cfg StartConfig, l laibTeil) string {
	as := einrichtung(r, cfg)
	if l.aufgaben != nil {
		as = mitLaibs(r, cfg, as, *l.aufgaben)
	}
	c := cards.Card{Title: "Braucht dich"}
	n := 0
	for _, a := range offen(as) {
		if !a.wichtig && !a.frage {
			continue
		}
		if n++; n <= maxBraucht {
			c.AddLink(a.titel, a.stand, a.href)
		}
	}
	if n > maxBraucht {
		c.AddLink(texte.Anzahl(n-maxBraucht, "weitere Aufgabe", "weitere Aufgaben"), "", aufgabenPath)
	}
	if !isAdmin(r) {
		if l.status == nil {
			return c.HTML()
		}
		now, loc := cfg.clock()
		return boxAchtung(l.status.ready, l.status.services, now, loc) + c.HTML()
	}
	if l.statusErr != nil {
		c.Add(saysBoxSilent, "")
	}
	if l.status != nil {
		count := map[string]int{}
		notGreen(l.status.services, l.befunde, func(class string, _ *laib.Service, _ *laib.Finding) { count[class]++ })
		var says []string
		for _, class := range []string{"rot", "gelb"} {
			if count[class] > 0 {
				says = append(says, strconv.Itoa(count[class])+" "+class)
			}
		}
		if f := l.status.ready.AufgabenFaellig; f > 0 {
			says = append(says, texte.Anzahl(f, texte.T("box.faellig"), texte.T("box.faellig.n")))
		}
		if len(says) > 0 {
			c.AddLink("Box: "+strings.Join(says, " · "), "", "/box")
		}
	}
	if n := len(l.wartet); n > 0 { // deciding is Laib's page's (ADR 0012, haus#157)
		c.AddLink(texte.Anzahl(n, texte.T("box.wartet"), texte.T("box.wartet.n")), "", strings.TrimRight(cfg.LaibURL, "/")+"/inbox")
	}
	return c.HTML()
}

// startBox answers StartBoxPath: Braucht dich with Laib's part (the status,
// the person's Box-Aufgaben, an admin's findings and waiting decisions)
// once laib has answered or backendWait has passed; "" when nothing is open.
func startBox(w http.ResponseWriter, r *http.Request, cfg StartConfig, box *boxStatus) {
	if !showOnly(w, r, "Die Startseite") {
		return
	}
	if box == nil {
		http.NotFound(w, r)
		return
	}
	var l laibTeil
	var derr, ierr error
	user := r.Header.Get("Remote-User")
	reads := []func(context.Context){func(context.Context) {
		ready, services, err, ok := box.read(backendWait)
		if !ok {
			err = context.DeadlineExceeded // laib not answering in time
		}
		if l.statusErr = err; err == nil {
			l.status = &boxRead{ready, services}
		}
	}}
	if user != "" {
		reads = append(reads, func(ctx context.Context) {
			if la, ok, err := cfg.Laib.KeptAufgaben(ctx, user); ok {
				l.aufgaben = &la
			} else if err != nil {
				logLaibAufgaben(err)
			}
		})
	}
	if isAdmin(r) {
		reads = append(reads, func(ctx context.Context) { l.befunde, derr = cfg.Laib.Befunde(ctx) },
			func(ctx context.Context) { l.wartet, ierr = cfg.Laib.Inbox(ctx) })
	}
	fetch(r, reads...)
	logErrs("start", map[string]error{"laib": l.statusErr, "befunde": derr, "inbox": ierr})
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write([]byte(brauchtDich(r, cfg, l)))
}
