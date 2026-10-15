// Package favoriten is each person's favorites on Start (laib ADR 0017:
// favorites go to Haus). The store is one file in Haus's data folder,
// favoriten.json: {"<Remote-User>": ["light.buero", "cover.wohnzimmer"]},
// each list in the person's order. Haus only reads it, on each ask, so a
// change shows on the next page load; nothing in Haus writes it yet (the
// pin from the Sprechen field comes with that field, haus#12).
package favoriten

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/mdopp/haus/internal/ha"
)

// File is the store's name in Haus's data folder.
const File = "favoriten.json"

// maxBytes is the most of the file Haus reads; a family's favorites are a
// few kilobytes.
const maxBytes = 1 << 20

// Store is the favorites file in Haus's data folder.
type Store struct{ path string }

// Open names the store in dir; the file need not be there.
func Open(dir string) *Store { return &Store{path: filepath.Join(dir, File)} }

// For is one person's favorites: Home Assistant entity ids, in their
// order, each once; anything else in the list is left out. No file, or no list for the person, is none; a file
// that does not read is an error, never "none".
func (s *Store) For(person string) ([]string, error) {
	if person == "" {
		return nil, nil
	}
	f, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
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
