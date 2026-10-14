package werkstatt

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// GitHub allows 60 unauthenticated requests an hour: the list is cached,
// so a page reload does not cost a request per repo and label.
func TestOpenIsCached(t *testing.T) {
	calls := 0
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		json.NewEncoder(w).Encode([]map[string]any{})
	}))
	t.Cleanup(gh.Close)
	tk := &Tickets{API: gh.URL, Repos: []string{"a/b", "c/d"}}
	for i := 0; i < 3; i++ {
		if _, err := tk.Open(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 4 { // 2 repos × 2 labels, once
		t.Fatalf("%d calls, want 4 (cached)", calls)
	}
}

func TestFromEnv(t *testing.T) {
	t.Setenv("HAUS_WERKSTATT_REPOS", "")
	if FromEnv() != nil {
		t.Fatal("no repos, no Werkstatt card")
	}
	t.Setenv("HAUS_WERKSTATT_REPOS", " mdopp/laib , mdopp/haus,")
	tk := FromEnv()
	if tk == nil || len(tk.Repos) != 2 || tk.Repos[1] != "mdopp/haus" || tk.API != "https://api.github.com" {
		t.Fatalf("%+v", tk)
	}
}
