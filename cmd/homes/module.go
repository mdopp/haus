package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/mdopp/homes/internal/haushalt"
	"github.com/mdopp/homes/internal/laib"
	"github.com/mdopp/homes/internal/modul"
	"github.com/mdopp/homes/internal/pages"
	"github.com/mdopp/homes/internal/texte"
)

// kern are the variables of what is always on and no module (ADR 0001 §1,
// §7): Laib, the proxy secret, the data folder, the address, the name.
var kern = []string{"LAIB_URL", "LAIB_TOKEN", "LAIB_PUBLIC_URL", "LAIB_DOMAIN", "HOMES_PROXY_SECRET", "HOMES_DATA",
	"HOMES_LISTEN", "HOMES_PUBLIC_URL", "HOMES_ASSISTANT", "HOMES_HAUSHALT_TOKEN", "HOMES_PHONE_COUNTRY", "HOMES_MODULE_AUS"}

// radicale is what the Radicale modules read (one account, ADR 0001 §7).
var radicale = []string{"HOMES_RADICALE_URL", "HOMES_RADICALE_USER", "HOMES_RADICALE_PASSWORD"}

// bau is one module's decision: on, or off because its backend is
// missing (fehlt). An off module keeps its entries and offers: they are
// what its trace names (ADR 0001 §10), never served.
func bau(m modul.Modul, an bool, grund string) modul.Modul {
	m.Titel = texte.Titel(m.Name)                           // the one table of words (internal/texte)
	m.Admins = m.Name == "werkstatt" || m.Name == "nutzung" // their trace is on the Box page (ADR 0001 §10)
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
		bau(modul.Modul{Name: "geraete", Backend: "home-assistant",
			Env:   []string{"HOMES_HA_URL", "HOMES_HA_TOKEN", "HOMES_HA_PUBLIC_URL", "HOMES_COVER_INVERTED"},
			Nav:   []modul.Eintrag{{Titel: "Geräte", Pfad: "/raeume", Ort: modul.Leiste}},
			Start: []string{"im-haus", "meine-geraete"}, App: modul.Angebot{Widgets: []string{"geraet"}}},
			n.HA != nil, "Auf dieser Box läuft kein Home Assistant."),
		bau(modul.Modul{Name: "energie", Backend: "home-assistant", Braucht: []string{"geraete"},
			Env: []string{"HOMES_ENERGY_FLOW", "HOMES_ENERGY_TOTALS", "HOMES_ENERGY_STATS"},
			Nav: []modul.Eintrag{{Titel: "Energie", Pfad: "/energie", Ort: modul.Leiste}},
			App: modul.Angebot{Widgets: []string{"energie"}}},
			n.HA != nil, "Auf dieser Box läuft kein Home Assistant."),
		bau(modul.Modul{Name: "sprechen", Backend: "bote",
			Env: []string{"HOMES_BOTE_URL", "HOMES_BOTE_KEY", "HOMES_STT_PROVIDER", "HOMES_STT_LANGUAGE", "HOMES_BOTE_SIGNAL"}},
			s.bote != nil, "Auf dieser Box läuft kein Bote."),
		bau(modul.Modul{Name: "kalender", Backend: "radicale", Standard: "caldav",
			Env: append([]string{"HOMES_RADICALE_CALENDAR", "HOMES_RADICALE_PUBLIC_URL"}, radicale...),
			Nav: []modul.Eintrag{{Titel: "Kalender", Pfad: "/kalender", Ort: modul.Haushalt}}, Start: []string{"heute"},
			App: modul.Angebot{Widgets: []string{"tool:task"}}},
			hh.Calendar != nil, "Auf dieser Box läuft kein Radicale."),
		bau(modul.Modul{Name: "kontakte", Backend: "radicale", Standard: "carddav",
			Env: append([]string{"HOMES_RADICALE_ADDRESSBOOK"}, radicale...),
			App: modul.Angebot{Widgets: []string{"tool:contacts"}, Sicherung: []string{"kontakte"}}},
			hh.Contacts != nil, "Auf dieser Box läuft kein Radicale."),
		bau(modul.Modul{Name: "konto", Backend: "radicale", Env: radicale,
			App: modul.Angebot{Konto: []string{"kontakte", "kalender"}}},
			n.Konto != nil, "Auf dieser Box läuft kein Radicale."),
		bau(modul.Modul{Name: "einkauf", Backend: "radicale", Env: radicale,
			Nav: []modul.Eintrag{{Titel: "Einkauf", Pfad: pages.EinkaufPath, Ort: modul.Haushalt}}},
			hh.Einkauf != nil, "Auf dieser Box läuft kein Radicale."),
		bau(modul.Modul{Name: "notizen", Backend: "file-share",
			Env: []string{"HOMES_NOTES_DIR", "HOMES_QMD_URL", "HOMES_QMD_COLLECTION", "HOMES_LLM_URL", "HOMES_LLM_KEY", "HOMES_LLM_MODEL"},
			Nav: []modul.Eintrag{{Titel: "Notizen", Pfad: "/notizen", Ort: modul.Haushalt}},
			App: modul.Angebot{Widgets: []string{"tool:note"}, Teilen: []string{"merken"}}},
			hh.Notes != nil, "Auf dieser Box gibt es keinen Ordner für Notizen."),
		bau(modul.Modul{Name: "dokumente", Backend: "paperless",
			Env: []string{"HOMES_PAPERLESS_URL", "HOMES_PAPERLESS_TOKEN", "HOMES_PAPERLESS_PUBLIC_URL", "HOMES_PAPERLESS_LAN_ONLY",
				"HOMES_UPLOAD_DIR", "HOMES_DOCUMENTS_MAIL", "HOMES_DOCUMENTS_DIR", "HOMES_QMD_DOCUMENTS", "HOMES_VERTRAG_ARTEN"},
			Nav: mehrEintrag("Dokumente", pages.DokumentePath), App: dokApp},
			docs, "Auf dieser Box läuft kein Paperless."),
		bau(modul.Modul{Name: "musik", Backend: "beets",
			Env: []string{"HOMES_MUSIC_INBOX", "HOMES_JELLYFIN_URL", "HOMES_JELLYFIN_TOKEN", "HOMES_DISCOGS_KEY", "HOMES_DISCOGS_SECRET", "HOMES_LLM_VISION_MODEL"},
			Nav: append(mehrEintrag("Musik", pages.MusikPath), append(mehrEintrag("Platte erfassen", pages.PlattePath), mehrEintrag("Discogs", pages.DiscogsPath)...)...)},
			musicInbox != "" || hh.Wuensche != nil, "Auf dieser Box gibt es keinen Musik-Eingang."),
		bau(modul.Modul{Name: "podcasts", Backend: "audiobookshelf",
			Env: []string{"HOMES_ABS_URL", "HOMES_ABS_TOKEN"}, Nav: mehrEintrag("Podcasts", pages.PodcastsPath)},
			s.podcasts != nil, "Auf dieser Box läuft kein Audiobookshelf."),
		bau(modul.Modul{Name: "gesehen", Backend: "media",
			Env: []string{"HOMES_JELLYFIN_URL", "HOMES_JELLYFIN_KEY", "HOMES_ABS_URL", "HOMES_ABS_TOKEN"},
			Nav: mehrEintrag("Gesehen & Gehört", pages.GesehenPath)},
			s.gesehen != nil, "Auf dieser Box laufen weder Jellyfin noch Audiobookshelf."),
		bau(modul.Modul{Name: "sicherung", Backend: "archiv",
			Env: []string{"HOMES_ARCHIV_DIR", "HOMES_ARCHIV_INTERN", "HOMES_ARCHIV_SECRET_"},
			Nav: mehrEintrag("Daten sichern", pages.DatenPath),
			App: modul.Angebot{Sicherung: []string{"whatsapp", "signal", "sms"}, Mehr: []string{"daten-sichern"}}},
			n.Sicherung != nil, "Auf dieser Box gibt es kein Archiv für Nachrichten."),
		bau(modul.Modul{Name: "standort", Backend: "reitti",
			Env: []string{"HOMES_REITTI_URL", "HOMES_REITTI_PUBLIC_URL"}, Nav: mehrEintrag("Standortverlauf", pages.StandortPath),
			App: modul.Angebot{Teilen: []string{"standort-import"}, Mehr: []string{"standortverlauf"}}},
			n.Reitti != nil, "Auf dieser Box läuft kein Reitti."),
		bau(modul.Modul{Name: "netzlaufwerk", Backend: "file-share",
			Env: []string{"HOMES_SAMBA_DIR", "HOMES_SAMBA_HOST"}, Nav: mehrEintrag("Netzlaufwerk", "/netzlaufwerk")},
			s.samba != nil, "Auf dieser Box gibt es kein Netzlaufwerk."),
		bau(modul.Modul{Name: "gesundheit", Backend: "gesundheit", Env: []string{"HOMES_GESUNDHEIT_URL"}},
			s.gesundheit != "", "Auf dieser Box läuft Gesundheit nicht."),
		bau(modul.Modul{Name: "werkstatt", Env: []string{"HOMES_WERKSTATT_REPOS", "HOMES_GITHUB_API"}},
			s.tickets != nil, "Für diese Box ist keine Werkstatt eingetragen."),
		bau(modul.Modul{Name: "nutzung", Backend: "usage-metrics",
			Env: []string{"HOMES_USAGE_URL", "HOMES_USAGE_TOKEN"}},
			s.usage != nil, "Auf dieser Box läuft kein usage-metrics."),
	}
}

