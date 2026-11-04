package reitti

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A token is kept only once Reitti said yes to it; the file is the
// person's alone (0600); Trennen forgets it.
func TestVerbindenKeepsOnlyATokenReittiTakes(t *testing.T) {
	var auth []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = append(auth, r.Header.Get("Authorization"))
		switch r.Header.Get("Authorization") {
		case "Bearer gut":
			w.Write([]byte(`{"result":"ok"}`))
		case "Bearer ohne-geraet":
			w.WriteHeader(500)
		default:
			w.WriteHeader(401)
		}
	}))
	defer srv.Close()
	r := &Reitti{URL: srv.URL, Dir: t.TempDir()}
	if err := r.Verbinden(t.Context(), "anna", "falsch"); !errors.Is(err, ErrToken) {
		t.Fatalf("an unknown token: %v", err)
	}
	var u *Unavailable
	if err := r.Verbinden(t.Context(), "anna", "ohne-geraet"); !errors.As(err, &u) || u.Status != 500 {
		t.Fatalf("a token without a device: %v", err)
	}
	if _, ok := r.Konto("anna"); ok {
		t.Fatal("a refused token was kept")
	}
	if err := r.Verbinden(t.Context(), "anna", "  gut\n"); err != nil {
		t.Fatal(err)
	}
	k, ok := r.Konto("anna")
	if !ok || k.Token != "gut" || k.Seit.IsZero() {
		t.Fatalf("kept %+v %v", k, ok)
	}
	if _, ok := r.Konto("ben"); ok {
		t.Fatal("ben has anna's")
	}
	fi, err := os.Stat(filepath.Join(r.Dir, kontenFile))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("the file: %v %v", fi, err)
	}
	if strings.Join(auth, ",") != "Bearer falsch,Bearer ohne-geraet,Bearer gut" {
		t.Errorf("Reitti saw %q", auth)
	}
	if err := r.Trennen("anna"); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.Konto("anna"); ok {
		t.Fatal("still connected after Trennen")
	}
	var none *Reitti
	if _, ok := none.Konto("anna"); ok {
		t.Fatal("no Reitti, no account")
	}
}

// Angekommen keeps when the newest point came, for the page: a later time
// only, never past now, and nothing for someone not connected.
func TestAngekommenKeepsTheNewestPointOnly(t *testing.T) {
	now := time.Date(2026, 11, 4, 17, 45, 0, 0, time.UTC)
	r := &Reitti{Dir: t.TempDir(), Now: func() time.Time { return now }}
	if err := r.save(map[string]Konto{"anna": {Token: "gut", Seit: now.Add(-time.Hour)}}); err != nil {
		t.Fatal(err)
	}
	if k, _ := r.Konto("anna"); !k.Zuletzt.IsZero() {
		t.Fatalf("a point before any came: %v", k.Zuletzt)
	}
	at := now.Add(-5 * time.Minute)
	for _, p := range []time.Time{at, at.Add(-time.Hour)} { // an older batch sent late changes nothing
		if err := r.Angekommen("anna", p); err != nil {
			t.Fatal(err)
		}
	}
	if k, _ := r.Konto("anna"); !k.Zuletzt.Equal(at) || k.Token != "gut" {
		t.Fatalf("kept %+v", k)
	}
	if err := r.Angekommen("anna", now.Add(time.Hour)); err != nil { // a phone clock ahead
		t.Fatal(err)
	}
	if k, _ := r.Konto("anna"); !k.Zuletzt.Equal(now) {
		t.Fatalf("a time past now: %v", k.Zuletzt)
	}
	if err := r.Angekommen("ben", now); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.Konto("ben"); ok {
		t.Fatal("a point made ben connected")
	}
}
