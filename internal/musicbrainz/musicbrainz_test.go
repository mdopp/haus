package musicbrainz

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/haus/internal/merken"
)

func rel(kind, u string) map[string]any {
	return map[string]any{"type": kind, "url": map[string]string{"resource": u}}
}

// a MusicBrainz that knows Wanda's Salbei: on a single and on an album to
// come, the album on a label with a Bandcamp, the band with a homepage
func fake(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("fmt") != "json" {
			http.Error(w, "fmt", 400)
			return
		}
		ans := func(v any) { json.NewEncoder(w).Encode(v) }
		wanda := []map[string]any{{"name": "Wanda", "artist": map[string]string{"id": "a1", "name": "Wanda"}}}
		switch p := r.URL.Path; {
		case p == "/ws/2/recording":
			if !strings.Contains(r.URL.Query().Get("query"), `recording:"Salbei" AND artist:"Wanda"`) {
				ans(map[string]any{"recordings": []any{}})
				return
			}
			ans(map[string]any{"recordings": []any{
				map[string]any{"title": "Salbei", "artist-credit": []map[string]any{{"name": "Gärtner", "artist": map[string]string{"id": "x"}}}, "releases": []any{}},
				map[string]any{"title": "Salbei", "artist-credit": wanda, "releases": []any{
					map[string]any{"id": "r-single", "title": "Salbei", "date": "2026-10-02", "release-group": map[string]string{"id": "g-single", "primary-type": "Single"}},
					map[string]any{"id": "r-album", "title": "Was mich umbringt", "date": "2027-04-09", "release-group": map[string]string{"id": "g-album", "primary-type": "Album"}},
				}}}})
		case p == "/ws/2/release/r-album":
			ans(map[string]any{"label-info": []any{map[string]any{"label": map[string]string{"id": "l1", "name": "Problembär"}}},
				"relations": []any{rel("purchase for download", "https://music.apple.com/at/album/1"), rel("streaming", "https://open.spotify.com/x")}})
		case p == "/ws/2/release-group/g-album":
			ans(map[string]any{"relations": []any{}})
		case p == "/ws/2/artist/a1":
			ans(map[string]any{"relations": []any{rel("official homepage", "https://www.wandamusik.com/"), rel("official homepage", "http://wandamusik.com"),
				rel("social network", "https://instagram.com/wanda"), rel("purchase for download", "https://mora.jp/x")}})
		case p == "/ws/2/label/l1":
			ans(map[string]any{"relations": []any{rel("bandcamp", "https://problembaer.bandcamp.com/"), rel("purchase for mail-order", "https://www.jpc.de/x")}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSuchen(t *testing.T) {
	c := &Client{Base: fake(t).URL, Fetch: merken.Fetcher{AllowPrivate: true}, Pause: time.Millisecond}
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	f, err := c.Suchen(context.Background(), "Wanda", "Salbei", false, now)
	if err != nil || !f.Gefunden || f.Album != "Was mich umbringt" || f.Datum != "2027-04-09" || !f.Kommt || f.Label != "Problembär" {
		t.Fatalf("%+v %v", f, err)
	}
	var got []string
	for _, l := range f.Links {
		got = append(got, l.Wo+" | "+l.Was+" | "+l.URL)
	}
	want := []string{
		"Bandcamp (Label Problembär) | Platte und Download, meist sofort | https://problembaer.bandcamp.com/",
		"JPC | Versand (Platte, CD) | https://www.jpc.de/x",
		"Band | Seite, oft mit Shop | https://www.wandamusik.com/",
		"Apple Music | Download | https://music.apple.com/at/album/1",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("links:\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if _, err := c.Suchen(context.Background(), "Wanda", "Gibtsnicht", false, now); err != ErrNichts {
		t.Errorf("unknown: %v", err)
	}
}
