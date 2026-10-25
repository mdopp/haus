// Package caldav is Haus's CalDAV and CardDAV client for Radicale (laib
// ADR 0017): appointments as VEVENT, reminders as VTODO with VALARM, in the
// calendars the family's phones sync, and contacts as vCards in their
// address books. It finds the account's collections with PROPFIND, reads
// every one of the kind with a REPORT (calendar-query, addressbook-query),
// and writes with PUT to one collection of each kind only: the configured
// one, else the one named "Haushalt", made when missing. Nothing is kept
// in Haus. Address, account and password come from the environment; no
// request leaves for another host.
package caldav

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/xml"
	"errors"
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
	// AddressBook is the same for contacts (/familie/kontakte/).
	AddressBook string
	Loc         *time.Location // how floating times are read; default time.Local
	HTTP        *http.Client
	Now         func() time.Time

	mu        sync.Mutex
	write     *Collection // the Haushalt calendar, once found
	writeBook *Collection // the Haushalt address book, once found
}

// Collection is one calendar or address book.
type Collection struct {
	URL   string
	Name  string
	comps []string // the components a calendar takes; empty: all
	book  bool     // an address book
	// laib's kalender properties on a mirror (laib#495): its last good run
	// (RFC 3339) and what went wrong since
	synced, problem string
}

// WriteName is the calendar and the address book the Bote writes to when
// none is configured.
const WriteName = "Haushalt"

// where a missing Haushalt collection is made, below the account's home:
// the address book cannot share the calendar's path
const (
	calSlug  = "haushalt"
	bookSlug = "haushalt-kontakte"
)

// ErrForeignHost is an address on a host other than the Radicale one:
// never followed, the account's Basic auth stays home.
var ErrForeignHost = errors.New("Radicale: Adresse auf einem anderen Host")

// ErrStatus is an answer the client did not expect.
type ErrStatus struct {
	Method string
	Code   int
}

func (e *ErrStatus) Error() string {
	if e.Code == http.StatusUnauthorized || e.Code == http.StatusForbidden {
		return fmt.Sprintf("Radicale: Anmeldung abgelehnt (%s, HTTP %d)", e.Method, e.Code)
	}
	if e.Code >= 300 && e.Code < 400 {
		return fmt.Sprintf("Radicale: %s leitet um (HTTP %d); dem folgt Haus nicht", e.Method, e.Code)
	}
	return fmt.Sprintf("Radicale: %s antwortet HTTP %d", e.Method, e.Code)
}

// a redirect is never followed: Basic auth stays with the configured
// address, and a 3xx is an ErrStatus
func noRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

var defaultHTTP = &http.Client{Timeout: 15 * time.Second, CheckRedirect: noRedirect}

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

// send makes one request; the caller closes the body.
func (c *Client) send(ctx context.Context, method, u, depth, body string, hdr map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, u, strings.NewReader(body))
	if err != nil {
		return nil, err
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
	h := defaultHTTP
	if c.HTTP != nil {
		hc := *c.HTTP
		hc.CheckRedirect = noRedirect
		h = &hc
	}
	resp, err := h.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Radicale nicht erreichbar: %w", err)
	}
	return resp, nil
}

func (c *Client) do(ctx context.Context, method, u, depth, body string, hdr map[string]string) (int, []byte, error) {
	resp, err := c.send(ctx, method, u, depth, body, hdr)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	return resp.StatusCode, raw, err
}

