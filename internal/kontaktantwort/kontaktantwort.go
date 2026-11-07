// Package kontaktantwort is „Kontakt prüfen lassen“ (owner 2026-11-07):
// a person starts it from one of their contacts, Homes makes a long random
// link, the person sends it themselves, and the contact confirms, corrects
// or adds their own name, numbers, e-mail addresses, postal addresses and
// birthday on a page without a login. The link works once and for 14 days;
// only a hash of it is kept. The answer is stored here, apart from every
// address book, and never written anywhere by itself: the person who
// started it sees each field before → after, with the checks, and takes
// what is right. A Haushalts-Kontakt is then written into Radicale; one of
// the person's own Handy-Kontakte waits here until their phone writes it
// into the phone's contact and says so (/napi/kontakte/korrekturen).
//
// The store is one JSON file in Homes' data folder, 0600, read and written
// whole under one lock. Nothing here logs a contact's data.
package kontaktantwort

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/mdopp/homes/internal/jsonfile"
)

const (
	// Frist is how long a link works.
	Frist = 14 * 24 * time.Hour
	// Aufbewahren is how long a decided answer is kept, then it goes.
	Aufbewahren = 30 * 24 * time.Hour
	// Datei is the store's file in Homes' data folder.
	Datei = "kontakt-antworten.json"
	// tokenBytes is the link's randomness: 256 bits.
	tokenBytes = 32
)

// The two books a contact comes from.
const (
	Haushalt = "haushalt" // the household's shared address books (Radicale)
	Privat   = "privat"   // the person's own Handy-Kontakte, written on their phone
)

// What became of one change.
const (
	Offen         = ""              // not decided yet
	Angenommen    = "angenommen"    // taken; a Handy-Kontakt's waits for the phone
	Uebernommen   = "uebernommen"   // written
	Abgelehnt     = "abgelehnt"     // not taken
	Uebersprungen = "uebersprungen" // taken, but the field had changed meanwhile, or the contact is gone
)

// Frage is one link and, once answered, its answer.
type Frage struct {
	ID          string      `json:"id"`   // the review's address, not the link's
	Hash        string      `json:"hash"` // SHA-256 of the link's token, hex
	Wer         string      `json:"wer"`  // who started it, and reviews it
	Buch        string      `json:"buch"` // Haushalt | Privat
	UID         string      `json:"uid"`
	Vorher      Felder      `json:"vorher"` // the fields as the link shows them
	Am          time.Time   `json:"am"`
	Bis         time.Time   `json:"bis"`
	Beantwortet *time.Time  `json:"beantwortet,omitempty"`
	Bestaetigt  bool        `json:"bestaetigt,omitempty"` // „Alles stimmt so“
	Aenderungen []Aenderung `json:"aenderungen,omitempty"`
	Hinweise    []string    `json:"hinweise,omitempty"` // about the whole answer
	Entschieden *time.Time  `json:"entschieden,omitempty"`
	Handy       *time.Time  `json:"handy,omitempty"` // when the phone said what it wrote
}

// Name is the contact's name as the link showed it.
func (f Frage) Name() string { return f.Vorher.Name() }

// ZuPruefen says whether the answer waits for its review.
func (f Frage) ZuPruefen() bool { return f.Beantwortet != nil && f.Entschieden == nil }

// WartetAufsHandy says whether accepted changes wait for the phone.
func (f Frage) WartetAufsHandy() bool {
	return f.Buch == Privat && f.Entschieden != nil && f.Handy == nil &&
		slices.ContainsFunc(f.Aenderungen, func(a Aenderung) bool { return a.Stand == Angenommen })
}

// Store is the file.
type Store struct {
	Path string
	Now  func() time.Time // the tests'; default time.Now
	Rand io.Reader        // the tests'; default crypto/rand
	mu   sync.Mutex
}

// ErrWeg is a link or review that is not (or no longer) there; the page
// says it in one sentence that tells nothing more.
var ErrWeg = errors.New("Link ungültig oder abgelaufen")

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// load reads the store and drops what is over: a link not answered after
// its time, an answer decided (and, for the phone, written) Aufbewahren ago.
func (s *Store) load() ([]Frage, error) {
	b, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var all []Frage
	if err := json.Unmarshal(b, &all); err != nil {
		return nil, err
	}
	now := s.now()
	return slices.DeleteFunc(all, func(f Frage) bool {
		switch {
		case f.Beantwortet == nil:
			return !now.Before(f.Bis)
		case f.Entschieden != nil:
			return now.Sub(*f.Entschieden) > Aufbewahren
		}
		return now.Sub(*f.Beantwortet) > Aufbewahren // never reviewed: it goes too
	}), nil
}

func (s *Store) save(all []Frage) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	if all == nil {
		all = []Frage{}
	}
	return jsonfile.Save(s.Path, all)
}

// change runs fn on the store under the lock and saves what it leaves.
func (s *Store) change(fn func([]Frage) ([]Frage, error)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.load()
	if err != nil {
		return err
	}
	if all, err = fn(all); err != nil {
		return err
	}
	return s.save(all)
}

func (s *Store) read() []Frage {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, _ := s.load()
	return all
}

func (s *Store) random(n int) ([]byte, error) {
	b := make([]byte, n)
	r := s.Rand
	if r == nil {
		r = rand.Reader
	}
	_, err := io.ReadFull(r, b)
	return b, err
}

