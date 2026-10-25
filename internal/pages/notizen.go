package pages

import (
	"context"
	"errors"
	"html"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/mdopp/haus/internal/cards"
	"github.com/mdopp/haus/internal/library"
	"github.com/mdopp/haus/internal/merken"
	"github.com/mdopp/haus/internal/notes"
)

const (
	maxHits   = 20  // search hits on the page; the notes package caps what it reads
	maxNewest = 10  // notes under Zuletzt geändert
	maxQuery  = 200 // characters of a search
)

// Notizen is the household's notes (haus#12, laib ADR 0017): a search
// field (GET, q=) over the memory library — by meaning, or by words and
// said so when the library cannot (haus#13) — and the newest notes;
// /notizen/<name> is one note's own page. Haus keeps no index (qmd does) and renders
// no Markdown: a note is shown as its text, paragraphs and line breaks,
// never as markup from the file.
func Notizen(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && cfg.Notes != nil {
			noteWrite(w, r, cfg) // Neue Notiz, Bearbeiten (owner, 2026-10-24)
			return
		}
		if !showOnly(w, r, "Notizen") {
			return
		}
		if cfg.Notes == nil {
			notSetNotes.write(w, r, cfg, "Notizen")
			return
		}
		if ref := strings.TrimLeft(strings.TrimPrefix(r.URL.Path, "/notizen"), "/"); ref != "" {
			if r.URL.Query().Get("bearbeiten") == "1" {
				noteEdit(w, r, cfg, ref, http.StatusOK, "", "")
				return
			}
			notePage(w, r, cfg, ref)
			return
		}
		now, loc := cfg.clock()
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		if rs := []rune(q); len(rs) > maxQuery {
			q = string(rs[:maxQuery])
		}

		docs := cfg.Library != nil && cfg.Library.Documents != "" && cfg.Docs != nil
		field := "Notizen durchsuchen"
		if docs {
			field = "Notizen und Dokumente durchsuchen"
		}
		b := familyHead(r, cfg, "Notizen", "Notizen")
		b.WriteString(noteStyle + "<h1>Notizen</h1><p class=k>" + html.EscapeString(saysNotesHow()) + "</p>")
		if q == "" {
			b.WriteString(merkenForm("", "", ""))
			b.WriteString(newNoteForm("", "", "", notes.Tags(r.URL.Query()["tag"]), r.URL.Query().Get("neu") == "1")) // a filter's tags ticked
		}
		b.WriteString(`<form method=get action=/notizen role=search>` +
			`<input type=search name=q value="` + html.EscapeString(q) + `" placeholder="` + field + `" aria-label="` + field + `"> <button data-laeuft="Sucht …">Suchen</button></form>`)

		if q != "" {
			var res library.Found
			var err error
			fetch(r, func(ctx context.Context) {
				res, err = library.Find(ctx, cfg.Library, library.Tidy{Judge: cfg.Judge, Wait: library.PageTidy}, cfg.Notes, cfg.Docs, q, maxHits)
			})
			logErrs("notizen", map[string]error{"search": err})
			if err != nil {
				b.WriteString("<p class=k>" + saysNoNotes + "</p>")
				writePage(w, http.StatusOK, b)
				return
			}
			if res.NoMatch {
				// the library answered, nothing is close: said so, and what
				// only holds a word of it named as such
				if docs {
					b.WriteString("<p class=k>" + saysNothingMatches + "</p>")
				} else {
					b.WriteString("<p class=k>" + saysNoMatch + "</p>")
				}
				if len(res.Hits) > 0 {
					c := cards.Card{Title: byWordsTitle}
					noteRows(&c, res)
					b.WriteString(c.HTML())
				}
				docCard(b, res, docsByWordsTitle)
				writePage(w, http.StatusOK, b)
				return
			}
			if res.Reduced {
				b.WriteString("<p class=k>" + saysWordsOnly + "</p>")
			}
			title, docsTitle := "Treffer für „"+q+"“", "Dokumente zu „"+q+"“"
			if docs {
				title = "Notizen zu „" + q + "“"
			}
			// a search that found documents only shows no empty notes card
			if res.Total > 0 || len(res.Docs) == 0 || res.Partial {
				c := cards.Card{Title: title}
				noteRows(&c, res)
				if res.Total == 0 {
					c.Add("Nichts gefunden.", saysTryAnother)
				}
				if res.Partial {
					c.Add("Nicht alle Notizen durchsucht", "es sind zu viele für eine Suche")
				}
				b.WriteString(c.HTML())
			}
			docCard(b, res, docsTitle)
			writePage(w, http.StatusOK, b)
			return
		}

		var recent, tagged []notes.Entry
		var counts []notes.TagCount
		var err, terr error
		want := notes.Tags(r.URL.Query()["tag"])
		fetch(r, func(ctx context.Context) { recent, err = cfg.Notes.Recent(ctx, maxNewest) },
			func(ctx context.Context) { tagged, counts, terr = cfg.Notes.ByTags(ctx, want, maxTagged) })
		logErrs("notizen", map[string]error{"notes": err, "tags": terr})
		if terr == nil {
			b.WriteString(tagNav(counts, want))
		}
		switch {
		case err != nil:
			b.WriteString("<p class=k>" + saysNoNotes + "</p>")
		case len(want) > 0:
			c := cards.Card{Title: "Notizen mit #" + strings.Join(want, " #")}
			for _, n := range tagged {
				c.AddLink(summary(n.Title), entryDetail(n, now, loc), cards.NotePage(n.Name))
			}
			if c.Empty() {
				c.Add("Keine Notiz trägt diese Tags.", "")
			}
			b.WriteString(c.HTML())
		default:
			c := cards.Card{Title: "Zuletzt geändert"}
			for _, n := range recent {
				c.AddLink(summary(n.Title), entryDetail(n, now, loc), cards.NotePage(n.Name))
			}
			if c.Empty() {
				c.Add("Noch keine Notizen", "")
			}
			b.WriteString(c.HTML())
		}
		writePage(w, http.StatusOK, b)
	}
}

