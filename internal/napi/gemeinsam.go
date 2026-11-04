package napi

import (
	"crypto/sha256"
	"encoding/hex"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/mdopp/homes/internal/caldav"
)

// Das Homes-Konto (haus#218): the app keeps an Android account "Homes" on each
// family member's phone, and fills its contacts and calendars from here, so
// the dialer, WhatsApp, mail and the calendar app see the household's shared
// data. Read only: what changes, changes in Homes. Shared is what lives in
// Homes' own Radicale account (caldav/gemeinsam.go); a person's own
// calendars and contacts go the other way, from the phone to the archive.
// Standard formats both: vCard as Radicale keeps it, iCalendar with the
// series expanded over a window. Each answer has an ETag of its body, so
// the phones' hourly sync costs a 304 while nothing changed.

const (
	saysNoCalendarHere = "Kalender und Kontakte (Radicale) sind auf dieser Box nicht eingerichtet."
	saysGemeinsamCut   = "Das Adressbuch des Haushalts ist zu groß, um es ganz zu lesen. Sag dem Admin Bescheid."
	saysGemeinsamFail  = "Die Box kommt gerade nicht an Kalender und Kontakte. Die App versucht es später noch einmal."
)

// the window of appointments the phones get: a month back, a year ahead
const (
	gemeinsamBack  = 31 * 24 * time.Hour
	gemeinsamAhead = 366 * 24 * time.Hour
)

type gemeinsam struct {
	dav    *caldav.Client // Homes' own Radicale account; nil: 503
	whoami func(string) (string, bool)
	loc    *time.Location
	now    func() time.Time
}

func (g *gemeinsam) routes(gs gruppen) {
	gs.add("konto", "GET /napi/gemeinsam/kontakte", g.person(g.kontakte))
	gs.add("konto", "GET /napi/gemeinsam/kalender", g.person(g.kalender))
}

// person lets a device of a person through, and only with Radicale set.
func (g *gemeinsam) person(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		person := ""
		if bearer, ok := bearerOf(r); ok && g.whoami != nil {
			person, _ = g.whoami(bearer)
		}
		if !validPerson(person) {
			fehler(w, http.StatusForbidden, saysNoPerson)
			return
		}
		if g.dav == nil {
			fehler(w, http.StatusServiceUnavailable, saysNoCalendarHere)
			return
		}
		next(w, r)
	}
}

// GET /napi/gemeinsam/kontakte → text/vcard: every card of the household's
// address books. A read a cap cut short is refused: the phone would take a
// missing card for a deleted one.
func (g *gemeinsam) kontakte(w http.ResponseWriter, r *http.Request) {
	cards, partial, err := g.dav.FamilyCards(r.Context())
	if err != nil {
		log.Printf("homes: gemeinsam: kontakte: %v", err)
		fehler(w, http.StatusBadGateway, saysGemeinsamFail)
		return
	}
	if partial {
		log.Printf("homes: gemeinsam: kontakte: cut at %d cards", len(cards))
		fehler(w, http.StatusInsufficientStorage, saysGemeinsamCut)
		return
	}
	var b strings.Builder
	for _, c := range cards {
		b.WriteString(c)
		if !strings.HasSuffix(c, "\n") {
			b.WriteString("\r\n")
		}
	}
	g.write(w, r, "text/vcard; charset=utf-8", b.String())
}

// GET /napi/gemeinsam/kalender → text/calendar: one VCALENDAR per shared
// calendar and one of the reminders (caldav.FeedICS).
func (g *gemeinsam) kalender(w http.ResponseWriter, r *http.Request) {
	now := g.clock()
	cals, err := g.dav.FamilyFeed(r.Context(), now.Add(-gemeinsamBack), now.Add(gemeinsamAhead))
	if err != nil {
		log.Printf("homes: gemeinsam: kalender: %v", err)
		fehler(w, http.StatusBadGateway, saysGemeinsamFail)
		return
	}
	// DTSTAMP is the stream's own time; the day keeps the ETag still within it
	day := now.UTC().Truncate(24 * time.Hour)
	g.write(w, r, "text/calendar; charset=utf-8", caldav.FeedICS(cals, day, g.loc))
}

// write answers body with its ETag, or 304 to a phone that has it.
func (g *gemeinsam) write(w http.ResponseWriter, r *http.Request, ctype, body string) {
	sum := sha256.Sum256([]byte(body))
	etag := `"` + hex.EncodeToString(sum[:16]) + `"`
	w.Header().Set("ETag", etag)
	if match := r.Header.Get("If-None-Match"); match != "" {
		for _, m := range strings.Split(match, ",") {
			if strings.TrimSpace(m) == etag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}
	}
	w.Header().Set("Content-Type", ctype)
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(body))
}

func (g *gemeinsam) clock() time.Time {
	if g.now != nil {
		return g.now()
	}
	return time.Now()
}
