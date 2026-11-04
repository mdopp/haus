// Package texte holds Homes' family-visible words by a stable id, one
// table per language (owner 2026-11-04: Homes becomes multilingual — de,
// en, es, fr — after the module work). Only German is filled today, and
// only the words added since then live here: the module titles, the
// traces of what a box lacks and „Hier anfordern“ (ADR 0001 §10), each
// module's setup steps and the person's tasks. Older strings move here in
// that later project. The words are in katalog.go (not counted by
// scripts/loc.sh); the logic here is. Standard library only (shared).
package texte

import (
	"strconv"
	"strings"
)

// ID is a word's stable key: the app and other languages refer to it.
type ID string

// sprachen are the tables by language; a missing word falls back to German.
var sprachen = map[string]map[ID]string{"de": de}

// T is a word in German.
func T(id ID) string { return In("de", id) }

// In is a word in a language; German when that language lacks it, and
// the id itself when German does too (a test holds every id to German).
func In(lang string, id ID) string {
	if s, ok := sprachen[lang][id]; ok {
		return s
	}
	if s, ok := de[id]; ok {
		return s
	}
	return string(id)
}

// Titel is a module's title.
func Titel(modul string) string { return T(ID("modul." + modul)) }

// Anzahl is "1 Datei", "12 Dateien".
func Anzahl(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return Tausender(int64(n)) + " " + many
}

// Tausender is n with the German thousands point: 12.345.
func Tausender(n int64) string {
	s := strconv.FormatInt(n, 10)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "." + s[i:]
	}
	return s
}

// Groesse is a size as the family reads it: "3,2 GB", "450 MB", "12 KB".
func Groesse(n int64) string {
	units := []struct {
		size int64
		name string
	}{{1 << 40, "TB"}, {1 << 30, "GB"}, {1 << 20, "MB"}, {1 << 10, "KB"}}
	for _, u := range units {
		if n >= u.size {
			v := float64(n) / float64(u.size)
			if v >= 10 {
				return Tausender(int64(v+0.5)) + " " + u.name
			}
			return strings.Replace(strconv.FormatFloat(v, 'f', 1, 64), ".", ",", 1) + " " + u.name
		}
	}
	return Tausender(n) + " Bytes"
}
