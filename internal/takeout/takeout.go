// Package takeout brings a Google Takeout archive into the household's own
// places, once (laib ADR 0017 §4, haus#13): the calendars into the
// Haushalt calendar and the contacts into the Haushalt address book, both
// in Radicale where the phones sync them, and the Google Keep notes as
// Markdown notes into the notes folder, where the memory library finds
// them. It is a one-shot import, not a service: Haus keeps nothing of it.
//
// Running it twice adds nothing twice. Every entry gets a name that comes
// from what it is — a calendar entry its UID, a contact its UID or one
// made from its name and its first address or number, a note its title and
// when Keep made it — and a write never replaces what is there: an entry
// already imported, and since changed on a phone, stays as it is.
package takeout

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/mdopp/haus/internal/caldav"
	"github.com/mdopp/haus/internal/notes"
)

// Target is where an import writes, and what it leaves out.
type Target struct {
	Radicale *caldav.Client // the Haushalt calendar and address book; nil: no calendars, no contacts
	Notes    *notes.Dir     // nil: no notes
	// SkipCalendars are calendars (by name, any case) not imported, e.g.
	// a holiday calendar the phones show anyway.
	SkipCalendars []string
	// DryRun reads the archive and counts, and writes nothing.
	DryRun bool
	Loc    *time.Location // how a Keep note's dates are written; nil: time.Local

	// seen is what this run has written or found there, by kind and
	// name: an entry the archive lists twice is there the second time,
	// in a dry run too, which therefore counts as the run would.
	seen map[string]bool
}

// again says whether this run has had key already, and from now on has.
func (t Target) again(key string) bool {
	if t.seen[key] {
		return true
	}
	t.seen[key] = true
	return false
}

// Count is what became of one kind of entry.
type Count struct {
	New    int // written now
	There  int // there already (imported before, or twice in the archive)
	Failed int // not written; why is in the log
}

// Report is what an import did.
type Report struct {
	Calendars   []string // the calendars imported, by name
	Skipped     []string // the calendars left out
	Events      Count
	Contacts    Count
	Notes       Count
	Attachments Count
}

// Folder is the notes folder's subfolder the Keep notes go to, and their
// attachments below it.
const (
	Folder      = "Google Notizen"
	Attachments = "Anhänge"
)

// what one member of an archive may be at most
const (
	maxICS        = 64 << 20
	maxVCF        = 64 << 20
	maxKeep       = 1 << 20
	maxAttachment = 25 << 20
)

// archive is the members of one or more zips (Takeout splits a big export
// into -001.zip, -002.zip, …), by their path.
type archive struct {
	files map[string]*zip.File
	names []string // sorted
}

