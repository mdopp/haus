// Package favoriten is each person's favorites on Start (laib ADR 0017:
// favorites go to Haus). Not yet read.
package favoriten

// File is the store's name in Haus's data folder.
const File = "favoriten.json"

// maxBytes is the most of the file Haus reads.
const maxBytes = 1 << 20

// Store is the favorites file in Haus's data folder.
type Store struct{ path string }

// Open names the store in dir.
func Open(dir string) *Store { return &Store{path: dir} }

// For is one person's favorites.
func (s *Store) For(person string) ([]string, error) { return nil, nil }
