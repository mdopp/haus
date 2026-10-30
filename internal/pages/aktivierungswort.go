package pages

import (
	"context"
	"html"
	"log"
	"net/http"
	"sort"
	"strings"

	"github.com/mdopp/haus/internal/cards"
	"github.com/mdopp/haus/internal/ha"
)

// The wake word (owner 2026-10-30: „Kannst du auf Haus darstellen, was das
// aktuelle Wakeword ist“): the word each voice satellite listens for, as
// the device itself reports it in Home Assistant — its wake-word select, a
// Voice PE's „… Aktivierungswort“ (ha.Registry.WakeWords). The house
// persona's phrase (laib ADR 0020) is what laib trains and puts on the
// devices; it stands in only where no device reports one, and the page
// says so. Where a device still listens for another word, the page says
// that too: the persona's phrase is set, the device does not have it yet.

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
// answering, seen is false and only the persona's phrase is known.
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
	saysWakeSet       = "So ist es eingestellt."
	saysWakeUnseen    = "So ist es eingestellt. Ob der Lautsprecher es schon kennt, kann Haus gerade nicht sehen."
	saysWakeNoDevice  = "So ist es eingestellt. Kein Lautsprecher in Home Assistant meldet ein Aktivierungswort."
	saysWakeNone      = "Kein Lautsprecher in Home Assistant meldet ein Aktivierungswort."
	saysWakeAway      = "antwortet gerade nicht"
	saysWakeNotYet    = "kommt noch auf den Lautsprecher"
	saysWakeUntilThen = "ist eingestellt, aber noch nicht auf dem Lautsprecher. Bis dahin hört er auf "
)

// wakeCard is the Stimme page's Aktivierungswort card: each device's own
// word first, the persona's phrase where none tells. Empty when there is
// nothing to say (no HA, no phrase).
func wakeCard(l wakeLook) cards.Card {
	c := cards.Card{Title: saysWakeTitle}
	phrase := House.Phrase
	switch {
	case !l.seen && phrase != "":
		detail := saysWakeSet
		if l.asked {
			detail = saysWakeUnseen
		}
		c.Add(quoted(phrase), detail)
		return c
	case !l.seen:
		return c
	case len(l.devices) == 0 && phrase != "":
		c.Add(quoted(phrase), saysWakeNoDevice)
		return c
	case len(l.devices) == 0:
		c.Add(saysWakeNone, "")
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
			detail := saysWakeAway
			if phrase != "" {
				detail += "; eingestellt ist " + quoted(phrase)
			}
			c.Add(where, detail)
			continue
		}
		c.Add(quoted(d.word), where)
		if phrase != "" && !strings.EqualFold(d.word, phrase) {
			other = append(other, d.word)
		}
	}
	if len(other) > 0 {
		until := "das Wort oben."
		if w := l.one(); w != "" {
			until = quoted(w) + "."
		}
		c.Add(quoted(phrase)+" "+saysWakeNotYet, quoted(phrase)+" "+saysWakeUntilThen+until)
	}
	return c
}

// wakeLine is Sprechen's one line on the speakers: which word wakes them,
// leading to the Stimme page. "" when nothing is known.
func wakeLine(l wakeLook, cfg StartConfig) string {
	answering := false
	for _, d := range l.devices {
		answering = answering || !d.away
	}
	word, how := l.one(), ""
	switch {
	case word != "":
	case answering: // they differ: the Stimme page says which
		how = "Am Lautsprecher sagst du zuerst sein Aktivierungswort, dann deine Frage."
	case House.Phrase != "":
		word = House.Phrase
	default:
		return ""
	}
	if how == "" {
		how = "Am Lautsprecher sagst du zuerst " + html.EscapeString(quoted(word)) + ", dann deine Frage."
	}
	if stimmePage(cfg) {
		how += ` <a href="` + StimmePath + `">Aktivierungswort und Stimme</a>`
	}
	return "<p class=\"k lautsprecher\">" + how + "</p>"
}
