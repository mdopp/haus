package pages

import (
	"context"
	"errors"
	"html"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/mdopp/haus/internal/cards"
	"github.com/mdopp/haus/internal/ha"
)

// Ding is one thing's own page (haus#28), the page a device widget and an
// active-devices row open: its name, its room, its state in the family's
// words and when that changed. Read-only like Räume. Only what a widget may
// show: an entity of a card domain (cards.Domains) that HA does not hide.
// Anything else — a person, a tracker, a hidden entity, one HA does not
// know — is the same 404, which shows nothing of it. Until HA's registry
// has been read, whether a thing is hidden is not known: 503.
func Ding(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !showOnly(w, r, "Das Gerät") {
			return
		}
		if cfg.HA == nil {
			toStart(w, r)
			return
		}
		id := strings.TrimPrefix(r.URL.Path, cards.ThingPages)
		if id == "" {
			http.Redirect(w, r, "/raeume", http.StatusFound)
			return
		}
		none := missing{http.StatusNotFound, "Dieses Gerät gibt es nicht.", "Räume", "/raeume", "Alle Räume"}
		domain, _, _ := strings.Cut(id, ".")
		if !ha.EntityRE.MatchString(id) || !cards.Domains[domain] {
			none.write(w, r, cfg)
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
		switch {
		case errors.Is(err, ha.ErrNotFound) || (roomsOK && reg.Hidden[id]):
			none.write(w, r, cfg)
			return
		case err != nil || !roomsOK:
			if err != nil {
				log.Printf("haus: gerät page: %q: %v", id, err)
			}
			unread := none
			unread.code, unread.say = http.StatusServiceUnavailable, "Das Gerät lässt sich gerade nicht lesen."
			unread.write(w, r, cfg)
			return
		}
		now, loc := cfg.clock()
		name := friendly(s)
		b := familyHead(r, cfg, name, "Räume")
		b.WriteString("<h1>" + html.EscapeString(name) + "</h1>")
		c := cards.Card{Title: "Jetzt"}
		c.Add(roomDetail(domain, s, reg.TempUnit), changedWords(s, now, loc))
		room := trimRunes(oneLine(reg.Rooms[s.EntityID]), 60)
		if room == "" {
			room = noRoom
		}
		c.Add(room, "Raum")
		b.WriteString(c.HTML())
		b.WriteString("<p><a href=/raeume>Alle Räume</a></p>")
		writePage(w, http.StatusOK, b)
	}
}

// changedWords is when a state last changed: "seit heute 14:03"; "" when
// HA does not say.
func changedWords(s ha.State, now time.Time, loc *time.Location) string {
	at, err := time.Parse(time.RFC3339Nano, s.LastChanged)
	if err != nil {
		return ""
	}
	return "seit " + dayWord(at, now, loc) + " " + at.In(loc).Format("15:04")
}