func open(paths []string) (*archive, func(), error) {
	a := &archive{files: map[string]*zip.File{}}
	var closers []io.Closer
	done := func() {
		for _, c := range closers {
			c.Close()
		}
	}
	for _, p := range paths {
		r, err := zip.OpenReader(p)
		if err != nil {
			done()
			return nil, nil, fmt.Errorf("%s: keine lesbare ZIP-Datei: %w", p, err)
		}
		closers = append(closers, r)
		for _, f := range r.File {
			if f.FileInfo().IsDir() {
				continue
			}
			name := strings.ReplaceAll(f.Name, `\`, "/")
			if _, ok := a.files[name]; !ok {
				a.names = append(a.names, name)
			}
			a.files[name] = f
		}
	}
	sort.Strings(a.names)
	return a, done, nil
}

// read is one member, at most max bytes; a bigger one is an error.
func (a *archive) read(name string, max int64) ([]byte, error) {
	f := a.files[name]
	if f.UncompressedSize64 > uint64(max) {
		return nil, fmt.Errorf("%s: größer als %d MB", name, max>>20)
	}
	r, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	b, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, fmt.Errorf("%s: größer als %d MB", name, max>>20)
	}
	return b, nil
}

func ext(name string) string { return strings.ToLower(path.Ext(name)) }

// Import reads the archives and writes what Target has a place for; log
// gets a line for each entry that could not be written and why.
func Import(ctx context.Context, zips []string, t Target, log io.Writer) (Report, error) {
	var rep Report
	a, done, err := open(zips)
	if err != nil {
		return rep, err
	}
	defer done()
	if t.Loc == nil {
		t.Loc = time.Local
	}
	t.seen = map[string]bool{}
	if t.Radicale != nil {
		for _, name := range a.names {
			if ext(name) != ".ics" {
				continue
			}
			if err := calendar(ctx, a, name, t, &rep, log); err != nil {
				return rep, err
			}
		}
		for _, name := range a.names {
			if ext(name) != ".vcf" {
				continue
			}
			if err := contacts(ctx, a, name, t, &rep, log); err != nil {
				return rep, err
			}
		}
	}
	if t.Notes != nil {
		if err := keep(ctx, a, t, &rep, log); err != nil {
			return rep, err
		}
	}
	return rep, nil
}

// fatal says whether a write's error ends the import: Radicale not
// answering, refusing the account, or a context that is done. Anything
// else is that entry's.
func fatal(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return true
	}
	var st *caldav.ErrStatus
	if errors.As(err, &st) {
		return st.Code == 401 || st.Code == 403 || (st.Code >= 300 && st.Code < 400)
	}
	// Radicale not there, or a Haushalt calendar that takes no appointments:
	// every entry would fail the same way
	return strings.Contains(err.Error(), "nicht erreichbar") || strings.Contains(err.Error(), "nimmt kein")
}

// id is a stable name for what has none of its own: a hash of what it is.
func id(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(h[:16])
}

// --- calendars ---

// block is one component as it stands in the file: its lines, folded as
// they came.
type block struct {
	name  string
	lines []string
}

// unfolded is a block's content lines with folding undone.
func (b block) unfolded() []string {
	var out []string
	for _, l := range b.lines {
		if (strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t")) && len(out) > 0 {
			out[len(out)-1] += l[1:]
			continue
		}
		out = append(out, l)
	}
	return out
}

// prop is the value of the block's own first property name (not a nested
// component's), "" when it has none.
func (b block) prop(name string) string {
	depth := 0
	for _, l := range b.unfolded() {
		switch {
		case hasPrefixFold(l, "BEGIN:"):
			depth++
			continue
		case hasPrefixFold(l, "END:"):
			depth--
			continue
		}
		if depth != 1 {
			continue
		}
		i := strings.IndexAny(l, ";:")
		if i < 0 {
			continue
		}
		key := strings.ToUpper(l[:i])
		if j := strings.LastIndexByte(key, '.'); j >= 0 {
			key = key[j+1:] // a vCard group: item1.EMAIL
		}
		if key == name {
			if k := strings.IndexByte(l, ':'); k >= 0 {
				return strings.TrimSpace(l[k+1:])
			}
		}
	}
	return ""
}

// lines is a file's lines, line ends as CRLF or LF.
func lines(b []byte) []string {
	s := strings.ToValidUTF8(strings.ReplaceAll(string(b), "\r\n", "\n"), "�")
	s = strings.TrimPrefix(s, "\ufeff")
	return strings.Split(s, "\n")
}

// split reads a stream and returns its top-level components (outer "")
// or the components directly inside BEGIN:<outer> (each whole, nested
// ones included), and the outer's own lines.
func split(b []byte, outer string) (inner []block, own []string) {
	want := 1
	if outer != "" {
		want = 2
	}
	depth := 0
	var cur *block
	for _, l := range lines(b) {
		if strings.TrimSpace(l) == "" {
			continue
		}
		begin, end := hasPrefixFold(l, "BEGIN:"), hasPrefixFold(l, "END:")
		if begin {
			depth++
			if depth == want {
				cur = &block{name: strings.ToUpper(strings.TrimSpace(l[6:]))}
			}
		}
		if cur != nil {
			cur.lines = append(cur.lines, l)
		} else if depth == want-1 && !begin && !end {
			own = append(own, l)
		}
		if end {
			if depth == want && cur != nil {
				inner = append(inner, *cur)
				cur = nil
			}
			depth--
		}
	}
	return inner, own
}

// hasPrefixFold is strings.HasPrefix, any case (ASCII prefixes only).
func hasPrefixFold(s, prefix string) bool {
	return len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix)
}

const prodID = "-//mdopp//Haus Takeout-Import//DE"

// calName is a calendar's name: X-WR-CALNAME, else its file's.
func calName(file string, own []string) string {
	for _, l := range own {
		if hasPrefixFold(l, "X-WR-CALNAME:") {
			if v := strings.TrimSpace(caldav.UnescapeText(l[len("X-WR-CALNAME:"):])); v != "" {
				return v
			}
		}
	}
	base := path.Base(file)
	return strings.TrimSpace(strings.TrimSuffix(base, path.Ext(base)))
}

func skipped(name string, skip []string) bool {
	for _, s := range skip {
		if strings.EqualFold(strings.TrimSpace(s), name) {
			return true
		}
	}
	return false
}

// calendar imports one .ics: its entries grouped by UID (an exception of
// a series shares its UID and belongs with it), each with the time zones
// it names, as one object.
func calendar(ctx context.Context, a *archive, file string, t Target, rep *Report, log io.Writer) error {
	b, err := a.read(file, maxICS)
	if err != nil {
		fmt.Fprintf(log, "Kalender %s: %v\n", file, err)
		rep.Events.Failed++
		return nil
	}
	cals, _ := split(b, "")
	for _, cal := range cals {
		if cal.name != "VCALENDAR" {
			continue
		}
		comps, own := split([]byte(strings.Join(cal.lines, "\n")), "VCALENDAR")
		name := calName(file, own)
		if skipped(name, t.SkipCalendars) {
			rep.Skipped = append(rep.Skipped, name)
			continue
		}
		rep.Calendars = append(rep.Calendars, name)
		zones := map[string]block{}
		type group struct {
			comp   string
			blocks []block
		}
		groups := map[string]*group{}
		var order []string
		for _, c := range comps {
			switch c.name {
			case "VTIMEZONE":
				zones[c.prop("TZID")] = c
			case "VEVENT", "VTODO":
				uid := c.prop("UID")
				if uid == "" {
					uid = "takeout-" + id(c.prop("SUMMARY"), c.prop("DTSTART"), c.prop("DTEND"), c.prop("DUE")) + "@haus"
					c.lines = append([]string{c.lines[0], "UID:" + uid}, c.lines[1:]...)
				}
				g, ok := groups[uid]
				if !ok {
					g = &group{comp: c.name}
					groups[uid] = g
					order = append(order, uid)
				}
				g.blocks = append(g.blocks, c)
			}
		}
		for _, uid := range order {
			g := groups[uid]
			if t.again("ics\x00" + uid) {
				rep.Events.There++
				continue
			}
			if t.DryRun {
				rep.Events.New++
				continue
			}
			err := t.Radicale.ImportObject(ctx, g.comp, uid, object(g.blocks, zones))
			switch {
			case err == nil:
				rep.Events.New++
			case errors.Is(err, caldav.ErrExists):
				rep.Events.There++
			case fatal(ctx, err):
				return err
			default:
				delete(t.seen, "ics\x00"+uid) // not written: a second listing may be
				rep.Events.Failed++
				fmt.Fprintf(log, "Termin %q (%s): %v\n", summary(g.blocks[0]), name, err)
			}
		}
	}
	return nil
}

func summary(b block) string {
	s := caldav.UnescapeText(b.prop("SUMMARY"))
	if s == "" {
		return "ohne Titel"
	}
	return s
}

// object is one calendar object: the group's components with the time
// zones they name, in a VCALENDAR of its own.
func object(blocks []block, zones map[string]block) string {
	var named []string
	seen := map[string]bool{}
	for _, b := range blocks {
		for _, l := range b.unfolded() {
			for _, part := range strings.Split(strings.SplitN(l, ":", 2)[0], ";") {
				if hasPrefixFold(part, "TZID=") {
					tz := strings.Trim(part[len("TZID="):], `"`)
					if !seen[tz] {
						seen[tz] = true
						named = append(named, tz)
					}
				}
			}
		}
	}
	out := []string{"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:" + prodID}
	for _, tz := range named {
		if z, ok := zones[tz]; ok {
			out = append(out, z.lines...)
		}
	}
	for _, b := range blocks {
		out = append(out, b.lines...)
	}
	out = append(out, "END:VCALENDAR")
	return strings.Join(out, "\r\n") + "\r\n"
}

