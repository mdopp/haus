// Package notes keeps the household's notes as Markdown files in one
// directory of the file share (laib ADR 0017): every phone and laptop that
// syncs the share sees and edits them. Haus holds no index; a search reads
// the files. Every access goes through os.Root, so no name — "..", an
// absolute path, a symlink — reaches outside the directory.
package notes

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode"
)

// Dir is the notes directory.
type Dir struct {
	path string
}

// Open checks that path is a directory Haus can use.
func Open(p string) (*Dir, error) {
	st, err := os.Stat(p)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("%s: kein Verzeichnis", p)
	}
	return &Dir{path: p}, nil
}

// ErrOutside is a name that would leave the directory.
var ErrOutside = errors.New("Notizen: dieser Pfad liegt außerhalb der Notizen")

// ErrNotFound is a note that is not there.
var ErrNotFound = errors.New("Notizen: keine solche Notiz")

const (
	maxText  = 100 << 10 // a note, not a document
	maxRead  = 20 << 10  // what one answer carries
	maxBytes = 1 << 20   // files larger than this are not searched
)

// Slug is a file name stem from a title: lower case, umlauts spelled out,
// everything but letters and digits a dash. It never holds a separator or
// a leading dot.
func Slug(title string) string {
	r := strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "Ä", "ae", "Ö", "oe", "Ü", "ue", "ß", "ss")
	s := strings.ToLower(r.Replace(title))
	var b strings.Builder
	dash := false
	n := 0
	for _, c := range s {
		if n >= 60 {
			break
		}
		if unicode.IsLetter(c) || unicode.IsDigit(c) {
			b.WriteRune(c)
			dash = false
			n++
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
			n++
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "notiz"
	}
	return out
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// Create writes "# <title>\n\n<text>" as <slug>.md, or <slug>-2.md, -3, …
// when the name is taken: an existing note is never overwritten. The file
// appears complete or not at all (written aside, then hard-linked).
func (d *Dir) Create(title, text string) (string, error) {
	title = oneLine(title)
	if title == "" {
		return "", errors.New("Notizen: titel fehlt")
	}
	if len(text) > maxText {
		return "", fmt.Errorf("Notizen: der Text ist länger als %d KB", maxText>>10)
	}
	root, err := os.OpenRoot(d.path)
	if err != nil {
		return "", err
	}
	defer root.Close()
	content := []byte("# " + title + "\n\n" + strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n")) + "\n")
	rnd := make([]byte, 8)
	rand.Read(rnd)
	tmp := ".haus-" + hex.EncodeToString(rnd) + ".tmp"
	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", err
	}
	_, werr := f.Write(content)
	if serr := f.Sync(); werr == nil {
		werr = serr
	}
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	defer root.Remove(tmp)
	if werr != nil {
		return "", werr
	}
	slug := Slug(title)
	for i := 1; i < 1000; i++ {
		name := slug + ".md"
		if i > 1 {
			name = fmt.Sprintf("%s-%d.md", slug, i)
		}
		err := root.Link(tmp, name)
		if err == nil {
			return name, nil
		}
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		// a share without hard links: exclusive create still never overwrites
		g, gerr := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(gerr, fs.ErrExist) {
			continue
		}
		if gerr != nil {
			return "", gerr
		}
		_, werr := g.Write(content)
		if cerr := g.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			root.Remove(name)
			return "", werr
		}
		return name, nil
	}
	return "", fmt.Errorf("Notizen: zu viele Notizen namens %s", slug)
}

// Hit is one search result.
type Hit struct {
	Name  string   // the file, relative to the directory
	Title string   // the first "# " heading, else the file name
	Lines []string // the first lines of the body
}

func isNote(name string) bool {
	ext := strings.ToLower(path.Ext(name))
	return ext == ".md" || ext == ".markdown" || ext == ".txt"
}

func title(name string, body []byte) string {
	sc := bufio.NewScanner(strings.NewReader(string(body)))
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if l == "" {
			continue
		}
		if strings.HasPrefix(l, "# ") {
			return strings.TrimSpace(l[2:])
		}
		break
	}
	return strings.TrimSuffix(path.Base(name), path.Ext(name))
}

