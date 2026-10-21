package ansicht

import (
	"os"
	"path/filepath"
	"testing"
)

// haus#66: each person's view changes alone; a file that does not read is
// never written over.
func TestEachPersonsViewAlone(t *testing.T) {
	dir := t.TempDir()
	s := Open(dir)
	yes, no := true, false
	if err := s.Set("anna", "/haus/haushalt/", &yes, "#2f6fdf"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("ben", "/haus/haushalt/", nil, "#c92a2a"); err != nil {
		t.Fatal(err)
	}
	a, _ := s.For("anna")
	b, _ := s.For("ben")
	if a.Shows("/haus/haushalt/") || a.Color("/haus/haushalt/") != "#2f6fdf" || !b.Shows("/haus/haushalt/") || b.Color("/haus/haushalt/") != "#c92a2a" {
		t.Fatalf("anna %+v ben %+v", a, b)
	}
	s.Set("anna", "/haus/haushalt/", &no, "-")
	if a, _ = s.For("anna"); !a.Shows("/haus/haushalt/") || a.Color("/haus/haushalt/") != "" {
		t.Fatalf("back: %+v", a)
	}
	for _, bad := range []string{"red", "#FFF", "#12345g"} {
		if s.Set("anna", "/haus/haushalt/", nil, bad) == nil {
			t.Fatalf("colour %q taken", bad)
		}
	}
	if s.Set("", "/haus/haushalt/", &yes, "") == nil || s.Set("anna", "haushalt", &yes, "") == nil {
		t.Fatal("no person or no calendar taken")
	}
	os.WriteFile(filepath.Join(dir, File), []byte("{kaputt"), 0o600)
	if s.Set("anna", "/haus/haushalt/", &yes, "") == nil {
		t.Fatal("a broken file written over")
	}
	if got, _ := os.ReadFile(filepath.Join(dir, File)); string(got) != "{kaputt" {
		t.Fatalf("file changed: %s", got)
	}
}
