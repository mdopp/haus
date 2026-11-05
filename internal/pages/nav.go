package pages

import (
	"html"
	"net/http"
	"slices"
	"strings"

	"github.com/mdopp/homes/internal/modul"
)

// The family's selection (haus#35, haus#67; ADR 0001 H6): the bar (Start,
// Geräte, Haushalt, Energie, Mehr), the Haushalt pills (Kalender, Einkauf,
// Notizen) and Mehr's list. Each entry is the core's (kern) or a module's
// (Nav); a module's is there while the module is on. Rang keeps today's
// order: the core's entries sit between the modules' (owner 2026-10-24,
// 2026-10-27: Dokumente unter Mehr). Every page is in exactly one place.

// The icons, 24×24, stroked.
const (
	iconStart        = `<path d="M4 11l8-7 8 7v9h-5v-6H9v6H4z"/>`
	iconGeraete      = `<rect x="4" y="4" width="7" height="7" rx="1.5"/><rect x="13" y="4" width="7" height="7" rx="1.5"/><rect x="4" y="13" width="7" height="7" rx="1.5"/><rect x="13" y="13" width="7" height="7" rx="1.5"/>`
	iconHaushalt     = `<rect x="4" y="5" width="16" height="15" rx="2"/><path d="M4 10h16M9 3v4M15 3v4"/>`
	iconEnergie      = `<path d="M13 3L5 13.5h6L10 21l8-10.5h-6z"/>`
	iconMehr         = `<circle cx="5.5" cy="12" r="1.3"/><circle cx="12" cy="12" r="1.3"/><circle cx="18.5" cy="12" r="1.3"/>`
	iconDokumente    = `<path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z"/><path d="M14 3v5h5M9 13h6M9 17h6"/>`
	iconPlatte       = `<circle cx="12" cy="12" r="9"/><circle cx="12" cy="12" r="2.5"/>`
	iconDiscogs      = `<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>`
	iconPodcasts     = `<rect x="9" y="3" width="6" height="11" rx="3"/><path d="M5 11a7 7 0 0 0 14 0M12 18v3"/>`
	iconGesehen      = `<path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12z"/><circle cx="12" cy="12" r="3"/>`
	iconMusik        = `<path d="M9 18V5l11-2v13"/><circle cx="6" cy="18" r="3"/><circle cx="17" cy="16" r="3"/>`
	iconDaten        = `<path d="M4 5h16v11H9l-5 4z"/><path d="M12 7.5v5M9.5 10L12 12.5 14.5 10"/>`
	iconStandort     = `<path d="M12 21s-6-5.5-6-11a6 6 0 0 1 12 0c0 5.5-6 11-6 11z"/><circle cx="12" cy="10" r="2.2"/>`
	iconNetzlaufwerk = `<rect x="3" y="5" width="18" height="6" rx="1.5"/><rect x="3" y="13" width="18" height="6" rx="1.5"/><path d="M7 8h.01M7 16h.01"/>`
	iconAufgaben     = `<path d="M9 11l2 2 4-4"/><rect x="4" y="4" width="16" height="16" rx="3"/>`
	iconBox          = `<rect x="3" y="7" width="18" height="12" rx="2"/><path d="M3 11h18M8 15h2"/>`
	abmeldenIcon     = `<path d="M14 4h4a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2h-4M10 16l-4-4 4-4M6 12h10"/>`
)

// angemeldet: someone is signed in (Authelia names them).
func angemeldet(r *http.Request) bool { return r.Header.Get("Remote-User") != "" }

