package notes

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// haus#116: a note's head — kind, tags, source — read from the front
// matter in its flow and block forms, older lines kept as they were; no
// head, an unclosed one or an unknown kind change nothing of the text.
func TestSplitKopf(t *testing.T) {
	k, rest := SplitKopf("---\nart: rezept\ntags: [Kochen, \"Vegetarisch\", kochen]\nquelle: https://www.chefkoch.de/r/1\nid: 4cfd\n---\n\n# Lasagne\n\nNudeln\n")
	if k.Art != "rezept" || !reflect.DeepEqual(k.Tags, []string{"kochen", "vegetarisch"}) || k.Quelle != "https://www.chefkoch.de/r/1" || rest != "# Lasagne\n\nNudeln\n" {
		t.Fatalf("%+v %q", k, rest)
	}
	if got := k.Text(); got != "---\nart: rezept\ntags: [kochen, vegetarisch]\nquelle: https://www.chefkoch.de/r/1\nid: 4cfd\n---\n" {
		t.Fatalf("text %q", got)
	}
	k, _ = SplitKopf("---\ntags:\n  - Reisen\n  - Sommer 2027\ntitle: Ideen\n---\nText")
	if !reflect.DeepEqual(k.Tags, []string{"reisen", "sommer-2027"}) || k.Titel != "Ideen" {
		t.Fatalf("block list %+v", k)
	}
	for _, s := range []string{"# Ohne Kopf\n", "---\nart: rezept\nnie geschlossen", "--- nicht am Anfang\n---\n"} {
		if k, rest := SplitKopf(s); !k.Empty() || rest != s {
			t.Errorf("%q → %+v %q", s, k, rest)
		}
	}
	if k, _ := SplitKopf("---\nart: geheim\n---\n"); k.Art != "" || !strings.Contains(k.Text(), "art: geheim") {
		t.Errorf("unknown kind: %+v", k)
	}
	if Tag("  Sommer 2027! ") != "sommer-2027" || Tag("!!!") != "" || len(Tag(strings.Repeat("a", 99))) != maxTagLen {
		t.Error("Tag")
	}
}

// A note made with a head carries it, its title stays the "# " line;
// ByTags finds the notes with every tag asked for, newest first, and
// counts each tag over all notes.
func TestCreateWithAndByTags(t *testing.T) {
	dir := t.TempDir()
	d, _ := Open(dir)
	name, err := d.CreateWith("Lasagne", "Nudeln, Tomaten", Kopf{Art: "rezept", Tags: []string{"Kochen", "italienisch"}})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, name))
	if !strings.HasPrefix(string(b), "---\nart: rezept\ntags: [kochen, italienisch]\n---\n\n# Lasagne\n") {
		t.Fatalf("%q", b)
	}
	n, _ := d.Read(context.Background(), name)
	if n.Title != "Lasagne" || n.Kopf.Art != "rezept" {
		t.Fatalf("%+v", n)
	}
	os.WriteFile(filepath.Join(dir, "curry.md"), []byte("---\ntags: [kochen]\n---\n# Curry\n"), 0o644)
	old := time.Now().Add(-time.Hour)
	os.Chtimes(filepath.Join(dir, "curry.md"), old, old)
	os.WriteFile(filepath.Join(dir, "rom.md"), []byte("---\ntags: [reisen, italienisch]\n---\n# Rom\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "ohne.md"), []byte("# Ohne\n"), 0o644)
	got, counts, err := d.ByTags(context.Background(), []string{"Kochen"}, 10)
	if err != nil || len(got) != 2 || got[0].Title != "Lasagne" || got[1].Title != "Curry" {
		t.Fatalf("%+v %v", got, err)
	}
	if !reflect.DeepEqual(counts, []TagCount{{"italienisch", 2}, {"kochen", 2}, {"reisen", 1}}) {
		t.Fatalf("counts %v", counts)
	}
	if got, _, _ := d.ByTags(context.Background(), []string{"kochen", "italienisch"}, 10); len(got) != 1 || got[0].Title != "Lasagne" {
		t.Fatalf("both tags %+v", got)
	}
}
