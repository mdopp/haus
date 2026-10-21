package caldav

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"
)

// The calendars Haushalt/Kalender lists and makes (haus#66, laib#495). They
// all live in Radicale, named by where they come from:
//
//	haus/haushalt            the household's, Haus's account's
//	haus/familie-<name>      a calendar "für die Familie", made by Haus
//	haus/abo-<name>          a public calendar the family subscribed to
//	<person>/privat-<name>   a calendar "nur für mich", made by Haus
//	<person>/google-<name>   a Google calendar laib's kalender mirrors, read only
//	<person>/abo-<name>      a public calendar the person subscribed to, read only
//
// Laib's kalender template writes the mirrors as its own account; Radicale's
// rights let Haus's account list each person's home and read those, and make
// privat-* there, nothing else of theirs.

// Calendar is one row of the Kalender page's list.
type Calendar struct {
	Path    string    // /lea/google-familie/
	URL     string    // where Haus reads it
	Name    string    // its display name
	Kind    string    // haushalt, familie, privat, google, abo; "" for any other of Haus's account's
	Family  bool      // in Haus's account's home: everyone's
	Synced  time.Time // a mirror's last good run (laib's kalender); zero: not yet, or no mirror
	Problem string    // what went wrong since, in laib's words
}

// ReadOnly is a mirror: what the family writes there, the next run undoes.
func (k Calendar) ReadOnly() bool { return k.Kind == "google" || k.Kind == "abo" }

// kindOf is a collection's kind by its name.
func kindOf(name string) string {
	switch {
	case name == calSlug:
		return "haushalt"
	case strings.HasPrefix(name, "familie-"):
		return "familie"
	case strings.HasPrefix(name, "privat-"):
		return "privat"
	case strings.HasPrefix(name, "google-") && name != "google-kontakte":
		return "google"
	case strings.HasPrefix(name, "abo-"):
		return "abo"
	}
	return ""
}

// KindOf is the kind of the calendar at u (a URL or a path), as Calendar.Kind.
func KindOf(u string) string { return kindOf(path.Base(strings.TrimRight(u, "/"))) }

// isMirror is a calendar laib's kalender writes.
func isMirror(u string) bool {
	k := kindOf(path.Base(strings.TrimRight(u, "/")))
	return k == "google" || k == "abo"
}

// principals are the homes below the server Haus's account may list, its
// own left out. Rights that do not let it list the root (a Radicale before
// laib#495) are none, not an error.
func (c *Client) principals(ctx context.Context) ([]string, error) {
	root, err := c.resolve("")
	if err != nil {
		return nil, err
	}
	home, _ := c.home()
	code, raw, err := c.do(ctx, "PROPFIND", root, "1", `<?xml version="1.0" encoding="utf-8"?><D:propfind xmlns:D="DAV:"><D:prop><D:resourcetype/></D:prop></D:propfind>`, nil)
	if err != nil {
		return nil, err
	}
	if code == http.StatusForbidden || code == http.StatusNotFound {
		return nil, nil
	}
	if code != http.StatusMultiStatus {
		return nil, &ErrStatus{"PROPFIND", code}
	}
	var ms multistatus
	if err := xml.Unmarshal(raw, &ms); err != nil {
		return nil, fmt.Errorf("Radicale: PROPFIND unlesbar: %w", err)
	}
	base, _ := url.Parse(root)
	var out []string
	for _, r := range ms.Responses {
		u, err := c.resolve(r.Href)
		if err != nil || u == home {
			continue
		}
		p, _ := url.Parse(u)
		rest := strings.Trim(strings.TrimPrefix(p.Path, base.Path), "/")
		if rest != "" && !strings.Contains(rest, "/") {
			out = append(out, u)
		}
	}
	sort.Strings(out)
	return out, nil
}

// mirrors are the calendars from outside in each person's home (laib#495).
func (c *Client) mirrors(ctx context.Context) ([]Collection, error) {
	homes, err := c.principals(ctx)
	if err != nil {
		return nil, err
	}
	var out []Collection
	for _, h := range homes {
		cols, err := c.listAt(ctx, h, false)
		if err != nil {
			return nil, err
		}
		for _, col := range cols {
			if isMirror(col.URL) {
				out = append(out, col)
			}
		}
	}
	return out, nil
}

