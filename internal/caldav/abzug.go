package caldav

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"strings"
)

// Abzug is a collection's objects as they are (haus#174): what a list or
// calendar held before it was deleted, so a Rückgängig can put it back
// object by object, each under its own name, its text untouched.
type Abzug struct {
	Objekte []AbzugObjekt `json:"objekte"`
}

// AbzugObjekt is one object: its name in the collection and its iCalendar text.
type AbzugObjekt struct {
	Name string `json:"name"`
	ICS  string `json:"ics"`
}

// alles asks for one kind of component; a list holds VTODOs, a calendar VEVENTs and may hold VTODOs
const allesBody = `<?xml version="1.0" encoding="utf-8"?>
<C:calendar-query xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">
  <D:prop><D:getetag/><C:calendar-data/></D:prop>
  <C:filter><C:comp-filter name="VCALENDAR"><C:comp-filter name="KIND"/></C:comp-filter></C:filter>
</C:calendar-query>`

// ListAbzug is a list's objects.
func (c *Client) ListAbzug(ctx context.Context, r ListRef) (Abzug, error) {
	u, err := c.listURL(r)
	if err != nil {
		return Abzug{}, err
	}
	return c.abzug(ctx, u)
}

// CalendarAbzug is a calendar's objects, by its path.
func (c *Client) CalendarAbzug(ctx context.Context, calPath string) (Abzug, error) {
	u, err := c.resolve(calPath)
	if err != nil {
		return Abzug{}, err
	}
	return c.abzug(ctx, u)
}

func (c *Client) abzug(ctx context.Context, u string) (Abzug, error) {
	var a Abzug
	seen := map[string]bool{}
	for _, kind := range []string{"VEVENT", "VTODO"} {
		objs, err := c.objects(ctx, Collection{URL: u}, strings.Replace(allesBody, "KIND", kind, 1))
		if err != nil {
			return Abzug{}, err
		}
		for _, o := range objs {
			if n := path.Base(o.href); !seen[n] {
				seen[n] = true
				a.Objekte = append(a.Objekte, AbzugObjekt{Name: n, ICS: o.data})
			}
		}
	}
	return a, nil
}

// ListZurueck puts a list's objects back into it (made again by the caller).
func (c *Client) ListZurueck(ctx context.Context, r ListRef, a Abzug) error {
	u, err := c.listURL(r)
	if err != nil {
		return err
	}
	return c.zurueck(ctx, u, a)
}

// CalendarZurueck puts a calendar's objects back into it (made again by the caller).
func (c *Client) CalendarZurueck(ctx context.Context, calPath string, a Abzug) error {
	u, err := c.resolve(calPath)
	if err != nil {
		return err
	}
	return c.zurueck(ctx, u, a)
}

func (c *Client) zurueck(ctx context.Context, u string, a Abzug) error {
	for _, o := range a.Objekte {
		name := path.Base(o.Name)
		if name == "" || name == "." || name == "/" || strings.ContainsAny(name, "\\\x00") {
			continue
		}
		code, _, err := c.do(ctx, http.MethodPut, strings.TrimRight(u, "/")+"/"+name, "", o.ICS,
			map[string]string{"Content-Type": "text/calendar; charset=utf-8", "If-None-Match": "*"})
		if err != nil {
			return err
		}
		if code != http.StatusCreated && code != http.StatusNoContent && code != http.StatusOK && code != http.StatusPreconditionFailed {
			return fmt.Errorf("Radicale: %s zurücklegen antwortet HTTP %d", name, code)
		}
	}
	return nil
}
