package gesichter

import (
	"encoding/base64"
	"fmt"
	"image/color"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/homes/internal/caldav/davfake"
	"github.com/mdopp/homes/internal/immich/immichfake"
)

func foto(jpeg []byte) string {
	return "PHOTO;ENCODING=b;TYPE=JPEG:" + base64.StdEncoding.EncodeToString(jpeg) + "\r\n"
}

// bilderBox: abgleichBox, plus an unnamed face u7 and Ole (with faces) in
// Immich, and anna's own contacts with pictures: Mama's face is u7's,
// Tina's Anna Müller's, a drawing without a face, a group of two, Dora's
// face Immich gives no person, Zwilling the same bytes as Dora's, Ole
// with a face of his own already, and one with only Homes' face. Each
// picture's faces are in erkannt; the clock in uhr.
func bilderBox(t *testing.T) (*Gesichter, *immichfake.Server, map[string][]string, *time.Time, *davfake.Server) {
	t.Helper()
	g, fake, dav := abgleichBox(t)
	uhr := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	g.Now = func() time.Time { return uhr }
	fake.Person("u7", "", "1", false, Face(t, color.RGBA{10, 200, 10, 255}))
	fake.Person("p-ole", "Ole", "1", false, Face(t, color.RGBA{10, 10, 10, 255}))
	erkannt := map[string][]string{}
	bild := func(uid, fn string, c color.RGBA, faces ...string) {
		b := Face(t, c)
		erkannt[string(b)] = faces
		dav.Put("/anna/handy-kontakte/"+uid+".vcf", vcard(uid, fn, foto(b)))
	}
	bild("m", "Mama", color.RGBA{40, 0, 0, 255}, "u7")
	bild("ti", "Tina", color.RGBA{80, 0, 0, 255}, "p-anna")
	bild("co", "Comic", color.RGBA{120, 0, 0, 255})
	bild("gr", "Gruppe", color.RGBA{160, 0, 0, 255}, "", "u7")
	bild("d", "Dora  Lind", color.RGBA{200, 0, 0, 255}, "")
	bild("zw", "Zwilling", color.RGBA{200, 0, 0, 255}, "")
	bild("o", "Ole", color.RGBA{240, 0, 0, 255}, "u7")
	dav.Put("/anna/handy-kontakte/h.vcf", vcard("h", "Hanna", "homes-foto.PHOTO;ENCODING=b;TYPE=JPEG;X-HOMES=immich:"+base64.StdEncoding.EncodeToString(Face(t, color.RGBA{0, 40, 0, 255}))+"\r\n"))
	fake.Erkennen = func(_ string, jpeg []byte) []string { return erkannt[string(jpeg)] }
	g.Merke("anna", "anna@box.test")
	g.Auffrischen(t.Context())
	if err := g.Abgleichen(t.Context()); err != nil {
		t.Fatal(err)
	}
	return g, fake, erkannt, &uhr, dav
}

