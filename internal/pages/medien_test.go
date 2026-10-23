package pages

import (
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/mdopp/haus/internal/ha/hafake"
)

// HA's media player features: PAUSE, VOLUME_SET, VOLUME_STEP, PLAY
const fullPlayer = 1.0 + 4 + 1024 + 16384

// a house with players: two playing, one paused, one off, one ready, one
// HA cannot reach, one hidden, one group
func players(t *testing.T, fake *hafake.HA) {
	t.Helper()
	fake.SetRegistries(
		[]map[string]any{{"area_id": "wohnzimmer", "name": "Wohnzimmer"}, {"area_id": "kueche", "name": "Küche"}, {"area_id": "kinder", "name": "Kinderzimmer"}},
		[]map[string]any{{"id": "dev-tv", "area_id": "wohnzimmer"}},
		[]map[string]any{
			{"entity_id": "media_player.wz", "area_id": "wohnzimmer"},
			{"entity_id": "media_player.tv", "device_id": "dev-tv"},
			{"entity_id": "media_player.kueche", "area_id": "kueche"},
			{"entity_id": "media_player.kinder", "area_id": "kinder"},
			{"entity_id": "media_player.alt", "area_id": "kueche", "hidden_by": "user"},
		})
	fake.Set("media_player.wz", "playing", map[string]any{"friendly_name": "Sonos", "media_title": "Abbey Road", "media_artist": "The Beatles",
		"volume_level": 0.35, "supported_features": fullPlayer})
	fake.Set("media_player.tv", "playing", map[string]any{"friendly_name": "Fernseher", "app_name": "Netflix", "supported_features": 1.0})
	fake.Set("media_player.kueche", "paused", map[string]any{"friendly_name": "Küchenradio", "media_title": "Deutschlandfunk",
		"volume_level": 0.2, "supported_features": fullPlayer})
	fake.Set("media_player.kinder", "off", map[string]any{"friendly_name": "Kinderzimmer", "supported_features": fullPlayer})
	fake.Set("media_player.bad", "idle", map[string]any{"friendly_name": "Badradio", "supported_features": fullPlayer})
	fake.Set("media_player.garage", "unavailable", map[string]any{"friendly_name": "Garagenlautsprecher"})
	fake.Set("media_player.alt", "playing", map[string]any{"friendly_name": "Altes Radio", "media_title": "Versteckt", "supported_features": fullPlayer})
	fake.Set("media_player.alle", "playing", map[string]any{"friendly_name": "Überall", "entity_id": []any{"media_player.wz", "media_player.kueche"},
		"supported_features": fullPlayer})
	fake.Set("light.flur", "on", map[string]any{"friendly_name": "Flurlicht"})
}

