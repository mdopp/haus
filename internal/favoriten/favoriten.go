// Package favoriten is each person's favorites on Start (laib ADR 0017:
// favorites go to Haus). The store is one file in Haus's data folder,
// favoriten.json: {"<Remote-User>": ["light.buero", "cover.wohnzimmer"]},
// each list in the person's order. Haus reads it on each ask, so a change
// shows on the next page load; the ☆ on a thing's own page writes it
// (haus#35), the person's own list only.
package favoriten

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/mdopp/haus/internal/ha"
)

// File is the store's name in Haus's data folder.
const File = "favoriten.json"

// maxBytes is the most of the file Haus reads; a family's favorites are a
// few kilobytes.
const maxBytes = 1 << 20

// MaxPerPerson is the longest list a person pins; Start shows the first
// twelve.
const MaxPerPerson = 50

// ErrFull is a pin past MaxPerPerson.
var ErrFull = errors.New("the list is full")

// Store is the favorites file in Haus's data folder.
type Store struct {
	path string
	mu   sync.Mutex // one write at a time; a read needs none (a write replaces the file whole)
}

// Open names the store in dir; the file need not be there.
func Open(dir string) *Store { return &Store{path: filepath.Join(dir, File)} }

// For is one person's favorites: Home Assistant entity ids, in their
// order, each once; anything else in the list is left out. No file, or no list for the person, is none; a file
// that does not read is an error, never "none".
func (s *Store) For(person string) ([]string, error) {
	if person == "" {
		return nil, nil
	}
	all, err := s.read()
	if err != nil {
		return nil, err
	}
	var out []string
	seen := map[string]bool{}
	for _, id := range all[person] {
		if ha.EntityRE.MatchString(id) && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out, nil
}

// read is the whole file; no file is an empty map.
func (s *Store) read() (map[string][]string, error) {
	f, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string][]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxBytes {
		return nil, fmt.Errorf("%s is larger than %d bytes", s.path, maxBytes)
	}
	var all map[string][]string
	if err := json.Unmarshal(b, &all); err != nil {
		return nil, fmt.Errorf("%s: %w", s.path, err)
	}
	if all == nil {
		all = map[string][]string{}
	}
	return all, nil
}

// Set pins entityID to the end of person's list (on) or takes it off;
// pinning what is there, or taking off what is not, changes nothing.
// Nobody else's list changes. A file that does not read is an error and
// is never written over; the new file replaces the old one whole.
func (s *Store) Set(person, entityID string, on bool) error {
	if person == "" {
		return errors.New("no person")
	}
	if !ha.EntityRE.MatchString(entityID) {
		return fmt.Errorf("%q is not an entity id", entityID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.read()
	if err != nil {
		return err
	}
	list := all[person]
	at := slices.Index(list, entityID)
	switch {
	case on && at >= 0, !on && at < 0:
		return nil
	case on && len(list) >= MaxPerPerson:
		return ErrFull
	case on:
		all[person] = append(list, entityID)
	default:
		all[person] = slices.DeleteFunc(list, func(id string) bool { return id == entityID })
	}
	b, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(s.path, b)
}

// writeFile replaces path with b: written beside it, synced, renamed over
// it, so a reader sees the old file or the new one, never half.
func writeFile(path string, b []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+File+"-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // gone after the rename; left over only on failure
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
