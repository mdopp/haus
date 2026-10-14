package notes

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func open(t *testing.T) (*Dir, string) {
	t.Helper()
	dir := t.TempDir()
	d, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return d, dir
}

func TestSlug(t *testing.T) {
	for in, want := range map[string]string{
		"Einkaufsliste":           "einkaufsliste",
		"Größe der Fußmatte":      "groesse-der-fussmatte",
		"  WLAN: Passwort Gäste!": "wlan-passwort-gaeste",
		"../../etc/passwd":        "etc-passwd",
		".hidden":                 "hidden",
		"///":                     "notiz",
		"":                        "notiz",
	} {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
	if s := Slug(strings.Repeat("abc ", 40)); len(s) > 60 || strings.HasSuffix(s, "-") {
		t.Fatalf("long titles are cut: %q", s)
	}
}

// A note is "# title" plus the text in <slug>.md; the same title again is
// -2, -3: an existing note is never overwritten, and no temp file stays.
func TestCreateNeverOverwrites(t *testing.T) {
	d, dir := open(t)
	if err := os.WriteFile(filepath.Join(dir, "einkauf.md"), []byte("vom Telefon"), 0o644); err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"einkauf-2.md", "einkauf-3.md"} {
		name, err := d.Create("Einkauf", "Milch\nBrot "+string(rune('a'+i)))
		if err != nil || name != want {
			t.Fatalf("create %d: %q %v", i, name, err)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "einkauf.md")); string(b) != "vom Telefon" {
		t.Fatalf("the phone's note was touched: %q", b)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "einkauf-2.md"))
	if string(b) != "# Einkauf\n\nMilch\nBrot a\n" {
		t.Fatalf("content: %q", b)
	}
	es, _ := os.ReadDir(dir)
	if len(es) != 3 {
		t.Fatalf("only the notes, no temp files: %v", es)
	}
}

// A title is one line; a hostile title still lands inside the directory.
func TestCreateStaysInside(t *testing.T) {
	d, dir := open(t)
	name, err := d.Create("../../etc/passwd\nzweite Zeile", "x")
	if err != nil || name != "etc-passwd-zweite-zeile.md" {
		t.Fatalf("%q %v", name, err)
	}
	if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Create("   ", "x"); err == nil {
		t.Fatal("a note without a title is refused")
	}
	if _, err := d.Create("groß", strings.Repeat("x", maxText+1)); err == nil {
		t.Fatal("a note is not a document")
	}
}

func TestSearch(t *testing.T) {
	d, dir := open(t)
	os.MkdirAll(filepath.Join(dir, "rezepte"), 0o755)
	os.MkdirAll(filepath.Join(dir, ".stversions"), 0o755)
	write := func(name, body string) { os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644) }
	write("rezepte/kuchen.md", "# Apfelkuchen\n\n200 g Mehl\n3 Äpfel\nZimt\nZucker\n")
	write("garten.md", "# Garten\n\nTomaten gießen\nÄpfel ernten im Oktober\n")
	write("ohne-titel.md", "nur Text über apfel\n")
	write(".stversions/alt.md", "# Apfel alt\n")
	write("bild.jpg", "apfel")
	hits, total, err := d.Search("APFEL", 10)
	if err != nil || total != 2 {
		t.Fatalf("%+v %d %v", hits, total, err)
	}
	if hits[0].Title != "Apfelkuchen" || hits[0].Name != "rezepte/kuchen.md" || strings.Join(hits[0].Lines, "|") != "200 g Mehl|3 Äpfel|Zimt" {
		t.Fatalf("title match first, first lines: %+v", hits[0])
	}
	if hits[1].Title != "ohne-titel" || hits[1].Lines[0] != "nur Text über apfel" {
		t.Fatalf("a note without a heading is named by its file: %+v", hits[1])
	}
	hits, total, _ = d.Search("äpfel", 1)
	if total != 2 || len(hits) != 1 {
		t.Fatalf("capped at max, the total said: %+v %d", hits, total)
	}
	if _, _, err := d.Search("  ", 10); err == nil {
		t.Fatal("an empty search is an error")
	}
}

func TestRead(t *testing.T) {
	d, dir := open(t)
	os.MkdirAll(filepath.Join(dir, "rezepte"), 0o755)
	os.WriteFile(filepath.Join(dir, "rezepte", "kuchen.md"), []byte("# Apfelkuchen\n\nMehl\n"), 0o644)
	name, _ := d.Create("WLAN Gäste", "Netz: Gast")
	for _, ref := range []string{name, "WLAN Gäste", "wlan gäste"} {
		n, err := d.Read(ref)
		if err != nil || n.Name != "wlan-gaeste.md" || n.Title != "WLAN Gäste" || !strings.Contains(n.Text, "Netz: Gast") {
			t.Fatalf("%q: %+v %v", ref, n, err)
		}
	}
	for _, ref := range []string{"rezepte/kuchen.md", "Apfelkuchen"} {
		if n, err := d.Read(ref); err != nil || n.Name != "rezepte/kuchen.md" {
			t.Fatalf("%q: %+v %v", ref, n, err)
		}
	}
	if _, err := d.Read("gibt es nicht"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	os.WriteFile(filepath.Join(dir, "lang.md"), []byte(strings.Repeat("x", maxRead+100)), 0o644)
	if n, err := d.Read("lang.md"); err != nil || !n.Truncated || len(n.Text) != maxRead {
		t.Fatalf("a long note is cut: %v %v %d", err, n.Truncated, len(n.Text))
	}
}

// No name leaves the directory: not "..", not an absolute path, not a
// symlink that points out.
func TestReadNeverLeavesTheDirectory(t *testing.T) {
	outside := t.TempDir()
	secret := filepath.Join(outside, "geheim.md")
	os.WriteFile(secret, []byte("# Geheim\n\nnicht für den Bote"), 0o644)
	d, dir := open(t)
	if err := os.Symlink(secret, filepath.Join(dir, "link.md")); err != nil {
		t.Fatal(err)
	}
	rel, _ := filepath.Rel(dir, secret)
	for _, ref := range []string{rel, "../geheim.md", secret, "/etc/passwd", "rezepte/../../geheim.md", `..\geheim.md`, "link.md"} {
		n, err := d.Read(ref)
		if err == nil || strings.Contains(n.Text, "nicht für den Bote") {
			t.Fatalf("%q was read: %+v", ref, n)
		}
		if ref != "link.md" && !errors.Is(err, ErrOutside) {
			t.Fatalf("%q: want ErrOutside, got %v", ref, err)
		}
	}
	if hits, _, _ := d.Search("Bote", 10); len(hits) != 0 {
		t.Fatalf("a symlink out is not searched: %+v", hits)
	}
}
