package pages

import (
	"image/color"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/mdopp/homes/internal/caldav"
	"github.com/mdopp/homes/internal/gesichter"
	"github.com/mdopp/homes/internal/immich"
	"github.com/mdopp/homes/internal/immich/immichfake"
)

// Owner 2026-11-06: Immich's people are no longer offered as contacts;
// every contact gets an Immich person instead (gesichter.Abgleichen), and
// the key's owner reaches each of their own Handy-Kontakte's photos.

// privatConfig: Immich with the key of anna's account (anna@box.test),
// Oma Erna with her face; the household's book; anna's own Handy-Kontakte
// Carla and Oma Erna, ben's his own; the hourly run done once.
func privatConfig(t *testing.T) (StartConfig, *immichfake.Server) {
	t.Helper()
	dav, c := addressBook(t, "oma|Oma Erna|0170 1234567|erna@example.org", "x|Xaver||")
	dav.AddPrincipal("anna")
	dav.AddPrincipal("ben")
	dav.AddAddressBook("/anna/handy-kontakte/", "Handy-Kontakte")
	dav.Put("/anna/handy-kontakte/c.vcf", "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:c\r\nFN:Carla Berg\r\nTEL:0711 2\r\nEND:VCARD\r\n")
	dav.Put("/anna/handy-kontakte/o.vcf", "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:o\r\nFN:Oma Erna\r\nEND:VCARD\r\n")
	dav.AddAddressBook("/ben/handy-kontakte/", "Handy-Kontakte")
	dav.Put("/ben/handy-kontakte/e.vcf", "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:e\r\nFN:Emil Geheim\r\nEND:VCARD\r\n")
	fake := immichfake.New("key")
	t.Cleanup(fake.Close)
	fake.Person("p-oma", "Oma Erna", "1", false, faceJPEG(t, color.RGBA{200, 40, 40, 255}))
	g := &gesichter.Gesichter{Immich: &immich.Client{URL: fake.URL, Key: "key"}, Kontakte: c, Web: "https://photos.box.test", Dir: t.TempDir()}
	cfg := StartConfig{Contacts: c, Gesichter: g}
	if err := g.Auffrischen(t.Context()); err != nil {
		t.Fatal(err)
	}
	return cfg, fake
}

// as asks a page as a person with an e-mail.
func as(page func(StartConfig) http.HandlerFunc, cfg StartConfig, target, user, email string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", target, nil)
	req.Header.Set("Remote-User", user)
	req.Header.Set("Remote-Email", email)
	req.Header.Set("Remote-Groups", "family")
	rec := httptest.NewRecorder()
	page(cfg).ServeHTTP(rec, req)
	return rec
}

// The suggestions are gone: Kontakte offers no Immich person as a
// contact, an old hidden-list file is ignored, the old address is no
// page of Kontakte's.
func TestKontakteOhneImmichVorschlaege(t *testing.T) {
	cfg, _ := fotosConfig(t) // Ben Berg is in Immich, not in the book
	os.WriteFile(filepath.Join(cfg.Gesichter.Dir, "immich-ausgeblendet.json"), []byte(`["p-ben"]`), 0o600)
	_, body, _ := familyPage(t, Kontakte, cfg, "GET", KontaktePath, "family")
	mustNotContain(t, body, "Ben Berg", "In Immich, aber", "anlegen", "ausblenden", "/kontakte/aus-immich")
	for _, r := range Gruppen(cfg)["kontakte"] {
		if r.Muster == "/kontakte/aus-immich" {
			t.Fatal("the suggestions' address is still routed")
		}
	}
}

// Meine Kontakte: the key's owner (Homes learns the login from a page seen
// with the key account's e-mail) sees their own Handy-Kontakte, each
// leading to its Immich person; the household's book and another
// person's are not there.
func TestMeineKontakte(t *testing.T) {
	cfg, fake := privatConfig(t)
	mustNotContain(t, as(Kontakte, cfg, KontaktePath, "anna", "ben@box.test").Body.String(), MeineKontaktePath)
	mustContain(t, as(Kontakte, cfg, KontaktePath, "anna", "Anna@Box.test").Body.String(), `<a href=/kontakte/privat>Meine Kontakte</a>`)
	cfg.Gesichter.Abgleichen(t.Context()) // the hourly run, now with anna's own
	rec := as(MeineKontakte, cfg, MeineKontaktePath, "anna", "anna@box.test")
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, body)
	}
	mustContain(t, body, "<h1>Meine Kontakte</h1>", `<a href="/kontakte/privat/c/fotos">Carla Berg</a>`, `href="tel:07112"`, `<a href="/kontakte/privat/o/fotos">Oma Erna</a>`, `id=kontakte-suche`)
	mustNotContain(t, body, "Xaver", "Emil Geheim")
	for path, want := range map[string]string{"/kontakte/privat/c/fotos": "https://photos.box.test/people/" + fake.ID("Carla Berg"), "/kontakte/privat/o/fotos": "https://photos.box.test/people/p-oma"} {
		if rec := as(MeineKontakte, cfg, path, "anna", "anna@box.test"); rec.Code != http.StatusFound || rec.Header().Get("Location") != want {
			t.Errorf("%s: %d %q", path, rec.Code, rec.Header().Get("Location"))
		}
	}
	if rec := as(MeineKontakte, cfg, "/kontakte/privat/x/fotos", "anna", "anna@box.test"); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown: %d", rec.Code)
	}
	// the household's „Fotos“ leads to the person Homes gave the card
	if rec := oeffne(cfg, "/kontakte/x/fotos", "anna", zuHause); rec.Header().Get("Location") != "https://photos.box.test/people/"+fake.ID("Xaver") {
		t.Fatalf("household: %q", rec.Header().Get("Location"))
	}
	// anyone else: to Kontakte, no link
	for _, path := range []string{MeineKontaktePath, "/kontakte/privat/c/fotos"} {
		if rec := as(MeineKontakte, cfg, path, "ben", "ben@box.test"); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != KontaktePath {
			t.Errorf("ben %s: %d", path, rec.Code)
		}
	}
	// a missing permission is said on the page
	fake.Ohne = []string{"person.create"}
	cfg.Contacts.PutContact(t.Context(), caldav.Contact{UID: "neu", Name: "Neu Mann"})
	cfg.Gesichter.Abgleichen(t.Context())
	mustContain(t, as(MeineKontakte, cfg, MeineKontaktePath, "anna", "anna@box.test").Body.String(), "fehlt diese Berechtigung: person.create")
}
