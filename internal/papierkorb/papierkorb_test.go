package papierkorb

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestKorb(t *testing.T) {
	now := time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)
	k := &Korb{Dir: t.TempDir(), Now: func() time.Time { return now }}
	e, err := k.Rein("liste", "Baumarkt", "anna", map[string]string{"ref": "liste-baumarkt"})
	if err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(filepath.Join(k.Dir, "papierkorb", e.ID+".json")); fi == nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("not kept 0600: %v", fi)
	}
	if got, err := k.Holen("liste", e.ID); err != nil || got.Name != "Baumarkt" || got.Wer != "anna" {
		t.Fatalf("Holen: %v %v", got, err)
	}
	if _, err := k.Holen("kalender", e.ID); err != ErrWeg {
		t.Error("another kind's entry")
	}
	for _, bad := range []string{"../x", "", "ABCDEF0123456789"} {
		if _, err := k.Holen("liste", bad); err != ErrWeg {
			t.Errorf("%q: %v", bad, err)
		}
	}
	if l := k.Liste("liste"); len(l) != 1 || !l[0].Bis().Equal(now.Add(Frist)) {
		t.Fatalf("Liste: %v", l)
	}
	now = now.Add(Frist + time.Minute)
	if l := k.Liste(""); len(l) != 0 {
		t.Fatalf("after 30 days: %v", l)
	}
	if _, err := os.Stat(filepath.Join(k.Dir, "papierkorb", e.ID+".json")); err == nil {
		t.Error("the old entry's file stays")
	}
	e2, _ := k.Rein("musik", "Album", "anna", nil)
	k.Raus(e2.ID)
	if _, err := k.Holen("musik", e2.ID); err != ErrWeg {
		t.Error("out but still there")
	}
}