// logModule builds the box's module list, switches off what
// HOMES_MODULE_AUS names, and logs it (ADR 0001): what this box can do,
// and why not. A broken catalog stops Homes.
func logModule(s *site) *modul.Liste {
	l, err := modul.Bauen(ausschalten(s, katalog(*s, s.musik), os.Getenv("HOMES_MODULE_AUS")))
	if err != nil {
		log.Fatalf("homes: %v", err)
	}
	log.Printf("homes: %s", l)
	return l
}

// schliesseWuensche ticks off, once Laib answers, the open wish of each
// module that is on (ADR 0001 §10, laib#1038): Homes closes it as the
// homes hand with no person, which Laib reads as umgesetzt. A Laib that
// takes no wish ends it; one that does not answer is asked again.
func schliesseWuensche(ctx context.Context, l *laib.Client, liste *modul.Liste, wait time.Duration) {
	for try := 0; try < 8; try++ {
		if try > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait << try):
			}
		}
		if geht, err := l.NimmtWuensche(ctx); !geht {
			if err == nil {
				return // an older Laib takes no wish: none to close
			}
			continue
		}
		je, err := l.Wuensche(ctx, liste.Frage())
		if err != nil {
			log.Printf("homes: wishes of the modules on: %v", err)
			continue
		}
		for name, w := range je {
			if liste.An(name) && (w.Stand == "angefordert" || w.Stand == "wartet" || w.Stand == "eingerichtet") {
				if _, err := l.AufgabeErledigt(ctx, w.Aufgabe, ""); err != nil {
					log.Printf("homes: closing the wish for %s (Box-Aufgabe #%d): %v", name, w.Aufgabe, err)
				} else {
					log.Printf("homes: %s is on: its wish (Box-Aufgabe #%d) is ticked off", name, w.Aufgabe)
				}
			}
		}
		return
	}
}

