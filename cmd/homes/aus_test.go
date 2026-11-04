package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/homes/internal/abs"
	"github.com/mdopp/homes/internal/bote"
	"github.com/mdopp/homes/internal/caldav"
	"github.com/mdopp/homes/internal/caldav/davfake"
	"github.com/mdopp/homes/internal/einkauf"
	"github.com/mdopp/homes/internal/gesehen"
	"github.com/mdopp/homes/internal/haushalt"
	"github.com/mdopp/homes/internal/modul"
	"github.com/mdopp/homes/internal/napi"
	"github.com/mdopp/homes/internal/podcasts"
	"github.com/mdopp/homes/internal/reitti"
	"github.com/mdopp/homes/internal/samba"
	"github.com/mdopp/homes/internal/usage"
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
	// H4: the lists and the music wish list on the one Radicale, the music
	// inbox, Paperless's inbox and its mail address
	ctx := context.Background()
	if err := errors.Join(s.hh.Calendar.EnsureList(ctx, caldav.Einkauf, einkauf.Name),
		s.hh.Calendar.EnsureList(ctx, einkauf.MusikListe, einkauf.MusikName)); err != nil {
		t.Fatal(err)
	}
	s.haushalt(func(h *haushalt.Config) {
		h.Einkauf = einkauf.Open(s.hh.Calendar)
		h.Wuensche = &einkauf.Wuensche{Listen: h.Einkauf, Dir: t.TempDir()}
	})
	s.musik, s.napi.UploadDir, s.docsMail = t.TempDir(), t.TempDir(), "scan@example.org"
	// H6: the Bote with the house's Signal number, the counts
	s.bote, s.boteSignal = &bote.Client{URL: "http://127.0.0.1:1/v1", Key: "k"}, "+49 170 1234567"
	s.usage = usage.New("http://127.0.0.1:1", "u")
	return s
}

// switched builds s as main does with HOMES_MODULE_AUS=aus.
func switched(t *testing.T, s site, aus string) (site, *modul.Liste) {
	t.Helper()
	l, err := modul.Bauen(ausschalten(&s, katalog(s), aus))
	if err != nil {
		t.Fatal(err)
	}
	s.napi.Module = l
	s.napi.Einrichtung = einrichtung(s, nil, nil)
	return s, l
}

