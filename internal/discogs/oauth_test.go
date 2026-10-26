package discogs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// a Discogs that runs the OAuth dance for the app key "ck"/"cs" and knows
// who an access token belongs to
func oauthFake(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "OAuth ") || !strings.Contains(auth, `oauth_consumer_key="ck"`) || !strings.Contains(auth, `oauth_signature_method="PLAINTEXT"`) {
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/oauth/request_token":
			if !strings.Contains(auth, `oauth_signature="cs&"`) || !strings.Contains(auth, `oauth_callback="https%3A%2F%2Fhaus.box%2Fdiscogs%2Fzurueck"`) {
				w.WriteHeader(401)
				return
			}
			w.Write([]byte("oauth_token=rt&oauth_token_secret=rs&oauth_callback_confirmed=true"))
		case "/oauth/access_token":
			if !strings.Contains(auth, `oauth_signature="cs&rs"`) || !strings.Contains(auth, `oauth_verifier="v1"`) || !strings.Contains(auth, `oauth_token="rt"`) {
				w.WriteHeader(401)
				return
			}
			w.Write([]byte("oauth_token=at&oauth_token_secret=as"))
		case "/oauth/identity":
			if !strings.Contains(auth, `oauth_token="at"`) || !strings.Contains(auth, `oauth_signature="cs&as"`) {
				w.WriteHeader(401)
				return
			}
			json.NewEncoder(w).Encode(map[string]string{"username": "anna_vinyl"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// owner 2026-10-26: one Discogs account per person, connected through the
// household's app; a return that is not one's own connection is refused.
func TestVerbinden(t *testing.T) {
	srv := oauthFake(t)
	dir := t.TempDir()
	r := &Regale{App: &App{Key: "ck", Secret: "cs", Base: srv.URL, Web: "https://www.discogs.test"}, Dir: dir}
	ctx := context.Background()
	if _, ok := r.For("anna"); ok {
		t.Fatal("connected before connecting")
	}
	authorize, err := r.Verbinden(ctx, "anna", "https://haus.box/discogs/zurueck")
	if err != nil || authorize != "https://www.discogs.test/oauth/authorize?oauth_token=rt" {
		t.Fatalf("%q %v", authorize, err)
	}
	if _, err := r.Fertig(ctx, "ben", "rt", "v1"); err != ErrFremd {
		t.Errorf("Ben finishing Anna's: %v", err)
	}
	if _, err := r.Fertig(ctx, "anna", "rt", "v1"); err != ErrFremd { // Ben's try used it up
		t.Errorf("used up: %v", err)
	}
	r.Verbinden(ctx, "anna", "https://haus.box/discogs/zurueck")
	name, err := r.Fertig(ctx, "anna", "rt", "v1")
	if err != nil || name != "anna_vinyl" {
		t.Fatalf("%q %v", name, err)
	}
	rg, ok := r.For("anna")
	if !ok || rg.C.OAuthToken != "at" {
		t.Fatalf("%+v %v", rg, ok)
	}
	if u, err := rg.C.User(ctx); err != nil || u != "anna_vinyl" {
		t.Errorf("as anna: %q %v", u, err)
	}
	if _, ok := r.For("ben"); ok {
		t.Error("Ben has Anna's shelf")
	}
	fi, err := os.Stat(filepath.Join(dir, kontenFile))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("the tokens' file: %v %v", fi.Mode(), err)
	}
	if err := r.Trennen("anna"); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.For("anna"); ok {
		t.Error("still connected")
	}
	if _, ok := (*Regale)(nil).For("anna"); ok {
		t.Error("no app, no shelf")
	}
}
