package texte

import (
	"os"
	"regexp"
	"testing"
)

// TestEveryIDHasGerman: each id the package declares is in German.
func TestEveryIDHasGerman(t *testing.T) {
	src, err := os.ReadFile("katalog.go")
	if err != nil {
		t.Fatal(err)
	}
	ids := regexp.MustCompile(`ID = "([^"]+)"`).FindAllStringSubmatch(string(src), -1)
	if len(ids) < 10 {
		t.Fatalf("found %d ids", len(ids))
	}
	for _, m := range ids {
		if de[ID(m[1])] == "" {
			t.Errorf("%s has no German", m[1])
		}
	}
	if Titel("standort") != "Standortverlauf" || In("fr", SpurAus) != "ausgeschaltet" || T("gibts.nicht") != "gibts.nicht" {
		t.Errorf("Titel %q, fr falls back %q, unknown %q", Titel("standort"), In("fr", SpurAus), T("gibts.nicht"))
	}
}
