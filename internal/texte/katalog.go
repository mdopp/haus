package texte

// The catalog: every word's id and its German (owner 2026-11-04: „Texte
// aus Übersetzungen sind kein Code“). Words only, no logic; the other
// languages come as tables beside it. scripts/loc.sh does not count it.

// The module titles are "modul.<name>"; Titel finds them.
const (
	SpurFehlt           ID = "spur.fehlt"        // the box does not run the module's backend
	SpurAus             ID = "spur.aus"          // the owner switched it off
	SpurAngefordert     ID = "spur.angefordert"  // a wish is open
	SpurWartet          ID = "spur.wartet"       // a proposal for it waits in the owner's Inbox
	SpurAbgelehnt       ID = "spur.abgelehnt"    // the proposal was rejected, or the wish closed without it
	SpurEingerichtet    ID = "spur.eingerichtet" // the proposal was approved, the module is not on yet
	Anfordern           ID = "anfordern"         // the trace's button
	Anfordernd          ID = "anfordern.laeuft"  // the pressed button
	SpurenTitel         ID = "spuren.titel"      // Mehr's card of traces
	SpurenHinweis       ID = "spuren.hinweis"    // its quiet line
	SpurenStandNicht    ID = "spuren.stand-nicht"
	AnfordernNicht      ID = "anfordern.nicht"        // Laib refused or failed
	AnfordernStumm      ID = "anfordern.stumm"        // Laib did not answer in time
	AnfordernGibtsNicht ID = "anfordern.gibts-nicht"  // no such module, or it is on
	AnfordernKeinLaib   ID = "anfordern.kein-laib"    // this Laib takes no wish yet
	GrundAus            ID = "grund.aus"              // why a module is off: the owner switched it off
	GrundBraucht        ID = "grund.braucht"          // … because the module it needs is off: „%s“ is its title
	ModulSeiteTitel     ID = "modul.seite.titel"      // /module/<name>, admins
	ModulAn             ID = "modul.an"               // a module that is on
	ModulDienst         ID = "modul.dienst"           // the row naming its backend
	ModulStand          ID = "modul.stand"            // the row saying on or off
	ModulWunsch         ID = "modul.wunsch"           // the row with its wish
	WunschVon           ID = "wunsch.von"             // „%s, gewünscht von %s seit %s“
	AusVorschlagen      ID = "schalten.aus"           // the admin's button: switch it off
	AnVorschlagen       ID = "schalten.an"            // the admin's button: switch it on again
	Vorschlagend        ID = "schalten.laeuft"        // the pressed button
	Vorgeschlagen       ID = "schalten.vorgeschlagen" // the proposal waits in Laib's Inbox
	ZurInbox            ID = "schalten.inbox"         // the link to it
	VorschlagNicht      ID = "schalten.nicht"         // Laib refused or failed
	VorschlagStumm      ID = "schalten.stumm"         // Laib did not answer in time
	VorschlagKeinLaib   ID = "schalten.kein-laib"     // this Laib takes no such proposal yet
	GrundVorschlagAus   ID = "schalten.grund.aus"     // the proposal's reason, the owner's notification: „%s verschwindet …“
	GrundVorschlagAn    ID = "schalten.grund.an"
	NichtSchaltbar      ID = "schalten.nicht-schaltbar"
	ModuleSeite         ID = "box.module" // the Box pill
	// Start's „Deine Geräte“ as the app's tiles (homes-android#245, owner 2026-11-04)
	Geraete             ID = "geraete.titel"
	GeraeteAlle         ID = "geraete.alle"     // the head's link to Räume
	GeraeteHinweis      ID = "geraete.hinweis"  // what a tap and a long press do
	GeraeteSchaltet     ID = "geraete.schaltet" // a tile right after a tap
	BoxWeg              ID = "verbindung.weg"   // the page cannot reach the box
	Stand               ID = "verbindung.stand" // „Stand %s“: since when the values are
	TorOeffnen          ID = "tor.frage"        // „%s öffnen?“, asked before a garage opens
	TorJa               ID = "tor.ja"
	TorNein             ID = "tor.nein"
	StandortKurz        ID = "standort.kurz"        // Standortverlauf once connected (owner 2026-11-04): the one-line intro
	StandortOeffnen     ID = "standort.oeffnen"     // the main button, into Reitti
	StandortZuletzt     ID = "standort.zuletzt"     // „… %s“: when the newest point came
	StandortEinschalten ID = "standort.einschalten" // the step while no point has come
	// each module's setup, a task per person (owner 2026-11-04); the steps
	// are the catalog's, "schritt.<…>" with {adresse} and {name}
	Aufgabe       ID = "einrichten.aufgabe" // „%s auf dem Handy einrichten“
	MitApp        ID = "einrichten.mit-app" // the task's line: „Mit der App „%s“.“
	HomesApp      ID = "einrichten.homes"   // the Homes app, in a task's title
	AppLaden      ID = "einrichten.app"     // the first step: „Lade die App „%s“ aufs Handy.“
	Laden         ID = "einrichten.laden"   // its link to the store
	Oeffnen       ID = "einrichten.oeffnen" // the module's address
	Anleitung     ID = "einrichten.anleitung"
	Eingerichtet  ID = "einrichten.fertig"   // the person's tick, where nothing can tell
	Hakt          ID = "einrichten.hakt"     // the pressed tick
	Weiteres      ID = "einrichten.weiteres" // what is left of the block once it is set up
	HakenNicht    ID = "einrichten.nicht"    // the tick could not be kept
	Offen         ID = "aufgaben.offen"      // the counter's words: „%d offen“
	Hallo         ID = "willkommen.hallo"    // „Hallo %s“
	Benutzername  ID = "willkommen.benutzername"
	Schritte      ID = "willkommen.schritte"
	AllesFertig   ID = "willkommen.alles"
	ZurStartseite ID = "willkommen.weiter"
	GrundDienst   ID = "grund.dienst" // a link module's service is not on the box: „%s“ is its name
	// Musik, managing the music (owner 2026-11-04: „Musik verwalten“, not what is playing)
	MusikDazu             ID = "musik.dazu"              // the section how music gets in
	MusikHandy            ID = "musik.handy"             // from the phone: Teilen → Homes
	MusikComputer         ID = "musik.computer"          // from a computer at home; its word „Netzlaufwerk“ links to the page
	MusikWirdEinsortiert  ID = "musik.wird-einsortiert"  // a file beets has not finished
	MusikDauert           ID = "musik.dauert"            // an upload waits longer than ten minutes
	MusikFertig           ID = "musik.fertig"            // „%s“ what went in
	MusikNicht            ID = "musik.nicht"             // a file beets kept back; its reason after
	MusikPlatten          ID = "musik.platten"           // the records
	MusikPlatteErfassen   ID = "musik.platte-erfassen"   // its button
	MusikWunschliste      ID = "musik.wunschliste"       // the music wish list's link, its open wishes after
	MusikDiscogsVerbunden ID = "musik.discogs-verbunden" // „%s“ the Discogs account
	MusikDiscogsVerbinden ID = "musik.discogs-verbinden" // not connected yet: the quiet way to connect
	MusikDiscogsBringt    ID = "musik.discogs-bringt"    // what connecting brings
	// „Web zeigt, Handy bearbeitet“ (owner 2026-11-05): the calendar and the contacts say where to edit, alike
	KalenderBearbeiten ID = "kalender.bearbeiten"
	KontakteBearbeiten ID = "kontakte.bearbeiten"
	KontakteSuchen     ID = "kontakte.suchen"     // the search field: name, number, e-mail
	KontakteLeer       ID = "kontakte.leer"       // the address book has no card
	KontakteKeiner     ID = "kontakte.keiner"     // „%s“: no card matches the search
	KontakteAdressbuch ID = "kontakte.adressbuch" // the card's title while the list is not grouped by letter
)

