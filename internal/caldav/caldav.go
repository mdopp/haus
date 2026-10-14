// Package caldav is Haus's CalDAV client for Radicale (laib ADR 0017):
// appointments as VEVENT, reminders as VTODO with VALARM, in the calendar
// the family's phones sync. It finds the account's calendars with PROPFIND,
// reads ranges with a REPORT calendar-query and writes with PUT; nothing is
// kept in Haus. Address, account and password come from the environment.
package caldav

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"sync"
	"time"
)

// Client talks to one Radicale account.
type Client struct {
	URL      string // the server, e.g. http://host.containers.internal:5232
	User     string
	Password string
	// Calendar is a collection path (/familie/kalender/) to use instead of
	// discovering the account's own calendars.
	Calendar string
	Loc      *time.Location // how floating times are read; default time.Local
	HTTP     *http.Client
	Now      func() time.Time

	mu    sync.Mutex
	colls map[string]Collection // by component, VEVENT / VTODO
}

// Collection is one calendar.
type Collection struct {
	URL  string
	Name string
}

// ErrStatus is an answer the client did not expect.
type ErrStatus struct {
	Method string
	Code   int
}

func (e *ErrStatus) Error() string {
	if e.Code == http.StatusUnauthorized || e.Code == http.StatusForbidden {
		return fmt.Sprintf("Kalender: Anmeldung abgelehnt (%s, HTTP %d)", e.Method, e.Code)
	}
	return fmt.Sprintf("Kalender: %s antwortet HTTP %d", e.Method, e.Code)
}

var defaultHTTP = &http.Client{Timeout: 15 * time.Second}

