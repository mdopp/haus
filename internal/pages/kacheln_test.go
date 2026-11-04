package pages

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/mdopp/homes/internal/ha"
)

// tileOf is the <li> of a tile on Start, by its thing.
func tileOf(t *testing.T, g, id string) string {
	t.Helper()
	i := strings.Index(g, `data-seite="/raeume/`+id+`"`)
	if i < 0 {
		t.Fatalf("no tile of %s:\n%s", id, g)
	}
	i = strings.LastIndex(g[:i], "<li ")
	return g[i : i+strings.Index(g[i:], "</li>")+5]
}

// owner, 2026-11-04 („Das Design ist großartig! Bitte auf der Webseite
// genau so umsetzen!“): Deine Geräte as the app's tiles, variant A2. The
// head says „Alle →“ and what a tap does; the picture says the state.
// A dimmer has its level („aus“, „60 %“) as the button to its sheet and
// the tile as its switch; a cover takes the row with ▲ ■ ▼ and its tile
// opens the sheet, which has Stopp; a garage asks before it opens; a lock
// only leads to its page; a heating says its reading in words. On a phone
// a tile left without a neighbour takes the row. No ★, no outline.
func TestDeineGeraeteAreTheAppsTiles(t *testing.T) {
	fake, hc := house(t)
	dimHouse(t, fake)
	fake.Set("lock.haustuer", "locked", map[string]any{"friendly_name": "Haustür"})
	fake.Set("climate.bad", "heat", map[string]any{"friendly_name": "Heizung Bad", "current_temperature": 21.5, "temperature": 23.0})
	cfg := StartConfig{HA: hc, Favorites: favorites(t, `{"anna": ["light.dimmer", "cover.rollo", "cover.garage", "light.bambi", "lock.haustuer", "climate.bad", "light.schalter"]}`)}
	_, body := startPage(t, cfg, "family")
	g := section(t, body, "karte-deine-geraete")
	mustContain(t, body, `<section class="karte geraete" aria-labelledby=karte-deine-geraete><p class=stand>Box nicht erreichbar – versucht es wieder …<br>Stand 10:00</p>`+
		`<div class=kopfzeile><h2 id=karte-deine-geraete>Deine Geräte</h2><a class=alle href=/raeume>Alle →</a></div>`,
		`<p class=hinweis>Tippen schaltet, lange drücken öffnet das Gerät.</p><ul class=kacheln>`)
	mustNotContain(t, g, "★", "☆", "<b class=zustand>", "balken")

	// a dimmer that is off: alone before a cover, the tile its switch, „aus“ its sheet's button, the dark lamp
	d := tileOf(t, g, "light.dimmer")
	mustContain(t, d, `<li class="kachel allein" style="--farbe:var(--muted)"`,
		`<span class=flaeche><form method=post action=/schalten><input type=hidden name=geraet value="light.dimmer"><input type=hidden name=tun value="light.turn_on"><input type=hidden name=zurueck value="/start"><button class=schalter role=switch aria-checked=false aria-label="Z-Wave-Dimmer"></button></form></span>`,
		`<button type=button class=stufe popovertarget=regler-light-dimmer aria-haspopup=dialog aria-label="Z-Wave-Dimmer Helligkeit aus, einstellen">aus</button>`,
		`<div popover id=regler-light-dimmer class=blatt`, bilder["licht-aus"])
	mustNotContain(t, d, "class=worte")

	// a blind: across the row, ▲ ■ ▼ as pictures, its level, the tile opens the sheet with Stopp
	r := tileOf(t, g, "cover.rollo")
	mustContain(t, r, `<li class="kachel aktiv breit" style="--farbe:var(--state)"`,
		`<span class=flaeche><button type=button popovertarget=regler-cover-rollo aria-haspopup=dialog aria-label="Rollo"></button></span>`,
		`aria-label="Rollo Position 40 %, einstellen">40 %</button>`,
		`<span class=knoepfe><form method=post action=/schalten><input type=hidden name=geraet value="cover.rollo"><input type=hidden name=tun value="cover.open_cover"><input type=hidden name=zurueck value="/start"><button data-laeuft="" class=hoch aria-label="Rollo: Hoch"></button></form>`,
		`value="cover.stop_cover"><input type=hidden name=zurueck value="/start"><button data-laeuft="" class=stopp aria-label="Rollo: Stopp"></button>`,
		`value="cover.close_cover"><input type=hidden name=zurueck value="/start"><button data-laeuft="" class=runter aria-label="Rollo: Runter"></button>`,
		`aria-label="Position Zu">Zu</button></form><form method=post action=/schalten><input type=hidden name=geraet value="cover.rollo"><input type=hidden name=tun value="cover.stop_cover"><input type=hidden name=zurueck value="/start"><button data-laeuft="Schaltet …">Stopp</button></form></div>`)

	// a garage, shut: ▲ asks, as the tile does; ▼ and ■ go at once; no slider
	gar := tileOf(t, g, "cover.garage")
	mustContain(t, gar, `<li class="kachel breit" style="--farbe:var(--ok)"`,
		`<span class=flaeche><button type=button popovertarget=frage-cover-garage-cover-open-cover aria-label="Garage: Hoch"></button></span>`,
		`<button type=button popovertarget=frage-cover-garage-cover-open-cover class=hoch aria-label="Garage: Hoch"></button><div popover id=frage-cover-garage-cover-open-cover`,
		"Garage öffnen?", `<input type=hidden name=bestaetigt value=ja><button data-laeuft="…">Öffnen</button>`, ">Abbrechen</button>", bilder["tor-zu"])
	mustNotContain(t, gar, "class=stufe", "regler-")
	if n := strings.Count(gar, "<div popover"); n != 1 {
		t.Errorf("the garage's question %d times:\n%s", n, gar)
	}

	// a lamp on and a lock: a pair; the lock leads only to its page
	mustContain(t, tileOf(t, g, "light.bambi"), `<li class="kachel aktiv" style="--farbe:var(--state)"`, ">60 %</button>", bilder["licht-an"])
	l := tileOf(t, g, "lock.haustuer")
	mustContain(t, l, `<li class="kachel" style="--farbe:var(--ok)"`, `<a class=name href="/raeume/lock.haustuer" style="--n:7">Haustür</a>`, bilder["schloss-zu"])
	mustNotContain(t, l, "flaeche", "<form", "class=worte")

	// the heating says its reading; with the lamp that only switches, a pair
	mustContain(t, tileOf(t, g, "climate.bad"), `<li class="kachel aktiv"`, `<span class=worte>21,5 °C · Soll 23 °C</span>`)
	mustContain(t, tileOf(t, g, "light.schalter"), `<li class="kachel aktiv"`, `role=switch aria-checked=true aria-label="Stehlampe"`)
	mustNotContain(t, tileOf(t, g, "light.schalter"), "class=stufe")

	for i, id := range []string{"light.dimmer", "cover.rollo", "cover.garage", "light.bambi", "lock.haustuer", "climate.bad"} {
		next := []string{"cover.rollo", "cover.garage", "light.bambi", "lock.haustuer", "climate.bad", "light.schalter"}[i]
		before(t, g, `data-seite="/raeume/`+id+`"`, `data-seite="/raeume/`+next+`"`)
	}
}