// schaltbar are the modules HOMES_MODULE_AUS can switch off today (ADR 0001
// steps H3–H5: the leaf areas, each with its own paths and backend; the
// documents, the notes, the music and the lists; the calendar, the
// contacts and the account on the one Radicale connection), each with how
// its backend leaves the site: off, its field stays nil, so no page,
// route, tool or background work learns of it (ADR 0001 §1). The others
// follow in H6.
var schaltbar = map[string]func(*site){
	"gesundheit":   func(s *site) { s.gesundheit = "" },
	"werkstatt":    func(s *site) { s.tickets = nil },
	"netzlaufwerk": func(s *site) { s.samba = nil },
	"podcasts":     func(s *site) { s.podcasts, s.hh.Podcasts, s.napi.Haushalt.Podcasts = nil, nil, nil },
	"gesehen":      func(s *site) { s.gesehen = nil },
	"standort":     func(s *site) { s.napi.Reitti = nil },
	"sicherung":    func(s *site) { s.napi.Sicherung = nil },
	// the Dokumente page's ways in, the app's upload, the doc widget and tools
	"dokumente": func(s *site) {
		s.haushalt(func(h *haushalt.Config) { h.Documents = nil })
		s.napi.UploadDir, s.docsMail, s.paperlessPublic = "", "", ""
	},
	// the folder, its search by meaning, the note widget, Merken and the tools
	"notizen": func(s *site) { s.haushalt(func(h *haushalt.Config) { h.Notes, h.Library = nil, nil }) },
	// the inbox, the wish list (a shared song is a note again), Discogs and Platte
	"musik": func(s *site) { s.musik = ""; s.haushalt(func(h *haushalt.Config) { h.Wuensche = nil }) },
	// the lists and their tools; the music wish list keeps its own (hh.Wuensche.Listen)
	"einkauf": func(s *site) { s.haushalt(func(h *haushalt.Config) { h.Einkauf = nil }) },
	// H5, one Radicale account: each module leaves only its own field, the
	// others keep theirs (Contacts and Konto are their own, not Calendar)
	"kalender": func(s *site) { s.haushalt(func(h *haushalt.Config) { h.Calendar = nil }) },
	"kontakte": func(s *site) { s.haushalt(func(h *haushalt.Config) { h.Contacts = nil }) },
	"konto":    func(s *site) { s.napi.Konto = nil },
	// H6: the house, its energy, the Sprechen field, the counts. With
	// geraete off Homes speaks no HA: the house paths, the microphone (HA's
	// speech-to-text) and the voice counts go; the event stream stays (core)
	"geraete":  func(s *site) { s.napi.HA, s.voice = nil, nil },
	"energie":  func(s *site) { s.napi.OhneEnergie = true },
	"sprechen": func(s *site) { s.bote, s.boteSignal = nil, "" },
	"nutzung":  func(s *site) { s.usage, s.voice = nil, nil },
}

