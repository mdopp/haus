// Package papierkorb keeps what the family deleted for 30 days (haus#174:
// „calm, plain and undoable“, laib#60): a list with its lines, a calendar
// with its appointments, an upload beets kept back. Each entry is one file
// in Haus's data folder, 0600: its kind, its name as the family knows it,
// when and by whom, and what it takes to put it back. Older entries go by
// themselves when the store is read.
package papierkorb

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

// Frist is how long a deleted thing can be put back.
const Frist = 30 * 24 * time.Hour

// Eintrag is one deleted thing.
type Eintrag struct {
	ID    string          `json:"id"`
	Art   string          `json:"art"`  // liste | kalender | musik
	Name  string          `json:"name"` // as the family knows it
	Wer   string          `json:"wer"`
	Am    time.Time       `json:"am"`
	Daten json.RawMessage `json:"daten"` // what the kind needs to put it back
}

// Bis is the last day it can be put back.
func (e Eintrag) Bis() time.Time { return e.Am.Add(Frist) }

// Korb is the store: a folder.
type Korb struct {
	Dir string
	Now func() time.Time
}

// ErrWeg is an entry that is not (or no longer) there.
var ErrWeg = errors.New("Das liegt nicht mehr im Papierkorb: es ist schon zurück oder älter als 30 Tage.")

var idRE = regexp.MustCompile(`^[0-9a-f]{16}$`)

func (k *Korb) now() time.Time {
	if k.Now != nil {
		return k.Now()
	}
	return time.Now()
}

func (k *Korb) dir() string { return filepath.Join(k.Dir, "papierkorb") }

// Rein keeps a deleted thing; its entry.
func (k *Korb) Rein(art, name, wer string, daten any) (Eintrag, error) {
	b := make([]byte, 8)
	rand.Read(b)
	raw, err := json.Marshal(daten)
	if err != nil {
		return Eintrag{}, err
	}
	e := Eintrag{ID: hex.EncodeToString(b), Art: art, Name: name, Wer: wer, Am: k.now().UTC(), Daten: raw}
	if err := os.MkdirAll(k.dir(), 0o700); err != nil {
		return Eintrag{}, err
	}
	out, _ := json.Marshal(e)
	tmp := filepath.Join(k.dir(), "."+e.ID+".neu")
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return Eintrag{}, err
	}
	return e, os.Rename(tmp, filepath.Join(k.dir(), e.ID+".json"))
}

// Liste is the entries of a kind ("" for all), newest first; older than Frist go.
func (k *Korb) Liste(art string) []Eintrag {
	es, _ := os.ReadDir(k.dir())
	var out []Eintrag
	for _, f := range es {
		if filepath.Ext(f.Name()) != ".json" {
			continue
		}
		e, err := k.lies(f.Name()[:len(f.Name())-5])
		if err != nil {
			continue
		}
		if k.now().After(e.Bis()) {
			os.Remove(filepath.Join(k.dir(), f.Name()))
			continue
		}
		if art == "" || e.Art == art {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Am.After(out[j].Am) })
	return out
}

func (k *Korb) lies(id string) (Eintrag, error) {
	if !idRE.MatchString(id) {
		return Eintrag{}, ErrWeg
	}
	b, err := os.ReadFile(filepath.Join(k.dir(), id+".json"))
	if err != nil {
		return Eintrag{}, ErrWeg
	}
	var e Eintrag
	if json.Unmarshal(b, &e) != nil {
		return Eintrag{}, ErrWeg
	}
	return e, nil
}

// Holen is an entry still there (within Frist) of that kind.
func (k *Korb) Holen(art, id string) (Eintrag, error) {
	e, err := k.lies(id)
	if err != nil || e.Art != art || k.now().After(e.Bis()) {
		return Eintrag{}, ErrWeg
	}
	return e, nil
}

// Raus removes an entry once it is back.
func (k *Korb) Raus(id string) {
	if idRE.MatchString(id) {
		os.Remove(filepath.Join(k.dir(), id+".json"))
	}
}
