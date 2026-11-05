package caldav

import (
	"context"
	"errors"
	"maps"
	"net/url"
	"path"
	"slices"
	"strings"
	"time"
)

// The phones write back (owner 2026-11-05: the Homes account is writable):
// one appointment of a shared calendar or one card of the shared address
// books, found by its UID, written only over the version the phone last
// saw (If-Match) or, new, only where none is (If-None-Match). The phone
// sends what it shows (phoneProps); everything else of the stored object
// stays as it was: another app's attendees, Homes' own X-HAUS-*, a
// series' changed instances, a card's IBANs, and its photo unless the
// phone sends one (sentOnly).

// Stored is one shared object as Radicale has it; Data "" is none (yet)
// at that place. It is what the Papierkorb keeps of a deleted one.
type Stored struct {
	Collection string `json:"sammlung"` // its collection's path
	Name       string `json:"name"`     // its file there
	ETag       string `json:"etag"`
	Data       string `json:"text"`
	comp       string // VEVENT or VCARD
}

// ErrNotShared is a calendar the phones do not write into: none of the
// household's, or a mirror (what is written there, the next run undoes).
var ErrNotShared = errors.New("Kalender: kein gemeinsamer Kalender, in den das Handy schreibt")

// ErrNotOne is a body without the object of that UID, or a card that
// shares its file with others (never written back, carddav.go).
var ErrNotOne = errors.New("Kalender: nicht genau dieser eine Eintrag")

// phoneProps is what the phone shows and so writes, of an appointment
// (its alarms with it) and of a card; with the stamp each write renews.
var phoneProps = map[string]map[string]bool{
	"VEVENT": set("SUMMARY LOCATION DESCRIPTION DTSTART DTEND DURATION RRULE EXDATE VALARM DTSTAMP LAST-MODIFIED"),
	"VCARD":  set("FN N TEL EMAIL ADR ORG TITLE BDAY NOTE URL REV"),
}

// sentOnly is what the phone writes only when it sends it: a card's
// picture the person set there replaces the stored one, and a phone that
// sends none (homes-android does not yet) leaves it as it was. The face
// Homes adds to a card (pages.KontaktKarte) is taken out before this.
var sentOnly = map[string]map[string]bool{"VCARD": set("PHOTO")}

func set(names string) map[string]bool {
	m := map[string]bool{}
	for _, n := range strings.Fields(names) {
		m[n] = true
	}
	return m
}

// FamilyEvent is the appointment uid in the shared calendar id (as the
// feed names it) the phones may write; Data "" when it is not there.
func (c *Client) FamilyEvent(ctx context.Context, id, uid string) (Stored, error) {
	cols, err := c.familyCalendars(ctx)
	if err != nil {
		return Stored{}, err
	}
	for _, col := range cols {
		if feedID(col) != id || isMirror(col.URL) {
			continue
		}
		s := Stored{Collection: urlPath(col.URL), comp: "VEVENT"}
		objs, err := c.objects(ctx, col, uidQuery("VEVENT", uid))
		for _, o := range objs {
			if len(withUID(o.data, "VEVENT", uid)) > 0 {
				s.Name, s.ETag, s.Data = path.Base(o.href), o.etag, o.data
				break
			}
		}
		return s, err
	}
	return Stored{}, ErrNotShared
}

// FamilyCard is the card uid of the shared address books; Data "" when it
// is not there, and then the place a new one goes: the Haushalt's.
func (c *Client) FamilyCard(ctx context.Context, uid string) (Stored, error) {
	cards, _, err := c.Cards(ctx)
	if err != nil {
		return Stored{}, err
	}
	for _, cd := range cards {
		if strings.TrimSpace(cd.UID) != uid {
			continue
		}
		if cd.href == "" {
			return Stored{}, ErrNotOne
		}
		p := urlPath(cd.href)
		return Stored{Collection: path.Dir(p) + "/", Name: path.Base(p), ETag: cd.etag, Data: cd.data, comp: "VCARD"}, nil
	}
	col, err := c.target(ctx, c.books(), "")
	return Stored{Collection: urlPath(col.URL), comp: "VCARD"}, err
}