// A thing on its way or out of reach says so in words, greyed; the
// picture alone cannot.
func TestATileSaysWhatItsPictureCannot(t *testing.T) {
	for _, c := range []struct {
		id, state, words, farbe string
	}{
		{"cover.rollo", "opening", "öffnet …", "--unterwegs"},
		{"light.flur", "unavailable", "nicht erreichbar", "--faint"},
		{"lock.tuer", "jammed", "klemmt", "--danger"},
		{"lock.tuer", "unlocked", "", "--warn"},
		{"light.flur", "off", "", "--muted"},
	} {
		k := neueKachel(ha.State{EntityID: c.id, State: c.state, Attributes: map[string]any{"device_class": "shutter"}}, "", "/start")
		if k.worte != c.words || k.farbe != c.farbe {
			t.Errorf("%s %s: %q %s, want %q %s", c.id, c.state, k.worte, k.farbe, c.words, c.farbe)
		}
	}
	if k := neueKachel(ha.State{EntityID: "light.flur", State: "unavailable"}, "", "/start"); k.tippen != nil {
		t.Error("a lamp HA does not reach switches")
	}
}

// The app's name rule: the room HA appends goes, a name that is nothing
// else stays.
func TestKurzname(t *testing.T) {
	for in, want := range map[string]string{"Garagentor Garage": "Garagentor", "Küche Küchenlicht": "Küchenlicht", "Bürolicht": "Bürolicht",
		"Licht Flur": "Licht Flur", "Haus Tür": "Haus Tür", "Wohnzimmer": "Wohnzimmer", "Ess Esszimmer Lampe": "Esszimmer Lampe"} {
		if got := kurzname(in); got != want {
			t.Errorf("kurzname(%q) = %q, want %q", in, got, want)
		}
	}
}

