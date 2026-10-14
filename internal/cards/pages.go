package cards

import (
	"net/url"
	"strconv"
	"strings"
)

// Where a thing or a tool row leads (haus#28): each has its own page on
// Haus, and the widgets' /napi JSON names it (page) for the app to open in
// the browser. The routes and the rows read the same names here, so they
// cannot drift apart.
const (
	ThingPages    = "/raeume/"
	ReminderPages = "/kalender/erinnerung/"
	ContactPages  = "/kontakte/"
	DocumentPages = "/dokumente/"
	NotePages     = "/notizen/"
)

// Domains are the Home Assistant domains Haus draws as a card: what a
// widget shows, and what a thing's own page may show. A person, a tracker,
// a zone, an automation never.
var Domains = map[string]bool{"sensor": true, "binary_sensor": true, "cover": true, "light": true, "switch": true,
	"climate": true, "media_player": true, "lock": true}

// ThingPage is a Home Assistant entity's own page.
func ThingPage(entityID string) string { return ThingPages + url.PathEscape(entityID) }

// ReminderPage is a reminder's, by its UID.
func ReminderPage(uid string) string { return ReminderPages + url.PathEscape(uid) }

// ContactPage is a contact's, by its UID.
func ContactPage(uid string) string { return ContactPages + url.PathEscape(uid) }

// DocumentPage is a Paperless document's; Haus sends it on to Paperless.
func DocumentPage(id int) string { return DocumentPages + strconv.Itoa(id) }

// NotePage is a note's: its file name, each part escaped, the folders kept.
func NotePage(name string) string {
	parts := strings.Split(name, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return NotePages + strings.Join(parts, "/")
}
