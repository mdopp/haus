package gesichter

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/mdopp/homes/internal/immich"
	"github.com/mdopp/homes/internal/namen"
)

// Two persons in Immich, one human (owner 2026-11-06). Immich's web app
// offers a merge only while a name is typed there, and a „No“ keeps the
// twin; Immich's workflows cannot touch persons. So Homes does two things
// with the key's owner's persons, through Immich's API only:
//
//   - Zusammenfuehren, in the hourly run: the named, unhidden persons of
//     one name (namen.Plain) become the one with the most photos.
//   - „Dieselbe Person?“: for each named person, the kandidaten persons
//     whose face Immich holds closest (closestPersonId: an order, no
//     distance), asked in the background (Vorschlagen), shown as pairs
//     (Paare), answered by the owner (Antworten). A „Nein“ is kept.

const (
	maxVerschmelzen = 50 // persons merged away in one run
	maxNaehe        = 40 // persons asked for their closest in one run
	maxNachladen    = 80 // faces and photo counts fetched in one run
	kandidaten      = 3  // the closest taken per person
	// naeheTTL is how long a person's closest are kept before asking again.
	naeheTTL = 24 * time.Hour
)

// naehe is a person's closest others, closest first.
type naehe struct {
	Kandidaten []string  `json:"kandidaten"`
	Geholt     time.Time `json:"geholt"`
}

// zahl is how many photos show a person.
type zahl struct {
	N      int       `json:"n"`
	Geholt time.Time `json:"geholt"`
}

// ErrKeinPaar is an answer to a pair not offered (any more).
var ErrKeinPaar = errors.New("gesichter: no such pair")

// ErrNichtZusammen is Immich merging neither of the two: one is gone.
var ErrNichtZusammen = errors.New("gesichter: immich merged nothing")

