// Package haushalt is the "Haushalt" MCP source (laib ADR 0017 §2): the one
// way the Bote and Claude reach the household's own data — appointments,
// reminders and contacts in Radicale, notes as Markdown in the file share,
// documents in Paperless. Haus keeps none of it: the Bote remembers about
// you, never for you (§3), so a note becomes a file, a reminder a calendar
// entry and a contact a card, where people see them. A tool is listed only
// when its backend is configured.
package haushalt

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/mdopp/haus/internal/caldav"
	"github.com/mdopp/haus/internal/cards"
	"github.com/mdopp/haus/internal/library"
	"github.com/mdopp/haus/internal/mcpserver"
	"github.com/mdopp/haus/internal/notes"
	"github.com/mdopp/haus/internal/paperless"
	"github.com/mdopp/haus/internal/qmd"
)

// Config is what the endpoint serves.
type Config struct {
	Token     string            // HAUS_HAUSHALT_TOKEN, the Bote's bearer
	Calendar  *caldav.Client    // nil: no calendar or reminder tools
	Contacts  *caldav.Client    // nil: no contact tools (the same Radicale account)
	Notes     *notes.Dir        // nil: no note tools
	Library   *qmd.Client       // the memory library's search (haus#13); nil, or no Notes: no notizen_finden
	Documents *paperless.Client // nil: no document tool
	Loc       *time.Location    // the house's time zone
	// PhoneCountry is the house's calling code (HAUS_PHONE_COUNTRY, default
	// 49): a national number 0170 … is +49 170 … when numbers are compared.
	PhoneCountry string
	// PublicURL is Haus's own origin (HAUS_PUBLIC_URL): an answer links a
	// contact or a document to its page on Haus; "": no links.
	PublicURL string
	Version   string
}

const defaultCountry = "49"

// defaultCollection is the notes' collection in qmd's index.yml (laib#195).
const defaultCollection = "notizen"

var countryRE = regexp.MustCompile(`^[1-9][0-9]{0,2}$`)

// httpURL says whether u is http(s)://host[...].
func httpURL(u string) bool {
	p, err := url.Parse(u)
	return err == nil && (p.Scheme == "http" || p.Scheme == "https") && p.Host != ""
}

