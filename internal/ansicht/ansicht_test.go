package ansicht

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// haus#66: each person's view changes alone; a file that does not read is
// never written over.
func TestEachPersonsViewAlone(t *testing.T) {
	dir := t.TempDir()
	s := Open(dir, "homes")
	yes, no := true, false
	if err := s.Set("anna", "/homes/haushalt/", &yes, "#2f6fdf"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("ben", "/homes/haushalt/", nil, "#c92a2a"); err != nil {
		t.Fatal(err)
	}
	a, _ := s.For("anna")
	b, _ := s.For("ben")
	if a.Shows("/homes/haushalt/") || a.Color("/homes/haushalt/") != "#2f6fdf" || !b.Shows("/homes/haushalt/") || b.Color("/homes/haushalt/") != "#c92a2a" {
		t.Fatalf("anna %+v ben %+v", a, b)
	}
	s.Set("anna", "/homes/haushalt/", &no, "-")
	if a, _ = s.For("anna"); !a.Shows("/homes/haushalt/") || a.Color("/homes/haushalt/") != "" {
		t.Fatalf("back: %+v", a)
	}
	for _, bad := range []string{"red", "#FFF", "#12345g"} {
		if s.Set("anna", "/homes/haushalt/", nil, bad) == nil {
			t.Fatalf("colour %q taken", bad)
		}
	}
	if s.Set("", "/homes/haushalt/", &yes, "") == nil || s.Set("anna", "haushalt", &yes, "") == nil {
		t.Fatal("no person or no calendar taken")
	}
	os.WriteFile(filepath.Join(dir, File), []byte("{kaputt"), 0o600)
	if s.Set("anna", "/homes/haushalt/", &yes, "") == nil {
		t.Fatal("a broken file written over")
	}
	if got, _ := os.ReadFile(filepath.Join(dir, File)); string(got) != "{kaputt" {
		t.Fatalf("file changed: %s", got)
	}
}

// laib#1021: a view stored while Homes' account was haus is read as the
// account's now; a colour already under the new path wins, and the next
// Set writes the new paths.
func TestLegacyAccountPaths(t *testing.T) {
	dir := t.TempDir()
	old := `{"anna": {"aus": ["/haus/abo-feiertage-ni/", "/lea/abo-muell/", "/homes/abo-feiertage-ni/"],
	  "farben": {"/haus/haushalt/": "#2f6fdf", "/haus/familie-urlaub/": "#c92a2a", "/homes/familie-urlaub/": "#2b8a3e"}}}`
	if err := os.WriteFile(filepath.Join(dir, File), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	s := Open(dir, "homes")
	a, err := s.For("anna")
	if err != nil {
		t.Fatal(err)
	}
	if a.Shows("/homes/abo-feiertage-ni/") || !a.Shows("/lea/abo-feiertage/") || len(a.Hidden) != 2 {
		t.Fatalf("hidden: %v", a.Hidden)
	}
	if a.Color("/homes/haushalt/") != "#2f6fdf" || a.Color("/homes/familie-urlaub/") != "#2b8a3e" || a.Color("/haus/haushalt/") != "" || len(a.Colors) != 2 {
		t.Fatalf("colours: %v", a.Colors)
	}
	yes := true
	if err := s.Set("anna", "/homes/haushalt/", &yes, ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, File)); strings.Contains(string(got), "/haus/") {
		t.Fatalf("old paths written back: %s", got)
	}
	if a, _ := Open(dir, "haus").For("anna"); a.Color("/haus/haushalt/") != "" {
		t.Fatal("the mapping ran backwards")
	}
}