// Zusammenfuehren merges the key owner's named, unhidden persons of one
// name into the one with the most photos (person.statistics; without it
// Immich's own order of faces), on a tie the one a contact has; at most
// maxVerschmelzen a run, the rest next time, one German line in the log.
// The contacts' lines follow. Without person.merge it does nothing, and
// Fehlt says so.
func (g *Gesichter) Zusammenfuehren(ctx context.Context) error {
	if g == nil || g.Immich == nil {
		return nil
	}
	g.mu.Lock()
	g.laden()
	liste, zu, darf := slices.Clone(g.stand.Personen), slices.Collect(maps.Values(g.stand.Zuordnung)), g.darf("person.merge")
	if !darf {
		g.stand.FehltMerge = "person.merge"
	}
	g.mu.Unlock()
	if !darf {
		return nil
	}
	gruppen, rang, schreib := map[string][]string{}, map[string]int{}, map[string]string{}
	for i, p := range liste {
		if k := namen.Plain(p.Name); k != "" {
			gruppen[k], rang[p.ID], schreib[k] = append(gruppen[k], p.ID), len(liste)-i, cmp.Or(schreib[k], strings.Join(strings.Fields(p.Name), " "))
		}
	}
	zugeordnet, verdacht := map[string]bool{}, map[string]bool{}
	for k, ids := range gruppen {
		verdacht[k] = len(ids) > 1
	}
	for _, z := range zu {
		zugeordnet[z.ID] = true
		if k := namen.Plain(z.Name); len(gruppen[k]) > 0 && !slices.Contains(gruppen[k], z.ID) {
			verdacht[k] = true // a person Homes made, no face yet so not in the list, beside one of its name
		}
	}
	var gemacht []string
	n, err := 0, error(nil)
	for _, k := range slices.Sorted(maps.Keys(gruppen)) {
		if !verdacht[k] || n >= maxVerschmelzen || err != nil {
			continue
		}
		var ids, weg []string
		if ids, err = g.gleichnamige(ctx, schreib[k], gruppen[k]); err != nil || len(ids) < 2 {
			continue
		}
		ziel := g.ziel(ctx, ids, rang, zugeordnet)
		quellen := slices.DeleteFunc(ids, func(id string) bool { return id == ziel })
		weg, err = g.verschmelzen(ctx, ziel, quellen[:min(len(quellen), maxVerschmelzen-n)])
		if n += len(weg); len(weg) > 0 {
			gemacht = append(gemacht, fmt.Sprintf("%s (%d zu 1)", schreib[k], len(weg)+1))
		}
	}
	if len(gemacht) > 0 {
		rest := ""
		if n >= maxVerschmelzen {
			rest = "; weitere in der nächsten Stunde"
		}
		log.Printf("homes: Immich: gleichnamige Personen zusammengeführt: %s%s", strings.Join(gemacht, ", "), rest)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	var fehlt immich.FehltRecht
	if errors.As(err, &fehlt) && fehlt.Recht == "person.merge" {
		g.stand.FehltMerge, err = fehlt.Recht, nil
	} else if err == nil {
		g.stand.FehltMerge = ""
	}
	return errors.Join(err, g.save())
}

// gleichnamige are the persons of the list's ids and the ones of name
// Immich's search finds, faceless ones too: the same name (namen.Plain),
// none hidden.
func (g *Gesichter) gleichnamige(ctx context.Context, name string, ids []string) ([]string, error) {
	ps, err := g.Immich.Suchen(ctx, name)
	for _, p := range ps {
		if namen.Plain(p.Name) == namen.Plain(name) && !p.Versteckt && !slices.Contains(ids, p.ID) {
			ids = append(ids, p.ID)
		}
	}
	return ids, err
}

// ziel is the person of ids the others merge into: the most photos
// (uncounted: the first in Immich's order, rang, higher is more faces,
// faceless 0), else one a contact has, else the least id.
func (g *Gesichter) ziel(ctx context.Context, ids []string, rang map[string]int, zugeordnet map[string]bool) string {
	fotos, ok := g.zaehlen(ctx, ids)
	return slices.MaxFunc(ids, func(a, b string) int {
		c := cmp.Compare(rang[a], rang[b])
		if ok {
			c = cmp.Compare(fotos[a], fotos[b])
		}
		return cmp.Or(c, cmp.Compare(btoi(zugeordnet[a]), btoi(zugeordnet[b])), strings.Compare(b, a))
	})
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// zaehlen are the photos of each of ids, kept for BildTTL; ok: all counted.
func (g *Gesichter) zaehlen(ctx context.Context, ids []string) (map[string]int, bool) {
	out := map[string]int{}
	for _, id := range ids {
		g.mu.Lock()
		z, da := g.stand.Fotos[id]
		darf := g.darf("person.statistics")
		g.mu.Unlock()
		if !da || g.now().Sub(z.Geholt) > BildTTL {
			n, err := 0, error(nil)
			if darf {
				n, err = g.Immich.Fotos(ctx, id)
			}
			if !darf || err != nil {
				return out, false
			}
			z = zahl{n, g.now()}
			g.mu.Lock()
			g.stand.Fotos[id] = z
			g.mu.Unlock()
		}
		out[id] = z.N
	}
	return out, true
}

// verschmelzen merges quellen into ziel in Immich and follows: a contact's
// line to a person merged away points to ziel (Homes did not make ziel for
// it, so it keeps ziel's name), and what Homes kept of those persons goes.
func (g *Gesichter) verschmelzen(ctx context.Context, ziel string, quellen []string) ([]string, error) {
	weg, err := g.Immich.Zusammenfuehren(ctx, ziel, quellen)
	g.mu.Lock()
	defer g.mu.Unlock()
	g.laden()
	st := &g.stand
	for k, z := range st.Zuordnung {
		if slices.Contains(weg, z.ID) {
			st.Zuordnung[k] = zuordnung{ID: ziel, Name: z.Name}
		}
	}
	gegangen := func(id string) bool { return slices.Contains(weg, id) }
	st.Personen = slices.DeleteFunc(st.Personen, func(p immich.Person) bool { return gegangen(p.ID) })
	for _, id := range append(weg, ziel) {
		delete(st.Naehe, id)
		delete(st.Fotos, id)
		if id != ziel {
			delete(st.Andere, id)
			delete(st.Bilder, id)
		}
	}
	for id, n := range st.Naehe {
		n.Kandidaten = slices.DeleteFunc(n.Kandidaten, gegangen)
		st.Naehe[id] = n
	}
	if len(weg) > 0 {
		err = errors.Join(err, g.save())
	}
	return weg, err
}

// Vorschlagen asks Immich, for at most maxNaehe named persons not asked
// for naeheTTL, which kandidaten faces are closest, and fetches the faces
// and photo counts the pairs show, at most maxNachladen.
func (g *Gesichter) Vorschlagen(ctx context.Context) error {
	if g == nil || g.Immich == nil {
		return nil
	}
	g.mu.Lock()
	g.laden()
	liste, alt := slices.Clone(g.stand.Personen), maps.Clone(g.stand.Naehe)
	g.mu.Unlock()
	favoriten := 0
	for _, p := range liste {
		favoriten += btoi(p.Favorit)
	}
	slices.SortStableFunc(liste, func(a, b immich.Person) int { return alt[a.ID].Geholt.Compare(alt[b.ID].Geholt) })
	neu, andere, gefragt := map[string]naehe{}, map[string]immich.Person{}, 0
	var err error
	for _, p := range liste {
		if gefragt >= maxNaehe || g.now().Sub(alt[p.ID].Geholt) < naeheTTL {
			break
		}
		gefragt++
		var ps []immich.Person
		// Immich puts the favourites first whatever their face: read past them, and take none
		if ps, err = g.Immich.Naechste(ctx, p.ID, favoriten+kandidaten+1); err != nil {
			break
		}
		n := naehe{Geholt: g.now()}
		for _, q := range ps {
			if q.ID != p.ID && !q.Favorit && len(n.Kandidaten) < kandidaten {
				n.Kandidaten, andere[q.ID] = append(n.Kandidaten, q.ID), q
			}
		}
		neu[p.ID] = n
	}
	g.mu.Lock()
	st := &g.stand
	maps.Copy(st.Naehe, neu)
	maps.Copy(st.Andere, andere)
	named := map[string]bool{}
	for _, p := range st.Personen {
		named[p.ID] = true
	}
	maps.DeleteFunc(st.Naehe, func(id string, _ naehe) bool { return !named[id] })
	gezeigt := map[string]bool{}
	for id, n := range st.Naehe {
		gezeigt[id] = true
		for _, k := range n.Kandidaten {
			gezeigt[k] = true
		}
	}
	maps.DeleteFunc(st.Andere, func(id string, _ immich.Person) bool { return !gezeigt[id] })
	fehlen := slices.Sorted(maps.Keys(gezeigt))
	g.mu.Unlock()
	g.nachladen(ctx, fehlen)
	g.mu.Lock()
	defer g.mu.Unlock()
	return errors.Join(err, g.save())
}

// nachladen fetches the faces of the unnamed persons in ids that have
// none kept, and the photo counts not kept, at most maxNachladen calls.
func (g *Gesichter) nachladen(ctx context.Context, ids []string) {
	aufrufe := 0
	for _, id := range ids {
		g.mu.Lock()
		p, anders := g.stand.Andere[id]
		b, hat := g.stand.Bilder[id]
		g.mu.Unlock()
		if anders && (!hat || b.Stand != p.Stand) && aufrufe < maxNachladen {
			aufrufe++
			if roh, err := g.Immich.Gesicht(ctx, id); err == nil {
				if klein, err := Klein(roh, Kante); err == nil {
					g.mu.Lock()
					g.stand.Bilder[id] = bild{JPEG: klein, Stand: p.Stand, Geholt: g.now()}
					g.mu.Unlock()
				}
			}
		}
	}
	for _, id := range ids {
		g.mu.Lock()
		z, da := g.stand.Fotos[id]
		g.mu.Unlock()
		if da && g.now().Sub(z.Geholt) <= BildTTL {
			continue
		}
		if aufrufe++; aufrufe > maxNachladen {
			return
		}
		if _, ok := g.zaehlen(ctx, []string{id}); !ok {
			return
		}
	}
}

// Seite is one person of a pair, as the page shows it.
type Seite struct {
	ID, Name string // Name "": without a name
	Fotos    int    // -1: not counted
	Bild     []byte // the small face; nil none
}

// Paar is two persons Immich holds close: A a named one, B one of its
// closest.
type Paar struct{ A, B Seite }

// Key is the pair's name in Nein and Spaeter, the same either way round.
func (p Paar) Key() string { return paarKey(p.A.ID, p.B.ID) }

func paarKey(a, b string) string {
	return min(a, b) + "|" + max(a, b)
}

// Paare are the pairs to ask about, the likeliest first: closest, more so
// when each is among the other's closest, an unnamed face before a named
// person; the skipped last, a „Nein“ never.
func (g *Gesichter) Paare() []Paar {
	if g == nil || g.Immich == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.laden()
	st := g.stand
	wer := maps.Clone(st.Andere)
	for _, p := range st.Personen {
		wer[p.ID] = p
	}
	seite := func(id string) Seite {
		n := -1
		if z, ok := st.Fotos[id]; ok {
			n = z.N
		}
		return Seite{id, strings.TrimSpace(wer[id].Name), n, st.Bilder[id].JPEG}
	}
	type kandidat struct {
		Paar
		wert int
	}
	gesehen := map[string]bool{}
	var out []kandidat
	for _, a := range slices.Sorted(maps.Keys(st.Naehe)) {
		for i, b := range st.Naehe[a].Kandidaten {
			key := paarKey(a, b)
			if _, nein := st.Nein[key]; nein || gesehen[key] || wer[b].ID == "" || wer[a].ID == "" || a == b {
				continue
			}
			gesehen[key] = true
			wert := 2 * (i + 1)
			if slices.Contains(st.Naehe[b].Kandidaten, a) {
				wert -= 2
			}
			if strings.TrimSpace(wer[b].Name) == "" {
				wert--
			}
			out = append(out, kandidat{Paar{seite(a), seite(b)}, wert})
		}
	}
	slices.SortStableFunc(out, func(x, y kandidat) int {
		return cmp.Or(st.Spaeter[x.Key()].Compare(st.Spaeter[y.Key()]), cmp.Compare(x.wert, y.wert))
	})
	paare := make([]Paar, len(out))
	for i, k := range out {
		paare[i] = k.Paar
	}
	return paare
}

// Antworten takes the owner's answer to the pair a, b: "ja" merges B into
// A when only A has a name, else the one with fewer photos into the other
// (A when not counted), and answers the name kept; "nein" keeps the pair
// from coming back; "spaeter" puts it last.
func (g *Gesichter) Antworten(ctx context.Context, a, b, antwort string) (string, error) {
	paare := g.Paare()
	i := slices.IndexFunc(paare, func(p Paar) bool { return p.Key() == paarKey(a, b) })
	if i < 0 {
		return "", ErrKeinPaar
	}
	p := paare[i]
	g.mu.Lock()
	switch antwort {
	case "nein", "spaeter":
		m := map[string]map[string]time.Time{"nein": g.stand.Nein, "spaeter": g.stand.Spaeter}[antwort]
		m[p.Key()] = g.now()
		defer g.mu.Unlock()
		return "", g.save()
	}
	g.mu.Unlock()
	if antwort != "ja" {
		return "", fmt.Errorf("gesichter: no answer %q", antwort)
	}
	ziel, quelle := p.A, p.B
	if quelle.Name != "" && quelle.Fotos > ziel.Fotos && ziel.Fotos >= 0 {
		ziel, quelle = quelle, ziel
	}
	weg, err := g.verschmelzen(ctx, ziel.ID, []string{quelle.ID})
	var fehlt immich.FehltRecht
	g.mu.Lock()
	defer g.mu.Unlock()
	switch {
	case errors.As(err, &fehlt):
		g.stand.FehltMerge = fehlt.Recht
	case err == nil && len(weg) == 0: // one of them is gone: ask Immich again
		delete(g.stand.Naehe, a)
		delete(g.stand.Naehe, b)
		err = ErrNichtZusammen
	default:
		return cmp.Or(ziel.Name, quelle.Name), err
	}
	return "", errors.Join(err, g.save())
}
