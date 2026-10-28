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
	return d.CreateWith(title, text, Kopf{})
}

// CreateWith is Create with a head (haus#116): kind, tags, source.
func (d *Dir) CreateWith(title, text string, k Kopf) (string, error) {
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
	k.Tags = Tags(k.Tags)
	if !validArt(k.Art) || k.Art == "notiz" {
		k.Art = ""
	}
	content := []byte(k.With("# " + title + "\n\n" + strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n")) + "\n"))
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

// PutNew writes content as name, a path inside the directory (its
// folders made as needed), unless name is taken: then it writes nothing
// and says so (false). An import that runs twice (haus#13) finds its
// files there. The file appears whole or not at all, like Create's, and
// never replaces one; a name that leaves the directory or is hidden is
// refused.
func (d *Dir) PutNew(name string, content []byte) (bool, error) {
	clean, err := checkName(name)
	if err != nil {
		return false, err
	}
	root, err := os.OpenRoot(d.path)
	if err != nil {
		return false, err
	}
	defer root.Close()
	dir := path.Dir(clean)
	if dir != "." {
		if err := root.MkdirAll(dir, 0o755); err != nil {
			return false, err
		}
	}
	rnd := make([]byte, 8)
	rand.Read(rnd)
	tmp := path.Join(dir, ".haus-"+hex.EncodeToString(rnd)+".tmp")
	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return false, err
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
		return false, werr
	}
	if err := link(root, tmp, clean); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return false, nil
		}
		return false, fmt.Errorf("Notizen: %s lässt sich nicht anlegen: %w", clean, err)
	}
	return true, nil
}

// Delete removes the note name when it is still as it was read (was, its
// Changed): else ErrChanged and nothing goes. Its content, for the
// Papierkorb (haus#183). A symlink or a folder is not deleted through.
func (d *Dir) Delete(name string, was time.Time) ([]byte, error) {
	clean, err := checkName(name)
	if err != nil {
		return nil, err
	}
	if !isNote(clean) {
		return nil, ErrNotFound
	}
	root, err := os.OpenRoot(d.path)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	st, err := root.Lstat(clean)
	if errors.Is(err, fs.ErrNotExist) || (err == nil && !st.Mode().IsRegular()) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if !st.ModTime().Equal(was) {
		return nil, ErrChanged
	}
	b, err := fs.ReadFile(root.FS(), clean)
	if err != nil {
		return nil, err
	}
	return b, root.Remove(clean)
}

// ErrChanged is a note changed by someone else since it was read.
var ErrChanged = errors.New("Notizen: die Notiz hat sich inzwischen geändert")

// MaxText is how long a note may be, for a page's field.
const MaxText = maxText

// Update writes text as the note name, a file that is there, when it is
// still as it was read (was, its Changed): else ErrChanged, and nothing is
// written — one person's edit never overwrites another's. The new text
// appears whole, through a temp file renamed over the old one.
func (d *Dir) Update(name, text string, was time.Time) error {
	clean, err := checkName(name)
	if err != nil {
		return err
	}
	if !isNote(clean) {
		return ErrNotFound
	}
	if len(text) > maxText {
		return fmt.Errorf("Notizen: der Text ist zu lang (höchstens %d KB)", maxText>>10)
	}
	root, err := os.OpenRoot(d.path)
	if err != nil {
		return err
	}
	defer root.Close()
	st, err := root.Lstat(clean)
	if errors.Is(err, fs.ErrNotExist) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return ErrNotFound // a symlink or a folder is not edited through
	}
	if !st.ModTime().Equal(was) {
		return ErrChanged
	}
	content := strings.ReplaceAll(text, "\r\n", "\n")
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	rnd := make([]byte, 8)
	rand.Read(rnd)
	tmp := path.Join(path.Dir(clean), ".haus-"+hex.EncodeToString(rnd)+".tmp")
	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, st.Mode().Perm())
	if err != nil {
		return err
	}
	defer root.Remove(tmp)
	_, werr := f.WriteString(content)
	if serr := f.Sync(); werr == nil {
		werr = serr
	}
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return werr
	}
	return root.Rename(tmp, clean)
}

// Hit is one search result.
type Hit struct {
	Name  string   // the file, relative to the directory
	Title string   // the first "# " heading, else the file name
	Lines []string // the first lines of the body
	Kopf  Kopf     // its head (haus#116)
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
	k, rest := SplitKopf(string(body))
	sc := bufio.NewScanner(strings.NewReader(rest))
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
	if k.Titel != "" {
		return cut(k.Titel, MaxTitle)
	}
	return cut(strings.TrimSuffix(path.Base(name), path.Ext(name)), MaxTitle)
}

// tableSeparator is a Markdown table's separator row: |---|:---:|
func tableSeparator(l string) bool {
	return strings.HasPrefix(l, "|") && strings.Trim(l, "|-: \t") == "" && strings.Contains(l, "-")
}

