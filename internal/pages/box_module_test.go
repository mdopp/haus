package pages

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/homes/internal/laib"
	"github.com/mdopp/homes/internal/laib/laibfake"
	"github.com/mdopp/homes/internal/modul"
)

// A switch Laib does not answer in time ends with an honest sentence.
func TestModuleSwitchTimesOut(t *testing.T) {
	f := laibfake.New()
	f.Warte = 300 * time.Millisecond
	srv := f.Server()
	defer srv.Close()
	l, err := modul.Bauen([]modul.Modul{{Name: "gesehen", Titel: "Gesehen & Gehört", Schaltbar: true}})
	if err != nil {
		t.Fatal(err)
	}
	was := wishWait
	wishWait = 30 * time.Millisecond
	defer func() { wishWait = was }()
	req := httptest.NewRequest("POST", "/module/gesehen", strings.NewReader(url.Values{"tun": {"aus"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Remote-User", "anna")
	req.Header.Set("Remote-Groups", "admins")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	rec := httptest.NewRecorder()
	ModulSeite(StartConfig{Module: l, Laib: &laib.Client{URL: srv.URL, Token: "homes"}})(rec, req)
	if rec.Code != 504 || !strings.Contains(rec.Body.String(), "nicht rechtzeitig") {
		t.Errorf("Laib slow → %d %s", rec.Code, rec.Body)
	}
}
