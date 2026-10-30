package caldav

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"sync"
	"testing"
	"time"
)

// lldap is Radicale behind LLDAP as on the box (haus#184, laib#877): a
// login Radicale has not cached is a bind, and LLDAP checks one password
// at a time (0.13 s alone; three at once took 0.415 s each); a login it
// has cached costs nothing until it expires (900 s on the box).
type lldap struct {
	mu          sync.Mutex
	bind        sync.Mutex
	cachedUntil time.Time
	binds       int
}

func (l *lldap) front(t *testing.T, dav string, bind time.Duration) string {
	t.Helper()
	u, _ := url.Parse(dav)
	proxy := httputil.NewSingleHostReverseProxy(u)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		l.mu.Lock()
		cached := time.Now().Before(l.cachedUntil)
		l.mu.Unlock()
		if !cached {
			l.bind.Lock()
			time.Sleep(bind)
			l.bind.Unlock()
			l.mu.Lock()
			l.binds++
			l.cachedUntil = time.Now().Add(15 * time.Minute)
			l.mu.Unlock()
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// haus#184: on the box a /kalender after a quiet spell sent its first
// three requests at once; each was a bind, and LLDAP took them one after
// another (0.415 s each, Radicale's log 2026-10-29 16:53). After a quiet
// spell the first request goes alone, so the rest find the login cached:
// one bind, not one per request in flight.
func TestAfterAQuietSpellOneLoginNotThree(t *testing.T) {
	const bind = 60 * time.Millisecond
	fake, c := mirrorBox(t)
	for _, p := range []string{"ben", "cara"} {
		fake.AddPrincipal(p)
		fake.AddCalendar("/"+p+"/google-familie/", "Familie "+p)
	}
	l := &lldap{}
	c.URL = l.front(t, fake.URL, bind)
	from, to := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)

	page := func() time.Duration {
		start := time.Now()
		var wg sync.WaitGroup
		var evErr, todoErr error
		wg.Add(2) // what /kalender asks, both at once (pages.fetch)
		go func() { defer wg.Done(); _, evErr = c.PersonEvents(context.Background(), "lea", from, to) }()
		go func() { defer wg.Done(); _, todoErr = c.OpenTodos(context.Background()) }()
		wg.Wait()
		if evErr != nil || todoErr != nil {
			t.Fatal(evErr, todoErr)
		}
		return time.Since(start)
	}
	cold := page()
	t.Logf("cold: %v, %d binds", cold, l.binds)
	if l.binds != 1 {
		t.Errorf("%d binds after a quiet spell, want 1", l.binds)
	}
	if cold > 2*bind {
		t.Errorf("cold page took %v, more than two binds (%v)", cold, 2*bind)
	}
	warm := page()
	t.Logf("warm: %v, %d binds", warm, l.binds)
	if l.binds != 1 {
		t.Errorf("%d binds with the login cached", l.binds)
	}
}
