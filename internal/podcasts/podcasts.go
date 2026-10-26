// Package podcasts is the family's podcasts (haus#123; owner 2026-10-25):
// subscribed by a link, in Audiobookshelf, keeping only the last three
// episodes, and a new episode told only to who subscribed. Haus keeps who
// subscribed to what (abos.json in HAUS_DATA); ABS keeps the podcasts.
package podcasts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mdopp/haus/internal/abs"
	"github.com/mdopp/haus/internal/merken"
)

// File is the store's name in HAUS_DATA.
const File = "podcasts.json"

// Abo is one podcast and who subscribed to it.
type Abo struct {
	Feed     string   `json:"feed"`
	Item     string   `json:"item"` // ABS's library item
	Title    string   `json:"title"`
	Personen []string `json:"personen"`
	// Seit is when the box subscribed: an episode published before it is
	// no news to tell (the three fetched at the start are old ones)
	Seit time.Time `json:"seit,omitzero"`
	// Geladen: the newest episodes were asked for once (ABS's own check
	// takes only what comes after the subscription)
	Geladen bool `json:"geladen,omitempty"`
}

type state struct {
	Abos []Abo `json:"abos"`
	// Seen are the episodes already told of (and those there when Haus
	// first looked): a new one is one not in it.
	Seen  []string `json:"gesehen"`
	Ready bool     `json:"bereit"` // the first look is done: from then on, new is new
}

// Service is the podcasts: ABS, how links are fetched, and the store.
type Service struct {
	ABS   *abs.Client
	Fetch merken.Fetcher
	path  string
	mu    sync.Mutex
}

// Open is the service with its store in dir.
func Open(c *abs.Client, dir string) *Service {
	return &Service{ABS: c, path: filepath.Join(dir, File)}
}

func (s *Service) load() (state, error) {
	var st state
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	if err := json.Unmarshal(b, &st); err != nil {
		return st, fmt.Errorf("podcasts: %s: %w", s.path, err)
	}
	return st, nil
}

func (s *Service) save(st state) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Ergebnis is what subscribing did, in a sentence to say.
type Ergebnis struct {
	Title, Satz string
	Schon       bool
}

