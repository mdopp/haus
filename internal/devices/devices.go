// Package devices is Haus's own pairing: a phone gets a bearer with the prefix
// the app expects (legacy.TokenPrefix), stored hashed in one JSON file. Pairing is a
// page behind Authelia; the person who pairs is the device's uid.
package devices

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mdopp/haus/internal/legacy"
)

// Device is one paired phone.
type Device struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	UID      string    `json:"uid"` // Remote-User who paired it
	Hash     string    `json:"hash"`
	Created  time.Time `json:"created"`
	LastSeen time.Time `json:"lastSeen,omitempty"`
}

// Store is the file-backed set.
type Store struct {
	path string
	mu   sync.Mutex
	list []Device
}

// Open loads (or starts) the store at dir/devices.json.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &Store{path: filepath.Join(dir, "devices.json")}
	b, err := os.ReadFile(s.path)
	if err == nil {
		if err := json.Unmarshal(b, &s.list); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return s, nil
}

func (s *Store) save() error {
	b, _ := json.MarshalIndent(s.list, "", "  ")
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func hash(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// Mint creates a device and returns the one-time plaintext token.
func (s *Store) Mint(name, uid string) (Device, string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return Device{}, "", err
	}
	token := legacy.TokenPrefix + base64.RawURLEncoding.EncodeToString(raw)
	id := make([]byte, 6)
	rand.Read(id)
	d := Device{ID: hex.EncodeToString(id), Name: strings.TrimSpace(name), UID: uid, Hash: hash(token), Created: time.Now().UTC()}
	if d.Name == "" {
		d.Name = "Telefon"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.list = append(s.list, d)
	return d, token, s.save()
}

// Lookup returns the device behind a bearer and notes the sighting.
func (s *Store) Lookup(bearer string) (Device, bool) {
	h := hash(bearer)
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.list {
		if s.list[i].Hash == h {
			if time.Since(s.list[i].LastSeen) > time.Hour {
				s.list[i].LastSeen = time.Now().UTC()
				s.save()
			}
			return s.list[i], true
		}
	}
	return Device{}, false
}

// Revoke forgets a device.
func (s *Store) Revoke(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.list {
		if s.list[i].ID == id {
			s.list = append(s.list[:i], s.list[i+1:]...)
			return s.save()
		}
	}
	return errors.New("no such device")
}

// List is every device, newest first.
func (s *Store) List() []Device {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]Device(nil), s.list...)
	sort.Slice(out, func(i, j int) bool { return out[i].Created.After(out[j].Created) })
	return out
}
