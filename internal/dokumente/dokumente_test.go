package dokumente

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/haus/internal/paperless"
	"github.com/mdopp/haus/internal/paperless/paperlessfake"
)

var bg = context.Background()

const token = "pl-token"

// a Paperless with three documents of a made-up household
func archive(t *testing.T) (*paperlessfake.Server, *paperless.Client) {
	t.Helper()
	f := paperlessfake.New(token)
	t.Cleanup(f.Close)
	f.Correspondent(3, "Stadtwerke Musterstadt")
	f.Tag(4, "Strom")
	f.Tag(5, "Vertrag")
	f.Add(
		paperlessfake.Doc{ID: 7, Title: "Stromvertrag", Content: "Vertragslaufzeit bis 31.12.2027.\r\nKündigungsfrist drei Monate.", Correspondent: 3,
			Created: "2025-03-14", Tags: []int{4, 5}, Modified: "2025-03-15T10:20:30.123456+01:00"},
		paperlessfake.Doc{ID: 8, Title: "Zeugnis\nLena", Content: "Mathematik: gut", Created: "2026-07-24T00:00:00+02:00", Modified: "2026-07-25T08:00:00Z"},
		paperlessfake.Doc{ID: 12, Title: "", Content: "Ein Zettel ohne Titel", Modified: "2026-01-02T03:04:05Z"},
	)
	return f, &paperless.Client{URL: f.URL, Token: token}
}

func files(t *testing.T, dir string) []string {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// haus#13: each document Paperless holds becomes <id>.md — its title as
// the heading, then date, sender and tags as Paperless names them, then
// the text its OCR read; the file's time is the document's last change.
func TestMirrorWritesOneFilePerDocument(t *testing.T) {
	_, c := archive(t)
	dir := t.TempDir()
	rep, err := Mirror(bg, c, dir)
	if err != nil || rep != (Report{Written: 3}) {
		t.Fatalf("%+v %v", rep, err)
	}
	if got := strings.Join(files(t, dir), ","); got != "12.md,7.md,8.md" {
		t.Fatalf("files: %s", got)
	}
	want := "# Stromvertrag\n\nDatum: 14.03.2025\nVon: Stadtwerke Musterstadt\nSchlagworte: Strom, Vertrag\n\n" +
		"Vertragslaufzeit bis 31.12.2027.\nKündigungsfrist drei Monate.\n"
	if got := read(t, filepath.Join(dir, "7.md")); got != want {
		t.Fatalf("7.md:\n%q\nwant\n%q", got, want)
	}
	// a title over two lines is one heading; no date, no sender, no tags: no lines for them
	if got := read(t, filepath.Join(dir, "8.md")); got != "# Zeugnis Lena\n\nDatum: 24.07.2026\n\nMathematik: gut\n" {
		t.Fatalf("8.md: %q", got)
	}
	if got := read(t, filepath.Join(dir, "12.md")); got != "# Dokument 12\n\nEin Zettel ohne Titel\n" {
		t.Fatalf("12.md: %q", got)
	}
	st, _ := os.Stat(filepath.Join(dir, "7.md"))
	if want := time.Date(2025, 3, 15, 9, 20, 30, 0, time.UTC); st.ModTime().Unix() != want.Unix() {
		t.Fatalf("time: %v, want %v", st.ModTime(), want)
	}
}

// A second run reads no document's text again unless Paperless changed
// it since; a changed one is written anew, a deleted one is deleted.
// What else lies in the folder is not the mirror's.
func TestMirrorFollowsPaperless(t *testing.T) {
	f, c := archive(t)
	dir := t.TempDir()
	if _, err := Mirror(bg, c, dir); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "liesmich.txt"), []byte("nicht meins"), 0o644)
	os.WriteFile(filepath.Join(dir, "007.md"), []byte("nicht meins"), 0o644)
	n := len(f.Seen())
	rep, err := Mirror(bg, c, dir)
	if err != nil || rep != (Report{Unchanged: 3}) {
		t.Fatalf("again: %+v %v", rep, err)
	}
	for _, r := range f.Seen()[n:] {
		if strings.Contains(r.URL.Query().Get("fields"), "content") {
			t.Fatalf("a document's text read again: %s", r.URL)
		}
	}

	f.Change(paperlessfake.Doc{ID: 7, Title: "Stromvertrag", Content: "Verlängert bis 2029.", Correspondent: 3, Created: "2025-03-14",
		Modified: "2026-10-01T12:00:00Z"})
	f.Remove(8)
	rep, err = Mirror(bg, c, dir)
	if err != nil || rep != (Report{Written: 1, Unchanged: 1, Removed: 1}) {
		t.Fatalf("after a change: %+v %v", rep, err)
	}
	if got := strings.Join(files(t, dir), ","); got != "007.md,12.md,7.md,liesmich.txt" {
		t.Fatalf("files: %s", got)
	}
	if got := read(t, filepath.Join(dir, "7.md")); !strings.Contains(got, "Verlängert bis 2029.") || strings.Contains(got, "31.12.2027") {
		t.Fatalf("7.md not rewritten: %q", got)
	}
}

