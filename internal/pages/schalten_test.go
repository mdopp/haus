package pages

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/haus/internal/ha/hafake"
)

// post sends a family page's form as a browser on Haus's own page does:
// the person (Remote-User; "" for none) and Sec-Fetch-Site same-origin,
// unless hdr says otherwise ("" removes a header).
func post(t *testing.T, h http.HandlerFunc, target string, form url.Values, user string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", target, strings.NewReader(form.Encode()))
	req.Host = "haus.box.test"
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	if user != "" {
		req.Header.Set("Remote-User", user)
	}
	for k, v := range hdr {
		if v == "" {
			req.Header.Del(k)
		} else {
			req.Header.Set(k, v)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// a house to switch: two lamps on, one off, a group, a hidden lamp, a
// player, a lock, a garage door
func switchHouse(t *testing.T) (*hafake.HA, StartConfig) {
	t.Helper()
	fake, hc := house(t)
	fake.SetRegistries(nil, nil, []map[string]any{{"entity_id": "light.alt", "hidden_by": "user"}})
	fake.Set("light.buero", "on", map[string]any{"friendly_name": "Bürolicht"})
	fake.Set("light.kueche", "on", map[string]any{"friendly_name": "Küche"})
	fake.Set("light.flur", "off", map[string]any{"friendly_name": "Flurlicht"})
	fake.Set("light.eg", "on", map[string]any{"friendly_name": "Erdgeschoss", "entity_id": []any{"light.buero", "light.kueche"}})
	fake.Set("light.alt", "on", map[string]any{"friendly_name": "Alte Lampe"})
	fake.Set("switch.kaffee", "on", map[string]any{"friendly_name": "Kaffeemaschine"})
	fake.Set("media_player.wohnzimmer", "playing", map[string]any{"friendly_name": "Wohnzimmer", "supported_features": 1.0})
	fake.Set("lock.haustuer", "locked", map[string]any{"friendly_name": "Haustür"})
	fake.Set("cover.garage", "closed", map[string]any{"friendly_name": "Garage", "device_class": "garage"})
	fake.Set("cover.rollo", "closed", map[string]any{"friendly_name": "Rollo", "device_class": "shutter"})
	return fake, StartConfig{HA: hc}
}

func calls(fake *hafake.HA) []string {
	var out []string
	for _, c := range fake.Calls() {
		id, _ := c.Data["entity_id"].(string)
		out = append(out, c.Domain+"."+c.Service+" "+id)
	}
	return out
}

// haus#35: a family page switches a thing in place — a lamp, a switch, a
// player's pause — through the widgets' own rule (package schalter), and
// lands back where it was (303, so a reload sends nothing again).
func TestSchaltenSwitchesAndGoesBack(t *testing.T) {
	fake, cfg := switchHouse(t)
	h := Schalten(cfg)
	for _, c := range []struct {
		geraet, tun, zurueck, back string
	}{
		{"light.buero", "light.turn_off", "/start", "/start"},
		{"light.flur", "light.turn_on", "/raeume/light.flur", "/raeume/light.flur"},
		{"light.kueche", "light.turn_off", "/raeume#karte-kueche", "/raeume#karte-kueche"}, // a lamp switched in Räume (haus#12)
		{"light.flur", "light.turn_off", "/raeume", "/raeume"},
		{"switch.kaffee", "switch.turn_off", "", "/start"},
		{"media_player.wohnzimmer", "media_player.media_pause", "/start", "/start"},
	} {
		rec := post(t, h, "/schalten", url.Values{"geraet": {c.geraet}, "tun": {c.tun}, "zurueck": {c.zurueck}}, "anna", nil)
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != c.back {
			t.Errorf("%s %s → %d %q, want 303 %s", c.tun, c.geraet, rec.Code, rec.Header().Get("Location"), c.back)
		}
	}
	want := []string{"light.turn_off light.buero", "light.turn_on light.flur", "light.turn_off light.kueche", "light.turn_off light.flur", "switch.turn_off switch.kaffee", "media_player.media_pause media_player.wohnzimmer"}
	if got := calls(fake); !reflect.DeepEqual(got, want) {
		t.Fatalf("HA got %v, want %v", got, want)
	}
}

// "Alle aus" turns off every lamp Start counted as on — not a group, not
// what HA hides — each by its own call, through the same rule.
func TestSchaltenAllLightsOff(t *testing.T) {
	fake, cfg := switchHouse(t)
	rec := post(t, Schalten(cfg), "/schalten", url.Values{"tun": {"alle-lichter-aus"}, "zurueck": {"/start"}}, "anna", nil)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/start" {
		t.Fatalf("→ %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if got, want := calls(fake), []string{"light.turn_off light.buero", "light.turn_off light.kueche"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("HA got %v, want %v", got, want)
	}
}

// What a page may switch is narrower than the widgets' rule, never wider:
// a lamp or a switch on and off, a player's pause. Anything else — a
// lock, a garage door, a cover, playing a URL, a service of another
// domain, another target in the data — is refused and HA hears nothing.
func TestSchaltenRefusesWhatAPageDoesNotSwitch(t *testing.T) {
	fake, cfg := switchHouse(t)
	h := Schalten(cfg)
	for _, f := range []url.Values{
		{"geraet": {"lock.haustuer"}, "tun": {"lock.unlock"}},
		{"geraet": {"lock.haustuer"}, "tun": {"lock.lock"}},
		{"geraet": {"cover.garage"}, "tun": {"cover.open_cover"}},
		{"geraet": {"cover.rollo"}, "tun": {"cover.open_cover"}},
		{"geraet": {"light.buero"}, "tun": {"light.toggle"}},
		{"geraet": {"media_player.wohnzimmer"}, "tun": {"media_player.play_media"}},
		{"geraet": {"media_player.wohnzimmer"}, "tun": {"media_player.turn_off"}},
		{"geraet": {"light.buero"}, "tun": {"switch.turn_off"}},
		{"geraet": {"light.buero"}, "tun": {"homeassistant.turn_off"}},
		{"geraet": {"button.tuer"}, "tun": {"button.press"}},
		{"geraet": {"person.anna"}, "tun": {"person.turn_off"}},
		{"geraet": {"light.Buero"}, "tun": {"light.turn_off"}},
		{"geraet": {""}, "tun": {"light.turn_off"}},
		{"geraet": {"light.buero"}, "tun": {""}},
		{"tun": {"alle-schloesser-auf"}},
	} {
		rec := post(t, h, "/schalten", f, "anna", nil)
		if rec.Code != http.StatusForbidden && rec.Code != http.StatusBadRequest {
			t.Errorf("%v → %d, want a refusal", f, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "<form") {
			t.Errorf("%v: a refusal offers a button", f)
		}
	}
	if len(fake.Calls()) != 0 {
		t.Fatalf("HA got %v", calls(fake))
	}
	// what else the form carries rides nowhere: no other target, no confirmed
	rec := post(t, h, "/schalten", url.Values{"geraet": {"light.buero"}, "tun": {"light.turn_off"}, "entity_id": {"lock.haustuer"},
		"area_id": {"eg"}, "confirmed": {"true"}}, "anna", nil)
	if got := calls(fake); rec.Code != http.StatusSeeOther || !reflect.DeepEqual(got, []string{"light.turn_off light.buero"}) {
		t.Fatalf("→ %d, HA got %v", rec.Code, got)
	}
	if d := fake.Calls()[0].Data; len(d) != 1 {
		t.Fatalf("HA got data %v", d)
	}
	// were a page ever to offer a sensitive action, its form still could not
	// confirm it: a page never says confirmed, the widgets' rule refuses
	pageDoes["lock.unlock"] = true
	defer delete(pageDoes, "lock.unlock")
	rec = post(t, h, "/schalten", url.Values{"geraet": {"lock.haustuer"}, "tun": {"lock.unlock"}, "confirmed": {"true"}}, "anna", nil)
	if rec.Code != http.StatusForbidden || len(fake.Calls()) != 1 {
		t.Fatalf("a confirmed from the form → %d, HA got %v", rec.Code, calls(fake))
	}
}

// Only Haus's own page switches: a POST from another site (Sec-Fetch-Site,
// or an Origin that is not Haus, or neither), without a person, or not a
// POST, is refused and HA hears nothing.
func TestSchaltenOnlyFromHausItself(t *testing.T) {
	fake, cfg := switchHouse(t)
	h := Schalten(cfg)
	f := url.Values{"geraet": {"light.buero"}, "tun": {"light.turn_off"}}
	for _, c := range []struct {
		user string
		hdr  map[string]string
	}{
		{"anna", map[string]string{"Sec-Fetch-Site": "cross-site"}},
		{"anna", map[string]string{"Sec-Fetch-Site": "same-site"}},
		{"anna", map[string]string{"Sec-Fetch-Site": "none"}},
		{"anna", map[string]string{"Sec-Fetch-Site": "", "Origin": "https://boese.example"}},
		{"anna", map[string]string{"Sec-Fetch-Site": "", "Origin": "null"}},
		{"anna", map[string]string{"Sec-Fetch-Site": ""}}, // neither header: Haus's own form always sends one
		{"", nil},
	} {
		if rec := post(t, h, "/schalten", f, c.user, c.hdr); rec.Code != http.StatusForbidden {
			t.Errorf("%q %v → %d, want 403", c.user, c.hdr, rec.Code)
		}
	}
	for _, m := range []string{"GET", "HEAD", "PUT"} {
		req := httptest.NewRequest(m, "/schalten?"+f.Encode(), nil)
		req.Header.Set("Remote-User", "anna")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s → %d, want 405", m, rec.Code)
		}
	}
	if len(fake.Calls()) != 0 {
		t.Fatalf("HA got %v", calls(fake))
	}
	// an older browser without Sec-Fetch-Site, from Haus's own origin, is fine
	if rec := post(t, h, "/schalten", f, "anna", map[string]string{"Sec-Fetch-Site": "", "Origin": "https://haus.box.test"}); rec.Code != http.StatusSeeOther {
		t.Fatalf("same origin → %d", rec.Code)
	}
}

// Back leads only to Haus's own Start, Räume (a room on it), or the
// thing's page: anything else lands on Start.
func TestSchaltenGoesBackOnlyToHaus(t *testing.T) {
	_, cfg := switchHouse(t)
	h := Schalten(cfg)
	for _, back := range []string{"https://boese.example/", "//boese.example", "/raeume/../box", "/box", "/raeume/lock.haustuer?x", "javascript:alert(1)", "/start#x",
		"/raeume/person.anna", "/raeume/automation.alles", "/raeume/", "/raeume#x", "/raeume#karte-", `/raeume#karte-"><b>`, "/raeume#karte-K",
		"/raeume?x", "/raeume/#karte-kueche", "/raeume#karte-kueche#x", "/raeume#karte-kueche/../box"} {
		rec := post(t, h, "/schalten", url.Values{"geraet": {"light.buero"}, "tun": {"light.turn_off"}, "zurueck": {back}}, "anna", nil)
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/start" {
			t.Errorf("zurueck %q → %d %q, want /start", back, rec.Code, rec.Header().Get("Location"))
		}
	}
}

// HA not answering a switch is one plain sentence and the way back —
// never HA's detail, its address, or a 500.
func TestSchaltenSaysPlainlyWhenHADoesNotAnswer(t *testing.T) {
	fake, cfg := switchHouse(t)
	fake.FailCalls(500)
	rec := post(t, Schalten(cfg), "/schalten", url.Values{"geraet": {"light.buero"}, "tun": {"light.turn_off"}, "zurueck": {"/raeume/light.buero"}}, "anna", nil)
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("→ %d", rec.Code)
	}
	mustContain(t, body, "Das hat nicht geklappt: Licht und Geräte antworten gerade nicht.", "<p><a href=/raeume/light.buero>Zurück</a></p>", "<nav class=tabs")
	mustNotLeak(t, body, "some detail", "service failed", host(cfg.HA.URL))
	fake.FailCalls(0)
	fake.Down(true)
	rec = post(t, Schalten(cfg), "/schalten", url.Values{"tun": {"alle-lichter-aus"}}, "anna", nil)
	mustContain(t, rec.Body.String(), "Das hat nicht geklappt: Licht und Geräte antworten gerade nicht.", "<p><a href=/start>Zurück</a></p>")
	mustNotLeak(t, rec.Body.String())
}

// Without the house configured there is nothing to switch: Start.
func TestSchaltenWithoutHA(t *testing.T) {
	rec := post(t, Schalten(StartConfig{}), "/schalten", url.Values{"geraet": {"light.buero"}, "tun": {"light.turn_off"}}, "anna", nil)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/start" {
		t.Fatalf("→ %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

// haus#35: a thing's own page pins it to the person's Start with ☆ and
// takes it off again, as a plain form (no script needed); the page says
// where it then shows. A lamp's page switches it, too.
func TestDingPinsAndUnpins(t *testing.T) {
	fake, cfg := switchHouse(t)
	_ = fake
	cfg.Favorites = favorites(t, `{"anna": ["switch.kaffee"], "ben": ["light.buero"]}`)
	page := func(user string) string {
		_, body, _ := familyPageAs(t, Ding, cfg, "GET", "/raeume/light.buero", user, "family")
		return body
	}
	body := page("anna")
	mustContain(t, body, `<form method=post action="/raeume/light.buero">`, `name=stern value=an`, "☆ Auf Start zeigen",
		"Dann steht es auf Start unter „Meine Geräte“.",
		`<form method=post action=/schalten>`, `name=tun value="light.turn_off"`, `name=zurueck value="/raeume/light.buero"`, ">Ausschalten</button>")
	mustNotContain(t, body, "<script", "★")

	rec := post(t, Ding(cfg), "/raeume/light.buero", url.Values{"stern": {"an"}}, "anna", nil)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/raeume/light.buero" {
		t.Fatalf("pin → %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if got, _ := cfg.Favorites.For("anna"); !reflect.DeepEqual(got, []string{"switch.kaffee", "light.buero"}) {
		t.Fatalf("anna: %v", got)
	}
	body = page("anna")
	mustContain(t, body, `name=stern value=aus`, "★ Von Start nehmen", "Steht auf Start unter „Meine Geräte“.")
	mustNotContain(t, body, "☆ Auf Start zeigen")
	_, start := startPage(t, cfg, "family")
	mustContain(t, section(t, start, "karte-meine-geraete"), `<a href="/raeume/light.buero">Bürolicht</a>`)

	rec = post(t, Ding(cfg), "/raeume/light.buero", url.Values{"stern": {"aus"}}, "anna", nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("unpin → %d", rec.Code)
	}
	if got, _ := cfg.Favorites.For("anna"); !reflect.DeepEqual(got, []string{"switch.kaffee"}) {
		t.Fatalf("anna after unpinning: %v", got)
	}
	if got, _ := cfg.Favorites.For("ben"); !reflect.DeepEqual(got, []string{"light.buero"}) {
		t.Fatalf("ben's list changed: %v", got)
	}
	if len(fake.Calls()) != 0 {
		t.Fatalf("pinning switched something: %v", calls(fake))
	}
}

// Pinning is the person's own and only from Haus's page: without a
// person, from another site, for what has no page, or with a word the
// form does not send, nothing is written. Without a store there is no ☆,
// and the page takes no POST.
func TestDingPinIsGuarded(t *testing.T) {
	_, cfg := switchHouse(t)
	cfg.Favorites = favorites(t, `{"anna": ["switch.kaffee"]}`)
	h := Ding(cfg)
	for _, c := range []struct {
		path string
		form url.Values
		user string
		hdr  map[string]string
		want int
	}{
		{"/raeume/light.buero", url.Values{"stern": {"an"}}, "", nil, http.StatusForbidden},
		{"/raeume/light.buero", url.Values{"stern": {"an"}}, "anna", map[string]string{"Sec-Fetch-Site": "cross-site"}, http.StatusForbidden},
		{"/raeume/light.buero", url.Values{"stern": {"an"}}, "anna", map[string]string{"Sec-Fetch-Site": "", "Origin": "https://boese.example"}, http.StatusForbidden},
		{"/raeume/light.buero", url.Values{"stern": {"an"}}, "anna", map[string]string{"Sec-Fetch-Site": ""}, http.StatusForbidden},
		{"/raeume/person.anna", url.Values{"stern": {"an"}}, "anna", nil, http.StatusNotFound},
		{"/raeume/Light.Buero", url.Values{"stern": {"an"}}, "anna", nil, http.StatusNotFound},
		{"/raeume/light.buero", url.Values{"stern": {"ja"}}, "anna", nil, http.StatusBadRequest},
		{"/raeume/light.buero", url.Values{}, "anna", nil, http.StatusBadRequest},
	} {
		if rec := post(t, h, c.path, c.form, c.user, c.hdr); rec.Code != c.want {
			t.Errorf("%s %v as %q %v → %d, want %d", c.path, c.form, c.user, c.hdr, rec.Code, c.want)
		}
	}
	if got, _ := cfg.Favorites.For("anna"); !reflect.DeepEqual(got, []string{"switch.kaffee"}) {
		t.Fatalf("anna: %v", got)
	}
	cfg.Favorites = nil
	_, body, _ := familyPage(t, Ding, cfg, "GET", "/raeume/light.buero", "family")
	mustNotContain(t, body, "☆", "★", "name=stern")
	if rec := post(t, Ding(cfg), "/raeume/light.buero", url.Values{"stern": {"an"}}, "anna", nil); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("without a store → %d, want 405", rec.Code)
	}
	// nobody asking sees no ☆: there is no list to pin to
	cfg.Favorites = favorites(t, "")
	_, body, _ = familyPageAs(t, Ding, cfg, "GET", "/raeume/light.buero", "", "")
	mustNotContain(t, body, "☆", "name=stern")
}

// A favorites file that does not read is not written over; the page says
// so plainly, and the way back.
func TestDingPinSaysABrokenFilePlainly(t *testing.T) {
	_, cfg := switchHouse(t)
	cfg.Favorites = favorites(t, `{"anna": [`)
	rec := post(t, Ding(cfg), "/raeume/light.buero", url.Values{"stern": {"an"}}, "anna", nil)
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("→ %d", rec.Code)
	}
	mustContain(t, body, "Deine Favoriten lassen sich gerade nicht lesen.", "<p><a href=/raeume/light.buero>Zurück zum Gerät</a></p>")
	mustNotLeak(t, body, "favoriten.json", "unexpected", "JSON")
	if _, err := cfg.Favorites.For("anna"); err == nil {
		t.Fatal("the broken file was written over")
	}
	// from Räume, the way back is to the room (haus#12)
	rec = post(t, Ding(cfg), "/raeume/light.buero", url.Values{"stern": {"an"}, "zurueck": {"/raeume#karte-buero"}}, "anna", nil)
	mustContain(t, rec.Body.String(), "Deine Favoriten lassen sich gerade nicht lesen.", "<p><a href=/raeume#karte-buero>Zurück zu den Räumen</a></p>")
	if _, err := cfg.Favorites.For("anna"); err == nil {
		t.Fatal("the broken file was written over")
	}
}

// familyPageAs is familyPage for one person ("" for nobody).
func familyPageAs(t *testing.T, page func(StartConfig) http.HandlerFunc, cfg StartConfig, method, target, user, groups string) (int, string, http.Header) {
	t.Helper()
	if cfg.Loc == nil {
		cfg.Loc = berlin(t)
	}
	if cfg.Now == nil {
		now := startNow(t)
		cfg.Now = func() time.Time { return now }
	}
	req := httptest.NewRequest(method, target, nil)
	if user != "" {
		req.Header.Set("Remote-User", user)
	}
	if groups != "" {
		req.Header.Set("Remote-Groups", groups)
	}
	rec := httptest.NewRecorder()
	page(cfg).ServeHTTP(rec, req)
	return rec.Code, rec.Body.String(), rec.Header()
}