// cells are a table row's cells, in their columns, empty ones kept.
func cells(row string) []string {
	row = strings.TrimSuffix(strings.TrimPrefix(row, "|"), "|")
	out := strings.Split(row, "|")
	for i, c := range out {
		out[i] = strings.TrimSpace(c)
	}
	return out
}

// tables is what body's tables say about their rows: the rows that head
// a table (each right before a separator row), and each other row's
// column headers.
type tables struct {
	header  map[string]bool
	columns map[string][]string
}

func readTables(body string) tables {
	t := tables{header: map[string]bool{}, columns: map[string][]string{}}
	lines := strings.Split(body, "\n")
	var cols []string
	for i, l := range lines {
		l = strings.TrimSpace(l)
		switch {
		case !strings.HasPrefix(l, "|"):
			cols = nil
		case i+1 < len(lines) && tableSeparator(strings.TrimSpace(lines[i+1])):
			t.header[l] = true
			cols = cells(l)
		case tableSeparator(l):
		case cols != nil:
			t.columns[l] = cols
		}
	}
	return t
}

var listMark = []string{"- [ ] ", "- [x] ", "- [X] ", "* [ ] ", "* [x] ", "- ", "* ", "+ ", "> "}

// plain is one line of a note as text (haus#13), or "" when it is only
// markup: the note's "# " title (the hit's title says it), a table's
// header or separator row. A table row is each value after its column's
// header ("Strom 41.230"; a column without one: the value alone), joined
// by " · "; a
// list dash, a checkbox, a quote mark, a heading's hashes and emphasis
// go. Cut at 120 characters.
func plain(l string, t tables) string {
	return cut(plainWhole(l, t), 120)
}

// plainWhole is plain without the cut.
func plainWhole(l string, t tables) string {
	l = strings.TrimSpace(l)
	if l == "" || strings.HasPrefix(l, "# ") || l == "#" {
		return ""
	}
	if strings.HasPrefix(l, "|") {
		if tableSeparator(l) || t.header[l] {
			return ""
		}
		cols := t.columns[l]
		var parts []string
		for i, c := range cells(l) {
			if c == "" {
				continue
			}
			if i < len(cols) && cols[i] != "" {
				c = cols[i] + " " + c
			}
			parts = append(parts, c)
		}
		l = strings.Join(parts, " · ")
	}
	if strings.HasPrefix(l, "#") {
		l = strings.TrimLeft(l, "#")
	}
	for again := true; again; {
		again = false
		l = strings.TrimSpace(l)
		for _, m := range listMark {
			if rest, ok := strings.CutPrefix(l, m); ok {
				l, again = rest, true
				break
			}
		}
	}
	l = strings.NewReplacer("**", "", "__", "", "`", "").Replace(l)
	return strings.TrimSpace(l)
}

// Passage is what a hit shows of a note (haus#13): raw, the lines a
// search picked (qmd's best passage, or none), made plain, at most n; when
// none of them is more than markup — a passage that is only a table's
// header — the note's first lines with real content. body is the note's
// text, which says which table rows are headers and what heads each
// column.
func Passage(raw []string, body string, n int) []string {
	t := readTables(body)
	var out []string
	add := func(lines []string) {
		for _, l := range lines {
			if len(out) == n {
				return
			}
			if p := plain(l, t); p != "" {
				out = append(out, p)
			}
		}
	}
	add(raw)
	if len(out) == 0 {
		add(strings.Split(body, "\n"))
	}
	return out
}

// Excerpt is a longer passage of a note, for reading rather than showing
// (the tidy-up's, haus#13): its lines made plain and whole, joined by
// spaces, at most n characters. All of the note when it fits; else from a
// little before line from (at most a quarter of n), where a search's best
// passage starts, on — a line before it often holds the answer.
func Excerpt(body string, from, n int) string {
	t := readTables(body)
	type line struct {
		no   int
		text string
	}
	var ls []line
	whole := 0
	for i, l := range strings.Split(body, "\n") {
		if p := plainWhole(l, t); p != "" {
			ls = append(ls, line{i + 1, p})
			whole += len([]rune(p)) + 1
		}
	}
	at := 0
	if whole-1 > n {
		for at < len(ls) && ls[at].no < from {
			at++
		}
		if at == len(ls) {
			at = 0
		}
		for back := 0; at > 0; at-- {
			back += len([]rune(ls[at-1].text)) + 1
			if back > n/4 {
				break
			}
		}
	}
	parts := make([]string, 0, len(ls)-at)
	for _, l := range ls[at:] {
		parts = append(parts, l.text)
	}
	return cut(strings.Join(parts, " "), n)
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

// match is a note the word search found: how many of the words, and
// whether one was in its title or file name.
type match struct {
	hit     Hit
	words   int
	inTitle bool
}

// common are words too common to find a note by (haus#13): a sentence
// asked of the search is mostly these. Words under three letters are
// left out as well.
var common = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`der die das den dem des ein eine einer eines einem einen und oder aber
		wann wo wie was wer wen wem warum wieso weshalb welche welcher welches woher wohin
		ist sind war waren bin bist sein hat haben hatte wird werden wurde kommt kommen muss müssen
		soll sollen kann können darf dürfen will wollen mag gibt geht
		mit von zum zur aus bei nach vor über unter für ohne gegen bis seit durch auf
		ich du er sie es wir ihr mein meine unser unsere unseren dein deine sich man
		nicht noch schon auch nur mal denn doch nein dann wenn als wie bitte heute morgen`) {
		common[w] = true
	}
}

