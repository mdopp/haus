package pages

import (
	"context"
	"errors"
	"html"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/mdopp/haus/internal/cards"
	"github.com/mdopp/haus/internal/ha"
)

// Ding is one thing's own page (haus#28), the page a device widget and an
// active-devices row open: its name, its room, its state in the family's
// words and when that changed. Read-only like Räume. A thing HA does not
// know is a 404 that says so.
func Ding(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !showOnly(w, r, "Das Gerät") {
			return
		}
		if cfg.HA == nil {
			toStart(w, r)
			return
		}
		id := itemRef(r, cards.ThingPages)
		if id == "" {
			http.Redirect(w, r, "/raeume", http.StatusFound)
			return
		}
		if !entityRE.MatchString(id) {
			notThere(w, r, cfg, http.StatusNotFound, "Dieses Gerät gibt es nicht.", "Räume", "/raeume", "Alle Räume")
			return
		}
		var (
			s       ha.State
			err     error
			reg     ha.Registry
			roomsOK bool
		)
		fetch(r,
			func(ctx context.Context) { s, err = cfg.HA.State(ctx, id) },
			func(ctx context.Context) { reg, roomsOK = cfg.HA.CachedRegistry(ctx) })
		if err != nil {
			code, say := http.StatusNotFound, "Dieses Gerät gibt es nicht."
			if !errors.Is(err, ha.ErrNotFound) {
				log.Printf("haus: gerät page: %q: %v", id, err)
				code, say = http.StatusServiceUnavailable, "Das Gerät lässt sich gerade nicht lesen."
			}
			notThere(w, r, cfg, code, say, "Räume", "/raeume", "Alle Räume")
			return
		}
		now, loc := cfg.clock()
		domain, _, _ := strings.Cut(s.EntityID, ".")
		name := friendly(s)
		b := familyHead(r, cfg, name, "Räume")
		b.WriteString("<h1>" + html.EscapeString(name) + "</h1>")
		c := cards.Card{Title: "Jetzt"}
		c.Add(roomDetail(domain, s, reg.TempUnit), changedWords(s, now, loc))
		if roomsOK {
			room := trimRunes(oneLine(reg.Rooms[s.EntityID]), 60)
			if room == "" {
				room = noRoom
			}
			c.Add(room, "Raum")
		}
		b.WriteString(c.HTML())
		b.WriteString("<p><a href=/raeume>Alle Räume</a></p>")
		writePage(w, http.StatusOK, b)
	}
}

// entityRE is the shape of an entity id; anything else is not asked of HA.
var entityRE = regexp.MustCompile(`^[a-z_]+\.[a-z0-9_]+$`)

// changedWords is when a state last changed: "seit heute 14:03"; "" when
// HA does not say.
func changedWords(s ha.State, now time.Time, loc *time.Location) string {
	at, err := time.Parse(time.RFC3339Nano, s.LastChanged)
	if err != nil {
		return ""
	}
	return "seit " + dayWord(at, now, loc) + " " + at.In(loc).Format("15:04")
}
