package pages

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mdopp/haus/internal/cards"
	"github.com/mdopp/haus/internal/usage"
)

// What the family uses (haus#50, owner decision 2026-10-16): Haus counts
// per event and UTC day, never who — no person, no device, no address, no
// content — and the Box page shows the last 30 days to admins. The counts
// go to usage-metrics (internal/usage); this file names the events and
// says them in the admin's words.

// pagePaths are the pages a view is counted for, by path; the prefixes
// are an item's own page. Anything else (the font, the icon, /healthz, a
// path Haus does not serve) is no page view.
var pagePaths = map[string]string{"/start": "start", "/raeume": "raeume", "/energie": "energie", "/medien": "medien",
	"/kalender": "kalender", "/notizen": "notizen", "/sprechen": "sprechen", "/box": "box", "/box/geraete": "geraete",
	"/pair-device": "koppeln", cards.SwitchPath: "schalten", "/mehr": "mehr"}

// fragmentPaths are parts of a page fetched after it (haus#67): the plain
// lines they show count for their page, a view does not.
var fragmentPaths = map[string]string{StartBoxPath: "start"}

var pagePrefixes = []struct{ prefix, page string }{
	{cards.ReminderPages, "erinnerung"}, {cards.ThingPages, "ding"}, {cards.ContactPages, "kontakt"},
	{cards.DocumentPages, "dokument"}, {cards.NotePages, "notiz"}}

func pageOf(path string) string {
	if p, ok := pagePaths[path]; ok {
		return p
	}
	for _, p := range pagePrefixes {
		if len(path) > len(p.prefix) && strings.HasPrefix(path, p.prefix) {
			return p.page
		}
	}
	return ""
}

// degradedLines are the plain lines a page says when something does not
// answer (family.go, haus#33), by the name a count carries. Longest first:
// Schalten's line holds Licht und Geräte's, and counts once, as its own.
var degradedLines = func() []struct{ key, says string } {
	ls := []struct{ key, says string }{
		{"schalten", saysSwitchFailed}, {"licht", saysNoHouse}, {"raeume", saysNoRooms}, {"kalender", saysNoCalendar},
		{"kontakte", saysNoContacts}, {"notizen", saysNoNotes}, {"bedeutung", saysWordsOnly}, {"dokumente", saysNoDocuments},
		{"favoriten", saysNoFavorites}, {"favoriten-verdeckt", saysFavsUnseen}, {"strom", saysNoEnergy}, {"strom-heute", saysNoEnergyToday}, {"verlauf", saysNoEnergyHistory},
		{"lautsprecher", saysNoPlayers}, {"bote", saysDown("")}, {"bote-langsam", saysSlow("")},
		{"box", "Wie es der Box geht, lässt sich gerade nicht sagen."}, {"laib", "Laib antwortet nicht"},
		{"github", "GitHub ist gerade nicht erreichbar."}, {"dienste", " gerade nicht erreichbar."},
	}
	sort.SliceStable(ls, func(i, j int) bool { return len(ls[i].says) > len(ls[j].says) })
	return ls
}()

// lineSays is what the card quotes for a line's name.
func lineSays(key string) string {
	switch key {
	case "bote":
		return strings.TrimSpace(saysDown(House.Display))
	case "bote-langsam":
		return strings.TrimSpace(saysSlow(House.Display))
	case "dienste":
		return "Ein Dienst ist gerade nicht erreichbar."
	}
	for _, l := range degradedLines {
		if l.key == key {
			return l.says
		}
	}
	return ""
}

// widgetKinds are the Haus app's widgets whose tap opens a page with
// ?von=widget.<kind> (haus-android#181), in the admin's words; any other
// value is no source tag and counts nothing.
var widgetKinds = map[string]string{"device": "Gerät", "devices": "Geräte", "active": "Was ist an", "energy": "Energie jetzt",
	"energy-stats": "Energie-Verlauf", "energy-totals": "Energie-Summen", "box-overview": "Box", "box-services": "Dienste", "tool": "Werkzeug"}