// noteRows are the notes found: title, the first line of the passage,
// the note's page; and how many more.
func noteRows(c *cards.Card, res library.Found) {
	for _, h := range res.Hits {
		first := ""
		if len(h.Lines) > 0 {
			first = h.Lines[0]
		}
		c.AddLink(summary(h.Title), first, cards.NotePage(h.Name))
	}
	more(c, res.Total-len(res.Hits))
}

// docCard is the documents found (haus#13): each with its date, sender and
// the first line of the passage, leading to its page, which sends the
// person on to Paperless — only when Paperless's public address is set.
// Paperless not answering is one plain line where they would be.
func docCard(b *strings.Builder, res library.Found, title string) {
	if res.DocsDown {
		b.WriteString("<p class=k>" + saysNoDocuments + "</p>")
		return
	}
	c := cards.Card{Title: title}
	for _, d := range res.Docs {
		var parts []string
		if !d.Created.IsZero() {
			parts = append(parts, d.Created.Format("02.01.2006"))
		}
		if from := oneLine(d.Correspondent); from != "" {
			parts = append(parts, trimRunes(from, 60))
		}
		if len(d.Lines) > 0 {
			parts = append(parts, d.Lines[0])
		}
		detail := strings.Join(parts, " · ")
		if d.Link != "" {
			c.AddLink(summary(d.Title), detail, cards.DocumentPage(d.ID))
		} else {
			c.Add(summary(d.Title), detail)
		}
	}
	more(&c, res.DocsMore)
	b.WriteString(c.HTML())
}