// --- contacts ---

// contacts imports one .vcf, card by card. Google's cards carry no UID: a
// card's is made from its name and its first email address (else number),
// sorted, so the same person in two of Takeout's address books (All
// Contacts, My Contacts, a label's) is imported once, and a later export
// with a number more is the same card. One without a name gets the
// address or number as its name, as Google shows it; one with none of
// these is left out.
func contacts(ctx context.Context, a *archive, file string, t Target, rep *Report, log io.Writer) error {
	b, err := a.read(file, maxVCF)
	if err != nil {
		fmt.Fprintf(log, "Kontakte %s: %v\n", file, err)
		rep.Contacts.Failed++
		return nil
	}
	cards, _ := split(b, "")
	for _, c := range cards {
		if c.name != "VCARD" {
			continue
		}
		name := caldav.UnescapeText(c.prop("FN"))
		var mails, tels []string
		for _, l := range c.unfolded() {
			key, val := propLine(l)
			switch key {
			case "EMAIL":
				mails = append(mails, strings.ToLower(strings.TrimPrefix(val, "mailto:")))
			case "TEL":
				if d := digits(val); d != "" {
					tels = append(tels, d)
				}
			}
		}
		sort.Strings(mails)
		sort.Strings(tels)
		if strings.TrimSpace(name) == "" {
			if n := nameFromN(c.prop("N")); n != "" {
				name = n
			} else if len(mails) > 0 {
				name = mails[0]
			} else if len(tels) > 0 {
				name = tels[0]
			}
			if strings.TrimSpace(name) == "" {
				rep.Contacts.Failed++
				fmt.Fprintf(log, "Kontakt in %s ohne Namen, E-Mail und Nummer: ausgelassen\n", path.Base(file))
				continue
			}
			c.lines = insert(c.lines, "FN:"+caldav.EscapeText(name))
		}
		uid := c.prop("UID")
		if uid == "" {
			key := strings.ToLower(strings.TrimSpace(name))
			switch {
			case len(mails) > 0:
				key += "|" + mails[0]
			case len(tels) > 0:
				key += "|" + tels[0]
			}
			uid = "takeout-" + id(key)
			c.lines = insert(c.lines, "UID:"+uid)
		}
		if t.again("vcf\x00" + uid) {
			rep.Contacts.There++
			continue
		}
		if t.DryRun {
			rep.Contacts.New++
			continue
		}
		err := t.Radicale.ImportCard(ctx, uid, strings.Join(c.lines, "\r\n")+"\r\n")
		switch {
		case err == nil:
			rep.Contacts.New++
		case errors.Is(err, caldav.ErrExists):
			rep.Contacts.There++
		case fatal(ctx, err):
			return err
		default:
			delete(t.seen, "vcf\x00"+uid)
			rep.Contacts.Failed++
			fmt.Fprintf(log, "Kontakt %q: %v\n", name, err)
		}
	}
	return nil
}

