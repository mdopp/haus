package pages

import (
	"context"
	"encoding/json"
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

// boteWait is how long Sprechen waits for the Bote's first word (haus#63):
// the first message after the model was reloaded takes 40–50 s, with a
// tool more, so two and a half minutes. Once the reply comes in pieces,
// boteTurn caps the whole turn. Variables for the tests; saysSlow names
// the first.
var (
	boteWait = 150 * time.Second
	boteTurn = 5 * time.Minute
)

// beatEvery keeps the streamed answer's connection talking while the Bote
// thinks, so no proxy in between takes the quiet for a dead line.
var beatEvery = 15 * time.Second

// maxSay is how many characters one message may have, as the field takes them.
const maxSay = 1000

// What Sprechen says when there is no reply. The persona's name: it is the
// one who does not answer (laib ADR 0021: the family sees the persona).
func saysDown(name string) string {
	return name + " antwortet gerade nicht. Versuch es gleich noch einmal."
}

func saysSlow(name string) string {
	return name + " hat nach zweieinhalb Minuten noch nicht geantwortet. Vielleicht passiert es trotzdem noch: schau nach, bevor du es noch einmal schickst."
}

// saysCut is a reply that stopped in the middle: what came is shown, and this.
func saysCut(name string) string {
	return name + " ist mit der Antwort nicht fertig geworden; hier bricht sie ab."
}

// saysThinking is what the page shows at once while the Bote works (haus#63).
func saysThinking(name string) string { return name + " denkt nach …" }

const saysTooLong = "Das ist zu lang: höchstens 1000 Zeichen."

// errSlow ends a turn whose first word did not come within boteWait.
var errSlow = errors.New("sprechen: no first word within boteWait")

// sprechenField is the field (haus#12): one line and Senden, posting to
// Haus itself, and the microphone when Haus can hear (haus#63). Without
// script it is a plain form; with it, the post goes in the background and
// the answer comes in pieces on /sprechen (sprechenScript). On Start it
// carries its name and hands the sentence on to /sprechen; on its own page
// the heading says it, and the answer shows right there under the persona's
// name — which only that page says (haus#35).
func sprechenField(cfg StartConfig, here bool) string {
	var b strings.Builder
	b.WriteString("<form method=post action=/sprechen class=sprechen")
	if here {
		name := html.EscapeString(House.Display)
		b.WriteString(` data-hier data-wer="` + name + `" data-denkt="` + html.EscapeString(saysThinking(House.Display)) + `"`)
	}
	b.WriteString(">")
	if !here {
		b.WriteString("<label for=nachricht>Sprechen</label>")
	}
	b.WriteString(`<div><input type=text id=nachricht name=nachricht maxlength=1000 required autocomplete=off enterkeyhint=send ` +
		`placeholder="Eine Frage oder eine Bitte" aria-label=Sprechen>`)
	if canHear(cfg) {
		b.WriteString(micButton)
	}
	b.WriteString("<button class=senden>Senden</button></div><p class=hinweis role=status aria-live=polite></p></form>")
	b.WriteString(sprechenScript)
	return b.String()
}

// exchange is a person's last message and what came back: the reply, the
// line that says why there is none, or both when the reply broke off.
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
// the Bote, never through Haus. A post that accepts text/event-stream (the
// page's script) gets the reply in pieces as they come (haus#63); a plain
// form post waits for the whole and is sent back to the page (303).
func Sprechen(cfg StartConfig) http.HandlerFunc {
	t := &talks{last: map[string]exchange{}}
	return func(w http.ResponseWriter, r *http.Request) {
		person := r.Header.Get("Remote-User")
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			if cfg.Bote == nil {
				notSet{"Sprechen geht hier noch nicht: " + House.Display + " ist noch nicht eingerichtet.", "Für Admins: der Bote läuft nicht (laib add bote, mit einem Kanal; Haus bekommt BOTE_API_KEY)."}.write(w, r, cfg, "Sprechen")
				return
			}
			e, ok := t.get(person)
			b := familyHead(r, cfg, "Sprechen", "Start") // no tab of its own: the field is Start's (haus#12)
			b.WriteString("<h1>Sprechen</h1>" + sprechenField(cfg, true))
			if ok {
				b.WriteString(e.html())
			} else {
				b.WriteString("<p class=\"k vorher\">Was du hier schreibst, liest " + html.EscapeString(House.Display) + ". Die Antwort erscheint hier; das kann einen Moment dauern.</p>")
			}
			b.WriteString(wakeLine(lookWake(r, cfg), cfg)) // and at the speaker: its wake word (owner 2026-10-30)
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
			cfg.Usage.Count("sprechen.senden") // that one was sent, never what (haus#50)
			if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
				streamTurn(w, r, cfg, t, person, text)
				return
			}
			t.set(person, turn(r, cfg, person, text, nil))
			http.Redirect(w, r, "/sprechen", http.StatusSeeOther)
		default:
			w.Header().Set("Allow", "GET, HEAD, POST")
			http.Error(w, "Sprechen geht nur mit dem Feld auf der Seite.", http.StatusMethodNotAllowed)
		}
	}
}