// Counted counts the pages next serves: a view per page shown (GET, 200),
// the plain lines it showed, the Notizen search (never its words), and a
// widget's tap by its kind. The response is read as it is written, never
// waited for; nil counts nothing.
func Counted(c *usage.Counter, next http.Handler) http.Handler {
	if c == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := pageOf(r.URL.Path)
		fragment := false
		if page == "" {
			page, fragment = fragmentPaths[r.URL.Path], true
		}
		if page == "" {
			next.ServeHTTP(w, r)
			return
		}
		rec := &seenPage{ResponseWriter: w, code: http.StatusOK}
		next.ServeHTTP(rec, r)
		if rec.code == http.StatusNotFound {
			return
		}
		if r.Method == http.MethodGet && !fragment {
			if rec.code == http.StatusOK && page != "schalten" {
				c.Count("seite." + page)
				if page == "notizen" && strings.TrimSpace(r.URL.Query().Get("q")) != "" {
					c.Count("suche.notizen")
				}
			}
			if kind, ok := strings.CutPrefix(r.URL.Query().Get("von"), "widget."); ok && widgetKinds[kind] != "" {
				c.Count("von.widget." + kind)
			}
		}
		body := withoutScripts(rec.body.String()) // a script's words are not lines the page shows
		for _, l := range degradedLines {
			if strings.Contains(body, l.says) {
				c.Count("gestoert." + page + "." + l.key)
				body = strings.ReplaceAll(body, l.says, "")
			}
		}
	})
}

// withoutScripts is a page without its scripts.
func withoutScripts(s string) string {
	for {
		i := strings.Index(s, "<script")
		if i < 0 {
			return s
		}
		j := strings.Index(s[i:], "</script>")
		if j < 0 {
			return s[:i]
		}
		s = s[:i] + s[i+j+len("</script>"):]
	}
}

// seenPage passes a page through and keeps what it said, up to a page's size.
type seenPage struct {
	http.ResponseWriter
	code int
	body bytes.Buffer
}

func (s *seenPage) WriteHeader(code int) {
	s.code = code
	s.ResponseWriter.WriteHeader(code)
}

// Unwrap lets a streamed answer flush through (http.ResponseController).
func (s *seenPage) Unwrap() http.ResponseWriter { return s.ResponseWriter }

func (s *seenPage) Write(p []byte) (int, error) {
	if s.body.Len() < 1<<20 {
		s.body.Write(p)
	}
	return s.ResponseWriter.Write(p)
}

// feature is something of Haus the card can call unused: its event, what
// it is in the admin's words, and whether this box has it at all.
type feature struct {
	event, says string
	has         func(StartConfig) bool
}

func always(StartConfig) bool          { return true }
func withHouse(c StartConfig) bool     { return c.HA != nil }
func withCalendar(c StartConfig) bool  { return c.Calendar != nil }
func withNotes(c StartConfig) bool     { return c.Notes != nil }
func withBote(c StartConfig) bool      { return c.Bote != nil }
func withFavorites(c StartConfig) bool { return c.HA != nil && c.Favorites != nil }

var features = []feature{
	{"seite.start", "Start geöffnet", always},
	{"seite.raeume", "Räume geöffnet", withHouse},
	{"seite.ding", "Ein Gerät geöffnet", withHouse},
	{"seite.energie", "Energie geöffnet", withHouse},
	{"seite.medien", "Medien geöffnet", withHouse},
	{"seite.kalender", "Kalender geöffnet", withCalendar},
	{"seite.notizen", "Notizen geöffnet", withNotes},
	{"suche.notizen", "In den Notizen gesucht", withNotes},
	{"sprechen.senden", "Im Sprechen-Feld geschrieben", withBote},
	{"schalten.alle-lichter-aus", "Alle Lichter ausgeschaltet", withHouse},
	{"stern.an", "Ein Gerät auf Start geheftet (☆)", withFavorites},
}

