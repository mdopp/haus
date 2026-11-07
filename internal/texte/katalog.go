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
	KontaktFotos         ID = "kontakt.fotos"          // the button to the Immich person
	KontaktFotosKurz     ID = "kontakt.fotos-kurz"     // the web address's label in the phone book
	KontaktKeinImmich    ID = "kontakt.kein-immich"    // the box has no Immich (any more)
	MeineKontakte        ID = "meine-kontakte"         // the page, and its line under Kontakte
	MeineKontakteKurz    ID = "meine-kontakte.kurz"    // that line's words after the link
	MeineKontakteHinweis ID = "meine-kontakte.hinweis" // the page's quiet line
	MeineKontakteLeer    ID = "meine-kontakte.leer"    // the phone sent none yet
	MeineKontakteWeg     ID = "meine-kontakte.weg"     // a uid not in the book
	MeineKontakteRecht   ID = "meine-kontakte.recht"   // the key lacks „%s“ (one or a list)
	Dieselbe             ID = "dieselbe"               // the page „Dieselbe Person?“ and its link
	DieselbeKurz         ID = "dieselbe.kurz"          // the link's words after it: „%s“ is how many
	DieselbeHinweis      ID = "dieselbe.hinweis"       // the page's quiet line
	DieselbeLeer         ID = "dieselbe.leer"          // no pair to ask about
	DieselbeOhneName     ID = "dieselbe.ohne-name"     // a face without a name
	DieselbeJa           ID = "dieselbe.ja"            // the buttons, and each pressed
	DieselbeJaLaeuft     ID = "dieselbe.ja.laeuft"
	DieselbeNein         ID = "dieselbe.nein"
	DieselbeNeinLaeuft   ID = "dieselbe.nein.laeuft"
	DieselbeSpaeter      ID = "dieselbe.spaeter"
	DieselbeSpaeterLauft ID = "dieselbe.spaeter.laeuft"
	DieselbeZusammen     ID = "dieselbe.zusammen"    // merged: „%s“ is the name kept
	DieselbeGemerkt      ID = "dieselbe.gemerkt"     // „Nein“ kept
	DieselbeHinten       ID = "dieselbe.hinten"      // skipped
	DieselbeWeg          ID = "dieselbe.weg"         // the pair is no longer offered
	DieselbeNichtGemacht ID = "dieselbe.nicht"       // Immich merged neither: one is gone
	DieselbeStumm        ID = "dieselbe.stumm"       // Immich did not answer
	DieselbeSeiten       ID = "dieselbe.seiten"      // „Seite %d von %d“
	DieselbeWeiter       ID = "dieselbe.weiter"      // the next ten
	DieselbeZurueck      ID = "dieselbe.zurueck"     // the ten before
	DieselbeOrte         ID = "dieselbe.orte"        // under a face: „%s“ the places, then the years
	DieselbeMit          ID = "dieselbe.mit"         // under a face: „%s“ the named persons in the same photos
	DieselbeInImmich     ID = "dieselbe.immich"      // a face's link: „%s“ the name, all its photos in Immich
	DieselbeInImmichOhne ID = "dieselbe.immich.ohne" // the same for a face without a name
	DieselbeAufgabe      ID = "dieselbe.aufgabe"     // the key owner's task: „%s“ how many, „3 Personen“
	DieselbeAufgabeWarum ID = "dieselbe.aufgabe.warum"
	// „Ist das …?“: a contact's picture beside the person Immich gave its face
	IstDas                ID = "ist-das"             // the section
	IstDasHinweis         ID = "ist-das.hinweis"     // its quiet line
	IstDasFrage           ID = "ist-das.frage"       // „%s“ is the contact's name
	IstDasKontaktbild     ID = "ist-das.kontaktbild" // under the contact's picture
	IstDasJa              ID = "ist-das.ja"          // „%s“ is the contact's name
	IstDasKeinFoto        ID = "ist-das.kein-foto"   // „%s“ is the contact's name
	IstDasKeinFotoLaeuft  ID = "ist-das.kein-foto.laeuft"
	IstDasNeinGemerkt     ID = "ist-das.nein.gemerkt"      // „Nein“ kept
	IstDasKeinFotoGemerkt ID = "ist-das.kein-foto.gemerkt" // „Kein Foto“ kept
	IstDasStumm           ID = "ist-das.stumm"             // Immich did not answer
	// Kontakt prüfen lassen (owner 2026-11-07): the start, the answers' review, the page without a login
	PruefenTitel          ID = "pruefen"      // the start page, its link and the contact's button
	PruefenKurz           ID = "pruefen.kurz" // the quiet line's words after it
	PruefenHinweis        ID = "pruefen.hinweis"
	PruefenHandy          ID = "pruefen.handy" // the two books
	PruefenHaus           ID = "pruefen.haus"
	PruefenLeer           ID = "pruefen.leer"
	PruefenVorher         ID = "pruefen.vorher"    // „%s“ is the contact's name
	PruefenWas            ID = "pruefen.was"       // „%s“ is the first name
	PruefenErstellen      ID = "pruefen.erstellen" // the button, and pressed
	PruefenErstelltLaeuft ID = "pruefen.erstellen.laeuft"
	PruefenErstellt       ID = "pruefen.erstellt"
	PruefenNachricht      ID = "pruefen.nachricht" // the text field's name
	PruefenText           ID = "pruefen.text"      // the prepared message: the first name, the link
	PruefenTeilen         ID = "pruefen.teilen"    // the share sheet, and copy
	PruefenKopieren       ID = "pruefen.kopieren"
	PruefenKopiert        ID = "pruefen.kopiert"
	PruefenNurJetzt       ID = "pruefen.nur-jetzt"
	PruefenWeg            ID = "pruefen.weg"
	PruefenZurueck        ID = "pruefen.zurueck"
	PruefenNicht          ID = "pruefen.nicht"
	PruefenNurAdmins      ID = "pruefen.nur-admins"
	AntwortenTitel        ID = "antworten"         // the review's page
	AntwortenAufgabe      ID = "antworten.aufgabe" // the task: „%s“ how many, „2 Kontakt-Antworten“
	AntwortenAufgabeWarum ID = "antworten.aufgabe.warum"
	AntwortenLeer         ID = "antworten.leer"
	AntwortenBestaetigt   ID = "antworten.bestaetigt"
	AntwortenZurueck      ID = "antworten.zurueck"
	AntwortWeg            ID = "antwort.weg"
	AntwortVon            ID = "antwort.von" // the date, the book
	BuchHaus              ID = "buch.haus"
	BuchHandy             ID = "buch.handy"
	AntwortNurBestaetigt  ID = "antwort.nur-bestaetigt"
	AntwortNeu            ID = "antwort.neu"
	AntwortEntfernt       ID = "antwort.entfernt"
	AntwortAuswahl        ID = "antwort.auswahl" // the buttons, and pressed
	AntwortAlle           ID = "antwort.alle"
	AntwortLaeuft         ID = "antwort.laeuft"
	AntwortErledigt       ID = "antwort.erledigt"
	AntwortVerwerfen      ID = "antwort.verwerfen"
	AntwortVerwirft       ID = "antwort.verwirft"
	AntwortVerwerfenFrage ID = "antwort.verwerfen.frage"
	AntwortVerworfen      ID = "antwort.verworfen" // what came of it
	AntwortGeschrieben    ID = "antwort.geschrieben"
	AntwortZumHandy       ID = "antwort.zum-handy"
	AntwortUebersprungen  ID = "antwort.uebersprungen"
	AntwortKontaktWeg     ID = "antwort.kontakt-weg"
	AntwortNicht          ID = "antwort.nicht"
	AntwortErledigtGesagt ID = "antwort.erledigt.gesagt"
	StandUebernommen      ID = "stand.uebernommen" // what became of a change
	StandAbgelehnt        ID = "stand.abgelehnt"
	StandUebersprungen    ID = "stand.uebersprungen"
	StandAngenommen       ID = "stand.angenommen"
	ArtName               ID = "art.name" // the fields
	ArtTelefon            ID = "art.telefon"
	ArtMail               ID = "art.mail"
	ArtAdresse            ID = "art.adresse"
	ArtGeburtstag         ID = "art.geburtstag"
	ArtVorname            ID = "art.vorname"
	ArtNachname           ID = "art.nachname"
	ArtFirma              ID = "art.firma"
	ArtPosition           ID = "art.position"
	NummerMobil           ID = "nummer.mobil"
	NummerPrivat          ID = "nummer.privat"
	NummerGeschaeftlich   ID = "nummer.geschaeftlich"
	FrageNummerArt        ID = "frage.nummer-art"
	FrageTag              ID = "frage.tag"
	FrageMonat            ID = "frage.monat"
	FrageJahr             ID = "frage.jahr"
	FrageBeruf            ID = "frage.beruf"
	FrageTitel            ID = "frage" // the page without a login, /k/<token>
	FrageHinweis          ID = "frage.hinweis"
	FrageNummern          ID = "frage.nummern"
	FrageWeitereNummer    ID = "frage.weitere-nummer"
	FrageWeitereMail      ID = "frage.weitere-mail"
	FrageWeitereAdresse   ID = "frage.weitere-adresse"
	FrageStrasse          ID = "frage.strasse"
	FragePLZ              ID = "frage.plz"
	FrageOrt              ID = "frage.ort"
	FrageLand             ID = "frage.land"
	FrageLoeschen         ID = "frage.loeschen"
	FrageSenden           ID = "frage.senden"
	FrageStimmt           ID = "frage.stimmt"
	FrageSendet           ID = "frage.sendet"
	FrageBis              ID = "frage.bis"
	FrageDanke            ID = "frage.danke"
	FrageDankeText        ID = "frage.danke.text"
	FrageUngueltig        ID = "frage.ungueltig" // the one page for every link that is none
	FrageUngueltigText    ID = "frage.ungueltig.text"
	FrageZuViele          ID = "frage.zu-viele"
	FrageZuVieleText      ID = "frage.zu-viele.text"
	FrageZuGross          ID = "frage.zu-gross"
	FrageDatum            ID = "frage.datum"
	FrageNicht            ID = "frage.nicht"
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

	"schritt.anmelden":          "Trag als Server-Adresse {adresse} ein und melde dich mit deinem Benutzernamen und Passwort an.",
	"schritt.fotos":             "Trag als Server-Adresse {adresse} ein, tippe auf „Mit Familienkonto anmelden“ und melde dich an.",
	"schritt.fotos-sichern":     "Schalte unter „Sicherung“ ein, dass neue Fotos von selbst auf die Box kommen.",
	"schritt.tresor":            "Wähle „Selbst gehostet“, trag {adresse} ein und melde dich mit „Single Sign-On“ und deinem Familienkonto an.",
	"schritt.tresor-passwort":   "Beim ersten Mal legst du ein Master-Passwort fest. Das kennst nur du, niemand kann es zurücksetzen: Schreib es auf.",
	"schritt.signal":            "Tippe unten auf „Öffnen“ und schreib „Hallo“. {name} antwortet, sobald ein Admin dich einmal freigegeben hat.",
	"schritt.homes":             "Öffne die App, trag {adresse} ein und melde dich an. Die App richtet sich dann selbst ein.",
	"schritt.homes-iphone":      "iPhone: Öffne {adresse} in Safari, tippe auf „Teilen“ und dann auf „Zum Home-Bildschirm“.",
	"musik.dazu":                "Hinzufügen",
	"musik.handy":               "Auf dem Handy: Lieder oder ein ganzes Album als ZIP teilen und Homes wählen. Die Box sortiert sie selbst ein.",
	"musik.computer":            "Am Computer, zu Hause: in den Ordner „musik-eingang“ auf dem Netzlaufwerk legen.",
	"musik.wird-einsortiert":    "wird einsortiert …",
	"musik.dauert":              "Das dauert länger als sonst; sobald es fertig ist, bekommst du eine Nachricht.",
	"musik.fertig":              "fertig: %s in der Musik",
	"musik.nicht":               "nicht übernommen",
	"musik.platten":             "Platten",
	"musik.platte-erfassen":     "Platte erfassen",
	"musik.wunschliste":         "Wunschliste",
	"musik.discogs-verbunden":   "Discogs: verbunden mit „%s“",
	"musik.discogs-verbinden":   "Mit Discogs verbinden",
	"musik.discogs-bringt":      "dann erfasst du Platten mit einem Foto in deine Sammlung.",
	"kalender.bearbeiten":       "Neue Termine und Änderungen machst du auf dem Handy im Haushalts-Kalender oder über den Boten.",
	"kontakte.bearbeiten":       "Neue Kontakte und Änderungen machst du auf dem Handy in den Haushalts-Kontakten oder über den Boten.",
	"kontakte.suchen":           "Name, Nummer, E-Mail",
	"kontakte.leer":             "Im Adressbuch steht noch niemand.",
	"kontakte.keiner":           "Kein Kontakt passt zu „%s“.",
	"kontakte.adressbuch":       "Adressbuch",
	"kontakt.dokumente":         "Dokumente in Paperless",
	"kontakt.nachrichten":       "Nachrichten im Archiv",
	"kontakt.dokumente-kurz":    "Dokumente",
	"kontakt.nachrichten-kurz":  "Nachrichten",
	"kontakt.kein-paperless":    "Auf dieser Box gibt es kein Paperless.",
	"kontakt.kein-archiv":       "Für dich gibt es auf dieser Box noch kein Archiv für Nachrichten.",
	"kontakt.archiv-daheim":     "Dein Archiv öffnet sich nur zu Hause im WLAN; gerade bist du unterwegs.",
	"kontakt.fotos":             "Fotos in Immich",
	"kontakt.fotos-kurz":        "Fotos",
	"kontakt.kein-immich":       "Auf dieser Box gibt es kein Immich.",
	"meine-kontakte":            "Meine Kontakte",
	"meine-kontakte.kurz":       "deine eigenen Handy-Kontakte, jeweils mit seinen Fotos in Immich.",
	"meine-kontakte.hinweis":    "Deine eigenen Handy-Kontakte; nur du siehst sie. Ein Tipp auf einen Namen öffnet seine Fotos in Immich. Jeder Kontakt hat dort eine Person mit seinem Namen: Gibst du in Immich einem Gesicht diesen Namen, führt Homes beide innerhalb einer Stunde zusammen.",
	"meine-kontakte.leer":       "Dein Handy hat noch keine Kontakte geschickt. In der Homes-App: Kontakte übertragen.",
	"meine-kontakte.weg":        "Diesen Kontakt gibt es in deinen Handy-Kontakten nicht mehr.",
	"meine-kontakte.recht":      "Dem Immich-Schlüssel von Homes fehlt diese Berechtigung: %s. Ohne sie bekommen neue Kontakte keine Person in Immich, doppelte Personen bleiben getrennt, oder die Bilder deiner Kontakte kommen nicht als Vorschlag. In Immich: oben rechts dein Bild → Kontoeinstellungen → API-Schlüssel, beim Schlüssel „Homes“ auf Bearbeiten tippen und sie ankreuzen.",
	"dieselbe":                  "Dieselbe Person?",
	"dieselbe.kurz":             "%s, welche Gesichter in Immich zu einer Person gehören könnten.",
	"dieselbe.hinweis":          "Immich hält diese Gesichter für ähnlich. Sagst du Ja, führt Homes beide in Immich zu einer Person zusammen. Ein Nein merkt sich Homes, dann fragt es nicht wieder. Nur du siehst diese Seite.",
	"dieselbe.leer":             "Gerade gibt es nichts zu fragen. Homes schaut jede Stunde in Immich nach ähnlichen Gesichtern.",
	"dieselbe.ohne-name":        "ohne Namen",
	"dieselbe.ja":               "Ja, dieselbe Person",
	"dieselbe.ja.laeuft":        "Führt zusammen …",
	"dieselbe.nein":             "Nein",
	"dieselbe.nein.laeuft":      "Merkt es sich …",
	"dieselbe.spaeter":          "Überspringen",
	"dieselbe.spaeter.laeuft":   "Schiebt nach hinten …",
	"dieselbe.zusammen":         "Zusammengeführt: Die Fotos gehören jetzt zu %s.",
	"dieselbe.gemerkt":          "Gemerkt: Nach diesen beiden fragt Homes nicht wieder.",
	"dieselbe.hinten":           "Übersprungen: Dieser Vorschlag kommt am Ende wieder.",
	"dieselbe.weg":              "Diesen Vorschlag gibt es nicht mehr. Hier sind die aktuellen.",
	"dieselbe.nicht":            "Immich hat die beiden nicht zusammengeführt, weil es eine davon nicht mehr gibt. Nichts wurde geändert.",
	"dieselbe.stumm":            "Immich hat gerade nicht geantwortet, nichts wurde zusammengeführt. Versuch es gleich noch einmal.",
	"dieselbe.seiten":           "Seite %d von %d",
	"dieselbe.weiter":           "Weitere",
	"dieselbe.zurueck":          "Zurück",
	"dieselbe.orte":             "Orte: %s",
	"dieselbe.mit":              "Auch dabei: %s",
	"dieselbe.immich":           "Alle Fotos von %s in Immich",
	"dieselbe.immich.ohne":      "Alle Fotos dieses Gesichts in Immich",
	"dieselbe.aufgabe":          "%s in Immich prüfen",
	"dieselbe.aufgabe.warum":    "Immich hält Gesichter für dieselbe Person oder fragt, ob ein Kontaktbild passt. Ein Ja führt sie zusammen.",
	"ist-das":                   "Ist das …?",
	"ist-das.hinweis":           "Das Bild aus deinem Handy-Kontakt, daneben die Person, der Immich das Gesicht darauf zuordnet. Erst dein Ja führt sie zusammen.",
	"ist-das.frage":             "Ist das %s?",
	"ist-das.kontaktbild":       "Bild im Kontakt",
	"ist-das.ja":                "Ja, das ist %s",
	"ist-das.kein-foto":         "Kein Foto von %s",
	"ist-das.kein-foto.laeuft":  "Nimmt das Bild heraus …",
	"ist-das.nein.gemerkt":      "Gemerkt: Homes hat das Bild aus Immich genommen und fragt nach dieser Person nicht wieder.",
	"ist-das.kein-foto.gemerkt": "Gemerkt: Homes hat das Bild aus Immich genommen und nimmt es nicht wieder.",
	"ist-das.stumm":             "Immich hat gerade nicht geantwortet, nichts wurde geändert. Versuch es gleich noch einmal.",

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
	"box.diag-fehlt":       "Gelbe Befunde (laib diag) zeigt Homes, sobald Laib das Verb befunde kennt (ein Laib-Update); bis dahin nur die roten aus laib status.",
	"box.diag-nie":         "laib diag ist noch nicht gelaufen: Befunde gibt es noch keine.",
	"box.gemessen":         "Befunde gemessen %s",
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
	"pruefen":                    "Kontakt prüfen lassen",
	"pruefen.kurz":               "ein Link, mit dem jemand seine Angaben selbst prüft",
	"pruefen.hinweis":            "Du schickst einem Kontakt einen Link. Dort sieht er nur seine eigenen Angaben – Name, Nummern, E-Mail, Adresse, Geburtstag – und kann sie bestätigen, korrigieren oder ergänzen. Bevor etwas übernommen wird, siehst du jede Änderung.",
	"pruefen.handy":              "Deine Handy-Kontakte",
	"pruefen.haus":               "Kontakte des Hauses",
	"pruefen.leer":               "Hier gibt es noch keine Kontakte.",
	"pruefen.vorher":             "%s prüfen lassen",
	"pruefen.was":                "Das sieht %s unter dem Link:",
	"pruefen.erstellen":          "Link erstellen",
	"pruefen.erstellen.laeuft":   "Erstellt …",
	"pruefen.erstellt":           "Link erstellt. Er gilt 14 Tage und nur einmal. Schick ihn selbst, etwa per WhatsApp, SMS oder Mail.",
	"pruefen.nachricht":          "Die Nachricht mit dem Link",
	"pruefen.text":               "Hallo %s, ich möchte deine Kontaktdaten in unserem Adressbuch aktuell halten. Magst du kurz prüfen, ob alles stimmt? Der Link gilt 14 Tage und nur einmal: %s",
	"pruefen.teilen":             "Teilen …",
	"pruefen.kopieren":           "Kopieren",
	"pruefen.kopiert":            "Kopiert",
	"pruefen.nur-jetzt":          "Homes zeigt diesen Link nur jetzt. Geht er verloren, erstelle einfach einen neuen – der alte gilt dann nicht mehr.",
	"pruefen.weg":                "Diesen Kontakt gibt es nicht mehr.",
	"pruefen.zurueck":            "Einen anderen Kontakt prüfen lassen",
	"pruefen.nicht":              "Der Link ließ sich nicht erstellen. Versuch es gleich noch einmal; hilft das nicht, sag dem Admin Bescheid.",
	"pruefen.nur-admins":         "Kontakte des Hauses lässt nur ein Admin prüfen. Deine eigenen Handy-Kontakte kannst du prüfen lassen.",
	"antworten":                  "Kontakt-Antworten",
	"antworten.aufgabe":          "%s prüfen",
	"antworten.aufgabe.warum":    "Jemand hat seine Kontaktdaten geprüft. Übernimm, was stimmt.",
	"antworten.leer":             "Keine Antworten offen.",
	"antworten.bestaetigt":       "alles stimmt",
	"antworten.zurueck":          "Zu den Kontakt-Antworten",
	"antwort.weg":                "Diese Antwort gibt es nicht (mehr).",
	"antwort.von":                "Antwort vom %s · %s",
	"buch.haus":                  "Kontakt des Hauses",
	"buch.handy":                 "dein Handy-Kontakt",
	"antwort.nur-bestaetigt":     "%s hat bestätigt: alles stimmt so.",
	"antwort.neu":                "neu",
	"antwort.entfernt":           "entfernt",
	"antwort.auswahl":            "Ausgewählte übernehmen",
	"antwort.alle":               "Alle übernehmen",
	"antwort.laeuft":             "Übernimmt …",
	"antwort.erledigt":           "Erledigt",
	"antwort.verwerfen":          "Verwerfen",
	"antwort.verwirft":           "Verwirft …",
	"antwort.verwerfen.frage":    "Diese Antwort verwerfen? Nichts davon wird übernommen.",
	"antwort.verworfen":          "Verworfen: nichts wurde geändert.",
	"antwort.geschrieben":        "%s ins Adressbuch geschrieben; die Handys bekommen sie beim nächsten Abgleich.",
	"antwort.zum-handy":          "%s gehen an dein Handy: die Homes-App trägt sie beim nächsten Sichern in den Kontakt ein, sofort unter Daten sichern → Jetzt sichern.",
	"antwort.uebersprungen":      "%s nicht geschrieben: das Feld war inzwischen anders. Sieh dir den Kontakt an.",
	"antwort.kontakt-weg":        "Den Kontakt gibt es im Adressbuch nicht mehr; nichts wurde geschrieben.",
	"antwort.nicht":              "Das Adressbuch hat nicht geantwortet; nichts wurde geschrieben. Versuch es gleich noch einmal.",
	"antwort.erledigt.gesagt":    "Erledigt.",
	"stand.uebernommen":          "übernommen",
	"stand.abgelehnt":            "nicht übernommen",
	"stand.uebersprungen":        "nicht geschrieben: inzwischen anders, oder der Kontakt fehlt",
	"stand.angenommen":           "wartet auf dein Handy",
	"art.name":                   "Name",
	"art.telefon":                "Telefon",
	"art.mail":                   "E-Mail",
	"art.adresse":                "Adresse",
	"art.geburtstag":             "Geburtstag",
	"frage":                      "Stimmen deine Kontaktdaten?",
	"frage.hinweis":              "So stehen deine Angaben in unserem Adressbuch. Ändere oder ergänze, was nicht stimmt, und schick sie ab. Du siehst hier nur deine eigenen Angaben.",
	"frage.nummern":              "Telefonnummern",
	"frage.weitere-nummer":       "Weitere Nummer",
	"frage.weitere-mail":         "Weitere E-Mail",
	"frage.weitere-adresse":      "Weitere Adresse",
	"frage.strasse":              "Straße und Hausnummer",
	"frage.plz":                  "PLZ",
	"frage.ort":                  "Ort",
	"frage.land":                 "Land",
	"frage.geburtstag":           "Tag, Monat, Jahr (TT.MM.JJJJ)",
	"frage.loeschen":             "Stimmt etwas nicht mehr, lösch es einfach aus dem Feld.",
	"frage.senden":               "Änderungen senden",
	"frage.stimmt":               "Alles stimmt so",
	"frage.sendet":               "Wird gesendet …",
	"frage.bis":                  "Der Link gilt bis %s und nur einmal.",
	"frage.danke":                "Danke!",
	"frage.danke.text":           "Deine Angaben sind angekommen. Bevor sich im Adressbuch etwas ändert, sieht sie sich jemand an. Du kannst die Seite jetzt schließen.",
	"frage.ungueltig":            "Link ungültig oder abgelaufen",
	"frage.ungueltig.text":       "Frag bei der Person nach, die dir den Link geschickt hat.",
	"frage.zu-viele":             "Zu viele Versuche",
	"frage.zu-viele.text":        "Bitte warte ein paar Minuten und versuch es dann noch einmal.",
	"frage.zu-gross":             "Das waren zu viele Angaben auf einmal. Lass ein paar leere Felder weg und schick es noch einmal.",
	"frage.datum":                "Der Geburtstag ist so kein Tag im Kalender: Tag und Monat als Zahl, das Jahr mit vier Ziffern oder leer.",
	"frage.nicht":                "Das ließ sich gerade nicht speichern. Versuch es in ein paar Minuten noch einmal.",
	"art.vorname":                "Vorname",
	"art.nachname":               "Nachname",
	"art.firma":                  "Firma",
	"art.position":               "Position",
	"nummer.mobil":               "mobil",
	"nummer.privat":              "privat",
	"nummer.geschaeftlich":       "geschäftlich",
	"frage.nummer-art":           "Art",
	"frage.tag":                  "Tag",
	"frage.monat":                "Monat",
	"frage.jahr":                 "Jahr (wenn du magst)",
	"frage.beruf":                "Firma und Position",
}