// turn is one sentence to the Bote and what came back. Leaving the page
// does not stop the turn (Hermes runs it on): Haus waits on for the reply
// and keeps it; only boteWait without a first word, or boteTurn in all,
// ends the wait. piece sees each part of the reply as it comes.
func turn(r *http.Request, cfg StartConfig, person, text string, piece func(string)) exchange {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), boteTurn)
	defer cancel()
	ctx, slow := context.WithCancelCause(ctx)
	defer slow(nil)
	first := time.AfterFunc(boteWait, func() { slow(errSlow) })
	defer first.Stop()
	reply, err := cfg.Bote.Stream(ctx, bote.Person{User: person, Name: r.Header.Get("Remote-Name")}, text, func(s string) {
		first.Stop()
		if piece != nil {
			piece(s)
		}
	})
	e := exchange{said: text, reply: reply}
	if err != nil {
		log.Printf("haus: sprechen as %q: %v", person, err)
		switch {
		case reply != "":
			e.line = saysCut(House.Display)
		case errors.Is(context.Cause(ctx), errSlow), errors.Is(err, context.DeadlineExceeded):
			e.line = saysSlow(House.Display)
		default:
			e.line = saysDown(House.Display)
		}
	}
	return e
}

// streamTurn answers the page's script (haus#63): server-sent events, at
// once, so the page knows the sentence arrived; a beat while the Bote
// thinks; each piece of the reply as {"stueck":…}; and at the end
// {"fertig":…}, the exchange as the page shows it after a reload — the
// reply or, never silence, the line why there is none.
func streamTurn(w http.ResponseWriter, r *http.Request, cfg StartConfig, t *talks, person, text string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	rc := http.NewResponseController(w)
	var mu sync.Mutex
	send := func(v any) {
		b, _ := json.Marshal(v)
		mu.Lock()
		defer mu.Unlock()
		w.Write([]byte("data: " + string(b) + "\n\n")) // a page that left: nobody reads it, the turn goes on
		rc.Flush()
	}
	send(map[string]bool{"angekommen": true})
	done := make(chan struct{})
	var beats sync.WaitGroup
	beats.Add(1)
	go func() {
		defer beats.Done()
		tick := time.NewTicker(beatEvery)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-tick.C:
				mu.Lock()
				w.Write([]byte(": denkt\n\n"))
				rc.Flush()
				mu.Unlock()
			}
		}
	}()
	e := turn(r, cfg, person, text, func(s string) { send(map[string]string{"stueck": s}) })
	close(done)
	beats.Wait()
	t.set(person, e)
	send(map[string]string{"fertig": e.html()})
}

// html is the exchange: the person's words, then the persona's reply
// under its name, and Haus's line why there is none (or why it stops) —
// not in the persona's name, it did not say it.
func (e exchange) html() string {
	var b strings.Builder
	b.WriteString("<section class=gespraech aria-label=Gespräch><p class=frage><span class=wer>Du</span>" + html.EscapeString(e.said) + "</p>")
	if e.reply != "" {
		b.WriteString("<div class=antwort><span class=wer>" + html.EscapeString(House.Display) + "</span>" + replyHTML(e.reply) + "</div>")
	}
	if e.line != "" {
		b.WriteString("<p class=keine>" + html.EscapeString(e.line) + "</p>")
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
