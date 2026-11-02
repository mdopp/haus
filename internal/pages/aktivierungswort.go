package pages

import (
	"context"
	"html"
	"log"
	"net/http"
	"sort"
	"strings"

	"github.com/mdopp/haus/internal/assistent"
	"github.com/mdopp/haus/internal/cards"
	"github.com/mdopp/haus/internal/ha"
)

// The wake word (owner 2026-10-30: „Kannst du auf Haus darstellen, was das
// aktuelle Wakeword ist“): the word each voice satellite listens for, as
// the device itself reports it in Home Assistant — its wake-word select, a
// Voice PE's „… Aktivierungswort“ (ha.Registry.WakeWords). Every speaker
// is to listen for „Hey Mycroft“ (assistent.Wake): a Voice PE offers it out
// of the box and Laib selects it. It stands in only where no device reports
// a word, and the page says so. Where a device still listens for another
// word, the page says that too, and that Laib selects „Hey Mycroft“.
const AktivierungswortPath = "/aktivierungswort"

// Aktivierungswort is the page: the word each speaker listens for. Only on
// a box with Home Assistant, where speakers can be.
func Aktivierungswort(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != AktivierungswortPath || cfg.HA == nil {
			missing{http.StatusNotFound, "Diese Seite gibt es nicht.", "Mehr", mehrPath, "Mehr"}.write(w, r, cfg)
			return
		}
		if !showOnly(w, r, saysWakeTitle) {
			return
		}
		b := familyHead(r, cfg, saysWakeTitle, saysWakeTitle)
		b.WriteString("<h1>" + saysWakeTitle + "</h1>")
		b.WriteString(wakeCard(lookWake(r, cfg)).HTML())
		writePage(w, http.StatusOK, b)
	}
}

// wakeDevice is one satellite's wake word as HA reports it.
type wakeDevice struct {
	name, room, word string
	away             bool // HA has no word from it (unavailable, unknown)
}

// wakeLook is what Haus could see of the wake words.
type wakeLook struct {
	devices []wakeDevice
	asked   bool // this Haus has Home Assistant
	seen    bool // HA answered and its registry is known: devices is the whole truth
}

// lookWake reads the wake-word selects' states; without HA, or with HA not
// answering, seen is false and only the phrase meant to be set is known.
func lookWake(r *http.Request, cfg StartConfig) wakeLook {
	if cfg.HA == nil {
		return wakeLook{}
	}
	look := wakeLook{asked: true}
	var (
		states []ha.State
		err    error
		reg    ha.Registry
		ok     bool
	)
	fetch(r,
		func(ctx context.Context) { states, err = cfg.HA.States(ctx) },
		func(ctx context.Context) { reg, ok = cfg.HA.CachedRegistry(ctx) })
	if err != nil {
		log.Printf("haus: wake word: home assistant: %v", err)
		return look
	}
	if !ok { // the registry not read yet: which selects are wake words is unknown
		return look
	}
	look.seen = true
	for _, s := range states {
		if !reg.WakeWords[s.EntityID] {
			continue
		}
		name := friendly(s)
		for _, suffix := range []string{" Aktivierungswort", " Wake word"} {
			name = strings.TrimSuffix(name, suffix)
		}
		d := wakeDevice{name: name, room: reg.Rooms[s.EntityID], word: oneLine(s.State)}
		if d.word == "" || d.word == "unavailable" || d.word == "unknown" {
			d.word, d.away = "", true
		}
		look.devices = append(look.devices, d)
	}
	sort.Slice(look.devices, func(i, j int) bool { return look.devices[i].name < look.devices[j].name })
	return look
}

// one is the word every answering device reports, "" when they differ or none answers.
func (l wakeLook) one() string {
	word := ""
	for _, d := range l.devices {
		switch {
		case d.away:
		case word == "":
			word = d.word
		case !strings.EqualFold(word, d.word):
			return ""
		}
	}
	return word
}

// quoted is a word in German quotes; the cards escape it, a page's own line escapes it itself.
func quoted(w string) string { return "„" + w + "“" }

// What the Aktivierungswort card says.
const (
	saysWakeTitle     = "Aktivierungswort"
	saysWakeReported  = "So meldet es der Lautsprecher selbst."
	saysWakeUnseen    = "So ist es eingestellt. Was der Lautsprecher gerade hört, kann Haus im Moment nicht sehen."
	saysWakeNoDevice  = "So ist es eingestellt. Kein Lautsprecher in Home Assistant meldet ein Aktivierungswort."
	saysWakeAway      = "antwortet gerade nicht"
	saysWakeChosen    = "wird noch ausgewählt"
	saysWakeUntilThen = "kann der Lautsprecher schon, Laib wählt es für ihn aus. Bis dahin hört er auf "
)

// wakeCard is the Aktivierungswort page's card: each device's own word
// first, „Hey Mycroft“ where none tells.
func wakeCard(l wakeLook) cards.Card {
	c := cards.Card{Title: saysWakeTitle}
	phrase := assistent.Wake
	switch {
	case !l.seen:
		c.Add(quoted(phrase), saysWakeUnseen)
		return c
	case len(l.devices) == 0:
		c.Add(quoted(phrase), saysWakeNoDevice)
		return c
	}
	c.Note = saysWakeReported
	var other []string // the words heard instead of the phrase
	for _, d := range l.devices {
		where := "Lautsprecher " + quoted(d.name)
		if d.room != "" && !strings.EqualFold(d.room, d.name) {
			where += " · " + d.room
		}
		if d.away {
			c.Add(where, saysWakeAway+"; eingestellt ist "+quoted(phrase))
			continue
		}
		c.Add(quoted(d.word), where)
		if !strings.EqualFold(d.word, phrase) {
			other = append(other, d.word)
		}
	}
	if len(other) > 0 {
		until := "das Wort oben."
		if w := l.one(); w != "" {
			until = quoted(w) + "."
		}
		c.Add(quoted(phrase)+" "+saysWakeChosen, quoted(phrase)+" "+saysWakeUntilThen+until)
	}
	return c
}

// wakeLine is Sprechen's one line on the speakers: which word wakes them,
// leading to the Aktivierungswort page. "" without Home Assistant: no
// speaker Haus could know of.
func wakeLine(l wakeLook) string {
	if !l.asked {
		return ""
	}
	answering := false
	for _, d := range l.devices {
		answering = answering || !d.away
	}
	how := ""
	switch word := l.one(); {
	case word != "":
		how = "Am Lautsprecher sagst du zuerst " + html.EscapeString(quoted(word)) + ", dann deine Frage."
	case answering: // they differ: the page says which
		how = "Am Lautsprecher sagst du zuerst sein Aktivierungswort, dann deine Frage."
	default:
		how = "Am Lautsprecher sagst du zuerst " + html.EscapeString(quoted(assistent.Wake)) + ", dann deine Frage."
	}
	return "<p class=\"k lautsprecher\">" + how + ` <a href="` + AktivierungswortPath + `">Aktivierungswort</a></p>`
}