// FromEnv reads HAUS_HAUSHALT_TOKEN, HAUS_RADICALE_URL (+ _USER,
// _PASSWORD, optional _CALENDAR and _ADDRESSBOOK), HAUS_NOTES_DIR,
// HAUS_QMD_URL (+ optional _COLLECTION, default notizen),
// HAUS_PAPERLESS_URL (+ _TOKEN, optional _PUBLIC_URL), HAUS_PHONE_COUNTRY,
// HAUS_PUBLIC_URL and TZ. A malformed backend address or country code
// refuses to start; a missing backend only leaves its tools out. Haus's own
// address is the setup link's too, so a malformed one only drops the links.
func FromEnv() (Config, error) {
	cfg := Config{Token: os.Getenv("HAUS_HAUSHALT_TOKEN"), Loc: time.Local, PhoneCountry: defaultCountry}
	if pub := strings.TrimRight(os.Getenv("HAUS_PUBLIC_URL"), "/"); pub != "" {
		if httpURL(pub) {
			cfg.PublicURL = pub
		} else {
			log.Printf("haus: HAUS_PUBLIC_URL %q is not http(s)://host: the Haushalt's answers carry no links", pub)
		}
	}
	if cc := strings.TrimPrefix(strings.TrimSpace(os.Getenv("HAUS_PHONE_COUNTRY")), "+"); cc != "" {
		if !countryRE.MatchString(cc) {
			return cfg, fmt.Errorf("HAUS_PHONE_COUNTRY %q: want a calling code like 49", cc)
		}
		cfg.PhoneCountry = cc
	}
	if tz := os.Getenv("TZ"); tz != "" {
		if l, err := time.LoadLocation(tz); err == nil {
			cfg.Loc = l
		}
	}
	if cfg.Token == "" {
		log.Printf("haus: HAUS_HAUSHALT_TOKEN is not set: /mcp/haushalt refuses everyone")
	}
	if u := strings.TrimRight(os.Getenv("HAUS_RADICALE_URL"), "/"); u != "" {
		if !httpURL(u) {
			return cfg, fmt.Errorf("HAUS_RADICALE_URL %q: want http(s)://host[:port]", u)
		}
		user, pw := os.Getenv("HAUS_RADICALE_USER"), os.Getenv("HAUS_RADICALE_PASSWORD")
		if user == "" || pw == "" {
			log.Printf("haus: HAUS_RADICALE_USER/HAUS_RADICALE_PASSWORD not set: no calendar or contact tools")
		} else {
			cfg.Calendar = &caldav.Client{URL: u, User: user, Password: pw, Calendar: os.Getenv("HAUS_RADICALE_CALENDAR"),
				AddressBook: os.Getenv("HAUS_RADICALE_ADDRESSBOOK"), Loc: cfg.Loc}
			cfg.Contacts = cfg.Calendar
		}
	}
	if u := strings.TrimRight(os.Getenv("HAUS_PAPERLESS_URL"), "/"); u != "" {
		if !httpURL(u) {
			return cfg, fmt.Errorf("HAUS_PAPERLESS_URL %q: want http(s)://host[:port]", u)
		}
		pub := strings.TrimRight(os.Getenv("HAUS_PAPERLESS_PUBLIC_URL"), "/")
		if pub != "" && !httpURL(pub) {
			return cfg, fmt.Errorf("HAUS_PAPERLESS_PUBLIC_URL %q: want http(s)://host", pub)
		}
		if tok := os.Getenv("HAUS_PAPERLESS_TOKEN"); tok == "" {
			log.Printf("haus: HAUS_PAPERLESS_TOKEN not set: no document tool")
		} else {
			cfg.Documents = &paperless.Client{URL: u, Token: tok, PublicURL: pub}
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
	// haus#13: qmd, in Haus's pod on loopback (laib#195); its hits are note files
	if u := strings.TrimRight(os.Getenv("HAUS_QMD_URL"), "/"); u != "" {
		if !httpURL(u) {
			return cfg, fmt.Errorf("HAUS_QMD_URL %q: want http://127.0.0.1:<port>", u)
		}
		coll := strings.TrimSpace(os.Getenv("HAUS_QMD_COLLECTION"))
		if coll == "" {
			coll = defaultCollection
		}
		if cfg.Notes == nil {
			log.Printf("haus: HAUS_QMD_URL is set but there is no notes folder: no memory library")
		} else {
			cfg.Library = &qmd.Client{URL: u, Collection: coll}
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
		Instructions: "Der Haushalt: Kalender, Erinnerungen, Notizen, Kontakte und Dokumente der Familie. Was du hier anlegst, " +
			"sieht die Familie in ihren eigenen Apps (Kalender und Adressbuch auf dem Telefon, Notizen im Dateiordner, " +
			"Dokumente in Paperless, die du nur liest); Haus merkt sich nichts selbst. Zeiten sind Ortszeit (" + cfg.Loc.String() + "), " +
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
	// the tools appended since from reach backend over the network
	plainFrom := func(from int, backend string) {
		for i := from; i < len(out); i++ {
			out[i].Call = plainWhenDown(backend, out[i].Name, out[i].Call)
		}
	}
	if c := cfg.Calendar; c != nil {
		from := len(out)
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
		plainFrom(from, "Radicale")
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
		if lib := cfg.Library; lib != nil {
			pub := strings.TrimRight(cfg.PublicURL, "/")
			out = append(out, mcpserver.Tool{
				Name: "notizen_finden",
				Description: "Findet Notizen der Familie nach ihrer Bedeutung, auch wenn andere Wörter darin stehen " +
					"(„wann kommt der Schornsteinfeger“ findet die Notiz zum Kaminkehrer). Zeigt Titel, Dateiname, eine passende Stelle und die Seite der Notiz.",
				Schema: obj(map[string]any{"text": str("Wonach gesucht wird, in eigenen Worten")}, "text"),
				Call:   func(ctx context.Context, a map[string]any) (string, error) { return notizenFinden(ctx, lib, n, pub, a) },
			})
		}
	}
	cc := cfg.PhoneCountry
	if cc == "" {
		cc = defaultCountry
	}
	pub := strings.TrimRight(cfg.PublicURL, "/")
	if c := cfg.Contacts; c != nil {
		from := len(out)
		out = append(out,
			mcpserver.Tool{
				Name:        "kontakte_suchen",
				Description: "Sucht im Adressbuch der Familie nach Name, Telefonnummer oder E-Mail und zeigt Telefon, E-Mail und Geburtstag.",
				Schema:      obj(map[string]any{"text": str("Name, Teil einer Nummer oder E-Mail-Adresse")}, "text"),
				Call:        func(ctx context.Context, a map[string]any) (string, error) { return kontakteSuchen(ctx, c, cc, a) },
			},
			mcpserver.Tool{
				Name: "kontakt_lesen",
				Description: "Zeigt einen Kontakt aus dem Adressbuch der Familie ganz: alle Nummern und E-Mail-Adressen, Geburtstag, Anschrift " +
					"und einen Link zu seiner Seite. Passen mehrere, nennt die Antwort sie mit ihrer Kennung; dann mit dem ganzen Namen oder der Kennung noch einmal fragen.",
				Schema: obj(map[string]any{"kontakt": str("Name (ganz oder ein Teil), Nummer, E-Mail-Adresse oder die Kennung des Kontakts")}, "kontakt"),
				Call:   func(ctx context.Context, a map[string]any) (string, error) { return kontaktLesen(ctx, c, cc, pub, a) },
			},
			mcpserver.Tool{
				Name:        "kontakt_anlegen",
				Description: "Legt einen Kontakt im Adressbuch der Familie an, das die Telefone synchronisieren; ändert nie einen bestehenden.",
				Schema: obj(map[string]any{"name": str("Der ganze Name, wie er im Adressbuch stehen soll"), "telefon": str("optional: Telefonnummer, z. B. +49 170 1234567"),
					"email": str("optional: E-Mail-Adresse"), "geburtstag": str("optional: JJJJ-MM-TT, oder --MM-TT ohne Jahr")}, "name"),
				Call: func(ctx context.Context, a map[string]any) (string, error) { return kontaktAnlegen(ctx, c, a) },
			},
		)
		plainFrom(from, "Radicale")
	}
	if d := cfg.Documents; d != nil {
		from := len(out)
		out = append(out, mcpserver.Tool{
			Name:        "dokumente_suchen",
			Description: "Sucht in den Dokumenten der Familie (Paperless, Volltext) und zeigt Datum, Absender, Titel, Schlagworte und einen Link; nur lesen.",
			Schema:      obj(map[string]any{"text": str("Wonach gesucht wird, z. B. Rechnung Stadtwerke")}, "text"),
			Call:        func(ctx context.Context, a map[string]any) (string, error) { return dokumenteSuchen(ctx, d, pub, a) },
		})
		plainFrom(from, "Paperless")
	}
	return out
}

// plainWhenDown answers a backend that cannot be reached (refused, timed
// out, no such host) in a plain sentence: which one, and to try again
// later. The transport's words and the internal address go to Haus's log,
// not to the person. Any other error stays as it is.
func plainWhenDown(backend, tool string, call func(context.Context, map[string]any) (string, error)) func(context.Context, map[string]any) (string, error) {
	return func(ctx context.Context, a map[string]any) (string, error) {
		out, err := call(ctx, a)
		var ue *url.Error
		if err != nil && errors.As(err, &ue) {
			log.Printf("haus: haushalt %s: %v", tool, err)
			return "", fmt.Errorf("%s antwortet gerade nicht; später noch einmal versuchen", backend)
		}
		return out, err
	}
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
		switch {
		case !t.Due.IsZero() && t.DueAllDay: // due on a day: no time to show
			when = day(t.Due.In(loc))
		case !t.Due.IsZero():
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

// maxFound is how many notes notizen_finden shows: the best few, not a list.
const maxFound = 8

// reducedNote opens notizen_finden's answer when the search by meaning
// cannot answer and the words did (haus#13).
const reducedNote = "Die Suche nach Bedeutung geht gerade nicht; gesucht wurde nur nach den Wörtern."

// notizenFinden is the memory library's search: title, file, a passage
// and, with Haus's address, the note's page.
func notizenFinden(ctx context.Context, lib *qmd.Client, n *notes.Dir, pub string, a map[string]any) (string, error) {
	q := mcpserver.Str(a, "text")
	res, err := library.Find(ctx, lib, n, q, maxFound)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if res.Reduced {
		b.WriteString(reducedNote + "\n\n")
	}
	if len(res.Hits) == 0 {
		if res.Reduced {
			b.WriteString("Keine Notiz enthält „" + cut(q, MaxField) + "“.")
		} else {
			b.WriteString("Keine Notiz passt zu „" + cut(q, MaxField) + "“.")
		}
		return b.String(), nil
	}
	for _, h := range res.Hits {
		b.WriteString("## " + h.Title + " (" + h.Name + ")\n")
		for _, l := range h.Lines {
			b.WriteString(l + "\n")
		}
		if pub != "" {
			b.WriteString(pub + cards.NotePage(h.Name) + "\n")
		}
		b.WriteString("\n")
	}
	if res.Partial {
		b.WriteString("Nur ein Teil der Notizen ist durchsucht (zu viele oder zu große Dateien); such genauer.\n")
	}
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

// maxContacts is how many contacts a search shows: an answer for a phone
// call, not an export.
const maxContacts = 10

var (
	phoneRE  = regexp.MustCompile(`^\+?[0-9 ()./-]{3,40}$`)
	digitsRE = regexp.MustCompile(`[^0-9]`)
)

func digits(s string) string { return digitsRE.ReplaceAllString(s, "") }

// birthdayDay shows JJJJ-MM-TT as TT.MM.JJJJ and --MM-TT as TT.MM.
func birthdayDay(b string) string {
	if t, err := time.Parse("2006-01-02", b); err == nil {
		return t.Format("02.01.2006")
	}
	if md, ok := strings.CutPrefix(b, "--"); ok {
		if t, err := time.Parse("2006-01-02", "2000-"+md); err == nil {
			return t.Format("02.01.")
		}
	}
	return cut(oneLine(b), 40)
}

// contactLine is one contact in a line: name, then at most three phones and
// three emails, and the birthday.
func contactLine(ct caldav.Contact) string {
	line := orDash(ct.Name)
	if org := oneLine(ct.Org); org != "" {
		line += " (" + cut(org, 60) + ")"
	}
	var parts []string
	few := func(vs []string, n int) string {
		var keep []string
		for _, v := range vs {
			if len(keep) == 3 {
				break
			}
			if v = oneLine(v); v != "" {
				keep = append(keep, cut(v, n))
			}
		}
		return strings.Join(keep, ", ")
	}
	if p := few(ct.Phones, 40); p != "" {
		parts = append(parts, "Tel. "+p)
	}
	if e := few(ct.Emails, 100); e != "" {
		parts = append(parts, "E-Mail "+e)
	}
	if ct.Birthday != "" {
		parts = append(parts, "Geburtstag "+birthdayDay(ct.Birthday))
	}
	if len(parts) > 0 {
		line += " — " + strings.Join(parts, " · ")
	}
	return line
}

// international is a number as country code and national number, digits
// only, however it is written: +49 (0)170 …, 0049 170 …, 0170 … (the
// house's country cc) all give 49170….
func international(s, cc string) string {
	d := digits(s)
	if strings.HasPrefix(strings.TrimSpace(s), "+") || strings.HasPrefix(d, "00") {
		d = strings.TrimPrefix(d, "00")
		if rest, ok := strings.CutPrefix(d, cc); ok { // another country's code stays as written
			return cc + strings.TrimLeft(rest, "0") // +49 (0)711: the trunk 0 goes
		}
		return d
	}
	return cc + strings.TrimLeft(d, "0")
}

// matches says whether a contact fits the search: name, nickname,
// organisation or email contain it, any case. A number (three digits or
// more) matches a phone by its digits, however either side is written:
// with + or 00 the whole international number counts (+43 660 … never
// finds +49 660 …); otherwise it is a piece of the number, leading zeros
// dropped (0170 1234567 finds +49 170 1234567, 0815 finds …0815).
func matches(ct caldav.Contact, q, cc string) bool {
	lq := strings.ToLower(q)
	for _, f := range append([]string{ct.Name, ct.Nickname, ct.Org}, ct.Emails...) {
		if strings.Contains(strings.ToLower(f), lq) {
			return true
		}
	}
	if !phoneRE.MatchString(q) {
		return false
	}
	want := strings.TrimLeft(digits(q), "0")
	if len(want) < 3 {
		return false
	}
	if strings.HasPrefix(q, "+") || strings.HasPrefix(digits(q), "00") {
		want = international(q, cc)
	}
	for _, p := range ct.Phones {
		if strings.Contains(international(p, cc), want) {
			return true
		}
	}
	return false
}

func kontakteSuchen(ctx context.Context, c *caldav.Client, cc string, a map[string]any) (string, error) {
	q := oneLine(mcpserver.Str(a, "text"))
	if q == "" {
		return "", errors.New("text fehlt")
	}
	if err := tooLong("text", q, MaxField); err != nil {
		return "", err
	}
	all, partial, err := c.Contacts(ctx)
	if err != nil {
		return "", err
	}
	var hits []caldav.Contact
	for _, ct := range all {
		if matches(ct, q, cc) {
			hits = append(hits, ct)
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return strings.ToLower(hits[i].Name) < strings.ToLower(hits[j].Name) })
	more := ""
	if partial {
		more = "Nur ein Teil des Adressbuchs ist durchsucht (zu viele Kontakte); such genauer.\n"
	}
	if len(hits) == 0 {
		return strings.TrimRight(more+"Kein Kontakt passt zu „"+cut(q, MaxField)+"“.", "\n"), nil
	}
	var b strings.Builder
	for i, ct := range hits {
		if i == maxContacts {
			fmt.Fprintf(&b, "… und %d weitere; such genauer.\n", len(hits)-maxContacts)
			break
		}
		b.WriteString(contactLine(ct) + "\n")
	}
	b.WriteString(more)
	return strings.TrimRight(b.String(), "\n"), nil
}

// findContact is the contact q names: the one whose id it is, else those
// whose whole name it is (any case), else those a search for it finds.
func findContact(all []caldav.Contact, q, cc string) []caldav.Contact {
	var hits []caldav.Contact
	for _, ct := range all {
		if oneLine(ct.UID) == q {
			hits = append(hits, ct)
		}
	}
	if len(hits) > 0 {
		return hits
	}
	for _, ct := range all {
		if strings.EqualFold(oneLine(ct.Name), q) {
			hits = append(hits, ct)
		}
	}
	if len(hits) > 0 {
		return hits
	}
	for _, ct := range all {
		if matches(ct, q, cc) {
			hits = append(hits, ct)
		}
	}
	return hits
}

// some is at most max of vs, each one line cut at n, joined with sep, and
// how many more there are.
func some(vs []string, max, n int, sep string) (out string, more int) {
	var keep []string
	for _, v := range vs {
		if v = oneLine(v); v == "" {
			continue
		}
		if len(keep) == max {
			more++
			continue
		}
		keep = append(keep, cut(v, n))
	}
	return strings.Join(keep, sep), more
}

// listAll is some with the rest said: "a, b … und 3 weitere".
func listAll(vs []string, n int, sep string) string {
	out, more := some(vs, maxOfAKind, n, sep)
	if more > 0 {
		out += fmt.Sprintf(" … und %d weitere", more)
	}
	return out
}

// maxOfAKind is how many numbers, emails or addresses one contact shows.
const maxOfAKind = 10

// contactCard is one contact whole, a line a kind, and its page on Haus
// when Haus's address is known.
func contactCard(ct caldav.Contact, pub string) string {
	head := orDash(ct.Name)
	if nick := oneLine(ct.Nickname); nick != "" {
		head += " („" + cut(nick, 60) + "“)"
	}
	lines := []string{head}
	add := func(label, v string) {
		if v != "" {
			lines = append(lines, label+": "+v)
		}
	}
	add("Firma", cut(oneLine(ct.Org), 100))
	add("Telefon", listAll(ct.Phones, 40, ", "))
	add("E-Mail", listAll(ct.Emails, 100, ", "))
	if ct.Birthday != "" {
		add("Geburtstag", birthdayDay(ct.Birthday))
	}
	add("Adresse", listAll(ct.Addresses, MaxField, "; "))
	if pub != "" && oneLine(ct.UID) != "" { // the UID as the page matches it
		add("Seite", pub+cards.ContactPage(ct.UID))
	}
	return strings.Join(lines, "\n")
}

func kontaktLesen(ctx context.Context, c *caldav.Client, cc, pub string, a map[string]any) (string, error) {
	q := oneLine(mcpserver.Str(a, "kontakt"))
	if q == "" {
		return "", errors.New("kontakt fehlt")
	}
	if err := tooLong("kontakt", q, MaxField); err != nil {
		return "", err
	}
	all, partial, err := c.Contacts(ctx)
	if err != nil {
		return "", err
	}
	hits := findContact(all, q, cc)
	more := ""
	if partial {
		more = "\nNur ein Teil des Adressbuchs ist durchsucht (zu viele Kontakte); such genauer."
	}
	switch len(hits) {
	case 0:
		return "Kein Kontakt passt zu „" + cut(q, MaxField) + "“." + more, nil
	case 1:
		return contactCard(hits[0], pub), nil
	}
	sort.SliceStable(hits, func(i, j int) bool { return strings.ToLower(hits[i].Name) < strings.ToLower(hits[j].Name) })
	var b strings.Builder
	b.WriteString("Mehrere Kontakte passen zu „" + cut(q, MaxField) + "“; welcher ist gemeint? Den ganzen Namen oder die Kennung nennen:\n")
	for i, ct := range hits {
		if i == maxContacts {
			fmt.Fprintf(&b, "… und %d weitere; such genauer.\n", len(hits)-maxContacts)
			break
		}
		line := contactLine(ct)
		if uid := oneLine(ct.UID); uid != "" {
			line += " · Kennung " + cut(uid, MaxField)
		}
		b.WriteString(line + "\n")
	}
	return strings.TrimRight(b.String(), "\n") + more, nil
}

func kontaktAnlegen(ctx context.Context, c *caldav.Client, a map[string]any) (string, error) {
	ct := caldav.Contact{Name: oneLine(mcpserver.Str(a, "name"))}
	if ct.Name == "" {
		return "", errors.New("name fehlt")
	}
	if err := tooLong("name", ct.Name, MaxField); err != nil {
		return "", err
	}
	if p := mcpserver.Str(a, "telefon"); p != "" {
		if !phoneRE.MatchString(p) || len(digits(p)) < 3 {
			return "", fmt.Errorf("telefon: %q ist keine Telefonnummer (Ziffern, +, Leerzeichen, -, /)", cut(p, 40))
		}
		ct.Phones = []string{p}
	}
	if e := mcpserver.Str(a, "email"); e != "" {
		if addr, err := mail.ParseAddress(e); err != nil || addr.Address != e || addr.Name != "" || len(e) > 254 {
			return "", fmt.Errorf("email: %q ist keine E-Mail-Adresse", cut(oneLine(e), 100))
		}
		ct.Emails = []string{e}
	}
	if g := mcpserver.Str(a, "geburtstag"); g != "" {
		if !caldav.ValidBirthday(g) {
			return "", fmt.Errorf("geburtstag: %q: erwarte JJJJ-MM-TT, oder --MM-TT ohne Jahr", cut(g, 40))
		}
		ct.Birthday = g
	}
	col, err := c.PutContact(ctx, ct)
	if err != nil {
		return "", err
	}
	return "Kontakt angelegt im Adressbuch „" + col.Name + "“: " + contactLine(ct), nil
}

// maxDocs is how many documents a search shows.
const maxDocs = 20

// maxTags is how many tags a document shows.
const maxTags = 5

func dokumenteSuchen(ctx context.Context, d *paperless.Client, pub string, a map[string]any) (string, error) {
	q := oneLine(mcpserver.Str(a, "text"))
	if q == "" {
		return "", errors.New("text fehlt")
	}
	if err := tooLong("text", q, MaxField); err != nil {
		return "", err
	}
	docs, total, err := d.Search(ctx, q, maxDocs)
	if err != nil {
		return "", err
	}
	if len(docs) == 0 {
		return "Kein Dokument passt zu „" + cut(q, MaxField) + "“.", nil
	}
	var b strings.Builder
	for _, doc := range docs {
		var parts []string
		if !doc.Created.IsZero() {
			parts = append(parts, doc.Created.Format("02.01.2006"))
		}
		if c := oneLine(doc.Correspondent); c != "" {
			parts = append(parts, cut(c, 100))
		}
		parts = append(parts, orDash(doc.Title))
		if t, more := some(doc.Tags, maxTags, 40, ", "); t != "" {
			if more > 0 {
				t += fmt.Sprintf(" (+%d)", more)
			}
			parts = append(parts, "Schlagworte: "+t)
		}
		line := strings.Join(parts, " · ")
		// Haus's page sends the person on to Paperless (haus#28); it leads
		// somewhere only when Paperless's public address is set
		if pub != "" && doc.Link != "" {
			line += " — " + pub + cards.DocumentPage(doc.ID)
		}
		b.WriteString(line + "\n")
	}
	if total > len(docs) {
		fmt.Fprintf(&b, "… und %d weitere; such genauer.\n", total-len(docs))
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// ErrBadUID is a reminder id that cannot be one: empty, too long, or with
// control characters.
var ErrBadUID = errors.New("Erinnerung: keine gültige Kennung")

// maxUID caps a reminder's id; phones write 36 to a little over 100 bytes.
const maxUID = 512

// ReminderDone ticks the reminder with that UID off in Radicale, the widget's
// one row action (haus#10): it changes what the family's phones show, so it
// goes through here, checked, like every write of the Haushalt source.
func ReminderDone(ctx context.Context, c *caldav.Client, uid string) error {
	if uid == "" || len(uid) > maxUID || strings.IndexFunc(uid, unicode.IsControl) >= 0 || !utf8.ValidString(uid) {
		return ErrBadUID
	}
	return c.CompleteTodo(ctx, uid)
}

// Day is a date as the family reads it: "Do 15.10.2026".
func Day(t time.Time) string { return day(t) }

// DayTime is Day with the time: "Do 15.10.2026 18:00".
func DayTime(t time.Time) string { return dayTime(t) }

// BirthdayText shows JJJJ-MM-TT as TT.MM.JJJJ and --MM-TT as TT.MM.
func BirthdayText(b string) string { return birthdayDay(b) }
