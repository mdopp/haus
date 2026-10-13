package legacy

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// haus#5, laib ADR 0021 rule 8: a legacy name lives only in a named place.
// Solaris and ServiceBay names stay where installed phones or the migration
// need them — this package and the Solaris bridge — and nowhere else.
func TestLegacyNamesOnlyInTheirPlace(t *testing.T) {
	allowed := map[string]bool{
		"internal/legacy/legacy.go": true, // the app's contract
		"internal/napi/bridge.go":   true, // the Solaris migration bridge
	}
	root := filepath.Join("..", "..")
	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if allowed[filepath.ToSlash(rel)] {
			return nil
		}
		b, _ := os.ReadFile(path)
		low := strings.ToLower(string(b))
		for _, name := range []string{"solaris", "servicebay", "sol_device"} {
			if strings.Contains(low, name) {
				t.Errorf("%s names %q outside the legacy place (ADR 0021 rule 8)", rel, name)
			}
		}
		return nil
	})
}
