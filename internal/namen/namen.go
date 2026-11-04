// Package namen compares names as a person means them: a song, an artist,
// a film or a book written one way in Jellyfin, another in MusicBrainz or
// on the shopping list. It was namen.Plain; the music and the media
// packages share it, so it is no connector's.
package namen

import (
	"strings"
	"unicode"
)

// Plain is a name compared: lower case, letters and digits only, umlauts
// as their base letter.
func Plain(s string) string {
	r := strings.NewReplacer("ä", "a", "ö", "o", "ü", "u", "ß", "ss", "é", "e", "è", "e", "á", "a", "à", "a", "ó", "o", "í", "i", "&", "und")
	s = r.Replace(strings.ToLower(s))
	var b strings.Builder
	for _, c := range s {
		if unicode.IsLetter(c) || unicode.IsDigit(c) {
			b.WriteRune(c)
		}
	}
	return b.String()
}
