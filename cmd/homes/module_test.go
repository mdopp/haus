package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/mdopp/homes/internal/modul"
)

// TestEveryVariableHasAnOwner (ADR 0001 §8): every HOMES_* the production
// code reads is a module's or the core's, and every one a module names is
// read somewhere, so the catalog cannot go stale.
func TestEveryVariableHasAnOwner(t *testing.T) {
	s, _, _ := fullSite(t)
	owned := slices.Clone(kern)
	for _, m := range katalog(s, "") {
		owned = append(owned, m.Env...)
	}
	read := map[string]string{}
	re := regexp.MustCompile(`"((?:HOMES|LAIB)_[A-Z0-9_]+)`)
	for _, root := range []string{"../../cmd", "../../internal"} {
		filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") || strings.Contains(p, "fake") {
				return err
			}
			b, _ := os.ReadFile(p)
			for _, m := range re.FindAllStringSubmatch(string(b), -1) {
				read[m[1]] = p
			}
			return nil
		})
	}
	if len(read) < 40 {
		t.Fatalf("found only %d variables: the walk missed the code", len(read))
	}
	for v, file := range read {
		if !slices.Contains(owned, v) {
			t.Errorf("%s (read in %s) belongs to no module's Env and is not in kern (cmd/homes/module.go)", v, file)
		}
	}
	for _, v := range owned {
		if _, ok := read[v]; !ok {
			t.Errorf("%s is named in the catalog but read nowhere", v)
		}
	}
}

func TestTheCatalogBuilds(t *testing.T) {
	full, _, _ := fullSite(t)
	full.podcasts, full.gesundheit = nil, ""
	l, err := modul.Bauen(katalog(full, "/musik-eingang"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"geraete", "energie", "kalender", "kontakte", "konto", "notizen", "dokumente", "musik"} {
		if !l.An(name) {
			t.Errorf("%s is off with its backend set", name)
		}
	}
	for _, name := range []string{"podcasts", "gesundheit", "standort", "sicherung", "sprechen"} {
		if l.An(name) {
			t.Errorf("%s is on without its backend", name)
		}
	}
	log := l.String()
	for _, want := range []string{"geraete (home-assistant)", "kalender (radicale)", "standort (fehlt: Auf dieser Box läuft kein Reitti.)"} {
		if !strings.Contains(log, want) {
			t.Errorf("the log line lacks %q: %s", want, log)
		}
	}
	empty, err := modul.Bauen(katalog(site{}, ""))
	if err != nil {
		t.Fatalf("a box with nothing: %v", err)
	}
	if strings.Contains(empty.String(), "Module an: geraete") || !strings.Contains(empty.String(), "energie (fehlt") {
		t.Errorf("a box with nothing: %s", empty)
	}
	names := map[string]bool{}
	for _, m := range katalog(site{}, "") {
		names[m.Name] = true
	}
	if len(names) != 18 {
		t.Errorf("the catalog has %d modules, ADR 0001 §7 lists 18", len(names))
	}
}
