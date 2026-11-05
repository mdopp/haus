package napi

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/mdopp/homes/internal/caldav"
	"github.com/mdopp/homes/internal/haushalt"
	"github.com/mdopp/homes/internal/papierkorb"
)

// Das Homes-Konto (haus#218): the app keeps an Android account "Homes" on each
// family member's phone, and fills its contacts and calendars from here, so
// the dialer, WhatsApp, mail and the calendar app see the household's shared
// data. Writable since the owner's call of 2026-11-05: a phone sends one
// appointment or card back by its UID, over the version it last saw, and a
// delete goes into the Papierkorb (caldav/schreiben.go). Shared is what lives in
// Homes' own Radicale account (caldav/gemeinsam.go); a person's own
// calendars and contacts go the other way, from the phone to the archive.
// Standard formats both: vCard as Radicale keeps it, iCalendar with the
// series expanded over a window. Each answer has an ETag of its body, so
// the phones' hourly sync costs a 304 while nothing changed.

const (
	saysNotShared      = "In diesen Kalender schreibt das Handy nicht: er ist nur zum Lesen da oder nicht mehr im Haus."
	saysNotOne         = "Homes hat darin nicht genau diesen einen Eintrag gefunden."
	saysNoPrecondition = "Homes schreibt nur mit If-Match (ändern, löschen) oder If-None-Match: * (neu)."
	saysNoKorb         = "Ohne Papierkorb löscht Homes nichts."
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
	korb   *papierkorb.Korb
	whoami func(string) (string, bool)
	hh     haushalt.Config // the house's time zone, and what Homes adds to a card (Karte)
	now    func() time.Time
}

func (g *gemeinsam) routes(gs gruppen) {
	gs.add("konto", "GET /napi/gemeinsam/kontakte", g.person(g.kontakte))
	gs.add("konto", "GET /napi/gemeinsam/kalender", g.person(g.kalender))
	for _, p := range []string{"/napi/gemeinsam/kontakte/{uid}", "/napi/gemeinsam/kalender/{kalender}/{uid}"} {
		gs.add("konto", "PUT "+p, g.person(g.eintrag))
		gs.add("konto", "DELETE "+p, g.person(g.eintrag))
	}
}

// person lets a device of a person through, and only with Radicale set.
func (g *gemeinsam) person(next personHandler) http.HandlerFunc {
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
		next(w, r, person)
	}
}

// GET /napi/gemeinsam/kontakte → text/vcard: every card of the household's
// address books. A read a cap cut short is refused: the phone would take a
// missing card for a deleted one.
func (g *gemeinsam) kontakte(w http.ResponseWriter, r *http.Request, _ string) {
	cards, partial, err := g.dav.FamilyCards(r.Context(), r.URL.Query().Get("schreibbar") == "1")
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
		b.WriteString(g.hh.Karte.Von(c, "")) // the contact's links, before its end
		if !strings.HasSuffix(c, "\n") {
			b.WriteString("\r\n")
		}
	}
	g.write(w, r, "text/vcard; charset=utf-8", b.String())
}

// GET /napi/gemeinsam/kalender → text/calendar: one VCALENDAR per shared
// calendar and one of the reminders (caldav.FeedICS).
func (g *gemeinsam) kalender(w http.ResponseWriter, r *http.Request, _ string) {
	now := g.clock()
	cals, err := g.dav.FamilyFeed(r.Context(), now.Add(-gemeinsamBack), now.Add(gemeinsamAhead), r.URL.Query().Get("schreibbar") == "1")
	if err != nil {
		log.Printf("homes: gemeinsam: kalender: %v", err)
		fehler(w, http.StatusBadGateway, saysGemeinsamFail)
		return
	}
	// DTSTAMP is the stream's own time; the day keeps the ETag still within it
	day := now.UTC().Truncate(24 * time.Hour)
	g.write(w, r, "text/calendar; charset=utf-8", caldav.FeedICS(cals, day, g.hh.Loc))
}

