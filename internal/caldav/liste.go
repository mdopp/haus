package caldav

import (
	"context"
	"encoding/xml"
	"errors"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

// A list is a calendar of tasks only, below the account's home, that the
// phones show as a task list and Haus as a list (haus#118): the household's
// shopping list at einkauf/, later named ones at liste-*/. A list is no
// calendar: listing calendars leaves it out, and its tasks are no reminders.

// ShoppingList is the household's shopping list's slug.
const ShoppingList = "einkauf"

// isList is a collection's last path part naming a list.
func isList(u string) bool {
	base := path.Base(strings.TrimRight(u, "/"))
	return base == ShoppingList || strings.HasPrefix(base, "liste-")
}

// ListRef names a list (haus#119): whose home it is in — "" for Haus's
// account, the household's; else the person's, for them alone — and its
// slug there.
type ListRef struct {
	Person, Slug string
}

// Einkauf is the household's shopping list.
var Einkauf = ListRef{Slug: ShoppingList}

// ID is the list as a page names it: "einkauf", "liste-baumarkt",
// "anna/liste-geschenke".
func (r ListRef) ID() string {
	if r.Person == "" {
		return r.Slug
	}
	return r.Person + "/" + r.Slug
}

// Personal is a list of one person's.
func (r ListRef) Personal() bool { return r.Person != "" }

// ParseListRef reads an ID back; one that names no list is false.
func ParseListRef(id string) (ListRef, bool) {
	person, slug, ok := strings.Cut(id, "/")
	if !ok {
		person, slug = "", id
	}
	r := ListRef{Person: person, Slug: slug}
	return r, isList(slug) && slug != "" && !strings.ContainsAny(slug, "/\\?#") && !strings.ContainsAny(person, "/\\?#") && (person == "" || slug != ShoppingList)
}

// List is a list and its name.
type List struct {
	Ref  ListRef
	Name string
}

// Lists are the household's lists (Einkauf first, then by name) and, for
// person, their own (by name).
func (c *Client) Lists(ctx context.Context, person string) ([]List, error) {
	home, err := c.home()
	if err != nil {
		return nil, err
	}
	fam, err := c.collectionsAt(ctx, home, false, true)
	if err != nil {
		return nil, err
	}
	var own []Collection
	if h, err := c.personHome(person); err == nil {
		if own, err = c.collectionsAt(ctx, h, false, true); err != nil {
			return nil, err
		}
	}
	read := func(cols []Collection, who string) []List {
		var out []List
		for _, col := range cols {
			out = append(out, List{Ref: ListRef{Person: who, Slug: path.Base(strings.TrimRight(col.URL, "/"))}, Name: col.Name})
		}
		sort.SliceStable(out, func(i, j int) bool {
			if (out[i].Ref.Slug == ShoppingList) != (out[j].Ref.Slug == ShoppingList) {
				return out[i].Ref.Slug == ShoppingList
			}
			return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
		})
		return out
	}
	out := read(fam, "")
	if person != "" {
		out = append(out, read(own, person)...)
	}
	return out, nil
}

// ErrListExists is a list of that name that is there already.
var ErrListExists = errors.New("Liste: die gibt es schon")

// MakeList makes a list for the household (person "") or for one person,
// named name, its slug liste-<slug>.
func (c *Client) MakeList(ctx context.Context, person, slug, name string) (ListRef, error) {
	r := ListRef{Person: person, Slug: "liste-" + slug}
	u, err := c.listURL(r)
	if err != nil {
		return r, err
	}
	code, err := c.mklist(ctx, u, name)
	switch {
	case err != nil:
		return r, err
	case code == http.StatusMethodNotAllowed, code == http.StatusConflict:
		return r, ErrListExists
	case code != http.StatusCreated:
		return r, &ErrStatus{"MKCALENDAR", code}
	}
	return r, nil
}

// DeleteList deletes a list with everything on it; the household's
// shopping list stays.
func (c *Client) DeleteList(ctx context.Context, r ListRef) error {
	if r == Einkauf {
		return errors.New("Liste: die Einkaufsliste des Haushalts bleibt")
	}
	u, err := c.listURL(r)
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

// Item is one line of a list: what it says, where it came from (the
// recipes, in DESCRIPTION), whether it is ticked off.
type Item struct {
	UID, Summary, Herkunft string
	Done                   bool
	Created                time.Time

	href, etag, data string
}

const mklistBody = `<?xml version="1.0" encoding="utf-8"?>
<C:mkcalendar xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">
  <D:set><D:prop>
    <D:displayname>NAME</D:displayname>
    <C:supported-calendar-component-set><C:comp name="VTODO"/></C:supported-calendar-component-set>
  </D:prop></D:set>
</C:mkcalendar>`

func (c *Client) listURL(r ListRef) (string, error) {
	if _, ok := ParseListRef(r.ID()); !ok {
		return "", errors.New("Liste: kein Listenname")
	}
	home, err := c.home()
	if r.Personal() {
		home, err = c.personHome(r.Person)
	}
	if err != nil {
		return "", err
	}
	return home + r.Slug + "/", nil
}

func (c *Client) mklist(ctx context.Context, u, name string) (int, error) {
	var esc strings.Builder
	xml.EscapeText(&esc, []byte(name))
	code, _, err := c.do(ctx, "MKCALENDAR", u, "", strings.Replace(mklistBody, "NAME", esc.String(), 1), nil)
	return code, err
}

// EnsureList makes the list when it is not there yet; one there already
// is no error.
func (c *Client) EnsureList(ctx context.Context, r ListRef, name string) error {
	u, err := c.listURL(r)
	if err != nil {
		return err
	}
	code, err := c.mklist(ctx, u, name)
	switch {
	case err != nil:
		return err
	case code == http.StatusCreated, code == http.StatusMethodNotAllowed, code == http.StatusConflict:
		return nil
	}
	return &ErrStatus{"MKCALENDAR", code}
}

const listQuery = `<?xml version="1.0" encoding="utf-8"?>
<C:calendar-query xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">
  <D:prop><D:getetag/><C:calendar-data/></D:prop>
  <C:filter><C:comp-filter name="VCALENDAR"><C:comp-filter name="VTODO"/></C:comp-filter></C:filter>
</C:calendar-query>`

// ListItems is the list's lines: the open ones first, then the ticked-off
// ones, each in the order they were added. A cancelled task is left out.
func (c *Client) ListItems(ctx context.Context, r ListRef) ([]Item, error) {
	u, err := c.listURL(r)
	if err != nil {
		return nil, err
	}
	objs, err := c.objects(ctx, Collection{URL: u}, listQuery)
	var st *ErrStatus
	if errors.As(err, &st) && st.Code == http.StatusNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Item
	for _, o := range objs {
		root, err := Parse(o.data)
		if err != nil {
			continue
		}
		root.Walk(func(t *Component) {
			if t.Name != "VTODO" || recurring(t) {
				return
			}
			status := strings.ToUpper(t.Text("STATUS"))
			if status == "CANCELLED" {
				return
			}
			_, completed := t.Get("COMPLETED")
			it := Item{UID: strings.TrimSpace(t.Text("UID")), Summary: strings.TrimSpace(t.Text("SUMMARY")), Herkunft: strings.TrimSpace(t.Text("DESCRIPTION")),
				Done: status == "COMPLETED" || completed, href: o.href, etag: o.etag, data: o.data}
			if p, ok := t.Get("CREATED"); ok {
				it.Created, _, _ = When(p, c.loc())
			} else if p, ok := t.Get("DTSTAMP"); ok {
				it.Created, _, _ = When(p, c.loc())
			}
			if it.UID != "" && it.Summary != "" {
				out = append(out, it)
			}
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Done != out[j].Done {
			return !out[i].Done
		}
		if !out[i].Created.Equal(out[j].Created) {
			return out[i].Created.Before(out[j].Created)
		}
		return out[i].UID < out[j].UID
	})
	return out, nil
}

// AddItem writes a new open line to the list.
func (c *Client) AddItem(ctx context.Context, r ListRef, summary, herkunft string) error {
	u, err := c.listURL(r)
	if err != nil {
		return err
	}
	now := c.now()
	// lines of one second (a recipe's) keep their order by their UID
	uid := strconv.FormatInt(time.Now().UnixNano(), 10) + "-" + NewUID()
	var b strings.Builder
	lines := []string{"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:" + prodID, "BEGIN:VTODO", "UID:" + uid, "DTSTAMP:" + utc(now), "CREATED:" + utc(now),
		"SUMMARY:" + EscapeText(summary), "STATUS:NEEDS-ACTION"}
	if herkunft != "" {
		lines = append(lines, "DESCRIPTION:"+EscapeText(herkunft))
	}
	for _, l := range append(lines, "END:VTODO", "END:VCALENDAR") {
		fold(&b, l)
	}
	code, _, err := c.do(ctx, http.MethodPut, u+uid+".ics", "", b.String(),
		map[string]string{"Content-Type": "text/calendar; charset=utf-8", "If-None-Match": "*"})
	if err != nil {
		return err
	}
	if code != http.StatusCreated && code != http.StatusNoContent && code != http.StatusOK {
		return &ErrStatus{"PUT", code}
	}
	return nil
}

// what an edit of a line replaces
var itemProps = map[string]bool{"SUMMARY": true, "DESCRIPTION": true, "STATUS": true, "COMPLETED": true, "PERCENT-COMPLETE": true, "DTSTAMP": true, "LAST-MODIFIED": true}

// UpdateItem writes the line back as it was read with its text, origin
// and tick changed, only if nobody changed it since (If-Match): else
// ErrChanged, and the caller reads the list again.
func (c *Client) UpdateItem(ctx context.Context, it Item) error {
	if it.etag == "" || it.href == "" {
		return errors.New("Liste: Radicale nennt kein ETag; ohne schreibt Haus nicht zurück")
	}
	now := c.now()
	set := []string{"SUMMARY:" + EscapeText(it.Summary)}
	if it.Herkunft != "" {
		set = append(set, "DESCRIPTION:"+EscapeText(it.Herkunft))
	}
	if it.Done {
		set = append(set, "STATUS:COMPLETED", "COMPLETED:"+utc(now), "PERCENT-COMPLETE:100")
	} else {
		set = append(set, "STATUS:NEEDS-ACTION")
	}
	set = append(set, "DTSTAMP:"+utc(now), "LAST-MODIFIED:"+utc(now))
	u, err := c.resolve(it.href)
	if err != nil {
		return err
	}
	code, _, err := c.do(ctx, http.MethodPut, u, "", patchTodo(it.data, it.UID, set, itemProps),
		map[string]string{"Content-Type": "text/calendar; charset=utf-8", "If-Match": it.etag})
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

// DeleteItem removes the line, only as it was read; one gone already is
// no error.
func (c *Client) DeleteItem(ctx context.Context, it Item) error {
	u, err := c.resolve(it.href)
	if err != nil {
		return err
	}
	hdr := map[string]string{}
	if it.etag != "" {
		hdr["If-Match"] = it.etag
	}
	code, _, err := c.do(ctx, http.MethodDelete, u, "", "", hdr)
	if err != nil {
		return err
	}
	switch code {
	case http.StatusOK, http.StatusNoContent, http.StatusNotFound:
		return nil
	case http.StatusPreconditionFailed:
		return ErrChanged
	}
	return &ErrStatus{"DELETE", code}
}
