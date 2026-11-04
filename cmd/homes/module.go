package main

import (
	"log"

	"github.com/mdopp/homes/internal/modul"
	"github.com/mdopp/homes/internal/pages"
)

// kern are the variables of what is always on and no module (ADR 0001 §1,
// §7): Laib, the proxy secret, the data folder, the address, the name.
var kern = []string{"LAIB_URL", "LAIB_TOKEN", "LAIB_PUBLIC_URL", "LAIB_DOMAIN", "HOMES_PROXY_SECRET", "HOMES_DATA",
	"HOMES_LISTEN", "HOMES_PUBLIC_URL", "HOMES_ASSISTANT", "HOMES_HAUSHALT_TOKEN", "HOMES_PHONE_COUNTRY"}

// radicale is what the Radicale modules read (one account, ADR 0001 §7).
var radicale = []string{"HOMES_RADICALE_URL", "HOMES_RADICALE_USER", "HOMES_RADICALE_PASSWORD"}

// bau is one module's decision: on, or off because its backend is
// missing (fehlt). An off module keeps its entries and offers: they are
// what its trace names (ADR 0001 §10), never served.
func bau(m modul.Modul, an bool, grund string) modul.Modul {
	if !an {
		m.Warum, m.Grund = modul.Fehlt, grund
	}
	return m
}

func mehrEintrag(titel, pfad string) []modul.Eintrag {
	return []modul.Eintrag{{Titel: titel, Pfad: pfad, Ort: modul.Mehr}}
}