// hash is the token as it is kept.
func hash(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

// Neu makes the link for one contact: the token, shown once and never
// kept, and its Frage. A link of the same person for the same contact
// that is not answered yet stops working: the new one replaces it.
func (s *Store) Neu(wer, buch, uid string, vorher Felder) (string, Frage, error) {
	tok, err := s.random(tokenBytes)
	if err != nil {
		return "", Frage{}, err
	}
	id, err := s.random(8)
	if err != nil {
		return "", Frage{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(tok)
	now := s.now()
	f := Frage{ID: hex.EncodeToString(id), Hash: hex.EncodeToString(hash(token)), Wer: wer, Buch: buch, UID: uid, Vorher: vorher, Am: now, Bis: now.Add(Frist)}
	err = s.change(func(all []Frage) ([]Frage, error) {
		all = slices.DeleteFunc(all, func(o Frage) bool { return o.Beantwortet == nil && o.Wer == wer && o.Buch == buch && o.UID == uid })
		return append(all, f), nil
	})
	return token, f, err
}

// TokenForm is what a link's token looks like; anything else is no link.
func TokenForm(token string) bool {
	if len(token) != base64.RawURLEncoding.EncodedLen(tokenBytes) {
		return false
	}
	_, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil
}

// zum is the index of the open link token in all, else -1. Every kept
// hash is compared in constant time and none is skipped, so how long it
// takes says nothing about which link exists.
func zum(all []Frage, token string, now time.Time) int {
	want := hash(token)
	found := -1
	for i, f := range all {
		h, err := hex.DecodeString(f.Hash)
		if err != nil || len(h) != len(want) {
			h = make([]byte, len(want))
		}
		if subtle.ConstantTimeCompare(h, want) == 1 && err == nil {
			found = i
		}
	}
	if found < 0 || all[found].Beantwortet != nil || !now.Before(all[found].Bis) {
		return -1
	}
	return found
}

// Zum is the open link token's Frage; ErrWeg when it is none, answered or over.
func (s *Store) Zum(token string) (Frage, error) {
	if !TokenForm(token) {
		return Frage{}, ErrWeg
	}
	all := s.read()
	if i := zum(all, token, s.now()); i >= 0 {
		return all[i], nil
	}
	return Frage{}, ErrWeg
}

// Antwort is what the contact sent, as Beantworten keeps it.
type Antwort struct {
	Bestaetigt  bool
	Aenderungen []Aenderung
	Hinweise    []string
}

// Beantworten keeps the answer to token; the link is dead from then on.
// ErrWeg when it was answered meanwhile or is over.
func (s *Store) Beantworten(token string, a Antwort) (Frage, error) {
	if !TokenForm(token) {
		return Frage{}, ErrWeg
	}
	var f Frage
	err := s.change(func(all []Frage) ([]Frage, error) {
		now := s.now()
		i := zum(all, token, now)
		if i < 0 {
			return nil, ErrWeg
		}
		all[i].Beantwortet, all[i].Bestaetigt, all[i].Aenderungen, all[i].Hinweise = &now, a.Bestaetigt, a.Aenderungen, a.Hinweise
		f = all[i]
		return all, nil
	})
	return f, err
}

// ZuPruefen are wer's answers waiting for the review, the oldest first.
func (s *Store) ZuPruefen(wer string) []Frage {
	var out []Frage
	for _, f := range s.read() {
		if f.Wer == wer && f.ZuPruefen() {
			out = append(out, f)
		}
	}
	slices.SortFunc(out, func(a, b Frage) int { return a.Beantwortet.Compare(*b.Beantwortet) })
	return out
}

// Eine is wer's Frage id.
func (s *Store) Eine(id, wer string) (Frage, error) {
	for _, f := range s.read() {
		if f.ID == id && f.Wer == wer {
			return f, nil
		}
	}
	return Frage{}, ErrWeg
}

// Entscheiden keeps the review of wer's answer id: stand per change
// (Angenommen or Abgelehnt, or after a write Uebernommen or
// Uebersprungen). ErrWeg when it is not there or decided already.
func (s *Store) Entscheiden(id, wer string, stand []string) (Frage, error) {
	var f Frage
	err := s.change(func(all []Frage) ([]Frage, error) {
		i := slices.IndexFunc(all, func(f Frage) bool { return f.ID == id && f.Wer == wer && f.ZuPruefen() })
		if i < 0 || len(stand) != len(all[i].Aenderungen) {
			return nil, ErrWeg
		}
		now := s.now()
		for j := range stand {
			all[i].Aenderungen[j].Stand = stand[j]
		}
		all[i].Entschieden = &now
		f = all[i]
		return all, nil
	})
	return f, err
}

// FuersHandy are wer's accepted changes of their Handy-Kontakte that the
// phone has not written yet.
func (s *Store) FuersHandy(wer string) []Frage {
	var out []Frage
	for _, f := range s.read() {
		if f.Wer == wer && f.WartetAufsHandy() {
			out = append(out, f)
		}
	}
	return out
}

// VomHandy keeps what wer's phone wrote of answer id: the numbers of the
// changes it wrote; every other accepted one it could not (the field had
// changed, or the contact is not on the phone). ErrWeg when nothing waits.
func (s *Store) VomHandy(id, wer string, geschrieben []int) (Frage, error) {
	var f Frage
	err := s.change(func(all []Frage) ([]Frage, error) {
		i := slices.IndexFunc(all, func(f Frage) bool { return f.ID == id && f.Wer == wer && f.WartetAufsHandy() })
		if i < 0 {
			return nil, ErrWeg
		}
		now := s.now()
		for j, a := range all[i].Aenderungen {
			if a.Stand != Angenommen {
				continue
			}
			all[i].Aenderungen[j].Stand = Uebersprungen
			if slices.Contains(geschrieben, j) {
				all[i].Aenderungen[j].Stand = Uebernommen
			}
		}
		all[i].Handy = &now
		f = all[i]
		return all, nil
	})
	return f, err
}
