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
	WunschVon           ID = "wunsch.von"             // „%s, angefragt von %s seit %s“
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
	// a contact's two fixed links (owner 2026-11-05): the page's buttons, the phone book's labels, what a link says instead
	KontaktDokumente       ID = "kontakt.dokumente"        // the button to Paperless
	KontaktNachrichten     ID = "kontakt.nachrichten"      // the button to the viewer's archive
	KontaktDokumenteKurz   ID = "kontakt.dokumente-kurz"   // the web address's label in the phone book
	KontaktNachrichtenKurz ID = "kontakt.nachrichten-kurz" // the same
	KontaktKeinPaperless   ID = "kontakt.kein-paperless"   // the box has no Paperless (any more)
	KontaktKeinArchiv      ID = "kontakt.kein-archiv"      // the viewer has no message archive
	KontaktArchivDaheim    ID = "kontakt.archiv-daheim"    // the archive opens at home only, and the viewer is away
	// Immich for the contacts (owner 2026-11-05): the third link, and the Kontakte page's suggestions
	KontaktFotos            ID = "kontakt.fotos"              // the button to the Immich person
	KontaktFotosKurz        ID = "kontakt.fotos-kurz"         // the web address's label in the phone book
	KontaktKeinImmich       ID = "kontakt.kein-immich"        // the box has no Immich (any more)
	KontakteImmich          ID = "kontakte.immich"            // the suggestions' title
	KontakteImmichHinweis   ID = "kontakte.immich.hinweis"    // their quiet line
	KontakteAnlegen         ID = "kontakte.anlegen"           // a suggestion's button
	KontakteAnlegend        ID = "kontakte.anlegen.laeuft"    // pressed
	KontakteAusblenden      ID = "kontakte.ausblenden"        // a suggestion's other button
	KontakteAusblendend     ID = "kontakte.ausblenden.laeuft" // pressed
	KontakteAngelegt        ID = "kontakte.angelegt"          // „%s“ was made
	KontakteAusgeblendet    ID = "kontakte.ausgeblendet"      // hidden for good
	KontakteAnlegenNicht    ID = "kontakte.anlegen.nicht"     // Radicale did not take it
	KontakteAusblendenNicht ID = "kontakte.ausblenden.nicht"  // not saved
	KontakteVorschlagWeg    ID = "kontakte.vorschlag.weg"     // a suggestion Homes does not know (any more)
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
	SpurenHinweis:       "Das kann Homes, auf dieser Box aber noch nicht. Fordere es an, dann sieht es der Admin.",
	SpurenStandNicht:    "Ob es schon jemand angefordert hat, lässt sich gerade nicht lesen.",
	AnfordernNicht:      "Die Anfrage ließ sich gerade nicht aufschreiben. Versuch es später noch einmal.",
	AnfordernStumm:      "Die Box hat nicht rechtzeitig geantwortet. Schau gleich nach, ob die Anfrage angekommen ist, bevor du es noch einmal versuchst.",
	AnfordernGibtsNicht: "Das gibt es hier schon, oder es gibt es gar nicht.",
	AnfordernKeinLaib:   "Diese Box nimmt noch keine Anfragen an.",
	GrundAus:            "Auf dieser Box ausgeschaltet.",
	GrundBraucht:        "Ausgeschaltet, weil es „%s“ braucht, und das ist hier ausgeschaltet.",
	ModulSeiteTitel:     "Was Homes kann",
	ModulAn:             "an",
	ModulDienst:         "Dienst",
	ModulStand:          "Stand",
	ModulWunsch:         "Anfrage",
	WunschVon:           "%s, angefragt von %s seit %s",
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

	"schritt.anmelden":           "Trag als Server-Adresse {adresse} ein und melde dich mit deinem Benutzernamen und Passwort an.",
	"schritt.fotos":              "Trag als Server-Adresse {adresse} ein, tippe auf „Mit Familienkonto anmelden“ und melde dich an.",
	"schritt.fotos-sichern":      "Schalte unter „Sicherung“ ein, dass neue Fotos von selbst auf die Box kommen.",
	"schritt.tresor":             "Wähle „Selbst gehostet“, trag {adresse} ein und melde dich mit „Single Sign-On“ und deinem Familienkonto an.",
	"schritt.tresor-passwort":    "Beim ersten Mal legst du ein Master-Passwort fest. Das kennst nur du, niemand kann es zurücksetzen: Schreib es auf.",
	"schritt.signal":             "Tippe unten auf „Öffnen“ und schreib „Hallo“. {name} antwortet, sobald ein Admin dich einmal freigegeben hat.",
	"schritt.homes":              "Öffne die App, trag {adresse} ein und melde dich an. Die App richtet sich dann selbst ein.",
	"schritt.homes-iphone":       "iPhone: Öffne {adresse} in Safari, tippe auf „Teilen“ und dann auf „Zum Home-Bildschirm“.",
	"musik.dazu":                 "Hinzufügen",
	"musik.handy":                "Auf dem Handy: Lieder oder ein ganzes Album als ZIP teilen und Homes wählen. Die Box sortiert sie selbst ein.",
	"musik.computer":             "Am Computer, zu Hause: in den Ordner „musik-eingang“ auf dem Netzlaufwerk legen.",
	"musik.wird-einsortiert":     "wird einsortiert …",
	"musik.dauert":               "Das dauert länger als sonst; sobald es fertig ist, bekommst du eine Nachricht.",
	"musik.fertig":               "fertig: %s in der Musik",
	"musik.nicht":                "nicht übernommen",
	"musik.platten":              "Platten",
	"musik.platte-erfassen":      "Platte erfassen",
	"musik.wunschliste":          "Wunschliste",
	"musik.discogs-verbunden":    "Discogs: verbunden mit „%s“",
	"musik.discogs-verbinden":    "Mit Discogs verbinden",
	"musik.discogs-bringt":       "dann erfasst du Platten mit einem Foto in deine Sammlung.",
	"kalender.bearbeiten":        "Neue Termine und Änderungen machst du auf dem Handy im Haushalts-Kalender oder über den Boten.",
	"kontakte.bearbeiten":        "Neue Kontakte und Änderungen machst du auf dem Handy in den Haushalts-Kontakten oder über den Boten.",
	"kontakte.suchen":            "Name, Nummer, E-Mail",
	"kontakte.leer":              "Im Adressbuch steht noch niemand.",
	"kontakte.keiner":            "Kein Kontakt passt zu „%s“.",
	"kontakte.adressbuch":        "Adressbuch",
	"kontakt.dokumente":          "Dokumente in Paperless",
	"kontakt.nachrichten":        "Nachrichten im Archiv",
	"kontakt.dokumente-kurz":     "Dokumente",
	"kontakt.nachrichten-kurz":   "Nachrichten",
	"kontakt.kein-paperless":     "Auf dieser Box gibt es kein Paperless.",
	"kontakt.kein-archiv":        "Für dich gibt es auf dieser Box noch kein Archiv für Nachrichten.",
	"kontakt.archiv-daheim":      "Dein Archiv öffnet sich nur zu Hause im WLAN; gerade bist du unterwegs.",
	"kontakt.fotos":              "Fotos in Immich",
	"kontakt.fotos-kurz":         "Fotos",
	"kontakt.kein-immich":        "Auf dieser Box gibt es kein Immich.",
	"kontakte.immich":            "In Immich, aber noch kein Haushalts-Kontakt",
	"kontakte.immich.hinweis":    "Personen, die in Immich einen Namen haben. Homes legt nichts von selbst an.",
	"kontakte.anlegen":           "als Haushalts-Kontakt anlegen",
	"kontakte.anlegen.laeuft":    "Legt an …",
	"kontakte.ausblenden":        "ausblenden",
	"kontakte.ausblenden.laeuft": "Blendet aus …",
	"kontakte.angelegt":          "„%s“ ist jetzt ein Haushalts-Kontakt, mit dem Gesicht aus Immich. Nummern und Adressen ergänzt du auf dem Handy.",
	"kontakte.ausgeblendet":      "Ausgeblendet: Homes schlägt diese Person nicht mehr vor.",
	"kontakte.anlegen.nicht":     "Der Kontakt ließ sich gerade nicht anlegen. Versuch es gleich noch einmal.",
	"kontakte.ausblenden.nicht":  "Das ließ sich gerade nicht speichern. Versuch es gleich noch einmal.",
	"kontakte.vorschlag.weg":     "Diese Person kennt Homes aus Immich nicht mehr. Lade die Seite neu.",

	// „So hängt alles zusammen“ (owner 2026-11-05): the page, its web's nodes ("knoten.<…>", a word and a line under it), its moments ("<…>" and "<…>.so")
	"zusammen.titel":                    "So hängt alles zusammen",
	"zusammen.mehr":                     "Von der Rechnung zum Anruf: was deine Daten miteinander können",
	"zusammen.kurz":                     "Kontakte, Dokumente, Nachrichten und Termine kennen einander. Das macht vieles leichter.",
	"zusammen.bild":                     "In der Mitte ein Haushalts-Kontakt, verbunden mit Dokumenten, Nachrichten, Terminen, Bank, Telefonbuch und seinen Fotos; drumherum Zeitleiste, Musik und Geräte.",
	"zusammen.mitte":                    "Haushalts-",
	"zusammen.mitte.unter":              "Kontakt",
	"zusammen.knoten.termine":           "Termine",
	"zusammen.knoten.termine.unter":     "Haushalts-Kalender",
	"zusammen.knoten.bank":              "Bank & IBAN",
	"zusammen.knoten.bank.unter":        "aus der Rechnung",
	"zusammen.knoten.telefonbuch":       "Telefonbuch",
	"zusammen.knoten.telefonbuch.unter": "auf jedem Handy",
	"zusammen.knoten.nachrichten":       "Nachrichten",
	"zusammen.knoten.nachrichten.unter": "dein Archiv",
	"zusammen.knoten.dokumente":         "Dokumente",
	"zusammen.knoten.dokumente.unter":   "Paperless",
	"zusammen.knoten.zeitleiste":        "Zeitleiste",
	"zusammen.knoten.zeitleiste.unter":  "Standortverlauf",
	"zusammen.knoten.musik":             "Musik",
	"zusammen.knoten.musik.unter":       "Wunschliste",
	"zusammen.knoten.fotos":             "Fotos",
	"zusammen.knoten.fotos.unter":       "Immich",
	"zusammen.knoten.geraete":           "Geräte",
	"zusammen.knoten.geraete.unter":     "per Signal",
	"zusammen.geht":                     "Das geht",
	"zusammen.rechnung":                 "Von der Rechnung zum Anruf",
	"zusammen.rechnung.so":              "Eine Rechnung wird gescannt, die Firma landet mit ihrer IBAN in den Haushalts-Kontakten, und du rufst sie aus dem Telefonbuch an.",
	"zusammen.tipp":                     "Ein Tipp zu allen Dokumenten eines Kontakts",
	"zusammen.tipp.so":                  "Beim Kontakt auf „Dokumente“ tippen: alles, was er geschickt hat, in Paperless.",
	"zusammen.gesichter":                "Ein Tipp vom Kontakt zu allen Fotos dieser Person",
	"zusammen.gesichter.so":             "Beim Kontakt auf „Fotos“ tippen: Immich zeigt alle Bilder, auf denen es dieses Gesicht erkannt hat.",
	"zusammen.termin":                   "Termin auf dem Handy verschieben, alle sehen es",
	"zusammen.termin.so":                "Im Haushalts-Kalender geändert, kurz darauf auf allen Handys.",
	"zusammen.zeitachse":                "Google-Zeitachse teilen, die Zeitleiste füllt sich",
	"zusammen.zeitachse.so":             "Den Takeout mit der Homes-App teilen: Jahre voller Orte in deinem Standortverlauf, nur für dich.",
	"zusammen.waschmaschine":            "Waschmaschine fertig, Nachricht in Signal",
	"zusammen.waschmaschine.so":         "Geräte im Haus sagen Bescheid, der Bote schreibt dir.",
	"zusammen.fotos":                    "Fotos vom Handy landen von selbst in Immich",
	"zusammen.fotos.so":                 "Einmal in der Immich-App die Sicherung einschalten; ein Tipp öffnet deine Fotos.",
	"zusammen.fehlt":                    "Etwas fehlt? Hier anfordern",
	"zusammen.fehlt.so":                 "Was diese Box noch nicht kann, steht unten auf Mehr. Eine Anfrage, und der Admin sieht es.",
	// Box → Auslastung (owner 2026-11-06, laib auslastung)
	"auslastung.titel":         "Auslastung",
	"auslastung.kein-laib":     "Laib gibt diese Werte noch nicht her. Nach dem nächsten Laib-Update wartet in der Inbox die Freigabe „Neue Standard-Verben“; danach stehen sie hier.",
	"auslastung.speicher":      "Speicher",
	"auslastung.platte.system": "System",
	"auslastung.platte.data":   "Daten",
	"auslastung.platte.boot":   "Startbereich",
	"auslastung.belegt":        "%s von %s belegt · %.0f %% · %s frei",
	"auslastung.raid":          "Ob der Spiegel der Datenplatten heil ist, steht auf der Übersicht, sobald ihm etwas fehlt.",
	"auslastung.rechner":       "Rechner",
	"auslastung.cpu":           "Prozessor",
	"auslastung.last":          "Last %s · %s · %s, %d Kerne",
	"auslastung.ram":           "Arbeitsspeicher",
	"auslastung.gpu":           "Grafikkarte",
	"auslastung.von":           "%s von %s",
	"auslastung.swap":          "Auslagerung:",
	"auslastung.seit":          "läuft seit %s",
	"auslastung.tage":          "%d Tagen",
	"auslastung.stunden":       "%d Stunden",
	"auslastung.watt":          "%.0f von %.0f Watt",
	"auslastung.haushalt":      "Gerade frei für den Haushalt, Modell %s.",
	"auslastung.gehalten":      "Gerade belegt von %s (Fenster %s).",
	"auslastung.temperaturen":  "Temperaturen",
	"auslastung.temp.cpu":      "Prozessor",
	"auslastung.temp.platte":   "Platte %s",
	"auslastung.fehlt":         "Nicht messbar:",
	"auslastung.gemessen":      "Gemessen um %s, alle 5 Sekunden neu",
	"auslastung.alt":           "gerade keine neuen Werte",
	// Box → Übersicht, „Was braucht mich?“ (owner 2026-11-06)
	"box.ruhig":            "Alles in Ordnung.",
	"box.gesichert":        "Letzte Sicherung %s, an %s",
	"box.keine-sicherung":  "Noch keine gültige Sicherung",
	"box.geaendert":        "Letzte Änderung: %s (%s)",
	"box.faellig":          "Box-Aufgabe fällig",
	"box.faellig.n":        "Box-Aufgaben fällig",
	"box.wartet":           "Entscheidung wartet",
	"box.wartet.n":         "Entscheidungen warten",
	"box.drift":            "%d Dateien auf der Box weichen von Laibs Plan ab",
	"box.diag-stumm":       "Die Befunde von laib diag lassen sich gerade nicht lesen.",
	"box.diag-fehlt":       "Gelbe Befunde (laib diag) zeigt Homes, sobald Laib ihm das Verb diag gibt; bis dahin nur die roten aus laib status.",
	"box.inbox-stumm":      "Laibs Inbox antwortet gerade nicht: ob Entscheidungen warten, ist offen.",
	"operator.alle-gut":    "Alle Skills liefen zuletzt gut.",
	"operator.alle-zeigen": "Alle Skills zeigen",
	// Box → Haushalt: the five rows and their state (owner 2026-11-06)
	"haushalt.karte":         "Was zum Haushalt gehört",
	"haushalt.einladen":      "Jemanden einladen",
	"haushalt.einladung":     "Einladung offen",
	"haushalt.einladung.n":   "Einladungen offen",
	"haushalt.telefon":       "Telefon gekoppelt",
	"haushalt.telefon.n":     "Telefone gekoppelt",
	"haushalt.stumm":         "lässt sich gerade nicht lesen",
	"haushalt.adresse":       "Adresse gesetzt",
	"haushalt.keine-adresse": "Noch keine Adresse",
	"haushalt.module":        "%d von %d Modulen an",
	"haushalt.nutzung":       "Homes zählt seit %s",
	// Dokumente: finden (owner 2026-11-06: „Kategorien und Rechnungsnummern/Versicherungsart+Nr/gekaufte Produkte … suchbar“)
	"dok.finden":                 "Finden",
	"dok.suchen":                 "Suchen",
	"dok.sucht":                  "Sucht …",
	"dok.suchen.feld":            "Name, Nummer, Produkt",
	"dok.versicherungen":         "Versicherungen",
	"dok.garantie":               "Garantie läuft noch",
	"dok.fuer":                   "Für %s",
	"dok.alle":                   "Alle Dokumente",
	"dok.gesucht":                "Gesucht: „%s“",
	"dok.zurueck":                "Dokumente",
	"dok.fehler":                 "Paperless antwortet gerade nicht. Später noch einmal versuchen.",
	"dok.keine":                  "Nichts gefunden für „%s“. Ein Stück der Nummer reicht, oder ein Wort aus dem Titel.",
	"dok.leer":                   "Hier liegt noch nichts.",
	"dok.versicherungen.leer":    "Keine Versicherung gefunden. Hier stehen Versicherungsscheine und Verträge mit dem Bereich „Versicherung“.",
	"dok.garantie.leer":          "Keine Garantie, die noch läuft. Hier steht, was im Feld „Garantie bis“ ein Datum in der Zukunft hat.",
	"dok.mehr":                   "Die ersten %d. Mit einem Wort mehr wird es genauer.",
	"dok.nr.Rechnungsnummer":     "Rechnung %s",
	"dok.nr.Versicherungsnummer": "Vers.-Nr. %s",
	"dok.nr.Vertragsnummer":      "Vertrag %s",
	"dok.nr.Kundennummer":        "Kunde %s",
	"dok.garantie.bis":           "Garantie bis %s",
	"dok.kuendbar":               "kündbar bis %s",
	"dok.termin":                 "Kündigungstermin %s",
}
