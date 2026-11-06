package gesichter

import (
	"fmt"
	"image/color"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/homes/internal/caldav"
	"github.com/mdopp/homes/internal/caldav/davfake"
	"github.com/mdopp/homes/internal/immich"
	"github.com/mdopp/homes/internal/immich/immichfake"
)

func vcard(uid, fn, extra string) string {
	return "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:" + uid + "\r\nFN:" + fn + "\r\n" + extra + "END:VCARD\r\n"
}

// abgleichBox: Immich knows Anna Müller (with faces) and a hidden Ben;
// the household's book has Anna, Carla (a birthday) and a firm; anna's own
// Handy-Kontakte have Ben, Dora (a birthday without a year) and Carla
// again; ben's his own.
func abgleichBox(t *testing.T) (*Gesichter, *immichfake.Server, *davfake.Server) {
	t.Helper()
	fake := immichfake.New("key")
	t.Cleanup(fake.Close)
	fake.Person("p-anna", "Anna Müller", "1", false, Face(t, color.RGBA{200, 50, 50, 255}))
	fake.Person("p-ben", "Ben", "1", true, nil)
	dav := davfake.New("homes", "pw")
	t.Cleanup(dav.Close)
	dav.AddAddressBook("/homes/haushalt-kontakte/", "Haushalt")
	dav.Put("/homes/haushalt-kontakte/a.vcf", vcard("a", "anna müller", ""))
	dav.Put("/homes/haushalt-kontakte/c.vcf", vcard("c", "Carla Berg", "BDAY:1990-04-12\r\n"))
	dav.Put("/homes/haushalt-kontakte/f.vcf", vcard("f", "Stadtwerke", "CATEGORIES:Firma\r\n"))
	dav.AddPrincipal("anna")
	dav.AddPrincipal("ben")
	dav.AddAddressBook("/anna/handy-kontakte/", "Handy-Kontakte")
	dav.Put("/anna/handy-kontakte/b.vcf", vcard("b", "Ben", ""))
	dav.Put("/anna/handy-kontakte/d.vcf", vcard("d", "Dora  Lind", "BDAY:--07-01\r\n"))
	dav.Put("/anna/handy-kontakte/c2.vcf", vcard("c2", "Carla Berg", ""))
	dav.AddAddressBook("/ben/handy-kontakte/", "Handy-Kontakte")
	dav.Put("/ben/handy-kontakte/e.vcf", vcard("e", "Emil Geheim", ""))
	g := &Gesichter{Immich: &immich.Client{URL: fake.URL, Key: "key"}, Kontakte: &caldav.Client{URL: dav.URL, User: "homes", Password: "pw", Loc: time.UTC},
		Web: "https://photos.box.test", Dir: t.TempDir(), Now: func() time.Time { return time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC) }}
	if err := g.Auffrischen(t.Context()); err != nil {
		t.Fatal(err)
	}
	return g, fake, dav
}

