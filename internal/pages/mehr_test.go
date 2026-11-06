package pages

import (
	"strings"
	"testing"

	"github.com/mdopp/homes/internal/modul"
)

// haus#192: Mehr carries one card for the Gesundheit program when the box
// has it: each of its pages a link, who sees what in one quiet line; without the
// address there is no card and no empty space. Nothing of a person's health
// passes through Homes.
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

// Gesundheit retired (2026-11-06): its address may still be set, but with its
// module off (Laib no longer runs the service) Mehr shows no card.
func TestMehrHasNoGesundheitCardWhenItsServiceIsGone(t *testing.T) {
	l, err := modul.Bauen([]modul.Modul{{Name: "gesundheit", Titel: "Gesundheit", Warum: modul.Fehlt, Grund: "Auf dieser Box läuft Gesundheit nicht."}})
	if err != nil {
		t.Fatal(err)
	}
	_, body, _ := familyPage(t, Mehr, StartConfig{GesundheitURL: "https://gesundheit.example.org", Module: l}, "GET", mehrPath, "family")
	if strings.Contains(body, "gesundheit.example.org") || strings.Contains(body, saysGesundheitWer) {
		t.Fatal("a card for a service that is gone")
	}
}
