// Package dokumente puts the family's Paperless documents into the memory
// library (haus#13): Paperless has read every document already (its OCR),
// and this mirrors that text into a folder qmd indexes as its own
// collection, one Markdown file per document, <id>.md — the title, then
// the date, the sender and the tags, then the text. Paperless stays the
// one place a document lives. The folder is a copy that each run puts
// right: a document changed in Paperless is written again, one deleted
// there (or no longer shown to Haus's user) is deleted here. Haus reads
// nothing back from it: a hit is checked against Paperless itself.
package dokumente

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mdopp/haus/internal/paperless"
)

// Report is what one run did.
type Report struct {
	Written   int // new or changed in Paperless since the last run
	Unchanged int
	Removed   int // gone from Paperless
}

// SourceError is Paperless failing — not answering, refusing, an answer
// that is none. The folder is left as it was: what a run cannot list, it
// does not delete.
type SourceError struct{ Err error }

func (e SourceError) Error() string { return e.Err.Error() }
func (e SourceError) Unwrap() error { return e.Err }

// batch is how many documents one request reads with their text.
var batch = 10

// maxText caps the text a file carries of one document, in bytes.
var maxText = 2 << 20

// docName is a mirrored document's file: its Paperless id.
var docName = regexp.MustCompile(`^[1-9][0-9]{0,9}\.md$`)

// Name is the file of document id in the folder.
func Name(id int) string { return strconv.Itoa(id) + ".md" }

// ID is the document a file of the folder stands for; false when name is
// none of them.
func ID(name string) (int, bool) {
	if !docName.MatchString(name) {
		return 0, false
	}
	id, err := strconv.Atoi(strings.TrimSuffix(name, ".md"))
	return id, err == nil
}

// Mirror makes dir hold what Paperless holds: every document this user
// sees as <id>.md, nothing else of that form. A file whose time is the
// document's last change in Paperless is up to date and not read again.
func Mirror(ctx context.Context, c *paperless.Client, dir string) (Report, error) {
	var rep Report
	root, err := os.OpenRoot(dir)
	if err != nil {
		return rep, err
	}
	defer root.Close()
	stamps, err := c.Stamps(ctx)
	if err != nil {
		return rep, SourceError{err}
	}
	have := map[int]time.Time{}
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return rep, err
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") && strings.HasSuffix(e.Name(), ".tmp") {
			root.Remove(e.Name()) // a run stopped halfway
			continue
		}
		id, ok := ID(e.Name())
		if !ok || !e.Type().IsRegular() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return rep, err
		}
		have[id] = info.ModTime()
		if info.Size() == 0 {
			// never a file Markdown wrote: a power cut before its text
			// reached the disk. Its time says nothing; it is written again.
			have[id] = time.Time{}
		}
	}
	keep := map[int]bool{}
	var todo []int
	for _, s := range stamps {
		keep[s.ID] = true
		// to the microsecond, as Paperless keeps it: an edit within the
		// second a run mirrored the document is an edit
		if at, ok := have[s.ID]; ok && !s.Modified.IsZero() && at.UnixMicro() == s.Modified.UnixMicro() {
			rep.Unchanged++
			continue
		}
		todo = append(todo, s.ID)
	}
	for len(todo) > 0 {
		n := min(batch, len(todo))
		texts, err := c.Texts(ctx, todo[:n])
		if err != nil {
			return rep, SourceError{err}
		}
		for _, t := range texts {
			if err := write(root, t); err != nil {
				return rep, err
			}
			rep.Written++
		}
		todo = todo[n:]
	}
	for id := range have {
		if !keep[id] {
			if err := root.Remove(Name(id)); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return rep, err
			}
			rep.Removed++
		}
	}
	return rep, nil
}

// write puts one document's file in place whole: written aside and
// synced, its time set to the document's last change, then renamed over
// the old one.
func write(root *os.Root, t paperless.Text) error {
	name := Name(t.ID)
	tmp := "." + name + ".tmp"
	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer root.Remove(tmp)
	_, werr := f.WriteString(Markdown(t))
	// on the disk before the rename: a power cut must not leave the name
	// with the document's time and none of its text
	if serr := f.Sync(); werr == nil {
		werr = serr
	}
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return werr
	}
	if !t.Modified.IsZero() {
		if err := root.Chtimes(tmp, t.Modified, t.Modified); err != nil {
			return err
		}
	}
	return root.Rename(tmp, name)
}

// oneLine is s on one line, its runs of space one space.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// Markdown is a document's file: "# Titel", then Datum, Von and
// Schlagworte (each only when Paperless names it), then its text.
func Markdown(t paperless.Text) string {
	title := oneLine(t.Title)
	if title == "" {
		title = fmt.Sprintf("Dokument %d", t.ID)
	}
	var b strings.Builder
	b.WriteString("# " + title + "\n\n")
	head := false
	if !t.Created.IsZero() {
		b.WriteString("Datum: " + t.Created.Format("02.01.2006") + "\n")
		head = true
	}
	if c := oneLine(t.Correspondent); c != "" {
		b.WriteString("Von: " + c + "\n")
		head = true
	}
	var tags []string
	for _, tg := range t.Tags {
		if tg = oneLine(tg); tg != "" {
			tags = append(tags, tg)
		}
	}
	if len(tags) > 0 {
		b.WriteString("Schlagworte: " + strings.Join(tags, ", ") + "\n")
		head = true
	}
	if head {
		b.WriteString("\n")
	}
	text := strings.ToValidUTF8(strings.ReplaceAll(t.Content, "\r\n", "\n"), "�")
	if len(text) > maxText {
		text = text[:maxText]
		for !utf8.ValidString(text) {
			text = text[:len(text)-1]
		}
	}
	b.WriteString(strings.TrimSpace(text) + "\n")
	return b.String()
}

// HeadLine says whether l is one of the lines Markdown puts above a
// document's text: a hit shows the text, the page names the rest itself.
func HeadLine(l string) bool {
	l = strings.TrimSpace(l)
	for _, p := range []string{"# ", "Datum: ", "Von: ", "Schlagworte: "} {
		if strings.HasPrefix(l, p) {
			return true
		}
	}
	return false
}
