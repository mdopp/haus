// Package notes keeps the household's notes as Markdown files in one
// directory of the file share (laib ADR 0017): every phone and laptop that
// syncs the share sees and edits them. Haus holds no index; a search reads
// the files, within caps. Every access goes through os.Root, so no name —
// "..", an absolute path, a symlink — reaches outside the directory, and
// hidden files and folders (Syncthing's, the temp files) are never notes.
package notes

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
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

// ErrHidden is a name with a part starting with a dot.
var ErrHidden = errors.New("Notizen: versteckte Dateien und Ordner sind keine Notizen")

// ErrNotFound is a note that is not there.
var ErrNotFound = errors.New("Notizen: keine solche Notiz")

// MaxTitle is the longest title, in characters.
const MaxTitle = 200

const (
	maxText  = 100 << 10 // a note, not a document
	maxRead  = 20 << 10  // what one answer carries
	maxBytes = 1 << 20   // files larger than this are not searched
)

// what one search reads at most; variables for the tests
var (
	maxFiles       = 5000
	maxScan  int64 = 64 << 20
)

// link puts the finished temp file in place; it fails when the name is
// taken. A variable for the tests.
var link = func(r *os.Root, oldname, newname string) error { return r.Link(oldname, newname) }

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
// appears complete or not at all: written aside, then hard-linked into
// place. A share without hard links gets an error, not a weaker write.
func (d *Dir) Create(title, text string) (string, error) {
	title = oneLine(title)
	if title == "" {
		return "", errors.New("Notizen: titel fehlt")
	}
	if utf8.RuneCountInString(title) > MaxTitle {
		return "", fmt.Errorf("Notizen: der titel ist zu lang (höchstens %d Zeichen)", MaxTitle)
	}
	if len(text) > maxText {
		return "", fmt.Errorf("Notizen: der Text ist zu lang (höchstens %d KB)", maxText>>10)
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
	defer root.Remove(tmp)
	_, werr := f.Write(content)
	if serr := f.Sync(); werr == nil {
		werr = serr
	}
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return "", werr
	}
	slug := Slug(title)
	for i := 1; i < 1000; i++ {
		name := slug + ".md"
		if i > 1 {
			name = fmt.Sprintf("%s-%d.md", slug, i)
		}
		err := link(root, tmp, name)
		if err == nil {
			return name, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", fmt.Errorf("Notizen: %s lässt sich nicht anlegen: %w", name, err)
		}
	}
	return "", fmt.Errorf("Notizen: zu viele Notizen namens %s", slug)
}

// Hit is one search result.
type Hit struct {
	Name  string   // the file, relative to the directory
	Title string   // the first "# " heading, else the file name
	Lines []string // the first lines of the body
}

// Result is a search's answer.
type Result struct {
	Hits    []Hit
	Total   int  // matches found, of which Hits are the first
	Partial bool // the caps stopped the search before every file was read
}

func isNote(name string) bool {
	ext := strings.ToLower(path.Ext(name))
	return ext == ".md" || ext == ".markdown" || ext == ".txt"
}

func cut(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func title(name string, body []byte) string {
	sc := bufio.NewScanner(strings.NewReader(string(body)))
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if l == "" {
			continue
		}
		if strings.HasPrefix(l, "# ") {
			return cut(strings.TrimSpace(l[2:]), MaxTitle)
		}
		break
	}
	return cut(strings.TrimSuffix(path.Base(name), path.Ext(name)), MaxTitle)
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
		out = append(out, cut(l, 120))
		if len(out) == n {
			break
		}
	}
	return out
}

// errStop ends a walk early when a cap is reached.
var errStop = errors.New("stop")

// walk calls fn for every note file, hidden directories (.stfolder,
// .stversions, .trash) left out, at most maxFiles files and maxScan bytes;
// partial says a cap stopped it. Each file is read through a limit, never
// sized first and read after.
func (d *Dir) walk(ctx context.Context, fn func(name string, body []byte)) (partial bool, err error) {
	root, err := os.OpenRoot(d.path)
	if err != nil {
		return false, err
	}
	defer root.Close()
	fsys := root.FS()
	files, scanned := 0, int64(0)
	err = fs.WalkDir(fsys, ".", func(name string, e fs.DirEntry, err error) error {
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
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
		if files >= maxFiles {
			partial = true
			return errStop
		}
		files++
		f, err := fsys.Open(name)
		if err != nil {
			return nil
		}
		budget := min(int64(maxBytes), maxScan-scanned)
		body, err := io.ReadAll(io.LimitReader(f, budget+1))
		f.Close()
		if err != nil {
			return nil
		}
		if int64(len(body)) > budget {
			if budget < maxBytes { // the search's budget, not this file's size
				partial = true
				return errStop
			}
			return nil // a file over maxBytes is no note
		}
		scanned += int64(len(body))
		fn(name, body)
		return nil
	})
	if errors.Is(err, errStop) {
		err = nil
	}
	return partial, err
}