// says is an event in the admin's words; "" for one the card does not show.
var says = map[string]string{
	"seite.erinnerung": "Eine Erinnerung geöffnet", "seite.kontakt": "Einen Kontakt geöffnet", "seite.dokument": "Ein Dokument geöffnet",
	"seite.notiz": "Eine Notiz geöffnet", "seite.sprechen": "Sprechen geöffnet", "seite.box": "Box geöffnet", "seite.geraete": "Box · Geräte geöffnet",
	"seite.koppeln": "Ein Telefon gekoppelt", "seite.mehr": "Mehr geöffnet",
	"sprechen.mikrofon":      "Ins Mikrofon gesprochen",
	"schalten.light.turn_on": "Licht eingeschaltet", "schalten.light.turn_off": "Licht ausgeschaltet",
	"schalten.switch.turn_on": "Schalter eingeschaltet", "schalten.switch.turn_off": "Schalter ausgeschaltet",
	"schalten.media_player.media_pause": "Musik oder Film angehalten", "schalten.media_player.media_play": "Musik oder Film weiterlaufen lassen",
	"schalten.media_player.volume_up": "Lauter gestellt", "schalten.media_player.volume_down": "Leiser gestellt",
	"schalten.light.dimmen":     "Licht gedimmt",
	"schalten.cover.open_cover": "Rollladen hochgefahren", "schalten.cover.close_cover": "Rollladen heruntergefahren",
	"schalten.cover.stop_cover": "Rollladen angehalten", "schalten.cover.set_cover_position": "Rollladen auf eine Position gestellt",
	"stern.aus":         "Ein Gerät von Start genommen (☆)",
	"napi.portal.state": "Geräte-Widget aktualisiert", "napi.portal.active": "Widget „Was ist an“ aktualisiert",
	"napi.portal.energy": "Energie-Widget aktualisiert", "napi.portal.entity-history": "Verlauf im Widget angesehen",
	"napi.portal.camera.snapshot": "Kamera-Widget aktualisiert", "napi.portal.events": "Widgets live verbunden",
	"napi.portal.tasks": "Aufgaben-Widget aktualisiert", "napi.portal.contacts": "Kontakte-Widget aktualisiert",
	"napi.portal.documents": "Dokumente-Widget aktualisiert", "napi.portal.notes": "Notizen-Widget aktualisiert",
	"napi.box.home": "Box-Widget aktualisiert", "napi.box.services": "Dienste-Widget aktualisiert",
	"napi.box.approvals": "Freigaben-Widget aktualisiert", "napi.box.upgrades": "Updates-Widget aktualisiert",
	"napi.ha.call": "Aus einem Widget geschaltet", "napi.action-callback": "In einem Widget etwas erledigt",
	"napi.upload": "Ein Foto ins Archiv geschickt",
}

func init() {
	for _, f := range features {
		says[f.event] = f.says
	}
}

// pageIn is where a page is, for "… auf Start".
var pageIn = map[string]string{"start": "auf Start", "raeume": "in Räume", "ding": "auf der Seite eines Geräts", "energie": "in Energie",
	"medien": "in Medien", "kalender": "im Kalender", "erinnerung": "bei einer Erinnerung", "kontakt": "bei einem Kontakt",
	"dokument": "bei einem Dokument", "notizen": "in Notizen", "notiz": "bei einer Notiz", "sprechen": "im Sprechen-Feld",
	"schalten": "beim Schalten", "mehr": "in Mehr", "box": "auf der Box-Seite", "geraete": "bei den Geräten", "koppeln": "beim Koppeln"}

// eventSays is a Haus event in words, "" when the card does not show it.
func eventSays(event string) string {
	if s := says[event]; s != "" {
		return s
	}
	if kind, ok := strings.CutPrefix(event, "von.widget."); ok && widgetKinds[kind] != "" {
		return "Vom Widget „" + widgetKinds[kind] + "“ aus geöffnet"
	}
	return ""
}