// notePage is one note: its title, then its text as paragraphs. A name
// that is not a note — outside the folder, hidden, not there — is a 404
// that says so plainly and shows nothing from outside; a folder that
// cannot be read is Notizen's plain line (haus#33).
func notePage(w http.ResponseWriter, r *http.Request, cfg StartConfig, ref string) {
	var n notes.Note
	var err error
	fetch(r, func(ctx context.Context) { n, err = cfg.Notes.Read(ctx, ref) })
	if err != nil {
		m := missing{http.StatusNotFound, "Diese Notiz gibt es nicht.", "Notizen", "/notizen", "Alle Notizen"}
		if !errors.Is(err, notes.ErrNotFound) && !errors.Is(err, notes.ErrOutside) && !errors.Is(err, notes.ErrHidden) {
			log.Printf("haus: notizen page: %q: %v", ref, err)
			m.code, m.say = http.StatusOK, saysNoNotes
		}
		m.write(w, r, cfg)
		return
	}
	title := summary(n.Title)
	b := familyHead(r, cfg, title, "Notizen")
	b.WriteString(noteStyle + "<h1>" + html.EscapeString(title) + "</h1>")
	if said := noteSaid[r.URL.Query().Get("gesagt")]; said != "" {
		b.WriteString("<div class=gut role=status><span class=punkt></span><p>" + html.EscapeString(said) + "</p></div>")
	}
	b.WriteString(kopfLine(n.Kopf))
	_, body := notes.SplitKopf(n.Text)
	for _, p := range paragraphs(body) {
		b.WriteString("<p class=notiz>" + html.EscapeString(p) + "</p>")
	}
	if n.Truncated {
		b.WriteString("<p class=k>Die Notiz ist länger; hier steht ihr Anfang. Bearbeiten geht bei ihr nur über das Netzlaufwerk.</p>")
	}
	b.WriteString("<p class=wege>")
	if !n.Truncated {
		b.WriteString(`<a class=knopf href="` + html.EscapeString(cards.NotePage(n.Name)) + `?bearbeiten=1">Bearbeiten</a> `)
	}
	b.WriteString("<a href=/notizen>Alle Notizen</a></p>")
	writePage(w, http.StatusOK, b)
}

// paragraphs splits a note's text at blank lines, its "# Titel" line left
// out (the page's heading says it), the line breaks inside kept. Control
// characters other than tab go; invalid UTF-8 becomes U+FFFD.
func paragraphs(text string) []string {
	text = strings.ToValidUTF8(strings.ReplaceAll(text, "\r\n", "\n"), "�")
	text = strings.Map(func(r rune) rune {
		if r != '\n' && r != '\t' && unicode.IsControl(r) {
			return -1
		}
		return r
	}, text)
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(l), "# ") {
			lines = lines[i+1:]
		}
		break
	}
	var out []string
	var cur []string
	flush := func() {
		if len(cur) > 0 {
			out = append(out, strings.Join(cur, "\n"))
			cur = nil
		}
	}
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			flush()
			continue
		}
		cur = append(cur, strings.TrimRight(l, " \t"))
	}
	flush()
	return out
}

// What writing a note says (owner, 2026-10-24): where notes come from, and
// each outcome in a sentence. The persona by name, as the one to tell it.
func saysNotesHow() string {
	return "Notizen entstehen, wenn du " + House.Display + " sagst „Merk dir …“ (unter Sprechen oder per Signal), hier mit „Neue Notiz“ oder als Datei im Ordner notes auf dem Netzlaufwerk. " +
		House.Display + " findet sie wieder, wenn du fragst."
}

const (
	saysNoteNoTitle  = "Bitte einen Titel eintragen."
	saysNoteTooLong  = "Die Notiz ist zu lang: höchstens 100 KB, für Längeres ist Paperless da."
	saysNoteChanged  = "Inzwischen hat jemand diese Notiz geändert. Oben steht, was jetzt gespeichert ist, unten dein Text: übernimm, was fehlt, und speichere noch einmal."
	saysNoteNotSaved = "Das ließ sich gerade nicht speichern. Dein Text steht noch unten; versuch es gleich noch einmal."
)

// noteSaid is the sentence after a write, by ?gesagt=.
var noteSaid = map[string]string{"angelegt": "Notiz angelegt.", "gespeichert": "Gespeichert.",
	"gemerkt":   "Gemerkt. Oben das Wichtigste, unten der Link und was auf der Seite stand.",
	"schon":     "Diesen Link hattest du schon gemerkt; hier ist die Notiz.",
	"teilweise": "Gemerkt sind nur Link und Titel: Die Seite ließ sich nicht lesen. Den Text kannst du unter „Bearbeiten“ ergänzen."}