// haus#12: Medien says what plays where — each playing or paused player
// with its room and what it plays, pause or go on, quieter or louder,
// right there through /schalten — and which players are quiet. Only what
// the player can do has a button. What HA hides, and a group, are not
// players.
func TestMedienWhatPlaysWhere(t *testing.T) {
	fake, hc := house(t)
	players(t, fake)
	code, body, _ := familyPage(t, Medien, StartConfig{HA: hc}, "GET", "/medien", "family")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	mustContain(t, body, "<title>Medien · Zuhause</title>", "<h1>Medien</h1>")
	laeuft := section(t, body, "karte-laeuft-gerade")
	mustContain(t, laeuft,
		`<a href="/raeume/media_player.wz">Sonos</a> <span class=k>Wohnzimmer · Abbey Road · The Beatles · Lautstärke 35 %</span>`,
		`<a href="/raeume/media_player.tv">Fernseher</a> <span class=k>Wohnzimmer · Netflix</span>`,
		`<a href="/raeume/media_player.kueche">Küchenradio</a> <span class=k>Küche · pausiert · Deutschlandfunk · Lautstärke 20 %</span>`)
	// playing first, by name; then paused
	before(t, laeuft, "Fernseher", "Sonos")
	before(t, laeuft, "Sonos", "Küchenradio")
	// each player's buttons, posting to /schalten and back to Medien
	forms := strings.Split(laeuft, "<li")
	wants := map[string][]string{
		"Sonos":       {"media_player.media_pause", "media_player.volume_down", "media_player.volume_up"},
		"Fernseher":   {"media_player.media_pause"},
		"Küchenradio": {"media_player.media_play", "media_player.volume_down", "media_player.volume_up"},
	}
	for name, want := range wants {
		var got []string
		for _, li := range forms {
			if !strings.Contains(li, ">"+name+"</a>") {
				continue
			}
			for _, f := range strings.Split(li, "<form")[1:] {
				mustContain(t, f, ` method=post action=/schalten>`, `name=zurueck value="/medien"`)
				i := strings.Index(f, `name=tun value="`)
				got = append(got, strings.SplitN(f[i+len(`name=tun value="`):], `"`, 2)[0])
			}
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: buttons %v, want %v", name, got, want)
		}
	}
	mustContain(t, laeuft, ">Pause</button>", ">Weiter</button>", ">Leiser</button>", ">Lauter</button>")
	// every player's buttons on a line of their own, one alone too
	if n := strings.Count(laeuft, `<li class="tu viele">`); n != 3 {
		t.Errorf("%d rows with their buttons below, want 3:\n%s", n, laeuft)
	}
	still := section(t, body, "karte-gerade-still")
	mustContain(t, still, `<a href="/raeume/media_player.bad">Badradio</a> <span class=k>bereit</span>`,
		`<a href="/raeume/media_player.garage">Garagenlautsprecher</a> <span class=k>nicht erreichbar</span>`,
		`<a href="/raeume/media_player.kinder">Kinderzimmer</a> <span class=k>aus</span>`)
	mustNotContain(t, still, "<form")
	before(t, still, "Badradio", "Garagenlautsprecher")
	before(t, still, "Garagenlautsprecher", "Kinderzimmer")
	before(t, body, "karte-laeuft-gerade", "karte-gerade-still")
	mustNotContain(t, body, "Altes Radio", "Versteckt", "Überall", "Flurlicht", "media_player.alt", "media_player.alle", "<script", "0.35")
}

// The buttons switch through the one path: Medien's are in what a page
// may do, they land back on Medien, and HA hears each.
func TestMedienSwitchesThroughSchalten(t *testing.T) {
	fake, hc := house(t)
	players(t, fake)
	h := Schalten(StartConfig{HA: hc})
	for _, tun := range []string{"media_player.media_pause", "media_player.media_play", "media_player.volume_down", "media_player.volume_up"} {
		rec := post(t, h, "/schalten", url.Values{"geraet": {"media_player.wz"}, "tun": {tun}, "zurueck": {"/medien"}}, "anna", nil)
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/medien" {
			t.Errorf("%s → %d %q, want 303 /medien", tun, rec.Code, rec.Header().Get("Location"))
		}
	}
	want := []string{"media_player.media_pause media_player.wz", "media_player.media_play media_player.wz",
		"media_player.volume_down media_player.wz", "media_player.volume_up media_player.wz"}
	if got := calls(fake); !reflect.DeepEqual(got, want) {
		t.Fatalf("HA got %v, want %v", got, want)
	}
	// still no URL on the speakers, no set volume, no mute, no turning off
	for _, tun := range []string{"media_player.play_media", "media_player.volume_set", "media_player.volume_mute", "media_player.turn_off",
		"media_player.media_stop", "media_player.media_next_track", "media_player.select_source"} {
		if rec := post(t, h, "/schalten", url.Values{"geraet": {"media_player.wz"}, "tun": {tun}, "zurueck": {"/medien"}}, "anna", nil); rec.Code != http.StatusBadRequest {
			t.Errorf("%s → %d, want 400", tun, rec.Code)
		}
	}
	if len(fake.Calls()) != len(want) {
		t.Fatalf("HA got %v", calls(fake))
	}
	// HA failing a switch from Medien: the plain line, the way back to Medien
	fake.FailCalls(502)
	rec := post(t, h, "/schalten", url.Values{"geraet": {"media_player.wz"}, "tun": {"media_player.volume_up"}, "zurueck": {"/medien"}}, "anna", nil)
	mustContain(t, rec.Body.String(), saysSwitchFailed, "<p><a href=/medien>Zurück</a></p>", "href=/medien aria-current=page>")
	mustNotLeak(t, rec.Body.String())
	// the player refusing it (HA 500, "The command to the player failed", haus#83): said as such
	fake.FailCalls(500)
	rec = post(t, h, "/schalten", url.Values{"geraet": {"media_player.wz"}, "tun": {"media_player.media_pause"}, "zurueck": {"/medien"}}, "anna", nil)
	mustContain(t, rec.Body.String(), saysPlayerNo, "<p><a href=/medien>Zurück</a></p>")
	mustNotContain(t, rec.Body.String(), saysSwitchFailed)
	mustNotLeak(t, rec.Body.String())
}

