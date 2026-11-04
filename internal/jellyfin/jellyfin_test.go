package jellyfin

import (
	"context"
	"encoding/json"
	"image/color"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mdopp/homes/internal/jellyfin/jellyfinfake"
)

func TestFind(t *testing.T) {
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		if r.URL.Path != "/Items" || r.URL.Query().Get("IncludeItemTypes") != "Audio,MusicAlbum" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"Items": []map[string]any{
			{"Name": "Salbei", "Type": "Audio", "Album": "Ciao!", "AlbumArtist": "Wanda", "Artists": []string{"Wanda"}},
			{"Name": "Bologna", "Type": "Audio", "Album": "Amore", "AlbumArtist": "Various Artists", "Artists": []string{"Wanda"}},
			{"Name": "Amore", "Type": "MusicAlbum", "AlbumArtist": "Wanda"},
			{"Name": "Salbei", "Type": "Audio", "Album": "Kräuter", "AlbumArtist": "Gärtner"},
		}})
	}))
	defer srv.Close()
	c := &Client{URL: srv.URL, Token: "k"}
	ctx := context.Background()
	if tr, ok, err := c.Find(ctx, "Wanda", "Salbei"); err != nil || !ok || tr.Album != "Ciao!" || tr.Titel != "Salbei" {
		t.Errorf("Salbei: %+v %v %v", tr, ok, err)
	}
	if tr, ok, _ := c.Find(ctx, "wanda", "BOLOGNA"); !ok || tr.Album != "Amore" {
		t.Errorf("by the song's artist: %+v %v", tr, ok)
	}
	if tr, ok, _ := c.Find(ctx, "Wanda", "Amore"); !ok || tr.Album != "Amore" || tr.Titel != "" {
		t.Errorf("an album: %+v %v", tr, ok)
	}
	if _, ok, _ := c.Find(ctx, "Bilderbuch", "Salbei"); ok {
		t.Error("another artist's Salbei")
	}
	if auth != `MediaBrowser Token="k", Client="Homes"` {
		t.Errorf("auth %q", auth)
	}
	if _, _, err := (&Client{URL: srv.URL + "/nix", Token: "k"}).Find(ctx, "a", "b"); err == nil {
		t.Error("a 404 is no answer")
	}
}

// Zuletzt hinzugefügt on Musik (owner 2026-11-04): the newest albums, how many there are, and
// each cover through Homes; an id that is no Jellyfin id is never asked for.
func TestNeuesteUndBild(t *testing.T) {
	f := jellyfinfake.New()
	defer f.Close()
	f.Setze(jellyfinfake.Album{ID: strings.Repeat("a", 32), Name: "Ciao!", Kuenstler: "Wanda", Farbe: color.RGBA{1, 2, 3, 255}},
		jellyfinfake.Album{ID: strings.Repeat("b", 32), Name: "Ohne", Kuenstler: "X"})
	c := &Client{URL: f.URL, Token: "k"}
	ctx := context.Background()
	alben, n, err := c.Neueste(ctx, 6)
	if err != nil || n != 24 || len(alben) != 2 || alben[0] != (Album{ID: strings.Repeat("a", 32), Name: "Ciao!", Kuenstler: "Wanda", Bild: true}) || alben[1].Bild {
		t.Fatalf("%+v %d %v", alben, n, err)
	}
	if b, ct, err := c.Bild(ctx, strings.Repeat("a", 32)); err != nil || ct != "image/png" || len(b) == 0 {
		t.Errorf("cover: %s %v", ct, err)
	}
	for _, id := range []string{strings.Repeat("b", 32), "../../System/Info", ""} {
		if _, _, err := c.Bild(ctx, id); err == nil {
			t.Errorf("%q: a cover", id)
		}
	}
	f.Close()
	if _, _, err := c.Neueste(ctx, 6); err == nil {
		t.Error("Jellyfin away, no error")
	}
}