// noteStyle is the note forms' look: a roomy field, phone first.
const noteStyle = `<style>.notizform label{display:block;font-size:.85rem;color:var(--muted);margin:12px 0 6px}
.notizform input[type=text],.notizform textarea{width:100%;font:inherit;color:var(--ink);background:var(--card);border:1px solid var(--edge);border-radius:12px;padding:10px 14px}
.notizform input[type=text],.notizform select{min-height:44px}.notizform select{width:100%;font:inherit;color:var(--ink);background:var(--card);border:1px solid var(--edge);border-radius:12px;padding:0 10px}
.tagwahl{border:0;padding:0;margin:12px 0 0;display:flex;flex-wrap:wrap;gap:8px}.tagwahl legend{font-size:.85rem;color:var(--muted);margin-bottom:6px}.tagwahl .tag{display:inline-flex;align-items:center;gap:6px;min-height:44px;padding:0 14px;border:1px solid var(--edge);border-radius:999px;margin:0;color:var(--ink)}
.tagfilter{display:flex;flex-wrap:wrap;gap:8px;margin:14px 0 0}.tagfilter a{display:inline-flex;align-items:center;gap:6px;min-height:44px;padding:0 14px;border-radius:999px;background:var(--card);border:1px solid var(--edge);color:var(--ink);text-decoration:none}
.tagfilter a[aria-current]{border-color:var(--accent);color:var(--accent);font-weight:600}.kopf a{text-decoration:none}.notizform button:disabled{opacity:.6}.notizform textarea{min-height:12rem;resize:vertical;line-height:1.45}.notizform button{margin:12px 0 0;min-height:44px}
details.neu{margin:16px 0}details.neu summary{cursor:pointer;font-weight:600;min-height:44px;display:flex;align-items:center;color:var(--accent)}
.wege{display:flex;gap:16px;align-items:center;flex-wrap:wrap}.wege .knopf{display:inline-flex;align-items:center;min-height:44px;padding:0 18px;border-radius:999px;background:var(--accent-soft);color:var(--accent);font-weight:600;text-decoration:none}
.vorher{white-space:pre-wrap;border:1px solid var(--edge);border-radius:12px;padding:10px 14px;background:var(--card);font-size:.9rem;max-height:20rem;overflow:auto}</style>`

// newNoteForm is „Neue Notiz“, folded unless asked for or after a problem.
func newNoteForm(title, text, art string, tags []string, open bool) string {
	o := ""
	if open {
		o = " open"
	}
	return `<details class=neu` + o + `><summary>Neue Notiz</summary><form class=notizform method=post action=/notizen>` +
		`<label for=titel>Titel</label><input id=titel type=text name=titel required maxlength=200 value="` + html.EscapeString(title) + `">` +
		`<label for=text>Text</label><textarea id=text name=text>` + html.EscapeString(text) + `</textarea>` +
		kopfFields(art, tags) +
		`<button data-laeuft="Legt an …">Anlegen</button></form></details>`
}

