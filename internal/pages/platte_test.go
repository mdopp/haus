package pages

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mdopp/haus/internal/discogs"
	"github.com/mdopp/haus/internal/einkauf"
	"github.com/mdopp/haus/internal/judge"
	"github.com/mdopp/haus/internal/merken"
	"github.com/mdopp/haus/internal/musicbrainz"
)

// a Discogs that knows Wanda's Amore by barcode and by catalogue number,
// and keeps what was added
func discogsFake(t *testing.T) (*discogs.Regale, *[]string) {
	var added []string
	shelf := []any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ans := func(v any) { json.NewEncoder(w).Encode(v) }
		q := r.URL.Query()
		amore := map[string]any{"id": 11, "title": "Wanda (2) - Amore", "year": "2014", "country": "Germany", "format": []string{"Vinyl", "LP", "Album"}, "label": []string{"Problembär"}, "catno": "PB 011"}
		switch p := r.URL.Path; {
		case p == "/oauth/identity":
			ans(map[string]string{"username": "mdopp"})
		case p == "/oauth/request_token":
			w.Write([]byte("oauth_token=rt&oauth_token_secret=rs"))
		case p == "/oauth/access_token":
			w.Write([]byte("oauth_token=at2&oauth_token_secret=as2"))
		case p == "/database/search" && (q.Get("barcode") == "4006381333931" || q.Get("catno") == "PB 011"):
			ans(map[string]any{"results": []any{amore}})
		case p == "/database/search":
			ans(map[string]any{"results": []any{}})
		case strings.HasPrefix(p, "/users/mdopp/collection/folders/1/releases/"):
			added = append(added, strings.TrimPrefix(p, "/users/mdopp/collection/folders/1/releases/"))
			shelf = append(shelf, map[string]any{"id": 11, "basic_information": map[string]any{"title": "Amore", "year": 2014, "artists": []any{map[string]string{"name": "Wanda (2)"}}}})
			w.WriteHeader(201)
			ans(map[string]any{})
		case p == "/users/mdopp/collection/folders/0/releases":
			ans(map[string]any{"pagination": map[string]int{"pages": 1}, "releases": shelf})
		case strings.HasPrefix(p, "/users/mdopp/wants/"):
			w.WriteHeader(204)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	// Anna has connected her Discogs; Ben has not
	os.WriteFile(filepath.Join(dir, "discogs-konten.json"), []byte(`{"anna":{"name":"mdopp","token":"at","secret":"as"}}`), 0o600)
	return &discogs.Regale{App: &discogs.App{Key: "ck", Secret: "cs", Base: srv.URL, Web: "https://www.discogs.test"}, Dir: dir}, &added
}

// a house model that reads a record's label from a photo, or is busy
func modelFake(t *testing.T, busy bool) *judge.Client {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/model-lease" {
			if busy {
				json.NewEncoder(w).Encode(map[string]string{"state": "held", "alias": "qwen", "holder": "die Werkstatt", "until": "22:30"})
				return
			}
			json.NewEncoder(w).Encode(map[string]string{"state": "ready", "alias": "gemma"})
			return
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"image_url"`) || !strings.Contains(string(body), "data:image/jpeg;base64,") {
			http.Error(w, "no image", 400)
			return
		}
		content, _ := json.Marshal(map[string]string{"barcode": "123", "katalognummer": "PB 011", "label": "Problembär", "kuenstler": "Wanda", "titel": "Amore"})
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": string(content)}}}})
	}))
	t.Cleanup(srv.Close)
	return &judge.Client{URL: srv.URL + "/v1", Model: "gemma", Key: "k"}
}

func plattePost(t *testing.T, h http.HandlerFunc, fields map[string]string, photo bool) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		mw.WriteField(k, v)
	}
	if photo {
		fw, _ := mw.CreateFormFile("foto", "rueckseite.jpg")
		fw.Write([]byte("\xff\xd8\xff\xe0\x00\x10JFIF\x00 ein Foto"))
	}
	mw.Close()
	req := httptest.NewRequest("POST", PlattePath, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Remote-User", "anna")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// owner 2026-10-25: photos of a record become its pressing on Discogs —
// the barcode first, the model's reading of the label without one — and
// only "Das ist sie" adds it; a wish waiting for it is ticked off.
func TestPlatteErfassen(t *testing.T) {
	dc, added := discogsFake(t)
	l, _ := shoppingList(t)
	l.Changed = nil
	w := &einkauf.Wuensche{Listen: l, Dir: t.TempDir(), Regale: dc, MB: fundFake{musicbrainz.Fund{Gefunden: true, Album: "Amore"}}}
	cfg := StartConfig{Discogs: dc, Wuensche: w, Judge: modelFake(t, false)}
	h := Platte(cfg)

	_, body, _ := familyPage(t, Platte, cfg, "GET", PlattePath, "family")
	mustContain(t, body, "<h1>Platte erfassen</h1>", `name=foto accept="image/*" capture=environment multiple`, "BarcodeDetector", `inputmode=numeric`)
	_, body, _ = familyPage(t, Mehr, cfg, "GET", "/mehr", "family")
	mustContain(t, body, "Platte erfassen") // a box without Musik: on Mehr itself
	cfg.MusicInbox = t.TempDir()            // owner 2026-10-26: else under Musik, not on Mehr
	_, body, _ = familyPage(t, Mehr, cfg, "GET", "/mehr", "family")
	mustNotContain(t, body, "Platte erfassen", "Discogs-Konto verbinden")
	mustContain(t, body, "Hochladen, Platten erfassen, Discogs")
	_, body, _ = familyPage(t, Musik, cfg, "GET", MusikPath, "family")
	mustContain(t, body, "<h2 id=m-p>Platten und Wünsche</h2>", `href=/platte>Platte erfassen</a>`, `href=/discogs>Dein Discogs-Konto</a>`, `?liste=liste-musik">Musik-Wunschliste</a>`)
	mustNotContain(t, body, "noch nicht eingerichtet")

	// the phone read the barcode: Discogs's pressing, to confirm
	rec := plattePost(t, h, map[string]string{"tun": "suchen", "barcode": "4006381333931"}, false)
	body = rec.Body.String()
	mustContain(t, body, "Gelesen: Barcode 4006381333931", "<b>Wanda – Amore</b>", "2014 · Germany · Vinyl, LP, Album · Problembär PB 011",
		"<span class=grund>Barcode stimmt</span>", `<img src="/platte/bild?r=11"`, `<input type=hidden name=release value=11>`, ">Das ist sie</button>")
	if len(*added) != 0 {
		t.Fatal("added before it was confirmed")
	}
	// a misread barcode is said, not searched
	rec = plattePost(t, h, map[string]string{"tun": "suchen", "barcode": "4006381333932"}, false)
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "stimmt nicht (Prüfziffer)") {
		t.Errorf("bad barcode → %d %q", rec.Code, rec.Body)
	}
	// no barcode: the model reads the photo; its unsure barcode is dropped
	rec = plattePost(t, h, map[string]string{"tun": "suchen"}, true)
	mustContain(t, rec.Body.String(), "Den Barcode hat Haus auf dem Foto nicht sicher gelesen.", "Gelesen: Katalognummer PB 011 · Label Problembär · Wanda – Amore",
		"<span class=grund>Katalognummer stimmt</span>")
	// the model busy: who has the card, and the way without it
	busy := Platte(StartConfig{Discogs: dc, Judge: modelFake(t, true)})
	rec = plattePost(t, busy, map[string]string{"tun": "suchen"}, true)
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "Die Grafikkarte nutzt gerade die Werkstatt bis 22:30.") {
		t.Errorf("busy → %d %q", rec.Code, rec.Body)
	}

	// confirmed: into the collection, the waiting wish ticked off
	w.Wuenschen(context.Background(), merken.Lied{Kuenstler: "Wanda", Titel: "Amore", Album: true}, "anna")
	rec = plattePost(t, h, map[string]string{"tun": "hinzufuegen", "release": "11", "kuenstler": "Wanda", "titel": "Amore"}, false)
	mustContain(t, rec.Body.String(), "„Wanda – Amore“ ist in deiner Discogs-Sammlung.", "ich habe „Wanda – Amore“ auf der Wunschliste abgehakt", "Nächste Platte")
	if strings.Join(*added, ",") != "11" {
		t.Errorf("added %v", *added)
	}
	if g, _ := w.Wuenschen(context.Background(), merken.Lied{Kuenstler: "Wanda", Titel: "Amore", Album: true}, "anna"); !g.Schon || g.Satz != "Hast du schon auf Vinyl: „Amore“ (2014)." {
		t.Errorf("on the shelf: %+v", g)
	}
	if code, _, _ := familyPage(t, Platte, StartConfig{}, "GET", PlattePath, "family"); code != http.StatusNotFound {
		t.Errorf("without Discogs → %d", code)
	}
	// Ben has not connected his own: the way to, and no adding to Anna's
	_, body, _ = familyPageAs(t, Platte, cfg, "GET", PlattePath, "ben", "family")
	mustContain(t, body, saysDiscogsFirst, `href=/discogs>Discogs verbinden</a>`)
	mustNotContain(t, body, "Fotos machen")
	req := httptest.NewRequest("POST", PlattePath, strings.NewReader("tun=hinzufuegen&release=11"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Remote-User", "ben")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict || len(*added) != 1 {
		t.Errorf("Ben adding → %d, %v", rec.Code, *added)
	}
}