// A player's own page has the same buttons, landing back on its page.
func TestDingPlayerHasMedienButtons(t *testing.T) {
	fake, hc := house(t)
	players(t, fake)
	_, body, _ := familyPage(t, Ding, StartConfig{HA: hc}, "GET", "/raeume/media_player.kueche", "family")
	mustContain(t, body, `name=tun value="media_player.media_play"`, `name=tun value="media_player.volume_up"`,
		`name=zurueck value="/raeume/media_player.kueche"`, ">Weiter</button>", ">Lauter</button>")
	_, body, _ = familyPage(t, Ding, StartConfig{HA: hc}, "GET", "/raeume/media_player.kinder", "family")
	mustNotContain(t, body, "action=/schalten")
}

// Nothing playing says so in Läuft gerade; no players at all is one plain
// line; HA not answering is one plain line, nothing of HA on the page.
func TestMedienSaysPlainlyWhatIsNot(t *testing.T) {
	fake, hc := house(t)
	fake.Set("media_player.kinder", "off", map[string]any{"friendly_name": "Kinderzimmer"})
	_, body, _ := familyPage(t, Medien, StartConfig{HA: hc}, "GET", "/medien", "family")
	mustContain(t, section(t, body, "karte-laeuft-gerade"), "Gerade läuft nichts.")
	mustContain(t, section(t, body, "karte-gerade-still"), "Kinderzimmer")

	fake2, hc2 := house(t)
	fake2.Set("light.flur", "on", map[string]any{"friendly_name": "Flurlicht"})
	code, body, _ := familyPage(t, Medien, StartConfig{HA: hc2}, "GET", "/medien", "family")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	if n := strings.Count(body, "<p class=k>Im Haus ist noch kein Lautsprecher oder Fernseher eingerichtet.</p>"); n != 1 {
		t.Fatalf("said %d times, want once:\n%s", n, body)
	}
	mustNotContain(t, body, "karte-")

	fake2.Down(true)
	code, body, _ = familyPage(t, Medien, StartConfig{HA: hc2}, "GET", "/medien", "family")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	if n := strings.Count(body, "<p class=k>Lautsprecher und Fernseher antworten gerade nicht.</p>"); n != 1 {
		t.Fatalf("said %d times, want once:\n%s", n, body)
	}
	mustNotContain(t, body, "karte-", "<form", "eingerichtet")
	mustNotLeak(t, body, host(hc2.URL))
}

// What a player says is text, never markup.
func TestMedienEscapes(t *testing.T) {
	fake, hc := house(t)
	fake.Set("media_player.x", "playing", map[string]any{"friendly_name": "<b>Box</b>", "media_title": "<img src=x onerror=alert(1)>"})
	_, body, _ := familyPage(t, Medien, StartConfig{HA: hc}, "GET", "/medien", "family")
	mustContain(t, body, "&lt;b&gt;Box&lt;/b&gt;", "&lt;img src=x")
	mustNotContain(t, body, "<b>Box", "<img src=x")
}

// A path under Medien that Haus does not serve is a 404 with the way
// back, never Start pretending all is well (haus#12).
func TestMedienUnknownPathIsA404(t *testing.T) {
	fake, hc := house(t)
	players(t, fake)
	for _, p := range []string{"/medien/", "/medien/radio", "/medien/x/y"} {
		code, body, _ := familyPage(t, Medien, StartConfig{HA: hc}, "GET", p, "family")
		if code != 404 {
			t.Fatalf("%s → %d, want 404", p, code)
		}
		mustContain(t, body, "<p>Diese Seite gibt es nicht.</p>", "<p><a href=/medien>Medien</a></p>", "href=/medien aria-current=page>")
		mustNotContain(t, body, "karte-", "<form")
	}
}