// toolWords are the Bote's tools in the admin's words, never their names:
// laib's plugin counts werkzeug.<server>.<tool> from Hermes'
// mcp__<server>__<tool>, and Hermes' own tools without a server (laib#275).
// HA's Assist intents arrive lower-case with their leading hass, laib's
// verbs with their laib_.
var toolWords = map[string]string{
	"haushalt.termine": "Termine nachgesehen", "haushalt.termin_anlegen": "Termin eingetragen",
	"haushalt.erinnerungen": "Erinnerungen nachgesehen", "haushalt.erinnerung_anlegen": "Erinnerung angelegt",
	"haushalt.notiz_anlegen": "Notiz angelegt", "haushalt.notiz_lesen": "Notiz gelesen",
	"haushalt.notizen_suchen": "In den Notizen gesucht", "haushalt.notizen_finden": "In den Notizen gesucht",
	"haushalt.dokumente_suchen": "Dokumente gesucht", "haushalt.kontakte_suchen": "Kontakte gesucht",
	"haushalt.kontakt_lesen": "Kontakt nachgesehen", "haushalt.kontakt_anlegen": "Kontakt angelegt",
	"haushalt.kontakt_ergaenzen": "Firma ins Adressbuch eingetragen",
	"ha.hassturnon":              "Geräte eingeschaltet", "ha.hassturnoff": "Geräte ausgeschaltet", "ha.hasstoggle": "Geräte umgeschaltet",
	"ha.hasslightset": "Licht eingestellt", "ha.hasssetposition": "Rollläden bewegt", "ha.hasssetvolume": "Lautstärke geändert",
	"ha.hassmediapause": "Musik angehalten", "ha.hassmediaunpause": "Musik weitergespielt", "ha.hassmedianext": "Nächstes Lied",
	"ha.hassclimatesettemperature": "Heizung eingestellt", "ha.hassclimategettemperature": "Temperatur nachgesehen",
	"ha.hassgetstate": "Nachgesehen, was im Haus an ist", "ha.getlivecontext": "Nachgesehen, was im Haus an ist",
	"ha.hassstarttimer": "Timer gestellt",
	"laib.status":       "Wie es der Box geht", "laib.inbox": "Offene Vorschläge nachgesehen", "laib.journal": "Im Box-Tagebuch nachgesehen",
	"laib.propose": "Der Box etwas vorgeschlagen", "laib.note": "Der Box etwas notiert", "laib.escalate": "Claude gefragt",
	"laib.wish": "Einen Wunsch notiert", "laib.diag": "Die Box geprüft", "laib.logs": "In die Protokolle geschaut",
	"frage.frage":        "Im Netz gesucht",
	"automat.entitaeten": "Geräte für Automationen nachgesehen", "automat.automationen": "Automationen nachgesehen",
	"automat.automation_lesen": "Automation gelesen", "automat.automation_schreiben": "Automation geschrieben",
	"automat.automation_loeschen": "Automation gelöscht", "automat.automation_schalten": "Automation an- oder ausgeschaltet",
	"memory": "Gedächtnis", "todo": "Aufgabenliste", "session_search": "Frühere Gespräche durchsucht", "clarify": "Zurückgefragt",
}

// otherOf is a server's tool that toolWords does not know yet: still no name.
var otherOf = map[string]string{"haushalt": "Sonst etwas im Haushalt", "ha": "Sonst etwas an den Geräten", "laib": "Sonst etwas an der Box",
	"frage": "Im Netz gesucht", "automat": "Sonst etwas an den Automationen"}

// toolSays is a Bote tool as the admin reads it: what was done, in German
// words, never the tool's name.
func toolSays(tool string) string {
	server, name, _ := strings.Cut(tool, ".")
	if server == "laib" {
		tool = "laib." + strings.TrimPrefix(name, "laib_")
	}
	if s := toolWords[tool]; s != "" {
		return s
	}
	if s := otherOf[server]; s != "" && name != "" {
		return s
	}
	return "Ein anderes Werkzeug"
}

const usageDays = 30

// usageCard is the Box page's "Was wird genutzt" (haus#50): the last 30
// days in plain words — what is used most, what hardly or never, which
// plain lines the family saw, the Bote's and the voice's numbers, and what
// nobody could answer. Without usage-metrics configured, no card.
func usageCard(ctx context.Context, family StartConfig, voice *usage.Voice) string {
	c := family.Usage
	if c == nil {
		return ""
	}
	var sum usage.Summary
	var miss usage.MissList
	var sumErr, missErr error
	fetchCtx(ctx, func(ctx context.Context) { sum, sumErr = c.Summary(ctx, usageDays) },
		func(ctx context.Context) { miss, missErr = c.Misses(ctx, usageDays) })
	logErrs("box", map[string]error{"usage summary": sumErr, "usage misses": missErr})
	var b strings.Builder
	b.WriteString(usageStyle + "<section class=genutzt aria-labelledby=genutzt><h2 id=genutzt>Was wird genutzt</h2>")
	if sumErr != nil {
		b.WriteString("<p class=leise>Die Nutzungszahlen lassen sich gerade nicht lesen.</p></section>")
		return b.String()
	}
	b.WriteString("<p class=leise>Die letzten 30 Tage. Gezählt wird nur, wie oft etwas benutzt wird — nie, wer.</p>")
	if voice.Refused() {
		b.WriteString("<p class=achtung>Sprachbefehle werden nicht gezählt: Home Assistant zeigt Haus die Sprachverläufe nicht. " +
			"Dafür braucht der Benutzer hinter Haus' Home-Assistant-Token Administrator-Rechte.</p>")
	}
	if sum.Total == 0 && len(miss.Misses) == 0 {
		fmt.Fprintf(&b, "<p>Noch nichts gezählt. Haus zählt seit %s Uhr; was die Familie tut, steht nach ein paar Minuten hier.</p></section>",
			c.Started.In(familyLoc(family)).Format("02.01. um 15:04"))
		return b.String()
	}
	haus := sum.Events(usage.App)
	b.WriteString(mostUsed(haus))
	b.WriteString(leastUsed(haus, family))
	b.WriteString(degradedSeen(haus))
	b.WriteString(boteAndVoice(sum))
	if missErr == nil {
		b.WriteString(unanswered(miss))
	} else {
		b.WriteString("<h3>Was niemand beantworten konnte</h3><p class=leise>Die Sätze lassen sich gerade nicht lesen.</p>")
	}
	b.WriteString("</section>")
	return b.String()
}

