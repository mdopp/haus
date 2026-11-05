package pages

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mdopp/homes/internal/gesichter"
	"github.com/mdopp/homes/internal/haushalt"
	"github.com/mdopp/homes/internal/immich"
	"github.com/mdopp/homes/internal/immich/immichfake"
	"github.com/mdopp/homes/internal/napi"
)

// Owner 2026-11-05: Immich for the household's contacts — a „Fotos“ link,
// the face as the phones' picture of a card without one, and Immich's
// named people without a contact as suggestions on Kontakte.

// faceJPEG is a face as Immich cuts one: 250 px, one colour.
func faceJPEG(t *testing.T, c color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 250, 250))
	for y := range 250 {
		for x := range 250 {
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	jpeg.Encode(&b, img, nil)
	return b.Bytes()
}

// fotosConfig is sprungConfig's box with Immich and the owner's key:
// Immich knows Oma Erna and Ben, and Ben has no contact.
func fotosConfig(t *testing.T) (StartConfig, *immichfake.Server) {
	t.Helper()
	cfg, _ := sprungConfig(t)
	fake := immichfake.New("key")
	t.Cleanup(fake.Close)
	fake.Person("p-oma", "Oma Erna", "1", false, faceJPEG(t, color.RGBA{200, 40, 40, 255}))
	fake.Person("p-ben", "Ben Berg", "1", false, faceJPEG(t, color.RGBA{40, 40, 200, 255}))
	cfg.Gesichter = &gesichter.Gesichter{Immich: &immich.Client{URL: fake.URL, Key: "key"}, Web: "https://photos.box.test", Dir: t.TempDir()}
	if err := cfg.Gesichter.Auffrischen(t.Context()); err != nil {
		t.Fatal(err)
	}
	return cfg, fake
}

func immichSuche(name string) string {
	return "https://photos.box.test/search?" + url.Values{"query": {`{"query":"` + name + `"}`}}.Encode()
}

// „Fotos“: the Immich person of the contact's name, else Immich's search
// for it; without the key the search too; without Immich one plain line.
func TestKontaktSprungFotos(t *testing.T) {
	cfg, _ := fotosConfig(t)
	for _, c := range []struct{ path, want string }{
		{"/kontakte/oma/fotos", "https://photos.box.test/people/p-oma"}, // match
		{"/kontakte/x/fotos", immichSuche("Xaver")},                     // no match
		{"/kontakte/berg/fotos", immichSuche("Herr Berg")},              // no match either: names, not organisations
	} {
		if rec := oeffne(cfg, c.path, "anna", unterwegs); rec.Code != http.StatusFound || rec.Header().Get("Location") != c.want {
			t.Errorf("%s: %d %q", c.path, rec.Code, rec.Header().Get("Location"))
		}
	}
	cfg.Gesichter.Immich = nil // no key
	if rec := oeffne(cfg, "/kontakte/oma/fotos", "anna", zuHause); rec.Header().Get("Location") != immichSuche("Oma Erna") {
		t.Fatalf("no key: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	mustContain(t, oeffne(cfg, "/kontakte/oma", "anna", zuHause).Body.String(), `<p class=weiter-in><a href="/kontakte/oma/fotos">Fotos in Immich</a></p>`)
	for _, off := range []StartConfig{{Gesichter: nil}, {Gesichter: cfg.Gesichter, Module: modulListe(t, "kontakte")}} {
		c := cfg
		c.Gesichter, c.Module = off.Gesichter, off.Module
		if rec := oeffne(c, "/kontakte/oma/fotos", "anna", zuHause); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Auf dieser Box gibt es kein Immich.") {
			t.Errorf("no Immich: %d %s", rec.Code, rec.Body)
		}
		mustNotContain(t, oeffne(c, "/kontakte/oma", "anna", zuHause).Body.String(), "/kontakte/oma/fotos")
	}
}

// photoOf is the bytes of a card's PHOTO line, unfolded; nil without one.
func photoOf(t *testing.T, card string) []byte {
	t.Helper()
	for _, l := range strings.Split(unfoldCard.Replace(card), "\n") {
		if _, name, _, v := zeile(l); name == "PHOTO" {
			_, b64, ok := strings.Cut(v, "base64,")
			if !ok {
				b64 = v
			}
			b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
			if err != nil {
				t.Fatal(err)
			}
			return b
		}
	}
	return nil
}

// The phones' card: the Fotos link, and the small face as a marked PHOTO
// where the card has none (vCard 3.0 and 4.0); a card's own stays alone.
func TestKontaktKarteGesicht(t *testing.T) {
	cfg, _ := fotosConfig(t)
	karte := KontaktKarte(cfg)
	served := karte(omaKarte, "")
	mustContain(t, served, "homes-fotos.URL;TYPE=homes:https://homes.box.test/kontakte/oma/fotos\r\nhomes-fotos.X-ABLabel:Fotos\r\n",
		"homes-foto.PHOTO;ENCODING=b;TYPE=JPEG;X-HOMES=immich:")
	for _, l := range strings.Split(served, "\r\n") {
		if (strings.HasPrefix(l, " ") || strings.Contains(l, "PHOTO")) && len(l) > 75 { // the face folded (the links are short enough on a box)
			t.Fatalf("a line of %d octets", len(l))
		}
	}
	img, err := jpeg.Decode(bytes.NewReader(photoOf(t, served)))
	if err != nil || img.Bounds().Dx() > gesichter.Kante || img.Bounds().Dy() > gesichter.Kante {
		t.Fatalf("%v %v", err, img)
	}
	v4 := strings.Replace(omaKarte, "VERSION:3.0", "VERSION:4.0", 1)
	mustContain(t, unfoldCard.Replace(karte(v4, "")), "homes-foto.PHOTO;MEDIATYPE=image/jpeg;X-HOMES=immich:data:image/jpeg;base64,")
	own := strings.Replace(omaKarte, "END:VCARD", "PHOTO;ENCODING=b;TYPE=JPEG:AAAA\r\nEND:VCARD", 1)
	if got := karte(own, ""); strings.Count(got, "PHOTO") != 1 || strings.Contains(got, "X-HOMES") {
		t.Fatalf("a card's own picture:\n%s", got)
	}
	// no key: the link, no face
	cfg.Gesichter.Immich = nil
	if got := KontaktKarte(cfg)(omaKarte, ""); strings.Contains(got, "PHOTO") || !strings.Contains(got, "/kontakte/oma/fotos") {
		t.Fatalf("no key:\n%s", got)
	}
}

// Back from a phone: the face Homes made comes out, marked by its group,
// its parameter, or only its bytes; a picture the person set stays.
func TestKontaktKarteGesichtZurueck(t *testing.T) {
	cfg, _ := fotosConfig(t)
	karte := KontaktKarte(cfg)
	served := karte(omaKarte, "")
	gemacht := base64.StdEncoding.EncodeToString(photoOf(t, served))
	if back := karte(served, "oma"); strings.Contains(back, "PHOTO") || strings.Contains(back, "/kontakte/oma/fotos") {
		t.Fatalf("as served:\n%s", back)
	}
	for _, l := range []string{"PHOTO;ENCODING=b;TYPE=JPEG;X-HOMES=immich:" + gemacht, "PHOTO;ENCODING=b;TYPE=JPEG:" + gemacht, "PHOTO:data:image/jpeg;base64," + gemacht} {
		if back := karte(strings.Replace(omaKarte, "END:VCARD", l+"\r\nEND:VCARD", 1), "oma"); strings.Contains(back, "PHOTO") {
			t.Errorf("%.40s… kept", l)
		}
	}
	eigen := base64.StdEncoding.EncodeToString(faceJPEG(t, color.RGBA{0, 160, 0, 255}))
	if back := karte(strings.Replace(omaKarte, "END:VCARD", "PHOTO;ENCODING=b;TYPE=JPEG:"+eigen+"\r\nEND:VCARD", 1), "oma"); !strings.Contains(back, eigen) {
		t.Fatalf("the person's own picture went:\n%.300s", back)
	}
}

// The whole way, through napi and Radicale: the face is never stored; a
// picture set on the phone is, and from then on the phones get that one.
func TestKontaktKarteGesichtThroughNapi(t *testing.T) {
	cfg, _ := fotosConfig(t)
	dav, c := addressBook(t)
	dav.Put("/homes/haushalt-kontakte/oma.vcf", omaKarte)
	h := napi.Handler(napi.Config{Validate: func(_ context.Context, b string) bool { return b == "anna1" }, Konto: c, Haushalt: haushalt.Config{Loc: berlin(t), Karte: KontaktKarte(cfg)},
		Whoami: func(string) (string, bool) { return "anna", true }})
	call := func(method, path, body string, hdr ...string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer anna1")
		for i := 0; i+1 < len(hdr); i += 2 {
			req.Header.Set(hdr[i], hdr[i+1])
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	etagOf := func(feed string) string {
		return strings.TrimSpace(strings.SplitN(strings.SplitN(feed, "X-HOMES-ETAG:", 2)[1], "\r\n", 2)[0])
	}
	feed := call("GET", "/napi/gemeinsam/kontakte?schreibbar=1", "").Body.String()
	mustContain(t, feed, "X-HOMES=immich")
	// an edit sent back with the face as served: the edit is stored, the face not
	phone := KontaktKarte(cfg)(omaKarte, "")
	phone = strings.Replace(phone, "URL:https://erna.example/blog", "URL:https://erna.example/blog\r\nTEL:0711 1", 1)
	if rec := call("PUT", "/napi/gemeinsam/kontakte/oma", phone, "If-Match", etagOf(feed)); rec.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", rec.Code, rec.Body)
	}
	stored := dav.Objects()["/homes/haushalt-kontakte/oma.vcf"]
	mustContain(t, stored, "TEL:0711 1")
	mustNotContain(t, stored, "PHOTO", "X-HOMES")
	// a picture the person set on the phone is stored, and served instead of the face
	feed = call("GET", "/napi/gemeinsam/kontakte?schreibbar=1", "").Body.String()
	eigen := base64.StdEncoding.EncodeToString(faceJPEG(t, color.RGBA{0, 160, 0, 255}))
	phone = strings.Replace(omaKarte, "END:VCARD", "PHOTO;ENCODING=b;TYPE=JPEG:"+eigen+"\r\nEND:VCARD", 1)
	if rec := call("PUT", "/napi/gemeinsam/kontakte/oma", phone, "If-Match", etagOf(feed)); rec.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", rec.Code, rec.Body)
	}
	if !bytes.Equal(photoOf(t, dav.Objects()["/homes/haushalt-kontakte/oma.vcf"]), photoOf(t, phone)) {
		t.Fatal("the person's picture was not stored")
	}
	feed = call("GET", "/napi/gemeinsam/kontakte?schreibbar=1", "").Body.String()
	if strings.Contains(feed, "X-HOMES=immich") || strings.Count(feed, "PHOTO") != 1 {
		t.Fatalf("the feed after:\n%.600s", feed)
	}
	// a phone that sends no picture leaves the stored one as it is
	phone = strings.Replace(omaKarte, "FN:Oma Erna", "FN:Oma Erna B.", 1)
	if rec := call("PUT", "/napi/gemeinsam/kontakte/oma", phone, "If-Match", etagOf(feed)); rec.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", rec.Code, rec.Body)
	}
	if photoOf(t, dav.Objects()["/homes/haushalt-kontakte/oma.vcf"]) == nil {
		t.Fatal("a phone without a picture took the stored one")
	}
}

func postKontakte(t *testing.T, cfg StartConfig, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", KontakteImmichPath, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Remote-User", "anna")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	rec := httptest.NewRecorder()
	KontakteAusImmich(cfg).ServeHTTP(rec, req)
	return rec
}

