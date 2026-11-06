package main

import (
	"github.com/mdopp/homes/internal/paperless"
	"github.com/mdopp/homes/internal/paperless/paperlessfake"
)

// demoPaperless is a Paperless after dokumente's ordnen (owner 2026-11-06:
// the Dokumente page's search): the types, Bereich tags and fields, two
// people, and a household's few documents.
func demoPaperless() *paperless.Client {
	f := paperlessfake.New("demo")
	for id, n := range map[int]string{1: "Rechnung", 2: "Vertrag", 3: "Versicherungsschein", 4: "Beitrag/Bescheid", 5: "Beleg/Garantie", 6: "Brief", 7: "Steuer"} {
		f.DocType(id, n)
	}
	for id, n := range map[int]string{10: "Haus", 11: "Auto", 12: "Versicherung", 13: "Finanzen", 14: "Steuer", 15: "Freizeit", 16: "gesundheit"} {
		f.Tag(id, n)
	}
	for id, n := range map[int]string{20: "Stadtwerke Musterstadt", 21: "HUK-Coburg", 22: "Allianz", 23: "Elektro Schmidt", 24: "Finanzamt Musterstadt", 25: "Telekom"} {
		f.Correspondent(id, n)
	}
	for id, d := range map[int][2]string{30: {"Rechnungsnummer", "string"}, 31: {"Vertragsnummer", "string"}, 32: {"Versicherungsnummer", "string"},
		33: {"Kundennummer", "string"}, 34: {"Produkte", "longtext"}, 35: {"Garantie bis", "date"}, 36: {"Person", "select"}, 37: {"Betrag", "monetary"},
		38: {"Nächster Kündigungstermin", "date"}, 39: {"Kündigungsfrist", "string"}, 40: {"Anbieter", "string"}} {
		f.Field(id, d[0], d[1])
	}
	f.Options(36, "Anna", "Ben")
	f.Add(
		paperlessfake.Doc{ID: 1, Title: "Stromrechnung Oktober", DocumentType: 1, Correspondent: 20, Tags: []int{10}, Created: "2026-10-28",
			Fields: map[int]any{30: "SW-2026-104711", 33: "70012345", 37: "EUR84.50", 36: "opt-Anna"}},
		paperlessfake.Doc{ID: 2, Title: "Kfz-Versicherung 2026", DocumentType: 3, Correspondent: 21, Tags: []int{11, 12}, Created: "2025-11-02",
			Fields: map[int]any{32: "KFZ 123 456 789", 34: "Vollkasko\nSchutzbrief", 38: "2027-01-01", 39: "1 Monat", 37: "EUR412.80"}},
		paperlessfake.Doc{ID: 3, Title: "Privathaftpflicht Police", DocumentType: 3, Correspondent: 22, Tags: []int{12}, Created: "2024-03-15",
			Fields: map[int]any{32: "PH-7788-001", 34: "Privathaftpflicht", 38: "2027-03-15", 39: "3 Monate"}},
		paperlessfake.Doc{ID: 4, Title: "Waschmaschine Kassenbon", DocumentType: 5, Correspondent: 23, Tags: []int{10}, Created: "2025-06-03",
			Fields: map[int]any{34: "Waschmaschine WM 7 Pro", 35: "2027-06-03", 37: "EUR649.00", 36: "opt-Ben"}},
		paperlessfake.Doc{ID: 5, Title: "Einkommensteuerbescheid 2025", DocumentType: 7, Correspondent: 24, Tags: []int{14}, Created: "2026-09-12"},
		paperlessfake.Doc{ID: 6, Title: "Mobilfunkvertrag", DocumentType: 2, Correspondent: 25, Tags: []int{13}, Created: "2025-01-10",
			Fields: map[int]any{31: "MV-55512", 33: "K-889911"}},
		paperlessfake.Doc{ID: 7, Title: "Abschlag Strom 2027", DocumentType: 4, Correspondent: 20, Tags: []int{10}, Created: "2026-11-02",
			Content: "Bitte überweisen Sie an IBAN DE89 3704 0044 0532 0130 00."},
	)
	return &paperless.Client{URL: f.URL, Token: "demo", PublicURL: "https://paperless.example"}
}
