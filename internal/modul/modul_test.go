package modul

import (
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