// Many documents: the listing goes page by page, all of them; the texts
// come a few per request.
func TestMirrorReadsEveryPage(t *testing.T) {
	f := paperlessfake.New(token)
	defer f.Close()
	for i := 1; i <= 230; i++ {
		f.Add(paperlessfake.Doc{ID: i, Title: fmt.Sprintf("Brief %d", i), Content: "Text", Modified: "2026-01-01T00:00:00Z"})
	}
	dir := t.TempDir()
	rep, err := Mirror(bg, &paperless.Client{URL: f.URL, Token: token}, dir)
	if err != nil || rep.Written != 230 || len(files(t, dir)) != 230 {
		t.Fatalf("%+v %v (%d files)", rep, err, len(files(t, dir)))
	}
	pages, texts := 0, 0
	for _, r := range f.Seen() {
		q := r.URL.Query()
		switch {
		case q.Get("fields") == "id,modified":
			pages++
		case strings.Contains(q.Get("fields"), "content"):
			texts++
			if n := len(strings.Split(q.Get("id__in"), ",")); n > batch {
				t.Fatalf("%d texts in one request", n)
			}
		}
	}
	if pages != 3 || texts != 23 {
		t.Fatalf("%d listing pages, %d text requests", pages, texts)
	}
}

// Paperless not answering, refusing the token or failing midway leaves
// the folder as it was — a run that cannot list deletes nothing — and
// says it is Paperless's trouble (SourceError), not the folder's.
func TestMirrorKeepsTheFolderWhenPaperlessFails(t *testing.T) {
	for _, c := range []struct {
		name  string
		spoil func(*paperlessfake.Server, *paperless.Client)
	}{
		{"down", func(f *paperlessfake.Server, _ *paperless.Client) { f.Close() }},
		{"500", func(f *paperlessfake.Server, _ *paperless.Client) { f.Status = http.StatusInternalServerError }},
		{"token refused", func(_ *paperlessfake.Server, c *paperless.Client) { c.Token = "falsch" }},
	} {
		f, cl := archive(t)
		dir := t.TempDir()
		if _, err := Mirror(bg, cl, dir); err != nil {
			t.Fatal(err)
		}
		f.Remove(8) // would be deleted, could the run list
		c.spoil(f, cl)
		_, err := Mirror(bg, cl, dir)
		var src SourceError
		if !errors.As(err, &src) {
			t.Fatalf("%s: %v, want a SourceError", c.name, err)
		}
		if strings.Contains(err.Error(), token) {
			t.Fatalf("%s: the token in %v", c.name, err)
		}
		if got := strings.Join(files(t, dir), ","); got != "12.md,7.md,8.md" {
			t.Fatalf("%s: files: %s", c.name, got)
		}
	}
}

// A folder Haus cannot write is the run's own failure, not Paperless's.
func TestMirrorFolderFailing(t *testing.T) {
	_, c := archive(t)
	_, err := Mirror(bg, c, filepath.Join(t.TempDir(), "gibtsnicht"))
	var src SourceError
	if err == nil || errors.As(err, &src) {
		t.Fatalf("%v", err)
	}
}

// Only <id>.md is a document's file: what qmd names otherwise is none.
func TestID(t *testing.T) {
	for name, want := range map[string]int{"7.md": 7, "1234567.md": 1234567} {
		if id, ok := ID(name); !ok || id != want {
			t.Fatalf("%s: %d %v", name, id, ok)
		}
	}
	for _, name := range []string{"0.md", "007.md", "7", "7.md.tmp", ".7.md", "a7.md", "7.MD", "../7.md", "sub/7.md", "-7.md", "12345678901.md", ""} {
		if _, ok := ID(name); ok {
			t.Fatalf("%q taken for a document", name)
		}
	}
	if Name(42) != "42.md" {
		t.Fatal(Name(42))
	}
}

