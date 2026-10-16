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
	"github.com/mdopp/haus/internal/favoriten"
	"github.com/mdopp/haus/internal/ha"
)

// Ding is one thing's own page (haus#28), the page a device widget and an
// active-devices row open: its name, its room, its state in the family's
// words and when that changed; a lamp's or a switch's button to switch it
// (through /schalten), and the ☆ that pins it to the person's Start
// (haus#35). Only what a widget may show: an entity of a card domain
// (cards.Domains) that HA does not hide. Anything else — a person, a
// tracker, a hidden entity, one HA does not know — is the same 404, which
// shows nothing of it. HA not answering, or its registry not read yet
// (whether a thing is hidden is not known), is Räume's plain line and the
// way back, a 200 like Start (haus#33).
func Ding(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pinning := r.Method == http.MethodPost && cfg.Favorites != nil
		if !pinning && !showOnly(w, r, "Das Gerät") {
			return
		}
		if cfg.HA == nil {
			toStart(w, r)
			return
		}
		id := strings.TrimPrefix(r.URL.Path, cards.ThingPages)
		if id == "" && !pinning {
			http.Redirect(w, r, "/raeume", http.StatusFound)
			return
		}
		none := missing{http.StatusNotFound, "Dieses Gerät gibt es nicht.", "Räume", "/raeume", "Alle Räume"}
		domain, _, _ := strings.Cut(id, ".")
		if !ha.EntityRE.MatchString(id) || !cards.Domains[domain] {
			none.write(w, r, cfg)
			return
		}
		if pinning {
			pin(w, r, cfg, id)
			return
		}
		var (
			s       ha.State
			err     error
			reg     ha.Registry
			roomsOK bool
			favs    []string
			favErr  error
		)
		person := r.Header.Get("Remote-User")
		reads := []func(context.Context){
			func(ctx context.Context) { s, err = cfg.HA.State(ctx, id) },
			func(ctx context.Context) { reg, roomsOK = cfg.HA.CachedRegistry(ctx) }}
		if cfg.Favorites != nil && person != "" {
			reads = append(reads, func(context.Context) { favs, favErr = cfg.Favorites.For(person) })
		}
		fetch(r, reads...)
		switch {
		case errors.Is(err, ha.ErrNotFound) || (roomsOK && reg.Hidden[id]):
			none.write(w, r, cfg)
			return
		case err != nil || !roomsOK:
			unread := none
			unread.code, unread.say = http.StatusOK, saysNoRooms
			if err != nil {
				log.Printf("haus: gerät page: %q: %v", id, err)
				unread.say = saysNoHouse
			}
			unread.write(w, r, cfg)
			return
		}
		now, loc := cfg.clock()
		name := friendly(s)
		b := familyHead(r, cfg, name, "Räume")
		b.WriteString("<h1>" + html.EscapeString(name) + "</h1>")
		c := cards.Card{Title: "Jetzt"}
		row := cards.Row{Text: roomDetail(domain, s, reg.TempUnit), Detail: changedWords(s, now, loc)}
		if t := tile(domain, s, reg.TempUnit, cards.ThingPage(id)); t.Action != nil {
			row.Action = t.Action
			row.Action.Switch, row.Action.Label = false, map[bool]string{true: "Ausschalten", false: "Einschalten"}[t.On]
		}
		c.Rows = append(c.Rows, row)
		room := trimRunes(oneLine(reg.Rooms[s.EntityID]), 60)
		if room == "" {
			room = noRoom
		}
		c.Add(room, "Raum")
		b.WriteString(c.HTML())
		if cfg.Favorites != nil && person != "" {
			b.WriteString(star(id, favs, favErr))
		}
		b.WriteString("<p><a href=/raeume>Alle Räume</a></p>")
		writePage(w, http.StatusOK, b)
	}
}

// star is the ☆ (haus#35): a plain form that pins the thing to the
// person's Start, or takes it off, and says where it then shows.
func star(id string, favs []string, err error) string {
	if err != nil {
		return "<p class=k>" + saysNoFavorites + "</p>"
	}
	value, label, says := "an", "☆ Auf Start zeigen", "Dann steht es auf Start unter „Meine Geräte“."
	for _, f := range favs {
		if f == id {
			value, label, says = "aus", "★ Von Start nehmen", "Steht auf Start unter „Meine Geräte“."
		}
	}
	return `<section class=stern><form method=post action="` + html.EscapeString(cards.ThingPage(id)) + `"><input type=hidden name=stern value=` + value +
		"><button>" + label + "</button></form><p class=k>" + says + "</p></section>"
}

// pin is the ☆'s POST: the person's own list only, only from Haus's own
// page; then back to Räume (one room on it) when it came from there, else
// to the thing's page (303). A favorites file that does
// not read is never written over: one plain line and the way back.
func pin(w http.ResponseWriter, r *http.Request, cfg StartConfig, id string) {
	if !fromHaus(r) {
		http.Error(w, "Anheften geht nur von Haus' eigener Seite.", http.StatusForbidden)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Das Formular ist nicht lesbar.", http.StatusBadRequest)
		return
	}
	on, ok := map[string]bool{"an": true, "aus": false}[r.PostForm.Get("stern")]
	if !ok {
		http.Error(w, "Anheften kennt nur an und aus.", http.StatusBadRequest)
		return
	}
	back, backText := cards.ThingPage(id), "Zurück zum Gerät"
	if z := r.PostForm.Get("zurueck"); inRaeume.MatchString(z) {
		back, backText = z, "Zurück zu den Räumen"
	}
	err := cfg.Favorites.Set(r.Header.Get("Remote-User"), id, on)
	if err == nil {
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	}
	log.Printf("haus: gerät page: pinning %q: %v", id, err)
	m := missing{http.StatusOK, saysNoFavorites, "Räume", back, backText}
	if errors.Is(err, favoriten.ErrFull) {
		m.say = "Auf Start ist kein Platz mehr. Nimm erst ein anderes Gerät herunter."
	}
	m.write(w, r, cfg)
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