// noteWrite takes a POST: to /notizen a new note (titel, text), to
// /notizen/<name> the note's new text (text, stand: its time as read).
// Only a person, only from Haus's own page; who wrote what is in the log.
// Done is a 303 to the note with its sentence; a problem keeps the text.
func noteWrite(w http.ResponseWriter, r *http.Request, cfg StartConfig) {
	if !fromHaus(r) {
		http.Error(w, "Notizen schreiben geht nur von Haus' eigener Seite.", http.StatusForbidden)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, notes.MaxText+8<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, saysNoteTooLong, http.StatusRequestEntityTooLarge)
		return
	}
	who := r.Header.Get("Remote-User")
	text := r.PostForm.Get("text")
	art, tags := kopfFrom(r.PostForm)
	ref := strings.TrimLeft(strings.TrimPrefix(r.URL.Path, "/notizen"), "/")
	if ref == merkenRef {
		merkenPost(w, r, cfg)
		return
	}
	if ref == "" {
		title := oneLine(r.PostForm.Get("titel"))
		problem := ""
		switch {
		case title == "":
			problem = saysNoteNoTitle
		case len(text) > notes.MaxText:
			problem = saysNoteTooLong
		}
		var name string
		var err error
		if problem == "" {
			if name, err = cfg.Notes.CreateWith(title, text, notes.Kopf{}.Set(art, tags)); err != nil {
				log.Printf("haus: notizen: new %q by %s: %v", title, who, err)
				problem = saysNoteNotSaved
			}
		}
		if problem != "" {
			b := familyHead(r, cfg, "Notizen", "Notizen")
			b.WriteString(noteStyle + "<h1>Notizen</h1><div class=achtung role=alert><p>" + html.EscapeString(problem) + "</p></div>" + newNoteForm(title, text, art, tags, true))
			writePage(w, http.StatusBadRequest, b)
			return
		}
		log.Printf("haus: notizen: %s angelegt by %s", name, who)
		http.Redirect(w, r, cards.NotePage(name)+"?gesagt=angelegt", http.StatusSeeOther)
		return
	}
	ns, err := strconv.ParseInt(r.PostForm.Get("stand"), 10, 64)
	if err != nil {
		http.Error(w, "Das Formular ist nicht vollständig.", http.StatusBadRequest)
		return
	}
	if len(text) > notes.MaxText {
		noteEdit(w, r, cfg, ref, http.StatusBadRequest, saysNoteTooLong, text)
		return
	}
	var was notes.Note
	var rerr error
	fetch(r, func(ctx context.Context) { was, rerr = cfg.Notes.Read(ctx, ref) })
	if rerr != nil && !errors.Is(rerr, notes.ErrNotFound) && !errors.Is(rerr, notes.ErrOutside) && !errors.Is(rerr, notes.ErrHidden) {
		log.Printf("haus: notizen: %s by %s: %v", ref, who, rerr)
		noteEdit(w, r, cfg, ref, http.StatusOK, saysNoteNotSaved, text)
		return
	}
	body := text
	text = was.Kopf.Set(art, tags).With(body) // the head from the fields, its other lines kept
	switch err := cfg.Notes.Update(ref, text, time.Unix(0, ns)); {
	case err == nil:
		log.Printf("haus: notizen: %s gespeichert by %s", ref, who)
		http.Redirect(w, r, cards.NotePage(ref)+"?gesagt=gespeichert", http.StatusSeeOther)
	case errors.Is(err, notes.ErrChanged):
		noteEdit(w, r, cfg, ref, http.StatusConflict, saysNoteChanged, body)
	case errors.Is(err, notes.ErrNotFound), errors.Is(err, notes.ErrOutside), errors.Is(err, notes.ErrHidden):
		missing{http.StatusNotFound, "Diese Notiz gibt es nicht.", "Notizen", "/notizen", "Alle Notizen"}.write(w, r, cfg)
	default:
		log.Printf("haus: notizen: %s by %s: %v", ref, who, err)
		noteEdit(w, r, cfg, ref, http.StatusOK, saysNoteNotSaved, text)
	}
}

// noteEdit is a note's Bearbeiten: its whole file in a field, with the
// time it was read; after a problem the sentence, what is saved now (when
// someone else changed it) and the person's own text kept in the field.
func noteEdit(w http.ResponseWriter, r *http.Request, cfg StartConfig, ref string, code int, problem, mine string) {
	var n notes.Note
	var err error
	fetch(r, func(ctx context.Context) { n, err = cfg.Notes.Read(ctx, ref) })
	if err != nil || n.Truncated {
		if err == nil {
			http.Redirect(w, r, cards.NotePage(n.Name), http.StatusSeeOther) // too long for the field: read-only here
			return
		}
		m := missing{http.StatusNotFound, "Diese Notiz gibt es nicht.", "Notizen", "/notizen", "Alle Notizen"}
		if !errors.Is(err, notes.ErrNotFound) && !errors.Is(err, notes.ErrOutside) && !errors.Is(err, notes.ErrHidden) {
			log.Printf("haus: notizen edit: %q: %v", ref, err)
			m.code, m.say = http.StatusOK, saysNoNotes
		}
		m.write(w, r, cfg)
		return
	}
	title := summary(n.Title)
	b := familyHead(r, cfg, title, "Notizen")
	b.WriteString(noteStyle + "<h1>" + html.EscapeString(title) + " bearbeiten</h1>")
	k, text := notes.SplitKopf(n.Text)
	if problem != "" {
		b.WriteString("<div class=achtung role=alert><p>" + html.EscapeString(problem) + "</p></div>")
		if code == http.StatusConflict {
			b.WriteString("<p class=k>Jetzt gespeichert:</p><div class=vorher>" + html.EscapeString(n.Text) + "</div>")
		}
		text = mine
	}
	b.WriteString(`<form class=notizform method=post action="` + html.EscapeString(cards.NotePage(n.Name)) + `">` +
		`<input type=hidden name=stand value=` + strconv.FormatInt(n.Changed.UnixNano(), 10) + `>` +
		`<label for=text>Text (die erste Zeile „# …“ ist der Titel)</label><textarea id=text name=text>` + html.EscapeString(text) + `</textarea>` +
		kopfFields(k.Art, k.Tags) +
		`<button data-laeuft="Speichert …">Speichern</button></form>` +
		`<p><a href="` + html.EscapeString(cards.NotePage(n.Name)) + `">Abbrechen</a></p>`)
	writePage(w, code, b)
}

