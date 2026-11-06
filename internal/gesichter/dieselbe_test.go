package gesichter

import (
	"errors"
	"fmt"
	"image/color"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/homes/internal/immich"
	"github.com/mdopp/homes/internal/immich/immichfake"
)

// zwillinge: Immich has Anna Müller twice (10 and 30 photos) and once
// hidden, „Anna Mueller“ (another name to Homes), Ben and Bea, an unnamed
// face, two Doras of 4 photos each (a contact has the second), and
// Carla Berg, named by the owner on a face (5 photos), beside the
// faceless Carla Berg Homes made for the contact c.
func zwillinge(t *testing.T) (*Gesichter, *immichfake.Server) {
	t.Helper()
	fake := immichfake.New("key")
	t.Cleanup(fake.Close)
	for _, p := range []struct {
		id, name string
		fotos    int
		hidden   bool
	}{{"a1", "Anna Müller", 10, false}, {"a2", "anna  MÜLLER", 30, false}, {"a3", "Anna Müller", 99, true}, {"a4", "Anna Mueller", 50, false},
		{"b1", "Ben", 3, false}, {"b2", "Bea", 3, false}, {"u1", "", 40, false}, {"d1", "Dora", 4, false}, {"d2", "Dora", 4, false}, {"c1", "Carla Berg", 5, false}} {
		fake.Person(p.id, p.name, "1", p.hidden, nil)
		fake.Lage(p.id, p.fotos, 0, 0, false)
	}
	g := &Gesichter{Immich: &immich.Client{URL: fake.URL, Key: "key"}, Web: "https://photos.box.test", Dir: t.TempDir(),
		Now: func() time.Time { return time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC) }}
	carla, err := g.Immich.Anlegen(t.Context(), "Carla Berg", "1990-04-12")
	if err != nil {
		t.Fatal(err)
	}
	g.mu.Lock()
	g.laden()
	g.stand.Zuordnung["haushalt/c"] = zuordnung{carla.ID, "Carla Berg", "1990-04-12", true}
	g.stand.Zuordnung["privat/d"] = zuordnung{ID: "d2", Name: "Dora"}
	g.stand.Zuordnung["haushalt/a"] = zuordnung{ID: "a1", Name: "Anna Müller"}
	g.mu.Unlock()
	if err := g.Auffrischen(t.Context()); err != nil {
		t.Fatal(err)
	}
	return g, fake
}

// The same name merges into the one with the most photos, on a tie into
// the one a contact has; a hidden one, another name and an unnamed face
// never; the contacts follow; a second run merges nothing.
func TestZusammenfuehren(t *testing.T) {
	g, fake := zwillinge(t)
	carla := g.stand.Zuordnung["haushalt/c"].ID
	if err := g.Zusammenfuehren(t.Context()); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"a2", "a1"}, {"c1", carla}, {"d2", "d1"}}
	if got := fake.Merges(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("merges %v, want %v", got, want)
	}
	for key, id := range map[string]string{"haushalt/a": "a2", "haushalt/c": "c1", "privat/d": "d2"} {
		if z := g.stand.Zuordnung[key]; z.ID != id || z.Eigen {
			t.Errorf("%s: %+v", key, z)
		}
	}
	if g.stand.Zuordnung["haushalt/c"].Name != "Carla Berg" {
		t.Fatal("the contact's name went")
	}
	for _, id := range []string{"a3", "a4", "b1", "b2", "u1"} {
		if fake.ID(map[string]string{"a3": "Anna Müller", "a4": "Anna Mueller", "b1": "Ben", "b2": "Bea", "u1": ""}[id]) == "" {
			t.Errorf("%s was merged", id)
		}
	}
	if got := g.Link("haushalt/a", ""); got != "https://photos.box.test/people/a2" {
		t.Fatal(got)
	}
	g.Auffrischen(t.Context())
	if err := g.Zusammenfuehren(t.Context()); err != nil || len(fake.Merges()) != 3 {
		t.Fatalf("again: %v %v", fake.Merges(), err)
	}
}