func firstLines(body []byte, n int) []string {
	var out []string
	heading := true
	for _, l := range strings.Split(string(body), "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if heading && strings.HasPrefix(l, "# ") {
			heading = false
			continue
		}
		heading = false
		if r := []rune(l); len(r) > 120 {
			l = string(r[:120]) + "…"
		}
		out = append(out, l)
		if len(out) == n {
			break
		}
	}
	return out
}

// walk calls fn for every note file, hidden directories (.stfolder,
// .stversions, .trash) left out.
func (d *Dir) walk(fn func(name string, body []byte)) error {
	root, err := os.OpenRoot(d.path)
	if err != nil {
		return err
	}
	defer root.Close()
	fsys := root.FS()
	return fs.WalkDir(fsys, ".", func(name string, e fs.DirEntry, err error) error {
		if err != nil {
			return nil // one unreadable folder does not end the search
		}
		if e.IsDir() {
			if name != "." && strings.HasPrefix(e.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}
		if !e.Type().IsRegular() || strings.HasPrefix(e.Name(), ".") || !isNote(name) {
			return nil
		}
		if info, err := e.Info(); err != nil || info.Size() > maxBytes {
			return nil
		}
		body, err := fs.ReadFile(fsys, name)
		if err == nil {
			fn(name, body)
		}
		return nil
	})
}

// Search finds notes whose title or text contains q (any case), titles
// first, at most max.
func (d *Dir) Search(q string, max int) ([]Hit, int, error) {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return nil, 0, errors.New("Notizen: wonach suchen?")
	}
	var inTitle, inBody []Hit
	err := d.walk(func(name string, body []byte) {
		t := title(name, body)
		h := Hit{Name: name, Title: t, Lines: firstLines(body, 3)}
		switch {
		case strings.Contains(strings.ToLower(t), q) || strings.Contains(strings.ToLower(name), q):
			inTitle = append(inTitle, h)
		case strings.Contains(strings.ToLower(string(body)), q):
			inBody = append(inBody, h)
		}
	})
	if err != nil {
		return nil, 0, err
	}
	all := append(inTitle, inBody...)
	total := len(all)
	if len(all) > max {
		all = all[:max]
	}
	return all, total, nil
}

// Note is one note read whole (up to maxRead).
type Note struct {
	Name      string
	Title     string
	Text      string
	Truncated bool
}

// Read finds a note by file name (relative), by its title's slug, or by
// its title, and reads it. A name that would leave the directory is
// refused, not looked for.
func (d *Dir) Read(ref string) (Note, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return Note{}, errors.New("Notizen: welche Notiz?")
	}
	root, err := os.OpenRoot(d.path)
	if err != nil {
		return Note{}, err
	}
	defer root.Close()
	if strings.ContainsAny(ref, `/\`) || isNote(ref) || strings.HasPrefix(ref, ".") {
		if !filepath.IsLocal(ref) || strings.Contains(ref, `\`) {
			return Note{}, ErrOutside
		}
	}
	try := func(name string) (Note, bool, error) {
		if !isNote(name) {
			return Note{}, false, nil
		}
		st, err := root.Stat(name) // follows a symlink only while it stays inside
		if errors.Is(err, fs.ErrNotExist) {
			return Note{}, false, nil
		}
		if err != nil {
			if strings.Contains(err.Error(), "escapes") {
				return Note{}, false, ErrOutside
			}
			return Note{}, false, err
		}
		if !st.Mode().IsRegular() {
			return Note{}, false, nil
		}
		f, err := root.Open(name)
		if err != nil {
			return Note{}, false, err
		}
		defer f.Close()
		buf := make([]byte, maxRead+1)
		n, _ := io.ReadFull(f, buf)
		n2 := Note{Name: name, Text: string(buf[:min(n, maxRead)]), Truncated: n > maxRead}
		n2.Title = title(name, buf[:n])
		return n2, true, nil
	}
	for _, name := range []string{ref, Slug(ref) + ".md"} {
		if n, ok, err := try(name); err != nil || ok {
			return n, err
		}
	}
	var found string
	want := strings.ToLower(oneLine(ref))
	d.walk(func(name string, body []byte) {
		if found == "" && strings.ToLower(title(name, body)) == want {
			found = name
		}
	})
	if found != "" {
		if n, ok, err := try(found); err != nil || ok {
			return n, err
		}
	}
	return Note{}, ErrNotFound
}
