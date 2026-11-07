package pages

import (
	"net/http"

	"github.com/mdopp/homes/internal/cards"
	"github.com/mdopp/homes/internal/modul"
)

// gruppen are the modules' pages as they are built, by module name.
type gruppen map[string][]modul.Route

func (gs gruppen) add(name, muster string, h http.HandlerFunc) {
	gs[name] = append(gs[name], modul.Route{Muster: muster, Handler: h})
}

// Gruppen are each module's pages (ADR 0001 §1, homes#243), by module
// name; Homes mounts them behind the proxy secret with the core's (Start,
// Mehr, Box, Willkommen, …). Each is what its module serves with its
// backend set and, where a page says so, without one (notSet: „noch
// nicht eingerichtet“); HOMES_MODULE_AUS unsets an off module's backend,
// so its group is a box's without it. With einkauf off, the music wish
// list's page is musik's.
func Gruppen(cfg StartConfig) map[string][]modul.Route {
	gs := gruppen{}
	if cfg.HA != nil { // the word each speaker listens for, as it reports it (owner 2026-10-30)
		gs.add("geraete", AktivierungswortPath, Aktivierungswort(cfg))
	}
	gs.add("geraete", "/raeume", Raeume(cfg))
	gs.add("geraete", "/medien", Medien(cfg))
	gs.add("geraete", "/medien/", Medien(cfg))         // a path it does not serve: 404 and the way back
	gs.add("geraete", cards.ThingPages, Ding(cfg))     // each thing's own page, which the widgets name (haus#28)
	gs.add("geraete", cards.SwitchPath, Schalten(cfg)) // a page's switch, the widgets' rule (haus#35)
	if !cfg.OhneEnergie {
		gs.add("energie", "/energie", Energie(cfg))
		gs.add("energie", "/energie/", Energie(cfg)) // a path it does not serve: 404 and the way back
	}
	gs.add("sprechen", "/sprechen", Sprechen(cfg)) // the Sprechen field to the Bote, and its reply (haus#12)
	gs.add("sprechen", HoerenPath, Hoeren(cfg))    // the field's microphone, HA's speech-to-text (haus#63)
	gs.add("kalender", "/kalender", Kalender(cfg))
	gs.add("kalender", "/kalender/verwalten", KalenderVerwalten(cfg))     // Deine Kalender: hinzufügen, Farbe, Trennen (haus#66)
	gs.add("kalender", "/kalender/hinzufuegen", KalenderHinzufuegen(cfg)) // Google, ein Abo, ein neuer Kalender
	gs.add("kalender", "/kalender/aendern", KalenderAendern(cfg))         // one calendar's buttons
	gs.add("kalender", cards.ReminderPages, Erinnerung(cfg))
	gs.add("kontakte", KontaktePath, Kontakte(cfg)) // the address book, by name (owner 2026-11-05)
	gs.add("kontakte", cards.ContactPages, Kontakt(cfg))
	gs.add("kontakte", MeineKontaktePath, MeineKontakte(cfg))     // the Immich key owner's own, each to its photos (owner 2026-11-06)
	gs.add("kontakte", MeineKontaktePath+"/", MeineKontakte(cfg)) // …/<uid>/fotos
	gs.add("kontakte", DieselbePath, Dieselbe(cfg))               // „Dieselbe Person?“: faces Immich holds close, for the key's owner
	gs.add("kontakte", PruefenPath, Pruefen(cfg))                 // Kontakt prüfen lassen: the link to a contact (owner 2026-11-07)
	gs.add("kontakte", AntwortenPath, Antworten(cfg))             // its answers, before → after, taken or not
	gs.add("kontakte", AntwortenPath+"/", Antworten(cfg))
	if cfg.Einkauf != nil || cfg.Wuensche != nil { // haus#118; with einkauf off, the music wish list only (ADR 0001 H4)
		name := "einkauf"
		if cfg.Einkauf == nil {
			name = "musik"
		}
		gs.add(name, EinkaufPath, Einkauf(cfg))
		gs.add(name, EinkaufRezeptePath, Einkauf(cfg))
	}
	gs.add("notizen", "/notizen", Notizen(cfg))
	gs.add("notizen", cards.NotePages, Notizen(cfg))             // a note's own page
	gs.add("dokumente", DokumentePath, Dokumente(cfg))           // the ways into the archive (haus#68)
	gs.add("dokumente", DokumenteSuchePath, DokumenteSuche(cfg)) // the household's documents by words, Bereich, type, person (owner 2026-11-06)
	gs.add("dokumente", ScannenPath, Scannen(cfg))               // the scan orders to Laib (haus#76, laib#568)
	gs.add("dokumente", TrennblattPath, Trennblatt(cfg))         // separator sheets to print, PATCH T (haus#161)
	gs.add("dokumente", cards.DocumentPages, Dokument(cfg))
	musikGruppe(gs, cfg)
	gs.add("podcasts", PodcastsPath, Podcasts(cfg))            // haus#123; off, its trace (modulAus)
	gs.add("gesehen", GesehenPath, Gesehen(cfg))               // haus#152
	gs.add("sicherung", DatenPath, Nachrichten(cfg))           // what goes into the person's archive and how to switch it on (haus#207)
	gs.add("sicherung", NachrichtenPath, Nachrichten(cfg))     // the page's old address, a redirect
	gs.add("standort", StandortPath, Standort(cfg))            // the person's Reitti token for the app's Standortverlauf (owner 2026-11-02)
	gs.add("netzlaufwerk", "/netzlaufwerk", Netzlaufwerk(cfg)) // each person's network-drive password (haus#52)
	return gs
}

// musikGruppe is the music's pages: the inbox beets sorts in (haus#107),
// and the records with the wish list (owner 2026-10-26): one's own
// Discogs, and with the household's Discogs app Platte erfassen.
func musikGruppe(gs gruppen, cfg StartConfig) {
	if cfg.MusicInbox != "" {
		gs.add("musik", MusikPath, Musik(cfg))
	}
	if cfg.Wuensche != nil {
		gs.add("musik", DiscogsPath, Discogs(cfg)) // connect one's own Discogs; without the app's keys, how to set them
	}
	if cfg.Wuensche != nil && cfg.Discogs != nil {
		gs.add("musik", DiscogsZurueckPath, Discogs(cfg)) // the way back from Discogs
		gs.add("musik", PlattePath, Platte(cfg))
		gs.add("musik", PlatteBildPath, Platte(cfg))
	}
}
