// Package texte holds Homes' family-visible words by a stable id, one
// table per language (owner 2026-11-04: Homes becomes multilingual — de,
// en, es, fr — after the module work). Only German is filled today, and
// only the words added since then live here: the module titles, the
// traces of what a box lacks and „Hier anfordern“ (ADR 0001 §10). Older
// strings move here in that later project. Standard library only
// (scripts/loc.sh: shared).
package texte

// ID is a word's stable key: the app and other languages refer to it.
type ID string

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
	Geraete         ID = "geraete.titel"
	GeraeteAlle     ID = "geraete.alle"     // the head's link to Räume
	GeraeteHinweis  ID = "geraete.hinweis"  // what a tap and a long press do
	GeraeteSchaltet ID = "geraete.schaltet" // a tile right after a tap
	BoxWeg          ID = "verbindung.weg"   // the page cannot reach the box
	Stand           ID = "verbindung.stand" // „Stand %s“: since when the values are
	TorOeffnen      ID = "tor.frage"        // „%s öffnen?“, asked before a garage opens
	TorJa           ID = "tor.ja"
	TorNein         ID = "tor.nein"
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
	"modul.gesehen":      "Gesehen & Gehört",
	"modul.sicherung":    "Daten sichern",
	"modul.standort":     "Standortverlauf",
	"modul.netzlaufwerk": "Netzlaufwerk",
	"modul.gesundheit":   "Gesundheit",
	"modul.werkstatt":    "Werkstatt",
	"modul.nutzung":      "Was wird genutzt",

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
}

// sprachen are the tables by language; a missing word falls back to German.
var sprachen = map[string]map[ID]string{"de": de}

// T is a word in German.
func T(id ID) string { return In("de", id) }

// In is a word in a language; German when that language lacks it, and
// the id itself when German does too (a test holds every id to German).
func In(lang string, id ID) string {
	if s, ok := sprachen[lang][id]; ok {
		return s
	}
	if s, ok := de[id]; ok {
		return s
	}
	return string(id)
}

// Titel is a module's title.
func Titel(modul string) string { return T(ID("modul." + modul)) }