// propLine is a content line's name (without a vCard group) and value.
func propLine(l string) (string, string) {
	i := strings.IndexAny(l, ";:")
	k := strings.IndexByte(l, ':')
	if i < 0 || k < 0 {
		return "", ""
	}
	key := strings.ToUpper(l[:i])
	if j := strings.LastIndexByte(key, '.'); j >= 0 {
		key = key[j+1:]
	}
	return key, strings.TrimSpace(l[k+1:])
}

func digits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// nameFromN is a card's structured name (family;given;additional;prefix;
// suffix) as people write it: "Given Family".
func nameFromN(n string) string {
	p := strings.Split(n, ";")
	for len(p) < 3 {
		p = append(p, "")
	}
	var parts []string
	for _, s := range []string{p[1], p[2], p[0]} {
		if s = strings.TrimSpace(caldav.UnescapeText(s)); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " ")
}

// insert puts a line right after a component's BEGIN (and VERSION, which
// a vCard wants first).
func insert(ls []string, l string) []string {
	at := 1
	if len(ls) > 1 && strings.HasPrefix(strings.ToUpper(ls[1]), "VERSION:") {
		at = 2
	}
	out := append([]string{}, ls[:at]...)
	out = append(out, l)
	return append(out, ls[at:]...)
}

// --- Keep ---