// ADR 0001 §8, the leaf modules (H3): each one off in turn answers its Aus
// or the surface's 404, never its handler; its entries are gone; /napi/kann
// has it an:false, warum aus; every other leaf module is as before.
func TestEachLeafModuleOff(t *testing.T) {
	leaves := []string{"gesundheit", "werkstatt", "netzlaufwerk", "podcasts", "gesehen", "standort", "sicherung",
		"dokumente", "notizen", "musik", "einkauf", "kalender", "kontakte", "konto", "geraete", "energie", "sprechen", "nutzung"}
	// what each serves while on, and what it answers off: a path and a word on it
	type probe struct{ path, on, off string }
	const wunschliste = "/einkauf?liste=liste-musik" // the Haushalt pills: served with einkauf or musik on
	probes := map[string][]probe{
		"gesundheit":   {{"/mehr", "gesundheit.example/werte", "!gesundheit.example/werte"}},
		"werkstatt":    {{"/box", `box-werkstatt`, "!box-werkstatt"}},
		"netzlaufwerk": {{"/mehr", `href="/netzlaufwerk"`, `!href="/netzlaufwerk"`}, {"/netzlaufwerk", "Passwort", "noch nicht eingerichtet"}},
		"podcasts":     {{"/mehr", `href="/podcasts"`, `!href="/podcasts"`}, {"/podcasts", "Podcasts", "#404"}},
		"gesehen":      {{"/mehr", `href="/gesehen"`, `!href="/gesehen"`}, {"/gesehen", "Gesehen", "#404"}},
		"standort":     {{"/mehr", `href="/standort"`, `!href="/standort"`}, {"/standort", "Reitti", "noch nicht eingerichtet"}, {"napi:/napi/standort", "#200", "#503"}},
		"sicherung":    {{"/mehr", `href="/daten-sichern"`, `!href="/daten-sichern"`}, {"/daten-sichern", "WhatsApp", "nicht eingerichtet"}, {"napi:/napi/sicherung/stand", "#200", "#503"}},
		// H4: the tool widgets' catalog, the app's paths and the Bote's tools follow
		"dokumente": {{"/mehr", `href="/dokumente"`, `!href="/dokumente"`}, {"/dokumente", "scan@example.org", "noch nicht eingerichtet"},
			{"napi:/napi/defs/tool", `"tool-id":"doc"`, `!"tool-id":"doc"`}, {"napi:/napi/portal/documents", "#200", "#404"},
			{"napi:POST /napi/upload", "#400", "#404"}, {"mcp:", `"dokumente_suchen"`, `!"dokumente_suchen"`}},
		"notizen": {{wunschliste, `href=/notizen`, `!href=/notizen`}, {"/notizen", "Neue Notiz", "noch nicht eingerichtet"},
			{"napi:/napi/defs/tool", `"tool-id":"note"`, `!"tool-id":"note"`}, {"napi:/napi/portal/notes", "#200", "#404"},
			{"napi:POST /napi/merken", "#400", "#404"}, {"mcp:", `"notiz_anlegen"`, `!"notiz_anlegen"`}},
		"musik": {{"/mehr", `href="/musik"`, `!href="/musik"`}, {"/musik", "Musik hinzufügen", "#404"}, {"/discogs", "#200", "#404"}},
		// the music wish list is musik's: it stays with einkauf off, the other lists go
		"einkauf": {{wunschliste, `href=/einkauf`, `!href=/einkauf`}, {"/einkauf", "<h1>Einkauf</h1>", "#404"},
			{"/einkauf?liste=liste-musik", "<h1>Musik</h1>", "<h1>Musik</h1>"}, {"/einkauf?liste=liste-musik", "Listen verwalten", "!Listen verwalten"},
			{"mcp:", `"einkaufsliste_dazu"`, `!"einkaufsliste_dazu"`}},
		// H5, one Radicale account: each goes alone, the others keep theirs
		"kalender": {{wunschliste, `href=/kalender`, `!href=/kalender`}, {"/kalender", "Milch kaufen", "noch nicht eingerichtet"},
			{"/start", "Heute und demnächst", "!Heute und demnächst"}, {"/kalender/erinnerung/milch-1", "Milch kaufen", "!Milch kaufen"},
			{"napi:/napi/defs/tool", `"tool-id":"task"`, `!"tool-id":"task"`}, {"napi:/napi/portal/tasks", "#200", "#404"},
			{"mcp:", `"termin_anlegen"`, `!"termin_anlegen"`}},
		"kontakte": {{"/kontakte/urn:uuid:b", "Bernd Bauer", "!Bernd Bauer"},
			{"napi:/napi/defs/tool", `"tool-id":"contacts"`, `!"tool-id":"contacts"`}, {"napi:/napi/portal/contacts", "#200", "#404"},
			{"napi:POST /napi/kontakte", "#415", "#503"}, {"mcp:", `"kontakte_suchen"`, `!"kontakte_suchen"`}},
		"konto": {{"napi:/napi/gemeinsam/kontakte", "#200", "#503"}, {"napi:/napi/gemeinsam/kalender", "#200", "#503"}},
		// H6: the house, its energy (Braucht geraete), the Sprechen field, the counts
		"geraete": {{"/start", "Im Haus", "!Im Haus"}, {"/start", `href=/raeume`, `!href=/raeume`}, {"/aktivierungswort", "#200", "#404"},
			{"napi:/napi/portal/state?entity_id=light.decke", "#200", "#404"}, {"napi:POST /napi/ha/call", "#400", "#404"},
			{"napi:POST /napi/portal/watch", "#200", "#404"}, {"napi:/napi/portal/favorites", "#200", "#404"}},
		"energie":  {{"/start", `href=/energie`, `!href=/energie`}, {"/energie", "#200", "#404"}, {"napi:/napi/portal/energy", "#200", "#404"}},
		"sprechen": {{"/start", "action=/sprechen", "!action=/sprechen"}, {"/willkommen", "App „Signal“", "!App „Signal“"}},
		"nutzung":  {{"/box/nutzung", "box-genutzt", "!box-genutzt"}},
	}
	check := func(t *testing.T, s site, p probe, want string) {
		t.Helper()
		h := routes(s)
		var code int
		var body string
		if path, ok := strings.CutPrefix(p.path, "napi:"); ok {
			code, body = napiRaw(h, path)
		} else if p.path == "mcp:" { // the Bote's tools
			req := httptest.NewRequest("POST", "/mcp/haushalt", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
			req.Header.Set("Authorization", "Bearer bote")
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			code, body = rec.Code, rec.Body.String()
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
				case m.Name != name && strings.Contains(strings.Join(leaves, ","), m.Name) && !m.An && !needs(l, m.Name, name):
					t.Errorf("kann: %s went off with %s", m.Name, name)
				}
			}
			// none of its offers is left in app, unless a module that is on offers it too
			k, others := l.Kann(modul.Wuensche{}), map[string]bool{}
			var bietet []string
			for _, m := range k.Module {
				if m.Name == name {
					bietet = angebotKeys(m.Bietet)
				} else if m.An {
					for _, key := range angebotKeys(m.Bietet) {
						others[key] = true
					}
				}
			}
			for _, key := range bietet {
				if !others[key] && slices.Contains(angebotKeys(k.App), key) {
					t.Errorf("kann: %s is off, and app still offers %s", name, key)
				}
			}
			for _, other := range leaves {
				if other == name || needs(l, other, name) { // energie goes with geraete
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
	if on.podcasts == nil || on.napi.Sicherung == nil || on.hh.Wuensche == nil {
		t.Error("nothing switched off, and still a backend is gone")
	}
	// tickWishes looks only while musik is on; einkauf off leaves the wishes their lists
	off, _ = switched(t, leafSite(t), "musik")
	if off.hh.Wuensche != nil || off.napi.Haushalt.Wuensche != nil || off.hh.Einkauf == nil {
		t.Errorf("musik off: wishes %v %v, einkauf %v", off.hh.Wuensche, off.napi.Haushalt.Wuensche, off.hh.Einkauf)
	}
	off, _ = switched(t, leafSite(t), "einkauf")
	if off.hh.Wuensche == nil || off.hh.Wuensche.Listen == nil || off.hh.Einkauf != nil || off.napi.Haushalt.Einkauf != nil {
		t.Errorf("einkauf off: wishes %v, einkauf %v %v", off.hh.Wuensche, off.hh.Einkauf, off.napi.Haushalt.Einkauf)
	}
}

// An unknown name is ignored; every module of the catalog can be switched
// off (ADR 0001 H6).
func TestModuleAusIgnoresWhatItCannot(t *testing.T) {
	_, l := switched(t, leafSite(t), "gibtsnicht,, ")
	for _, m := range l.Module() {
		if !m.Schaltbar {
			t.Errorf("%s cannot be switched off", m.Name)
		}
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

// H6: energie goes with geraete (Braucht), saying why; the voice counts
// and the microphone go with HA; the event stream is the core's and
// answers with geraete off, for konto's gemeinsam frames.
func TestGeraeteOff(t *testing.T) {
	s, l := switched(t, leafSite(t), "geraete")
	e, _ := l.Eins("energie")
	if e.An() || e.Warum != modul.Aus || e.Grund != "Ausgeschaltet, weil es „Geräte“ braucht, und das ist hier ausgeschaltet." || !s.napi.OhneEnergie {
		t.Errorf("energie with geraete off: %+v, OhneEnergie %v", e, s.napi.OhneEnergie)
	}
	if s.napi.HA != nil || s.voice != nil || s.napi.Konto == nil {
		t.Errorf("geraete off: HA %v, voice %v, konto %v", s.napi.HA, s.voice, s.napi.Konto)
	}
	srv := httptest.NewServer(routes(s))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/napi/portal/events", nil)
	req.Header.Set("Authorization", "Bearer dev1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Errorf("the stream with geraete off → %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
}

// needs: does module m need module b on (Braucht)?
func needs(l *modul.Liste, m, b string) bool {
	x, _ := l.Eins(m)
	return slices.Contains(x.Braucht, b)
}

// angebotKeys are an offer's keys, "widgets:tool:task", "konto:kalender".
func angebotKeys(a modul.Angebot) []string {
	var out []string
	for list, keys := range map[string][]string{"widgets": a.Widgets, "teilen": a.Teilen, "konto": a.Konto, "sicherung": a.Sicherung, "mehr": a.Mehr} {
		for _, k := range keys {
			out = append(out, list+":"+k)
		}
	}
	return out
}

// H5: the account and the contacts leave kann on their own (konto's parts,
// the backup's contacts); ensureHaushalt makes only the collections of the
// modules that are on.
func TestRadicaleModulesApart(t *testing.T) {
	_, l := switched(t, leafSite(t), "konto")
	if k := l.Kann(modul.Wuensche{}); len(k.App.Konto) != 0 || !slices.Contains(k.App.Sicherung, "kontakte") {
		t.Errorf("konto off: app konto %v, sicherung %v", k.App.Konto, k.App.Sicherung)
	}
	_, l = switched(t, leafSite(t), "kontakte")
	if k := l.Kann(modul.Wuensche{}); slices.Contains(k.App.Sicherung, "kontakte") || len(k.App.Konto) != 2 {
		t.Errorf("kontakte off: app sicherung %v, konto %v", k.App.Sicherung, k.App.Konto)
	}
	for _, c := range []struct {
		aus            string
		calendar, book bool
	}{{"", true, true}, {"kalender", true, true}, {"kalender,konto", false, true}, {"kontakte,konto", true, false}, {"kalender,kontakte,konto", false, false}} {
		dav := davfake.New("homes", "pw")
		_, l := switched(t, leafSite(t), c.aus)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		ensureHaushalt(ctx, &caldav.Client{URL: dav.URL, User: "homes", Password: "pw"}, l)
		cancel()
		if got := dav.DisplayName("/homes/haushalt/") != ""; got != c.calendar {
			t.Errorf("%q off: the calendar made %v, want %v", c.aus, got, c.calendar)
		}
		if got := dav.DisplayName("/homes/haushalt-kontakte/") != ""; got != c.book {
			t.Errorf("%q off: the address book made %v, want %v", c.aus, got, c.book)
		}
		dav.Close()
	}
}

// napiRaw is a paired phone's request: "GET" without a method in path.
func napiRaw(h http.Handler, path string) (int, string) {
	method, p, ok := strings.Cut(path, " ")
	if !ok {
		method, p = "GET", path
	}
	req := httptest.NewRequest(method, p, nil)
	req.Header.Set("Authorization", "Bearer dev1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func itoa(n int) string { return strconv.Itoa(n) }