// Without person.statistics Immich's order (by faces) decides; without
// person.merge nothing is merged and the page's line names it, whether
// the key's list says so or a merge is refused.
func TestZusammenfuehrenRechte(t *testing.T) {
	g, fake := zwillinge(t)
	fake.Ohne = []string{"person.statistics"}
	g.Auffrischen(t.Context())
	g.Zusammenfuehren(t.Context())
	if got := fake.Merges(); len(got) != 3 || fmt.Sprint(got[0]) != "[a2 a1]" || g.Fehlt() != "" {
		t.Fatalf("by Immich's order: %v %q", got, g.Fehlt())
	}

	g, fake = zwillinge(t)
	fake.Ohne = []string{"person.merge"}
	g.Auffrischen(t.Context())
	if err := g.Zusammenfuehren(t.Context()); err != nil || len(fake.Merges()) != 0 || g.Fehlt() != "person.merge" {
		t.Fatalf("listed: %v %v %q", fake.Merges(), err, g.Fehlt())
	}

	g, fake = zwillinge(t)
	fake.Ohne = []string{"person.merge"} // after Immich said "all": the refusal tells
	if err := g.Zusammenfuehren(t.Context()); err != nil || g.Fehlt() != "person.merge" {
		t.Fatalf("refused: %v %q", err, g.Fehlt())
	}
	fake.Ohne = nil
	g.Auffrischen(t.Context())
	if g.Zusammenfuehren(t.Context()); g.Fehlt() != "" || len(fake.Merges()) != 3 {
		t.Fatalf("given: %q %v", g.Fehlt(), fake.Merges())
	}
}

// At most maxVerschmelzen a run, the rest the next.
func TestZusammenfuehrenGrenze(t *testing.T) {
	fake := immichfake.New("key")
	t.Cleanup(fake.Close)
	for i := range maxVerschmelzen + 11 {
		fake.Person(fmt.Sprint("x", i), "Max", "1", false, nil)
	}
	g := &Gesichter{Immich: &immich.Client{URL: fake.URL, Key: "key"}, Dir: t.TempDir()}
	g.Auffrischen(t.Context())
	g.Zusammenfuehren(t.Context())
	if m := fake.Merges(); len(m) != 1 || len(m[0]) != maxVerschmelzen+1 {
		t.Fatalf("%d merged", len(m[0])-1)
	}
	g.Auffrischen(t.Context())
	g.Zusammenfuehren(t.Context())
	if fake.ID("Max") == "" || len(fake.Merges()) != 2 || len(fake.Merges()[1]) != 11 {
		t.Fatalf("%v", fake.Merges())
	}
}

// naeheBox: Anna (100 photos) with an unnamed face beside her and another
// a little further; Ben near Anna and Anna nearest to him; Oma, a
// favourite, close to everyone; Carl far away.
func naeheBox(t *testing.T) (*Gesichter, *immichfake.Server) {
	t.Helper()
	fake := immichfake.New("key")
	t.Cleanup(fake.Close)
	for _, p := range []struct {
		id, name string
		fotos    int
		x, y     float64
		fav      bool
	}{{"anna", "Anna", 100, 0, 0, false}, {"u1", "", 7, 1, 0, false}, {"u2", "", 3, 0, 3, false}, {"ben", "Ben", 20, 2, 0, false},
		{"oma", "Oma", 50, 0.5, 0.5, true}, {"carl", "Carl", 5, 50, 50, false}, {"u3", "", 2, 52, 50, false}} {
		fake.Person(p.id, p.name, "1", false, Face(t, color.RGBA{uint8(p.fotos), 90, 90, 255}))
		fake.Lage(p.id, p.fotos, p.x, p.y, p.fav)
	}
	g := &Gesichter{Immich: &immich.Client{URL: fake.URL, Key: "key"}, Dir: t.TempDir(), Now: func() time.Time { return time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC) }}
	if err := g.Auffrischen(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := g.Vorschlagen(t.Context()); err != nil {
		t.Fatal(err)
	}
	return g, fake
}