// Words are q's meaningful words, as written, each once: letters and
// digits, at least three, not a common word.
func Words(q string) []string {
	var out []string
	seen := map[string]bool{}
	for _, w := range strings.FieldsFunc(q, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		lw := strings.ToLower(w)
		if utf8.RuneCountInString(w) < 3 || common[lw] || seen[lw] {
			continue
		}
		seen[lw] = true
		out = append(out, w)
	}
	return out
}

func lower(ws []string) []string {
	out := make([]string, len(ws))
	for i, w := range ws {
		out[i] = strings.ToLower(w)
	}
	return out
}

// Search finds notes that hold any of q's meaningful words in their
// title, file name or text (any case; haus#13): the notes with more of
// them first, a title match before a text match, at most max in Hits. A
// q of only common words is looked for as written.
func (d *Dir) Search(ctx context.Context, q string, max int) (Result, error) {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return Result{}, errors.New("Notizen: wonach suchen?")
	}
	words := lower(Words(q))
	if len(words) == 0 {
		words = []string{q} // only common words: looked for as written
	}
	var found []match
	partial, err := d.walk(ctx, func(name string, body []byte) {
		t := title(name, body)
		head := strings.ToLower(t) + "\n" + strings.ToLower(name)
		text := strings.ToLower(string(body))
		m := match{}
		for _, w := range words {
			switch {
			case strings.Contains(head, w):
				m.words++
				m.inTitle = true
			case strings.Contains(text, w):
				m.words++
			}
		}
		if m.words > 0 {
			k, rest := SplitKopf(string(body))
			m.hit = Hit{Name: name, Title: t, Lines: Passage(nil, rest, 3), Kopf: k}
			found = append(found, m)
		}
	})
	if err != nil {
		return Result{}, err
	}
	// more of the words first, then a title match before a text match
	sort.SliceStable(found, func(i, j int) bool {
		if found[i].words != found[j].words {
			return found[i].words > found[j].words
		}
		return found[i].inTitle && !found[j].inTitle
	})
	all := make([]Hit, len(found))
	for i, m := range found {
		all[i] = m.hit
	}
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
	Changed   time.Time
	Kopf      Kopf // its head (haus#116): kind, tags, source
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
		n := Note{Name: name, Text: string(buf[:min(len(buf), maxRead)]), Truncated: len(buf) > maxRead, Title: title(name, buf), Changed: st.ModTime()}
		n.Kopf, _ = SplitKopf(n.Text)
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
	Kopf    Kopf // its head (haus#116)
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
		e.Kopf, _ = SplitKopf(string(head))
		out = append(out, e)
	}
	return out, nil
}

// Lookup reads the note with exactly this file name (relative, as the memory
// library names it, haus#13): no slug, no title, no search. A name that
// leaves the directory (a step out, a link out) is ErrOutside, a hidden
// one ErrHidden, one that is not a note file ErrNotFound; a directory that
// cannot be read is its own error.
func (d *Dir) Lookup(ctx context.Context, name string) (Note, error) {
	if err := ctx.Err(); err != nil {
		return Note{}, err
	}
	if name == "" {
		return Note{}, ErrNotFound
	}
	clean, err := checkName(name)
	if err != nil {
		return Note{}, err
	}
	root, err := os.OpenRoot(d.path)
	if err != nil {
		return Note{}, err
	}
	defer root.Close()
	if !isNote(clean) {
		return Note{}, ErrNotFound
	}
	f, err := root.Open(clean) // follows a symlink only while it stays inside
	if errors.Is(err, fs.ErrNotExist) {
		return Note{}, ErrNotFound
	}
	if err != nil {
		if viaSymlink(root, clean) {
			return Note{}, ErrOutside
		}
		return Note{}, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return Note{}, err
	}
	if !st.Mode().IsRegular() || st.Size() > maxBytes {
		return Note{}, ErrNotFound
	}
	buf, _ := io.ReadAll(io.LimitReader(f, maxRead+1))
	n := Note{Name: clean, Title: title(clean, buf), Text: string(buf[:min(len(buf), maxRead)]), Truncated: len(buf) > maxRead, Changed: st.ModTime()}
	n.Kopf, _ = SplitKopf(n.Text)
	return n, nil
}