// maxTagged is how many notes a tag filter lists.
const maxTagged = 50

// kopfFrom reads a form's kind (art) and tags: the offered ones ticked
// (tag) and the others typed, comma-separated (tags).
func kopfFrom(f url.Values) (string, []string) {
	tags := append([]string{}, f["tag"]...)
	tags = append(tags, strings.Split(f.Get("tags"), ",")...)
	return f.Get("art"), notes.Tags(tags)
}

// kopfFields are a note's kind and tags in a form: the kinds to choose,
// the offered tags to tick, the others to type.
func kopfFields(art string, tags []string) string {
	var b strings.Builder
	b.WriteString(`<label for=art>Art</label><select id=art name=art>`)
	for _, a := range notes.Arten {
		sel := ""
		if a.Key == art || art == "" && a.Key == "notiz" {
			sel = " selected"
		}
		b.WriteString(`<option value=` + a.Key + sel + `>` + a.Name + `</option>`)
	}
	b.WriteString(`</select><fieldset class=tagwahl><legend>Tags</legend>`)
	has := map[string]bool{}
	for _, t := range tags {
		has[t] = true
	}
	offered := map[string]bool{}
	for _, t := range notes.Vorschlaege {
		offered[t] = true
		chk := ""
		if has[t] {
			chk = " checked"
		}
		b.WriteString(`<label class=tag><input type=checkbox name=tag value=` + t + chk + `> #` + t + `</label>`)
	}
	var other []string
	for _, t := range tags {
		if !offered[t] {
			other = append(other, t)
		}
	}
	b.WriteString(`</fieldset><label for=tags>Weitere Tags, mit Komma getrennt</label><input id=tags type=text name=tags autocomplete=off value="` +
		html.EscapeString(strings.Join(other, ", ")) + `" placeholder="z. B. vegetarisch, sommer-2027">`)
	return b.String()
}

// tagNav is the tags as a filter: each with how many notes carry it, a tap
// adds it to the filter or takes it out again.
func tagNav(counts []notes.TagCount, want []string) string {
	if len(counts) == 0 {
		return ""
	}
	on := map[string]bool{}
	for _, t := range want {
		on[t] = true
	}
	var b strings.Builder
	b.WriteString(`<nav class=tagfilter aria-label="Nach Tags filtern">`)
	for _, c := range counts {
		q := url.Values{}
		for _, t := range want {
			if t != c.Tag {
				q.Add("tag", t)
			}
		}
		if !on[c.Tag] {
			q.Add("tag", c.Tag)
		}
		href := "/notizen"
		if len(q) > 0 {
			href += "?" + q.Encode()
		}
		cur := ""
		if on[c.Tag] {
			cur = " aria-current=true"
		}
		b.WriteString(`<a href="` + html.EscapeString(href) + `"` + cur + `>#` + html.EscapeString(c.Tag) + ` <span class=k>` + strconv.Itoa(c.N) + `</span></a>`)
	}
	if len(want) > 0 {
		b.WriteString(`<a href=/notizen class=alle>alle</a>`)
	}
	b.WriteString(`</nav>`)
	return b.String()
}

// entryDetail is a listed note's kind, tags and when it changed.
func entryDetail(n notes.Entry, now time.Time, loc *time.Location) string {
	var parts []string
	if n.Kopf.Art != "" {
		parts = append(parts, notes.ArtName(n.Kopf.Art))
	}
	if len(n.Kopf.Tags) > 0 {
		parts = append(parts, "#"+strings.Join(n.Kopf.Tags, " #"))
	}
	parts = append(parts, "geändert "+dayWord(n.Changed, now, loc)+" "+n.Changed.In(loc).Format("15:04"))
	return strings.Join(parts, " · ")
}

