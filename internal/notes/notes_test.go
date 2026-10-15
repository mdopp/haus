package notes

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

var bg = context.Background()

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
	res, err := d.Search(bg, "APFEL", 10)
	hits := res.Hits
	if err != nil || res.Total != 2 || res.Partial {
		t.Fatalf("%+v %v", res, err)
	}
	if hits[0].Title != "Apfelkuchen" || hits[0].Name != "rezepte/kuchen.md" || strings.Join(hits[0].Lines, "|") != "200 g Mehl|3 Äpfel|Zimt" {
		t.Fatalf("title match first, first lines: %+v", hits[0])
	}
	if hits[1].Title != "ohne-titel" || hits[1].Lines[0] != "nur Text über apfel" {
		t.Fatalf("a note without a heading is named by its file: %+v", hits[1])
	}
	res, _ = d.Search(bg, "äpfel", 1)
	if res.Total != 2 || len(res.Hits) != 1 {
		t.Fatalf("capped at max, the total said: %+v", res)
	}
	if _, err := d.Search(bg, "  ", 10); err == nil {
		t.Fatal("an empty search is an error")
	}
}

func TestRead(t *testing.T) {
	d, dir := open(t)
	os.MkdirAll(filepath.Join(dir, "rezepte"), 0o755)
	os.WriteFile(filepath.Join(dir, "rezepte", "kuchen.md"), []byte("# Apfelkuchen\n\nMehl\n"), 0o644)
	name, _ := d.Create("WLAN Gäste", "Netz: Gast")
	for _, ref := range []string{name, "WLAN Gäste", "wlan gäste"} {
		n, err := d.Read(bg, ref)
		if err != nil || n.Name != "wlan-gaeste.md" || n.Title != "WLAN Gäste" || !strings.Contains(n.Text, "Netz: Gast") {
			t.Fatalf("%q: %+v %v", ref, n, err)
		}
	}
	for _, ref := range []string{"rezepte/kuchen.md", "Apfelkuchen"} {
		if n, err := d.Read(bg, ref); err != nil || n.Name != "rezepte/kuchen.md" {
			t.Fatalf("%q: %+v %v", ref, n, err)
		}
	}
	if _, err := d.Read(bg, "gibt es nicht"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	os.WriteFile(filepath.Join(dir, "lang.md"), []byte(strings.Repeat("x", maxRead+100)), 0o644)
	if n, err := d.Read(bg, "lang.md"); err != nil || !n.Truncated || len(n.Text) != maxRead {
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
	if err := os.Symlink(outside, filepath.Join(dir, "raus")); err != nil {
		t.Fatal(err)
	}
	rel, _ := filepath.Rel(dir, secret)
	for _, ref := range []string{rel, "../geheim.md", secret, "/etc/passwd", "rezepte/../../geheim.md", `..\geheim.md`, "link.md", "raus/geheim.md"} {
		n, err := d.Read(bg, ref)
		if err == nil || strings.Contains(n.Text, "nicht für den Bote") {
			t.Fatalf("%q was read: %+v", ref, n)
		}
		if !errors.Is(err, ErrOutside) {
			t.Fatalf("%q: want ErrOutside, got %v", ref, err)
		}
	}
	if res, _ := d.Search(bg, "Bote", 10); len(res.Hits) != 0 {
		t.Fatalf("a symlink out is not searched: %+v", res)
	}
}

// Hidden files and folders are not notes: Syncthing's versions, the
// temp files, dotfiles are never read, whatever the name says.
func TestReadRefusesHidden(t *testing.T) {
	d, dir := open(t)
	os.MkdirAll(filepath.Join(dir, ".stversions"), 0o755)
	os.MkdirAll(filepath.Join(dir, "sub"), 0o755)
	os.WriteFile(filepath.Join(dir, ".stversions", "alt.md"), []byte("# alt"), 0o644)
	os.WriteFile(filepath.Join(dir, "sub", ".geheim.md"), []byte("# geheim"), 0o644)
	os.WriteFile(filepath.Join(dir, ".haus-1.tmp"), []byte("halb"), 0o644)
	for _, ref := range []string{".stversions/alt.md", "sub/.geheim.md", ".haus-1.tmp", "./.stversions/alt.md"} {
		if n, err := d.Read(bg, ref); !errors.Is(err, ErrHidden) {
			t.Fatalf("%q: want ErrHidden, got %+v %v", ref, n, err)
		}
	}
}

// A share without hard links gets an error, never a half-written or
// overwritten note; no temp file stays.
func TestCreateWithoutHardLinks(t *testing.T) {
	d, dir := open(t)
	old := link
	t.Cleanup(func() { link = old })
	link = func(*os.Root, string, string) error {
		return &os.LinkError{Op: "link", Err: syscall.EPERM}
	}
	if name, err := d.Create("Einkauf", "Milch"); err == nil {
		t.Fatalf("want an error, got %q", name)
	}
	if es, _ := os.ReadDir(dir); len(es) != 0 {
		t.Fatalf("left behind: %v", es)
	}
}

func TestTitleIsCapped(t *testing.T) {
	d, _ := open(t)
	if _, err := d.Create(strings.Repeat("a", MaxTitle+1), "x"); err == nil {
		t.Fatal("an over-long title is refused")
	}
	if _, err := d.Create(strings.Repeat("ä", MaxTitle), "x"); err != nil {
		t.Fatalf("the cap counts characters, not bytes: %v", err)
	}
}

// A search reads at most maxFiles files and maxScan bytes, and says when
// it stopped early; a cancelled request stops it.
func TestSearchIsCapped(t *testing.T) {
	d, dir := open(t)
	for _, n := range []string{"a", "b", "c", "d"} {
		os.WriteFile(filepath.Join(dir, n+".md"), []byte("# "+n+"\n\napfel\n"), 0o644)
	}
	oldF, oldS := maxFiles, maxScan
	t.Cleanup(func() { maxFiles, maxScan = oldF, oldS })
	maxFiles = 2
	res, err := d.Search(bg, "apfel", 10)
	if err != nil || res.Total != 2 || !res.Partial {
		t.Fatalf("file cap: %+v %v", res, err)
	}
	maxFiles, maxScan = 100, 30
	res, err = d.Search(bg, "apfel", 10)
	if err != nil || res.Total > 2 || !res.Partial {
		t.Fatalf("byte cap: %+v %v", res, err)
	}
	maxScan = oldS
	ctx, cancel := context.WithCancel(bg)
	cancel()
	if _, err := d.Search(ctx, "apfel", 10); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled: %v", err)
	}
	if _, err := d.Read(ctx, "nicht-da"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled read by title: %v", err)
	}
}

// haus#12: the Start page shows the newest notes: by modification time,
// newest first, at most n, hidden files and folders and non-notes left out.
func TestRecentNewestFirst(t *testing.T) {
	d, dir := open(t)
	base := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	put := func(name, body string, age time.Duration) {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, base.Add(-age), base.Add(-age)); err != nil {
			t.Fatal(err)
		}
	}
	put("alt.md", "# Alte Notiz\n", 72*time.Hour)
	put("einkauf.md", "# Einkaufsliste\nMilch\n", time.Hour)
	put("urlaub/packliste.md", "# Packliste\n", 2*time.Hour)
	put("wlan.txt", "Gäste-WLAN steht auf dem Router\n", 3*time.Hour)
	put(".versteckt.md", "# Geheim\n", 0)
	put(".stversions/einkauf~1.md", "# Einkaufsliste alt\n", 0)
	put("bild.jpg", "jpeg", 0)
	got, err := d.Recent(bg, 3)
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, n := range got {
		titles = append(titles, n.Title)
		if n.Changed.IsZero() {
			t.Fatalf("%s: no time", n.Name)
		}
	}
	if strings.Join(titles, "|") != "Einkaufsliste|Packliste|wlan" {
		t.Fatalf("recent: %v", titles)
	}
	if !got[0].Changed.Equal(base.Add(-time.Hour)) {
		t.Fatalf("changed: %v", got[0].Changed)
	}
	if none, err := d.Recent(bg, 0); err != nil || len(none) != 0 {
		t.Fatalf("n=0: %v %v", none, err)
	}
}

