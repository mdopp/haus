package pages

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

// The owner (2026-11-03): „Warum werden die Aufgaben nicht async geladen?“
// /mehr never waits for Laib: with nothing kept, the Einrichtung card is a
// place that fills itself from MehrAufgabenPath, with a plain link to Deine
// Aufgaben meanwhile and what it says should the part not come. Fails if
// Mehr waits for Laib again, however briefly.
func TestMehrDoesNotWaitForLaib(t *testing.T) {
	cl, _ := slowAufgaben(t, 3*time.Second)
	took, body := timedMehr(t, StartConfig{Laib: cl})
	t.Logf("/mehr, Laib 3s slow: %v", took)
	if took > 150*time.Millisecond {
		t.Fatalf("/mehr took %v: it waited for Laib", took)
	}
	mustContain(t, body, "<h1>Mehr</h1>", "Abmelden",
		`<div data-nachladen="/mehr/aufgaben" aria-busy=true>`, `<a href="/aufgaben">Deine Aufgaben</a>`, saysAufgabenLesen,
		"<template>", saysAufgabenNicht, "window.homesNachladen")
	mustNotContain(t, body, "Homes-App koppeln")
}

// The part: the card with Laib's tasks once Laib answers, kept a minute,
// so the next fragment and the next /mehr ask nobody; /mehr then has the
// card whole, nothing to fetch.
func TestMehrAufgabenFragment(t *testing.T) {
	cl, calls := slowAufgaben(t, 50*time.Millisecond)
	cfg := StartConfig{Laib: cl}
	code, card, h := familyPage(t, Mehr, cfg, "GET", MehrAufgabenPath, "family")
	if code != 200 || h.Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("%s → %d %q", MehrAufgabenPath, code, h.Get("Content-Type"))
	}
	mustContain(t, card, "<h2 id=karte-deine-aufgaben>Deine Aufgaben</h2>", `<a href="/willkommen">Homes-App koppeln</a>`)
	mustNotContain(t, card, "<!doctype", "data-nachladen", saysAufgabenLesen)
	n := calls.Load()
	familyPage(t, Mehr, cfg, "GET", MehrAufgabenPath, "family")
	took, body := timedMehr(t, cfg)
	if took > 150*time.Millisecond {
		t.Errorf("/mehr with the list kept took %v", took)
	}
	mustContain(t, body, `<a href="/willkommen">Homes-App koppeln</a>`)
	mustNotContain(t, body, "data-nachladen=", saysAufgabenLesen)
	if got := calls.Load(); got != n {
		t.Errorf("Laib asked %d more times within %v; the list is kept", got-n, laib.AufgabenFresh)
	}
}

// Laib failing or silent: the part says so in a line, never spins, never
// shows Laib's error.
func TestMehrAufgabenFragmentWhenLaibFails(t *testing.T) {
	_, card, _ := familyPage(t, Mehr, StartConfig{Laib: brokenLaib(t)}, "GET", MehrAufgabenPath, "family")
	mustContain(t, card, `<a href="/aufgaben">Alle Aufgaben</a>`, saysAufgabenNicht)
	mustNotLeak(t, card, "500", "boom")

	old := backendWait
	backendWait = 100 * time.Millisecond
	t.Cleanup(func() { backendWait = old })
	cl, _ := slowAufgaben(t, time.Second)
	began := time.Now()
	_, card, _ = familyPage(t, Mehr, StartConfig{Laib: cl}, "GET", MehrAufgabenPath, "family")
	if d := time.Since(began); d > time.Second {
		t.Fatalf("the part waited %v for a silent Laib", d)
	}
	mustContain(t, card, saysAufgabenNicht)
}