// Search finds notes whose title or text contains q (any case), titles
// first, at most max of them in Hits.
func (d *Dir) Search(ctx context.Context, q string, max int) (Result, error) {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return Result{}, errors.New("Notizen: wonach suchen?")
	}
	var inTitle, inBody []Hit
	partial, err := d.walk(ctx, func(name string, body []byte) {
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
		return Result{}, err
	}
	all := append(inTitle, inBody...)
	res := Result{Total: len(all), Partial: partial, Hits: all}
	if len(all) > max {
		res.Hits = all[:max]
	}
	return res, nil
}

// Note is one note read whole (up to maxRead).
type Note struct {
	Name      string
	Title     string
	Text      string
	Truncated bool
}

// viaSymlink says whether a part of name is a symlink: a name os.Root
// refused for that has tried to leave the directory.
func viaSymlink(root *os.Root, name string) bool {
	parts := strings.Split(name, "/")
	for i := range parts {
		st, err := root.Lstat(strings.Join(parts[:i+1], "/"))
		if err != nil {
			return false
		}
		if st.Mode()&fs.ModeSymlink != 0 {
			return true
		}
	}
	return false
}

// checkName refuses a name that leaves the directory or is hidden.
func checkName(ref string) (string, error) {
	if strings.Contains(ref, `\`) || !filepath.IsLocal(ref) {
		return "", ErrOutside
	}
	clean := path.Clean(ref)
	for _, part := range strings.Split(clean, "/") {
		if strings.HasPrefix(part, ".") {
			return "", ErrHidden
		}
	}
	return clean, nil
}

// Read finds a note by file name (relative), by its title's slug, or by
// its title, and reads it. A name that would leave the directory, or a
// hidden one, is refused, not looked for.
func (d *Dir) Read(ctx context.Context, ref string) (Note, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return Note{}, errors.New("Notizen: welche Notiz?")
	}
	if err := ctx.Err(); err != nil {
		return Note{}, err
	}
	if strings.ContainsAny(ref, `/\`) || isNote(ref) || strings.HasPrefix(ref, ".") {
		clean, err := checkName(ref)
		if err != nil {
			return Note{}, err
		}
		ref = clean
	}
	root, err := os.OpenRoot(d.path)
	if err != nil {
		return Note{}, err
	}
	defer root.Close()
	try := func(name string) (Note, bool, error) {
		if !isNote(name) {
			return Note{}, false, nil
		}
		f, err := root.Open(name) // follows a symlink only while it stays inside
		if errors.Is(err, fs.ErrNotExist) {
			return Note{}, false, nil
		}
		if err != nil {
			if viaSymlink(root, name) {
				return Note{}, false, ErrOutside
			}
			return Note{}, false, err
		}
		defer f.Close()
		st, err := f.Stat() // the file opened, not the name looked at earlier
		if err != nil || !st.Mode().IsRegular() {
			return Note{}, false, nil
		}
		buf, _ := io.ReadAll(io.LimitReader(f, maxRead+1))
		n := Note{Name: name, Text: string(buf[:min(len(buf), maxRead)]), Truncated: len(buf) > maxRead, Title: title(name, buf)}
		return n, true, nil
	}
	for _, name := range []string{ref, Slug(ref) + ".md"} {
		if n, ok, err := try(name); err != nil || ok {
			return n, err
		}
	}
	var found string
	want := strings.ToLower(oneLine(ref))
	if _, err := d.walk(ctx, func(name string, body []byte) {
		if found == "" && strings.ToLower(title(name, body)) == want {
			found = name
		}
	}); err != nil {
		return Note{}, err
	}
	if found != "" {
		if n, ok, err := try(found); err != nil || ok {
			return n, err
		}
	}
	return Note{}, ErrNotFound
}

// Entry is a note in a listing: its file, its title, when it last changed.
type Entry struct {
	Name    string
	Title   string
	Changed time.Time
}

// Recent lists the n most recently changed notes, newest first. It looks at
// at most maxFiles files, the hidden ones left out, and reads only the n
// it returns, each just far enough for its title.
func (d *Dir) Recent(ctx context.Context, n int) ([]Entry, error) {
	if n <= 0 {
		return []Entry{}, nil
	}
	root, err := os.OpenRoot(d.path)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	fsys := root.FS()
	var all []Entry
	err = fs.WalkDir(fsys, ".", func(name string, e fs.DirEntry, err error) error {
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		if err != nil {
			return nil
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
		if len(all) >= maxFiles {
			return errStop
		}
		if info, err := e.Info(); err == nil {
			all = append(all, Entry{Name: name, Changed: info.ModTime()})
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStop) {
		return nil, err
	}
	sort.SliceStable(all, func(i, j int) bool {
		if !all[i].Changed.Equal(all[j].Changed) {
			return all[i].Changed.After(all[j].Changed)
		}
		return all[i].Name < all[j].Name
	})
	out := make([]Entry, 0, n)
	for _, e := range all {
		if len(out) == n {
			break
		}
		f, err := fsys.Open(e.Name)
		if err != nil {
			continue
		}
		head, _ := io.ReadAll(io.LimitReader(f, 4<<10))
		f.Close()
		e.Title = title(e.Name, head)
		out = append(out, e)
	}
	return out, nil
}
