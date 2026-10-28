package pages

import (
	"strings"
	"testing"
)

// haus#192: Mehr carries one card for the Gesundheit program when the box
// has it, with what it is, who sees what, and the link; without the
// address there is no card and no empty space. Nothing of a person's health
// passes through Haus.
func TestMehrLinksToGesundheitOnlyWhenTheBoxHasIt(t *testing.T) {
	_, body, _ := familyPage(t, Mehr, StartConfig{GesundheitURL: "https://gesundheit.example.org"}, "GET", mehrPath, "family")
	for _, want := range []string{"Gesundheit", saysGesundheit, saysGesundheitWer, `href="https://gesundheit.example.org"`, saysGesundheitLink, saysOnlyAtHome} {
		if !strings.Contains(body, want) {
			t.Errorf("Mehr lacks %q", want)
		}
	}
	_, body, _ = familyPage(t, Mehr, StartConfig{}, "GET", mehrPath, "family")
	if strings.Contains(body, saysGesundheit) || strings.Contains(body, "gesundheit.example.org") {
		t.Fatal("a box without Gesundheit shows its card")
	}
}