var de = map[ID]string{
	"modul.geraete":      "Geräte",
	"modul.energie":      "Energie",
	"modul.sprechen":     "Sprechen",
	"modul.kalender":     "Kalender",
	"modul.kontakte":     "Kontakte",
	"modul.konto":        "Homes-Konto",
	"modul.einkauf":      "Einkauf",
	"modul.notizen":      "Notizen",
	"modul.dokumente":    "Dokumente",
	"modul.musik":        "Musik",
	"modul.podcasts":     "Podcasts",
	"modul.hoerbuecher":  "Hörbücher und Podcasts",
	"modul.gesehen":      "Gesehen & Gehört",
	"modul.sicherung":    "Daten sichern",
	"modul.standort":     "Standortverlauf",
	"modul.netzlaufwerk": "Netzlaufwerk",
	"modul.gesundheit":   "Gesundheit",
	"modul.werkstatt":    "Werkstatt",
	"modul.nutzung":      "Was wird genutzt",
	"modul.fotos":        "Fotos",
	"modul.passwoerter":  "Passwörter",
	"modul.filme":        "Filme und Musik",

	SpurFehlt:           "auf dieser Box nicht eingerichtet",
	SpurAus:             "ausgeschaltet",
	SpurAngefordert:     "angefordert",
	SpurWartet:          "wartet auf Freigabe",
	SpurAbgelehnt:       "abgelehnt",
	SpurEingerichtet:    "wird eingerichtet",
	Anfordern:           "Hier anfordern",
	Anfordernd:          "Wird angefordert …",
	SpurenTitel:         "Gibt es hier noch nicht",
	SpurenHinweis:       "Das kann Homes, auf dieser Box aber noch nicht. Wünsch es dir, dann sieht es der Admin.",
	SpurenStandNicht:    "Ob sich schon jemand etwas davon gewünscht hat, lässt sich gerade nicht lesen.",
	AnfordernNicht:      "Der Wunsch ließ sich gerade nicht aufschreiben. Versuch es später noch einmal.",
	AnfordernStumm:      "Die Box hat nicht rechtzeitig geantwortet. Schau gleich nach, ob der Wunsch angekommen ist, bevor du es noch einmal versuchst.",
	AnfordernGibtsNicht: "Das gibt es hier schon, oder es gibt es gar nicht.",
	AnfordernKeinLaib:   "Diese Box nimmt noch keine Wünsche an.",
	GrundAus:            "Auf dieser Box ausgeschaltet.",
	GrundBraucht:        "Ausgeschaltet, weil es „%s“ braucht, und das ist hier ausgeschaltet.",
	ModulSeiteTitel:     "Was Homes kann",
	ModulAn:             "an",
	ModulDienst:         "Dienst",
	ModulStand:          "Stand",
	ModulWunsch:         "Wunsch",
	WunschVon:           "%s, gewünscht von %s seit %s",
	AusVorschlagen:      "Ausschalten vorschlagen",
	AnVorschlagen:       "Wieder einschalten vorschlagen",
	Vorschlagend:        "Wird vorgeschlagen …",
	Vorgeschlagen:       "Vorgeschlagen. Der Vorschlag wartet in Laibs Inbox auf Freigabe.",
	ZurInbox:            "Zur Inbox",
	VorschlagNicht:      "Laib hat den Vorschlag nicht angenommen. Versuch es später noch einmal; sonst weiß laib diag mehr.",
	VorschlagStumm:      "Laib hat nicht rechtzeitig geantwortet. Schau in Laibs Inbox nach, ob der Vorschlag angekommen ist, bevor du es noch einmal versuchst.",
	VorschlagKeinLaib:   "Diese Box nimmt diesen Vorschlag noch nicht an: Laib braucht dafür ein Update.",
	GrundVorschlagAus:   "%s verschwindet aus Homes und der App.",
	GrundVorschlagAn:    "%s kommt in Homes und die App zurück.",
	NichtSchaltbar:      "Dieses Modul lässt sich noch nicht ausschalten.",
	ModuleSeite:         "Module",
	Geraete:             "Deine Geräte",
	GeraeteAlle:         "Alle →",
	GeraeteHinweis:      "Tippen schaltet, lange drücken öffnet das Gerät.",
	GeraeteSchaltet:     "schaltet …",
	BoxWeg:              "Box nicht erreichbar – versucht es wieder …",
	Stand:               "Stand %s",
	TorOeffnen:          "%s öffnen?",
	TorJa:               "Öffnen",
	TorNein:             "Abbrechen",
	StandortKurz:        "Wo du warst – nur für dich, auf eurer Box.",
	StandortOeffnen:     "Zeitleiste öffnen",
	StandortZuletzt:     "Verbunden · zuletzt aufgezeichnet %s",
	StandortEinschalten: "Noch ist kein Punkt angekommen. Schalte in der Homes-App unter „Standortverlauf“ die Aufzeichnung ein.",
	Aufgabe:             "%s auf dem Handy einrichten",
	MitApp:              "Mit der App „%s“.",
	HomesApp:            "Homes-App",
	AppLaden:            "Lade die App „%s“ aufs Handy.",
	Laden:               "App laden",
	Oeffnen:             "Öffnen",
	Anleitung:           "Ausführliche Anleitung",
	Eingerichtet:        "Ist eingerichtet",
	Hakt:                "Hakt ab …",
	Weiteres:            "Auf einem weiteren Gerät einrichten",
	HakenNicht:          "Das ließ sich gerade nicht abhaken. Versuch es gleich noch einmal.",
	Offen:               "%d offen",
	Hallo:               "Hallo %s",
	Benutzername:        "Dein Benutzername:",
	Schritte:            "Deine nächsten Schritte",
	AllesFertig:         "Alles eingerichtet.",
	ZurStartseite:       "Weiter zur Startseite",
	GrundDienst:         "Auf dieser Box läuft %s nicht.",

	"schritt.anmelden":        "Trag als Server-Adresse {adresse} ein und melde dich mit deinem Benutzernamen und Passwort an.",
	"schritt.fotos":           "Trag als Server-Adresse {adresse} ein, tippe auf „Mit Familienkonto anmelden“ und melde dich an.",
	"schritt.fotos-sichern":   "Schalte unter „Sicherung“ ein, dass neue Fotos von selbst auf die Box kommen.",
	"schritt.tresor":          "Wähle „Selbst gehostet“, trag {adresse} ein und melde dich mit „Single Sign-On“ und deinem Familienkonto an.",
	"schritt.tresor-passwort": "Beim ersten Mal legst du ein Master-Passwort fest. Das kennst nur du, niemand kann es zurücksetzen: Schreib es auf.",
	"schritt.signal":          "Tippe unten auf „Öffnen“ und schreib „Hallo“. {name} antwortet, sobald ein Admin dich einmal freigegeben hat.",
	"schritt.homes":           "Öffne die App, trag {adresse} ein und melde dich an. Die App richtet sich dann selbst ein.",
	"schritt.homes-iphone":    "iPhone: Öffne {adresse} in Safari, tippe auf „Teilen“ und dann auf „Zum Home-Bildschirm“.",
	"musik.dazu":              "Hinzufügen",
	"musik.handy":             "Auf dem Handy: Lieder oder ein ganzes Album als ZIP teilen und Homes wählen. Die Box sortiert sie selbst ein.",
	"musik.computer":          "Am Computer, zu Hause: in den Ordner „musik-eingang“ auf dem Netzlaufwerk legen.",
	"musik.wird-einsortiert":  "wird einsortiert …",
	"musik.dauert":            "Das dauert länger als sonst; sobald es fertig ist, bekommst du eine Nachricht.",
	"musik.fertig":            "fertig: %s in der Musik",
	"musik.nicht":             "nicht übernommen",
	"musik.platten":           "Platten",
	"musik.platte-erfassen":   "Platte erfassen",
	"musik.wunschliste":       "Wunschliste",
	"musik.discogs-verbunden": "Discogs: verbunden mit „%s“",
	"musik.discogs-verbinden": "Mit Discogs verbinden",
	"musik.discogs-bringt":    "dann erfasst du Platten mit einem Foto in deine Sammlung.",
	"kalender.bearbeiten":     "Neue Termine und Änderungen machst du auf dem Handy im Homes-Kalender oder über den Boten.",
	"kontakte.bearbeiten":     "Neue Kontakte und Änderungen machst du auf dem Handy im Homes-Adressbuch oder über den Boten.",
	"kontakte.suchen":         "Name, Nummer, E-Mail",
	"kontakte.leer":           "Im Adressbuch steht noch niemand.",
	"kontakte.keiner":         "Kein Kontakt passt zu „%s“.",
	"kontakte.adressbuch":     "Adressbuch",
}