// A button only for what the player says it can do (HA's
// MediaPlayerEntityFeature: PAUSE 1, VOLUME_SET 4, VOLUME_MUTE 8,
// PLAY_MEDIA 512, VOLUME_STEP 1024, PLAY 16384): Leiser and Lauter with
// either volume feature, Pause only with PAUSE, Weiter only with PLAY.
func TestMedienButtonsFollowFeatures(t *testing.T) {
	fake, hc := house(t)
	for _, p := range []struct {
		id, state string
		features  float64
	}{
		{"a", "playing", 1 + 4},       // Pause, Leiser, Lauter (set only)
		{"b", "playing", 1024},        // Leiser, Lauter (step only), no Pause
		{"c", "paused", 1 + 1024},     // no PLAY: no Weiter
		{"d", "playing", 2 + 8 + 512}, // seek, mute, play_media: nothing
		{"e", "paused", 16384},        // Weiter only
	} {
		fake.Set("media_player."+p.id, p.state, map[string]any{"friendly_name": "Spieler " + p.id, "supported_features": p.features})
	}
	_, body, _ := familyPage(t, Medien, StartConfig{HA: hc}, "GET", "/medien", "family")
	wants := map[string][]string{
		"a": {"media_player.media_pause", "media_player.volume_down", "media_player.volume_up"},
		"b": {"media_player.volume_down", "media_player.volume_up"},
		"c": {"media_player.volume_down", "media_player.volume_up"},
		"d": nil,
		"e": {"media_player.media_play"},
	}
	for _, li := range strings.Split(section(t, body, "karte-laeuft-gerade"), "<li")[1:] {
		i := strings.Index(li, ">Spieler ")
		if i < 0 {
			continue
		}
		id := li[i+len(">Spieler ") : i+len(">Spieler ")+1]
		var got []string
		for _, f := range strings.Split(li, "<form")[1:] {
			j := strings.Index(f, `name=tun value="`)
			got = append(got, strings.SplitN(f[j+len(`name=tun value="`):], `"`, 2)[0])
		}
		if !reflect.DeepEqual(got, wants[id]) {
			t.Errorf("Spieler %s: buttons %v, want %v", id, got, wants[id])
		}
		delete(wants, id)
	}
	if len(wants) != 0 {
		t.Errorf("players not shown: %v", wants)
	}
}

// The volume is said only when HA's level is one (0–1); a player named
// for its room does not say the room twice.
func TestMedienSaysVolumeAndRoomOnlyWhenTheyMeanSomething(t *testing.T) {
	fake, hc := house(t)
	fake.SetRegistries([]map[string]any{{"area_id": "kinder", "name": "Kinderzimmer"}}, nil,
		[]map[string]any{{"entity_id": "media_player.kinder", "area_id": "kinder"}, {"entity_id": "media_player.laut", "area_id": "kinder"}})
	fake.Set("media_player.kinder", "playing", map[string]any{"friendly_name": "Kinderzimmer", "media_title": "Benjamin", "volume_level": 1.0})
	fake.Set("media_player.laut", "playing", map[string]any{"friendly_name": "Box", "media_title": "Lied", "volume_level": 1.5})
	fake.Set("media_player.leise", "playing", map[string]any{"friendly_name": "Radio", "media_title": "Nachrichten", "volume_level": -0.2})
	_, body, _ := familyPage(t, Medien, StartConfig{HA: hc}, "GET", "/medien", "family")
	mustContain(t, body, "<a href=\"/raeume/media_player.kinder\">Kinderzimmer</a> <span class=k>Benjamin · Lautstärke\u00a0100\u00a0%</span>",
		`<a href="/raeume/media_player.laut">Box</a> <span class=k>Kinderzimmer · Lied</span>`,
		`<a href="/raeume/media_player.leise">Radio</a> <span class=k>Nachrichten</span>`)
}
