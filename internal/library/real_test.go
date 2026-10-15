package library

import (
	"os"
	"strings"
	"testing"

	"github.com/mdopp/haus/internal/notes"
	"github.com/mdopp/haus/internal/qmd"
)

// TestAgainstRealQmd runs only by hand, against a real Vermittler
// (laib#198) in front of qmd, indexing a notes folder as the collection
// "notizen" (haus#13): set HAUS_QMD_REAL to its address and
// HAUS_QMD_REAL_NOTES to the folder. CI never has one; the fake's shapes
// come from such a run (qmd/testdata).
// The folder is the eval's 44 German notes, the recipes moved to
// rezepte/, plus "Größen Kinder.md" and "schlüssel & keller (2).md".
func TestAgainstRealQmd(t *testing.T) {
	u, dir := os.Getenv("HAUS_QMD_REAL"), os.Getenv("HAUS_QMD_REAL_NOTES")
	if u == "" || dir == "" {
		t.Skip("no real qmd: set HAUS_QMD_REAL and HAUS_QMD_REAL_NOTES")
	}
	d, err := notes.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	c := &qmd.Client{URL: u, Collection: "notizen"}
	st, err := c.Status(bg)
	if err != nil || !st.Ready() {
		t.Fatalf("status: %+v %v", st, err)
	}
	for q, want := range map[string]string{
		"wann muss der Schornsteinfeger kommen": "kaminkehrer.md",
		"wie mache ich Teig für Pfannkuchen":    "rezepte/rezept-pfannkuchen.md",
		"Schuhgröße der Kinder":                 "Größen Kinder.md",
		"wo hängt der Kellerschlüssel":          "schlüssel & keller (2).md",
		"wer passt abends auf die Kinder auf":   "babysitter.md",
	} {
		got, err := Find(bg, c, Tidy{}, d, q, 5)
		if err != nil || got.Reduced || len(got.Hits) == 0 {
			t.Fatalf("%q: %+v %v", q, got, err)
		}
		if got.Hits[0].Name != want {
			t.Errorf("%q: first %s, want %s (%s)", q, got.Hits[0].Name, want, names(got.Hits))
		}
		for _, h := range got.Hits {
			if h.Title == "" || len(h.Lines) == 0 || strings.Contains(strings.Join(h.Lines, ""), "@@") || strings.HasPrefix(h.Name, ".") {
				t.Errorf("%q: %+v", q, h)
			}
		}
		t.Logf("%q → %s: %q", q, names(got.Hits), got.Hits[0].Lines)
	}
}