// Each contact gets a person of its name: one Immich has (hidden too) is
// taken as it is, else Homes makes one, with the birthday when it is a
// whole date; a name twice is one person; firms and another person's
// Handy-Kontakte are not; a second run changes nothing.
func TestAbgleichen(t *testing.T) {
	g, fake, _ := abgleichBox(t)
	if err := g.Abgleichen(t.Context()); err != nil {
		t.Fatal(err)
	}
	// before the owner was seen: the household's only
	if got := fmt.Sprint(fake.Alle()); got != "[Anna Müller| Ben| Carla Berg|1990-04-12]" {
		t.Fatalf("household: %s", got)
	}
	g.Merke("anna", "Someone@box.test") // not the key's account
	if g.Wessen() != "" {
		t.Fatal("a stranger is the owner")
	}
	g.Merke("anna", " ANNA@box.test")
	if err := g.Abgleichen(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(fake.Alle()); got != "[Anna Müller| Ben| Carla Berg|1990-04-12 Dora Lind|]" {
		t.Fatalf("with anna's own: %s", got)
	}
	writes := fake.Writes()
	again := &Gesichter{Immich: g.Immich, Kontakte: g.Kontakte, Dir: g.Dir, Web: g.Web, Now: g.Now}
	if err := again.Abgleichen(t.Context()); err != nil || fake.Writes() != writes {
		t.Fatalf("a second run wrote %d: %v", fake.Writes()-writes, err)
	}
	for key, want := range map[string]string{"haushalt/a": "p-anna", "privat/b": "p-ben", "haushalt/c": fake.ID("Carla Berg"), "privat/c2": fake.ID("Carla Berg"), "privat/d": fake.ID("Dora Lind")} {
		if got := again.Link(key, "x"); got != "https://photos.box.test/people/"+want {
			t.Errorf("%s: %s", key, got)
		}
	}
	if strings.Contains(fmt.Sprint(fake.Alle()), "Emil") || strings.Contains(fmt.Sprint(fake.Alle()), "Stadtwerke") {
		t.Fatal("ben's own or a firm")
	}
}

// A contact changed: the person Homes made follows its name and birthday;
// a person Homes did not make is never renamed, the contact is matched
// anew; a contact gone loses its line, its person stays.
func TestAbgleichenFolgt(t *testing.T) {
	g, fake, dav := abgleichBox(t)
	g.Merke("anna", "anna@box.test")
	g.Abgleichen(t.Context())
	carla := fake.ID("Carla Berg")
	dav.Put("/homes/haushalt-kontakte/c.vcf", vcard("c", "Carla Berg-Lind", "BDAY:1990-04-13\r\n"))
	dav.Put("/homes/haushalt-kontakte/a.vcf", vcard("a", "Anna M.", ""))
	dav.Put("/anna/handy-kontakte/c2.vcf", vcard("c2", "Clara", ""))
	if err := g.Abgleichen(t.Context()); err != nil {
		t.Fatal(err)
	}
	got := fake.Alle()
	if !slices.Contains(got, "Carla Berg-Lind|1990-04-13") || !slices.Contains(got, "Anna Müller|") || !slices.Contains(got, "Anna M.|") || !slices.Contains(got, "Clara|") {
		t.Fatalf("%v", got)
	}
	if fake.ID("Carla Berg-Lind") != carla || g.Link("haushalt/a", "") == "https://photos.box.test/people/p-anna" {
		t.Fatal("followed the wrong way")
	}
	n := len(got)
	dav.Put("/anna/handy-kontakte/d.vcf", "") // Dora gone from the phone
	g.Abgleichen(t.Context())
	if len(fake.Alle()) != n {
		t.Fatal("a person was deleted")
	}
}

// At most maxAufrufe calls a run, the rest next time; a missing
// permission stops the run and is kept for the page to say.
func TestAbgleichenGrenzeUndRecht(t *testing.T) {
	g, fake, dav := abgleichBox(t)
	for i := range maxAufrufe {
		dav.Put(fmt.Sprintf("/homes/haushalt-kontakte/n%03d.vcf", i), vcard(fmt.Sprintf("n%03d", i), fmt.Sprintf("Neu %03d", i), ""))
	}
	g.Abgleichen(t.Context())
	if w := fake.Writes(); w >= maxAufrufe || w == 0 {
		t.Fatalf("%d writes in one run", w)
	}
	for range 3 {
		g.Abgleichen(t.Context())
	}
	if len(fake.Alle()) != 2+maxAufrufe+1 {
		t.Fatalf("%d people after four runs", len(fake.Alle()))
	}
	fake.Ohne = []string{"person.create"}
	dav.Put("/homes/haushalt-kontakte/z.vcf", vcard("z", "Zoe", ""))
	if err := g.Abgleichen(t.Context()); err == nil || g.Fehlt() != "person.create" {
		t.Fatalf("%v %q", err, g.Fehlt())
	}
	if g.Link("haushalt/c", "") == "" || len(g.stand.Zuordnung) < maxAufrufe {
		t.Fatal("a broken run dropped the lines")
	}
	fake.Ohne = nil
	g.Abgleichen(t.Context())
	if g.Fehlt() != "" || fake.ID("Zoe") == "" {
		t.Fatal("not again")
	}
}
