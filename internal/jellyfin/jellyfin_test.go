package jellyfin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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
	if auth != `MediaBrowser Token="k", Client="Haus"` {
		t.Errorf("auth %q", auth)
	}
	if _, _, err := (&Client{URL: srv.URL + "/nix", Token: "k"}).Find(ctx, "a", "b"); err == nil {
		t.Error("a 404 is no answer")
	}
	if Plain("Ärzte & Co.") != "arzteundco" {
		t.Errorf("%q", Plain("Ärzte & Co."))
	}
}