// personHome is a person's home; "" for Haus's own account.
func (c *Client) personHome(person string) (string, error) {
	if person == "" || person == c.User || strings.ContainsAny(person, "/?#") {
		return "", errors.New("Kalender: keine Person")
	}
	return c.resolve(url.PathEscape(person) + "/")
}

// PersonCalendars is what Kalender lists for one person: the family's in
// Haus's account's home and the person's own Haus may see (privat-*, the
// mirrors), the family's first.
func (c *Client) PersonCalendars(ctx context.Context, person string) ([]Calendar, error) {
	fam, err := c.list(ctx, false)
	if err != nil {
		return nil, err
	}
	var own []Collection
	if h, err := c.personHome(person); err == nil {
		if own, err = c.listAt(ctx, h, false); err != nil {
			return nil, err
		}
	}
	var out []Calendar
	for i, cols := range [][]Collection{fam, own} {
		for _, col := range cols {
			u, _ := url.Parse(col.URL)
			k := Calendar{Path: u.Path, URL: col.URL, Name: col.Name, Kind: kindOf(path.Base(strings.TrimRight(u.Path, "/"))), Family: i == 0, Problem: col.problem}
			if i == 1 && k.Kind != "privat" && !k.ReadOnly() {
				continue
			}
			k.Synced, _ = time.Parse(time.RFC3339, col.synced)
			out = append(out, k)
		}
	}
	return out, nil
}

// PersonEvents is Events plus the person's calendars "nur für mich".
func (c *Client) PersonEvents(ctx context.Context, person string, from, to time.Time) ([]Event, error) {
	cols, err := c.readable(ctx, c.calendars(), "VEVENT")
	if err != nil {
		return nil, err
	}
	if h, err := c.personHome(person); err == nil {
		own, err := c.listAt(ctx, h, false)
		if err != nil {
			return nil, err
		}
		for _, col := range own {
			if kindOf(path.Base(strings.TrimRight(col.URL, "/"))) == "privat" && col.Takes("VEVENT") {
				cols = append(cols, col)
			}
		}
	}
	return c.events(ctx, cols, from, to)
}

// ErrCalendarExists is a calendar of that name that is there already.
var ErrCalendarExists = errors.New("Kalender: den gibt es schon")

// MakeCalendar makes a calendar for the family (haus/familie-<slug>) or
// for one person (<person>/privat-<slug>) and returns its path.
func (c *Client) MakeCalendar(ctx context.Context, person, slug, name string, family bool) (string, error) {
	home, err := c.home()
	where := home + "familie-" + slug + "/"
	if !family {
		home, err = c.personHome(person)
		where = home + "privat-" + slug + "/"
	}
	if err != nil {
		return "", err
	}
	var esc strings.Builder
	xml.EscapeText(&esc, []byte(name))
	body := strings.Replace(mkcalendarBody, "<D:displayname>Haushalt</D:displayname>", "<D:displayname>"+esc.String()+"</D:displayname>", 1)
	code, _, err := c.do(ctx, "MKCALENDAR", where, "", body, nil)
	switch {
	case err != nil:
		return "", err
	case code == http.StatusMethodNotAllowed:
		return "", ErrCalendarExists
	case code != http.StatusCreated:
		return "", &ErrStatus{"MKCALENDAR", code}
	}
	u, _ := url.Parse(where)
	return u.Path, nil
}

// DeleteCalendar deletes a calendar Haus made (familie-*, privat-*) with
// everything in it; any other is refused here, whatever Radicale allows.
func (c *Client) DeleteCalendar(ctx context.Context, calPath string) error {
	if k := kindOf(path.Base(strings.TrimRight(calPath, "/"))); k != "familie" && k != "privat" {
		return errors.New("Kalender: diesen Kalender löscht Haus nicht")
	}
	u, err := c.resolve(calPath)
	if err != nil {
		return err
	}
	code, _, err := c.do(ctx, http.MethodDelete, u, "", "", nil)
	if err != nil {
		return err
	}
	if code != http.StatusNoContent && code != http.StatusOK && code != http.StatusNotFound {
		return &ErrStatus{"DELETE", code}
	}
	return nil
}