// A document's text is capped, whole characters only, and stays valid UTF-8.
func TestMarkdownCapsTheText(t *testing.T) {
	old := maxText
	maxText = 10
	defer func() { maxText = old }()
	got := Markdown(paperless.Text{Doc: paperless.Doc{ID: 1, Title: "T"}, Content: "ääääää\xff"})
	if got != "# T\n\näääää\n" {
		t.Fatalf("%q", got)
	}
}

// Paperless keeps a document's last change to the microsecond: an edit
// within the same second as the run that mirrored it is still an edit,
// and the next run reads the text again.
func TestMirrorSeesAChangeWithinTheSameSecond(t *testing.T) {
	f, c := archive(t)
	dir := t.TempDir()
	f.Change(paperlessfake.Doc{ID: 8, Title: "Zeugnis", Content: "Mathematik: gut", Modified: "2026-07-25T08:00:00.100000Z"})
	if _, err := Mirror(bg, c, dir); err != nil {
		t.Fatal(err)
	}
	f.Change(paperlessfake.Doc{ID: 8, Title: "Zeugnis", Content: "Mathematik: sehr gut", Modified: "2026-07-25T08:00:00.700000Z"})
	rep, err := Mirror(bg, c, dir)
	if err != nil || rep != (Report{Written: 1, Unchanged: 2}) || !strings.Contains(read(t, filepath.Join(dir, "8.md")), "sehr gut") {
		t.Fatalf("%+v %v", rep, err)
	}
}

// A power cut can leave a file empty whose time the run had already set:
// it is no copy of the document, and the next run writes it again rather
// than taking it as up to date. What a run left halfway aside is cleared.
func TestMirrorRepairsWhatAPowerCutLeft(t *testing.T) {
	_, c := archive(t)
	dir := t.TempDir()
	if _, err := Mirror(bg, c, dir); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "7.md")
	st, _ := os.Stat(p)
	if err := os.Truncate(p, 0); err != nil {
		t.Fatal(err)
	}
	os.Chtimes(p, st.ModTime(), st.ModTime())
	os.WriteFile(filepath.Join(dir, ".8.md.tmp"), []byte("halb"), 0o644)
	rep, err := Mirror(bg, c, dir)
	if err != nil || rep != (Report{Written: 1, Unchanged: 2}) || !strings.Contains(read(t, p), "Vertragslaufzeit") {
		t.Fatalf("%+v %v", rep, err)
	}
	if got := strings.Join(files(t, dir), ","); got != "12.md,7.md,8.md" {
		t.Fatalf("files: %s", got)
	}
}

// Only a plain file is a mirrored document: a folder or a link that
// happens to be named <id>.md is not the mirror's, and does not stop it.
func TestMirrorLeavesWhatIsNoFile(t *testing.T) {
	_, c := archive(t)
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "13.md", "drin"), 0o755)
	outside := filepath.Join(t.TempDir(), "draussen.md")
	os.WriteFile(outside, []byte("nicht meins"), 0o644)
	os.Symlink(outside, filepath.Join(dir, "14.md"))
	rep, err := Mirror(bg, c, dir)
	if err != nil || rep != (Report{Written: 3}) {
		t.Fatalf("%+v %v", rep, err)
	}
	if read(t, outside) != "nicht meins" {
		t.Fatal("wrote through a link")
	}
	if _, err := os.Lstat(filepath.Join(dir, "14.md")); err != nil {
		t.Fatal("removed a link it does not own")
	}
}

// Each line the file puts above a document's text is one a hit leaves
// out; a line of the text is not.
func TestHeadLine(t *testing.T) {
	for _, l := range []string{"# Stromvertrag", "Datum: 14.03.2025", "Von: Stadtwerke", "Schlagworte: Strom, Vertrag", "  Von: eingerückt"} {
		if !HeadLine(l) {
			t.Fatalf("%q is a head line", l)
		}
	}
	for _, l := range []string{"Vertragslaufzeit bis 2027", "Vonovia schreibt", "Datum der Kündigung", "## Abschnitt", ""} {
		if HeadLine(l) {
			t.Fatalf("%q is text", l)
		}
	}
}
