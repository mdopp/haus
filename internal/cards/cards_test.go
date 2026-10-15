package cards

import (
	"encoding/json"
	"strings"
	"testing"
)

// haus#12: a card is a title, rows and an optional link, rendered as one
// labelled section; whatever a row says is text, never markup.
func TestCardRendersAsALabelledSection(t *testing.T) {
	c := Card{Title: "Jetzt", Rows: []Row{{Text: "2 Lichter an", Detail: "Küche, Bad"}, {Text: "<b>Zahnarzt</b>"}},
		Link: &Link{Href: "/raeume?a=1&b=2", Text: "Alle Räume"}}
	got := c.HTML()
	for _, want := range []string{`<section class=karte aria-labelledby=karte-jetzt>`, `<h2 id=karte-jetzt>Jetzt</h2>`,
		"2 Lichter an", "Küche, Bad", "&lt;b&gt;Zahnarzt&lt;/b&gt;", `href="/raeume?a=1&amp;b=2"`, "Alle Räume", "<ul>", "<li>"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q missing:\n%s", want, got)
		}
	}
	if strings.Contains(got, "<b>Zahnarzt") {
		t.Fatal("a row's text reached the page as markup")
	}
}

// A card without rows says nothing: no empty box on the family's page.
func TestEmptyCardSaysNothing(t *testing.T) {
	if got := (Card{Title: "Jetzt"}).HTML(); got != "" {
		t.Fatalf("empty card rendered %q", got)
	}
	if !(Card{Title: "Jetzt"}).Empty() || (Card{Title: "x", Rows: []Row{{Text: "y"}}}).Empty() {
		t.Fatal("Empty")
	}
}

// Two cards whose titles differ only by umlauts or spaces still get ids a
// label can point at.
func TestCardIDs(t *testing.T) {
	for title, want := range map[string]string{"Jetzt": "karte-jetzt", "Meine Favoriten": "karte-meine-favoriten", "Häufig genutzt": "karte-haeufig-genutzt", "": "karte"} {
		if got := (Card{Title: title}).ID(); got != want {
			t.Errorf("ID(%q) = %q, want %q", title, got, want)
		}
	}
}

// The same card goes to the widgets as JSON (haus#12): no null anywhere,
// Android reads it as "null".
func TestCardJSONHasNoNull(t *testing.T) {
	for _, c := range []Card{{Title: "Jetzt"}, {Title: "Haushalt", Rows: []Row{{Text: "Müll"}}}} {
		b, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "null") {
			t.Fatalf("null in %s", b)
		}
		if !strings.Contains(string(b), `"rows":[`) || !strings.Contains(string(b), `"title":`) {
			t.Fatalf("shape: %s", b)
		}
	}
}

// haus#12: a row may lead somewhere (a note, the calendar); its text stays
// text and its address is escaped. A row without one is no link.
func TestRowLinks(t *testing.T) {
	c := Card{Title: "Haushalt", Rows: []Row{{Text: "<i>Einkaufsliste</i>", Detail: "Notiz", Href: "/notizen/a.md?x=1&y=2"}, {Text: "Müll"}}}
	got := c.HTML()
	for _, want := range []string{`<li><a href="/notizen/a.md?x=1&amp;y=2">&lt;i&gt;Einkaufsliste&lt;/i&gt;</a> <span class=k>Notiz</span></li>`, "<li>Müll</li>"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q missing:\n%s", want, got)
		}
	}
	b, _ := json.Marshal(Card{Title: "x", Rows: []Row{{Text: "Müll"}, {Text: "a", Href: "/kalender"}}})
	if strings.Contains(string(b), "null") || !strings.Contains(string(b), `"href":"/kalender"`) {
		t.Fatalf("json: %s", b)
	}
}

// haus#12: the family reads cards on a phone. A long name or a long word
// breaks inside its row instead of pushing the page sideways, and a row
// that leads somewhere is a tap target a finger hits.
func TestCardsFitAPhone(t *testing.T) {
	for _, want := range []string{".karte li{", "overflow-wrap:anywhere", ".karte li a{display:block"} {
		if !strings.Contains(Style, want) {
			t.Errorf("the cards' style lacks %q:\n%s", want, Style)
		}
	}
}
