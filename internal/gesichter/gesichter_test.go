package gesichter

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"net/url"
	"testing"
	"time"

	"github.com/mdopp/homes/internal/immich"
	"github.com/mdopp/homes/internal/immich/immichfake"
)

// Face is a JPEG as Immich cuts one: 250 px, one colour.
func Face(t testing.TB, c color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 250, 250))
	for y := range 250 {
		for x := range 250 {
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func box(t *testing.T) (*Gesichter, *immichfake.Server) {
	t.Helper()
	fake := immichfake.New("key")
	t.Cleanup(fake.Close)
	fake.Person("p-anna", "Anna Müller", "2026-11-01T10:00:00Z", false, Face(t, color.RGBA{200, 50, 50, 255}))
	fake.Person("p-ben", "Ben", "2026-11-01T10:00:00Z", false, Face(t, color.RGBA{50, 50, 200, 255}))
	fake.Person("p-x1", "Zwilling", "", false, nil)
	fake.Person("p-x2", "zwilling", "", false, nil)
	return &Gesichter{Immich: &immich.Client{URL: fake.URL, Key: "key"}, Web: "https://photos.box.test", Dir: t.TempDir()}, fake
}

// Link: the person of that name (umlauts and case aside), else Immich's
// search for the name; two of a name are no match; without a key, search.
func TestLink(t *testing.T) {
	g, _ := box(t)
	if err := g.Auffrischen(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := g.Link("", "anna  MÜLLER"); got != "https://photos.box.test/people/p-anna" {
		t.Fatalf("match: %q", got)
	}
	search := func(name string) string {
		return "https://photos.box.test/search?" + url.Values{"query": {`{"query":"` + name + `"}`}}.Encode()
	}
	if got := g.Link("", "Carla"); got != search("Carla") {
		t.Fatalf("no match: %q", got)
	}
	if got := g.Link("", "Zwilling"); got != search("Zwilling") {
		t.Fatalf("two of a name: %q", got)
	}
	g.Immich = nil
	if got := g.Link("", "Anna Müller"); got != search("Anna Müller") {
		t.Fatalf("no key: %q", got)
	}
	if g.Bild("Anna Müller") != nil {
		t.Fatal("no key: no faces")
	}
}

// A face is fetched once, small, kept on disk; again only when Immich
// changed it or after BildTTL; a new Homes reads it without asking.
func TestBildCachedSmall(t *testing.T) {
	g, fake := box(t)
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	g.Now = func() time.Time { return now }
	for range 3 {
		if err := g.Auffrischen(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	jpg := g.Bild("Anna Müller")
	img, err := jpeg.Decode(bytes.NewReader(jpg))
	if err != nil || img.Bounds().Dx() != Kante || img.Bounds().Dy() != Kante || fake.Abgerufen("p-anna") != 1 {
		t.Fatalf("%v %v, fetched %d", err, img.Bounds(), fake.Abgerufen("p-anna"))
	}
	if r, _, _, _ := img.At(40, 40).RGBA(); r>>8 < 180 {
		t.Fatalf("not the face's colour: %d", r>>8)
	}
	again := &Gesichter{Immich: &immich.Client{URL: "http://127.0.0.1:1", Key: "key"}, Web: g.Web, Dir: g.Dir}
	if !bytes.Equal(again.Bild("Anna Müller"), jpg) {
		t.Fatal("not kept on disk")
	}
	fake.Person("p-anna", "Anna Müller", "2026-11-05T10:00:00Z", false, Face(t, color.RGBA{0, 200, 0, 255}))
	g.Auffrischen(t.Context())
	if fake.Abgerufen("p-anna") != 2 || bytes.Equal(g.Bild("Anna Müller"), jpg) {
		t.Fatalf("a changed face: fetched %d", fake.Abgerufen("p-anna"))
	}
	now = now.Add(BildTTL + time.Hour)
	g.Auffrischen(t.Context())
	if fake.Abgerufen("p-anna") != 3 || fake.Abgerufen("p-ben") != 2 {
		t.Fatalf("after the TTL: %d %d", fake.Abgerufen("p-anna"), fake.Abgerufen("p-ben"))
	}
	// Immich away: what is kept stays
	g.Immich.URL = "http://127.0.0.1:1"
	if g.Auffrischen(t.Context()) == nil || g.Bild("Ben") == nil {
		t.Fatal("Immich away")
	}
}
