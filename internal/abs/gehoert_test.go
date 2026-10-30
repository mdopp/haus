package abs

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/haus/internal/abs/absfake"
)

// haus#152: a person's books, last heard first, as ABS's findOne answers
// them; podcast episodes and books not begun left out; the person by name,
// case aside; a person ABS does not know is ErrNoUser.
func TestGehoerte(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer k" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/users":
			json.NewEncoder(w).Encode(map[string]any{"users": []any{map[string]any{"id": "u1", "username": "Anna"}}})
		case "/api/users/u1":
			json.NewEncoder(w).Encode(map[string]any{"mediaProgress": []any{
				map[string]any{"libraryItemId": "a", "mediaItemType": "book", "displayTitle": "Momo", "progress": 1, "isFinished": true, "lastUpdate": 1000, "finishedAt": 5000},
				map[string]any{"libraryItemId": "b", "mediaItemType": "book", "displayTitle": "Krabat", "progress": 0.4, "lastUpdate": 9000},
				map[string]any{"libraryItemId": "c", "episodeId": "e1", "mediaItemType": "podcastEpisode", "displayTitle": "Folge 3", "progress": 0.5, "lastUpdate": 9999},
				map[string]any{"libraryItemId": "d", "mediaItemType": "book", "displayTitle": "Nie begonnen", "progress": 0, "lastUpdate": 9999},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := &Client{URL: srv.URL, Token: "k"}
	got, err := c.Gehoerte(context.Background(), "anna", 10)
	if err != nil || len(got) != 2 || got[0].Titel != "Krabat" || got[1].Titel != "Momo" || !got[1].Fertig || got[1].Zuletzt != time.UnixMilli(5000) {
		t.Fatalf("%+v %v", got, err)
	}
	if got, _ := c.Gehoerte(context.Background(), "anna", 1); len(got) != 1 {
		t.Errorf("n: %+v", got)
	}
	if _, err := c.Gehoerte(context.Background(), "ben", 10); !errors.Is(err, ErrNoUser) {
		t.Errorf("ben: %v", err)
	}
	if _, err := (&Client{URL: srv.URL, Token: "x"}).Gehoerte(context.Background(), "anna", 10); err == nil || errors.Is(err, ErrNoUser) {
		t.Errorf("refused: %v", err)
	}
}

func TestHatBuch(t *testing.T) {
	fake := absfake.New("k")
	defer fake.Close()
	fake.Buch("Die unendliche Geschichte")
	c := &Client{URL: fake.URL, Token: "k"}
	plain := func(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }
	if name, ok, err := c.HatBuch(context.Background(), "die  unendliche geschichte", plain); err != nil || !ok || name != "Die unendliche Geschichte" {
		t.Errorf("%q %v %v", name, ok, err)
	}
	if _, ok, err := c.HatBuch(context.Background(), "Unendliche", plain); err != nil || ok {
		t.Errorf("a part of a title is not the book: %v %v", ok, err)
	}
}