// keepNote is the part of a Keep note's JSON an import reads.
type keepNote struct {
	Title       string `json:"title"`
	TextContent string `json:"textContent"`
	ListContent []struct {
		Text      string `json:"text"`
		IsChecked bool   `json:"isChecked"`
	} `json:"listContent"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
	Attachments []struct {
		FilePath string `json:"filePath"`
	} `json:"attachments"`
	Annotations []struct {
		Title string `json:"title"`
		URL   string `json:"url"`
	} `json:"annotations"`
	IsTrashed  bool  `json:"isTrashed"`
	IsArchived bool  `json:"isArchived"`
	Created    int64 `json:"createdTimestampUsec"`
	Edited     int64 `json:"userEditedTimestampUsec"`

	text, list bool // which of its contents the JSON has
}

// isKeep says whether a JSON file is a Keep note: an object with text or
// a list and Keep's time stamps. Takeout's other JSON files (Labels,
// YouTube, …) are not.
func isKeep(b []byte) (keepNote, bool) {
	var raw map[string]json.RawMessage
	if json.Unmarshal(b, &raw) != nil {
		return keepNote{}, false
	}
	_, text := raw["textContent"]
	_, list := raw["listContent"]
	_, created := raw["createdTimestampUsec"]
	_, edited := raw["userEditedTimestampUsec"]
	if !(text || list) || !(created || edited) {
		return keepNote{}, false
	}
	var n keepNote
	if json.Unmarshal(b, &n) != nil {
		return keepNote{}, false
	}
	n.text, n.list = text, list
	return n, true
}

// keep imports the Keep notes, each as Markdown into Folder, its
// attachments into Folder/Attachments; trashed notes are left out.
func keep(ctx context.Context, a *archive, t Target, rep *Report, log io.Writer) error {
	byBase := map[string][]string{} // an attachment is named by its file's name, in the note's folder
	for _, name := range a.names {
		byBase[path.Base(name)] = append(byBase[path.Base(name)], name)
	}
	for _, name := range a.names {
		if err := ctx.Err(); err != nil {
			return err
		}
		if ext(name) != ".json" {
			continue
		}
		f := a.files[name]
		if f.UncompressedSize64 > maxKeep {
			continue
		}
		b, err := a.read(name, maxKeep)
		if err != nil {
			continue
		}
		n, ok := isKeep(b)
		if !ok || n.IsTrashed {
			continue
		}
		var atts []string // the attachments' names in the folder
		for _, at := range n.Attachments {
			base := path.Base(strings.ReplaceAll(at.FilePath, `\`, "/"))
			if base == "." || base == "/" || strings.HasPrefix(base, ".") {
				continue
			}
			src := ""
			for _, cand := range byBase[base] {
				if path.Dir(cand) == path.Dir(name) {
					src = cand
				}
			}
			if src == "" && len(byBase[base]) > 0 {
				src = byBase[base][0]
			}
			if src == "" {
				rep.Attachments.Failed++
				fmt.Fprintf(log, "Anhang %s fehlt in der Sicherung\n", base)
				continue
			}
			rel := Attachments + "/" + base
			atts = append(atts, rel)
			if t.again("att\x00" + rel) {
				rep.Attachments.There++
				continue
			}
			if t.DryRun {
				rep.Attachments.New++
				continue
			}
			data, err := a.read(src, maxAttachment)
			if err != nil {
				delete(t.seen, "att\x00"+rel)
				rep.Attachments.Failed++
				fmt.Fprintf(log, "Anhang %s: %v\n", base, err)
				continue
			}
			created, err := t.Notes.PutNew(Folder+"/"+rel, data)
			switch {
			case err != nil:
				return fmt.Errorf("Anhang %s: %w", base, err)
			case created:
				rep.Attachments.New++
			default:
				rep.Attachments.There++
			}
		}
		title, body := markdown(n, path.Base(name), atts, t.Loc)
		file := Folder + "/" + notes.Slug(title) + "-" + id(fmt.Sprint(n.Created), n.Title, path.Base(name))[:8] + ".md"
		if t.DryRun {
			rep.Notes.New++
			continue
		}
		created, err := t.Notes.PutNew(file, []byte(body))
		switch {
		case err != nil:
			return fmt.Errorf("Notiz %q: %w", title, err)
		case created:
			rep.Notes.New++
		default:
			rep.Notes.There++
		}
	}
	return nil
}

// markdown is a Keep note as a note of the folder: "# Titel", its text or
// its list as Markdown checkboxes, its attachments and links, then a line
// that says where it comes from. A note without a title is titled by its
// first line, else by the day Keep made it.
func markdown(n keepNote, file string, atts []string, loc *time.Location) (string, string) {
	day := func(usec int64) string { return time.UnixMicro(usec).In(loc).Format("02.01.2006") }
	title := oneLine(n.Title)
	if title == "" {
		for _, l := range strings.Split(n.TextContent, "\n") {
			if l = oneLine(l); l != "" {
				title = cut(l, 60)
				break
			}
		}
	}
	if title == "" && len(n.ListContent) > 0 {
		title = cut(oneLine(n.ListContent[0].Text), 60)
	}
	if title == "" && n.Created > 0 {
		title = "Notiz vom " + day(n.Created)
	}
	if title == "" {
		title = strings.TrimSuffix(file, path.Ext(file))
	}
	var b strings.Builder
	b.WriteString("# " + title + "\n\n")
	if t := strings.TrimSpace(strings.ReplaceAll(n.TextContent, "\r\n", "\n")); t != "" {
		b.WriteString(t + "\n\n")
	}
	if len(n.ListContent) > 0 {
		for _, it := range n.ListContent {
			mark := " "
			if it.IsChecked {
				mark = "x"
			}
			b.WriteString("- [" + mark + "] " + oneLine(it.Text) + "\n")
		}
		b.WriteString("\n")
	}
	for _, at := range atts {
		b.WriteString("Anhang: " + at + "\n")
	}
	for _, an := range n.Annotations {
		if an.URL == "" {
			continue
		}
		label := oneLine(an.Title)
		if label == "" {
			label = an.URL
		}
		b.WriteString("Link: " + label + " – " + an.URL + "\n")
	}
	if len(atts) > 0 || len(n.Annotations) > 0 {
		b.WriteString("\n")
	}
	from := "Aus Google Notizen"
	if n.Created > 0 {
		from += ", angelegt am " + day(n.Created)
	}
	if n.Edited > 0 && (n.Created == 0 || day(n.Edited) != day(n.Created)) {
		from += ", zuletzt geändert am " + day(n.Edited)
	}
	var labels []string
	for _, l := range n.Labels {
		if l.Name = oneLine(l.Name); l.Name != "" {
			labels = append(labels, l.Name)
		}
	}
	if len(labels) > 0 {
		from += ". Labels: " + strings.Join(labels, ", ")
	}
	if n.IsArchived {
		from += ". In Keep archiviert"
	}
	b.WriteString(from + ".\n")
	return title, b.String()
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func cut(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimRightFunc(string(r[:n]), unicode.IsSpace) + "…"
}