// katalog is Homes' areas (ADR 0001 §7) in catalog order, each on or off
// by what the box gave Homes: today's checks of each backend, made
// explicit. musicInbox is the music inbox's folder ("" without one).
func katalog(s site, musicInbox string) []modul.Modul {
	hh, n := s.hh, s.napi
	docs := s.docsMail != "" || n.UploadDir != "" || s.paperlessPublic != "" || hh.Documents != nil
	var dokApp modul.Angebot
	if hh.Documents != nil {
		dokApp.Widgets = []string{"tool:doc"}
	}
	if n.UploadDir != "" {
		dokApp.Teilen = []string{"dokument"}
	}
	return []modul.Modul{
		bau(modul.Modul{Name: "geraete", Titel: "Geräte", Backend: "home-assistant",
			Env:   []string{"HOMES_HA_URL", "HOMES_HA_TOKEN", "HOMES_HA_PUBLIC_URL", "HOMES_COVER_INVERTED"},
			Nav:   []modul.Eintrag{{Titel: "Geräte", Pfad: "/raeume", Ort: modul.Leiste}},
			Start: []string{"im-haus", "meine-geraete"}, App: modul.Angebot{Widgets: []string{"geraet"}}},
			n.HA != nil, "Auf dieser Box läuft kein Home Assistant."),
		bau(modul.Modul{Name: "energie", Titel: "Energie", Backend: "home-assistant", Braucht: []string{"geraete"},
			Env: []string{"HOMES_ENERGY_FLOW", "HOMES_ENERGY_TOTALS", "HOMES_ENERGY_STATS"},
			Nav: []modul.Eintrag{{Titel: "Energie", Pfad: "/energie", Ort: modul.Leiste}},
			App: modul.Angebot{Widgets: []string{"energie"}}},
			n.HA != nil, "Auf dieser Box läuft kein Home Assistant."),
		bau(modul.Modul{Name: "sprechen", Titel: "Sprechen", Backend: "bote",
			Env: []string{"HOMES_BOTE_URL", "HOMES_BOTE_KEY", "HOMES_STT_PROVIDER", "HOMES_STT_LANGUAGE", "HOMES_BOTE_SIGNAL"}},
			s.bote != nil, "Auf dieser Box läuft kein Bote."),
		bau(modul.Modul{Name: "kalender", Titel: "Kalender", Backend: "radicale", Standard: "caldav",
			Env: append([]string{"HOMES_RADICALE_CALENDAR", "HOMES_RADICALE_PUBLIC_URL"}, radicale...),
			Nav: []modul.Eintrag{{Titel: "Kalender", Pfad: "/kalender", Ort: modul.Haushalt}}, Start: []string{"heute"},
			App: modul.Angebot{Widgets: []string{"tool:task"}}},
			hh.Calendar != nil, "Auf dieser Box läuft kein Radicale."),
		bau(modul.Modul{Name: "kontakte", Titel: "Kontakte", Backend: "radicale", Standard: "carddav",
			Env: append([]string{"HOMES_RADICALE_ADDRESSBOOK"}, radicale...),
			App: modul.Angebot{Widgets: []string{"tool:contacts"}, Sicherung: []string{"kontakte"}}},
			hh.Contacts != nil, "Auf dieser Box läuft kein Radicale."),
		bau(modul.Modul{Name: "konto", Titel: "Homes-Konto", Backend: "radicale", Env: radicale,
			App: modul.Angebot{Konto: []string{"kontakte", "kalender"}}},
			hh.Calendar != nil, "Auf dieser Box läuft kein Radicale."),
		bau(modul.Modul{Name: "einkauf", Titel: "Einkauf", Backend: "radicale", Env: radicale,
			Nav: []modul.Eintrag{{Titel: "Einkauf", Pfad: pages.EinkaufPath, Ort: modul.Haushalt}}},
			hh.Einkauf != nil, "Auf dieser Box läuft kein Radicale."),
		bau(modul.Modul{Name: "notizen", Titel: "Notizen", Backend: "file-share",
			Env: []string{"HOMES_NOTES_DIR", "HOMES_QMD_URL", "HOMES_QMD_COLLECTION", "HOMES_LLM_URL", "HOMES_LLM_KEY", "HOMES_LLM_MODEL"},
			Nav: []modul.Eintrag{{Titel: "Notizen", Pfad: "/notizen", Ort: modul.Haushalt}},
			App: modul.Angebot{Widgets: []string{"tool:note"}, Teilen: []string{"merken"}}},
			hh.Notes != nil, "Auf dieser Box gibt es keinen Ordner für Notizen."),
		bau(modul.Modul{Name: "dokumente", Titel: "Dokumente", Backend: "paperless",
			Env: []string{"HOMES_PAPERLESS_URL", "HOMES_PAPERLESS_TOKEN", "HOMES_PAPERLESS_PUBLIC_URL", "HOMES_PAPERLESS_LAN_ONLY",
				"HOMES_UPLOAD_DIR", "HOMES_DOCUMENTS_MAIL", "HOMES_DOCUMENTS_DIR", "HOMES_QMD_DOCUMENTS", "HOMES_VERTRAG_ARTEN"},
			Nav: mehrEintrag("Dokumente", pages.DokumentePath), App: dokApp},
			docs, "Auf dieser Box läuft kein Paperless."),
		bau(modul.Modul{Name: "musik", Titel: "Musik", Backend: "beets",
			Env: []string{"HOMES_MUSIC_INBOX", "HOMES_JELLYFIN_URL", "HOMES_JELLYFIN_TOKEN", "HOMES_DISCOGS_KEY", "HOMES_DISCOGS_SECRET", "HOMES_LLM_VISION_MODEL"},
			Nav: append(mehrEintrag("Musik", pages.MusikPath), append(mehrEintrag("Platte erfassen", pages.PlattePath), mehrEintrag("Discogs", pages.DiscogsPath)...)...)},
			musicInbox != "" || hh.Wuensche != nil, "Auf dieser Box gibt es keinen Musik-Eingang."),
		bau(modul.Modul{Name: "podcasts", Titel: "Podcasts", Backend: "audiobookshelf",
			Env: []string{"HOMES_ABS_URL", "HOMES_ABS_TOKEN"}, Nav: mehrEintrag("Podcasts", pages.PodcastsPath)},
			s.podcasts != nil, "Auf dieser Box läuft kein Audiobookshelf."),
		bau(modul.Modul{Name: "gesehen", Titel: "Gesehen & Gehört", Backend: "media",
			Env: []string{"HOMES_JELLYFIN_URL", "HOMES_JELLYFIN_KEY", "HOMES_ABS_URL", "HOMES_ABS_TOKEN"},
			Nav: mehrEintrag("Gesehen & Gehört", pages.GesehenPath)},
			s.gesehen != nil, "Auf dieser Box laufen weder Jellyfin noch Audiobookshelf."),
		bau(modul.Modul{Name: "sicherung", Titel: "Daten sichern", Backend: "archiv",
			Env: []string{"HOMES_ARCHIV_DIR", "HOMES_ARCHIV_INTERN", "HOMES_ARCHIV_SECRET_"},
			Nav: mehrEintrag("Daten sichern", pages.DatenPath),
			App: modul.Angebot{Sicherung: []string{"whatsapp", "signal", "sms"}, Mehr: []string{"daten-sichern"}}},
			n.Sicherung != nil, "Auf dieser Box gibt es kein Archiv für Nachrichten."),
		bau(modul.Modul{Name: "standort", Titel: "Standortverlauf", Backend: "reitti",
			Env: []string{"HOMES_REITTI_URL", "HOMES_REITTI_PUBLIC_URL"}, Nav: mehrEintrag("Standortverlauf", pages.StandortPath),
			App: modul.Angebot{Teilen: []string{"standort-import"}, Mehr: []string{"standortverlauf"}}},
			n.Reitti != nil, "Auf dieser Box läuft kein Reitti."),
		bau(modul.Modul{Name: "netzlaufwerk", Titel: "Netzlaufwerk", Backend: "file-share",
			Env: []string{"HOMES_SAMBA_DIR", "HOMES_SAMBA_HOST"}, Nav: mehrEintrag("Netzlaufwerk", "/netzlaufwerk")},
			s.samba != nil, "Auf dieser Box gibt es kein Netzlaufwerk."),
		bau(modul.Modul{Name: "gesundheit", Titel: "Gesundheit", Backend: "gesundheit", Env: []string{"HOMES_GESUNDHEIT_URL"}},
			s.gesundheit != "", "Auf dieser Box läuft Gesundheit nicht."),
		bau(modul.Modul{Name: "werkstatt", Titel: "Werkstatt", Env: []string{"HOMES_WERKSTATT_REPOS", "HOMES_GITHUB_API"}},
			s.tickets != nil, "Für diese Box ist keine Werkstatt eingetragen."),
		bau(modul.Modul{Name: "nutzung", Titel: "Was wird genutzt", Backend: "usage-metrics",
			Env: []string{"HOMES_USAGE_URL", "HOMES_USAGE_TOKEN"}},
			s.usage != nil, "Auf dieser Box läuft kein usage-metrics."),
	}
}

// logModule builds the box's module list and logs it (ADR 0001): what
// this box can do, and why not. A broken catalog stops Homes.
func logModule(s site) *modul.Liste {
	l, err := modul.Bauen(katalog(s, musicInbox()))
	if err != nil {
		log.Fatalf("homes: %v", err)
	}
	log.Printf("homes: %s", l)
	return l
}
