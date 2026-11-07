package pages

import (
	"encoding/base64"
	"image/color"
	"net/url"
	"strings"
	"testing"

	"github.com/mdopp/homes/internal/gesichter"
	"github.com/mdopp/homes/internal/immich"
	"github.com/mdopp/homes/internal/immich/immichfake"
)

// istConfig: anna's key; Immich has an unnamed face u1 and Ben with
// faces; anna's own Handy-Kontakte have Mama, whose picture's face Immich
// gave u1, and Tina, whose Ben's; the hourly runs done.
func istConfig(t *testing.T) (StartConfig, *immichfake.Server) {
	t.Helper()
	dav, c := addressBook(t, "x|Xaver||")
	dav.AddPrincipal("anna")
	dav.AddAddressBook("/anna/handy-kontakte/", "Handy-Kontakte")
	fake := immichfake.New("key")
	t.Cleanup(fake.Close)
	fake.Person("u1", "", "1", false, faceJPEG(t, color.RGBA{40, 160, 40, 255}))
	fake.Person("p-ben", "Ben", "1", false, faceJPEG(t, color.RGBA{40, 40, 160, 255}))
	erkannt := map[string]string{}
	for _, k := range []struct {
		uid, name, person string
		c                 color.RGBA
	}{{"m", "Mama", "u1", color.RGBA{200, 40, 40, 255}}, {"t", "Tina", "p-ben", color.RGBA{200, 200, 40, 255}}} {
		b := faceJPEG(t, k.c)
		erkannt[string(b)] = k.person
		dav.Put("/anna/handy-kontakte/"+k.uid+".vcf", "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:"+k.uid+"\r\nFN:"+k.name+"\r\nPHOTO;ENCODING=b;TYPE=JPEG:"+base64.StdEncoding.EncodeToString(b)+"\r\nEND:VCARD\r\n")
	}
	fake.Erkennen = func(_ string, jpeg []byte) []string { return []string{erkannt[string(jpeg)]} }
	g := &gesichter.Gesichter{Immich: &immich.Client{URL: fake.URL, Key: "key"}, Kontakte: c, Web: "https://photos.box.test", Dir: t.TempDir()}
	g.Auffrischen(t.Context())
	g.Merke("anna", "anna@box.test")
	g.Abgleichen(t.Context())
	g.Kontaktbilder(t.Context())
	g.Kontaktbilder(t.Context())
	return StartConfig{Contacts: c, Gesichter: g}, fake
}

// „Ist das …?“: the owner sees each contact's picture beside the person
// Immich gave its face, with Ja, Nein and Kein Foto; Ja merges and says
// so, Nein takes the picture out and says so; nothing before the Ja;
// anyone else neither sees nor answers.
func TestIstDas(t *testing.T) {
	cfg, fake := istConfig(t)
	mustContain(t, as(Kontakte, cfg, KontaktePath, "anna", "anna@box.test").Body.String(), `Dieselbe Person?</a> · 2 Vorschläge`)
	body := as(Dieselbe, cfg, DieselbePath, "anna", "anna@box.test").Body.String()
	mustContain(t, body, "<h2>Ist das …?</h2>", "<b>Ist das Mama?</b>", "<b>Mama</b><span>Bild im Kontakt</span>", "<i>ohne Namen</i>", "<b>Ist das Tina?</b>", "<b>Ben</b>",
		`name=kontakt value="privat/m"`, `name=b value="u1"`, `value=ja data-laeuft="Führt zusammen …">Ja, das ist Mama</button>`, `>Nein</button>`, `value=keinfoto class=leise data-laeuft="Nimmt das Bild heraus …">Kein Foto von Mama</button>`)
	// the contact's picture opens Homes' upload in Immich, the person all its photos, each in a new tab
	mustContain(t, body, `<a href="https://photos.box.test/photos/a-`, `target=_blank rel=noopener aria-label="Bild im Kontakt"><img`,
		`<a href="https://photos.box.test/people/u1" target=_blank rel=noopener`)
	if len(fake.Merges()) != 0 || len(fake.Fotos()) != 2 {
		t.Fatalf("before an answer: %v %v", fake.Merges(), fake.Fotos())
	}
	if rec := antworte(cfg, "ben", url.Values{"kontakt": {"privat/m"}, "b": {"u1"}, "antwort": {"ja"}}); len(fake.Merges()) != 0 || rec.Code != 303 {
		t.Fatal("ben merged")
	}
	if rec := antworte(cfg, "anna", url.Values{"kontakt": {"privat/m"}, "b": {"u1"}, "antwort": {"spaeter"}}); rec.Code != 400 {
		t.Fatalf("spaeter for a picture: %d", rec.Code)
	}
	mustContain(t, antworte(cfg, "anna", url.Values{"kontakt": {"privat/m"}, "b": {"u1"}, "antwort": {"ja"}}).Body.String(), "Zusammengeführt: Die Fotos gehören jetzt zu Mama.")
	if len(fake.Merges()) != 1 || fake.Merges()[0][0] != "u1" {
		t.Fatalf("%v", fake.Merges())
	}
	body = antworte(cfg, "anna", url.Values{"kontakt": {"privat/t"}, "b": {"p-ben"}, "antwort": {"nein"}}).Body.String()
	mustContain(t, body, "Homes hat das Bild aus Immich genommen und fragt nach dieser Person nicht wieder.")
	mustNotContain(t, body, "Ist das Tina?")
	if len(fake.Fotos()) != 1 || strings.Contains(body, "<h2>Ist das …?</h2>") {
		t.Fatalf("after Nein: %v", fake.Fotos())
	}
	mustContain(t, antworte(cfg, "anna", url.Values{"kontakt": {"privat/t"}, "b": {"p-ben"}, "antwort": {"keinfoto"}}).Body.String(), "Diesen Vorschlag gibt es nicht mehr.")
}