// resolve makes an href absolute; one on another host (or scheme) is refused.
func (c *Client) resolve(ref string) (string, error) {
	base, err := url.Parse(strings.TrimRight(c.URL, "/") + "/")
	if err != nil {
		return "", err
	}
	r, err := url.Parse(ref)
	if err != nil {
		return "", err
	}
	u := base.ResolveReference(r)
	if u.Scheme != base.Scheme || u.Host != base.Host {
		return "", ErrForeignHost
	}
	return u.String(), nil
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
					Calendar    *struct{} `xml:"urn:ietf:params:xml:ns:caldav calendar"`
					AddressBook *struct{} `xml:"urn:ietf:params:xml:ns:carddav addressbook"`
				} `xml:"DAV: resourcetype"`
				DisplayName string `xml:"DAV: displayname"`
				Comps       struct {
					Comp []struct {
						Name string `xml:"name,attr"`
					} `xml:"urn:ietf:params:xml:ns:caldav comp"`
				} `xml:"urn:ietf:params:xml:ns:caldav supported-calendar-component-set"`
				CalendarData string `xml:"urn:ietf:params:xml:ns:caldav calendar-data"`
				Abgleich     string `xml:"urn:laib:kalender abgleich"`
				Problem      string `xml:"urn:laib:kalender problem"`
				ETag         string `xml:"DAV: getetag"`
			} `xml:"DAV: prop"`
		} `xml:"DAV: propstat"`
	} `xml:"DAV: response"`
}

const propfindBody = `<?xml version="1.0" encoding="utf-8"?>
<D:propfind xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav" xmlns:L="urn:laib:kalender">
  <D:prop><D:resourcetype/><D:displayname/><C:supported-calendar-component-set/><L:abgleich/><L:problem/></D:prop>
</D:propfind>`

// Calendars lists the account's calendars, sorted by address; one whose
// href names another host is left out.
func (c *Client) Calendars(ctx context.Context) ([]Collection, error) { return c.list(ctx, false) }

// AddressBooks lists the account's address books, the same way.
func (c *Client) AddressBooks(ctx context.Context) ([]Collection, error) { return c.list(ctx, true) }

// list is the account's calendars, or its address books.
func (c *Client) list(ctx context.Context, books bool) ([]Collection, error) {
	home, err := c.home()
	if err != nil {
		return nil, err
	}
	return c.listAt(ctx, home, books)
}

// listAt is the calendars, or the address books, in the home at home.
func (c *Client) listAt(ctx context.Context, home string, books bool) ([]Collection, error) {
	return c.collectionsAt(ctx, home, books, false)
}

// collectionsAt is listAt, or with lists the lists only (haus#119).
func (c *Client) collectionsAt(ctx context.Context, home string, books, lists bool) ([]Collection, error) {
	code, raw, err := c.do(ctx, "PROPFIND", home, "1", propfindBody, nil)
	if err != nil {
		return nil, err
	}
	if code == http.StatusNotFound {
		return nil, nil // a fresh account: Radicale creates the home on first write
	}
	if code != http.StatusMultiStatus {
		return nil, &ErrStatus{"PROPFIND", code}
	}
	var ms multistatus
	if err := xml.Unmarshal(raw, &ms); err != nil {
		return nil, fmt.Errorf("Radicale: PROPFIND unlesbar: %w", err)
	}
	var out []Collection
	for _, r := range ms.Responses {
		isCal, isBook, name, synced, problem := false, false, "", "", ""
		var cs []string
		for _, ps := range r.Propstat {
			synced, problem = cmp.Or(ps.Prop.Abgleich, synced), cmp.Or(ps.Prop.Problem, problem)
			if ps.Prop.ResourceType.Calendar != nil {
				isCal = true
			}
			if ps.Prop.ResourceType.AddressBook != nil {
				isBook = true
			}
			if ps.Prop.DisplayName != "" {
				name = ps.Prop.DisplayName
			}
			for _, cp := range ps.Prop.Comps.Comp {
				cs = append(cs, strings.ToUpper(cp.Name))
			}
		}
		if (books && !isBook) || (!books && !isCal) || (isCal && isList(r.Href) != lists) { // a list is no calendar (haus#118)
			continue
		}
		u, err := c.resolve(r.Href)
		if err != nil {
			continue
		}
		if name == "" {
			name = path.Base(strings.TrimRight(r.Href, "/"))
		}
		out = append(out, Collection{URL: u, Name: name, comps: cs, book: books, synced: synced, problem: problem})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].URL < out[j].URL })
	return out, nil
}

