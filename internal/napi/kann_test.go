package napi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/homes/internal/laib"
	"github.com/mdopp/homes/internal/laib/laibfake"
	"github.com/mdopp/homes/internal/modul"
)

// kannBox: Geräte on, Standortverlauf missing, Podcasts switched off; a
// fake Laib that takes wishes; the device dev1 is anna's.
func kannBox(t *testing.T) (http.Handler, *laibfake.Laib) {
	t.Helper()
	fresh, f := kannBoxes(t)
	return fresh(), f
}

// kannBoxes makes a handler with a client of its own each call: a client
// keeps Laib's stand a minute.
func kannBoxes(t *testing.T) (func() http.Handler, *laibfake.Laib) {
	t.Helper()
	f := laibfake.New()
	srv := f.Server()
	t.Cleanup(srv.Close)
	l, err := modul.Bauen([]modul.Modul{
		{Name: "geraete", Titel: "Geräte", Backend: "home-assistant", App: modul.Angebot{Widgets: []string{"geraet"}}},
		{Name: "standort", Titel: "Standortverlauf", Backend: "reitti", Warum: modul.Fehlt, Grund: "Auf dieser Box läuft kein Reitti.",
			App: modul.Angebot{Teilen: []string{"standort-import"}, Mehr: []string{"standortverlauf"}}},
		{Name: "podcasts", Titel: "Podcasts", Backend: "audiobookshelf", Warum: modul.Aus, Grund: "Auf dieser Box ausgeschaltet."},
	})
	if err != nil {
		t.Fatal(err)
	}
	return func() http.Handler {
		return Handler(Config{Laib: &laib.Client{URL: srv.URL, Token: "homes"}, Module: l, Validate: StaticValidator([]string{"dev1"}),
			Whoami: func(b string) (string, bool) { return "anna", b == "dev1" }})
	}, f
}

type kannEntry struct {
	Name      string `json:"name"`
	An        bool   `json:"an"`
	SpurID    string `json:"spur_id"`
	Spur      string `json:"spur"`
	Anfordern bool   `json:"anfordern"`
	Wunsch    *struct {
		Stand string `json:"stand"`
	} `json:"wunsch"`
	Error string `json:"error"`
}

func kannOf(t *testing.T, h http.Handler) (map[string]kannEntry, string) {
	t.Helper()
	code, body := raw(t, h, "GET", "/napi/kann", "dev1", "")
	var out struct{ Module []kannEntry }
	if code != 200 || json.Unmarshal([]byte(body), &out) != nil {
		t.Fatalf("GET /napi/kann → %d %s", code, body)
	}
	byName := map[string]kannEntry{}
	for _, e := range out.Module {
		byName[e.Name] = e
	}
	return byName, body
}

func anfordern(t *testing.T, h http.Handler, bearer, name string) (int, kannEntry) {
	t.Helper()
	code, body := raw(t, h, "POST", "/napi/kann/anfordern", bearer, `{"modul":"`+name+`"}`)
	var e kannEntry
	json.Unmarshal([]byte(body), &e)
	return code, e
}

// The traces' words follow the wish (ADR 0001 §10): fehlt and aus offer
// „Hier anfordern“; a wish filed is angefordert, then its states from
// Laib; abgelehnt offers it again; a module on has no trace.
func TestKannTracesFollowTheWish(t *testing.T) {
	fresh, f := kannBoxes(t)
	h := fresh()
	k, _ := kannOf(t, h)
	if g := k["geraete"]; g.SpurID != "" || g.Spur != "" || g.Anfordern {
		t.Errorf("a module on has a trace: %+v", g)
	}
	if s := k["standort"]; s.SpurID != "fehlt" || s.Spur != "auf dieser Box nicht eingerichtet" || !s.Anfordern || s.Wunsch != nil {
		t.Errorf("standort: %+v", s)
	}
	if p := k["podcasts"]; p.SpurID != "aus" || p.Spur != "ausgeschaltet" || !p.Anfordern {
		t.Errorf("podcasts: %+v", p)
	}
	code, e := anfordern(t, h, "dev1", "standort")
	if code != 200 || e.SpurID != "angefordert" || e.Spur != "angefordert" || e.Anfordern || e.Wunsch == nil {
		t.Fatalf("anfordern → %d %+v", code, e)
	}
	if code, again := anfordern(t, h, "dev1", "standort"); code != 200 || again.SpurID != "angefordert" {
		t.Errorf("a second request → %d %+v", code, again)
	}
	if n := strings.Count(strings.Join(f.Gefragt, "|"), "standort=reitti"); n == 0 {
		t.Errorf("Laib was never asked with the backend: %v", f.Gefragt)
	}
	for stand, want := range map[string]string{"wartet": "wartet auf Freigabe", "eingerichtet": "wird eingerichtet", "abgelehnt": "abgelehnt", "umgesetzt": "auf dieser Box nicht eingerichtet"} {
		f.Setze("standort", stand, "")
		k, _ := kannOf(t, fresh())
		s := k["standort"]
		if s.Spur != want {
			t.Errorf("%s: the trace says %q, want %q", stand, s.Spur, want)
		}
		if offers := stand == "abgelehnt" || stand == "umgesetzt"; s.Anfordern != offers {
			t.Errorf("%s: anfordern %v, want %v", stand, s.Anfordern, offers)
		}
	}
}

func TestAnfordernRefuses(t *testing.T) {
	h, f := kannBox(t)
	if code, _ := anfordern(t, h, "", "standort"); code != 401 {
		t.Errorf("without a device → %d, want 401", code)
	}
	for _, name := range []string{"geraete", "gibtsnicht", ""} {
		if code, e := anfordern(t, h, "dev1", name); code != 404 || e.Error == "" {
			t.Errorf("%q → %d %+v, want 404 with a sentence", name, code, e)
		}
	}
	f.Kaputt = true
	if code, e := anfordern(t, h, "dev1", "standort"); code != 502 || !strings.Contains(e.Error, "nicht aufschreiben") {
		t.Errorf("Laib failing → %d %+v", code, e)
	}
	f.Kaputt, f.Warte = false, 300*time.Millisecond
	was := wishWait
	wishWait = 50 * time.Millisecond
	defer func() { wishWait = was }()
	if code, e := anfordern(t, h, "dev1", "podcasts"); code != 504 || !strings.Contains(e.Error, "nicht rechtzeitig") {
		t.Errorf("Laib slow → %d %+v", code, e)
	}
}

// An older Laib (no modul on laib_wish): the traces say fehlt or aus and
// offer nothing; a request is refused in words.
func TestKannWithAnOlderLaib(t *testing.T) {
	h, f := kannBox(t)
	f.Ohne = true
	k, _ := kannOf(t, h)
	if s := k["standort"]; s.Anfordern || s.Spur != "auf dieser Box nicht eingerichtet" {
		t.Errorf("standort: %+v", s)
	}
	if code, e := anfordern(t, h, "dev1", "standort"); code != 503 || e.Error == "" {
		t.Errorf("anfordern → %d %+v, want 503 with a sentence", code, e)
	}
}

// The ETag changes when a wish's state does.
func TestKannETagFollowsTheWish(t *testing.T) {
	h, _ := kannBox(t)
	_, before := kannOf(t, h)
	anfordern(t, h, "dev1", "standort")
	_, after := kannOf(t, h)
	if before == after {
		t.Error("the answer did not change with the wish")
	}
}