func fragen(g *Gesichter) (out []string) {
	for _, f := range g.Fragen() {
		out = append(out, f.Name+"→"+cmpOr(f.Person.Name, f.Person.ID))
	}
	return out
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// A run uploads each picture of a contact whose person has no face,
// archived, the same bytes once; the next asks where Immich put a single
// face in a person, waits for one without, and deletes Homes' uploads of
// a group; after a day, of a picture without a face. Nothing is merged
// or named, and the Immich photo that was there already stays.
func TestKontaktbilder(t *testing.T) {
	g, fake, _, uhr, _ := bilderBox(t)
	writes := fake.Writes()
	if err := g.Kontaktbilder(t.Context()); err != nil {
		t.Fatal(err)
	}
	fotos := fake.Fotos()
	if len(fotos) != 5 || fake.Writes()-writes != 5 || !strings.HasSuffix(fotos[0], "|archive") || !strings.HasPrefix(fotos[0], "homes-kontaktbild-") {
		t.Fatalf("uploads: %v", fotos)
	}
	if g.stand.Kontaktbilder["privat/zw"].Stand != bildDoppelt || g.stand.Kontaktbilder["privat/o"].Hash != "" || g.stand.Kontaktbilder["privat/h"].Hash != "" {
		t.Fatalf("Zwilling, Ole, Hanna: %+v", g.stand.Kontaktbilder)
	}
	if len(g.Fragen()) != 0 {
		t.Fatal("asked before Immich looked")
	}
	*uhr = uhr.Add(time.Hour)
	g.Kontaktbilder(t.Context())
	if got := fmt.Sprint(fragen(g)); got != "[Mama→u7 Tina→Anna Müller]" {
		t.Fatalf("questions: %s", got)
	}
	if f := g.Fragen()[0]; f.Bild == nil || f.Person.Bild == nil || f.Key != "privat/m" {
		t.Fatalf("no pictures: %+v", f)
	}
	if len(fake.Fotos()) != 4 || g.stand.Kontaktbilder["privat/gr"].Stand != bildMehrere {
		t.Fatalf("the group stays: %v", fake.Fotos())
	}
	*uhr = uhr.Add(erkennenZeit)
	g.Kontaktbilder(t.Context())
	if len(fake.Fotos()) != 3 || g.stand.Kontaktbilder["privat/co"].Stand != bildOhne || g.stand.Kontaktbilder["privat/d"].Stand != bildWartet {
		t.Fatalf("after a day: %v %+v", fake.Fotos(), g.stand.Kontaktbilder["privat/d"])
	}
	if len(fake.Merges()) != 0 || fake.ID("Mama") == "" || fake.ID("") != "u7" {
		t.Fatalf("changed without a Ja: %v", fake.Merges())
	}
}

// Ja merges the contact's person into the one Immich gave the face, the
// contact following; Nein deletes the upload and never asks that person
// again, even for a new picture; Kein Foto deletes it and never uploads
// that picture again, a new one yes; an answer not asked is refused.
func TestKontaktbilderAntworten(t *testing.T) {
	g, fake, erkannt, uhr, dav := bilderBox(t)
	g.Kontaktbilder(t.Context())
	*uhr = uhr.Add(time.Hour)
	g.Kontaktbilder(t.Context())
	mama := fake.ID("Mama")
	if _, err := g.Bestaetigen(t.Context(), "privat/m", "p-anna", "ja"); err != ErrKeineFrage {
		t.Fatalf("not asked: %v", err)
	}
	name, err := g.Bestaetigen(t.Context(), "privat/m", "u7", "ja")
	if err != nil || name != "Mama" || fmt.Sprint(fake.Merges()) != "[[u7 "+mama+"]]" || fake.ID("Mama") != "u7" || g.stand.Zuordnung["privat/m"].ID != "u7" {
		t.Fatalf("ja: %q %v %v", name, err, fake.Merges())
	}
	vorher := len(fake.Fotos())
	if _, err := g.Bestaetigen(t.Context(), "privat/ti", "p-anna", "nein"); err != nil || len(fake.Fotos()) != vorher-1 {
		t.Fatalf("nein: %v %v", err, fake.Fotos())
	}
	if len(g.Fragen()) != 0 {
		t.Fatalf("still asked: %v", fragen(g))
	}
	g.Kontaktbilder(t.Context())
	if fake.Fotos()[0] == "" || len(fake.Merges()) != 1 || len(g.Fragen()) != 0 {
		t.Fatal("asked again")
	}

	// Tina's new picture, Immich again puts it in Anna Müller: deleted, not asked
	neu := Face(t, color.RGBA{80, 80, 0, 255})
	erkannt[string(neu)] = []string{"p-anna"}
	dav.Put("/anna/handy-kontakte/ti.vcf", vcard("ti", "Tina", foto(neu)))
	g.Kontaktbilder(t.Context())
	n := len(fake.Fotos())
	*uhr = uhr.Add(time.Hour)
	g.Kontaktbilder(t.Context())
	if len(fake.Fotos()) != n-1 || g.stand.Kontaktbilder["privat/ti"].Stand != bildAnderer || len(g.Fragen()) != 0 {
		t.Fatalf("a refused person asked again: %+v", g.stand.Kontaktbilder["privat/ti"])
	}

	// Kein Foto: Tina's picture now in u7
	erkannt[string(neu)] = []string{"u7"}
	g.stand.Kontaktbilder["privat/ti"] = kontaktbild{Nein: []string{"p-anna"}}
	g.Kontaktbilder(t.Context())
	*uhr = uhr.Add(time.Hour)
	g.Kontaktbilder(t.Context())
	if got := fmt.Sprint(fragen(g)); got != "[Tina→Mama]" {
		t.Fatalf("Tina's question: %s", got)
	}
	n = len(fake.Fotos())
	if _, err := g.Bestaetigen(t.Context(), "privat/ti", "u7", "keinfoto"); err != nil || len(fake.Fotos()) != n-1 {
		t.Fatalf("kein foto: %v", err)
	}
	g.Kontaktbilder(t.Context())
	if len(fake.Fotos()) != n-1 || !slices.Contains(g.stand.Kontaktbilder["privat/ti"].Nie, bildHash(neu)) {
		t.Fatal("the picture went up again")
	}
	if _, err := g.Bestaetigen(t.Context(), "privat/ti", "u7", "vielleicht"); err == nil {
		t.Fatal("any answer taken")
	}
}

// Without asset.upload, face.read or asset.delete nothing goes up and
// Fehlt names them; at most maxHochladen a run; a contact gone, or its
// picture, takes its open upload with it.
func TestKontaktbilderGrenzen(t *testing.T) {
	g, fake, _, _, dav := bilderBox(t)
	fake.Ohne = []string{"face.read"}
	g.Auffrischen(t.Context())
	if err := g.Kontaktbilder(t.Context()); err != nil || len(fake.Fotos()) != 0 || g.Fehlt() != "face.read" {
		t.Fatalf("without face.read: %v %v %q", err, fake.Fotos(), g.Fehlt())
	}
	fake.Ohne = nil
	g.Auffrischen(t.Context())
	for i := range maxHochladen {
		dav.Put(fmt.Sprintf("/anna/handy-kontakte/n%d.vcf", i), vcard(fmt.Sprint("n", i), fmt.Sprint("Neu ", i), foto(Face(t, color.RGBA{0, 0, uint8(20 * (i + 1)), 255}))))
	}
	g.Abgleichen(t.Context())
	g.Abgleichen(t.Context())
	g.Kontaktbilder(t.Context())
	if len(fake.Fotos()) != maxHochladen {
		t.Fatalf("%d uploads in one run", len(fake.Fotos()))
	}
	mama := "homes-kontaktbild-" + g.stand.Kontaktbilder["privat/m"].Hash + ".jpg|archive"
	if !slices.Contains(fake.Fotos(), mama) {
		t.Fatalf("Mama's picture not up: %v", fake.Fotos())
	}
	if err := g.Kontakte.DeleteCard(t.Context(), dav.URL+"/anna/handy-kontakte/m.vcf", ""); err != nil {
		t.Fatal(err)
	}
	g.Kontaktbilder(t.Context())
	if _, da := g.stand.Kontaktbilder["privat/m"]; da || slices.Contains(fake.Fotos(), mama) {
		t.Fatalf("a contact gone kept its picture: %v", fake.Fotos())
	}
	g.Kontaktbilder(t.Context())
	dora := "homes-kontaktbild-" + g.stand.Kontaktbilder["privat/d"].Hash + ".jpg|archive"
	if !slices.Contains(fake.Fotos(), dora) {
		t.Fatalf("Dora's picture not up: %v", fake.Fotos())
	}
	dav.Put("/anna/handy-kontakte/d.vcf", vcard("d", "Dora  Lind", ""))
	g.Kontaktbilder(t.Context())
	if slices.Contains(fake.Fotos(), dora) || g.stand.Kontaktbilder["privat/d"].Hash != "" {
		t.Fatalf("a picture gone stayed: %v", fake.Fotos())
	}
}
