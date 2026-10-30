package jellyfin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// haus#152: a person's films and series by their Jellyfin user (the name,
// case aside), asked with the key and userId; a series once, with its last
// episode; a person Jellyfin does not know is ErrNoUser; HatFilm finds a
// film or series by its whole name.
func TestGeschaut(t *testing.T) {
	var q map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != `MediaBrowser Token="key", Client="Haus"` {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/Users":
			json.NewEncoder(w).Encode([]any{map[string]any{"Name": "Anna", "Id": "ua"}})
		case r.URL.Path == "/Items" && r.URL.Query().Get("userId") != "":
			q = map[string]string{"userId": r.URL.Query().Get("userId"), "Filters": r.URL.Query().Get("Filters"), "SortBy": r.URL.Query().Get("SortBy")}
			json.NewEncoder(w).Encode(map[string]any{"Items": []any{
				map[string]any{"Id": "e2", "Name": "Der Plan", "Type": "Episode", "SeriesId": "s1", "SeriesName": "Dark", "ParentIndexNumber": 2, "IndexNumber": 3,
					"UserData": map[string]any{"Played": true, "LastPlayedDate": "2026-10-29T20:00:00Z"}},
				map[string]any{"Id": "e1", "Name": "Geheimnisse", "Type": "Episode", "SeriesId": "s1", "SeriesName": "Dark", "ParentIndexNumber": 1, "IndexNumber": 1,
					"UserData": map[string]any{"Played": true, "LastPlayedDate": "2026-10-01T20:00:00Z"}},
				map[string]any{"Id": "m1", "Name": "Dune", "Type": "Movie", "ProductionYear": 2021, "UserData": map[string]any{"Played": true, "LastPlayedDate": "2026-10-28T20:00:00Z"}},
			}})
		case r.URL.Path == "/Items":
			json.NewEncoder(w).Encode(map[string]any{"Items": []any{map[string]any{"Name": "Dune: Part Two"}, map[string]any{"Name": "Dune"}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := &Client{URL: srv.URL, Token: "key"}
	got, err := c.Geschaut(context.Background(), "anna", 10)
	if err != nil || len(got) != 2 {
		t.Fatalf("%+v %v", got, err)
	}
	if g := got[0]; !g.Serie || g.Name != "Dark" || g.Folge != "S2E3 · Der Plan" || g.Zuletzt.Day() != 29 {
		t.Errorf("series: %+v", g)
	}
	if g := got[1]; g.Serie || g.Name != "Dune" || g.Jahr != 2021 {
		t.Errorf("film: %+v", g)
	}
	if q["userId"] != "ua" || q["Filters"] != "IsPlayed" || q["SortBy"] != "DatePlayed" {
		t.Errorf("asked %v", q)
	}
	if got, _ := c.Geschaut(context.Background(), "anna", 1); len(got) != 1 {
		t.Errorf("n: %+v", got)
	}
	if _, err := c.Geschaut(context.Background(), "ben", 10); !errors.Is(err, ErrNoUser) {
		t.Errorf("ben: %v", err)
	}
	if _, err := (&Client{URL: srv.URL, Token: "x"}).Geschaut(context.Background(), "anna", 10); err == nil || errors.Is(err, ErrNoUser) {
		t.Errorf("refused: %v", err)
	}
	if name, ok, err := c.HatFilm(context.Background(), "dune"); err != nil || !ok || name != "Dune" {
		t.Errorf("HatFilm: %q %v %v", name, ok, err)
	}
	if _, ok, _ := c.HatFilm(context.Background(), "Arrival"); ok {
		t.Error("Arrival is not there")
	}
}