// WriteShared writes the phone's text sent for uid at at (FamilyEvent,
// FamilyCard): over etag, or new where none is when etag is "". A version
// other than etag is ErrChanged. What is written is what is stored with
// the phone's properties in place of its own (patch); a new one is made
// from nothing in the same way. The answer is what was written, with the
// ETag Radicale named.
func (c *Client) WriteShared(ctx context.Context, at Stored, uid, sent, etag string) (Stored, error) {
	if etag != at.ETag || (etag == "") != (at.Data == "") {
		return Stored{}, ErrChanged
	}
	ext, ctype, base := ".ics", "text/calendar", "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:"+prodID+"\r\nBEGIN:VEVENT\r\nUID:"+EscapeText(uid)+"\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	if at.comp == "VCARD" {
		ext, ctype, base = ".vcf", "text/vcard", "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:"+EscapeText(uid)+"\r\nEND:VCARD\r\n"
	}
	if at.Data == "" {
		at.Name, at.Data = objectName(strings.TrimPrefix(uid, "urn:uuid:")+ext), base
	}
	own, ok := phoneSent(sent, at.comp, uid, c.now())
	if !ok {
		return Stored{}, ErrNotOne
	}
	replace := phoneProps[at.comp]
	if slices.ContainsFunc(own, func(l string) bool { p, ok := parseLine(l); return ok && sentOnly[at.comp][propName(p.Name)] }) {
		replace = maps.Clone(replace)
		maps.Copy(replace, sentOnly[at.comp])
	}
	u, err := c.resolve(at.Collection + url.PathEscape(at.Name))
	if err != nil {
		return Stored{}, err
	}
	at.Data = patch(at.Data, at.comp, uid, own, replace)
	at.ETag, err = c.rewrite(ctx, u, ctype, etag, at.Data)
	return at, err
}

// phoneSent is the lines of the phone's master comp of uid in sent that
// it writes (phoneProps; its components whole), and a fresh stamp.
func phoneSent(sent, comp, uid string, now time.Time) (own []string, ok bool) {
	blocks(unfold(sent), comp, func(ls []string, block bool) {
		if u, override := ownUID(ls); block && !ok && u == uid && !override {
			keep := maps.Clone(phoneProps[comp])
			maps.Copy(keep, sentOnly[comp])
			own, ok = phoneLines(ls[1:max(1, len(ls)-1)], keep), true
		}
	})
	if comp == "VCARD" {
		return append(own, "REV:"+utc(now)), ok
	}
	return append(own, "DTSTAMP:"+utc(now), "LAST-MODIFIED:"+utc(now)), ok
}

// stamps are what the server writes on every write, never the phone.
var stamps = set("DTSTAMP LAST-MODIFIED REV")

// phoneLines is the lines of a block's body named in keep: its own
// properties, and its components whole.
func phoneLines(body []string, keep map[string]bool) []string {
	var out []string
	depth, take := 0, false
	for _, l := range body {
		p, ok := parseLine(l)
		switch {
		case ok && p.Name == "BEGIN":
			if depth == 0 {
				take = keep[strings.ToUpper(p.Value)]
			}
			depth++
		case ok && p.Name == "END":
			depth--
		case depth == 0:
			take = ok && keep[propName(p.Name)] && !stamps[propName(p.Name)]
		}
		if take {
			out = append(out, l)
		}
	}
	return out
}

// DeleteShared removes at, only as the phone last saw it (etag): another
// version is ErrChanged; one gone meanwhile is no error.
func (c *Client) DeleteShared(ctx context.Context, at Stored, etag string) error {
	if at.Data == "" || etag != at.ETag {
		return ErrChanged
	}
	u, err := c.resolve(at.Collection + url.PathEscape(at.Name))
	if err != nil {
		return err
	}
	return c.DeleteCard(ctx, u, etag)
}

// RestoreShared puts a deleted object back where it was (the Papierkorb's
// Rückgängig); one there again meanwhile stays as it is.
func (c *Client) RestoreShared(ctx context.Context, s Stored) error {
	u, err := c.resolve(s.Collection)
	if err != nil {
		return err
	}
	return c.zurueck(ctx, u, Abzug{Objekte: []AbzugObjekt{{Name: s.Name, ICS: s.Data}}})
}

// Title is the object as the family knows it: an appointment's summary, a
// card's name.
func (s Stored) Title() string {
	root, err := Parse(s.Data)
	if err != nil {
		return ""
	}
	t := ""
	root.Walk(func(c *Component) {
		if t == "" && c.Name == "VEVENT" {
			t = c.Text("SUMMARY")
		} else if t == "" && c.Name == "VCARD" {
			t = c.Text("FN")
		}
	})
	return t
}

func urlPath(u string) string {
	if p, err := url.Parse(u); err == nil {
		return p.Path
	}
	return u
}
