package pages

import (
	"context"
	"errors"
	"html"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/mdopp/haus/internal/bote"
)

// boteWait is how long Sprechen waits for the Bote's reply: a model with
// tools takes its time, but the page does not wait forever. A variable for
// the tests; saysSlow names it.
var boteWait = 60 * time.Second

// maxSay is how many characters one message may have, as the field takes them.
const maxSay = 1000

// What Sprechen says when there is no reply. The persona's name: it is the
// one who does not answer (laib ADR 0021: the family sees the persona).
func saysDown(name string) string {
	return name + " antwortet gerade nicht. Versuch es später noch einmal."
}

func saysSlow(name string) string {
	return name + " hat nach einer Minute noch nicht geantwortet. Vielleicht passiert es trotzdem noch: schau nach, bevor du es noch einmal schickst."
}

const saysTooLong = "Das ist zu lang: höchstens 1000 Zeichen."

// sprechenField is the field (haus#12): one line and Senden, posting to
// Haus itself; no script, the browser keeps an empty one from going. On
// Start it carries its name; on its own page the heading says it.
const sprechenField = `<form method=post action=/sprechen class=sprechen><label for=nachricht>Sprechen</label>` + sprechenInput

const sprechenInput = `<div><input type=text id=nachricht name=nachricht maxlength=1000 required autocomplete=off enterkeyhint=send ` +
	`placeholder="Eine Frage oder eine Bitte" aria-label=Sprechen><button>Senden</button></div></form>`

// exchange is a person's last message and what came back: the reply, or
// the line that says why there is none.
type exchange struct {
	said, reply, line string
}

// talks holds each person's last exchange, in memory only: the page shows
// it after the post (303, so a reload sends nothing again). A restart
// forgets it; the Bote keeps its own history.
type talks struct {
	mu   sync.Mutex
	last map[string]exchange
}

func (t *talks) get(person string) (exchange, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	e, ok := t.last[person]
	return e, ok
}

func (t *talks) set(person string, e exchange) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.last[person] = e
}

// Sprechen is where the field posts, and the page its reply comes back on
// (haus#12, laib ADR 0016): the sentence goes to the Bote as the person
// Authelia names, the reply is shown as text under the persona's name.
// Only a person, only from Haus's own page (fromHaus). Haus relays and
// decides nothing (haus#1): what needs a yes goes to Laib's inbox through
// the Bote, never through Haus.
func Sprechen(cfg StartConfig) http.HandlerFunc {
	t := &talks{last: map[string]exchange{}}
	return func(w http.ResponseWriter, r *http.Request) {
		person := r.Header.Get("Remote-User")
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			if cfg.Bote == nil {
				toStart(w, r)
				return
			}
			e, ok := t.get(person)
			b := familyHead(r, cfg, "Sprechen", "Sprechen")
			b.WriteString("<h1>Sprechen</h1><form method=post action=/sprechen class=sprechen>" + sprechenInput)
			if ok {
				b.WriteString(e.html())
			} else {
				b.WriteString("<p class=k>Was du hier schreibst, liest " + html.EscapeString(House.Display) + ". Die Antwort erscheint hier; das kann einen Moment dauern.</p>")
			}
			writePage(w, http.StatusOK, b)
		case http.MethodPost:
			if !fromHaus(r) {
				http.Error(w, "Sprechen geht nur von Haus' eigener Seite.", http.StatusForbidden)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
			if err := r.ParseForm(); err != nil {
				http.Error(w, "Das Formular ist nicht lesbar.", http.StatusBadRequest)
				return
			}
			text := strings.TrimSpace(r.PostForm.Get("nachricht"))
			switch {
			case text == "":
				http.Redirect(w, r, "/sprechen", http.StatusSeeOther)
				return
			case utf8.RuneCountInString(text) > maxSay:
				missing{http.StatusBadRequest, saysTooLong, "Sprechen", "/sprechen", "Zurück"}.write(w, r, cfg)
				return
			case cfg.Bote == nil: // a page still open from before the Bote went
				missing{http.StatusOK, saysDown(House.Display), "Sprechen", "/start", "Zurück zu Start"}.write(w, r, cfg)
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), boteWait)
			defer cancel()
			reply, err := cfg.Bote.Ask(ctx, bote.Person{User: person, Name: r.Header.Get("Remote-Name")}, text)
			e := exchange{said: text, reply: reply}
			if err != nil {
				log.Printf("haus: sprechen as %q: %v", person, err)
				e.reply, e.line = "", saysDown(House.Display)
				if errors.Is(err, context.DeadlineExceeded) {
					e.line = saysSlow(House.Display)
				}
			}
			t.set(person, e)
			http.Redirect(w, r, "/sprechen", http.StatusSeeOther)
		default:
			w.Header().Set("Allow", "GET, HEAD, POST")
			http.Error(w, "Sprechen geht nur mit dem Feld auf der Seite.", http.StatusMethodNotAllowed)
		}
	}
}

// html is the exchange: the person's words, then the persona's reply
// under its name, or Haus's line why there is none — not in the
// persona's name, it did not say it.
func (e exchange) html() string {
	var b strings.Builder
	b.WriteString("<section class=gespraech aria-label=Gespräch><p class=frage><span class=wer>Du</span>" + html.EscapeString(e.said) + "</p>")
	if e.line != "" {
		b.WriteString("<p class=keine>" + html.EscapeString(e.line) + "</p>")
	} else {
		b.WriteString("<div class=antwort><span class=wer>" + html.EscapeString(House.Display) + "</span>" + replyHTML(e.reply) + "</div>")
	}
	b.WriteString("</section>")
	return b.String()
}

var bold = regexp.MustCompile(`\*\*([^*\n]+)\*\*`)

// replyHTML shows the Bote's text as text: every character escaped, a
// fenced code block (a persona's screen) as its own box with its lines
// kept, **bold** bold, the rest paragraphs with their line breaks. No
// link, no image, nothing that runs.
func replyHTML(s string) string {
	var b, para, code strings.Builder
	inCode := false
	flush := func() {
		if p := strings.Trim(para.String(), "\n"); strings.TrimSpace(p) != "" {
			b.WriteString("<p>" + bold.ReplaceAllString(html.EscapeString(p), "<strong>$1</strong>") + "</p>")
		}
		para.Reset()
	}
	for _, line := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			if inCode {
				b.WriteString("<pre>" + html.EscapeString(strings.TrimSuffix(code.String(), "\n")) + "</pre>")
				code.Reset()
			} else {
				flush()
			}
			inCode = !inCode
			continue
		}
		switch {
		case inCode:
			code.WriteString(line + "\n")
		case strings.TrimSpace(line) == "":
			flush()
		default:
			para.WriteString(line + "\n")
		}
	}
	if inCode { // a block the reply never closed
		b.WriteString("<pre>" + html.EscapeString(strings.TrimSuffix(code.String(), "\n")) + "</pre>")
	}
	flush()
	return b.String()
}
