package caldav

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Just enough iCalendar (RFC 5545) for appointments and reminders: content
// lines with parameters, folding, TEXT escaping, nested components, the
// three DATE-TIME forms (UTC, TZID, floating), DATE, and a simple DURATION.

// Prop is one content line.
type Prop struct {
	Name   string
	Params map[string]string
	Value  string
}

// Component is BEGIN:<Name> … END:<Name>.
type Component struct {
	Name     string
	Props    []Prop
	Children []*Component
}

// Get returns the component's own first property of that name.
func (c *Component) Get(name string) (Prop, bool) {
	for _, p := range c.Props {
		if p.Name == name {
			return p, true
		}
	}
	return Prop{}, false
}

// Text is a TEXT property's unescaped value, "" when absent.
func (c *Component) Text(name string) string {
	p, _ := c.Get(name)
	return UnescapeText(p.Value)
}

// Walk calls fn for c and every component below it.
func (c *Component) Walk(fn func(*Component)) {
	fn(c)
	for _, ch := range c.Children {
		ch.Walk(fn)
	}
}

// Parse reads an iCalendar stream into a root holding its top-level components.
func Parse(s string) (*Component, error) {
	root := &Component{}
	stack := []*Component{root}
	for _, l := range unfold(s) {
		p, ok := parseLine(l)
		if !ok {
			continue // a broken line is skipped, not fatal: one odd client must not hide the calendar
		}
		top := stack[len(stack)-1]
		switch p.Name {
		case "BEGIN":
			c := &Component{Name: strings.ToUpper(p.Value)}
			top.Children = append(top.Children, c)
			stack = append(stack, c)
		case "END":
			if len(stack) == 1 || top.Name != strings.ToUpper(p.Value) {
				return nil, fmt.Errorf("ical: END:%s without BEGIN", p.Value)
			}
			stack = stack[:len(stack)-1]
		default:
			top.Props = append(top.Props, p)
		}
	}
	if len(stack) != 1 {
		return nil, fmt.Errorf("ical: BEGIN:%s without END", stack[len(stack)-1].Name)
	}
	return root, nil
}

func unfold(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if (strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t")) && len(out) > 0 {
			out[len(out)-1] += l[1:]
			continue
		}
		if strings.TrimSpace(l) != "" {
			out = append(out, strings.TrimRight(l, "\r"))
		}
	}
	return out
}

func parseLine(l string) (Prop, bool) {
	i := strings.IndexAny(l, ";:")
	if i <= 0 {
		return Prop{}, false
	}
	p := Prop{Name: strings.ToUpper(l[:i]), Params: map[string]string{}}
	rest := l[i:]
	for rest[0] == ';' {
		rest = rest[1:]
		eq := strings.IndexAny(rest, ";:=")
		if eq < 0 {
			return Prop{}, false
		}
		if rest[eq] != '=' { // vCard 2.1: TEL;CELL: — a bare parameter is a TYPE
			t := strings.ToUpper(rest[:eq])
			if prev := p.Params["TYPE"]; prev != "" {
				t = prev + "," + t
			}
			p.Params["TYPE"], rest = t, rest[eq:]
			continue
		}
		key := strings.ToUpper(rest[:eq])
		rest = rest[eq+1:]
		var val string
		if strings.HasPrefix(rest, `"`) {
			end := strings.IndexByte(rest[1:], '"')
			if end < 0 {
				return Prop{}, false
			}
			val, rest = rest[1:1+end], rest[2+end:]
		} else {
			j := strings.IndexAny(rest, ";:")
			if j < 0 {
				return Prop{}, false
			}
			val, rest = rest[:j], rest[j:]
		}
		p.Params[key] = val
		if rest == "" {
			return Prop{}, false
		}
	}
	if rest[0] != ':' {
		return Prop{}, false
	}
	p.Value = rest[1:]
	return p, true
}

