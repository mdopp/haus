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
	"github.com/mdopp/haus/internal/notes"
)

const (
	maxHits   = 20  // search hits on the page; the notes package caps what it reads
	maxNewest = 10  // notes under Zuletzt geändert
	maxQuery  = 200 // characters of a search
)

// Notizen is the household's notes (haus#12, laib ADR 0017): a search
// field (GET, q=) over the notes package's search, and the newest notes;
// /notizen/<name> is one note's own page. Haus keeps no index and renders
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

		b := familyHead(r, cfg, "Notizen", "Notizen")
		b.WriteString("<h1 class=sr>Notizen</h1>")
		b.WriteString(`<form method=get action=/notizen role=search><label for=q class=sr>Notizen durchsuchen</label>` +
			`<input type=search id=q name=q value="` + html.EscapeString(q) + `" placeholder="Notizen durchsuchen"> <button>Suchen</button></form>`)

		if q != "" {
			var res notes.Result
			var err error
			fetch(r, func(ctx context.Context) { res, err = cfg.Notes.Search(ctx, q, maxHits) })
			logErrs("notizen", map[string]error{"search": err})
			if err == nil {
				c := cards.Card{Title: "Treffer für „" + q + "“"}
				for _, h := range res.Hits {
					first := ""
					if len(h.Lines) > 0 {
						first = h.Lines[0]
					}
					c.AddLink(summary(h.Title), first, noteHref(h.Name))
				}
				more(&c, res.Total-len(res.Hits))
				if res.Total == 0 {
					c.Add("Nichts gefunden", "")
				}
				if res.Partial {
					c.Add("Nicht alle Notizen durchsucht", "es sind zu viele für eine Suche")
				}
				b.WriteString(c.HTML())
			}
			writePage(w, http.StatusOK, b)
			return
		}

		var recent []notes.Entry
		var err error
		fetch(r, func(ctx context.Context) { recent, err = cfg.Notes.Recent(ctx, maxNewest) })
		logErrs("notizen", map[string]error{"notes": err})
		if err == nil {
			c := cards.Card{Title: "Zuletzt geändert"}
			for _, n := range recent {
				c.AddLink(summary(n.Title), "geändert "+dayWord(n.Changed, now, loc)+" "+n.Changed.In(loc).Format("15:04"), noteHref(n.Name))
			}
			if c.Empty() {
				c.Add("Noch keine Notizen", "")
			}
			b.WriteString(c.HTML())
		}
		writePage(w, http.StatusOK, b)
	}
}

// notePage is one note: its title, then its text as paragraphs. A name
// that is not a note — outside the folder, hidden, not there — is a 404
// that says so plainly and shows nothing from outside.
func notePage(w http.ResponseWriter, r *http.Request, cfg StartConfig, ref string) {
	var n notes.Note
	var err error
	fetch(r, func(ctx context.Context) { n, err = cfg.Notes.Read(ctx, ref) })
	if err != nil {
		code := http.StatusNotFound
		say := "Diese Notiz gibt es nicht."
		if !errors.Is(err, notes.ErrNotFound) && !errors.Is(err, notes.ErrOutside) && !errors.Is(err, notes.ErrHidden) {
			log.Printf("haus: notizen page: %q: %v", ref, err)
			code, say = http.StatusServiceUnavailable, "Die Notiz lässt sich gerade nicht lesen."
		}
		b := familyHead(r, cfg, "Notizen", "Notizen")
		b.WriteString("<h1 class=sr>Notizen</h1><p>" + say + "</p><p><a href=/notizen>Alle Notizen</a></p>")
		writePage(w, code, b)
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