func familyLoc(c StartConfig) *time.Location {
	_, loc := c.clock()
	return loc
}

type row struct {
	says string
	n    int64
}

func rows(b *strings.Builder, rs []row) {
	b.WriteString("<ul class=zeilen>")
	for _, r := range rs {
		fmt.Fprintf(b, "<li><span class=was>%s</span><span class=wieoft>%d×</span></li>", html.EscapeString(r.says), r.n)
	}
	b.WriteString("</ul>")
}

// mostUsed is the five most counted things the card has words for.
func mostUsed(haus map[string]int64) string {
	var rs []row
	for e, n := range haus {
		if s := eventSays(e); s != "" && n > 0 {
			rs = append(rs, row{s, n})
		}
	}
	if len(rs) == 0 {
		return ""
	}
	sort.Slice(rs, func(i, j int) bool { return rs[i].n > rs[j].n || rs[i].n == rs[j].n && rs[i].says < rs[j].says })
	if len(rs) > 5 {
		rs = rs[:5]
	}
	var b strings.Builder
	b.WriteString("<h3>Am meisten genutzt</h3>")
	rows(&b, rs)
	return b.String()
}

// leastUsed is what this box has and nobody or hardly anybody used: the
// features with the fewest counts, at most five, at most 2 each.
func leastUsed(haus map[string]int64, family StartConfig) string {
	var rs []row
	for _, f := range features {
		if f.has(family) && haus[f.event] <= 2 {
			rs = append(rs, row{f.says, haus[f.event]})
		}
	}
	if len(rs) == 0 {
		return ""
	}
	sort.SliceStable(rs, func(i, j int) bool { return rs[i].n < rs[j].n })
	if len(rs) > 5 {
		rs = rs[:5]
	}
	var b strings.Builder
	b.WriteString("<h3>Kaum oder nie genutzt</h3><ul class=zeilen>")
	for _, r := range rs {
		n := "nie"
		if r.n > 0 {
			n = strconv.FormatInt(r.n, 10) + "×"
		}
		fmt.Fprintf(&b, "<li><span class=was>%s</span><span class=wieoft>%s</span></li>", html.EscapeString(r.says), n)
	}
	b.WriteString("</ul>")
	return b.String()
}

// degradedSeen is which plain line the family saw where, and how often.
func degradedSeen(haus map[string]int64) string {
	var rs []row
	for e, n := range haus {
		rest, ok := strings.CutPrefix(e, "gestoert.")
		if !ok {
			continue
		}
		page, line, _ := strings.Cut(rest, ".")
		if s := lineSays(line); s != "" && pageIn[page] != "" {
			rs = append(rs, row{s + "\x00" + pageIn[page], n})
		}
	}
	if len(rs) == 0 {
		return ""
	}
	sort.Slice(rs, func(i, j int) bool { return rs[i].n > rs[j].n || rs[i].n == rs[j].n && rs[i].says < rs[j].says })
	var b strings.Builder
	b.WriteString("<h3>Was nicht antwortete</h3><ul class=zeilen>")
	for _, r := range rs {
		line, where, _ := strings.Cut(r.says, "\x00")
		fmt.Fprintf(&b, "<li class=gestoert><span class=was>„%s“ <span class=wo>%s</span></span><span class=wieoft>%d×</span></li>", html.EscapeString(line), where, r.n)
	}
	b.WriteString("</ul>")
	return b.String()
}