// PUT|DELETE /napi/gemeinsam/kalender/{kalender}/{uid} (text/calendar)
// and /napi/gemeinsam/kontakte/{uid} (text/vcard): one appointment of a
// shared calendar the phone may write, one card of the shared address
// books. PUT with If-None-Match: * makes it (201), with If-Match changes
// it (200): {uid, kalender, etag} and the ETag. DELETE with If-Match puts
// it into the Papierkorb (30 days, Rückgängig on the Kalender page), then
// removes it (204). Another version than the phone's is 412 with the
// one in Homes and its ETag (an empty body: there is none any more).
func (g *gemeinsam) eintrag(w http.ResponseWriter, r *http.Request, person string) {
	kal, uid, etag := r.PathValue("kalender"), strings.TrimSpace(r.PathValue("uid")), r.Header.Get("If-Match")
	find := func() (caldav.Stored, error) {
		if kal == "" {
			return g.dav.FamilyCard(r.Context(), uid)
		}
		return g.dav.FamilyEvent(r.Context(), kal, uid)
	}
	// exactly one of them: If-Match to change or delete, If-None-Match: * to make
	if (etag == "") == (r.Method == http.MethodDelete || r.Header.Get("If-None-Match") != "*") {
		fehler(w, http.StatusPreconditionRequired, saysNoPrecondition)
		return
	}
	at, err := find()
	art := map[bool]string{true: "kontakt", false: "termin"}[kal == ""]
	if err == nil && r.Method == http.MethodDelete {
		err = g.loeschen(r, at, art, person, etag)
	} else if err == nil {
		body, rerr := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<20))
		if at, err = g.dav.WriteShared(r.Context(), at, uid, g.hh.Karte.Von(string(body), uid), etag); rerr != nil {
			err = caldav.ErrNotOne
		}
	}
	switch {
	case errors.Is(err, caldav.ErrChanged):
		at, _ = find()
		w.Header().Set("ETag", at.ETag)
		w.Header().Set("Content-Type", map[bool]string{true: "text/vcard", false: "text/calendar"}[kal == ""]+"; charset=utf-8")
		w.WriteHeader(http.StatusPreconditionFailed)
		io.WriteString(w, at.Data)
	case errors.Is(err, caldav.ErrNotShared):
		fehler(w, http.StatusForbidden, saysNotShared)
	case errors.Is(err, caldav.ErrNotOne):
		fehler(w, http.StatusBadRequest, saysNotOne)
	case errors.Is(err, papierkorb.ErrWeg):
		fehler(w, http.StatusServiceUnavailable, saysNoKorb)
	case err != nil:
		log.Printf("homes: gemeinsam: %s %s %s/%s: %v", person, r.Method, kal, uid, err)
		fehler(w, http.StatusBadGateway, saysGemeinsamFail)
	case r.Method == http.MethodDelete:
		log.Printf("homes: gemeinsam: %s löscht %s %s/%s", person, art, kal, uid)
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("ETag", at.ETag)
		answer(w, map[bool]int{true: http.StatusCreated, false: http.StatusOK}[etag == ""], map[string]string{"uid": uid, "kalender": kal, "etag": at.ETag})
	}
}

// loeschen puts at into the Papierkorb, then removes it; a remove that
// fails takes it out again.
func (g *gemeinsam) loeschen(r *http.Request, at caldav.Stored, art, person, etag string) error {
	if g.korb == nil {
		return papierkorb.ErrWeg
	}
	if at.Data == "" || at.ETag != etag {
		return caldav.ErrChanged
	}
	e, err := g.korb.Rein(art, at.Title(), person, at)
	if err != nil {
		return err
	}
	if err = g.dav.DeleteShared(r.Context(), at, etag); err != nil {
		g.korb.Raus(e.ID)
	}
	return err
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
