// Package haushalt is the "Haushalt" MCP source (laib ADR 0017 §2): the one
// way the Bote and Claude reach the household's own data — appointments and
// reminders in Radicale, notes as Markdown in the file share. Haus keeps
// none of it: the Bote remembers about you, never for you (§3), so a note
// becomes a file and a reminder a calendar entry, where people see them.
// A tool is listed only when its backend is configured.
package haushalt

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mdopp/haus/internal/caldav"
	"github.com/mdopp/haus/internal/mcpserver"
	"github.com/mdopp/haus/internal/notes"
)

// Config is what the endpoint serves.
type Config struct {
	Token    string         // HAUS_HAUSHALT_TOKEN, the Bote's bearer
	Calendar *caldav.Client // nil: no calendar or reminder tools
	Notes    *notes.Dir     // nil: no note tools
	Loc      *time.Location // the house's time zone
	Version  string
}

// FromEnv reads HAUS_HAUSHALT_TOKEN, HAUS_RADICALE_URL (+ _USER,
// _PASSWORD, optional _CALENDAR), HAUS_NOTES_DIR and TZ. A malformed
// Radicale address refuses to start; a missing backend only leaves its
// tools out.
func FromEnv() (Config, error) {
	cfg := Config{Token: os.Getenv("HAUS_HAUSHALT_TOKEN"), Loc: time.Local}
	if tz := os.Getenv("TZ"); tz != "" {
		if l, err := time.LoadLocation(tz); err == nil {
			cfg.Loc = l
		}
	}
	if cfg.Token == "" {
		log.Printf("haus: HAUS_HAUSHALT_TOKEN is not set: /mcp/haushalt refuses everyone")
	}
	if u := strings.TrimRight(os.Getenv("HAUS_RADICALE_URL"), "/"); u != "" {
		if p, err := url.Parse(u); err != nil || (p.Scheme != "http" && p.Scheme != "https") || p.Host == "" {
			return cfg, fmt.Errorf("HAUS_RADICALE_URL %q: want http(s)://host[:port]", u)
		}
		user, pw := os.Getenv("HAUS_RADICALE_USER"), os.Getenv("HAUS_RADICALE_PASSWORD")
		if user == "" || pw == "" {
			log.Printf("haus: HAUS_RADICALE_USER/HAUS_RADICALE_PASSWORD not set: no calendar tools")
		} else {
			cfg.Calendar = &caldav.Client{URL: u, User: user, Password: pw, Calendar: os.Getenv("HAUS_RADICALE_CALENDAR"), Loc: cfg.Loc}
		}
	}
	if d := os.Getenv("HAUS_NOTES_DIR"); d != "" {
		n, err := notes.Open(d)
		if err != nil {
			log.Printf("haus: HAUS_NOTES_DIR: %v: no note tools", err)
		} else {
			cfg.Notes = n
		}
	}
	return cfg, nil
}

// Handler serves POST /mcp/haushalt.
func Handler(cfg Config) http.Handler {
	if cfg.Loc == nil {
		cfg.Loc = time.Local
	}
	return (&mcpserver.Server{
		Name:    "haushalt",
		Version: cfg.Version,
		Instructions: "Der Haushalt: Kalender, Erinnerungen und Notizen der Familie. Was du hier anlegst, " +
			"sieht die Familie in ihren eigenen Apps (Kalender auf dem Telefon, Notizen im Dateiordner); " +
			"Haus merkt sich nichts selbst. Zeiten sind Ortszeit (" + cfg.Loc.String() + "), " +
			"Datum JJJJ-MM-TT, Uhrzeit JJJJ-MM-TTTHH:MM.",
		Token: cfg.Token,
		Tools: Tools(cfg),
	}).Handler()
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }

func obj(props map[string]any, required ...string) map[string]any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{"type": "object", "properties": props, "required": required}
}

const whenHelp = "JJJJ-MM-TT (ganzer Tag) oder JJJJ-MM-TTTHH:MM, Ortszeit"

// MaxField caps a title or a place, MaxReminder a reminder's text, in
// characters: refused on a write, cut in an answer.
const (
	MaxField    = 200
	MaxReminder = 500
)