// Lookup is a note by its exact file name, as the memory library names it
// (haus#13): never by slug or title, never outside the folder, never a
// hidden file, a folder or what is not a note.
func TestLookupIsExact(t *testing.T) {
	d, dir := open(t)
	os.MkdirAll(filepath.Join(dir, "rezepte"), 0o755)
	os.MkdirAll(filepath.Join(dir, ".stversions"), 0o755)
	for name, body := range map[string]string{
		"kaminkehrer.md":         "# Kaminkehrer\n\nHerr Brandl kommt im Oktober.\n",
		"rezepte/pfannkuchen.md": "# Pfannkuchen\n\nMehl, Milch, Eier\n",
		"Größen Kinder.md":       "Lena 31, Max 26\n",
		".stversions/alt.md":     "# Alt\n",
		"rechnung.pdf":           "%PDF",
		"../draussen.md":         "# Draußen\n",
		"rezepte/ordner.md/x.md": "# Ordner\n",
	} {
		os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755)
		os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644)
	}
	os.Symlink(filepath.Join(dir, "..", "draussen.md"), filepath.Join(dir, "link.md"))
	// a file over the size the word search reads is no note here either
	os.WriteFile(filepath.Join(dir, "riesig.md"), append([]byte("# Riesig\n\n"), make([]byte, maxBytes)...), 0o644)
	for name, want := range map[string]string{"kaminkehrer.md": "Kaminkehrer", "rezepte/pfannkuchen.md": "Pfannkuchen", "Größen Kinder.md": "Größen Kinder"} {
		e, err := d.Lookup(bg, name)
		if err != nil || e.Name != name || e.Title != want || e.Changed.IsZero() {
			t.Fatalf("%s: %+v %v", name, e, err)
		}
	}
	for name, want := range map[string]error{
		"Kaminkehrer": ErrNotFound, "kaminkehrer": ErrNotFound, "riesig.md": ErrNotFound, "gibtsnicht.md": ErrNotFound, "rechnung.pdf": ErrNotFound,
		"rezepte/ordner.md": ErrNotFound, "rezepte": ErrNotFound,
		"../draussen.md": ErrOutside, "/etc/passwd": ErrOutside, "rezepte/../../draussen.md": ErrOutside, `..\draussen.md`: ErrOutside,
		"link.md": ErrOutside, ".stversions/alt.md": ErrHidden, "": ErrNotFound,
	} {
		if e, err := d.Lookup(bg, name); !errors.Is(err, want) {
			t.Fatalf("%q: %+v %v, want %v", name, e, err, want)
		}
	}
	// a folder that cannot be read is an error, not "no such note"
	os.RemoveAll(dir)
	if _, err := d.Lookup(bg, "kaminkehrer.md"); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("gone folder: %v", err)
	}
}

