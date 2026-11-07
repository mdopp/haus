package gesichter

import (
	"cmp"
	"context"
	"errors"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mdopp/homes/internal/immich"
)

// What the owner sees beside a face on „Dieselbe Person?“ (owner
// 2026-11-07), so a pair is decided without leaving the page: where the
// person's photos were taken and in which years, and who else is in them.
// It is read from the person's newest umfeldFotos photos once right
// after Homes starts, so a deploy fills it, and then once a day
// (umfeldTakt; owner 2026-11-07), not in every hourly run; a page never
// asks Immich.

const (
	umfeldFotos = 100            // the newest photos read per person
	maxUmfeld   = 500            // persons read in one run
	umfeldN     = 3              // places and companions kept
	umfeldTakt  = 24 * time.Hour // how often they are read
)

// umfeld is one person's places, years and company.
type umfeld struct {
	Orte   []string  `json:"orte,omitempty"` // the most frequent first: the city, else the country
	Von    int       `json:"von,omitempty"`  // the years of the photos; 0 unknown
	Bis    int       `json:"bis,omitempty"`
	Mit    []string  `json:"mit,omitempty"` // the named persons most often in the same photos
	Geholt time.Time `json:"geholt"`
}

// Umfelder reads the places and company of every person the pairs and
// questions show, at most maxUmfeld: on its first call, then when
// umfeldTakt has passed since the last run that went through; else it
// does nothing. Those no longer shown are dropped. Without asset.read it
// reads nothing.
func (g *Gesichter) Umfelder(ctx context.Context) error {
	if g == nil || g.Immich == nil {
		return nil
	}
	g.mu.Lock()
	faellig := g.umfeldLauf.IsZero() || g.now().Sub(g.umfeldLauf) >= umfeldTakt
	g.mu.Unlock()
	if !faellig {
		return nil
	}
	gezeigt := map[string]bool{}
	for _, p := range g.Paare() {
		gezeigt[p.A.ID], gezeigt[p.B.ID] = true, true
	}
	for _, f := range g.Fragen() {
		gezeigt[f.Person.ID] = true
	}
	g.mu.Lock()
	maps.DeleteFunc(g.stand.Umfeld, func(id string, _ umfeld) bool { return !gezeigt[id] })
	darf := g.darf("asset.read")
	g.mu.Unlock()
	var err error
	n := 0
	for _, id := range slices.Sorted(maps.Keys(gezeigt)) {
		if !darf || n >= maxUmfeld {
			break
		}
		n++
		var fotos []immich.Foto
		if fotos, err = g.Immich.Umfeld(ctx, id, umfeldFotos); err != nil {
			break
		}
		u := zusammenfassen(id, fotos)
		u.Geholt = g.now()
		g.mu.Lock()
		g.stand.Umfeld[id] = u
		g.mu.Unlock()
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	var fehlt immich.FehltRecht
	if errors.As(err, &fehlt) {
		err = nil // Fehlt names it: asset.read is in immich.Braucht
	}
	if err == nil {
		g.umfeldLauf = g.now()
	}
	return errors.Join(err, g.save())
}

// zusammenfassen is what id's photos say: the umfeldN most frequent
// places and named companions, and the span of years.
func zusammenfassen(id string, fotos []immich.Foto) umfeld {
	orte, mit := map[string]int{}, map[string]int{}
	var u umfeld
	for _, f := range fotos {
		if ort := strings.TrimSpace(cmp.Or(strings.TrimSpace(f.Exif.Stadt), f.Exif.Land)); ort != "" {
			orte[ort]++
		}
		if j, err := strconv.Atoi(f.Wann[:min(4, len(f.Wann))]); err == nil && j > 0 {
			u.Von, u.Bis = cmp.Or(min(u.Von, j), j), max(u.Bis, j)
		}
		for _, p := range f.Personen {
			if name := strings.TrimSpace(p.Name); name != "" && p.ID != id {
				mit[name]++
			}
		}
	}
	return umfeld{Orte: haeufigste(orte), Von: u.Von, Bis: u.Bis, Mit: haeufigste(mit)}
}

// haeufigste are the umfeldN keys of n counted most, a tie by name.
func haeufigste(n map[string]int) []string {
	keys := slices.SortedFunc(maps.Keys(n), func(a, b string) int { return cmp.Or(n[b]-n[a], strings.Compare(a, b)) })
	return keys[:min(len(keys), umfeldN)]
}