// haushalt changes the Haushalt source's backends, the Bote's and the
// tool widgets' (napi's copy) alike.
func (s *site) haushalt(f func(*haushalt.Config)) {
	f(&s.hh)
	f(&s.napi.Haushalt)
}

// ausschalten applies HOMES_MODULE_AUS, "podcasts,standort" (ADR 0001 §5):
// each module named that is on is marked aus and its backend taken out of
// s. A name Homes does not know, or cannot switch off yet, is logged and
// ignored, never fatal: Laib and Homes release apart.
func ausschalten(s *site, ms []modul.Modul, liste string) []modul.Modul {
	for i := range ms {
		ms[i].Schaltbar = schaltbar[ms[i].Name] != nil
	}
	for _, name := range strings.Split(liste, ",") {
		name = strings.TrimSpace(name)
		i := slices.IndexFunc(ms, func(m modul.Modul) bool { return m.Name == name })
		switch {
		case name == "":
		case i < 0:
			log.Printf("homes: HOMES_MODULE_AUS: no module %q, ignored", name)
		case schaltbar[name] == nil:
			log.Printf("homes: HOMES_MODULE_AUS: %s cannot be switched off yet (ADR 0001 H6), it stays on", name)
		case ms[i].An():
			ms[i].Warum, ms[i].Grund = modul.Aus, texte.T(texte.GrundAus)
			schaltbar[name](s)
		}
	}
	for i, m := range ms { // a module whose Braucht went off goes with it: energie with geraete
		for _, b := range m.Braucht {
			if j := slices.IndexFunc(ms, func(o modul.Modul) bool { return o.Name == b }); ms[i].An() && j >= 0 && !ms[j].An() {
				ms[i].Warum, ms[i].Grund = modul.Aus, fmt.Sprintf(texte.T(texte.GrundBraucht), ms[j].Titel)
				schaltbar[m.Name](s)
			}
		}
	}
	return ms
}

// startLeaf starts the modules' background work, only for those on: the
// podcasts' new episodes (haus#123), the backups' old parts (haus#207),
// the music wishes that arrived (laib#695).
func startLeaf(ctx context.Context, s site) {
	if s.hh.Wuensche != nil {
		go tickWishes(ctx, s.hh.Wuensche, s.ops)
	}
	if s.podcasts != nil && s.ops != nil {
		go tellNewEpisodes(ctx, s.podcasts, s.ops)
	}
	if s.napi.Sicherung != nil {
		go s.napi.Sicherung.Pflegen(nil) // parts untouched for a week go, now and daily
	}
}
