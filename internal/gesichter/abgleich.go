package gesichter

import (
	"context"
	"errors"
	"net/url"
	"slices"
	"strings"

	"github.com/mdopp/homes/internal/caldav"
	"github.com/mdopp/homes/internal/immich"
	"github.com/mdopp/homes/internal/namen"
)

// kontakt is one contact as Immich gets it.
type kontakt struct{ key, name, geburtstag string }

// kontakte are the Haushalts-Kontakte and the key owner's own
// Handy-Kontakte (once Merke knows the owner's login), firms aside; ok:
// both were read whole, so a contact not among them is gone.
func (g *Gesichter) kontakte(ctx context.Context) (out []kontakt, ok bool, err error) {
	all, partial, err := g.Kontakte.Contacts(ctx)
	if err != nil {
		return nil, false, err
	}
	add := func(prefix string, cts []caldav.Contact) {
		for _, ct := range cts {
			if name := strings.Join(strings.Fields(ct.Name), " "); name != "" && ct.UID != "" && !ct.IsFirm() {
				out = append(out, kontakt{prefix + ct.UID, name, geburtstag(ct.Birthday, g.now().Format("2006-01-02"))})
			}
		}
	}
	add("haushalt/", all)
	if login := g.Wessen(); login != "" {
		cards, _, err := g.Kontakte.BookCards(ctx, "/"+url.PathEscape(login)+"/handy-kontakte/")
		if err != nil {
			return nil, false, err
		}
		for _, cd := range cards {
			if root, err := caldav.Parse(cd.Data); err == nil {
				add("privat/", caldav.Contacts(root))
			}
		}
	}
	slices.SortFunc(out, func(a, b kontakt) int { return strings.Compare(a.key, b.key) })
	return out, !partial, nil
}

// geburtstag is a birthday as Immich takes it: a whole date, not after
// heute; "" otherwise.
func geburtstag(b, heute string) string {
	if strings.HasPrefix(b, "--") || !caldav.ValidBirthday(b) || b > heute {
		return ""
	}
	return b
}

// Abgleichen gives every contact an Immich person of its name, at most
// maxAufrufe calls a run (the rest in the next): the one it has (a person
// Homes made follows the contact's name and birthday), else one Immich
// has of that name, else a new one. Nothing else in Immich changes; a
// contact gone loses only its line in Zuordnung.
func (g *Gesichter) Abgleichen(ctx context.Context) error {
	if g == nil || g.Immich == nil || g.Kontakte == nil {
		return nil
	}
	ks, ganz, err := g.kontakte(ctx)
	if err != nil {
		return err
	}
	g.mu.Lock()
	g.laden()
	alt := g.stand.Zuordnung
	g.mu.Unlock()
	neu, aufrufe := map[string]zuordnung{}, 0
	for _, k := range ks {
		z := alt[k.key]
		if aufrufe < maxAufrufe && err == nil {
			var n int
			z, n, err = g.zuordnen(ctx, k, z, neu)
			aufrufe += n
		}
		if z.ID != "" {
			neu[k.key] = z
		}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for key, z := range alt {
		if _, da := neu[key]; !da && (!ganz || err != nil) { // not read, or the run broke off: kept
			neu[key] = z
		}
	}
	var fehlt immich.FehltRecht
	errors.As(err, &fehlt)
	g.stand.Zuordnung, g.stand.Fehlt = neu, fehlt.Recht
	if serr := g.save(); err == nil {
		err = serr
	}
	return err
}

// zuordnen is k's person and how many calls it took: z kept or followed,
// else one of k's name made in this run or in Immich, else a new one.
func (g *Gesichter) zuordnen(ctx context.Context, k kontakt, z zuordnung, neu map[string]zuordnung) (zuordnung, int, error) {
	n := 0
	switch {
	case z.ID != "" && z.Eigen && (z.Name != k.name || z.Geburtstag != k.geburtstag):
		n++
		err := g.Immich.Aendern(ctx, z.ID, k.name, k.geburtstag)
		var a immich.Antwort
		if err == nil {
			return zuordnung{z.ID, k.name, k.geburtstag, true}, n, nil
		}
		if !errors.As(err, &a) || a.Code != 400 { // 400: merged away or deleted in Immich
			return z, n, err
		}
	case z.ID != "" && (z.Eigen || namen.Plain(z.Name) == namen.Plain(k.name)):
		return z, n, nil
	}
	for _, d := range neu {
		if namen.Plain(d.Name) == namen.Plain(k.name) {
			return zuordnung{ID: d.ID, Name: k.name}, n, nil
		}
	}
	n++
	ps, err := g.Immich.Suchen(ctx, k.name)
	if err != nil {
		return zuordnung{}, n, err
	}
	for _, p := range ps {
		if namen.Plain(p.Name) == namen.Plain(k.name) {
			return zuordnung{ID: p.ID, Name: k.name}, n, nil
		}
	}
	n++
	p, err := g.Immich.Anlegen(ctx, k.name, k.geburtstag)
	return zuordnung{p.ID, k.name, k.geburtstag, true}, n, err
}
