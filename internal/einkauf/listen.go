package einkauf

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/mdopp/haus/internal/caldav"
	"github.com/mdopp/haus/internal/notes"
)

// MeineName is a person's list made when they say "auf meine Liste" and
// have none yet.
const MeineName = "Meine Liste"

// mine are the words for one's own list.
var mine = map[string]bool{"meine": true, "meine liste": true, "mir": true, "mich": true, "persönlich": true, "meine einkaufsliste": true, "privat": true}

// Waehle is the list someone names (haus#119): "" or "Einkauf" the
// household's; "meine" their own — the one they have, else a new
// "Meine Liste"; else a list by its name, the household's or their own,
// whole name first. It says whether it made one.
func (l *Liste) Waehle(ctx context.Context, person, name string) (*Liste, bool, error) {
	want := strings.ToLower(strings.TrimSpace(name))
	want = strings.TrimSuffix(strings.TrimPrefix(want, "die "), "-liste")
	if want == "" || want == "einkauf" || want == "einkaufsliste" || want == "haushalt" {
		return l.Andere(caldav.Einkauf), false, nil
	}
	lists, err := l.DAV.Lists(ctx, person)
	if err != nil {
		return nil, false, err
	}
	if mine[want] {
		if person == "" {
			return nil, false, errors.New("Eine eigene Liste geht nur mit Namen: Wer schreibt?")
		}
		var own []caldav.List
		for _, x := range lists {
			if x.Ref.Personal() {
				own = append(own, x)
			}
		}
		switch len(own) {
		case 0:
			r, err := l.DAV.MakeList(ctx, person, notes.Slug(MeineName), MeineName)
			if err != nil && !errors.Is(err, caldav.ErrListExists) {
				return nil, false, err
			}
			return l.Andere(r), err == nil, nil
		case 1:
			return l.Andere(own[0].Ref), false, nil
		}
		return nil, false, fmt.Errorf("Du hast mehrere eigene Listen: %s. Welche?", names(own))
	}
	for _, exact := range []bool{true, false} {
		for _, x := range lists {
			n := strings.ToLower(x.Name)
			if (exact && n == want) || (!exact && strings.Contains(n, want)) {
				return l.Andere(x.Ref), false, nil
			}
		}
	}
	return nil, false, fmt.Errorf("Keine Liste heißt „%s“. Es gibt: %s.", strings.TrimSpace(name), names(lists))
}

func names(ls []caldav.List) string {
	var n []string
	for _, x := range ls {
		n = append(n, x.Name)
	}
	return strings.Join(n, ", ")
}
