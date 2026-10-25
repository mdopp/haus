package notes

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"sort"
	"strings"
	"unicode"
)

// Kopf is a note's head (haus#116): its kind, its tags and where it came
// from, the front matter between two "---" lines at the top of the file.
// Haus reads and writes a small part of YAML by hand; any other line in a
// head (an older note's id, resident, …) is kept as it was.
type Kopf struct {
	Art    string   // one of Arten; "" is a plain note
	Tags   []string // normalised by Tag, in the order given
	Quelle string   // the address a note was made from
	Titel  string   // title:, which some older notes carry instead of "# "
	other  []string // every other line of the head, as it was
}

// Arten are the kinds a note can be: what Haus can do with it.
var Arten = []struct{ Key, Name string }{
	{"notiz", "Notiz"}, {"rezept", "Rezept"}, {"album", "Album"}, {"reise", "Reise"}, {"liste", "Liste"},
}

// Vorschlaege are the tags offered on a new note.
var Vorschlaege = []string{"kochen", "musik", "reisen", "haushalt", "ideen"}

const (
	maxTags   = 12
	maxTagLen = 32
)

// Tag is a tag as Haus keeps it: lower case, a word or words joined by
// "-", letters and digits only; "" when nothing is left.
func Tag(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r)
		default:
			dash = true
		}
		if b.Len() >= maxTagLen {
			break
		}
	}
	return b.String()
}

// Tags normalises a list of tags: each by Tag, no empty, no repeat, at
// most maxTags.
func Tags(in []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, t := range in {
		if t = Tag(t); t != "" && !seen[t] && len(out) < maxTags {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

// validArt is one of Arten.
func validArt(a string) bool {
	for _, x := range Arten {
		if x.Key == a {
			return true
		}
	}
	return false
}

// SplitKopf parts a note's text into its head and the rest; a text
// without a head has an empty one and is the rest whole. A head that is
// never closed is no head.
func SplitKopf(text string) (Kopf, string) {
	t := strings.TrimPrefix(text, "\ufeff")
	if !strings.HasPrefix(t, "---\n") && !strings.HasPrefix(t, "---\r\n") {
		return Kopf{}, text
	}
	lines := strings.Split(strings.ReplaceAll(t, "\r\n", "\n"), "\n")
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return Kopf{}, text
	}
	var k Kopf
	for i := 1; i < end; i++ {
		l := lines[i]
		key, val, ok := strings.Cut(l, ":")
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if !ok || strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t") || strings.HasPrefix(l, "-") {
			k.other = append(k.other, l)
			continue
		}
		switch strings.ToLower(key) {
		case "art":
			if a := strings.ToLower(unquote(val)); validArt(a) {
				k.Art = a
			} else {
				k.other = append(k.other, l)
			}
		case "tags":
			var raw []string
			if val == "" { // a block list: "  - kochen"
				for i+1 < end && strings.HasPrefix(strings.TrimSpace(lines[i+1]), "- ") {
					i++
					raw = append(raw, unquote(strings.TrimPrefix(strings.TrimSpace(lines[i]), "- ")))
				}
			} else {
				for _, t := range strings.Split(strings.Trim(val, "[]"), ",") {
					raw = append(raw, unquote(t))
				}
			}
			k.Tags = Tags(raw)
		case "quelle":
			k.Quelle = unquote(val)
		case "title":
			k.Titel = unquote(val)
		default:
			k.other = append(k.other, l)
		}
	}
	return k, strings.TrimLeft(strings.Join(lines[end+1:], "\n"), "\n")
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '"' && s[len(s)-1] == '"' || s[0] == '\'' && s[len(s)-1] == '\'') {
		s = s[1 : len(s)-1]
	}
	return strings.TrimSpace(s)
}

// Empty says the head has nothing to write.
func (k Kopf) Empty() bool {
	return k.Art == "" && len(k.Tags) == 0 && k.Quelle == "" && k.Titel == "" && len(k.other) == 0
}

// Text is the head as the file carries it, "" when it is empty: Haus's
// fields first, the other lines after them as they were.
func (k Kopf) Text() string {
	if k.Empty() {
		return ""
	}
	var b strings.Builder
	b.WriteString("---\n")
	if k.Art != "" {
		b.WriteString("art: " + k.Art + "\n")
	}
	if len(k.Tags) > 0 {
		b.WriteString("tags: [" + strings.Join(k.Tags, ", ") + "]\n")
	}
	if k.Quelle != "" {
		b.WriteString("quelle: " + oneLine(k.Quelle) + "\n")
	}
	if k.Titel != "" {
		b.WriteString("title: " + oneLine(k.Titel) + "\n")
	}
	for _, l := range k.other {
		b.WriteString(l + "\n")
	}
	b.WriteString("---\n")
	return b.String()
}

// With is a note's text with this head in front of the rest.
func (k Kopf) With(rest string) string {
	h := k.Text()
	if h == "" {
		return rest
	}
	return h + "\n" + strings.TrimLeft(rest, "\n")
}

// Has says the head carries every one of tags.
func (k Kopf) Has(tags []string) bool {
	for _, t := range tags {
		found := false
		for _, x := range k.Tags {
			found = found || x == t
		}
		if !found {
			return false
		}
	}
	return true
}

// ArtName is the kind in the family's words.
func ArtName(a string) string {
	for _, x := range Arten {
		if x.Key == a {
			return x.Name
		}
	}
	return "Notiz"
}

// TagCount is a tag and how many notes carry it.
type TagCount struct {
	Tag string
	N   int
}

// ByTags lists the notes carrying every one of want, newest first, at most
// n, and how many notes carry each tag (most first, then by name) — the
// filter's choices. It reads each note's head only (4 KB), at most
// maxFiles files, the hidden ones left out.
func (d *Dir) ByTags(ctx context.Context, want []string, n int) ([]Entry, []TagCount, error) {
	root, err := os.OpenRoot(d.path)
	if err != nil {
		return nil, nil, err
	}
	defer root.Close()
	fsys := root.FS()
	want = Tags(want)
	var all []Entry
	counts := map[string]int{}
	files := 0
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
		if files >= maxFiles {
			return errStop
		}
		files++
		info, err := e.Info()
		if err != nil {
			return nil
		}
		f, err := fsys.Open(name)
		if err != nil {
			return nil
		}
		head, _ := io.ReadAll(io.LimitReader(f, 4<<10))
		f.Close()
		k, _ := SplitKopf(string(head))
		for _, t := range k.Tags {
			counts[t]++
		}
		if len(want) > 0 && k.Has(want) {
			all = append(all, Entry{Name: name, Title: title(name, head), Changed: info.ModTime(), Kopf: k})
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStop) {
		return nil, nil, err
	}
	sort.SliceStable(all, func(i, j int) bool {
		if !all[i].Changed.Equal(all[j].Changed) {
			return all[i].Changed.After(all[j].Changed)
		}
		return all[i].Name < all[j].Name
	})
	if len(all) > n {
		all = all[:n]
	}
	var tc []TagCount
	for t, c := range counts {
		tc = append(tc, TagCount{t, c})
	}
	sort.Slice(tc, func(i, j int) bool {
		if tc[i].N != tc[j].N {
			return tc[i].N > tc[j].N
		}
		return tc[i].Tag < tc[j].Tag
	})
	return all, tc, nil
}

// Set is the head with this kind and these tags, everything else kept;
// "notiz" or an unknown kind is none.
func (k Kopf) Set(art string, tags []string) Kopf {
	if !validArt(art) || art == "notiz" {
		art = ""
	}
	k.Art, k.Tags = art, Tags(tags)
	return k
}
