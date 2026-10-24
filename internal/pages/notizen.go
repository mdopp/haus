package pages

import (
	"context"
	"errors"
	"html"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/mdopp/haus/internal/cards"
	"github.com/mdopp/haus/internal/library"
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
			b.WriteString(newNoteForm("", "", r.URL.Query().Get("neu") == "1"))
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

		var recent []notes.Entry
		var err error
		fetch(r, func(ctx context.Context) { recent, err = cfg.Notes.Recent(ctx, maxNewest) })
		logErrs("notizen", map[string]error{"notes": err})
		if err != nil {
			b.WriteString("<p class=k>" + saysNoNotes + "</p>")
		} else {
			c := cards.Card{Title: "Zuletzt geändert"}
			for _, n := range recent {
				c.AddLink(summary(n.Title), "geändert "+dayWord(n.Changed, now, loc)+" "+n.Changed.In(loc).Format("15:04"), cards.NotePage(n.Name))
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
	for _, p := range paragraphs(n.Text) {
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
var noteSaid = map[string]string{"angelegt": "Notiz angelegt.", "gespeichert": "Gespeichert."}

// noteStyle is the note forms' look: a roomy field, phone first.
const noteStyle = `<style>.notizform label{display:block;font-size:.85rem;color:var(--muted);margin:12px 0 6px}
.notizform input[type=text],.notizform textarea{width:100%;font:inherit;color:var(--ink);background:var(--card);border:1px solid var(--edge);border-radius:12px;padding:10px 14px}
.notizform input[type=text]{min-height:44px}.notizform button:disabled{opacity:.6}.notizform textarea{min-height:12rem;resize:vertical;line-height:1.45}.notizform button{margin:12px 0 0;min-height:44px}
details.neu{margin:16px 0}details.neu summary{cursor:pointer;font-weight:600;min-height:44px;display:flex;align-items:center;color:var(--accent)}
.wege{display:flex;gap:16px;align-items:center;flex-wrap:wrap}.wege .knopf{display:inline-flex;align-items:center;min-height:44px;padding:0 18px;border-radius:999px;background:var(--accent-soft);color:var(--accent);font-weight:600;text-decoration:none}
.vorher{white-space:pre-wrap;border:1px solid var(--edge);border-radius:12px;padding:10px 14px;background:var(--card);font-size:.9rem;max-height:20rem;overflow:auto}</style>`

// newNoteForm is „Neue Notiz“, folded unless asked for or after a problem.
func newNoteForm(title, text string, open bool) string {
	o := ""
	if open {
		o = " open"
	}
	return `<details class=neu` + o + `><summary>Neue Notiz</summary><form class=notizform method=post action=/notizen>` +
		`<label for=titel>Titel</label><input id=titel type=text name=titel required maxlength=200 value="` + html.EscapeString(title) + `">` +
		`<label for=text>Text</label><textarea id=text name=text>` + html.EscapeString(text) + `</textarea>` +
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
	ref := strings.TrimLeft(strings.TrimPrefix(r.URL.Path, "/notizen"), "/")
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
			if name, err = cfg.Notes.Create(title, text); err != nil {
				log.Printf("haus: notizen: new %q by %s: %v", title, who, err)
				problem = saysNoteNotSaved
			}
		}
		if problem != "" {
			b := familyHead(r, cfg, "Notizen", "Notizen")
			b.WriteString(noteStyle + "<h1>Notizen</h1><div class=achtung role=alert><p>" + html.EscapeString(problem) + "</p></div>" + newNoteForm(title, text, true))
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
	switch err := cfg.Notes.Update(ref, text, time.Unix(0, ns)); {
	case err == nil:
		log.Printf("haus: notizen: %s gespeichert by %s", ref, who)
		http.Redirect(w, r, cards.NotePage(ref)+"?gesagt=gespeichert", http.StatusSeeOther)
	case errors.Is(err, notes.ErrChanged):
		noteEdit(w, r, cfg, ref, http.StatusConflict, saysNoteChanged, text)
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
	text := n.Text
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
		`<button data-laeuft="Speichert …">Speichern</button></form>` +
		`<p><a href="` + html.EscapeString(cards.NotePage(n.Name)) + `">Abbrechen</a></p>`)
	writePage(w, code, b)
}
