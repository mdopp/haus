package pages

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/mdopp/haus/internal/laib"
)

// Start does not wait for laib (haus#67): laib's status takes a second, the
// rest of Start a few milliseconds. Haus keeps the status statusFresh and
// waits for a read at most boxInline; a page served before it came has a
// place for the box line, and the page's script fetches it from StartBoxPath
// once laib has answered. Variables for the tests.
var (
	statusFresh = 45 * time.Second
	boxInline   = 250 * time.Millisecond
)

// StartBoxPath is where Start's script fetches the box line.
const StartBoxPath = "/start/box"

// saysBoxLooking is the box line's place until laib has answered.
const saysBoxLooking = "Wie es der Box geht, wird gerade nachgesehen …"

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
// one under way) and waits for it at most wait. ok is false when it did
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

// boxLater is the box line's two places while laib has not answered, and
// the script that fills them: the top for what is wrong, the bottom for
// the calm line. ruhig says whether the rest of the page answered (no
// "Alles in Ordnung" otherwise). Without script the bottom says the box is
// being looked at; a fetch that fails says it cannot be said.
func boxLater(quiet bool) (top, bottom string) {
	q := "0"
	if quiet {
		q = "1"
	}
	_, failed := familyBox(laib.Readiness{}, nil, errLaibSilent, quiet, time.Time{}, time.UTC)
	f, _ := json.Marshal(failed)
	return "<div id=box-oben></div>", "<div id=box-unten><p class=ruhig><span class=\"punkt grau\"></span>" + saysBoxLooking + "</p></div>" +
		`<script>(function(){var o=document.getElementById('box-oben'),u=document.getElementById('box-unten'),f=` + string(f) + `;if(!window.fetch||!u)return;` +
		`fetch('` + StartBoxPath + `?ruhig=` + q + `',{credentials:'same-origin'}).then(function(r){if(!r.ok)throw new Error(r.status);return r.json()})` +
		`.then(function(j){o.innerHTML=j.oben||'';u.innerHTML=j.unten||''}).catch(function(){u.innerHTML=f})})()</script>`
}

// errLaibSilent stands for laib not answering in time.
var errLaibSilent = context.DeadlineExceeded

// startBox answers StartBoxPath: the box line as Start would have shown it,
// {"oben":…,"unten":…}, once laib has answered or backendWait has passed.
func startBox(w http.ResponseWriter, r *http.Request, cfg StartConfig, box *boxStatus) {
	if !showOnly(w, r, "Die Startseite") {
		return
	}
	if box == nil {
		http.NotFound(w, r)
		return
	}
	ready, services, err, ok := box.read(backendWait)
	if !ok {
		err = errLaibSilent
	}
	logErrs("start", map[string]error{"laib": err})
	now, loc := cfg.clock()
	quiet, _ := strconv.ParseBool(r.URL.Query().Get("ruhig"))
	top, bottom := familyBox(ready, services, err, quiet, now, loc)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(map[string]string{"oben": top, "unten": bottom})
}