// kopfLine is a note's kind and tags under its title, each tag leading to
// the notes that carry it, and where it came from.
func kopfLine(k notes.Kopf) string {
	if k.Art == "" && len(k.Tags) == 0 && k.Quelle == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString(`<p class="k kopf">`)
	if k.Art != "" {
		b.WriteString(html.EscapeString(notes.ArtName(k.Art)))
	}
	for _, t := range k.Tags {
		b.WriteString(` <a href="/notizen?tag=` + url.QueryEscape(t) + `">#` + html.EscapeString(t) + `</a>`)
	}
	if u, err := url.Parse(k.Quelle); err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" {
		b.WriteString(` · <a href="` + html.EscapeString(k.Quelle) + `" rel="noopener noreferrer" target=_blank>Quelle: ` + html.EscapeString(u.Host) + `</a>`)
	}
	b.WriteString(`</p>`)
	return b.String()
}

// merkenRef is where „Aus Link merken“ posts: /notizen/merken, never a
// note's name (a note ends in .md).
const merkenRef = "merken"

// merkenWait is how long keeping a link may take: the page, then the
// house model's summary.
var merkenWait = 90 * time.Second

// merkenForm is „Aus Link merken“ (haus#117): the link, and what to say
// with it; after a problem the sentence and what was typed.
func merkenForm(link, hint, problem string) string {
	var b strings.Builder
	b.WriteString(`<section class=karte aria-labelledby=k-merken><h2 id=k-merken>Aus Link merken</h2>`)
	if problem != "" {
		b.WriteString(`<div class=achtung role=alert><p>` + html.EscapeString(problem) + `</p></div>`)
	}
	b.WriteString(`<form class=notizform method=post action=/notizen/merken>` +
		`<label for=link>Link zu einem Rezept, einer Rezension, einer Reise …</label><input id=link type=url name=link required inputmode=url autocomplete=off placeholder="https://…" value="` + html.EscapeString(link) + `">` +
		`<label for=hinweis>Dazu, wenn du magst</label><input id=hinweis type=text name=hinweis maxlength=200 autocomplete=off placeholder="z. B. #weihnachten für die Feiertage" value="` + html.EscapeString(hint) + `">` +
		`<button data-laeuft="Liest die Seite …">Merken</button></form>` +
		`<p class=k>` + html.EscapeString(saysMerkenHow()) + `</p></section>`)
	return b.String()
}

// saysMerkenHow is the other way in: the persona on Signal.
func saysMerkenHow() string {
	return "Oder den Link einfach " + House.Display + " auf Signal schicken. Rezepte kommen mit Zutaten und Zubereitung, Rezensionen mit ihrem Kern, Reisen mit Ort und Preis; der Link bleibt immer dabei."
}

// merkenPost keeps a link as a note and opens it, or says why not with
// what was typed still there.
func merkenPost(w http.ResponseWriter, r *http.Request, cfg StartConfig) {
	link, hint := strings.TrimSpace(r.PostForm.Get("link")), oneLine(r.PostForm.Get("hinweis"))
	ctx, cancel := context.WithTimeout(r.Context(), merkenWait)
	defer cancel()
	got, err := merken.Merken(ctx, cfg.Fetcher, cfg.Judge, cfg.Notes, link, hint)
	if err != nil {
		say := err.Error()
		if !errors.Is(err, merken.ErrNoAddress) && !errors.Is(err, merken.ErrPrivate) && !errors.Is(err, merken.ErrNoAnswer) {
			log.Printf("haus: merken by %s: %v", r.Header.Get("Remote-User"), err)
			say = saysNoteNotSaved
		}
		b := familyHead(r, cfg, "Notizen", "Notizen")
		b.WriteString(noteStyle + "<h1>Notizen</h1>" + merkenForm(link, hint, say))
		writePage(w, http.StatusBadRequest, b)
		return
	}
	said := "gemerkt"
	switch {
	case got.Schon:
		said = "schon"
	case got.Teilweise:
		said = "teilweise"
	}
	log.Printf("haus: merken: %s by %s (%s)", got.Name, r.Header.Get("Remote-User"), said)
	http.Redirect(w, r, cards.NotePage(got.Name)+"?gesagt="+said, http.StatusSeeOther)
}
