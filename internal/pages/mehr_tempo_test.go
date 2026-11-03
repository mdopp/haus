package pages

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mdopp/homes/internal/laib"
)

// slowAufgaben is a Laib whose aufgabe list takes delay — what /mehr met on
// the box while Laib's MCP lock was held (aufgabe takes it exclusively,
// laib cmd/laib/main.go tier propose): /mehr waited 2.26s and 4.00s.
func slowAufgaben(t *testing.T, delay time.Duration) (*laib.Client, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		select {
		case <-time.After(delay):
		case <-r.Context().Done():
			return
		}
		text := `{"offen":1,"faellig":0,"aufgaben":[{"id":7,"titel":"Homes-App koppeln","fuer":"alle","weg":{"seite":"/willkommen"}}]}`
		json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"content": []map[string]string{{"text": text}}}})
	}))
	t.Cleanup(srv.Close)
	return &laib.Client{URL: srv.URL, Token: "t"}, &calls
}

func timedMehr(t *testing.T, cfg StartConfig) (time.Duration, string) {
	t.Helper()
	began := time.Now()
	code, body, _ := familyPage(t, Mehr, cfg, "GET", mehrPath, "family")
	took := time.Since(began)
	if code != http.StatusOK {
		t.Fatalf("/mehr: %d", code)
	}
	return took, body
}

// The owner (2026-11-03): /mehr took over a second, the other pages less.
// Its one remote read is Laib's aufgabe list; a slow Laib must cost the page
// at most mehrLaibWait, once, and the next visits read the kept list at once.
// Fails if Mehr waits for Laib again (backendWait, 4s) or asks it every time.
func TestMehrWaitsNotForASlowLaib(t *testing.T) {
	cl, calls := slowAufgaben(t, 1500*time.Millisecond)
	cfg := StartConfig{Laib: cl}

	took, body := timedMehr(t, cfg)
	t.Logf("first /mehr, Laib 1.5s slow: %v", took)
	if took > mehrLaibWait+300*time.Millisecond {
		t.Fatalf("first /mehr took %v; a slow Laib may cost it at most %v", took, mehrLaibWait)
	}
	mustContain(t, body, "<h1>Mehr</h1>", "Abmelden")
	mustNotContain(t, body, "Homes-App koppeln") // not there yet: the page did not wait

	// the read goes on without the page and is kept
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, body = timedMehr(t, cfg)
		if strings.Contains(body, "Homes-App koppeln") || time.Now().After(deadline) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	mustContain(t, body, `<a href="/willkommen">Homes-App koppeln</a>`)

	n := calls.Load()
	for i := 0; i < 5; i++ {
		took, body = timedMehr(t, cfg)
		if took > 200*time.Millisecond {
			t.Errorf("/mehr with the list kept took %v", took)
		}
		mustContain(t, body, "Homes-App koppeln")
	}
	t.Logf("/mehr with the list kept: %v", took)
	if got := calls.Load(); got != n {
		t.Errorf("Laib asked %d more times within %v; the list is kept", got-n, laib.AufgabenFresh)
	}
}

// A Laib that answers in time is on the page at the first visit.
func TestMehrShowsAQuickLaibAtOnce(t *testing.T) {
	cl, _ := slowAufgaben(t, 20*time.Millisecond)
	took, body := timedMehr(t, StartConfig{Laib: cl})
	t.Logf("/mehr, Laib 20ms: %v", took)
	mustContain(t, body, `<a href="/willkommen">Homes-App koppeln</a>`)
}