// navVon is each module's entries, by module name.
var navVon = map[string][]modul.Eintrag{
	"geraete":      {{Titel: "Geräte", Pfad: "/raeume", Icon: iconGeraete, Ort: modul.Leiste, Rang: 20}},
	"energie":      {{Titel: "Energie", Pfad: "/energie", Icon: iconEnergie, Ort: modul.Leiste, Rang: 40}},
	"kalender":     {{Titel: "Kalender", Pfad: "/kalender", Ort: modul.Haushalt, Rang: 10}},
	"kontakte":     {{Titel: "Kontakte", Pfad: KontaktePath, Ort: modul.Haushalt, Rang: 15}},
	"einkauf":      {{Titel: "Einkauf", Pfad: EinkaufPath, Ort: modul.Haushalt, Rang: 20}},
	"notizen":      {{Titel: "Notizen", Pfad: "/notizen", Ort: modul.Haushalt, Rang: 30}},
	"dokumente":    {{Titel: "Dokumente", Pfad: DokumentePath, Icon: iconDokumente, Ort: modul.Mehr, Rang: 10}},
	"podcasts":     {{Titel: "Podcasts", Pfad: PodcastsPath, Icon: iconPodcasts, Ort: modul.Mehr, Rang: 40}},
	"gesehen":      {{Titel: "Gesehen & Gehört", Pfad: GesehenPath, Icon: iconGesehen, Ort: modul.Mehr, Rang: 50, Fuer: angemeldet}},
	"sicherung":    {{Titel: "Daten sichern", Pfad: DatenPath, Icon: iconDaten, Ort: modul.Mehr, Rang: 80, Fuer: angemeldet}},
	"standort":     {{Titel: "Standortverlauf", Pfad: StandortPath, Icon: iconStandort, Ort: modul.Mehr, Rang: 90, Fuer: angemeldet}},
	"netzlaufwerk": {{Titel: "Netzlaufwerk", Pfad: "/netzlaufwerk", Icon: iconNetzlaufwerk, Ort: modul.Mehr, Rang: 100, Fuer: angemeldet}},
	// Platte erfassen and Discogs live on Musik (owner 2026-10-26); on their
	// own only on a box without a music inbox: the catalog chooses
	"musik": {{Titel: "Platte erfassen", Pfad: PlattePath, Icon: iconPlatte, Ort: modul.Mehr, Rang: 20},
		{Titel: "Discogs", Pfad: DiscogsPath, Icon: iconDiscogs, Ort: modul.Mehr, Rang: 30, Fuer: angemeldet},
		{Titel: "Musik", Pfad: MusikPath, Icon: iconMusik, Ort: modul.Mehr, Rang: 60}},
}

// Nav is a module's entries in the family's selection (ADR 0001 §1): the
// catalog gives them to the module; none for a module without one.
func Nav(name string) []modul.Eintrag { return slices.Clone(navVon[name]) }

// kern are the core's entries: Start and Mehr always (Mehr holds the
// account), Haushalt while it has a pill, Aufgaben for who is signed in
// and has some (haus#188; each module's setup is one since 2026-11-04,
// the guides of Apps einrichten went to the modules), Box for admins.
func kern(cfg StartConfig) []modul.Eintrag {
	return []modul.Eintrag{
		{Titel: "Start", Pfad: "/start", Icon: iconStart, Ort: modul.Leiste, Rang: 10},
		{Titel: "Haushalt", Pfad: "/kalender", Icon: iconHaushalt, Ort: modul.Leiste, Rang: 30},
		{Titel: "Mehr", Pfad: mehrPath, Icon: iconMehr, Ort: modul.Leiste, Rang: 50},
		{Titel: "Deine Aufgaben", Pfad: aufgabenPath, Icon: iconAufgaben, Ort: modul.Mehr, Rang: 110,
			Fuer: func(r *http.Request) bool { return angemeldet(r) && (cfg.Laib != nil || len(einrichtung(r, cfg)) > 0) }},
		{Titel: "Box", Pfad: "/box", Icon: iconBox, Ort: modul.Mehr, Rang: 120, Fuer: isAdmin},
	}
}

// eintraege are the entries at o this person sees on this box: the
// core's, and those of the modules on; without a module list, the core's.
func eintraege(r *http.Request, cfg StartConfig, o modul.Ort) []modul.Eintrag {
	return cfg.Module.Eintraege(r, o, kern(cfg)...)
}

