package app

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// haus#26, laib ADR 0022 §4: nothing old is carried. No Go file — code or
// test — names the Solaris app, ServiceBay or the old token prefix; this
// rule's own test is the one place the words appear.
func TestNoOldNamesInGoCode(t *testing.T) {
	root := filepath.Join("..", "..")
	self := filepath.Join("internal", "app", "names_test.go")
	seen := 0
	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if rel == self {
			return nil
		}
		seen++
		b, _ := os.ReadFile(path)
		low := strings.ToLower(string(b))
		for _, name := range []string{"solaris", "servicebay", "sol_device"} {
			if strings.Contains(low, name) {
				t.Errorf("%s names %q (laib ADR 0022 §4: nothing old is carried)", filepath.ToSlash(rel), name)
			}
		}
		return nil
	})
	if seen < 20 {
		t.Fatalf("read %d Go files; the walk did not find the repo", seen)
	}
}
