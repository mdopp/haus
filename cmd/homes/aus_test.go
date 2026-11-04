package main

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/mdopp/homes/internal/abs"
	"github.com/mdopp/homes/internal/gesehen"
	"github.com/mdopp/homes/internal/modul"
	"github.com/mdopp/homes/internal/napi"
	"github.com/mdopp/homes/internal/podcasts"
	"github.com/mdopp/homes/internal/reitti"
	"github.com/mdopp/homes/internal/samba"
	"github.com/mdopp/homes/internal/werkstatt"
)

// leafSite is kannSite with every leaf module's backend (ADR 0001 H3).
func leafSite(t *testing.T) site {
	t.Helper()
	s := kannSite(t)
	dead := &abs.Client{URL: "http://127.0.0.1:1", Token: "abs"}
	s.podcasts = podcasts.Open(dead, t.TempDir())
	s.hh.Podcasts, s.napi.Haushalt.Podcasts = s.podcasts, s.podcasts
	s.gesehen = &gesehen.Service{Buecher: dead}
	s.napi.Reitti = &reitti.Reitti{URL: "http://127.0.0.1:1", Dir: t.TempDir()}
	s.napi.Sicherung = napi.NewSicherung(t.TempDir(), t.TempDir())
	s.samba = &samba.Dir{Path: t.TempDir()}
	s.gesundheit = "https://gesundheit.example"
	s.tickets = &werkstatt.Tickets{API: "http://127.0.0.1:1", Repos: []string{"mdopp/homes"}}
	s.napi.Whoami = func(b string) (string, bool) { return "anna", b == "dev1" }
	return s
}

// switched builds s as main does with HOMES_MODULE_AUS=aus.
func switched(t *testing.T, s site, aus string) (site, *modul.Liste) {
	t.Helper()
	l, err := modul.Bauen(ausschalten(&s, katalog(s, ""), aus))
	if err != nil {
		t.Fatal(err)
	}
	s.napi.Module = l
	return s, l
}

// ADR 0001 §8, the leaf modules (H3): each one off in turn answers its Aus
// or the surface's 404, never its handler; its entries are gone; /napi/kann
// has it an:false, warum aus; every other leaf module is as before.
func TestEachLeafModuleOff(t *testing.T) {
	leaves := []string{"gesundheit", "werkstatt", "netzlaufwerk", "podcasts", "gesehen", "standort", "sicherung"}
	// what each serves while on, and what it answers off: a path and a word on it
	type probe struct{ path, on, off string }
	probes := map[string][]probe{
		"gesundheit":   {{"/mehr", "gesundheit.example/werte", "!gesundheit.example/werte"}},
		"werkstatt":    {{"/box", `box-werkstatt`, "!box-werkstatt"}},
		"netzlaufwerk": {{"/mehr", `href="/netzlaufwerk"`, `!href="/netzlaufwerk"`}, {"/netzlaufwerk", "Passwort", "noch nicht eingerichtet"}},
		"podcasts":     {{"/mehr", `href="/podcasts"`, `!href="/podcasts"`}, {"/podcasts", "Podcasts", "#404"}},
		"gesehen":      {{"/mehr", `href="/gesehen"`, `!href="/gesehen"`}, {"/gesehen", "Gesehen", "#404"}},
		"standort":     {{"/mehr", `href="/standort"`, `!href="/standort"`}, {"/standort", "Reitti", "noch nicht eingerichtet"}, {"napi:/napi/standort", "#200", "#503"}},
		"sicherung":    {{"/mehr", `href="/daten-sichern"`, `!href="/daten-sichern"`}, {"/daten-sichern", "WhatsApp", "nicht eingerichtet"}, {"napi:/napi/sicherung/stand", "#200", "#503"}},
	}
	check := func(t *testing.T, s site, p probe, want string) {
		t.Helper()
		h := routes(s)
		var code int
		var body string
		if path, ok := strings.CutPrefix(p.path, "napi:"); ok {
			code, body = napiRaw(h, path)
		} else {
			rec := as(h, "GET", p.path, "anna", "family,admins", nil)
			code, body = rec.Code, rec.Body.String()
		}
		switch {
		case strings.HasPrefix(want, "#"):
			if got := "#" + itoa(code); got != want {
				t.Errorf("%s → %s, want %s", p.path, got, want)
			}
		case strings.HasPrefix(want, "!"):
			if strings.Contains(body, want[1:]) {
				t.Errorf("%s still has %q", p.path, want[1:])
			}
		case !strings.Contains(body, want):
			t.Errorf("%s (%d) lacks %q", p.path, code, want)
		}
	}
	on, _ := switched(t, leafSite(t), "")
	for _, name := range leaves {
		for _, p := range probes[name] {
			check(t, on, p, p.on)
		}
	}
	for _, name := range leaves {
		t.Run(name, func(t *testing.T) {
			off, l := switched(t, leafSite(t), name)
			for _, p := range probes[name] {
				check(t, off, p, p.off)
			}
			for _, m := range l.Kann(modul.Wuensche{}).Module {
				switch {
				case m.Name == name && (m.An || m.Warum != modul.Aus || m.SpurID != "aus"):
					t.Errorf("kann: %s is %+v, want off, aus", name, m)
				case m.Name != name && strings.Contains(strings.Join(leaves, ","), m.Name) && !m.An:
					t.Errorf("kann: %s went off with %s", m.Name, name)
				}
			}
			for _, other := range leaves {
				if other == name {
					continue
				}
				for _, p := range probes[other] {
					check(t, off, p, p.on)
				}
			}
		})
	}
}

// The leaf modules' background work does not start while they are off.
func TestLeafWorkOnlyWhileOn(t *testing.T) {
	off, _ := switched(t, leafSite(t), "podcasts, sicherung")
	if off.podcasts != nil || off.hh.Podcasts != nil || off.napi.Haushalt.Podcasts != nil || off.napi.Sicherung != nil {
		t.Errorf("an off module kept its backend: podcasts %v %v %v, sicherung %v", off.podcasts, off.hh.Podcasts, off.napi.Haushalt.Podcasts, off.napi.Sicherung)
	}
	on, _ := switched(t, leafSite(t), "")
	if on.podcasts == nil || on.napi.Sicherung == nil {
		t.Error("nothing switched off, and still a backend is gone")
	}
}

// An unknown name, or one that cannot be switched off yet, is ignored.
func TestModuleAusIgnoresWhatItCannot(t *testing.T) {
	s, l := switched(t, leafSite(t), "gibtsnicht, kalender,, ")
	if !l.An("kalender") || s.hh.Calendar == nil {
		t.Error("kalender went off, which H3 cannot switch yet")
	}
	for _, m := range l.Module() {
		if m.Warum == modul.Aus {
			t.Errorf("%s is aus", m.Name)
		}
	}
	// a module that is missing stays fehlt, also when it is named
	_, l = switched(t, kannSite(t), "standort")
	if m, _ := l.Eins("standort"); m.Warum != modul.Fehlt {
		t.Errorf("standort without Reitti, named off: %s", m.Warum)
	}
}

func napiRaw(h http.Handler, path string) (int, string) {
	req := httptest.NewRequest("GET", path, nil)
	req.Header.Set("Authorization", "Bearer dev1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func itoa(n int) string { return strconv.Itoa(n) }