// Kontakte: Immich's named people without a contact, quiet, each to make
// or to hide; hidden stays hidden, made is a card with the name and no
// stored picture. Nothing without a tap; nothing without the key.
func TestKontakteVorschlaege(t *testing.T) {
	cfg, _ := fotosConfig(t)
	_, body, _ := familyPage(t, Kontakte, cfg, "GET", KontaktePath, "family")
	mustContain(t, body, "In Immich, aber noch kein Haushalts-Kontakt", "<span>Ben Berg</span>", `name=id value="p-ben"`,
		`data-laeuft="Legt an …">als Haushalts-Kontakt anlegen</button>`, `<button class=leise data-laeuft="Blendet aus …">ausblenden</button>`, `<img src="data:image/jpeg;base64,`)
	mustNotContain(t, body, "<span>Oma Erna</span>")
	before := len(mustContacts(t, cfg))

	if rec := postKontakte(t, cfg, url.Values{"tun": {"ausblenden"}, "id": {"p-ben"}}); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/kontakte?ausgeblendet=1" {
		t.Fatalf("ausblenden: %d %v", rec.Code, rec.Header())
	}
	_, body, _ = familyPage(t, Kontakte, cfg, "GET", "/kontakte?ausgeblendet=1", "family")
	mustContain(t, body, "Ausgeblendet: Homes schlägt diese Person nicht mehr vor.")
	mustNotContain(t, body, "Ben Berg", "In Immich, aber")
	if len(mustContacts(t, cfg)) != before {
		t.Fatal("hiding made a contact")
	}

	cfg, _ = fotosConfig(t) // a new Homes data folder: Ben suggested again
	rec := postKontakte(t, cfg, url.Values{"tun": {"anlegen"}, "id": {"p-ben"}})
	loc, _ := url.Parse(rec.Header().Get("Location"))
	if rec.Code != http.StatusSeeOther || loc.Path != KontaktePath || loc.Query().Get("angelegt") == "" {
		t.Fatalf("anlegen: %d %v", rec.Code, rec.Header())
	}
	all := mustContacts(t, cfg)
	if len(all) != before+1 {
		t.Fatalf("%d contacts", len(all))
	}
	_, body, _ = familyPage(t, Kontakte, cfg, "GET", loc.String(), "family")
	mustContain(t, body, "„Ben Berg“ ist jetzt ein Haushalts-Kontakt")
	mustNotContain(t, body, "In Immich, aber")
	served := KontaktKarte(cfg)("BEGIN:VCARD\r\nVERSION:4.0\r\nUID:"+loc.Query().Get("angelegt")+"\r\nFN:Ben Berg\r\nEND:VCARD\r\n", "")
	mustContain(t, served, "X-HOMES=immich") // the face comes as for every card without a picture
	// a second tap makes no second card
	postKontakte(t, cfg, url.Values{"tun": {"anlegen"}, "id": {"p-ben"}})
	if len(mustContacts(t, cfg)) != before+1 {
		t.Fatal("a second card")
	}
	if rec := postKontakte(t, cfg, url.Values{"tun": {"anlegen"}, "id": {"unbekannt"}}); rec.Code != http.StatusConflict ||
		!strings.Contains(rec.Body.String(), "Diese Person kennt Homes aus Immich nicht mehr.") {
		t.Fatalf("unknown: %d %s", rec.Code, rec.Body)
	}
	if code, _, _ := familyPage(t, KontakteAusImmich, cfg, "GET", KontakteImmichPath, "family"); code != http.StatusMethodNotAllowed {
		t.Fatalf("GET: %d", code)
	}

	cfg.Gesichter.Immich = nil // no key: no suggestions
	_, body, _ = familyPage(t, Kontakte, cfg, "GET", KontaktePath, "family")
	mustNotContain(t, body, "In Immich, aber")
}

func mustContacts(t *testing.T, cfg StartConfig) []string {
	t.Helper()
	all, _, err := cfg.Contacts.Contacts(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, c := range all {
		out = append(out, c.Name)
	}
	return out
}
