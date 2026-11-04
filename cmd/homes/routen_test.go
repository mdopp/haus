package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/mdopp/homes/internal/discogs"
	"github.com/mdopp/homes/internal/napi"
	"github.com/mdopp/homes/internal/pages"
)

// napiProben are a request for each /napi pattern Homes has ever served,
// and some it never did.
var napiProben = []string{
	"GET /napi/assistent", "GET /napi/app", "GET /napi/persona", "GET /napi/box/home", "GET /napi/box/services",
	"GET /napi/box/approvals", "GET /napi/box/upgrades", "POST /napi/box/services/homes/operate",
	"POST /napi/box/approvals/1/approve", "POST /napi/box/approvals/1/deny", "GET /napi/kann", "POST /napi/kann/anfordern",
	"GET /napi/whoami", "GET /napi/portal/events", "POST /napi/portal/watch", "POST /napi/ha/call", "GET /napi/portal/state",
	"GET /napi/portal/entity-history", "GET /napi/portal/active", "GET /napi/portal/camera/camera.tuer/snapshot",
	"GET /napi/portal/start/addable", "GET /napi/portal/favorites", "GET /napi/portal/energy", "GET /napi/defs/tool",
	"POST /napi/action-callback", "GET /napi/portal/tasks", "GET /napi/portal/contacts", "GET /napi/portal/documents",
	"GET /napi/portal/notes", "POST /napi/upload", "POST /napi/musik", "GET /napi/sicherung/stand", "POST /napi/sicherung/fehlend",
	"POST /napi/sicherung/uploads", "HEAD /napi/sicherung/uploads/x1", "PATCH /napi/sicherung/uploads/x1",
	"GET /napi/sicherung/schluessel", "GET /napi/sicherung/letzte", "GET /napi/sicherung/letzte/whatsapp/a/b.jpg",
	"HEAD /napi/sicherung/letzte/whatsapp/a/b.jpg", "POST /napi/merken", "POST /napi/kontakte", "GET /napi/gemeinsam/kontakte",
	"GET /napi/gemeinsam/kalender", "GET /napi/standort", "POST /napi/standort", "POST /napi/standort/import",
	"HEAD /napi/standort/import/x1", "PATCH /napi/standort/import/x1", "GET /napi/standort/import/x1",
	"GET /napi/gibtsnicht", "DELETE /napi/kann", "GET /napi/upload", "GET /napi/ha/call",
}

// routeTable is, for each probe, the pattern a mux answers it with ("-":
// none).
func routeTable(mux *http.ServeMux, proben []string) string {
	var b strings.Builder
	for _, p := range proben {
		method, path, _ := strings.Cut(p, " ")
		_, pattern := mux.Handler(httptest.NewRequest(method, path, nil))
		if pattern == "" {
			pattern = "-"
		}
		b.WriteString(p + " → " + pattern + "\n")
	}
	return b.String()
}

// routeBoxes are the boxes the route tables are taken on: every module,
// each module off in turn, fullSite, a box with nothing, and one without a
// music inbox but with Discogs.
func routeBoxes(t *testing.T) ([]string, []site) {
	t.Helper()
	full, _, _ := fullSite(t)
	ohne := leafSite(t)
	ohne.musik = ""
	ohne.hh.Wuensche.Regale = &discogs.Regale{App: &discogs.App{Key: "k", Secret: "s"}, Dir: t.TempDir()}
	ohne.napi.Haushalt.Wuensche = ohne.hh.Wuensche
	names, boxes := []string{"alles", "fullSite", "nichts", "ohne-eingang"}, []site{leafSite(t), full, testSite(t), ohne}
	for _, m := range katalog(leafSite(t)) {
		names, boxes = append(names, "ohne "+m.Name), append(boxes, leafSite(t))
	}
	for i := range boxes {
		aus := ""
		if n, ok := strings.CutPrefix(names[i], "ohne "); ok {
			aus = n
		}
		boxes[i], _ = switched(t, boxes[i], aus)
	}
	return names, boxes
}

// TestTheNapiRoutesAreAsTheyWere locks which pattern answers each /napi
// request on each box (homes#243: the route groups must not change it).
// UPDATE_GOLDEN=1 writes the file.
func TestTheNapiRoutesAreAsTheyWere(t *testing.T) {
	names, boxes := routeBoxes(t)
	var b strings.Builder
	for i, s := range boxes {
		b.WriteString("== " + names[i] + "\n" + routeTable(napi.Handler(s.napi).(*http.ServeMux), napiProben))
	}
	golden(t, "testdata/routen_napi.golden", b.String())
}

// golden compares got with the file, UPDATE_GOLDEN=1 writes it.
func golden(t *testing.T, file, got string) {
	t.Helper()
	if os.Getenv("UPDATE_GOLDEN") != "" {
		os.WriteFile(file, []byte(got), 0o644)
	}
	want, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("%s differs:\n%s", file, lineDiff(string(want), got))
	}
}

// beispiel is a request for a pattern: its method (GET without one) and
// its path with each wildcard a word.
func beispiel(muster string) *http.Request {
	method, path, ok := strings.Cut(muster, " ")
	if !ok {
		method, path = "GET", muster
	}
	path = regexp.MustCompile(`\{[^}]*\}`).ReplaceAllString(path, "x1")
	return httptest.NewRequest(method, path, nil)
}

