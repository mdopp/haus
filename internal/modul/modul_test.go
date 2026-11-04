package modul

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBauenRefusesABrokenCatalog(t *testing.T) {
	on := func(name string, braucht ...string) Modul { return Modul{Name: name, Braucht: braucht} }
	off := func(name string) Modul { return Modul{Name: name, Warum: Fehlt, Grund: "Kein Backend."} }
	for name, ms := range map[string][]Modul{
		"a name twice":          {on("podcasts"), on("podcasts")},
		"no name":               {on("")},
		"a capital":             {on("Podcasts")},
		"two words":             {on("a,b")},
		"needs one that is off": {off("geraete"), on("energie", "geraete")},
		"needs an unknown one":  {on("energie", "geraete")},
		"off without a reason":  {{Name: "standort", Warum: Fehlt}},
		"an unknown warum":      {{Name: "standort", Warum: "kaputt", Grund: "x"}},
	} {
		if _, err := Bauen(ms); err == nil {
			t.Errorf("%s: Bauen took it", name)
		}
	}
	l, err := Bauen([]Modul{off("geraete"), off("energie"), on("podcasts")})
	if err != nil {
		t.Fatal(err)
	}
	if l.An("geraete") || !l.An("podcasts") || l.An("gibtsnicht") {
		t.Errorf("An: geraete %v, podcasts %v, unknown %v", l.An("geraete"), l.An("podcasts"), l.An("gibtsnicht"))
	}
}

func TestStringSaysWhatIsOnAndWhyNot(t *testing.T) {
	l, err := Bauen([]Modul{
		{Name: "geraete", Backend: "home-assistant"},
		{Name: "werkstatt"},
		{Name: "standort", Backend: "reitti", Warum: Fehlt, Grund: "Auf dieser Box läuft kein Reitti."},
		{Name: "podcasts", Backend: "audiobookshelf", Warum: Aus, Grund: "Auf dieser Box ausgeschaltet."},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "Module an: geraete (home-assistant), werkstatt; aus: standort (fehlt: Auf dieser Box läuft kein Reitti.), podcasts (aus: Auf dieser Box ausgeschaltet.)"
	if got := l.String(); got != want {
		t.Errorf("String:\n got %s\nwant %s", got, want)
	}
	if !strings.HasPrefix((&Liste{}).String(), "Module an: ; aus: ") {
		t.Errorf("an empty list: %q", (&Liste{}).String())
	}
}

// Eintraege: the entries at one Ort of the modules on and the core's, by
// Rang, those the request may see; a nil Liste has the core's only.
func TestEintraegeByRangAndWhoSees(t *testing.T) {
	signedIn := func(r *http.Request) bool { return r.Header.Get("Remote-User") != "" }
	l, err := Bauen([]Modul{
		{Name: "podcasts", Nav: []Eintrag{{Titel: "Podcasts", Ort: Mehr, Rang: 40}}},
		{Name: "dokumente", Nav: []Eintrag{{Titel: "Dokumente", Ort: Mehr, Rang: 10}, {Titel: "Leiste", Ort: Leiste}}},
		{Name: "standort", Nav: []Eintrag{{Titel: "Standortverlauf", Ort: Mehr, Rang: 90, Fuer: signedIn}}},
		{Name: "gesehen", Warum: Aus, Grund: "aus", Nav: []Eintrag{{Titel: "Gesehen", Ort: Mehr, Rang: 50}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	kern := []Eintrag{{Titel: "Box", Ort: Mehr, Rang: 120}, {Titel: "Apps", Ort: Mehr, Rang: 70}, {Titel: "Start", Ort: Leiste}}
	titles := func(es []Eintrag) string {
		var out []string
		for _, e := range es {
			out = append(out, e.Titel)
		}
		return strings.Join(out, ",")
	}
	anna := httptest.NewRequest("GET", "/mehr", nil)
	anna.Header.Set("Remote-User", "anna")
	if got := titles(l.Eintraege(anna, Mehr, kern...)); got != "Dokumente,Podcasts,Apps,Standortverlauf,Box" {
		t.Errorf("signed in: %s", got)
	}
	if got := titles(l.Eintraege(httptest.NewRequest("GET", "/mehr", nil), Mehr, kern...)); got != "Dokumente,Podcasts,Apps,Box" {
		t.Errorf("anonymous: %s", got)
	}
	if got := titles((*Liste)(nil).Eintraege(anna, Mehr, kern...)); got != "Apps,Box" {
		t.Errorf("no list: %s", got)
	}
}