// Takes says whether the calendar holds that component.
func (col Collection) Takes(comp string) bool {
	if len(col.comps) == 0 {
		return true
	}
	for _, c := range col.comps {
		if c == comp {
			return true
		}
	}
	return false
}

// kind is calendars or address books: what is configured, where the found
// write collection is kept, and how a missing one is made.
type kind struct {
	book       bool
	configured string
	cache      **Collection
	method     string // MKCALENDAR, MKCOL
	body       string
	slug       string
}

func (c *Client) calendars() kind {
	return kind{false, c.Calendar, &c.write, "MKCALENDAR", mkcalendarBody, calSlug}
}

func (c *Client) books() kind {
	return kind{true, c.AddressBook, &c.writeBook, "MKCOL", mkcolBookBody, bookSlug}
}

func (c *Client) configured(k kind) (Collection, error) {
	u, err := c.resolve(strings.TrimRight(k.configured, "/") + "/")
	return Collection{URL: u, Name: path.Base(strings.TrimRight(k.configured, "/")), book: k.book}, err
}

// readable is every collection of the kind holding comp ("" for an
// address book): the account's own, and the configured one.
func (c *Client) readable(ctx context.Context, k kind, comp string) ([]Collection, error) {
	cols, err := c.list(ctx, k.book)
	if err != nil {
		return nil, err
	}
	var out []Collection
	seen := map[string]bool{}
	if k.configured != "" {
		col, err := c.configured(k)
		if err != nil {
			return nil, err
		}
		out, seen[col.URL] = append(out, col), true
	}
	if !k.book && comp == "VEVENT" { // the family's calendars from outside, in each person's home (laib#495)
		mirrors, err := c.mirrors(ctx)
		if err != nil {
			return nil, err
		}
		cols = append(cols, mirrors...)
	}
	for _, col := range cols {
		if (comp == "" || col.Takes(comp)) && !seen[col.URL] {
			out, seen[col.URL] = append(out, col), true
		}
	}
	return out, nil
}

// haushalt finds the collection named "Haushalt" (by display name, or by
// path: Haushalt or where Haus makes it), if any.
func haushalt(cols []Collection, slug string) (Collection, bool) {
	for _, col := range cols {
		base := path.Base(strings.TrimRight(col.URL, "/"))
		if strings.EqualFold(col.Name, WriteName) || strings.EqualFold(base, WriteName) || strings.EqualFold(base, slug) {
			return col, true
		}
	}
	return Collection{}, false
}

// target is where a write goes: the configured collection, else the one
// named "Haushalt", made when missing. Never whichever comes first.
func (c *Client) target(ctx context.Context, k kind, comp string) (Collection, error) {
	if k.configured != "" {
		return c.configured(k)
	}
	c.mu.Lock()
	cached := *k.cache
	c.mu.Unlock()
	col, ok := Collection{}, false
	if cached != nil {
		col, ok = *cached, true
	} else {
		cols, err := c.list(ctx, k.book)
		if err != nil {
			return Collection{}, err
		}
		col, ok = haushalt(cols, k.slug)
	}
	if !ok {
		home, err := c.home()
		if err != nil {
			return Collection{}, err
		}
		col = Collection{URL: home + k.slug + "/", Name: WriteName, book: k.book}
		code, _, err := c.do(ctx, k.method, col.URL, "", k.body, nil)
		if err != nil {
			return Collection{}, err
		}
		if code != http.StatusCreated {
			// 405/409: someone made it meanwhile, 403: we may not; either
			// way, look again and take what is there, or fail
			c.forget(k)
			cols, err := c.list(ctx, k.book)
			if err != nil {
				return Collection{}, err
			}
			if col, ok = haushalt(cols, k.slug); !ok {
				return Collection{}, &ErrStatus{k.method, code}
			}
		}
	}
	if comp != "" && !col.Takes(comp) {
		return Collection{}, fmt.Errorf("Kalender: „%s“ nimmt kein %s", col.Name, comp)
	}
	c.mu.Lock()
	*k.cache = &col
	c.mu.Unlock()
	return col, nil
}