// Abonnieren subscribes person to the podcast behind link: the feed found,
// ABS subscribed (once for the household), only the last three kept, the
// person noted as one to tell.
func (s *Service) Abonnieren(ctx context.Context, person, link string) (Ergebnis, error) {
	if person == "" {
		return Ergebnis{}, errors.New("Abonnieren geht nur angemeldet.")
	}
	found, err := abs.FindFeed(ctx, s.Fetch, link)
	if err != nil {
		return Ergebnis{}, err
	}
	pod, err := s.ABS.Feed(ctx, found.Feed)
	if err != nil {
		return Ergebnis{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.load()
	if err != nil {
		return Ergebnis{}, err
	}
	for i, a := range st.Abos {
		if a.Feed != pod.FeedURL && a.Feed != found.Feed {
			continue
		}
		for _, p := range a.Personen {
			if p == person {
				return Ergebnis{Title: a.Title, Schon: true, Satz: "Du hast „" + a.Title + "“ schon abonniert."}, nil
			}
		}
		st.Abos[i].Personen = append(st.Abos[i].Personen, person)
		return Ergebnis{Title: a.Title, Satz: satz(a.Title, found)}, s.save(st)
	}
	lib, err := s.ABS.PodcastLibrary(ctx)
	if err != nil {
		return Ergebnis{}, err
	}
	item := ""
	if have, err := s.ABS.Podcasts(ctx, lib); err == nil { // ABS has it already (from before Haus): take it
		for _, it := range have {
			if it.FeedURL == pod.FeedURL {
				item = it.ID
				_ = s.ABS.KeepLast(ctx, item)
			}
		}
	}
	if item == "" {
		if item, err = s.ABS.Subscribe(ctx, lib, pod); err != nil {
			return Ergebnis{}, err
		}
	}
	a := Abo{Feed: pod.FeedURL, Item: item, Title: pod.Title, Personen: []string{person}, Seit: time.Now()}
	a.Geladen = s.erste(ctx, a)
	st.Abos = append(st.Abos, a)
	return Ergebnis{Title: pod.Title, Satz: satz(pod.Title, found)}, s.save(st)
}

func satz(title string, f abs.Found) string {
	s := "Abonniert: „" + title + "“. Die letzten 3 Folgen kommen auf die Box; bei jeder neuen sage ich dir Bescheid."
	if f.ByName != "" {
		s = "Über den Namen gefunden: " + s
	}
	return s
}

// Meine are the podcasts person subscribed to, by title.
func (s *Service) Meine(person string) ([]Abo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.load()
	if err != nil {
		return nil, err
	}
	var out []Abo
	for _, a := range st.Abos {
		for _, p := range a.Personen {
			if p == person {
				out = append(out, a)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Title) < strings.ToLower(out[j].Title) })
	return out, nil
}

// Beenden ends person's subscription to feed; with the last one gone the
// podcast leaves ABS too, its files with it. It says whether it did.
func (s *Service) Beenden(ctx context.Context, person, feed string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.load()
	if err != nil {
		return "", err
	}
	for i, a := range st.Abos {
		if a.Feed != feed {
			continue
		}
		var keep []string
		for _, p := range a.Personen {
			if p != person {
				keep = append(keep, p)
			}
		}
		if len(keep) == len(a.Personen) {
			return "", errors.New("Diesen Podcast hast du nicht abonniert.")
		}
		if len(keep) > 0 {
			st.Abos[i].Personen = keep
			return "Abo beendet: „" + a.Title + "“. Andere hören ihn weiter.", s.save(st)
		}
		if err := s.ABS.Remove(ctx, a.Item); err != nil {
			var ae *abs.Error
			if !errors.As(err, &ae) || ae.Status != 404 {
				return "", err
			}
		}
		st.Abos = append(st.Abos[:i], st.Abos[i+1:]...)
		return "Abo beendet: „" + a.Title + "“. Er ist von der Box genommen.", s.save(st)
	}
	return "", errors.New("Diesen Podcast hast du nicht abonniert.")
}

// Nachricht is one person to tell of one new episode.
type Nachricht struct {
	Person, Text string
}

// Neue are the episodes new since the last look, each to tell its
// podcast's subscribers; the first look only notes what is there.
func (s *Service) Neue(ctx context.Context) ([]Nachricht, error) {
	lib, err := s.ABS.PodcastLibrary(ctx)
	if err != nil {
		return nil, err
	}
	eps, err := s.ABS.Recent(ctx, lib, 50)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.load()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, id := range st.Seen {
		seen[id] = true
	}
	who := map[string][]string{}
	since := map[string]time.Time{}
	for _, a := range st.Abos {
		who[a.Item], since[a.Item] = a.Personen, a.Seit
	}
	var out []Nachricht
	for _, e := range eps {
		if seen[e.ID] {
			continue
		}
		seen[e.ID] = true
		st.Seen = append(st.Seen, e.ID)
		// the first look, or an old episode fetched at the start (to the
		// second: ABS keeps milliseconds)
		if !st.Ready || e.Published.Before(since[e.ItemID].Truncate(time.Second)) {
			continue
		}
		for _, p := range who[e.ItemID] {
			out = append(out, Nachricht{Person: p, Text: told(e)})
		}
	}
	st.Ready = true
	if len(st.Seen) > 500 {
		st.Seen = st.Seen[len(st.Seen)-500:]
	}
	return out, s.save(st)
}

// told is a new episode in a line or two: the podcast, the title, the
// first sentence of what it is about.
func told(e abs.Episode) string {
	t := "Neue Folge von „" + e.Podcast + "“: " + e.Title
	if d := firstSentence(plain(e.Description)); d != "" {
		t += "\n" + d
	}
	return t
}

func plain(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		switch {
		case r == '<':
			in = true
		case r == '>':
			in = false
			b.WriteRune(' ')
		case !in:
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func firstSentence(s string) string {
	for i, r := range s {
		if (r == '.' || r == '!' || r == '?') && i > 30 {
			return s[:i+1]
		}
		if i > 240 {
			return s[:i] + "…"
		}
	}
	return s
}

// AusLink is a shared link as a subscription, when it is a podcast (a
// podcast app's address, a feed with audio, a page whose feed has audio):
// false for any other link, and for none on a box without podcasts (s nil).
func (s *Service) AusLink(ctx context.Context, person, link string) (Ergebnis, bool, error) {
	if s == nil || !abs.PodcastLink(ctx, s.Fetch, link) {
		return Ergebnis{}, false, nil
	}
	res, err := s.Abonnieren(ctx, person, link)
	return res, true, err
}

// Bekannt is a refusal said in the family's words already (shown as it
// is); any other error is the box's trouble, said in a sentence by the
// caller.
func Bekannt(err error) bool {
	for _, e := range []error{abs.ErrNoFeed, abs.ErrNoFeedFound, abs.ErrSpotifyOnly, abs.ErrNoLibrary, merken.ErrNoAddress, merken.ErrPrivate, merken.ErrNoAnswer} {
		if errors.Is(err, e) {
			return true
		}
	}
	msg := err.Error()
	return strings.HasPrefix(msg, "Diesen Podcast") || strings.HasPrefix(msg, "Abonnieren geht")
}

// erste asks ABS for the podcast's newest Keep episodes now: its own
// check takes only what comes after the subscription, so without this the
// box would stay empty until the next episode (owner 2026-10-26: "Die
// Folgen werden nicht geladen"). It says whether ABS took the order.
func (s *Service) erste(ctx context.Context, a Abo) bool {
	eps, err := s.ABS.Neueste(ctx, a.Feed, abs.Keep)
	if err == nil {
		err = s.ABS.Laden(ctx, a.Item, eps)
	}
	if err != nil {
		log.Printf("haus: podcasts: %q: the first episodes: %v", a.Title, err)
		return false
	}
	return true
}

// Nachladen fetches the first episodes of a subscription that has none on
// the box and was never asked for them (one made before Haus asked), and
// dates a subscription made before Haus kept the date.
func (s *Service) Nachladen(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.load()
	if err != nil {
		return err
	}
	changed := false
	for i, a := range st.Abos {
		if a.Seit.IsZero() {
			st.Abos[i].Seit, changed = time.Now(), true
		}
		if a.Geladen {
			continue
		}
		n, err := s.ABS.Folgen(ctx, a.Item)
		if err != nil {
			log.Printf("haus: podcasts: %q: %v", a.Title, err)
			continue
		}
		if n > 0 || s.erste(ctx, st.Abos[i]) {
			st.Abos[i].Geladen, changed = true, true
		}
	}
	if !changed {
		return nil
	}
	return s.save(st)
}
