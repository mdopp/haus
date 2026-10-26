package paperless

import (
	"context"
	"testing"

	"github.com/mdopp/haus/internal/paperless/paperlessfake"
)

func TestKuendigenBis(t *testing.T) {
	for _, c := range [][3]string{
		{"31.12.2026", "3 Monate", "30.09.2026"},
		{"31.12.2026", "3 Monate zum Quartalsende", "30.09.2026"},
		{"28.02.2027", "1 Monat zum Monatsende", "31.01.2027"}, // a month's end reaches back to a month's end
		{"15.03.2027", "1 Monat", "15.02.2027"},
		{"31.03.2027", "1 Monat", "28.02.2027"},
		{"01.07.2027", "6 Wochen", "20.05.2027"},
		{"10.01.2027", "14 Tage", "27.12.2026"},
		{"31.12.2026", "1 Jahr", "31.12.2025"},
	} {
		got := kuendigenBis(c[0], c[1])
		if got.Format("02.01.2006") != c[2] {
			t.Errorf("%s − %s = %s, want %s", c[0], c[1], got.Format("02.01.2006"), c[2])
		}
	}
	if !kuendigenBis("", "3 Monate").IsZero() || !kuendigenBis("31.12.2026", "jederzeit").IsZero() {
		t.Error("nothing to reckon from")
	}
}

// haus#139: the contracts a question names, with their fields by name, when
// to give notice, the document's link, and "prüfen" as unsure.
func TestVertraege(t *testing.T) {
	f := paperlessfake.New("tok")
	defer f.Close()
	f.DocType(1, "Versicherung")
	f.DocType(2, "Rechnung")
	f.Tag(7, "Vertrag")
	f.Tag(9, "prüfen")
	f.Correspondent(3, "HUK-Coburg")
	f.Field(11, "Anbieter", "string")
	f.Field(12, "Nächster Kündigungstermin", "date")
	f.Field(13, "Kündigungsfrist", "string")
	f.Field(14, "Betrag", "monetary")
	f.Add(
		paperlessfake.Doc{ID: 1, Title: "Kfz-Versicherung 2026", DocumentType: 1, Correspondent: 3, Created: "2025-11-02",
			Fields: map[int]any{11: "HUK-Coburg", 12: "2027-01-01", 13: "1 Monat", 14: "EUR84.50"}},
		paperlessfake.Doc{ID: 2, Title: "Stromvertrag Stadtwerke", Tags: []int{7, 9}, Created: "2024-05-01", Fields: map[int]any{11: "Stadtwerke"}},
		paperlessfake.Doc{ID: 3, Title: "Kfz-Werkstatt Rechnung", DocumentType: 2},
	)
	c := &Client{URL: f.URL, Token: "tok", PublicURL: "https://paperless.box"}
	ctx := context.Background()
	vs, err := c.Vertraege(ctx, "Wann läuft die Kfz-Versicherung aus?", nil, 5)
	if err != nil || len(vs) != 1 {
		t.Fatalf("%+v %v", vs, err)
	}
	v := vs[0]
	if v.Title != "Kfz-Versicherung 2026" || v.Art != "Versicherung" || v.Correspondent != "HUK-Coburg" || v.Felder["Betrag"] != "84,50 €" ||
		v.Felder["Nächster Kündigungstermin"] != "01.01.2027" || v.KuendigenBis.Format("02.01.2006") != "01.12.2026" || v.Link != "https://paperless.box/documents/1/details" || v.Unsicher {
		t.Errorf("%+v", v)
	}
	vs, _ = c.Vertraege(ctx, "Bis wann muss ich den Stromvertrag kündigen?", nil, 5)
	if len(vs) != 1 || !vs[0].Unsicher || vs[0].Art != "Vertrag" {
		t.Errorf("by tag, unsure: %+v", vs)
	}
	if vs, _ := c.Vertraege(ctx, "Wann kündigen?", nil, 5); len(vs) != 0 {
		t.Errorf("no word names one: %+v", vs)
	}
	f.ChatAnswer = "Laut Vertrag endet die Laufzeit am 30.06.2027."
	if a, err := c.Chat(ctx, 2, "Wann endet der Vertrag?"); err != nil || a != f.ChatAnswer || f.ChatAsked[0]["document_id"] != float64(2) {
		t.Errorf("%q %v %v", a, err, f.ChatAsked)
	}
	f.ChatAnswer = ""
	if _, err := c.Chat(ctx, 2, "x"); err != ErrNoChat {
		t.Errorf("no chat: %v", err)
	}
}