// EnsureHaushalt makes the household's calendar and address book when
// they are not there yet (owner 2026-10-24: there at once, so a phone can
// take them before the first entry), as the first write would: the
// configured ones are left as they are.
func (c *Client) EnsureHaushalt(ctx context.Context) error {
	_, cerr := c.target(ctx, c.calendars(), "VEVENT")
	_, berr := c.target(ctx, c.books(), "")
	return errors.Join(cerr, berr)
}

const mkcalendarBody = `<?xml version="1.0" encoding="utf-8"?>
<C:mkcalendar xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">
  <D:set><D:prop>
    <D:displayname>Haushalt</D:displayname>
    <C:supported-calendar-component-set><C:comp name="VEVENT"/><C:comp name="VTODO"/></C:supported-calendar-component-set>
  </D:prop></D:set>
</C:mkcalendar>`

// an address book is an extended MKCOL (RFC 5689) with its resource type
const mkcolBookBody = `<?xml version="1.0" encoding="utf-8"?>
<D:mkcol xmlns:D="DAV:" xmlns:CR="urn:ietf:params:xml:ns:carddav">
  <D:set><D:prop>
    <D:resourcetype><D:collection/><CR:addressbook/></D:resourcetype>
    <D:displayname>Haushalt</D:displayname>
  </D:prop></D:set>
</D:mkcol>`

func (c *Client) forget(k kind) {
	c.mu.Lock()
	*k.cache = nil
	c.mu.Unlock()
}

// object is one calendar object a REPORT returned: where it is, its ETag
// and its text.
type object struct {
	href, etag, data string
}

func (c *Client) objects(ctx context.Context, col Collection, body string) ([]object, error) {
	code, raw, err := c.do(ctx, "REPORT", col.URL, "1", body, nil)
	if err != nil {
		return nil, err
	}
	if code != http.StatusMultiStatus {
		return nil, &ErrStatus{"REPORT", code}
	}
	var ms multistatus
	if err := xml.Unmarshal(raw, &ms); err != nil {
		return nil, fmt.Errorf("Radicale: REPORT unlesbar: %w", err)
	}
	var out []object
	for _, r := range ms.Responses {
		for _, ps := range r.Propstat {
			if ps.Prop.CalendarData != "" {
				out = append(out, object{href: r.Href, etag: ps.Prop.ETag, data: ps.Prop.CalendarData})
			}
		}
	}
	return out, nil
}

func (c *Client) report(ctx context.Context, col Collection, body string) ([]*Component, error) {
	objs, err := c.objects(ctx, col, body)
	if err != nil {
		return nil, err
	}
	var out []*Component
	for _, o := range objs {
		if root, err := Parse(o.data); err == nil {
			out = append(out, root)
		}
	}
	return out, nil
}

// reportAll runs one query against each calendar; a calendar gone since
// the PROPFIND (404) is skipped.
func (c *Client) reportAll(ctx context.Context, cols []Collection, body string) ([]*Component, error) {
	var out []*Component
	for _, col := range cols {
		roots, err := c.report(ctx, col, body)
		var st *ErrStatus
		if errors.As(err, &st) && st.Code == http.StatusNotFound {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, roots...)
	}
	return out, nil
}

// Events lists the appointments overlapping [from, to) in every calendar
// that holds them, recurring ones expanded by the server, sorted by start.
func (c *Client) Events(ctx context.Context, from, to time.Time) ([]Event, error) {
	cols, err := c.readable(ctx, c.calendars(), "VEVENT")
	if err != nil {
		return nil, err
	}
	return c.events(ctx, cols, from, to)
}