// tabOf is the bar entry a page belongs to: Räume and Medien are Geräte,
// the Haushalt pills Haushalt, what Mehr can hold (Dokumente too, owner
// 2026-10-27) Mehr, on this box or not.
func tabOf(page string) string {
	switch page {
	case "Räume", "Medien":
		return "Geräte"
	}
	all := kern(StartConfig{})
	for _, es := range navVon {
		all = append(all, es...)
	}
	for _, e := range all {
		switch {
		case e.Titel != page:
		case e.Ort == modul.Haushalt:
			return "Haushalt"
		case e.Ort == modul.Mehr:
			return "Mehr"
		}
	}
	return page
}

func navLink(b *strings.Builder, e modul.Eintrag, class string, current bool) {
	b.WriteString("<a")
	if class != "" {
		b.WriteString(" class=" + class)
	}
	b.WriteString(" href=" + e.Pfad)
	if current {
		b.WriteString(" aria-current=page")
	}
	b.WriteString("><svg viewBox=\"0 0 24 24\" aria-hidden=true>" + e.Icon + "</svg>" + e.Titel + "</a>")
}

// familyNav is the family's selection (haus#35, haus#67): on the phone the
// bar at the bottom, Start · Geräte · Haushalt · Mehr; on a wide window a
// list on the right with Mehr open in it (its entries, who is signed in,
// Abmelden), so Mehr's own entry is the phone's only. The page (current,
// by name) is marked, and its bar entry. Haushalt leads to its first pill
// and is there while it has one.
func familyNav(r *http.Request, cfg StartConfig, current string) string {
	in, pills := tabOf(current), eintraege(r, cfg, modul.Haushalt)
	n := -1 // the open tasks beside Mehr and Aufgaben (owner 2026-11-04), counted once
	mitZahl := func(e modul.Eintrag) modul.Eintrag {
		if n < 0 {
			n = offeneZahl(r, cfg)
		}
		e.Titel += zahl(n)
		return e
	}
	var b strings.Builder
	b.WriteString("<nav class=tabs aria-label=Seiten>")
	for _, t := range eintraege(r, cfg, modul.Leiste) {
		class := ""
		switch t.Titel {
		case "Mehr":
			class = "zu-mehr"
			navLink(&b, mitZahl(t), class, t.Titel == in)
			continue
		case "Haushalt":
			if len(pills) == 0 {
				continue
			}
			t.Pfad = pills[0].Pfad
		}
		navLink(&b, t, class, t.Titel == in)
	}
	b.WriteString("<div class=mehr><p class=mehr-titel>Mehr</p>")
	for _, t := range eintraege(r, cfg, modul.Mehr) {
		cur := t.Titel == current
		if t.Pfad == aufgabenPath {
			t = mitZahl(t)
		}
		navLink(&b, t, "", cur)
	}
	if u := r.Header.Get("Remote-User"); u != "" {
		b.WriteString("<p class=mehr-wer>Angemeldet als <span class=wer>" + html.EscapeString(u) + "</span></p>")
	}
	navLink(&b, modul.Eintrag{Titel: "Abmelden", Pfad: "/abmelden", Icon: abmeldenIcon}, "", false)
	b.WriteString("</div></nav>")
	return b.String()
}

// haushaltNav is the Haushalt tab's pages as pills, the current marked;
// none for a box with only one of them.
func haushaltNav(r *http.Request, cfg StartConfig, current string) string {
	pills := eintraege(r, cfg, modul.Haushalt)
	if len(pills) < 2 {
		return ""
	}
	var b strings.Builder
	for _, t := range pills {
		mark := ""
		if t.Titel == current {
			mark = " aria-current=page"
		}
		b.WriteString("<a href=" + t.Pfad + mark + ">" + t.Titel + "</a>")
	}
	return `<nav class=unterseiten aria-label=Haushalt>` + b.String() + `</nav>`
}
