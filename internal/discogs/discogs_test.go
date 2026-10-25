package discogs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEAN(t *testing.T) {
	for code, ok := range map[string]bool{"4260085877419": false, "4006381333931": true, "036000291452": true, "96385074": true, "4006381333932": false, "123": false, "4 006381 333931": true} {
		if EAN(code) != ok {
			t.Errorf("EAN(%q) = %v", code, !ok)
		}
	}
}

// a Discogs that knows one record by barcode, one by catalogue number, a
// collection on two pages, and keeps the wantlist
func fake(t *testing.T) (*Client, *[]string) {
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Discogs token=tok" || !strings.HasPrefix(r.Header.Get("User-Agent"), "Haus/") {
			w.WriteHeader(401)
			json.NewEncoder(w).Encode(map[string]string{"message": "You must authenticate"})
			return
		}
		calls = append(calls, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		ans := func(v any) { json.NewEncoder(w).Encode(v) }
		q := r.URL.Query()
		switch p := r.URL.Path; {
		case p == "/oauth/identity":
			ans(map[string]string{"username": "mdopp"})
		case p == "/database/search" && q.Get("barcode") == "4006381333931":
			ans(map[string]any{"results": []any{map[string]any{"id": 11, "title": "Wanda (2) - Amore", "year": "2014", "country": "Germany", "format": []string{"Vinyl", "LP"}, "label": []string{"Problembär"}, "catno": "PB 011"}}})
		case p == "/database/search" && q.Get("catno") == "PB 011":
			ans(map[string]any{"results": []any{map[string]any{"id": 11, "title": "Wanda - Amore"}, map[string]any{"id": 12, "title": "Wanda - Amore", "country": "Austria"}}})
		case p == "/database/search" && q.Get("type") == "master":
			ans(map[string]any{"results": []any{map[string]any{"id": 900, "title": "Wanda - Bussi"}}})
		case p == "/masters/900":
			ans(map[string]any{"main_release": 13})
		case p == "/database/search":
			ans(map[string]any{"results": []any{}})
		case p == "/users/mdopp/collection/folders/1/releases/11" && r.Method == "POST":
			w.WriteHeader(201)
			ans(map[string]any{"instance_id": 1})
		case p == "/users/mdopp/collection/folders/0/releases":
			if q.Get("page") == "1" {
				ans(map[string]any{"pagination": map[string]int{"pages": 2}, "releases": []any{map[string]any{"id": 11, "basic_information": map[string]any{"title": "Amore", "year": 2014, "master_id": 800, "artists": []any{map[string]string{"name": "Wanda (2)"}}}}}})
			} else {
				ans(map[string]any{"pagination": map[string]int{"pages": 2}, "releases": []any{map[string]any{"id": 20, "basic_information": map[string]any{"title": "Niveau weshalb warum", "artists": []any{map[string]string{"name": "Wanda"}}}}}})
			}
		case strings.HasPrefix(p, "/users/mdopp/wants/"):
			if r.Method == "DELETE" && strings.HasSuffix(p, "/99") {
				w.WriteHeader(404)
				return
			}
			w.WriteHeader(201)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return &Client{Base: srv.URL, Token: "tok"}, &calls
}

func TestFindenAndShelf(t *testing.T) {
	c, calls := fake(t)
	ctx := context.Background()
	ts, err := c.Finden(ctx, Hinweise{Barcode: "4 006381 333931", Katalognummer: "PB 011"})
	if err != nil || len(ts) != 1 || ts[0].ID != 11 || ts[0].Grund != "Barcode stimmt" || ts[0].Kuenstler() != "Wanda" || ts[0].Titel() != "Amore" {
		t.Fatalf("%+v %v", ts, err)
	}
	ts, _ = c.Finden(ctx, Hinweise{Katalognummer: "PB 011", Label: "Problembär", Kuenstler: "Wanda", Titel: "Amore"})
	if len(ts) != 2 || ts[0].Grund != "Katalognummer stimmt" || ts[1].Country != "Austria" {
		t.Fatalf("by catno: %+v", ts)
	}
	if err := c.Hinzufuegen(ctx, 11); err != nil {
		t.Fatal(err)
	}
	shelf, err := c.Sammlung(ctx)
	if err != nil || len(shelf) != 2 || shelf[0].Kuenstler != "Wanda" || shelf[0].Master != 800 || shelf[1].Titel != "Niveau weshalb warum" {
		t.Fatalf("%+v %v", shelf, err)
	}
	a, ok, err := c.Album(ctx, "Wanda", "Bussi")
	if err != nil || !ok || a.ID != 13 || a.Master != 900 {
		t.Fatalf("album %+v %v %v", a, ok, err)
	}
	if err := c.Wuenschen(ctx, 13); err != nil {
		t.Fatal(err)
	}
	if err := c.Entwuenschen(ctx, 99); err != nil {
		t.Errorf("not on the list: %v", err)
	}
	identity := 0
	for _, call := range *calls {
		if strings.HasPrefix(call, "GET /oauth/identity") {
			identity++
		}
	}
	if identity != 1 {
		t.Errorf("asked who it is %d times", identity)
	}
	if _, err := (&Client{Base: c.Base, Token: "falsch"}).User(ctx); err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("bad token: %v", err)
	}
}