// ADR 0001 §8: every route a module declares is served, under its own
// pattern, with everything on; each group is a module of the catalog;
// the modules with /napi paths (§7) have them; and a module off keeps
// only the answers released apps rely on (§4: sicherung, standort,
// konto, kontakte answer 503), or nothing.
func TestEveryDeclaredNapiRouteIsServed(t *testing.T) {
	s, l := switched(t, leafSite(t), "")
	mux := napi.Handler(s.napi).(*http.ServeMux)
	gs := napi.Gruppen(s.napi)
	for name, rs := range gs {
		if !l.An(name) {
			t.Errorf("group %q is no module that is on", name)
		}
		for _, r := range rs {
			if _, p := mux.Handler(beispiel(r.Muster)); p != r.Muster {
				t.Errorf("%s declares %q, served as %q", name, r.Muster, p)
			}
		}
	}
	for _, name := range []string{"geraete", "energie", "kalender", "kontakte", "konto", "notizen", "dokumente", "sicherung", "standort"} {
		if len(gs[name]) == 0 {
			t.Errorf("%s has no /napi route", name)
		}
	}
	// a module off keeps the paths released apps read an answer from: 503
	// in words (sicherung, standort, konto, kontakte) or Merken's 404 sentence
	aus := map[string][]string{"sicherung": nil, "standort": nil, "konto": nil, "kontakte": {"POST /napi/kontakte"}, "notizen": {"POST /napi/merken"}}
	for name := range gs {
		off, _ := switched(t, leafSite(t), name)
		var got []string
		for _, r := range napi.Gruppen(off.napi)[name] {
			got = append(got, r.Muster)
		}
		want, ok := aus[name]
		if want == nil && ok {
			for _, r := range gs[name] {
				want = append(want, r.Muster)
			}
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s off declares %v, want %v", name, got, want)
		}
	}
}

// seitenProben are a request for each page pattern Homes has ever served,
// and some it never did.
var seitenProben = []string{
	"/start", "/start/box", "/mehr", "/mehr/aufgaben", "/mehr/spuren", "/mehr/anfordern", "/module/podcasts", "/aufgaben",
	"/live", "/willkommen", "/adresse", "/adresse/vorschlagen", "/abmelden", "/schrift/geist.woff2", "/symbol/icon.svg",
	"/box", "/box/einladen", "/box/aufgaben", "/box/geraete", "/box/module", "/pair-device", "/healthz", "/",
	"/raeume", "/raeume/light.flur", "/medien", "/medien/radio", "/schalten", "/aktivierungswort", "/energie", "/energie/heute",
	"/kalender", "/kalender/verwalten", "/kalender/hinzufuegen", "/kalender/aendern", "/kalender/erinnerung/x1",
	"/kontakte/x1", "/notizen", "/notizen/x1.md", "/dokumente", "/dokumente/x1", "/scannen", "/scannen/trennblatt",
	"/einkauf", "/einkauf/rezepte", "/musik", "/musik/bild", "/discogs", "/discogs/zurueck", "/platte",
	"/platte/bild", "/podcasts", "/gesehen", "/daten-sichern", "/nachrichten-sichern", "/standort", "/netzlaufwerk",
	"/sprechen", "/sprechen/hoeren", "/gibtsnicht", "/kalender/gibtsnicht",
}

// TestThePageRoutesAreAsTheyWere locks which pattern answers each page
// request on each box (homes#243). UPDATE_GOLDEN=1 writes the file.
func TestThePageRoutesAreAsTheyWere(t *testing.T) {
	names, boxes := routeBoxes(t)
	var b strings.Builder
	for i, s := range boxes {
		proben := make([]string, len(seitenProben))
		for j, p := range seitenProben {
			proben[j] = "GET " + p
		}
		b.WriteString("== " + names[i] + "\n" + routeTable(familyMux(s), proben))
	}
	golden(t, "testdata/routen_seiten.golden", b.String())
}

// ADR 0001 §8 for the pages: every page a module declares is served
// under its own pattern, each group is a module that is on, the modules
// with pages (§7) have them, and an off module's group is no more than
// what it serves while on (a page that says it is not set up, or nothing).
func TestEveryDeclaredPageIsServed(t *testing.T) {
	for _, box := range []string{"alles", "ohne-eingang"} {
		s, l := switched(t, leafSite(t), "")
		if box == "ohne-eingang" {
			s.musik = ""
			s.hh.Wuensche.Regale = &discogs.Regale{App: &discogs.App{Key: "k", Secret: "s"}, Dir: t.TempDir()}
		}
		mux, gs := familyMux(s), pages.Gruppen(familyOf(s))
		for name, rs := range gs {
			if !l.An(name) {
				t.Errorf("%s: group %q is no module that is on", box, name)
			}
			for _, r := range rs {
				if _, p := mux.Handler(beispiel(r.Muster)); p != r.Muster {
					t.Errorf("%s: %s declares %q, served as %q", box, name, r.Muster, p)
				}
			}
		}
		for _, name := range []string{"geraete", "energie", "sprechen", "kalender", "kontakte", "einkauf", "notizen", "dokumente",
			"musik", "podcasts", "gesehen", "sicherung", "standort", "netzlaufwerk"} {
			if len(gs[name]) == 0 {
				t.Errorf("%s: %s has no page", box, name)
			}
		}
	}
	on := pages.Gruppen(familyOf(leafSite(t)))
	for name := range on {
		off, _ := switched(t, leafSite(t), name)
		was := map[string]bool{}
		for _, r := range on[name] {
			was[r.Muster] = true
		}
		for _, r := range pages.Gruppen(familyOf(off))[name] {
			if !was[r.Muster] {
				t.Errorf("%s off declares %q, which it does not while on", name, r.Muster)
			}
		}
	}
}