func paare(g *Gesichter) (out []string) {
	for _, p := range g.Paare() {
		out = append(out, fmt.Sprintf("%s-%s", p.A.ID, p.B.ID))
	}
	return out
}

// The pairs: the closest unnamed face first, each pair once, a favourite
// never as the closest (Immich puts them first whatever their face); faces and photo counts kept; nothing merged by asking.
func TestVorschlagen(t *testing.T) {
	g, fake := naeheBox(t)
	got := paare(g)
	if len(got) == 0 || got[0] != "anna-u1" || slices.Contains(got, "ben-anna") || slices.ContainsFunc(got, func(s string) bool { return strings.HasSuffix(s, "-oma") }) {
		t.Fatalf("%v", got)
	}
	if !slices.Contains(got, "anna-ben") || !slices.Contains(got, "carl-u3") {
		t.Fatalf("%v", got)
	}
	p := g.Paare()[0]
	if p.A.Name != "Anna" || p.B.Name != "" || p.A.Fotos != 100 || p.B.Fotos != 7 || p.B.Bild == nil {
		t.Fatalf("%+v", p)
	}
	if len(fake.Merges()) != 0 {
		t.Fatal("asking merged")
	}
	// asked again only after a day
	n := fake.Abgerufen("u1")
	g.Vorschlagen(t.Context())
	if fake.Abgerufen("u1") != n {
		t.Fatal("the face again")
	}
}

// „Nein“ keeps the pair away, also for a Homes started anew; „Überspringen“
// puts it last; „Ja“ merges into the named one, two named ones into the
// one with more photos, and the contacts follow; a pair not offered is
// refused; without person.merge the answer says so.
func TestAntworten(t *testing.T) {
	g, fake := naeheBox(t)
	if _, err := g.Antworten(t.Context(), "u1", "anna", "nein"); err != nil {
		t.Fatal(err)
	}
	neu := &Gesichter{Immich: g.Immich, Dir: g.Dir}
	if slices.Contains(paare(neu), "anna-u1") || slices.Contains(paare(g), "anna-u1") {
		t.Fatal("no is not kept")
	}
	first := paare(g)[0]
	g.Antworten(t.Context(), g.Paare()[0].B.ID, g.Paare()[0].A.ID, "spaeter")
	if got := paare(g); got[len(got)-1] != first {
		t.Fatalf("skipped not last: %v", got)
	}
	if _, err := g.Antworten(t.Context(), "anna", "carl", "ja"); !errors.Is(err, ErrKeinPaar) {
		t.Fatalf("not offered: %v", err)
	}
	g.mu.Lock()
	g.stand.Zuordnung["haushalt/b"] = zuordnung{ID: "ben", Name: "Ben"}
	g.mu.Unlock()
	if name, err := g.Antworten(t.Context(), "ben", "anna", "ja"); err != nil || name != "Anna" {
		t.Fatalf("%q %v", name, err)
	}
	if name, err := g.Antworten(t.Context(), "carl", "u3", "ja"); err != nil || name != "Carl" {
		t.Fatalf("%q %v", name, err)
	}
	if got := fmt.Sprint(fake.Merges()); got != "[[anna ben] [carl u3]]" {
		t.Fatal(got)
	}
	if z := g.stand.Zuordnung["haushalt/b"]; z.ID != "anna" {
		t.Fatalf("the contact did not follow: %+v", z)
	}
	if slices.ContainsFunc(paare(g), func(s string) bool { return s == "anna-ben" || s == "carl-u3" }) {
		t.Fatal("a merged pair is still offered")
	}
	g.Auffrischen(t.Context())
	g.Vorschlagen(t.Context()) // the one kept is asked again
	if !slices.Contains(paare(g), "anna-u2") {
		t.Fatalf("not asked again: %v", paare(g))
	}
	fake.Ohne = []string{"person.merge"}
	var fehlt immich.FehltRecht
	if _, err := g.Antworten(t.Context(), "anna", "u2", "ja"); !errors.As(err, &fehlt) || g.Fehlt() != "person.merge" {
		t.Fatalf("%v %q", err, g.Fehlt())
	}
}