// A blind part-way is drawn down to its position: the lowered picture over
// the raised one, cut at 100 − position; open or shut, one picture.
func TestABlindPartWayIsDrawnDownToItsPosition(t *testing.T) {
	part := bildVon("cover", ha.State{State: "open", Attributes: map[string]any{"current_position": 25.0}})
	mustContain(t, part, bilder["rollo-auf"]+`<svg width=24 height=18.0 viewBox="0 0 24 18.0">`+bilder["rollo-zu"]+`</svg>`)
	if up := bildVon("cover", ha.State{State: "open", Attributes: map[string]any{"current_position": 100.0}}); strings.Contains(up, "height=") {
		t.Errorf("a blind all up has the shade:\n%s", up)
	}
	mustContain(t, bildVon("cover", ha.State{State: "open", Attributes: map[string]any{"device_class": "garage"}}), bilder["tor-auf"])
}

// The app asks before a garage opens; so does the page: open_cover on a
// garage goes only with its question's Ja (bestaetigt), closing goes at
// once, and nothing makes the page unlock a door.
func TestAGarageOpensOnlyFromItsQuestion(t *testing.T) {
	fake, cfg := switchHouse(t)
	h := Schalten(cfg)
	send := func(f url.Values) int {
		f.Set("zurueck", "/start")
		return post(t, h, "/schalten", f, "anna", nil).Code
	}
	n := len(fake.Calls())
	if code := send(url.Values{"geraet": {"cover.garage"}, "tun": {"cover.open_cover"}}); code == http.StatusSeeOther || len(fake.Calls()) != n {
		t.Fatalf("open without asking → %d, %d calls", code, len(fake.Calls())-n)
	}
	if code := send(url.Values{"geraet": {"cover.garage"}, "tun": {"cover.open_cover"}, "bestaetigt": {"ja"}}); code != http.StatusSeeOther || len(fake.Calls()) != n+1 {
		t.Fatalf("open with its Ja → %d, %d calls", code, len(fake.Calls())-n)
	}
	if code := send(url.Values{"geraet": {"cover.garage"}, "tun": {"cover.close_cover"}}); code != http.StatusSeeOther {
		t.Fatalf("close → %d", code)
	}
	n = len(fake.Calls())
	if code := send(url.Values{"geraet": {"lock.haustuer"}, "tun": {"lock.unlock"}, "bestaetigt": {"ja"}}); code == http.StatusSeeOther || len(fake.Calls()) != n {
		t.Fatalf("unlock → %d, %d calls", code, len(fake.Calls())-n)
	}
}
