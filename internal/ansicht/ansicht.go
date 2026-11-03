// Package ansicht is how each person sees the calendars on Haushalt/Kalender
// (haus#66): a colour per calendar and the ones they hid. It is Homes' own
// view, one file in Homes' data folder, ansicht.json:
// {"<Remote-User>": {"aus": ["/lea/abo-feiertage/"], "farben": {"/homes/haushalt/": "#2f6fdf"}}}.
// The calendars themselves stay in Radicale untouched; a phone keeps its own
// colours. A file that does not read is an error and is never written over.
package ansicht

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/mdopp/homes/internal/caldav"
)

// File is the store's name in Homes' data folder.
const File = "ansicht.json"

const maxBytes = 1 << 20

// Colors are the colours a calendar may have: the page's swatches, by name.
var Colors = []struct{ Name, Hex string }{
	{"Blau", "#2f6fdf"}, {"Grün", "#2b8a3e"}, {"Rot", "#c92a2a"}, {"Orange", "#e8590c"},
	{"Lila", "#7048e8"}, {"Türkis", "#0c8599"}, {"Grau", "#868e96"},
}

// ColorRE is a colour as the store keeps it.
var ColorRE = regexp.MustCompile(`^#[0-9a-f]{6}$`)

// One is one person's view.
type One struct {
	Hidden []string          `json:"aus,omitempty"`
	Colors map[string]string `json:"farben,omitempty"`
}

// Shows says whether the calendar at path is on the page.
func (o One) Shows(path string) bool { return !slices.Contains(o.Hidden, path) }

// Color is the calendar's colour, "" for none chosen.
func (o One) Color(path string) string { return o.Colors[path] }

// Store is the file.
type Store struct {
	path    string
	account string // Homes' Radicale account (HOMES_RADICALE_USER), for the legacy paths
	mu      sync.Mutex
}

// Open names the store in dir; the file need not be there. account is
// Homes' Radicale account: a calendar stored under the old account's path
// is read as its path now (caldav.LegacyAccountPath).
func Open(dir, account string) *Store {
	return &Store{path: filepath.Join(dir, File), account: account}
}

// For is one person's view; no file, or nothing for them, is the default.
func (s *Store) For(person string) (One, error) {
	if s == nil || person == "" {
		return One{}, nil
	}
	all, err := s.read()
	return all[person], err
}

func (s *Store) read() (map[string]One, error) {
	f, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]One{}, nil
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
	all := map[string]One{}
	if err := json.Unmarshal(b, &all); err != nil {
		return nil, fmt.Errorf("%s: %w", s.path, err)
	}
	for person, o := range all {
		all[person] = s.legacy(o)
	}
	return all, nil
}

// legacy is legacy: a view stored while Homes' account was haus names
// /haus/… calendars; read, they are the account's now, and the next Set
// writes them so. A colour stored under the new path wins over the old
// one. Ends with laib#1021 step 9.
func (s *Store) legacy(o One) One {
	var hidden []string
	for _, p := range o.Hidden {
		if p = caldav.LegacyAccountPath(p, s.account); !slices.Contains(hidden, p) {
			hidden = append(hidden, p)
		}
	}
	o.Hidden = hidden
	for p, c := range o.Colors {
		if q := caldav.LegacyAccountPath(p, s.account); q != p {
			delete(o.Colors, p)
			if _, ok := o.Colors[q]; !ok {
				o.Colors[q] = c
			}
		}
	}
	return o
}

// Set changes one calendar in one person's view: hidden or shown, and its
// colour ("" leaves it; "-" takes it back to none). Nobody else's changes.
func (s *Store) Set(person, path string, hide *bool, color string) error {
	if person == "" || !strings.HasPrefix(path, "/") || !strings.HasSuffix(path, "/") || len(path) > 300 {
		return errors.New("ansicht: no person or no calendar")
	}
	if color != "" && color != "-" && !ColorRE.MatchString(color) {
		return fmt.Errorf("ansicht: %q is no colour", color)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.read()
	if err != nil {
		return err
	}
	o := all[person]
	if hide != nil {
		o.Hidden = slices.DeleteFunc(o.Hidden, func(p string) bool { return p == path })
		if *hide {
			o.Hidden = append(o.Hidden, path)
		}
	}
	switch color {
	case "":
	case "-":
		delete(o.Colors, path)
	default:
		if o.Colors == nil {
			o.Colors = map[string]string{}
		}
		o.Colors[path] = color
	}
	all[person] = o
	b, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), "."+File+"-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.path)
}