func (c *Client) events(ctx context.Context, cols []Collection, from, to time.Time) ([]Event, error) {
	s, e := utc(from), utc(to)
	body := `<?xml version="1.0" encoding="utf-8"?>
<C:calendar-query xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">
  <D:prop><D:getetag/><C:calendar-data><C:expand start="` + s + `" end="` + e + `"/></C:calendar-data></D:prop>
  <C:filter><C:comp-filter name="VCALENDAR"><C:comp-filter name="VEVENT"><C:time-range start="` + s + `" end="` + e + `"/></C:comp-filter></C:comp-filter></C:filter>
</C:calendar-query>`
	var out []Event
	for _, col := range cols {
		roots, err := c.reportAll(ctx, []Collection{col}, body)
		if err != nil {
			return nil, err
		}
		for _, r := range roots {
			for _, ev := range Events(r, c.loc()) {
				// the server filters already; this keeps a lax one honest
				if ev.Start.Before(to) && (ev.End.After(from) || (ev.End.Equal(ev.Start) && !ev.Start.Before(from))) {
					ev.Calendar, ev.CalendarName = col.URL, col.Name
					out = append(out, ev)
				}
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out, nil
}

// OpenTodos lists the reminders nobody has ticked off, from every calendar
// that holds them, the soonest first,
// those without a due date last.
func (c *Client) OpenTodos(ctx context.Context) ([]Todo, error) {
	ts, err := c.todos(ctx, `<C:prop-filter name="COMPLETED"><C:is-not-defined/></C:prop-filter>`)
	var out []Todo
	for _, t := range ts {
		if t.Open() {
			out = append(out, t)
		}
	}
	return out, err
}

// Todos lists every reminder, the ticked-off ones too (with Completed),
// in the same order as OpenTodos.
func (c *Client) Todos(ctx context.Context) ([]Todo, error) { return c.todos(ctx, "") }

func (c *Client) todos(ctx context.Context, filter string) ([]Todo, error) {
	cols, err := c.readable(ctx, c.calendars(), "VTODO")
	if err != nil {
		return nil, err
	}
	body := `<?xml version="1.0" encoding="utf-8"?>
<C:calendar-query xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">
  <D:prop><D:getetag/><C:calendar-data/></D:prop>
  <C:filter><C:comp-filter name="VCALENDAR"><C:comp-filter name="VTODO">` + filter + `</C:comp-filter></C:comp-filter></C:filter>
</C:calendar-query>`
	roots, err := c.reportAll(ctx, cols, body)
	if err != nil {
		return nil, err
	}
	var out []Todo
	for _, r := range roots {
		out = append(out, Todos(r, c.loc())...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].Due, out[j].Due
		if a.IsZero() != b.IsZero() {
			return b.IsZero()
		}
		return a.Before(b)
	})
	return out, nil
}

// What CompleteTodo can answer besides a transport or status error.
var (
	ErrNoTodo    = errors.New("Kalender: keine solche Erinnerung")
	ErrRecurring = errors.New("Kalender: eine wiederkehrende Erinnerung hakt Haus nicht ab, das Telefon schon")
	ErrChanged   = errors.New("Kalender: die Erinnerung hat sich inzwischen geändert")
	ErrCancelled = errors.New("Kalender: die Erinnerung ist abgesagt, nicht offen")
)

// CompleteTodo ticks off the reminder whose UID is exactly uid, in whichever
// calendar holds it. The object is written back as it was plus STATUS,
// COMPLETED, PERCENT-COMPLETE (and a fresh DTSTAMP, LAST-MODIFIED), only if
// its ETag is still the one read (If-Match), so a phone's edit in between is
// never overwritten. A reminder ticked off already is no error; a
// cancelled one is ErrCancelled. The UID matches trimmed, as the app
// trims a row's id. A series
// (RRULE, RDATE or an override) is refused: completing its master would end
// every later occurrence.
func (c *Client) CompleteTodo(ctx context.Context, uid string) error {
	uid = strings.TrimSpace(uid)
	cols, err := c.readable(ctx, c.calendars(), "VTODO")
	if err != nil {
		return err
	}
	var esc strings.Builder
	xml.EscapeText(&esc, []byte(uid))
	body := `<?xml version="1.0" encoding="utf-8"?>
<C:calendar-query xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">
  <D:prop><D:getetag/><C:calendar-data/></D:prop>
  <C:filter><C:comp-filter name="VCALENDAR"><C:comp-filter name="VTODO"><C:prop-filter name="UID"><C:text-match collation="i;octet">` + esc.String() + `</C:text-match></C:prop-filter></C:comp-filter></C:comp-filter></C:filter>
</C:calendar-query>`
	for _, col := range cols {
		objs, err := c.objects(ctx, col, body)
		var st *ErrStatus
		if errors.As(err, &st) && st.Code == http.StatusNotFound {
			continue
		}
		if err != nil {
			return err
		}
		for _, o := range objs {
			root, err := Parse(o.data)
			if err != nil {
				continue
			}
			var mine []*Component
			root.Walk(func(t *Component) {
				if t.Name == "VTODO" && strings.TrimSpace(t.Text("UID")) == uid { // the server matches in part; the UID is whole
					mine = append(mine, t)
				}
			})
			if len(mine) == 0 {
				continue
			}
			if len(mine) > 1 || recurring(mine[0]) {
				return ErrRecurring
			}
			if _, done := mine[0].Get("COMPLETED"); done {
				return nil
			}
			switch strings.ToUpper(mine[0].Text("STATUS")) {
			case "COMPLETED":
				return nil
			case "CANCELLED":
				return ErrCancelled
			}
			if o.etag == "" {
				return errors.New("Kalender: Radicale nennt kein ETag; ohne schreibt Haus nicht zurück")
			}
			u, err := c.resolve(o.href)
			if err != nil {
				return err
			}
			code, _, err := c.do(ctx, http.MethodPut, u, "", completed(o.data, uid, c.now()),
				map[string]string{"Content-Type": "text/calendar; charset=utf-8", "If-Match": o.etag})
			if err != nil {
				return err
			}
			switch code {
			case http.StatusOK, http.StatusCreated, http.StatusNoContent:
				return nil
			case http.StatusPreconditionFailed:
				return ErrChanged
			}
			return &ErrStatus{"PUT", code}
		}
	}
	return ErrNoTodo
}

func recurring(t *Component) bool {
	for _, n := range []string{"RRULE", "RDATE", "RECURRENCE-ID"} {
		if _, ok := t.Get(n); ok {
			return true
		}
	}
	return false
}

// what completed replaces in the reminder itself (not in its alarms)
var completion = map[string]bool{"STATUS": true, "COMPLETED": true, "PERCENT-COMPLETE": true, "DTSTAMP": true, "LAST-MODIFIED": true}

// completed is the object's text with the VTODO uid ticked off at now:
// every other line as it was, the new properties before its first alarm.
func completed(data, uid string, now time.Time) string {
	return patchTodo(data, uid, []string{"STATUS:COMPLETED", "COMPLETED:" + utc(now), "PERCENT-COMPLETE:100", "DTSTAMP:" + utc(now), "LAST-MODIFIED:" + utc(now)}, completion)
}

// patchTodo is the object's text with the VTODO uid's own properties named
// in replace dropped and set written in their place, before its first
// alarm; every other line as it was.
func patchTodo(data, uid string, set []string, replace map[string]bool) string {
	lines := unfold(data)
	var b strings.Builder
	for i := 0; i < len(lines); i++ {
		if !strings.EqualFold(lines[i], "BEGIN:VTODO") {
			fold(&b, lines[i])
			continue
		}
		end := i + 1
		for end < len(lines) && !strings.EqualFold(lines[end], "END:VTODO") {
			end++
		}
		block := lines[i:min(end+1, len(lines))]
		i = end
		if !ownUID(block, uid) {
			for _, l := range block {
				fold(&b, l)
			}
			continue
		}
		added, depth := false, 0
		add := func() {
			if !added {
				for _, l := range set {
					fold(&b, l)
				}
				added = true
			}
		}
		for j, l := range block {
			p, ok := parseLine(l)
			switch {
			case j == 0:
			case j == len(block)-1 && depth == 0:
				add()
			case ok && p.Name == "BEGIN":
				if depth == 0 {
					add()
				}
				depth++
			case ok && p.Name == "END":
				depth--
			case ok && depth == 0 && replace[p.Name]:
				continue
			}
			fold(&b, l)
		}
	}
	return b.String()
}

// ownUID says whether a BEGIN:VTODO … END:VTODO block's own UID is uid.
func ownUID(block []string, uid string) bool {
	depth := 0
	for _, l := range block[1:] {
		p, ok := parseLine(l)
		switch {
		case !ok:
		case p.Name == "BEGIN":
			depth++
		case p.Name == "END":
			depth--
		case depth == 0 && p.Name == "UID":
			return strings.TrimSpace(UnescapeText(p.Value)) == uid
		}
	}
	return false
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
	return c.put(ctx, c.calendars(), "VEVENT", e.UID+".ics", "text/calendar", e.ICS(c.now()))
}

// PutTodo writes a new reminder; it never replaces one.
func (c *Client) PutTodo(ctx context.Context, t Todo) (Collection, error) {
	if t.UID == "" {
		t.UID = NewUID()
	}
	return c.put(ctx, c.calendars(), "VTODO", t.UID+".ics", "text/calendar", t.ICS(c.now()))
}

// ErrExists is an import whose name is taken: the entry is there already.
var ErrExists = errors.New("Radicale: schon da")

// ImportObject writes one calendar object as it came (an import,
// haus#13) into the write calendar, named after its UID; one already
// there is ErrExists, never replaced. comp is the component the calendar
// must take (VEVENT, VTODO).
func (c *Client) ImportObject(ctx context.Context, comp, uid, ics string) error {
	_, err := c.put(ctx, c.calendars(), comp, uid+".ics", "text/calendar", ics)
	return exists(err)
}

// ImportCard writes one card as it came into the write address book,
// named after its UID; one already there is ErrExists, never replaced.
func (c *Client) ImportCard(ctx context.Context, uid, vcf string) error {
	_, err := c.put(ctx, c.books(), "", strings.TrimPrefix(uid, "urn:uuid:")+".vcf", "text/vcard", vcf)
	return exists(err)
}

func exists(err error) error {
	var st *ErrStatus
	if errors.As(err, &st) && st.Code == http.StatusPreconditionFailed {
		return ErrExists
	}
	return err
}

// put writes a new object named name into the kind's write collection.
func (c *Client) put(ctx context.Context, k kind, comp, name, ctype, body string) (Collection, error) {
	col, err := c.target(ctx, k, comp)
	if err != nil {
		return col, err
	}
	name = strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r < 0x20 {
			return '-'
		}
		return r
	}, name)
	code, _, err := c.do(ctx, http.MethodPut, col.URL+url.PathEscape(name), "", body,
		map[string]string{"Content-Type": ctype + "; charset=utf-8", "If-None-Match": "*"})
	if err != nil {
		return col, err
	}
	if code != http.StatusCreated && code != http.StatusNoContent && code != http.StatusOK {
		if code == http.StatusNotFound || code == http.StatusConflict {
			c.forget(k)
		}
		return col, &ErrStatus{"PUT", code}
	}
	return col, nil
}
