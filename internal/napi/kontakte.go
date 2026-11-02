package napi

import (
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/mdopp/haus/internal/caldav"
)

// Handy-Kontakte (laib ADR 0029): the Haus app sends all of a person's
// phone contacts, and Haus mirrors them into that person's address book
// handy-kontakte in Radicale, as its own account (haus), which may write
// that one book of each person and nothing else of theirs. The person is
// the device's (Whoami), never the request's. The book is a mirror: a card
// the phone no longer has goes, so an upload of no cards at all — a
// permission taken back, an empty phone — is refused, never a wipe.

var kontakteMaxBody int64 = 64 << 20

const (
	kontakteBook = "handy-kontakte"
	kontakteName = "Handy-Kontakte"
)

const (
	saysNoRadicale      = "Die Kontakte-Ablage (Radicale) ist auf dieser Box nicht eingerichtet."
	saysKontakteTooBig  = "Die Kontakte sind zu groß: höchstens 64 MB auf einmal. Lass große Kontaktfotos weg oder sag dem Admin Bescheid."
	saysKontakteType    = "Die Kontakte kommen als text/vcard. Aktualisiere die Haus-App."
	saysKontakteBroken  = "Die Kontakte waren nicht lesbar (eine Karte ist nicht vollständig). Aktualisiere die Haus-App."
	saysKontakteLeer    = "Es kamen keine Kontakte an. Die Box behält die zuletzt gesicherten; erlaube der Haus-App den Zugriff auf die Kontakte, dann geht es weiter."
	saysKontakteBusy    = "Deine Kontakte werden gerade schon abgeglichen. Versuch es gleich noch einmal."
	saysKontakteChanged = "Das Adressbuch Handy-Kontakte hat sich gerade geändert. Versuch es gleich noch einmal."
	saysKontakteFailed  = "Die Box konnte die Kontakte nicht ablegen. Versuch es später noch einmal; hilft das nicht, sag dem Admin Bescheid."
)

// kontakte is the mirror of the phones' contacts; one upload per person at once.
type kontakte struct {
	dav    *caldav.Client // Haus's own Radicale account; nil: 503
	whoami func(string) (string, bool)
	mu     sync.Mutex
	busy   map[string]bool
}

// POST /napi/kontakte, text/vcard: all the phone's contacts →
// {kontakte, neu, geaendert, entfernt}.
func (k *kontakte) post(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if k.dav == nil {
		fehler(w, http.StatusServiceUnavailable, saysNoRadicale)
		return
	}
	person := ""
	if bearer, ok := bearerOf(r); ok && k.whoami != nil {
		person, _ = k.whoami(bearer)
	}
	if !validPerson(person) {
		fehler(w, http.StatusForbidden, saysNoPerson)
		return
	}
	ct := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]))
	if ct != "text/vcard" && ct != "text/x-vcard" {
		fehler(w, http.StatusUnsupportedMediaType, saysKontakteType)
		return
	}
	if r.ContentLength > kontakteMaxBody {
		fehler(w, http.StatusRequestEntityTooLarge, saysKontakteTooBig)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, kontakteMaxBody))
	if err != nil {
		var mb *http.MaxBytesError
		if errors.As(err, &mb) {
			fehler(w, http.StatusRequestEntityTooLarge, saysKontakteTooBig)
			return
		}
		fehler(w, http.StatusRequestTimeout, saysCut)
		return
	}
	cards, err := caldav.SplitCards(string(body))
	var nouid *caldav.ErrNoUID
	switch {
	case errors.As(err, &nouid):
		say := "Ein Kontakt hat keine UID"
		if nouid.Name != "" {
			say = "Der Kontakt „" + nouid.Name + "“ hat keine UID"
		}
		fehler(w, http.StatusBadRequest, say+"; so kann die Box ihn nicht zuordnen. Aktualisiere die Haus-App.")
		return
	case err != nil:
		fehler(w, http.StatusBadRequest, saysKontakteBroken)
		return
	case len(cards) == 0:
		fehler(w, http.StatusConflict, saysKontakteLeer)
		return
	}
	k.mu.Lock()
	if k.busy[person] {
		k.mu.Unlock()
		fehler(w, http.StatusConflict, saysKontakteBusy)
		return
	}
	k.busy[person] = true
	k.mu.Unlock()
	defer func() { k.mu.Lock(); delete(k.busy, person); k.mu.Unlock() }()
	m, err := k.dav.Mirror(r.Context(), "/"+url.PathEscape(person)+"/"+kontakteBook+"/", kontakteName, cards)
	if err != nil {
		log.Printf("haus: kontakte: mirror of %s (%d cards, %+v so far): %v", person, len(cards), m, err)
		if errors.Is(err, caldav.ErrChanged) {
			fehler(w, http.StatusConflict, saysKontakteChanged)
			return
		}
		fehler(w, http.StatusBadGateway, saysKontakteFailed)
		return
	}
	log.Printf("haus: kontakte: %s: %d cards, %d new, %d changed, %d removed", person, m.Kontakte, m.Neu, m.Geaendert, m.Entfernt)
	answer(w, http.StatusOK, m)
}
