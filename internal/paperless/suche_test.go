package paperless

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/mdopp/homes/internal/paperless/paperlessfake"
)

// sucheFake is the order after ordnen: types, Bereich tags, the fields
// and two people, and a few documents.
func sucheFake(t *testing.T) (*paperlessfake.Server, *Client) {
	f := paperlessfake.New("tok")
	t.Cleanup(f.Close)
	f.DocType(1, "Rechnung")
	f.DocType(2, "Versicherungsschein")
	f.DocType(3, "Beleg/Garantie")
	f.DocType(4, "Brief")
	f.Tag(10, "Haus")
	f.Tag(11, "Versicherung")
	f.Tag(12, "gesundheit")
	f.Tag(13, "Auto")
	f.Correspondent(20, "Stadtwerke Musterstadt")
	f.Correspondent(21, "HUK-Coburg")
	f.Field(30, "Rechnungsnummer", "string")
	f.Field(31, "Versicherungsnummer", "string")
	f.Field(32, "Produkte", "longtext")
	f.Field(33, "Garantie bis", "date")
	f.Field(34, "Person", "select")
	f.Field(35, "Betrag", "monetary")
	f.Options(34, "Anna", "Ben")
	f.Add(
		paperlessfake.Doc{ID: 1, Title: "Stromrechnung Oktober", DocumentType: 1, Correspondent: 20, Tags: []int{10}, Created: "2026-10-20",
			Fields: map[int]any{30: "RE-2026-4711", 35: "EUR84.50", 34: "opt-Anna"}},
		paperlessfake.Doc{ID: 2, Title: "Kfz-Versicherung", DocumentType: 2, Correspondent: 21, Tags: []int{11}, Created: "2025-11-02",
			Fields: map[int]any{31: "KV 123 456 789", 32: "Kasko"}},
		paperlessfake.Doc{ID: 3, Title: "Waschmaschine Kassenbon", DocumentType: 3, Tags: []int{10}, Created: "2025-03-01",
			Fields: map[int]any{32: "Waschmaschine WM 7", 33: "2027-03-01", 34: "opt-Ben"}},
		paperlessfake.Doc{ID: 4, Title: "Brief der Stadtwerke", DocumentType: 4, Correspondent: 20, Created: "2024-01-05",
			Content: "Ihre Zahlung an IBAN DE89 3704 0044 0532 0130 00 ist eingegangen."},
		paperlessfake.Doc{ID: 5, Title: "Alter Bon", DocumentType: 3, Fields: map[int]any{33: "2025-01-01"}},
	)
	return f, &Client{URL: f.URL, Token: "tok", PublicURL: "https://paperless.box"}
}

func ids(es []Eintrag) []int {
	var out []int
	for _, e := range es {
		out = append(out, e.ID)
	}
	return out
}

// owner 2026-11-06: one search field finds a document by its number, its
// product, its sender or its text; Bereich, type and person narrow it.
func TestFinde(t *testing.T) {
	f, c := sucheFake(t)
	ctx := context.Background()
	o, err := c.Ordnung(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(o.Bereiche, []Gezaehlt{{"Haus", 2}, {"Versicherung", 1}}) || o.Typen[0] != (Gezaehlt{"Beleg/Garantie", 2}) || !slices.Equal(o.Personen, []string{"Anna", "Ben"}) {
		t.Errorf("order: %+v %+v %v", o.Bereiche, o.Typen, o.Personen)
	}
	for q, want := range map[string][]int{
		"4711":        {1},    // the Rechnungsnummer, a piece of it
		"123456789":   {2},    // the Versicherungsnummer without its spaces
		"KV 123 456":  {2},    // and with them
		"waschmasch":  {3},    // a product bought (and its title)
		"stadtwerke":  {1, 4}, // the sender, then the text
		"3704 0044":   {4},    // a piece of an IBAN in the text
		"gibtsnicht!": nil,
	} {
		got, err := c.Finde(ctx, Filter{Text: q, Max: 20})
		if err != nil || !slices.Equal(ids(got), want) {
			t.Errorf("%q: %v %v, want %v", q, ids(got), err, want)
		}
	}
	got, _ := c.Finde(ctx, Filter{Text: "4711", Max: 20})
	if e := got[0]; e.Art != "Rechnung" || e.Correspondent != "Stadtwerke Musterstadt" || e.Felder["Rechnungsnummer"] != "RE-2026-4711" ||
		e.Felder["Betrag"] != "84,50 €" || e.Felder["Person"] != "opt-Anna" || !slices.Equal(e.Tags, []string{"Haus"}) || e.Created.Format("02.01.2006") != "20.10.2026" {
		t.Errorf("the row's fields: %+v", e)
	}
	for name, c2 := range map[string]struct {
		f    Filter
		want []int
	}{
		"Bereich, newest first": {Filter{Bereich: "haus"}, []int{1, 3}},
		"Bereich and words":     {Filter{Bereich: "Haus", Text: "stadtwerke"}, []int{1}},
		"types":                 {Filter{Typen: []string{"Versicherungsschein", "Vertrag"}}, []int{2}},
		"person":                {Filter{Person: "Ben"}, []int{3}},
		"warranty still runs":   {Filter{GarantieAb: time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)}, []int{3}},
		"a Bereich it lacks":    {Filter{Bereich: "Freizeit"}, nil},
		"a person it lacks":     {Filter{Person: "Carla"}, nil},
	} {
		c2.f.Max = 20
		got, err := c.Finde(ctx, c2.f)
		if err != nil || !slices.Equal(ids(got), c2.want) {
			t.Errorf("%s: %v %v, want %v", name, ids(got), err, c2.want)
		}
	}
	// the same ask again within the minute: Paperless is not asked again
	before := len(f.Seen())
	c.Finde(ctx, Filter{Text: "4711", Max: 20})
	if len(f.Seen()) != before {
		t.Errorf("asked again: %d requests", len(f.Seen())-before)
	}
	// the number fields' ask as Paperless 3 takes it: by id, OR of icontains, only fields it has
	for _, r := range f.Seen() {
		if v := r.URL.Query().Get("custom_field_query"); v != "" && r.URL.Query().Get("text") == "" && r.URL.Query().Get("correspondent__name__icontains") == "" {
			if want := `["AND",[["OR",[[30,"icontains","4711"],[31,"icontains","4711"],[32,"icontains","4711"]]]]]`; v == want {
				return
			}
		}
	}
	t.Error("no number ask of the expected form")
}

// A Paperless that fails is an error, never an empty list said as „nichts“.
func TestFindeFails(t *testing.T) {
	f, c := sucheFake(t)
	f.FailPath = "/api/custom_fields/"
	if _, err := c.Finde(context.Background(), Filter{Text: "x", Max: 5}); err == nil {
		t.Error("no error")
	}
}