// EscapeText escapes a TEXT value (RFC 5545 §3.3.11); control characters
// other than the newline are dropped, they are not allowed in TEXT.
func EscapeText(s string) string {
	var b strings.Builder
	s = strings.ReplaceAll(s, "\r\n", "\n")
	for _, r := range s {
		switch {
		case r == '\\':
			b.WriteString(`\\`)
		case r == ';':
			b.WriteString(`\;`)
		case r == ',':
			b.WriteString(`\,`)
		case r == '\n':
			b.WriteString(`\n`)
		case r < 0x20 && r != '\t', r == 0x7f:
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// UnescapeText reverses EscapeText.
func UnescapeText(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 == len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 'n', 'N':
			b.WriteByte('\n')
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// fold writes one content line, folded at 75 octets without splitting a
// UTF-8 sequence, terminated by CRLF.
func fold(b *strings.Builder, line string) {
	limit := 75
	for len(line) > limit {
		cut := limit
		for cut > 0 && !utf8.RuneStart(line[cut]) {
			cut--
		}
		b.WriteString(line[:cut])
		b.WriteString("\r\n ")
		line = line[cut:]
		limit = 74 // the leading space counts
	}
	b.WriteString(line)
	b.WriteString("\r\n")
}

const (
	utcLayout  = "20060102T150405Z"
	dateLayout = "20060102"
)

func utc(t time.Time) string { return t.UTC().Format(utcLayout) }

// When reads a DATE or DATE-TIME property; a TZID the box does not know
// and a floating time are read in loc.
func When(p Prop, loc *time.Location) (t time.Time, allDay bool, err error) {
	v := strings.TrimSpace(p.Value)
	if strings.EqualFold(p.Params["VALUE"], "DATE") || len(v) == len(dateLayout) {
		t, err = time.ParseInLocation(dateLayout, v, loc)
		return t, true, err
	}
	if strings.HasSuffix(v, "Z") {
		t, err = time.Parse(utcLayout, v)
		return t, false, err
	}
	in := loc
	if tz := p.Params["TZID"]; tz != "" {
		if l, lerr := time.LoadLocation(strings.TrimPrefix(tz, "/")); lerr == nil {
			in = l
		}
	}
	t, err = time.ParseInLocation("20060102T150405", v, in)
	return t, false, err
}

var durationRE = regexp.MustCompile(`^([+-])?P(?:(\d+)W)?(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?)?$`)

// Duration reads a DURATION value (P1D, PT1H30M, -PT15M).
func Duration(v string) (time.Duration, bool) {
	m := durationRE.FindStringSubmatch(strings.TrimSpace(v))
	if m == nil {
		return 0, false
	}
	n := func(s string) time.Duration { i, _ := strconv.Atoi(s); return time.Duration(i) }
	d := n(m[2])*7*24*time.Hour + n(m[3])*24*time.Hour + n(m[4])*time.Hour + n(m[5])*time.Minute + n(m[6])*time.Second
	if m[1] == "-" {
		d = -d
	}
	return d, true
}

// Event is one VEVENT as the tools see it.
type Event struct {
	UID      string
	Summary  string
	Location string
	Start    time.Time
	End      time.Time
	AllDay   bool
}

// Todo is one VTODO: a reminder.
type Todo struct {
	UID       string
	Summary   string
	Due       time.Time // zero: no due date
	DueAllDay bool      // due on a day (DUE;VALUE=DATE), not at a time: Due is that day's midnight
	Status    string
}

// Open is a reminder nobody has ticked off.
func (t Todo) Open() bool {
	s := strings.ToUpper(t.Status)
	return s != "COMPLETED" && s != "CANCELLED"
}

const prodID = "-//mdopp//Haus Haushalt//DE"

// ICS renders the event as a calendar object; times in UTC, a whole day as DATE.
func (e Event) ICS(now time.Time) string {
	var b strings.Builder
	for _, l := range []string{"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:" + prodID, "BEGIN:VEVENT", "UID:" + e.UID, "DTSTAMP:" + utc(now)} {
		fold(&b, l)
	}
	if e.AllDay {
		fold(&b, "DTSTART;VALUE=DATE:"+e.Start.Format(dateLayout))
		fold(&b, "DTEND;VALUE=DATE:"+e.End.Format(dateLayout))
	} else {
		fold(&b, "DTSTART:"+utc(e.Start))
		fold(&b, "DTEND:"+utc(e.End))
	}
	fold(&b, "SUMMARY:"+EscapeText(e.Summary))
	if e.Location != "" {
		fold(&b, "LOCATION:"+EscapeText(e.Location))
	}
	fold(&b, "END:VEVENT")
	fold(&b, "END:VCALENDAR")
	return b.String()
}

// ICS renders the reminder: a VTODO due then, with a display alarm at that moment.
func (t Todo) ICS(now time.Time) string {
	var b strings.Builder
	for _, l := range []string{"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:" + prodID, "BEGIN:VTODO", "UID:" + t.UID, "DTSTAMP:" + utc(now),
		"SUMMARY:" + EscapeText(t.Summary), "STATUS:NEEDS-ACTION"} {
		fold(&b, l)
	}
	if !t.Due.IsZero() {
		fold(&b, "DUE:"+utc(t.Due))
		for _, l := range []string{"BEGIN:VALARM", "ACTION:DISPLAY", "DESCRIPTION:" + EscapeText(t.Summary), "TRIGGER;VALUE=DATE-TIME:" + utc(t.Due), "END:VALARM"} {
			fold(&b, l)
		}
	}
	fold(&b, "END:VTODO")
	fold(&b, "END:VCALENDAR")
	return b.String()
}

// Events reads every VEVENT in a parsed stream.
func Events(root *Component, loc *time.Location) []Event {
	var out []Event
	root.Walk(func(c *Component) {
		if c.Name != "VEVENT" {
			return
		}
		ds, ok := c.Get("DTSTART")
		if !ok {
			return
		}
		start, allDay, err := When(ds, loc)
		if err != nil {
			return
		}
		e := Event{UID: c.Text("UID"), Summary: c.Text("SUMMARY"), Location: c.Text("LOCATION"), Start: start, End: start, AllDay: allDay}
		if de, ok := c.Get("DTEND"); ok {
			if end, _, err := When(de, loc); err == nil {
				e.End = end
			}
		} else if du, ok := c.Get("DURATION"); ok {
			if d, ok := Duration(du.Value); ok {
				e.End = start.Add(d)
			}
		} else if allDay {
			e.End = start.AddDate(0, 0, 1)
		}
		out = append(out, e)
	})
	return out
}

// Todos reads every VTODO in a parsed stream.
func Todos(root *Component, loc *time.Location) []Todo {
	var out []Todo
	root.Walk(func(c *Component) {
		if c.Name != "VTODO" {
			return
		}
		t := Todo{UID: c.Text("UID"), Summary: c.Text("SUMMARY"), Status: c.Text("STATUS")}
		if _, done := c.Get("COMPLETED"); done { // ticked off, whatever STATUS still says
			t.Status = "COMPLETED"
		}
		if p, ok := c.Get("DUE"); ok {
			t.Due, t.DueAllDay, _ = When(p, loc)
		}
		out = append(out, t)
	})
	return out
}
