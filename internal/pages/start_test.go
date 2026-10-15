package pages

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/haus/internal/caldav"
	"github.com/mdopp/haus/internal/caldav/davfake"
	"github.com/mdopp/haus/internal/cards"
	"github.com/mdopp/haus/internal/devices"
	"github.com/mdopp/haus/internal/favoriten"
	"github.com/mdopp/haus/internal/ha"
	"github.com/mdopp/haus/internal/ha/hafake"
	"github.com/mdopp/haus/internal/laib"
	"github.com/mdopp/haus/internal/notes"
	"github.com/mdopp/haus/internal/persona"
)

func berlin(t *testing.T) *time.Location {
	t.Helper()
	l, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// a Wednesday morning in Berlin
func startNow(t *testing.T) time.Time { return time.Date(2026, 10, 14, 10, 0, 0, 0, berlin(t)) }

// allGreen is a box where everything runs, last backed up yesterday
// evening by Start's test clock.
func allGreen(t *testing.T) map[string]any {
	return readyAt(t, time.Date(2026, 10, 13, 21, 31, 0, 0, berlin(t)), "läuft")
}

// readyAt is a box whose last backup was at (zero: none yet), with the
// Operator in that state.
func readyAt(t *testing.T, at time.Time, operator string) map[string]any {
	backup := map[string]any{"ok": false, "repos": 0}
	if !at.IsZero() {
		backup = map[string]any{"at": at.UTC().Format(time.RFC3339), "repos": 2, "ok": true}
	}
	return map[string]any{"state": "bereit", "services": 1, "green": 1, "operator": operator, "lastBackup": backup,
		"lastChange": map[string]any{"summary": "Paperless wird installiert", "at": at.UTC().Format(time.RFC3339), "reversible": true}}
}

func startPage(t *testing.T, cfg StartConfig, groups string) (int, string) {
	t.Helper()
	return startPageAs(t, cfg, "anna", groups)
}

// startPageAs is Start for one person (Remote-User; "" for nobody).
func startPageAs(t *testing.T, cfg StartConfig, user, groups string) (int, string) {
	t.Helper()
	if cfg.Loc == nil {
		cfg.Loc = berlin(t)
	}
	if cfg.Now == nil {
		now := startNow(t)
		cfg.Now = func() time.Time { return now }
	}
	req := httptest.NewRequest("GET", "/start", nil)
	if user != "" {
		req.Header.Set("Remote-User", user)
	}
	if groups != "" {
		req.Header.Set("Remote-Groups", groups)
	}
	rec := httptest.NewRecorder()
	Start(cfg).ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func house(t *testing.T) (*hafake.HA, *ha.Client) {
	t.Helper()
	fake := hafake.New("ha-token")
	t.Cleanup(fake.Close)
	return fake, &ha.Client{URL: fake.URL, Token: "ha-token"}
}

func calendar(t *testing.T) (*davfake.Server, *caldav.Client) {
	t.Helper()
	fake := davfake.New("haus", "pw")
	t.Cleanup(fake.Close)
	fake.AddCalendar("/haus/familie/", "Haushalt")
	return fake, &caldav.Client{URL: fake.URL, User: "haus", Password: "pw", Loc: berlin(t)}
}

func event(uid, start, end, summary string) string {
	return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:" + uid + "\r\nDTSTART:" + start + "\r\nDTEND:" + end + "\r\nSUMMARY:" + summary + "\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
}

func todo(uid, summary, extra string) string {
	return "BEGIN:VCALENDAR\r\nBEGIN:VTODO\r\nUID:" + uid + "\r\nSUMMARY:" + summary + "\r\n" + extra + "END:VTODO\r\nEND:VCALENDAR\r\n"
}

func notesDir(t *testing.T, ns map[string]time.Duration) *notes.Dir {
	t.Helper()
	dir := t.TempDir()
	for title, age := range ns {
		p := filepath.Join(dir, notes.Slug(title)+".md")
		if err := os.WriteFile(p, []byte("# "+title+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		at := time.Now().Add(-age)
		if err := os.Chtimes(p, at, at); err != nil {
			t.Fatal(err)
		}
	}
	d, err := notes.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func mustContain(t *testing.T, body string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(body, w) {
			t.Errorf("start page lacks %q:\n%s", w, body)
		}
	}
}

func mustNotContain(t *testing.T, body string, nots ...string) {
	t.Helper()
	for _, n := range nots {
		if strings.Contains(body, n) {
			t.Errorf("start page shows %q:\n%s", n, body)
		}
	}
}

// section is the part of body from the card titled id to the next card,
// or to the end.
func section(t *testing.T, body, id string) string {
	t.Helper()
	i := strings.Index(body, "<h2 id="+id+">")
	if i < 0 {
		t.Fatalf("no %s on the page:\n%s", id, body)
	}
	rest := body[i:]
	if j := strings.Index(rest, "</section>"); j >= 0 {
		rest = rest[:j]
	}
	return rest
}

// a family evening (haus#35's render): Anna and Ben home, two lamps on,
// music playing, four things pinned, three things coming up
func familyEvening(t *testing.T) StartConfig {
	t.Helper()
	fake, hc := house(t)
	fake.Set("person.anna", "home", map[string]any{"friendly_name": "Anna"})
	fake.Set("person.ben", "home", map[string]any{"friendly_name": "Ben"})
	fake.Set("person.carla", "not_home", map[string]any{"friendly_name": "Carla"})
	fake.Set("light.buero", "on", map[string]any{"friendly_name": "Bürolicht", "brightness": 128.0})
	fake.Set("light.kueche", "on", map[string]any{"friendly_name": "Küche"})
	fake.Set("light.flur", "off", map[string]any{"friendly_name": "Flurlicht"})
	fake.Set("cover.wohnzimmer", "open", map[string]any{"friendly_name": "Rollladen Wohnzimmer", "current_position": 40.0})
	fake.Set("climate.bad", "heat", map[string]any{"friendly_name": "Heizung Bad", "current_temperature": 21.5, "temperature": 23.0})
	fake.Set("media_player.wohnzimmer", "playing", map[string]any{"friendly_name": "Wohnzimmer", "media_title": "Abbey Road",
		"media_artist": "The Beatles", "supported_features": 152461.0})
	fake.Set("media_player.kueche", "paused", map[string]any{"friendly_name": "Küchenradio", "media_title": "Deutschlandfunk", "supported_features": 1.0})
	dav, cal := calendar(t)
	dav.Put("/haus/familie/1.ics", event("1", "20261013T080000Z", "20261013T090000Z", "Gestern vorbei"))
	dav.Put("/haus/familie/2.ics", eventAt("2", "20261015T063000Z", "20261015T073000Z", "Zahnarzt", "Ben"))
	dav.Put("/haus/familie/m.ics", todo("muell", "Müll rausbringen", "DUE:20261014T170000Z\r\n"))
	dav.Put("/haus/familie/o.ics", todo("oma", "Geschenk für Oma", "DUE;VALUE=DATE:20261017\r\n"))
	dav.Put("/haus/familie/e.ics", todo("e", "Erledigt schon", "STATUS:COMPLETED\r\nDUE:20261014T070000Z\r\n"))
	return StartConfig{Laib: fakeLaib(t, allGreen(t), []map[string]any{{"name": "immich", "state": "grün"}}), LaibURL: "https://laib.box.test",
		HA: hc, Calendar: cal, Notes: notesDir(t, map[string]time.Duration{"Einkaufsliste": time.Hour}),
		Favorites: favorites(t, `{"anna": ["light.buero", "cover.wohnzimmer", "climate.bad", "light.flur"], "ben": ["light.kueche"]}`)}
}

// haus#35: Start is "Zuhause" and the day — no persona's greeting, name,
// icon or colours, and no admin words on top. Then Im Haus, Meine Geräte,
// Heute und demnächst, and the box in one calm line at the bottom.
func TestStartIsZuhause(t *testing.T) {
	House = persona.Persona{Display: "Joshua", Greeting: map[string]string{"de": "Wollen wir ein Spiel spielen?"},
		Colors: map[string]string{"accent": "#3ddc84", "ground": "#0c120e"}, Icon: []byte("<svg/>")}
	defer func() { House = persona.Default() }()
	code, body := startPage(t, familyEvening(t), "family")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	mustContain(t, body, "<title>Zuhause</title>", "<h1>Zuhause</h1>", "Mittwoch, 14. Oktober", `lang=de`,
		"<h2 id=karte-im-haus>Im Haus</h2>", "<h2 id=karte-meine-geraete>Meine Geräte</h2>",
		"<h2 id=karte-heute-und-demnaechst>Heute und demnächst</h2>",
		"Alles in Ordnung · eure Daten sind von gestern Abend gesichert")
	mustNotContain(t, body, "Joshua", "Wollen wir", "#3ddc84", "#0c120e", "/persona/icon.svg",
		"Alles läuft", "Letzte Sicherung", "Orten", "Letzte Änderung", "Paperless wird installiert", "Operator", "Laib",
		"Haushalt", "Jetzt</h2>", "Favoriten</h2>", "Einkaufsliste", "<script", "fonts.googleapis", "fonts.gstatic")
	before(t, body, "<h1>Zuhause</h1>", "karte-im-haus")
	before(t, body, "karte-im-haus", "karte-meine-geraete")
	before(t, body, "karte-meine-geraete", "karte-heute-und-demnaechst")
	before(t, body, "karte-heute-und-demnaechst", "Alles in Ordnung")
}

// haus#35: Im Haus says who is home, what is on — each with its one
// action — and what plays. The appointments are not "now".
func TestStartImHaus(t *testing.T) {
	_, body := startPage(t, familyEvening(t), "family")
	im := section(t, body, "karte-im-haus")
	mustContain(t, im, "Anna und Ben sind da",
		"2 Lichter an", "Bürolicht, Küche",
		`<form method=post action=/schalten>`, `name=tun value="alle-lichter-aus"`, `>Alle aus</button>`,
		"Wohnzimmer spielt", "Abbey Road · The Beatles",
		`name=geraet value="media_player.wohnzimmer"`, `name=tun value="media_player.media_pause"`, `>Pause</button>`,
		`name=zurueck value="/start"`)
	mustNotContain(t, im, "Carla", "Küchenradio", "Deutschlandfunk", "Zahnarzt", "Flurlicht")
	before(t, im, "sind da", "Lichter an")
	before(t, im, "Lichter an", "Wohnzimmer spielt")
}

// One lamp on is switched off by name; none on says so, with nothing to
// press; a player that cannot pause gets no Pause.
func TestStartImHausSaysItPlainly(t *testing.T) {
	fake, hc := house(t)
	fake.Set("person.anna", "not_home", map[string]any{"friendly_name": "Anna"})
	fake.Set("light.flur", "off", map[string]any{"friendly_name": "Flur"})
	fake.Set("media_player.wohnzimmer", "idle", map[string]any{"friendly_name": "Wohnzimmer"})
	_, body := startPage(t, StartConfig{HA: hc}, "")
	mustContain(t, body, "Niemand ist zu Hause", "Alle Lichter sind aus")
	mustNotContain(t, body, "Wohnzimmer", "spielt", "<form")

	fake2, hc2 := house(t)
	fake2.Set("person.anna", "home", map[string]any{"friendly_name": "Anna"})
	fake2.Set("light.flur", "on", map[string]any{"friendly_name": "Flur"})
	fake2.Set("media_player.radio", "playing", map[string]any{"friendly_name": "Radio", "media_title": "WDR 2", "supported_features": 4.0})
	_, body = startPage(t, StartConfig{HA: hc2}, "")
	mustContain(t, body, "Anna ist da", "1 Licht an", "Flur",
		`name=geraet value="light.flur"`, `name=tun value="light.turn_off"`, `>Aus</button>`, "Radio spielt", "WDR 2")
	mustNotContain(t, body, "alle-lichter-aus", ">Pause</button>", "Niemand")

	fake3, hc3 := house(t)
	for _, p := range []string{"Anna", "Ben", "Carla"} {
		fake3.Set("person."+strings.ToLower(p), "home", map[string]any{"friendly_name": p})
	}
	_, body = startPage(t, StartConfig{HA: hc3}, "")
	mustContain(t, body, "Anna, Ben und Carla sind da")

	// no person entities: nothing said about who is home
	fake4, hc4 := house(t)
	fake4.Set("light.flur", "off", map[string]any{"friendly_name": "Flur"})
	_, body = startPage(t, StartConfig{HA: hc4}, "")
	mustNotContain(t, body, "sind da", "ist da", "Niemand")
}

// Many lights on: the names are cut, the count is not.
func TestStartCutsLongLightLists(t *testing.T) {
	fake, hc := house(t)
	for _, n := range []string{"A", "B", "C", "D", "E", "F", "G"} {
		fake.Set("light."+strings.ToLower(n), "on", map[string]any{"friendly_name": "Licht " + n})
	}
	_, body := startPage(t, StartConfig{HA: hc}, "")
	mustContain(t, body, "7 Lichter an", "Licht A", "Licht E", "und 2 weitere", "alle-lichter-aus")
	mustNotContain(t, body, "Licht F", "Licht G")
}

// A group (an entity carrying an entity_id list, light groups included)
// is not a lamp: only real lamps are counted.
func TestStartCountsLampsNotGroups(t *testing.T) {
	fake, hc := house(t)
	fake.Set("light.kueche", "on", map[string]any{"friendly_name": "Küche"})
	fake.Set("light.erdgeschoss", "on", map[string]any{"friendly_name": "Erdgeschoss", "entity_id": []any{"light.kueche", "light.flur"}})
	fake.Set("light.flur", "off", map[string]any{"friendly_name": "Flur"})
	_, body := startPage(t, StartConfig{HA: hc}, "")
	mustContain(t, body, "1 Licht an", "Küche", `name=geraet value="light.kueche"`)
	mustNotContain(t, body, "2 Lichter an", "Erdgeschoss")
}

// What HA hides is not counted or named on Start: a hidden lamp is not on,
// a config LED is no light, a diagnostic player plays nothing.
func TestStartLeavesOutWhatHAHides(t *testing.T) {
	fake, hc := house(t)
	fake.SetRegistries(nil, nil, []map[string]any{
		{"entity_id": "light.alt", "hidden_by": "user"},
		{"entity_id": "light.status_led", "entity_category": "config"},
		{"entity_id": "media_player.diag", "entity_category": "diagnostic"},
	})
	fake.Set("light.kueche", "on", map[string]any{"friendly_name": "Küche"})
	fake.Set("light.alt", "on", map[string]any{"friendly_name": "Alte Lampe"})
	fake.Set("light.status_led", "on", map[string]any{"friendly_name": "Status-LED"})
	fake.Set("media_player.diag", "playing", map[string]any{"friendly_name": "Diagnoseplayer", "media_title": "Test", "supported_features": 1.0})
	_, body := startPage(t, StartConfig{HA: hc}, "")
	mustContain(t, body, "1 Licht an", "Küche")
	mustNotContain(t, body, "Alte Lampe", "Status-LED", "Diagnoseplayer", "Lichter an")
}

// haus#35: Meine Geräte — the person's things as tiles like the phone
// widget, in their order: the state in a word (colour is never the only
// sign), a lamp's or a switch's switch right there, each leading to its
// own page. A cover or a heating has no switch on Start: its page has more.
func TestStartMeineGeraete(t *testing.T) {
	_, body := startPage(t, familyEvening(t), "family")
	g := section(t, body, "karte-meine-geraete")
	mustContain(t, g,
		`<a href="/raeume/light.buero">Bürolicht</a>`, `<b class=zustand>An</b>`, "50 %",
		`name=geraet value="light.buero"><input type=hidden name=tun value="light.turn_off"><input type=hidden name=zurueck value="/start"><button class=schalter role=switch aria-checked=true aria-label="Bürolicht">`,
		`<a href="/raeume/cover.wohnzimmer">Rollladen Wohnzimmer</a>`, `<b class=zustand>Offen</b>`, "40 %",
		`<a href="/raeume/climate.bad">Heizung Bad</a>`, `<b class=zustand>21,5 °C</b>`, "→ 23 °C",
		`<a href="/raeume/light.flur">Flurlicht</a>`, `<b class=zustand>Aus</b>`,
		`name=geraet value="light.flur"><input type=hidden name=tun value="light.turn_on"><input type=hidden name=zurueck value="/start"><button class=schalter role=switch aria-checked=false aria-label="Flurlicht">`)
	mustNotContain(t, g, `value="cover.wohnzimmer"`, `value="climate.bad"`, "Küche")
	before(t, g, "Bürolicht", "Rollladen Wohnzimmer")
	before(t, g, "Rollladen Wohnzimmer", "Heizung Bad")
	before(t, g, "Heizung Bad", "Flurlicht")

	// Ben's are Ben's
	_, body = startPageAs(t, familyEvening(t), "ben", "family")
	g = section(t, body, "karte-meine-geraete")
	mustContain(t, g, `<a href="/raeume/light.kueche">Küche</a>`)
	mustNotContain(t, g, "Bürolicht", "Rollladen")
}

// Only what a thing's page shows becomes a tile: a thing of a card domain
// that HA knows and does not hide.
func TestStartMeineGeraeteOnlyWhatItsPageShows(t *testing.T) {
	fake, hc := house(t)
	fake.SetRegistries(nil, nil, []map[string]any{{"entity_id": "light.alt", "hidden_by": "user"}})
	fake.Set("light.buero", "on", map[string]any{"friendly_name": "Bürolicht"})
	fake.Set("light.alt", "on", map[string]any{"friendly_name": "Alte Lampe"})
	fake.Set("person.ben", "home", map[string]any{"friendly_name": "Ben"})
	_, body := startPage(t, StartConfig{HA: hc, Favorites: favorites(t, `{"anna": ["light.weg", "person.ben", "light.alt", "light.buero"]}`)}, "")
	g := section(t, body, "karte-meine-geraete")
	mustContain(t, g, "Bürolicht")
	mustNotContain(t, g, "light.weg", "/raeume/person.ben", "Alte Lampe")
}

// haus#35: an empty Meine Geräte says how it fills — never a blank. With
// nobody asking (no Remote-User) or no store, there is nothing to fill:
// no section.
func TestStartMeineGeraeteEmptySaysHow(t *testing.T) {
	fake, hc := house(t)
	fake.Set("light.buero", "on", map[string]any{"friendly_name": "Bürolicht"})
	for name, fav := range map[string]*favoriten.Store{
		"no file":        favorites(t, ""),
		"an empty list":  favorites(t, `{"anna": []}`),
		"someone else's": favorites(t, `{"ben": ["light.buero"]}`),
		"only unknowns":  favorites(t, `{"anna": ["light.weg"]}`),
	} {
		_, body := startPage(t, StartConfig{HA: hc, Favorites: fav}, "")
		g := section(t, body, "karte-meine-geraete")
		if !strings.Contains(g, "Tippe in Räume bei einem Gerät auf ☆, dann erscheint es hier.") || !strings.Contains(g, `href="/raeume"`) {
			t.Errorf("%s: Meine Geräte does not say how it fills:\n%s", name, g)
		}
	}
	for name, c := range map[string]struct {
		fav  *favoriten.Store
		user string
	}{"no store": {nil, "anna"}, "nobody": {favorites(t, `{"anna": ["light.buero"]}`), ""}} {
		_, body := startPageAs(t, StartConfig{HA: hc, Favorites: c.fav}, c.user, "")
		mustNotContain(t, body, "Meine Geräte", "☆", "Favoriten")
		_ = name
	}
	// the house not answering and nothing pinned: Im Haus says it; how to
	// pin, through a Räume that does not answer either, can wait
	fake.Down(true)
	_, body := startPage(t, StartConfig{HA: hc, Favorites: favorites(t, `{"ben": ["light.buero"]}`)}, "")
	mustContain(t, body, "Licht und Geräte antworten gerade nicht.")
	mustNotContain(t, body, "karte-meine-geraete", "Tippe in Räume", "Favoriten")
	// favorites without the house configured: nothing to draw them from, nothing said
	for _, content := range []string{`{"anna": ["light.buero"]}`, `{"anna": [`} {
		_, body := startPage(t, StartConfig{Favorites: favorites(t, content)}, "")
		mustNotContain(t, body, "Meine Geräte", "Favoriten", "☆")
	}
}

// A favorites file that does not read is said plainly, not as "none";
// HA down with favorites says they cannot be seen.
func TestStartSaysABrokenFavoritesFilePlainly(t *testing.T) {
	fake, hc := house(t)
	fake.Set("light.buero", "on", map[string]any{"friendly_name": "Bürolicht"})
	code, body := startPage(t, StartConfig{HA: hc, Favorites: favorites(t, `{"anna": [`)}, "")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	mustContain(t, section(t, body, "karte-meine-geraete"), "Deine Favoriten lassen sich gerade nicht lesen.")
	mustNotContain(t, body, "unexpected", "JSON", "favoriten.json", "Tippe in Räume")
}

// Many favorites: the first twelve, and how many more.
func TestStartCutsLongFavoriteLists(t *testing.T) {
	fake, hc := house(t)
	var ids []string
	for i := 1; i <= 15; i++ {
		id := fmt.Sprintf("light.l%02d", i)
		fake.Set(id, "off", map[string]any{"friendly_name": fmt.Sprintf("Lampe %02d", i)})
		ids = append(ids, `"`+id+`"`)
	}
	_, body := startPage(t, StartConfig{HA: hc, Favorites: favorites(t, `{"anna": [`+strings.Join(ids, ",")+`]}`)}, "")
	g := section(t, body, "karte-meine-geraete")
	mustContain(t, g, "Lampe 01", "Lampe 12", "und 3 weitere")
	mustNotContain(t, g, "Lampe 13", "Lampe 15")
}

// haus#35: Heute und demnächst — appointments and reminders together, by
// time, each leading to its page; done, past and far things left out.
// Notes are not on Start: they have their own tab.
func TestStartHeuteUndDemnaechst(t *testing.T) {
	_, body := startPage(t, familyEvening(t), "family")
	a := section(t, body, "karte-heute-und-demnaechst")
	mustContain(t, a,
		`<time>heute 19:00</time>`, `<a href="/kalender/erinnerung/muell">Müll rausbringen</a>`,
		`<time>morgen 08:30</time>`, `<a href="/kalender">Zahnarzt</a>`, "Ben",
		`<time>Sa 17.10.</time>`, `<a href="/kalender/erinnerung/oma">Geschenk für Oma</a>`)
	mustNotContain(t, a, "Gestern vorbei", "Erledigt schon", "Einkaufsliste")
	before(t, a, "Müll rausbringen", "Zahnarzt")
	before(t, a, "Zahnarzt", "Geschenk für Oma")
}

// A reminder without a due date comes after everything with one, "offen";
// one overdue comes first; the list has an end, and says how much more.
func TestStartHeuteUndDemnaechstOrder(t *testing.T) {
	dav, cal := calendar(t)
	dav.Put("/haus/familie/a.ics", todo("a", "Irgendwann", ""))
	dav.Put("/haus/familie/b.ics", todo("b", "Liegt schon", "DUE:20261013T070000Z\r\n"))
	dav.Put("/haus/familie/c.ics", todo("c", "Steuer", "DUE:20261030T070000Z\r\n"))
	for i := 1; i <= 6; i++ {
		dav.Put(fmt.Sprintf("/haus/familie/e%d.ics", i), event(fmt.Sprintf("e%d", i), fmt.Sprintf("202610%02dT150000Z", 14+i), fmt.Sprintf("202610%02dT160000Z", 14+i), fmt.Sprintf("Termin %d", i)))
	}
	_, body := startPage(t, StartConfig{Calendar: cal}, "")
	a := section(t, body, "karte-heute-und-demnaechst")
	mustContain(t, a, "überfällig seit gestern 09:00", "Liegt schon", "Termin 1", "Termin 5", "und 2 weitere", `href="/kalender"`)
	mustNotContain(t, a, "Steuer", "Termin 6", "Irgendwann")
	before(t, a, "Liegt schon", "Termin 1")

	dav2, cal2 := calendar(t)
	dav2.Put("/haus/familie/a.ics", todo("a", "Irgendwann", ""))
	dav2.Put("/haus/familie/2.ics", event("2", "20261015T130000Z", "20261015T140000Z", "Zahnarzt"))
	_, body = startPage(t, StartConfig{Calendar: cal2}, "")
	a = section(t, body, "karte-heute-und-demnaechst")
	mustContain(t, a, "<time>offen</time>", "Irgendwann")
	before(t, a, "Zahnarzt", "Irgendwann")
}

// haus#35: nothing coming up says so and how it fills.
func TestStartHeuteUndDemnaechstEmptySaysHow(t *testing.T) {
	_, cal := calendar(t)
	_, body := startPage(t, StartConfig{Calendar: cal}, "")
	mustContain(t, section(t, body, "karte-heute-und-demnaechst"),
		"Nichts in den nächsten zwei Wochen.", "Termine und Erinnerungen aus dem Kalender stehen hier.")
}

// haus#35, owner decision 2026-10-15: the box is one calm line at the
// bottom, in the family's words. When something is wrong it moves to the
// top and says what it means for the family; the admin's words stay on
// /box. Amber when the box mends it itself, red only when a person is
// needed: nobody automatic on it, or the data not backed up for days.
func TestStartBoxLine(t *testing.T) {
	loc := berlin(t)
	ok := []map[string]any{{"name": "immich", "state": "grün"}, {"name": "paperless", "state": "grün"}}
	down := []map[string]any{{"name": "immich", "state": "rot"}, {"name": "paperless", "state": "rot"}, {"name": "media", "state": "gelb"}}
	for _, c := range []struct {
		name  string
		ready map[string]any
		rows  []map[string]any
		top   []string // in the banner above Im Haus
		calm  string   // the line at the bottom; "" for none
		kind  string   // the banner's: selbst (amber, the box mends it) or hilfe (red, a person is needed)
	}{
		{"all well", allGreen(t), ok, nil, "Alles in Ordnung · eure Daten sind von gestern Abend gesichert", ""},
		{"backed up tonight", readyAt(t, time.Date(2026, 10, 14, 3, 12, 0, 0, loc), "läuft"), ok, nil, "Alles in Ordnung · eure Daten sind von heute Nacht gesichert", ""},
		{"two down, the box on it", allGreen(t), down,
			[]string{"Gerade nicht erreichbar: Fotos und Dokumente.", "Die Box versucht, das selbst zu beheben.", "Eure Daten sind von gestern Abend gesichert."}, "", "selbst"},
		{"down, nobody automatic", readyAt(t, time.Date(2026, 10, 13, 21, 31, 0, 0, loc), "aus"), down[:1],
			[]string{"Gerade nicht erreichbar: Fotos.", "Wer die Box betreut, weiß Bescheid."}, "", "hilfe"},
		{"no backup for days", readyAt(t, time.Date(2026, 10, 10, 21, 0, 0, 0, loc), "läuft"), ok,
			[]string{"Eure Daten sind seit 3 Tagen nicht gesichert.", "Die Box versucht, das selbst zu beheben."}, "", "hilfe"},
		{"never backed up", readyAt(t, time.Time{}, "fehlt"), ok,
			[]string{"Eure Daten sind noch nicht gesichert.", "Wer die Box betreut, weiß Bescheid."}, "", "hilfe"},
	} {
		fake, hc := house(t)
		fake.Set("light.flur", "on", map[string]any{"friendly_name": "Flur"})
		_, body := startPage(t, StartConfig{Laib: fakeLaib(t, c.ready, c.rows), HA: hc}, "family")
		for _, w := range c.top {
			if !strings.Contains(body, w) {
				t.Errorf("%s: lacks %q:\n%s", c.name, w, body)
				continue
			}
			before(t, body, w, "karte-im-haus")
		}
		if c.calm != "" {
			mustContain(t, body, "class=ruhig", c.calm)
			before(t, body, "karte-im-haus", c.calm)
			mustNotContain(t, body, `class="achtung`)
		} else {
			other := map[string]string{"selbst": "hilfe", "hilfe": "selbst"}[c.kind]
			if !strings.Contains(body, `<section class="achtung `+c.kind+`"`) || strings.Contains(body, `class="achtung `+other+`"`) {
				t.Errorf("%s: the banner is not %s:\n%s", c.name, c.kind, body)
			}
			mustNotContain(t, body, "class=ruhig", "Alles in Ordnung")
		}
		mustNotContain(t, body, "Alles läuft", "Letzte Sicherung", "Orten", "Operator", "Admin", "Letzte Änderung", "Rückgängig",
			"immich", "paperless", "Laib", "rot<", "grün")
	}
	// the box well but the house not answering: the box line does not say
	// "Alles in Ordnung" under a page that says something does not answer
	fake, hc := house(t)
	fake.Down(true)
	_, body := startPage(t, StartConfig{Laib: fakeLaib(t, allGreen(t), ok), HA: hc}, "family")
	mustContain(t, body, "class=ruhig", "Eure Daten sind von gestern Abend gesichert", "Licht und Geräte antworten gerade nicht.")
	mustNotContain(t, body, "Alles in Ordnung", `class="achtung`)
	// Laib not answering: #34's line, at the bottom, without its error
	code, body := startPage(t, StartConfig{Laib: brokenLaib(t)}, "")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	mustContain(t, body, "class=ruhig", "Wie es der Box geht, lässt sich gerade nicht sagen.")
	mustNotContain(t, body, "Alles in Ordnung", `class="achtung`, "connection refused", "500", "boom")
	// no Laib configured: not a word
	_, body = startPage(t, StartConfig{}, "")
	mustNotContain(t, body, "class=ruhig", `class="achtung`, "Box geht", "Die Box", "gesichert", "Alles in Ordnung")
}

// How long ago the backup was, the way the family says it.
func TestBackupWords(t *testing.T) {
	loc := berlin(t)
	now := startNow(t) // Wednesday 14.10.2026, 10:00
	for at, want := range map[time.Time]string{
		time.Date(2026, 10, 14, 3, 12, 0, 0, loc):  "von heute Nacht",
		time.Date(2026, 10, 14, 7, 0, 0, 0, loc):   "von heute Morgen",
		time.Date(2026, 10, 14, 11, 0, 0, 0, loc):  "von heute Vormittag", // a clock a little ahead is still today
		time.Date(2026, 10, 13, 21, 31, 0, 0, loc): "von gestern Abend",
		time.Date(2026, 10, 13, 15, 0, 0, 0, loc):  "von gestern Nachmittag",
		time.Date(2026, 10, 13, 12, 30, 0, 0, loc): "von gestern Mittag",
		time.Date(2026, 10, 13, 10, 0, 0, 0, loc):  "von gestern Vormittag",
		time.Date(2026, 10, 12, 23, 30, 0, 0, loc): "von vorgestern Nacht",
		time.Date(2026, 10, 12, 4, 59, 0, 0, loc):  "von vorgestern Nacht",
		time.Date(2026, 10, 12, 5, 0, 0, 0, loc):   "von vorgestern Morgen",
		time.Date(2026, 10, 10, 20, 0, 0, 0, loc):  "von Samstagabend",
		time.Date(2026, 10, 8, 18, 0, 0, 0, loc):   "von Donnerstagabend",
		time.Date(2026, 10, 7, 20, 0, 0, 0, loc):   "vom 7. Oktober",
		time.Date(2026, 10, 1, 20, 0, 0, 0, loc):   "vom 1. Oktober",
	} {
		if got := backupWords(at, now, loc); got != want {
			t.Errorf("%v: %q, want %q", at, got, want)
		}
	}
}

// haus#12, haus#33: a backend in trouble is said in one plain line where
// its rows would be — never a blank, never a 500, never its error or its
// name: the error goes to the log, not to the kitchen table.
func TestStartSaysPlainlyWhatDoesNotAnswer(t *testing.T) {
	fake, hc := house(t)
	fake.Set("light.flur", "on", map[string]any{"friendly_name": "Flur"})
	fake.Down(true)
	_, cal := calendar(t)
	cal.Password = "falsch"
	fav := favorites(t, `{"anna": ["light.flur"]}`)
	code, body := startPage(t, StartConfig{HA: hc, Calendar: cal, Favorites: fav, Laib: brokenLaib(t)}, "family")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	mustContain(t, body,
		"Wie es der Box geht, lässt sich gerade nicht sagen.",
		"<h2 id=karte-im-haus>Im Haus</h2>", "Licht und Geräte antworten gerade nicht. Schalten geht im Moment nicht.",
		"<h2 id=karte-meine-geraete>Meine Geräte</h2>", "Deine Favoriten sind gerade nicht zu sehen.",
		"<h2 id=karte-heute-und-demnaechst>Heute und demnächst</h2>", "Der Kalender antwortet gerade nicht.")
	mustNotLeak(t, body, "Flur", "Laib", "favoriten.json", "Alles in Ordnung", "<form", host(hc.URL), host(cal.URL))
	for _, once := range []string{"Der Kalender antwortet gerade nicht.", "Licht und Geräte antworten gerade nicht."} {
		if n := strings.Count(body, once); n != 1 {
			t.Fatalf("%q said %d times", once, n)
		}
	}
	before(t, body, "karte-heute-und-demnaechst", "Wie es der Box geht")
}

// Only the reminders failing (the appointments read): the appointments,
// and the plain line, in the same section.
func TestStartSaysFailingRemindersWithTheAppointments(t *testing.T) {
	dav, cal := calendar(t)
	dav.Put("/haus/familie/2.ics", event("2", "20261015T130000Z", "20261015T140000Z", "Zahnarzt"))
	// in front of the fake: a reminders query answers 500, the rest passes
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Method == "REPORT" && strings.Contains(string(body), `name="VTODO"`) {
			http.Error(w, "REPORT 500", http.StatusInternalServerError)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		httputil.NewSingleHostReverseProxy(must(url.Parse(dav.URL))).ServeHTTP(w, r)
	}))
	t.Cleanup(front.Close)
	cal.URL = front.URL
	_, body := startPage(t, StartConfig{Calendar: cal}, "")
	a := section(t, body, "karte-heute-und-demnaechst")
	mustContain(t, a, "Zahnarzt", "morgen 15:00", "Der Kalender antwortet gerade nicht.")
	mustNotContain(t, body, "REPORT", "500", "Nichts in den nächsten")
}

// What is not configured is not mentioned: no HA, no calendar — no
// sections, and no word about them.
func TestStartSaysNothingAboutWhatIsNotConfigured(t *testing.T) {
	code, body := startPage(t, StartConfig{Laib: fakeLaib(t, allGreen(t), nil), LaibURL: "https://laib.box.test"}, "")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	mustContain(t, body, "Alles in Ordnung · eure Daten sind von gestern Abend gesichert")
	mustNotContain(t, body, "karte-im-haus", "karte-meine-geraete", "karte-heute", "Licht", "Termin", "Erinnerung", "Notiz", "nicht konfiguriert", "HAUS_")
}

// haus#35: the family's pages switch Home Assistant things through
// /schalten; Start itself still takes no POST.
func TestStartDecidesNothing(t *testing.T) {
	rec := httptest.NewRecorder()
	Start(StartConfig{}).ServeHTTP(rec, httptest.NewRequest("POST", "/start", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /start → %d, want 405", rec.Code)
	}
}

// Admins find the Box page from every family page's tab bar; the family
// does not see the tab.
func TestStartLinksTheBoxForAdmins(t *testing.T) {
	_, body := startPage(t, StartConfig{}, "admins,family")
	mustContain(t, body, `href=/box`)
	_, body = startPage(t, StartConfig{}, "family")
	mustNotContain(t, body, `href=/box`)
}

// haus#12: "/" and every path Haus does not serve land on Start, never on
// an error.
func TestEverythingElseLandsOnStart(t *testing.T) {
	for _, p := range []string{"/", "/download", "/p/start", "/unbekannt"} {
		rec := httptest.NewRecorder()
		ToStart().ServeHTTP(rec, httptest.NewRequest("GET", p, nil))
		if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/start" {
			t.Fatalf("%s → %d %q, want 302 /start", p, rec.Code, rec.Header().Get("Location"))
		}
	}
}

// brokenLaib answers every call with a 500.
func brokenLaib(t *testing.T) *laib.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "500: boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	return &laib.Client{URL: srv.URL, Token: "t"}
}

// haus#12: Box is the admins' page (laib DEKONSTRUKTION). Anyone else
// asking for /box or /box/… is sent to Start (303) — who sees the admin
// page, not a verdict on anything. Pairing from the phone is untouched.
func TestBoxIsForAdmins(t *testing.T) {
	store, _ := devices.Open(t.TempDir())
	box := Box(fakeLaib(t, allGreen(t), nil), "https://laib.box.test")
	geraete := Geraete(store, "https://haus.example")
	for _, c := range []struct {
		h      http.HandlerFunc
		method string
		path   string
	}{{box, "GET", "/box"}, {box, "GET", "/box/inbox"}, {geraete, "GET", "/box/geraete"}, {geraete, "POST", "/box/geraete"}} {
		for _, groups := range []string{"", "family", "administrators", "family,admins-light"} {
			req := httptest.NewRequest(c.method, c.path, strings.NewReader("name=Pixel"))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Remote-User", "ben")
			if groups != "" {
				req.Header.Set("Remote-Groups", groups)
			}
			rec := httptest.NewRecorder()
			c.h.ServeHTTP(rec, req)
			if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/start" {
				t.Fatalf("%s %s as %q → %d %q, want 303 /start", c.method, c.path, groups, rec.Code, rec.Header().Get("Location"))
			}
		}
	}
	if ds := store.List(); len(ds) != 0 {
		t.Fatalf("a non-admin minted a device: %v", ds)
	}
	for _, c := range []struct {
		h    http.HandlerFunc
		path string
	}{{box, "/box"}, {geraete, "/box/geraete"}} {
		req := httptest.NewRequest("GET", c.path, nil)
		req.Header.Set("Remote-User", "anna")
		req.Header.Set("Remote-Groups", "family,admins")
		rec := httptest.NewRecorder()
		c.h.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("an admin on %s → %d", c.path, rec.Code)
		}
	}
	// the phone's own pairing path is for everyone
	req := httptest.NewRequest("GET", "/pair-device", nil)
	req.Header.Set("Remote-User", "ben")
	req.Header.Set("Remote-Groups", "family")
	rec := httptest.NewRecorder()
	PairDevice(store).ServeHTTP(rec, req)
	if rec.Code == http.StatusSeeOther || rec.Code == http.StatusFound {
		t.Fatalf("pairing redirected a family member: %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

// favorites is a favorites store holding content ("" for no file).
func favorites(t *testing.T, content string) *favoriten.Store {
	t.Helper()
	dir := t.TempDir()
	if content != "" {
		if err := os.WriteFile(filepath.Join(dir, favoriten.File), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return favoriten.Open(dir)
}

// The family reads Start on a phone: the page scales to the screen, a
// long name breaks inside its row, and what a finger taps is at least
// 40 px (the design guidelines: "could my mother use it?").
func TestStartFitsAPhone(t *testing.T) {
	fake, hc := house(t)
	fake.Set("light.lang", "on", map[string]any{"friendly_name": strings.Repeat("Wohnzimmerdeckenfluter", 3)})
	_, body := startPage(t, StartConfig{HA: hc}, "")
	mustContain(t, body, "width=device-width", cards.Style, "min-height:40px")
	mustNotContain(t, body, "<script", "<table")
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