// each person connects their own Discogs: to Discogs and back, their name
// said; Ben's return cannot finish Anna's; Trennen forgets it
func TestDiscogsVerbinden(t *testing.T) {
	dc, _ := discogsFake(t)
	os.Remove(filepath.Join(dc.Dir, "discogs-konten.json"))
	cfg := StartConfig{Discogs: dc, PublicURL: "https://haus.box"}
	h := Discogs(cfg)
	_, body, _ := familyPage(t, Discogs, cfg, "GET", DiscogsPath, "family")
	mustContain(t, body, "<h1>Discogs</h1>", "Nicht verbunden", `<form method=post action=/discogs data-ganz><input type=hidden name=tun value=verbinden>`)
	rec := post(t, h, DiscogsPath, url.Values{"tun": {"verbinden"}}, "anna", nil)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "https://www.discogs.test/oauth/authorize?oauth_token=rt" {
		t.Fatalf("%d %q", rec.Code, rec.Header().Get("Location"))
	}
	_, body, _ = familyPageAs(t, Discogs, cfg, "GET", DiscogsZurueckPath+"?oauth_token=rt&oauth_verifier=v", "ben", "family")
	mustContain(t, body, discogs.ErrFremd.Error())
	post(t, h, DiscogsPath, url.Values{"tun": {"verbinden"}}, "anna", nil)
	_, body, _ = familyPageAs(t, Discogs, cfg, "GET", DiscogsZurueckPath+"?oauth_token=rt&oauth_verifier=v", "anna", "family")
	mustContain(t, body, "Verbunden: Haus nutzt jetzt dein Discogs-Konto „mdopp“.", "Verbunden mit „mdopp“ seit", ">Trennen</button>")
	if _, ok := dc.For("anna"); !ok {
		t.Fatal("not kept")
	}
	_, body, _ = familyPageAs(t, Discogs, cfg, "GET", DiscogsZurueckPath+"?denied=rt", "ben", "family")
	mustContain(t, body, "abgelehnt")
	rec = post(t, h, DiscogsPath, url.Values{"tun": {"trennen"}}, "anna", nil)
	mustContain(t, rec.Body.String(), "Getrennt", "Nicht verbunden")
	if _, ok := dc.For("anna"); ok {
		t.Error("still connected")
	}
}