func tooLong(name, v string, max int) error {
	if utf8.RuneCountInString(v) > max {
		return fmt.Errorf("%s ist zu lang (höchstens %d Zeichen)", name, max)
	}
	return nil
}

func cut(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// Tools are the tools whose backends are configured.
func Tools(cfg Config) []mcpserver.Tool {
	loc := cfg.Loc
	if loc == nil {
		loc = time.Local
	}
	var out []mcpserver.Tool
	if c := cfg.Calendar; c != nil {
		out = append(out,
			mcpserver.Tool{
				Name:        "termine",
				Description: "Termine aus dem Familienkalender in einem Zeitraum. Ohne bis: nur der Tag von von.",
				Schema:      obj(map[string]any{"von": str("Beginn des Zeitraums: " + whenHelp), "bis": str("Ende des Zeitraums (ausschließlich): " + whenHelp)}, "von"),
				Call:        func(ctx context.Context, a map[string]any) (string, error) { return termine(ctx, c, loc, a) },
			},
			mcpserver.Tool{
				Name:        "termin_anlegen",
				Description: "Legt einen Termin im Familienkalender an. Ohne ende: eine Stunde, bei einem Datum ohne Uhrzeit der ganze Tag.",
				Schema: obj(map[string]any{"titel": str("Worum es geht"), "beginn": str(whenHelp), "ende": str("optional: " + whenHelp),
					"ort": str("optional: wo")}, "titel", "beginn"),
				Call: func(ctx context.Context, a map[string]any) (string, error) { return terminAnlegen(ctx, c, loc, a) },
			},
			mcpserver.Tool{
				Name:        "erinnerungen",
				Description: "Offene Erinnerungen (Aufgaben) aus dem Familienkalender, die nächste zuerst.",
				Schema:      obj(map[string]any{}),
				Call:        func(ctx context.Context, a map[string]any) (string, error) { return erinnerungen(ctx, c, loc) },
			},
			mcpserver.Tool{
				Name:        "erinnerung_anlegen",
				Description: "Legt eine Erinnerung an: eine Aufgabe im Familienkalender, die das Telefon zum Zeitpunkt meldet.",
				Schema:      obj(map[string]any{"text": str("Woran erinnert werden soll"), "wann": str(whenHelp + "; ein Datum ohne Uhrzeit meint 9:00")}, "text", "wann"),
				Call:        func(ctx context.Context, a map[string]any) (string, error) { return erinnerungAnlegen(ctx, c, loc, a) },
			},
		)
	}
	if n := cfg.Notes; n != nil {
		out = append(out,
			mcpserver.Tool{
				Name:        "notizen_suchen",
				Description: "Sucht in den Notizen der Familie (Titel und Text, Groß-/Kleinschreibung egal) und zeigt Titel und erste Zeilen.",
				Schema:      obj(map[string]any{"text": str("Wonach gesucht wird")}, "text"),
				Call:        func(ctx context.Context, a map[string]any) (string, error) { return notizenSuchen(ctx, n, a) },
			},
			mcpserver.Tool{
				Name:        "notiz_lesen",
				Description: "Liest eine Notiz ganz, nach Titel oder Dateiname (wie notizen_suchen ihn zeigt).",
				Schema:      obj(map[string]any{"titel": str("Titel oder Dateiname der Notiz")}, "titel"),
				Call:        func(ctx context.Context, a map[string]any) (string, error) { return notizLesen(ctx, n, a) },
			},
			mcpserver.Tool{
				Name:        "notiz_anlegen",
				Description: "Legt eine neue Notiz als Markdown-Datei im Notizen-Ordner der Familie an; überschreibt nie eine bestehende.",
				Schema:      obj(map[string]any{"titel": str("Titel der Notiz"), "text": str("Inhalt, Markdown erlaubt")}, "titel", "text"),
				Call:        func(ctx context.Context, a map[string]any) (string, error) { return notizAnlegen(n, a) },
			},
		)
	}
	return out
}

// ParseWhen reads a date or a local time; dateOnly says there was no time.
func ParseWhen(s string, loc *time.Location) (t time.Time, dateOnly bool, err error) {
	s = strings.TrimSpace(s)
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.In(loc), false, nil
	}
	for _, l := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04", "2006-01-02 15:04:05"} {
		if t, err := time.ParseInLocation(l, s, loc); err == nil {
			return t, false, nil
		}
	}
	if t, err := time.ParseInLocation("2006-01-02", s, loc); err == nil {
		return t, true, nil
	}
	return time.Time{}, false, fmt.Errorf("%q: erwarte JJJJ-MM-TT oder JJJJ-MM-TTTHH:MM", s)
}