const tableNote = "# Handwerker\n\n| Gewerk | Firma | Telefon | Notiz |\n|---|---|:---:|---|\n| Heizung | Huber GmbH | 089 111 | Wartung im Herbst |\n| Dach | **Schmid** | 089 222 | |\n"

const listNote = "# Packliste Ski\n\n- Skischuhe\n* Helm\n+ Brille\n- [ ] Schneeketten\n- [x] Skipass\n## Für die Kinder\n> **Wichtig**: `Sonnencreme`\n1. Mütze\n"

// A passage is shown as plain text (haus#13): no table header or
// separator row, a table row as each value after its column's header
// ("Strom 41.230"; a column without a header: the value alone), joined
// by " · ", no list dash,
// checkbox, quote mark, emphasis or heading hash; the note's "# " title
// is the hit's title, not a line. A passage that is only a table's header
// is the note's first lines with real content. At most n lines, each cut
// at 120 characters.
func TestPassage(t *testing.T) {
	for _, c := range []struct {
		name string
		raw  []string
		body string
		n    int
		want string
	}{
		{"only the header", []string{"# Handwerker", "| Gewerk | Firma | Telefon | Notiz |"}, tableNote, 3,
			"Gewerk Heizung · Firma Huber GmbH · Telefon 089 111 · Notiz Wartung im Herbst|Gewerk Dach · Firma Schmid · Telefon 089 222"},
		{"separator and a row", []string{"|---|---|:---:|---|", "| Dach | **Schmid** | 089 222 | |"}, tableNote, 3, "Gewerk Dach · Firma Schmid · Telefon 089 222"},
		{"header with its separator", []string{"| Gewerk | Firma | Telefon | Notiz |", "|---|---|:---:|---|", "| Heizung | Huber GmbH | 089 111 | Wartung im Herbst |"}, tableNote, 3,
			"Gewerk Heizung · Firma Huber GmbH · Telefon 089 111 · Notiz Wartung im Herbst"},
		{"a list", nil, listNote, 10, "Skischuhe|Helm|Brille|Schneeketten|Skipass|Für die Kinder|Wichtig: Sonnencreme|1. Mütze"},
		{"a list passage", []string{"- Skischuhe", "* Helm"}, listNote, 3, "Skischuhe|Helm"},
		{"at most n", nil, listNote, 2, "Skischuhe|Helm"},
		{"qmd's lines first", []string{"- [ ] Schneeketten"}, listNote, 3, "Schneeketten"},
		{"nothing but markup", []string{"|---|"}, "# Leer\n\n|---|\n", 3, ""},
		{"a column without a header", nil, "# Putzplan\n\n| | Montag | Dienstag |\n|---|---|---|\n| Anna | Bad | |\n", 3, "Anna · Montag Bad"},
		{"meter readings", []string{"| 31.12.2024 | 41.230 | 18.442 | 812 |"}, "# Zählerstände\n\n| Datum | Strom | Gas | Wasser |\n|---|---|---|---|\n| 31.12.2024 | 41.230 | 18.442 | 812 |\n", 3,
			"Datum 31.12.2024 · Strom 41.230 · Gas 18.442 · Wasser 812"},
		{"a row of no known table", []string{"| a | b |"}, "", 3, "a · b"},
	} {
		got := strings.Join(Passage(c.raw, c.body, c.n), "|")
		if got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
	long := strings.Repeat("Wort ", 60)
	if l := Passage([]string{"- " + long}, "", 3); len(l) != 1 || len([]rune(l[0])) > 121 || !strings.HasSuffix(l[0], "…") {
		t.Fatalf("cut: %q", l)
	}
}

// The word search's lines are plain too.
func TestSearchLinesArePlain(t *testing.T) {
	d, dir := open(t)
	os.WriteFile(filepath.Join(dir, "handwerker.md"), []byte(tableNote), 0o644)
	os.WriteFile(filepath.Join(dir, "packliste.md"), []byte(listNote), 0o644)
	res, err := d.Search(bg, "Heizung", 10)
	if err != nil || len(res.Hits) != 1 || strings.Join(res.Hits[0].Lines, "|") != "Gewerk Heizung · Firma Huber GmbH · Telefon 089 111 · Notiz Wartung im Herbst|Gewerk Dach · Firma Schmid · Telefon 089 222" {
		t.Fatalf("%+v %v", res, err)
	}
	res, _ = d.Search(bg, "Helm", 10)
	if len(res.Hits) != 1 || strings.Join(res.Hits[0].Lines, "|") != "Skischuhe|Helm|Brille" {
		t.Fatalf("%+v", res)
	}
}

// A sentence is not one string to find (haus#13): a note matches when it
// holds any of the query's meaningful words — short and common ones left
// out — and the notes with more of them come first, a title match before
// a text match. A query of only common words is looked for as it is.
func TestSearchAnyWord(t *testing.T) {
	d, dir := open(t)
	write := func(name, body string) { os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644) }
	write("kaminkehrer.md", "# Kaminkehrer\n\nHerr Brandl kommt im Oktober.\nRechnung für die Steuer aufheben.\n")
	write("steuer.md", "# Steuer 2025\n\nRechnung vom Kaminkehrer einreichen.\n")
	write("brandl.md", "# Bezahlt\n\nRechnung bezahlt.\n")
	write("woerter.md", "# Wann\n\nder die das, wann kommt wo ist mit\n")
	write("kabel.md", "# Kabel\n\nWo ist das Ladekabel? Im Flur.\n")
	res, err := d.Search(bg, "Wann kommt der Kaminkehrer mit der Rechnung?", 10)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, h := range res.Hits {
		got = append(got, h.Name)
	}
	if strings.Join(got, ",") != "kaminkehrer.md,steuer.md,brandl.md" || res.Total != 3 {
		t.Fatalf("hits %v (%d)", got, res.Total)
	}
	res, _ = d.Search(bg, "wo ist das", 10)
	if len(res.Hits) != 1 || res.Hits[0].Name != "kabel.md" {
		t.Fatalf("only common words, as a phrase: %+v", res)
	}
	if res, _ := d.Search(bg, "wann kommt der Schornsteinfeger", 10); res.Total != 0 {
		t.Fatalf("no meaningful word in any note: %+v", res)
	}
}

// Words are the query's meaningful words as written, each once (haus#13).
func TestWords(t *testing.T) {
	for q, want := range map[string]string{
		"Wann kommt der Schornsteinfeger?": "Schornsteinfeger",
		"Wo liegt der Reisepass von Ben":   "liegt,Reisepass,Ben",
		"wo ist das":                       "",
		"Größe der Fußmatte, WC":           "Größe,Fußmatte",
		`"><script>alert(1)</script>`:      "script,alert",
	} {
		if got := strings.Join(Words(q), ","); got != want {
			t.Errorf("%q: %q, want %q", q, got, want)
		}
	}
}
