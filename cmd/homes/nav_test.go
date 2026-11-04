package main

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/mdopp/homes/internal/discogs"
)

// navOf is a page's family selection: the bar with Mehr's list, the
// Haushalt pills where the page has them, and Mehr's card of its pages.
var navOf = regexp.MustCompile(`<nav class=(?:tabs|unterseiten) .*?</nav>|<section class=karte aria-labelledby=[^>]*><h2 id=[^>]*>Seiten</h2>.*?</section>`)

// TestTheNavIsAsItWas locks the family's selection (ADR 0001 H6): the bar,
// Mehr's list and the Haushalt pills, their order, icons and who sees
// them, for fullSite, a box with every module (alles) and one without a
// music inbox but with Discogs (ohne-eingang), each signed in, as admin
// and anonymous; and the box with every module, each one off in turn.
// UPDATE_GOLDEN=1 writes the file.
func TestTheNavIsAsItWas(t *testing.T) {
	full, _, _ := fullSite(t)
	full, _ = switched(t, full, "")
	alles, _ := switched(t, leafSite(t), "")
	ohne := leafSite(t)
	ohne.musik = ""
	ohne.hh.Wuensche.Regale = &discogs.Regale{App: &discogs.App{Key: "k", Secret: "s"}, Dir: t.TempDir()}
	ohne.napi.Haushalt.Wuensche = ohne.hh.Wuensche
	ohne, _ = switched(t, ohne, "")
	var b strings.Builder
	for _, box := range []struct {
		name string
		s    site
	}{{"fullSite", full}, {"alles", alles}, {"ohne-eingang", ohne}} {
		h := routes(box.s)
		for _, who := range []struct{ name, user, groups string }{{"angemeldet", "anna", "family"}, {"admin", "anna", "family,admins"}, {"anonym", "", ""}} {
			for _, path := range []string{"/start", "/mehr", "/notizen"} {
				body := as(h, "GET", path, who.user, who.groups, nil).Body.String()
				b.WriteString("== " + box.name + " " + who.name + " " + path + "\n")
				for _, nav := range navOf.FindAllString(body, -1) {
					b.WriteString(nav + "\n")
				}
			}
		}
	}
	// each module off in turn, on the box with all of them: what an admin sees
	for _, m := range katalog(leafSite(t)) {
		off, _ := switched(t, leafSite(t), m.Name)
		h := routes(off)
		for _, path := range []string{"/mehr", "/einkauf?liste=liste-musik"} {
			body := as(h, "GET", path, "anna", "family,admins", nil).Body.String()
			b.WriteString("== alles ohne " + m.Name + " admin " + path + "\n")
			for _, nav := range navOf.FindAllString(body, -1) {
				b.WriteString(nav + "\n")
			}
		}
	}
	const golden = "testdata/nav.golden"
	if os.Getenv("UPDATE_GOLDEN") != "" {
		os.WriteFile(golden, []byte(b.String()), 0o644)
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if b.String() != string(want) {
		t.Errorf("the nav differs from %s:\n%s", golden, lineDiff(string(want), b.String()))
	}
}

// lineDiff names the first lines that differ.
func lineDiff(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	var out []string
	for i := 0; i < max(len(w), len(g)) && len(out) < 12; i++ {
		var a, c string
		if i < len(w) {
			a = w[i]
		}
		if i < len(g) {
			c = g[i]
		}
		if a != c {
			out = append(out, "want: "+a, " got: "+c)
		}
	}
	return strings.Join(out, "\n")
}