// boteAndVoice is the Bote's and the voice control's numbers in a line each.
func boteAndVoice(sum usage.Summary) string {
	bote, voice := sum.Events("bote"), sum.Events(usage.VoiceApp)
	var b strings.Builder
	if bote["zug"] > 0 {
		fmt.Fprintf(&b, "<p><b>Im Chat:</b> %d Anfragen, %d davon nicht beantwortet.", bote["zug"], bote["unbeantwortet"])
		var tools []row
		for e, n := range bote {
			if t, ok := strings.CutPrefix(e, "werkzeug."); ok {
				tools = append(tools, row{toolSays(t), n})
			}
		}
		sort.Slice(tools, func(i, j int) bool {
			return tools[i].n > tools[j].n || tools[i].n == tools[j].n && tools[i].says < tools[j].says
		})
		if len(tools) > 3 {
			tools = tools[:3]
		}
		if len(tools) > 0 {
			b.WriteString(" Am meisten gebraucht: ")
			for i, t := range tools {
				if i > 0 {
					b.WriteString(", ")
				}
				fmt.Fprintf(&b, "%s (%d)", html.EscapeString(t.says), t.n)
			}
			b.WriteString(".")
		}
		b.WriteString("</p>")
	}
	if total := voice["erkannt"] + voice["weitergereicht"] + voice["nicht-erkannt"]; total > 0 {
		fmt.Fprintf(&b, "<p><b>Per Stimme:</b> %d Sätze. %d hat Home Assistant selbst verstanden, %d an die KI weitergereicht, %d hat niemand verstanden.</p>",
			total, voice["erkannt"], voice["weitergereicht"], voice["nicht-erkannt"])
	}
	if b.Len() == 0 {
		return ""
	}
	return "<h3>Chat und Stimme</h3>" + b.String()
}

// unanswered is the sentences nobody could answer, as they were said.
func unanswered(m usage.MissList) string {
	if len(m.Misses) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<h3>Was niemand beantworten konnte</h3><p class=leise>So gesagt, wie es ankam. Die Sätze bleiben 30 Tage, ohne Namen.</p><ul class=zeilen>")
	for i, x := range m.Misses {
		if i == 10 {
			break
		}
		where := map[string]string{"bote": "im Chat", usage.VoiceApp: "per Stimme"}[x.Source]
		if where == "" {
			continue
		}
		fmt.Fprintf(&b, "<li><span class=was>„%s“ <span class=wo>%s</span></span><span class=wieoft>%d×</span></li>", html.EscapeString(x.Text), where, x.Count)
	}
	b.WriteString("</ul>")
	return b.String()
}

// fetchCtx is fetch for a context without a request.
func fetchCtx(ctx context.Context, reads ...func(context.Context)) {
	fetch((&http.Request{}).WithContext(ctx), reads...)
}

// usageStyle is the card's look: the house palette's light tokens (the
// family frame, haus#35) on the Box page — amber for what did not answer.
// A white section sets every token its content paints with, the
// surfaces too (--card, --panel, --bg): Box's .zeilen inside it is white
// with dark ink in either theme, not the page's dark card (haus#194).
const usageStyle = `<style>.genutzt{--ink:#0f172a;--muted:#64748b;--faint:#94a3b8;--edge:rgba(15,23,42,.12);--line:rgba(15,23,42,.08);--accent:#ea580c;--accent-soft:rgba(234,88,12,.12);--warn:#b45309;--warn-soft:rgba(180,83,9,.10);
--danger:#dc2626;--card:#fff;--panel:#fff;--bg:#f1f5f9;color-scheme:light;
margin:1.4rem 0;padding:16px;background:var(--card);color:var(--ink);border:1px solid var(--edge);border-radius:14px;font-family:Geist,system-ui,sans-serif}
.genutzt h2{margin:0 0 4px;color:var(--ink);text-transform:none;letter-spacing:0;font-size:1.1rem}
.genutzt h3{margin:18px 0 6px;font-size:.72rem;font-weight:600;letter-spacing:.1em;text-transform:uppercase;color:var(--muted)}
.genutzt p{margin:6px 0}.genutzt .leise{color:var(--muted);font-size:.85rem}
.genutzt .achtung{padding:10px 12px;background:var(--warn-soft);border:1px solid var(--warn);border-radius:10px}
.genutzt .zeilen{list-style:none;margin:0;padding:0}
.genutzt .zeilen li{display:flex;gap:12px;justify-content:space-between;align-items:baseline;padding:8px 12px;border-bottom:1px solid var(--line)}
.genutzt .zeilen li:last-child{border-bottom:0}
.genutzt .was{overflow-wrap:anywhere}.genutzt .wo{color:var(--muted);font-size:.85rem;white-space:nowrap}
.genutzt .wieoft{font-variant-numeric:tabular-nums;font-weight:600;white-space:nowrap}
.genutzt .gestoert .was::before{content:"";display:inline-block;width:8px;height:8px;border-radius:50%;background:var(--warn);margin-right:8px;vertical-align:1px}</style>`