var weekday = [...]string{"So", "Mo", "Di", "Mi", "Do", "Fr", "Sa"}

func day(t time.Time) string { return weekday[t.Weekday()] + " " + t.Format("02.01.2006") }

func dayTime(t time.Time) string { return day(t) + " " + t.Format("15:04") }

// eventLine is one appointment in a sentence's worth of text.
func eventLine(e caldav.Event, loc *time.Location) string {
	var when string
	if e.AllDay {
		last := e.End.AddDate(0, 0, -1)
		when = day(e.Start) + " ganztägig"
		if last.After(e.Start) {
			when = day(e.Start) + " – " + day(last) + " ganztägig"
		}
	} else {
		s, en := e.Start.In(loc), e.End.In(loc)
		switch {
		case !en.After(s):
			when = dayTime(s)
		case s.Format("20060102") == en.Format("20060102"):
			when = dayTime(s) + "–" + en.Format("15:04")
		default:
			when = dayTime(s) + " – " + dayTime(en)
		}
	}
	line := when + ": " + orDash(e.Summary)
	if loc := oneLine(e.Location); loc != "" {
		line += " (" + cut(loc, MaxField) + ")"
	}
	return line
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// orDash is a title for an answer: one line, cut at MaxField.
func orDash(s string) string {
	if s = oneLine(s); s == "" {
		return "(ohne Titel)"
	}
	return cut(s, MaxField)
}

const maxEvents = 50

func termine(ctx context.Context, c *caldav.Client, loc *time.Location, a map[string]any) (string, error) {
	from, fromDate, err := ParseWhen(mcpserver.Str(a, "von"), loc)
	if err != nil {
		return "", fmt.Errorf("von: %w", err)
	}
	to := from.AddDate(0, 0, 1)
	if !fromDate {
		to = from.Add(24 * time.Hour)
	}
	if b := mcpserver.Str(a, "bis"); b != "" {
		if to, _, err = ParseWhen(b, loc); err != nil {
			return "", fmt.Errorf("bis: %w", err)
		}
	}
	if !to.After(from) {
		return "", errors.New("bis muss nach von liegen")
	}
	if to.Sub(from) > 366*24*time.Hour {
		return "", errors.New("höchstens ein Jahr auf einmal")
	}
	evs, err := c.Events(ctx, from, to)
	if err != nil {
		return "", err
	}
	if len(evs) == 0 {
		return fmt.Sprintf("Keine Termine von %s bis %s.", dayTime(from), dayTime(to)), nil
	}
	var b strings.Builder
	for i, e := range evs {
		if i == maxEvents {
			fmt.Fprintf(&b, "… und %d weitere; frag nach einem kürzeren Zeitraum.\n", len(evs)-maxEvents)
			break
		}
		b.WriteString(eventLine(e, loc) + "\n")
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func terminAnlegen(ctx context.Context, c *caldav.Client, loc *time.Location, a map[string]any) (string, error) {
	title := mcpserver.Str(a, "titel")
	if title == "" {
		return "", errors.New("titel fehlt")
	}
	if err := errors.Join(tooLong("titel", title, MaxField), tooLong("ort", mcpserver.Str(a, "ort"), MaxField)); err != nil {
		return "", err
	}
	start, allDay, err := ParseWhen(mcpserver.Str(a, "beginn"), loc)
	if err != nil {
		return "", fmt.Errorf("beginn: %w", err)
	}
	e := caldav.Event{Summary: title, Location: mcpserver.Str(a, "ort"), Start: start, AllDay: allDay}
	if allDay {
		e.End = start.AddDate(0, 0, 1)
	} else {
		e.End = start.Add(time.Hour)
	}
	if s := mcpserver.Str(a, "ende"); s != "" {
		end, endDate, err := ParseWhen(s, loc)
		if err != nil {
			return "", fmt.Errorf("ende: %w", err)
		}
		if endDate != allDay {
			return "", errors.New("beginn und ende beide mit Uhrzeit oder beide ohne")
		}
		if allDay {
			end = end.AddDate(0, 0, 1) // the last day counts; DTEND is exclusive
		}
		e.End = end
	}
	if !e.End.After(e.Start) {
		return "", errors.New("ende muss nach beginn liegen")
	}
	col, err := c.PutEvent(ctx, e)
	if err != nil {
		return "", err
	}
	return "Termin angelegt im Kalender „" + col.Name + "“: " + eventLine(e, loc), nil
}

func erinnerungAnlegen(ctx context.Context, c *caldav.Client, loc *time.Location, a map[string]any) (string, error) {
	text := mcpserver.Str(a, "text")
	if text == "" {
		return "", errors.New("text fehlt")
	}
	if err := tooLong("text", text, MaxReminder); err != nil {
		return "", err
	}
	when, dateOnly, err := ParseWhen(mcpserver.Str(a, "wann"), loc)
	if err != nil {
		return "", fmt.Errorf("wann: %w", err)
	}
	if dateOnly { // a day without a time: 9:00 that day, also when the clocks change
		when = time.Date(when.Year(), when.Month(), when.Day(), 9, 0, 0, 0, loc)
	}
	col, err := c.PutTodo(ctx, caldav.Todo{Summary: text, Due: when})
	if err != nil {
		return "", err
	}
	return "Erinnerung angelegt im Kalender „" + col.Name + "“: " + dayTime(when.In(loc)) + ": " + orDash(text), nil
}

const maxTodos = 30

func erinnerungen(ctx context.Context, c *caldav.Client, loc *time.Location) (string, error) {
	todos, err := c.OpenTodos(ctx)
	if err != nil {
		return "", err
	}
	if len(todos) == 0 {
		return "Keine offenen Erinnerungen.", nil
	}
	var b strings.Builder
	for i, t := range todos {
		if i == maxTodos {
			fmt.Fprintf(&b, "… und %d weitere.\n", len(todos)-maxTodos)
			break
		}
		when := "ohne Zeitpunkt"
		if !t.Due.IsZero() {
			when = dayTime(t.Due.In(loc))
		}
		b.WriteString(when + ": " + orDash(t.Summary) + "\n")
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

const maxHits = 20

func notizenSuchen(ctx context.Context, n *notes.Dir, a map[string]any) (string, error) {
	q := mcpserver.Str(a, "text")
	res, err := n.Search(ctx, q, maxHits)
	if err != nil {
		return "", err
	}
	more := ""
	if res.Partial {
		more = "Nur ein Teil der Notizen ist durchsucht (zu viele oder zu große Dateien); such genauer.\n"
	}
	if len(res.Hits) == 0 {
		return strings.TrimRight(more+"Keine Notiz enthält „"+cut(q, MaxField)+"“.", "\n"), nil
	}
	var b strings.Builder
	for _, h := range res.Hits {
		b.WriteString("## " + h.Title + " (" + h.Name + ")\n")
		for _, l := range h.Lines {
			b.WriteString(l + "\n")
		}
		b.WriteString("\n")
	}
	if res.Total > len(res.Hits) {
		fmt.Fprintf(&b, "… und %d weitere; such genauer.\n", res.Total-len(res.Hits))
	}
	b.WriteString(more)
	return strings.TrimRight(b.String(), "\n"), nil
}

func notizLesen(ctx context.Context, n *notes.Dir, a map[string]any) (string, error) {
	note, err := n.Read(ctx, mcpserver.Str(a, "titel"))
	if err != nil {
		return "", err
	}
	out := "(" + note.Name + ")\n" + note.Text
	if note.Truncated {
		out += "\n… (gekürzt)"
	}
	return out, nil
}

func notizAnlegen(n *notes.Dir, a map[string]any) (string, error) {
	name, err := n.Create(mcpserver.Str(a, "titel"), mcpserver.Str(a, "text"))
	if err != nil {
		return "", err
	}
	return "Notiz angelegt: " + name + " im Notizen-Ordner.", nil
}
