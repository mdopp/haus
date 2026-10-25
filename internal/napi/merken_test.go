package napi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mdopp/haus/internal/caldav"
	"github.com/mdopp/haus/internal/caldav/davfake"
	"github.com/mdopp/haus/internal/einkauf"
	"github.com/mdopp/haus/internal/haushalt"
	"github.com/mdopp/haus/internal/merken"
	"github.com/mdopp/haus/internal/notes"
)

func shareText(t *testing.T, h http.Handler, bearer, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest("POST", "/napi/merken", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

// haus#120: a page shared to the app is kept as a note, as "Aus Link
// merken" keeps it: the link out of the shared text, the words around it
// the hint; once only; a text without a link, or a page that does not
// answer, a sentence; only for a paired device.
func TestShareToHaus(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/weg" {
			http.Error(w, "nein", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<html><head><title>Kürbissuppe</title><script type="application/ld+json">{"@type":"Recipe","name":"Kürbissuppe","recipeYield":"4",` +
			`"recipeIngredient":["1 Hokkaido","1 l Brühe"],"recipeInstructions":["Kochen."]}</script></head><body></body></html>`))
	}))
	defer site.Close()
	nd, err := notes.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := Handler(Config{Validate: StaticValidator([]string{"dev1"}),
		Haushalt: haushalt.Config{Notes: nd, Fetcher: merken.Fetcher{AllowPrivate: true}, PublicURL: "https://haus.box.test"}})

	code, out := shareText(t, h, "dev1", `{"text":"Kürbissuppe #herbst `+site.URL+`/rezept"}`)
	if code != 200 || out["art"] != "rezept" || out["titel"] != "Kürbissuppe" || out["schon"] != false || !strings.HasPrefix(out["kurz"].(string), "Rezept gemerkt: Kürbissuppe") {
		t.Fatalf("%d %v", code, out)
	}
	if n, _ := out["notiz"].(string); !strings.HasPrefix(n, "https://haus.box.test/notizen/") {
		t.Errorf("notiz %q", n)
	}
	got, _ := nd.Recent(t.Context(), 5)
	if len(got) != 1 || !got[0].Kopf.Has([]string{"herbst"}) {
		t.Errorf("notes %+v", got)
	}
	if code, out := shareText(t, h, "dev1", `{"text":"`+site.URL+`/rezept."}`); code != 200 || out["schon"] != true {
		t.Errorf("again → %d %v", code, out)
	}
	if code, out := shareText(t, h, "dev1", `{"text":"nur Text, kein Link"}`); code != 400 || !strings.Contains(out["error"].(string), "kein Link") {
		t.Errorf("no link → %d %v", code, out)
	}
	if code, out := shareText(t, h, "dev1", `{"text":"ftp://x.de und sonst nichts"}`); code != 400 || !strings.Contains(out["error"].(string), "kein Link") {
		t.Errorf("no web link → %d %v", code, out)
	}
	if code, out := shareText(t, h, "dev1", `{"text":"`+site.URL+`/weg"}`); code != 200 || out["teilweise"] != true {
		t.Errorf("a page that fails → %d %v", code, out) // kept as link and title, said so
	}
	if code, _ := shareText(t, h, "dev1", `kein json`); code != 400 {
		t.Errorf("bad body → %d", code)
	}
	if code, _ := shareText(t, h, "", `{"text":"`+site.URL+`"}`); code != http.StatusUnauthorized {
		t.Errorf("unpaired → %d", code)
	}
	if code, _ := shareText(t, Handler(Config{Validate: StaticValidator([]string{"dev1"})}), "dev1", `{"text":"`+site.URL+`"}`); code != http.StatusNotFound {
		t.Errorf("no notes → %d", code)
	}
	for in, want := range map[string][2]string{
		"Schau mal: https://example.com/a?b=1, toll!": {"https://example.com/a?b=1", "Schau mal: toll!"},
		"„https://x.de/r“ #kochen":                    {"https://x.de/r", "#kochen"},
		"https://x.de/(a)":                            {"https://x.de/(a", ""},
	} {
		if l, hint := sharedLink(in); l != want[0] || hint != want[1] {
			t.Errorf("sharedLink(%q) = %q, %q", in, l, hint)
		}
	}
}

// owner 2026-10-25: a shared song goes on the music wish list, for who
// shared it; a YouTube video that is no music video stays a note.
func TestShareASong(t *testing.T) {
	yt := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		title := "Wanda - Salbei (Official Video)"
		if strings.Contains(r.URL.Query().Get("url"), "kochen") {
			title = "Lasagne kochen"
		}
		json.NewEncoder(w).Encode(map[string]string{"title": title, "author_name": "wandamusikVEVO"})
	}))
	defer yt.Close()
	was := merken.YouTubeBase
	merken.YouTubeBase = yt.URL
	defer func() { merken.YouTubeBase = was }()
	dav := davfake.New("haus", "pw")
	defer dav.Close()
	l := einkauf.Open(&caldav.Client{URL: dav.URL, User: "haus", Password: "pw"})
	nd, _ := notes.Open(t.TempDir())
	h := Handler(Config{Validate: StaticValidator([]string{"dev1"}), Whoami: func(string) (string, bool) { return "anna", true },
		Haushalt: haushalt.Config{Notes: nd, Fetcher: merken.Fetcher{AllowPrivate: true}, PublicURL: "https://haus.box.test", Einkauf: l, Wuensche: &einkauf.Wuensche{Listen: l}}})
	code, out := shareText(t, h, "dev1", `{"text":"https://youtube.com/watch?v=Ca7RfL4m744&is=x"}`)
	if code != 200 || out["art"] != "musik" || out["kurz"] != "Auf der Musik-Wunschliste: „Wanda – Salbei“." || out["notiz"] != "https://haus.box.test/einkauf?liste=liste-musik" {
		t.Fatalf("%d %v", code, out)
	}
	items, _ := l.Andere(einkauf.MusikListe).Lesen(t.Context())
	if len(items) != 1 || einkauf.Wer(items[0].Herkunft) != "anna" {
		t.Errorf("%+v", items)
	}
	if code, out := shareText(t, h, "dev1", `{"text":"https://youtube.com/watch?v=Ca7RfL4m744"}`); code != 200 || out["schon"] != true {
		t.Errorf("twice → %d %v", code, out)
	}
	if recent, _ := nd.Recent(t.Context(), 5); len(recent) != 0 {
		t.Errorf("a song made a note: %+v", recent)
	}
}