func (c *Client) loc() *time.Location {
	if c.Loc != nil {
		return c.Loc
	}
	return time.Local
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Client) do(ctx context.Context, method, u, depth, body string, hdr map[string]string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, u, strings.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.SetBasicAuth(c.User, c.Password)
	if depth != "" {
		req.Header.Set("Depth", depth)
	}
	if body != "" && hdr["Content-Type"] == "" {
		req.Header.Set("Content-Type", "application/xml; charset=utf-8")
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	h := c.HTTP
	if h == nil {
		h = defaultHTTP
	}
	resp, err := h.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("Kalender nicht erreichbar: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	return resp.StatusCode, raw, err
}

func (c *Client) resolve(ref string) (string, error) {
	base, err := url.Parse(strings.TrimRight(c.URL, "/") + "/")
	if err != nil {
		return "", err
	}
	r, err := url.Parse(ref)
	if err != nil {
		return "", err
	}
	return base.ResolveReference(r).String(), nil
}

// the account's home: /<user>/ below the server's own path
func (c *Client) home() (string, error) {
	return c.resolve(url.PathEscape(c.User) + "/")
}

type multistatus struct {
	XMLName   xml.Name `xml:"DAV: multistatus"`
	Responses []struct {
		Href     string `xml:"DAV: href"`
		Propstat []struct {
			Status string `xml:"DAV: status"`
			Prop   struct {
				ResourceType struct {
					Calendar *struct{} `xml:"urn:ietf:params:xml:ns:caldav calendar"`
				} `xml:"DAV: resourcetype"`
				DisplayName string `xml:"DAV: displayname"`
				Comps       struct {
					Comp []struct {
						Name string `xml:"name,attr"`
					} `xml:"urn:ietf:params:xml:ns:caldav comp"`
				} `xml:"urn:ietf:params:xml:ns:caldav supported-calendar-component-set"`
				CalendarData string `xml:"urn:ietf:params:xml:ns:caldav calendar-data"`
			} `xml:"DAV: prop"`
		} `xml:"DAV: propstat"`
	} `xml:"DAV: response"`
}

const propfindBody = `<?xml version="1.0" encoding="utf-8"?>
<D:propfind xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">
  <D:prop><D:resourcetype/><D:displayname/><C:supported-calendar-component-set/></D:prop>
</D:propfind>`

// Calendars lists the account's calendars and the components each takes
// (empty: all).
func (c *Client) Calendars(ctx context.Context) ([]Collection, map[string][]string, error) {
	home, err := c.home()
	if err != nil {
		return nil, nil, err
	}
	code, raw, err := c.do(ctx, "PROPFIND", home, "1", propfindBody, nil)
	if err != nil {
		return nil, nil, err
	}
	if code == http.StatusNotFound {
		return nil, nil, nil // a fresh account: Radicale creates the home on first write
	}
	if code != http.StatusMultiStatus {
		return nil, nil, &ErrStatus{"PROPFIND", code}
	}
	var ms multistatus
	if err := xml.Unmarshal(raw, &ms); err != nil {
		return nil, nil, fmt.Errorf("Kalender: PROPFIND unlesbar: %w", err)
	}
	var out []Collection
	comps := map[string][]string{}
	for _, r := range ms.Responses {
		isCal, name := false, ""
		var cs []string
		for _, ps := range r.Propstat {
			if ps.Prop.ResourceType.Calendar != nil {
				isCal = true
			}
			if ps.Prop.DisplayName != "" {
				name = ps.Prop.DisplayName
			}
			for _, cp := range ps.Prop.Comps.Comp {
				cs = append(cs, strings.ToUpper(cp.Name))
			}
		}
		if !isCal {
			continue
		}
		u, err := c.resolve(r.Href)
		if err != nil {
			continue
		}
		if name == "" {
			name = path.Base(strings.TrimRight(r.Href, "/"))
		}
		out = append(out, Collection{URL: u, Name: name})
		comps[u] = cs
	}
	sort.Slice(out, func(i, j int) bool { return out[i].URL < out[j].URL })
	return out, comps, nil
}

func takes(cs []string, comp string) bool {
	if len(cs) == 0 {
		return true
	}
	for _, c := range cs {
		if c == comp {
			return true
		}
	}
	return false
}

// collection finds the calendar for a component; with create, an account
// without one gets "Haushalt" (VEVENT and VTODO).
func (c *Client) collection(ctx context.Context, comp string, create bool) (Collection, bool, error) {
	if c.Calendar != "" {
		u, err := c.resolve(strings.TrimRight(c.Calendar, "/") + "/")
		return Collection{URL: u, Name: path.Base(strings.TrimRight(c.Calendar, "/"))}, err == nil, err
	}
	c.mu.Lock()
	if col, ok := c.colls[comp]; ok {
		c.mu.Unlock()
		return col, true, nil
	}
	c.mu.Unlock()
	cols, comps, err := c.Calendars(ctx)
	if err != nil {
		return Collection{}, false, err
	}
	for _, col := range cols {
		if takes(comps[col.URL], comp) {
			c.remember(comp, col)
			return col, true, nil
		}
	}
	if !create {
		return Collection{}, false, nil
	}
	home, err := c.home()
	if err != nil {
		return Collection{}, false, err
	}
	col := Collection{URL: home + "haushalt/", Name: "Haushalt"}
	code, _, err := c.do(ctx, "MKCALENDAR", col.URL, "", mkcalendarBody, nil)
	if err != nil {
		return Collection{}, false, err
	}
	if code != http.StatusCreated && code != http.StatusMethodNotAllowed { // 405: it is there already
		return Collection{}, false, &ErrStatus{"MKCALENDAR", code}
	}
	c.remember(comp, col)
	return col, true, nil
}

const mkcalendarBody = `<?xml version="1.0" encoding="utf-8"?>
<C:mkcalendar xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">
  <D:set><D:prop>
    <D:displayname>Haushalt</D:displayname>
    <C:supported-calendar-component-set><C:comp name="VEVENT"/><C:comp name="VTODO"/></C:supported-calendar-component-set>
  </D:prop></D:set>
</C:mkcalendar>`

func (c *Client) remember(comp string, col Collection) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.colls == nil {
		c.colls = map[string]Collection{}
	}
	c.colls[comp] = col
}

func (c *Client) forget() {
	c.mu.Lock()
	c.colls = nil
	c.mu.Unlock()
}

func (c *Client) report(ctx context.Context, col Collection, body string) ([]*Component, error) {
	code, raw, err := c.do(ctx, "REPORT", col.URL, "1", body, nil)
	if err != nil {
		return nil, err
	}
	if code != http.StatusMultiStatus {
		if code == http.StatusNotFound {
			c.forget()
		}
		return nil, &ErrStatus{"REPORT", code}
	}
	var ms multistatus
	if err := xml.Unmarshal(raw, &ms); err != nil {
		return nil, fmt.Errorf("Kalender: REPORT unlesbar: %w", err)
	}
	var out []*Component
	for _, r := range ms.Responses {
		for _, ps := range r.Propstat {
			if ps.Prop.CalendarData == "" {
				continue
			}
			if root, err := Parse(ps.Prop.CalendarData); err == nil {
				out = append(out, root)
			}
		}
	}
	return out, nil
}

// Events lists the appointments overlapping [from, to), recurring ones
// expanded by the server, sorted by start.
func (c *Client) Events(ctx context.Context, from, to time.Time) ([]Event, Collection, error) {
	col, ok, err := c.collection(ctx, "VEVENT", false)
	if err != nil || !ok {
		return nil, col, err
	}
	s, e := utc(from), utc(to)
	body := `<?xml version="1.0" encoding="utf-8"?>
<C:calendar-query xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">
  <D:prop><D:getetag/><C:calendar-data><C:expand start="` + s + `" end="` + e + `"/></C:calendar-data></D:prop>
  <C:filter><C:comp-filter name="VCALENDAR"><C:comp-filter name="VEVENT"><C:time-range start="` + s + `" end="` + e + `"/></C:comp-filter></C:comp-filter></C:filter>
</C:calendar-query>`
	roots, err := c.report(ctx, col, body)
	if err != nil {
		return nil, col, err
	}
	var out []Event
	for _, r := range roots {
		for _, ev := range Events(r, c.loc()) {
			// the server filters already; this keeps a lax one honest
			if ev.Start.Before(to) && (ev.End.After(from) || (ev.End.Equal(ev.Start) && !ev.Start.Before(from))) {
				out = append(out, ev)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out, col, nil
}

// OpenTodos lists the reminders nobody has ticked off, the soonest first,
// those without a due date last.
func (c *Client) OpenTodos(ctx context.Context) ([]Todo, Collection, error) {
	col, ok, err := c.collection(ctx, "VTODO", false)
	if err != nil || !ok {
		return nil, col, err
	}
	body := `<?xml version="1.0" encoding="utf-8"?>
<C:calendar-query xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">
  <D:prop><D:getetag/><C:calendar-data/></D:prop>
  <C:filter><C:comp-filter name="VCALENDAR"><C:comp-filter name="VTODO"><C:prop-filter name="COMPLETED"><C:is-not-defined/></C:prop-filter></C:comp-filter></C:comp-filter></C:filter>
</C:calendar-query>`
	roots, err := c.report(ctx, col, body)
	if err != nil {
		return nil, col, err
	}
	var out []Todo
	for _, r := range roots {
		for _, t := range Todos(r, c.loc()) {
			if t.Open() {
				out = append(out, t)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].Due, out[j].Due
		if a.IsZero() != b.IsZero() {
			return b.IsZero()
		}
		return a.Before(b)
	})
	return out, col, nil
}

// NewUID is a fresh object id.
func NewUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b) + "@haus"
}

// PutEvent writes a new appointment; it never replaces one.
func (c *Client) PutEvent(ctx context.Context, e Event) (Collection, error) {
	if e.UID == "" {
		e.UID = NewUID()
	}
	return c.put(ctx, "VEVENT", e.UID, e.ICS(c.now()))
}

// PutTodo writes a new reminder; it never replaces one.
func (c *Client) PutTodo(ctx context.Context, t Todo) (Collection, error) {
	if t.UID == "" {
		t.UID = NewUID()
	}
	return c.put(ctx, "VTODO", t.UID, t.ICS(c.now()))
}

func (c *Client) put(ctx context.Context, comp, uid, ics string) (Collection, error) {
	col, _, err := c.collection(ctx, comp, true)
	if err != nil {
		return col, err
	}
	name := strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r < 0x20 {
			return '-'
		}
		return r
	}, uid)
	code, _, err := c.do(ctx, http.MethodPut, col.URL+url.PathEscape(name)+".ics", "", ics,
		map[string]string{"Content-Type": "text/calendar; charset=utf-8", "If-None-Match": "*"})
	if err != nil {
		return col, err
	}
	if code != http.StatusCreated && code != http.StatusNoContent && code != http.StatusOK {
		if code == http.StatusNotFound || code == http.StatusConflict {
			c.forget()
		}
		return col, &ErrStatus{"PUT", code}
	}
	return col, nil
}
