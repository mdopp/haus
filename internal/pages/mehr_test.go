package pages

import (
	"strings"
	"testing"
)

// haus#192: Mehr carries one card for the Gesundheit program when the box
// has it: each of its pages a link, who sees what in one quiet line; without the
// address there is no card and no empty space. Nothing of a person's health
// passes through Haus.
func TestMehrLinksToGesundheitOnlyWhenTheBoxHasIt(t *testing.T) {
	_, body, _ := familyPage(t, Mehr, StartConfig{GesundheitURL: "https://gesundheit.example.org"}, "GET", mehrPath, "family")
	for _, want := range []string{"Gesundheit", saysGesundheitWer, `href="https://gesundheit.example.org/werte"`, `href="https://gesundheit.example.org/befunde"`,
		`href="https://gesundheit.example.org/befinden"`, `href="https://gesundheit.example.org/arzt"`, ">Werte<", ">Befunde<", ">Wie es mir ging<", ">Für den Arzt<"} {
		if !strings.Contains(body, want) {
			t.Errorf("Mehr lacks %q", want)
		}
	}
	_, body, _ = familyPage(t, Mehr, StartConfig{}, "GET", mehrPath, "family")
	if strings.Contains(body, saysGesundheitWer) || strings.Contains(body, "gesundheit.example.org") {
		t.Fatal("a box without Gesundheit shows its card")
	}
}
