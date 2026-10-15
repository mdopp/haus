package pages

import (
	"context"
	"errors"
	"html"
	"log"
	"net/http"
	"strings"
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
		if !showOnly(w, r, "Notizen") {
			return
		}
		if cfg.Notes == nil {
			toStart(w, r)
			return
		}
		if ref := strings.TrimLeft(strings.TrimPrefix(r.URL.Path, "/notizen"), "/"); ref != "" {
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
		b.WriteString("<h1>Notizen</h1>")
		b.WriteString(`<form method=get action=/notizen role=search>` +
			`<input type=search name=q value="` + html.EscapeString(q) + `" placeholder="` + field + `" aria-label="` + field + `"> <button>Suchen</button></form>`)

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
	b.WriteString("<h1>" + html.EscapeString(title) + "</h1>")
	for _, p := range paragraphs(n.Text) {
		b.WriteString("<p class=notiz>" + html.EscapeString(p) + "</p>")
	}
	if n.Truncated {
		b.WriteString("<p class=k>Die Notiz ist länger; hier steht ihr Anfang.</p>")
	}
	b.WriteString("<p><a href=/notizen>Alle Notizen</a></p>")
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
