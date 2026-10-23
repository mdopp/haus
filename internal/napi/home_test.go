package napi

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mdopp/haus/internal/ha"
	"github.com/mdopp/haus/internal/ha/hafake"
	"github.com/mdopp/haus/internal/laib"
	"github.com/mdopp/haus/internal/schalter"
)

// haus#10: Haus serves the house paths itself, from Home Assistant, in the
// shapes the app parses (the app's data/ApiClient.kt, RealtimeProtocol.kt)
// and with the two structural rules for /napi/ha/call.

func house(t *testing.T) (*hafake.HA, http.Handler) {
	t.Helper()
	return houseWith(t, nil)
}

func houseWith(t *testing.T, tune func(*Config)) (*hafake.HA, http.Handler) {
	t.Helper()
	fake := hafake.New("ha-token")
	t.Cleanup(fake.Close)
	fake.Set("light.kueche", "on", map[string]any{"friendly_name": "Küche", "brightness": 128, "rgb_color": []int{255, 128, 0},
		"supported_color_modes": []string{"rgb"}, "color_mode": "rgb", "supported_features": 44})
	fake.Set("light.flur", "off", map[string]any{"friendly_name": "Flur"})
	fake.Set("switch.kaffee", "on", map[string]any{"friendly_name": "Kaffeemaschine"})
	fake.Set("cover.rollo", "closed", map[string]any{"friendly_name": "Rollo", "device_class": "shutter", "current_position": 0})
	fake.Set("cover.garage", "open", map[string]any{"friendly_name": "Garagentor", "device_class": "garage"})
	fake.Set("cover.ohne", "closed", map[string]any{"friendly_name": "Ohne Klasse"})
	fake.Set("lock.haustuer", "locked", map[string]any{"friendly_name": "Haustür", "supported_features": 1})
	fake.Set("climate.bad", "heat", map[string]any{"friendly_name": "Bad", "current_temperature": 21.5, "temperature": 22})
	fake.Set("sensor.temp", "21.5", map[string]any{"friendly_name": "Temperatur", "device_class": "temperature", "unit_of_measurement": "°C"})
	fake.Set("button.tuer", "unknown", map[string]any{"friendly_name": "Türöffner"})
	var calls []string
	lb := fakeLaib(t, &calls)
	t.Cleanup(lb.Close)
	fake.Set("cover.tor_gross", "closed", map[string]any{"friendly_name": "Tor", "device_class": "Garage"})
	cfg := Config{Laib: &laib.Client{URL: lb.URL, Token: "ops"}, Validate: StaticValidator([]string{"dev1", "dev2"}),
		HA: &ha.Client{URL: fake.URL, Token: "ha-token"}, Ping: 50 * time.Millisecond}
	if tune != nil {
		tune(&cfg)
	}
	return fake, Handler(cfg)
}

func raw(t *testing.T, h http.Handler, method, path, bearer, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func TestEveryHousePathWantsAPairedDevice(t *testing.T) {
	fake, h := house(t)
	for _, p := range []struct{ method, path, body string }{
		{"POST", "/napi/ha/call", `{"entity_id":"light.kueche","service":"light.toggle"}`},
		{"GET", "/napi/portal/state?entity_id=light.kueche", ""},
		{"GET", "/napi/portal/energy", ""},
		{"GET", "/napi/portal/entity-history?entity_id=sensor.temp&range=24h", ""},
		{"GET", "/napi/portal/active", ""},
		{"POST", "/napi/portal/watch", `{"entity_ids":["light.kueche"]}`},
		{"GET", "/napi/portal/events", ""},
	} {
		for _, bearer := range []string{"", "wrong"} {
			if code, _ := raw(t, h, p.method, p.path, bearer, p.body); code != 401 {
				t.Errorf("%s %s with %q → %d, want 401", p.method, p.path, bearer, code)
			}
		}
	}
	if n := len(fake.Calls()); n != 0 {
		t.Fatalf("HA was called %d times without a token", n)
	}
}

func TestHACallRules(t *testing.T) {
	fake, h := house(t)
	cases := []struct {
		body string
		want int
		why  string
	}{
		{`{"entity_id":"light.kueche","service":"light.toggle"}`, 200, "a light toggles on a tap"},
		{`{"entity_id":"switch.kaffee","service":"switch.turn_off"}`, 200, "a switch too"},
		{`{"entity_id":"climate.bad","service":"climate.set_temperature","data":{"temperature":21}}`, 200, "climate takes data"},
		{`{"entity_id":"cover.rollo","service":"cover.open_cover"}`, 200, "a shutter opens without asking"},
		{`{"entity_id":"cover.garage","service":"cover.close_cover"}`, 200, "closing the garage is not sensitive"},
		{`{"entity_id":"cover.garage","service":"cover.open_cover"}`, 403, "a garage open needs confirmed"},
		{`{"entity_id":"cover.garage","service":"cover.toggle"}`, 403, "a garage toggle may open it"},
		{`{"entity_id":"cover.garage","service":"cover.set_cover_position","data":{"position":50}}`, 403, "so may a position"},
		{`{"entity_id":"cover.garage","service":"cover.open_cover","confirmed":true}`, 200, "confirmed, it opens"},
		{`{"entity_id":"cover.ohne","service":"cover.open_cover"}`, 403, "no device_class fails closed"},
		{`{"entity_id":"cover.gibtsnicht","service":"cover.open_cover"}`, 403, "an unknown cover fails closed"},
		{`{"entity_id":"lock.haustuer","service":"lock.unlock"}`, 403, "any lock needs confirmed"},
		{`{"entity_id":"lock.haustuer","service":"lock.lock"}`, 403, "locking too"},
		{`{"entity_id":"lock.haustuer","service":"lock.unlock","confirmed":true}`, 200, "confirmed, it unlocks"},
		{`{"entity_id":"sensor.temp","service":"sensor.turn_on"}`, 400, "sensor is not a domain the card may act on"},
		{`{"entity_id":"button.tuer","service":"button.press"}`, 400, "button stays off the list"},
		{`{"entity_id":"alarm_control_panel.haus","service":"alarm_control_panel.alarm_disarm"}`, 400, "no alarm"},
		{`{"entity_id":"light.kueche","service":"switch.toggle"}`, 400, "the service's domain must equal the entity's"},
		{`{"entity_id":"cover.rollo","service":"lock.unlock","confirmed":true}`, 400, "even confirmed"},
		{`{"entity_id":"light.kueche","service":"toggle"}`, 400, "a service is dotted"},
		{`{"entity_id":"light.kueche","service":"light.../../x"}`, 400, "no path in the service"},
		{`{"entity_id":"Light.Kueche","service":"light.toggle"}`, 400, "an entity id is lower case"},
		{`{"entity_id":"cover.rollo","service":"cover.open_cover","data":{"entity_id":"cover.garage"}}`, 400, "data cannot name another target"},
		{`{"entity_id":"cover.rollo","service":"cover.open_cover","data":{"area_id":"garage"}}`, 400, "nor an area"},
		{`{"entity_id":"cover.rollo","service":"cover.open_cover","data":{"device_id":"abc"}}`, 400, "nor a device"},
		{`{"entity_id":"cover.rollo","service":"cover.open_cover","data":{"floor_id":"eg"}}`, 400, "nor a floor"},
		{`{"entity_id":"cover.rollo","service":"cover.open_cover","data":{"label_id":"tore"}}`, 400, "nor a label"},
		{`{"entity_id":"cover.rollo","service":"cover.open_cover","data":{"target":{"entity_id":"cover.garage"}}}`, 400, "nor a target block"},
		{`{"entity_id":"cover.garage","service":"cover.set_cover_tilt_position","data":{"tilt_position":50}}`, 403, "a garage tilt needs confirmed"},
		{`{"entity_id":"cover.garage","service":"cover.open_cover_tilt"}`, 403, "a garage tilt open too"},
		{`{"entity_id":"cover.garage","service":"cover.toggle_cover_tilt"}`, 403, "a garage tilt toggle too: only closing and stopping are free"},
		{`{"entity_id":"cover.garage","service":"cover.stop_cover"}`, 200, "stopping the garage is free"},
		{`{"entity_id":"cover.garage","service":"cover.close_cover_tilt"}`, 200, "closing its tilt is free"},
		{`{"entity_id":"cover.ohne","service":"cover.close_cover"}`, 200, "closing an unknown cover is free"},
		{`{"entity_id":"cover.ohne","service":"cover.toggle_cover_tilt"}`, 403, "anything else on an unknown cover fails closed"},
		{`{"entity_id":"cover.tor_gross","service":"cover.open_cover"}`, 403, "device_class is compared without case"},
		{`{"entity_id":"cover.rollo","service":"cover.toggle_cover_tilt"}`, 200, "a shutter's tilt is free"},
		{`not json`, 400, "bad json"},
	}
	for _, c := range cases {
		before := len(fake.Calls())
		code, body := raw(t, h, "POST", "/napi/ha/call", "dev1", c.body)
		if code != c.want {
			t.Errorf("%s: %s → %d %s, want %d", c.why, c.body, code, body, c.want)
			continue
		}
		var out map[string]any
		json.Unmarshal([]byte(body), &out)
		switch code {
		case 200:
			if out["ok"] != true || len(fake.Calls()) != before+1 {
				t.Errorf("%s: %s → %s, calls %d→%d", c.why, c.body, body, before, len(fake.Calls()))
			}
		case 403:
			if out["error"] != "sensitive_action" || len(fake.Calls()) != before {
				t.Errorf("%s: 403 must say sensitive_action and call nothing: %s", c.why, body)
			}
		default:
			if out["ok"] != false || len(fake.Calls()) != before {
				t.Errorf("%s: a refusal must call nothing: %s", c.why, body)
			}
		}
	}
	calls := fake.Calls()
	first := calls[0]
	if first.Domain != "light" || first.Service != "toggle" || first.Data["entity_id"] != "light.kueche" {
		t.Fatalf("the call HA got: %+v", first)
	}
	if c := calls[2]; c.Domain != "climate" || c.Service != "set_temperature" || c.Data["temperature"] != 21.0 || c.Data["entity_id"] != "climate.bad" {
		t.Fatalf("data rides along: %+v", c)
	}
}

// HA's own trouble is a 502 and its detail stays in Haus's log; only HA's
// 400 (a call HA refuses) is the caller's 400.
func TestHACallWhenHAFails(t *testing.T) {
	fake, h := house(t)
	toggle := `{"entity_id":"light.kueche","service":"light.toggle"}`
	fake.FailCalls(400)
	if code, body := raw(t, h, "POST", "/napi/ha/call", "dev1", toggle); code != 400 || strings.Contains(body, "some detail") {
		t.Fatalf("HA 400 → %d %s, want 400 without HA's detail", code, body)
	}
	for _, status := range []int{401, 403, 500} {
		fake.FailCalls(status)
		code, body := raw(t, h, "POST", "/napi/ha/call", "dev1", toggle)
		if code != 502 || !strings.Contains(body, `"ha_unavailable"`) || strings.Contains(body, "some detail") {
			t.Fatalf("HA %d → %d %s, want 502 ha_unavailable", status, code, body)
		}
	}
	fake.FailCalls(0)
	_, wrong := houseWith(t, func(c *Config) { c.HA = &ha.Client{URL: fake.URL, Token: "wrong"} })
	if code, body := raw(t, wrong, "POST", "/napi/ha/call", "dev1", toggle); code != 502 || strings.Contains(body, "401") {
		t.Fatalf("a wrong HA token → %d %s, want 502", code, body)
	}
	fake.Down(true)
	if code, _ := raw(t, h, "POST", "/napi/ha/call", "dev1", toggle); code != 502 {
		t.Fatalf("HA down → %d, want 502", code)
	}
}

func TestPortalStateIsTheCard(t *testing.T) {
	_, h := house(t)
	code, body := raw(t, h, "GET", "/napi/portal/state?entity_id=light.kueche", "dev1", "")
	if code != 200 {
		t.Fatalf("state: %d %s", code, body)
	}
	var out struct {
		OK   bool           `json:"ok"`
		Card map[string]any `json:"card"`
	}
	json.Unmarshal([]byte(body), &out)
	c := out.Card
	if !out.OK || c["entity_id"] != "light.kueche" || c["name"] != "Küche" || c["domain"] != "light" || c["state"] != "on" ||
		c["brightness"] != 128.0 || c["color_mode"] != "rgb" || c["supported_features"] != 44.0 {
		t.Fatalf("card: %s", body)
	}
	if rgb, _ := c["rgb_color"].([]any); len(rgb) != 3 || rgb[0] != 255.0 {
		t.Fatalf("rgb_color: %v", c["rgb_color"])
	}
	// HA's last_updated as epoch milliseconds: the app's staleness guard
	if c["updated_at_ms"] != float64(time.Date(2026, 10, 14, 10, 0, 0, 123000000, time.UTC).UnixMilli()) {
		t.Fatalf("updated_at_ms: %v", c["updated_at_ms"])
	}
	// Android's optString turns a JSON null into "null": an absent value is absent
	if strings.Contains(body, "null") {
		t.Fatalf("a null on the wire: %s", body)
	}
	_, body = raw(t, h, "GET", "/napi/portal/state?entity_id=sensor.temp", "dev1", "")
	if !strings.Contains(body, `"unit":"°C"`) || !strings.Contains(body, `"device_class":"temperature"`) || !strings.Contains(body, `"state":"21.5"`) {
		t.Fatalf("sensor card: %s", body)
	}
	_, body = raw(t, h, "GET", "/napi/portal/state?entity_id=climate.bad", "dev1", "")
	if !strings.Contains(body, `"current_temperature":21.5`) {
		t.Fatalf("climate card: %s", body)
	}
	if code, _ := raw(t, h, "GET", "/napi/portal/state?entity_id=nonsense", "dev1", ""); code != 400 {
		t.Fatalf("bad id → %d, want 400", code)
	}
	if code, _ := raw(t, h, "GET", "/napi/portal/state?entity_id=light.gibtsnicht", "dev1", ""); code == 200 {
		t.Fatalf("unknown entity answered 200")
	}
}

func TestPortalEnergy(t *testing.T) {
	fake, h := house(t)
	power := func(name, w string) map[string]any {
		return map[string]any{"friendly_name": name, "device_class": "power", "unit_of_measurement": "W"}
	}
	fake.Set("sensor.pv_now", "3200", power("Aktuell erzeugter PV-Strom", ""))
	fake.Set("sensor.house_now", "800", power("Aktueller Hausverbrauch", ""))
	fake.Set("sensor.grid_now", "-2100", power("Aktuelle Netz Leistung", ""))
	fake.Set("sensor.batt_now", "-300", power("Aktuelle Akku Leistung", ""))
	fake.Set("sensor.waschmaschine", "450", power("Waschmaschine", ""))
	fake.Set("sensor.pv_total", "12345.6", map[string]any{"friendly_name": "PV Erzeugung", "device_class": "energy", "unit_of_measurement": "kWh"})
	fake.Set("sensor.bezug_total", "4321", map[string]any{"friendly_name": "Netzbezug", "device_class": "energy", "unit_of_measurement": "kWh"})
	code, body := raw(t, h, "GET", "/napi/portal/energy", "dev1", "")
	var out struct {
		OK     bool `json:"ok"`
		Energy struct {
			Flow     []map[string]any `json:"flow"`
			Totals   []map[string]any `json:"totals"`
			Circuits []map[string]any `json:"circuits"`
		} `json:"energy"`
	}
	json.Unmarshal([]byte(body), &out)
	if code != 200 || !out.OK || len(out.Energy.Flow) != 4 {
		t.Fatalf("energy: %d %s", code, body)
	}
	want := [][3]string{{"PV", "supply", "3200"}, {"Haus", "draw", "800"}, {"Netz", "grid", "-2100"}, {"Akku", "battery", "-300"}}
	for i, w := range want {
		f := out.Energy.Flow[i]
		if f["label"] != w[0] || f["sense"] != w[1] || f["state"] != w[2] || f["unit"] != "W" || f["entity_id"] == "" {
			t.Errorf("flow[%d] = %v, want %v", i, f, w)
		}
	}
	if len(out.Energy.Totals) != 2 || out.Energy.Totals[0]["label"] != "PV-Erzeugung" || out.Energy.Totals[0]["state"] != "12345.6" ||
		out.Energy.Totals[0]["unit"] != "kWh" || out.Energy.Totals[1]["label"] != "Netzbezug" {
		t.Fatalf("totals: %v", out.Energy.Totals)
	}
	if len(out.Energy.Circuits) != 1 || out.Energy.Circuits[0]["name"] != "Waschmaschine" {
		t.Fatalf("circuits: %v", out.Energy.Circuits)
	}
	fake.Down(true)
	if code, _ := raw(t, h, "GET", "/napi/portal/energy", "dev1", ""); code != 502 {
		t.Fatalf("HA down → %d, want 502", code)
	}
}

func TestEntityHistory(t *testing.T) {
	fake, h := house(t)
	fake.SetHistory("sensor.temp", []map[string]any{
		{"entity_id": "sensor.temp", "state": "20.5", "last_changed": "2026-10-13T10:00:00+00:00", "attributes": map[string]any{}},
		{"state": "21.0", "last_changed": "2026-10-13T12:00:00+00:00"},
		{"state": "unavailable", "last_updated": "2026-10-13T13:00:00+00:00"},
	})
	code, body := raw(t, h, "GET", "/napi/portal/entity-history?entity_id=sensor.temp&range=7d", "dev1", "")
	var out struct {
		OK      bool             `json:"ok"`
		History []map[string]any `json:"history"`
	}
	json.Unmarshal([]byte(body), &out)
	if code != 200 || !out.OK || len(out.History) != 3 || out.History[0]["state"] != "20.5" || out.History[0]["t"] != "2026-10-13T10:00:00+00:00" ||
		out.History[2]["t"] != "2026-10-13T13:00:00+00:00" {
		t.Fatalf("history: %d %s", code, body)
	}
	starts, queries := fake.HistoryQueries()
	q := queries[0]
	if q.Get("filter_entity_id") != "sensor.temp" || q.Get("minimal_response") == "" || q.Get("no_attributes") == "" {
		t.Fatalf("query: %v", q)
	}
	start, err1 := time.Parse(time.RFC3339, starts[0])
	end, err2 := time.Parse(time.RFC3339, q.Get("end_time"))
	if err1 != nil || err2 != nil || end.Sub(start) != 168*time.Hour {
		t.Fatalf("7d window: %s → %s (%v %v)", starts[0], q.Get("end_time"), err1, err2)
	}
	for r, hours := range map[string]time.Duration{"24h": 24, "48h": 48} {
		raw(t, h, "GET", "/napi/portal/entity-history?entity_id=sensor.temp&range="+r, "dev1", "")
		starts, queries = fake.HistoryQueries()
		s, _ := time.Parse(time.RFC3339, starts[len(starts)-1])
		e, _ := time.Parse(time.RFC3339, queries[len(queries)-1].Get("end_time"))
		if e.Sub(s) != hours*time.Hour {
			t.Fatalf("%s window: %v", r, e.Sub(s))
		}
	}
	_, body = raw(t, h, "GET", "/napi/portal/entity-history?entity_id=light.flur&range=24h", "dev1", "")
	if body = strings.TrimSpace(body); body != `{"history":[],"ok":true}` && body != `{"ok":true,"history":[]}` {
		t.Fatalf("no history is an empty list: %s", body)
	}
	if code, _ := raw(t, h, "GET", "/napi/portal/entity-history?entity_id=sensor.temp&range=1y", "dev1", ""); code != 400 {
		t.Fatalf("bad range → %d", code)
	}
	if code, _ := raw(t, h, "GET", "/napi/portal/entity-history?entity_id=..%2F&range=24h", "dev1", ""); code != 400 {
		t.Fatalf("bad id → %d", code)
	}
}

func TestPortalActive(t *testing.T) {
	fake, h := house(t)
	fake.SetRegistries(
		[]map[string]any{{"area_id": "kueche", "name": "Küche"}, {"area_id": "garage", "name": "Garage"}},
		[]map[string]any{{"id": "dev-tor", "area_id": "garage"}},
		[]map[string]any{{"entity_id": "light.kueche", "area_id": "kueche"}, {"entity_id": "cover.garage", "device_id": "dev-tor"}},
	)
	code, body := raw(t, h, "GET", "/napi/portal/active", "dev1", "")
	var out struct {
		OK     bool             `json:"ok"`
		Active []map[string]any `json:"active"`
	}
	json.Unmarshal([]byte(body), &out)
	if code != 200 || !out.OK {
		t.Fatalf("active: %d %s", code, body)
	}
	got := map[string]map[string]any{}
	for _, a := range out.Active {
		got[a["entity_id"].(string)] = a
	}
	if len(got) != 3 || got["light.kueche"] == nil || got["switch.kaffee"] == nil || got["cover.garage"] == nil {
		t.Fatalf("on and open, nothing else: %s", body)
	}
	if k := got["light.kueche"]; k["room"] != "Küche" || k["name"] != "Küche" || k["domain"] != "light" || k["state"] != "on" || k["brightness"] != 128.0 {
		t.Fatalf("light: %v", k)
	}
	if g := got["cover.garage"]; g["room"] != "Garage" || g["device_class"] != "garage" {
		t.Fatalf("a device's area is the entity's room: %v", g)
	}
	if s, ok := got["switch.kaffee"]["room"]; !ok || s != "" {
		t.Fatalf("no area is an empty room: %v", got["switch.kaffee"])
	}
}

// the realtime stream: a device's watch-set decides which card_state frames
// it gets, from HA's state_changed over the WebSocket. Nothing here waits
// on the clock (haus#39): a stream that answered is listening, a frame that
// must come is waited for with patience, and a frame that must not come is
// ruled out by one that comes after it on the same stream.
func TestWatchAndEvents(t *testing.T) {
	fake, h := house(t)
	srv := httptest.NewServer(h)
	defer srv.Close()
	if code, body := raw(t, h, "POST", "/napi/portal/watch", "dev1", `{"entity_ids":["light.kueche","cover.rollo"]}`); code != 200 || !strings.Contains(body, `"ok":true`) {
		t.Fatalf("watch: %d %s", code, body)
	}
	open := func(bearer string) (*http.Response, <-chan string) {
		req, _ := http.NewRequest("GET", srv.URL+"/napi/portal/events", nil)
		req.Header.Set("Authorization", "Bearer "+bearer)
		req.Header.Set("Accept", "text/event-stream")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		lines := make(chan string, 100)
		go func() {
			sc := bufio.NewScanner(resp.Body)
			for sc.Scan() {
				lines <- sc.Text()
			}
			close(lines)
		}()
		return resp, lines
	}
	r1, l1 := open("dev1")
	defer r1.Body.Close()
	r2, l2 := open("dev2")
	defer r2.Body.Close()
	if r1.StatusCode != 200 || !strings.HasPrefix(r1.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("events: %d %s", r1.StatusCode, r1.Header.Get("Content-Type"))
	}
	if !fake.WaitSubscribed(1) {
		t.Fatal("Haus never subscribed to state_changed")
	}
	// once subscribed, Haus sends each watched entity's current card
	resync := readFrames(t, l1, 2, patience)
	if len(resync) != 2 || !strings.Contains(resync[0].data+resync[1].data, `"state":"on"`) || !strings.Contains(resync[0].data+resync[1].data, `"cover.rollo"`) {
		t.Fatalf("resync on subscribe: %+v", resync)
	}
	fake.Change("light.flur", "on", map[string]any{"friendly_name": "Flur"}) // nobody watches it
	fake.Change("light.kueche", "off", map[string]any{"friendly_name": "Küche"})
	fake.Change("light.kueche", "off", map[string]any{"friendly_name": "Küche"}) // the same card again: nothing new
	fake.Change("cover.rollo", "open", map[string]any{"friendly_name": "Rollo", "device_class": "shutter", "current_position": 100})

	frames := readFrames(t, l1, 2, patience)
	if frames[0].event != "card_state" || frames[1].event != "card_state" {
		t.Fatalf("frames: %+v", frames)
	}
	var f struct {
		EntityID string         `json:"entity_id"`
		Card     map[string]any `json:"card"`
	}
	json.Unmarshal([]byte(frames[0].data), &f)
	if f.EntityID != "light.kueche" || f.Card["entity_id"] != "light.kueche" || f.Card["state"] != "off" || f.Card["name"] != "Küche" {
		t.Fatalf("first frame: %s", frames[0].data)
	}
	json.Unmarshal([]byte(frames[1].data), &f)
	if f.EntityID != "cover.rollo" || f.Card["current_position"] != 100.0 {
		t.Fatalf("second frame (the duplicate kitchen card must be dropped): %s", frames[1].data)
	}
	// dev2 watched nothing while those changed: only keepalive comments
	// reached it. One stream's frames keep their order, so the first frame
	// it gets once it watches the coffee machine is that one, or another
	// device's frame came before it.
	raw(t, h, "POST", "/napi/portal/watch", "dev2", `{"entity_ids":["switch.kaffee"]}`)
	fake.Change("switch.kaffee", "off", map[string]any{"friendly_name": "Kaffeemaschine"})
	got := readFrames(t, l2, 1, patience)
	if len(got) == 1 {
		json.Unmarshal([]byte(got[0].data), &f)
	}
	if len(got) != 1 || f.EntityID != "switch.kaffee" {
		t.Fatalf("dev2 got another device's frames: %+v", got)
	}
	if !sawPing(l2, patience) {
		t.Fatal("no ': ping' keepalive comment")
	}
	// a watch is replaced, not added to
	raw(t, h, "POST", "/napi/portal/watch", "dev1", `{"entity_ids":["light.flur"]}`)
	fake.Change("light.kueche", "on", map[string]any{"friendly_name": "Küche"})
	fake.Change("light.flur", "off", map[string]any{"friendly_name": "Flur"})
	frames = readFrames(t, l1, 1, patience)
	if len(frames) != 1 || !strings.Contains(frames[0].data, `"light.flur"`) {
		t.Fatalf("after replacing the watch-set: %+v", frames)
	}
}

// patience bounds a wait for something that must come: it ends as soon as
// it came, so only a broken stream ever waits it out, however busy the CPU.
const patience = 30 * time.Second

// haus#28: a device that shows the active devices says so in its watch
// (active: true) and gets an active_set frame whenever the set of things
// that are on or open changes, whichever entity it was, watched or not:
// once when Haus subscribes, then at most every SensorEvery, the latest
// set always. The frame names the set; the app re-reads
// /napi/portal/active. A change inside the set (a brightness) is no frame,
// and a device that did not ask gets none.
func TestActiveSetEvents(t *testing.T) {
	fake, h := houseWith(t, func(c *Config) { c.SensorEvery = 200 * time.Millisecond })
	srv := httptest.NewServer(h)
	defer srv.Close()
	if code, body := raw(t, h, "POST", "/napi/portal/watch", "dev1", `{"entity_ids":[],"active":true}`); code != 200 {
		t.Fatalf("watch: %d %s", code, body)
	}
	raw(t, h, "POST", "/napi/portal/watch", "dev2", `{"entity_ids":["light.flur"]}`)
	r1, l1 := stream(t, srv.URL, "dev1")
	defer r1.Body.Close()
	r2, l2 := stream(t, srv.URL, "dev2")
	defer r2.Body.Close()
	if !fake.WaitSubscribed(1) {
		t.Fatal("Haus never subscribed to state_changed")
	}
	set := func(f []frame) []string {
		t.Helper()
		if len(f) != 1 || f[0].event != "active_set" {
			t.Fatalf("want one active_set frame, got %+v", f)
		}
		var d struct {
			EntityIDs []string `json:"entity_ids"`
		}
		if err := json.Unmarshal([]byte(f[0].data), &d); err != nil {
			t.Fatalf("active_set data: %s", f[0].data)
		}
		return d.EntityIDs
	}
	if got := strings.Join(set(readFrames(t, l1, 1, 2*time.Second)), " "); got != "cover.garage light.kueche switch.kaffee" {
		t.Fatalf("the set on subscribe: %q", got)
	}
	fake.Change("light.kueche", "on", map[string]any{"friendly_name": "Küche", "brightness": 200}) // still on
	fake.Change("light.flur", "on", map[string]any{"friendly_name": "Flur"})                       // dev1 watches no entity
	if got := strings.Join(set(readFrames(t, l1, 1, 2*time.Second)), " "); got != "cover.garage light.flur light.kueche switch.kaffee" {
		t.Fatalf("after the hall light: %q", got)
	}
	fake.Change("switch.kaffee", "off", map[string]any{"friendly_name": "Kaffeemaschine"})
	fake.Change("cover.garage", "closed", map[string]any{"friendly_name": "Garagentor", "device_class": "garage"})
	if got := strings.Join(set(readFrames(t, l1, 1, 2*time.Second)), " "); got != "light.flur light.kueche" {
		t.Fatalf("two changes inside the interval, the latest set once: %q", got)
	}
	if f := readFrames(t, l1, 1, 500*time.Millisecond); len(f) != 0 {
		t.Fatalf("nothing more changed the set: %+v", f)
	}
	for _, f := range readFrames(t, l2, 5, 800*time.Millisecond) {
		if f.event != "card_state" {
			t.Fatalf("dev2 did not ask for the active set: %+v", f)
		}
	}
}

// activeSet reads one frame and returns its set, or fails.
func activeSet(t *testing.T, f []frame) string {
	t.Helper()
	if len(f) != 1 || f[0].event != "active_set" {
		t.Fatalf("want one active_set frame, got %+v", f)
	}
	var d struct {
		EntityIDs []string `json:"entity_ids"`
	}
	if err := json.Unmarshal([]byte(f[0].data), &d); err != nil {
		t.Fatalf("active_set data: %s", f[0].data)
	}
	return strings.Join(d.EntityIDs, " ")
}

// haus#28: the first active_set goes per phone, at once: to a stream that
// opens while Haus already holds HA's subscription, and to an open stream
// whose watch turns active on. Not the interval, which is 10s here.
func TestEachPhoneGetsTheActiveSetAtOnce(t *testing.T) {
	fake, h := house(t)
	srv := httptest.NewServer(h)
	defer srv.Close()
	raw(t, h, "POST", "/napi/portal/watch", "dev1", `{"entity_ids":[],"active":true}`)
	r1, l1 := stream(t, srv.URL, "dev1")
	defer r1.Body.Close()
	if !fake.WaitSubscribed(1) {
		t.Fatal("Haus never subscribed")
	}
	const now = "cover.garage light.kueche switch.kaffee"
	if got := activeSet(t, readFrames(t, l1, 1, 2*time.Second)); got != now {
		t.Fatalf("the first phone: %q", got)
	}
	// a second phone, its watch already active, connects while the subscription runs
	raw(t, h, "POST", "/napi/portal/watch", "dev2", `{"entity_ids":[],"active":true}`)
	r2, l2 := stream(t, srv.URL, "dev2")
	defer r2.Body.Close()
	if got := activeSet(t, readFrames(t, l2, 1, time.Second)); got != now {
		t.Fatalf("the second phone: %q", got)
	}
	// a phone with an open stream turns its watch active on
	r3, l3 := stream(t, srv.URL, "dev1")
	defer r3.Body.Close()
	readFrames(t, l3, 1, 300*time.Millisecond) // dev1's watch is active: its new stream gets the set too
	raw(t, h, "POST", "/napi/portal/watch", "dev2", `{"entity_ids":[]}`)
	raw(t, h, "POST", "/napi/portal/watch", "dev2", `{"entity_ids":[],"active":true}`)
	if got := activeSet(t, readFrames(t, l2, 1, time.Second)); got != now {
		t.Fatalf("a watch turned active on: %q", got)
	}
	// nobody else got anything from that
	if f := readFrames(t, l1, 1, 300*time.Millisecond); len(f) != 0 {
		t.Fatalf("dev1's first stream: %+v", f)
	}
}

// haus#28: a new stream of a device whose watch is active gets the set.
func TestANewStreamOfAnActiveDeviceGetsTheSet(t *testing.T) {
	fake, h := house(t)
	srv := httptest.NewServer(h)
	defer srv.Close()
	raw(t, h, "POST", "/napi/portal/watch", "dev1", `{"entity_ids":[],"active":true}`)
	r1, l1 := stream(t, srv.URL, "dev1")
	defer r1.Body.Close()
	if !fake.WaitSubscribed(1) {
		t.Fatal("Haus never subscribed")
	}
	activeSet(t, readFrames(t, l1, 1, 2*time.Second))
	r2, l2 := stream(t, srv.URL, "dev1")
	defer r2.Body.Close()
	if got := activeSet(t, readFrames(t, l2, 1, time.Second)); got != "cover.garage light.kueche switch.kaffee" {
		t.Fatalf("the device's new stream: %q", got)
	}
}

// After HA drops and Haus resubscribes, a phone that already has the set
// gets no frame unless the set changed: the first frame is per phone, not
// per subscription. The interval is short here, so a frame would show.
func TestNoActiveSetAgainAfterAnHAReconnect(t *testing.T) {
	fake, h := houseWith(t, func(c *Config) { c.SensorEvery = 100 * time.Millisecond })
	srv := httptest.NewServer(h)
	defer srv.Close()
	raw(t, h, "POST", "/napi/portal/watch", "dev1", `{"entity_ids":[],"active":true}`)
	r1, l1 := stream(t, srv.URL, "dev1")
	defer r1.Body.Close()
	if !fake.WaitSubscribed(1) {
		t.Fatal("Haus never subscribed")
	}
	activeSet(t, readFrames(t, l1, 1, 2*time.Second))
	fake.DropAll()
	if !fake.WaitSubscribedExactly(0) || !fake.WaitSubscribed(1) {
		t.Fatal("no reconnect")
	}
	if f := readFrames(t, l1, 1, time.Second); len(f) != 0 {
		t.Fatalf("the same set again after a reconnect: %+v", f)
	}
	// a change missed while away does arrive
	fake.DropAll()
	if !fake.WaitSubscribedExactly(0) {
		t.Fatal("the drop did not drop")
	}
	fake.Set("light.flur", "on", map[string]any{"friendly_name": "Flur"})
	if !fake.WaitSubscribed(1) {
		t.Fatal("no reconnect")
	}
	if got := activeSet(t, readFrames(t, l1, 1, 3*time.Second)); got != "cover.garage light.flur light.kueche switch.kaffee" {
		t.Fatalf("the change missed while away: %q", got)
	}
}

// A set that is back to what the phone has within the interval (a light on
// and off again) sends nothing.
func TestTheSameSetWithinTheIntervalIsNoFrame(t *testing.T) {
	fake, h := houseWith(t, func(c *Config) { c.SensorEvery = time.Second })
	srv := httptest.NewServer(h)
	defer srv.Close()
	raw(t, h, "POST", "/napi/portal/watch", "dev1", `{"entity_ids":[],"active":true}`)
	r1, l1 := stream(t, srv.URL, "dev1")
	defer r1.Body.Close()
	if !fake.WaitSubscribed(1) {
		t.Fatal("Haus never subscribed")
	}
	activeSet(t, readFrames(t, l1, 1, 2*time.Second))
	fake.Change("light.flur", "on", map[string]any{"friendly_name": "Flur"})
	fake.Change("light.flur", "off", map[string]any{"friendly_name": "Flur"})
	if f := readFrames(t, l1, 1, 1800*time.Millisecond); len(f) != 0 {
		t.Fatalf("on and off again within the interval: %+v", f)
	}
}

// haus#28: every card names its page, the thing's own page on Haus, for
// the device widget and each active row to open.
func TestACardNamesItsPage(t *testing.T) {
	_, h := house(t)
	_, body := raw(t, h, "GET", "/napi/portal/state?entity_id=light.kueche", "dev1", "")
	var st struct {
		Card map[string]any `json:"card"`
	}
	json.Unmarshal([]byte(body), &st)
	if st.Card["page"] != "/raeume/light.kueche" {
		t.Fatalf("state card: %s", body)
	}
	_, body = raw(t, h, "GET", "/napi/portal/active", "dev1", "")
	var act struct {
		Active []map[string]any `json:"active"`
	}
	json.Unmarshal([]byte(body), &act)
	if len(act.Active) == 0 {
		t.Fatalf("active: %s", body)
	}
	for _, c := range act.Active {
		if c["page"] != "/raeume/"+c["entity_id"].(string) {
			t.Errorf("active card without its page: %v", c)
		}
	}
}

type frame struct{ event, data string }

// readFrames reads up to n SSE events within d; comments are skipped.
func readFrames(t *testing.T, lines <-chan string, n int, d time.Duration) []frame {
	t.Helper()
	var out []frame
	var cur frame
	deadline := time.After(d)
	for len(out) < n {
		select {
		case l, ok := <-lines:
			if !ok {
				return out
			}
			switch {
			case strings.HasPrefix(l, "event: "):
				cur.event = strings.TrimPrefix(l, "event: ")
			case strings.HasPrefix(l, "data: "):
				cur.data = strings.TrimPrefix(l, "data: ")
			case l == "" && cur.event != "":
				out = append(out, cur)
				cur = frame{}
			}
		case <-deadline:
			return out
		}
	}
	return out
}

func sawPing(lines <-chan string, d time.Duration) bool {
	deadline := time.After(d)
	for {
		select {
		case l, ok := <-lines:
			if !ok {
				return false
			}
			if l == ": ping" {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

// Without HAUS_HA_URL the house paths and the event stream are 404, passed
// on to nobody (haus#26).
func TestWithoutHATheHousePathsAre404(t *testing.T) {
	var calls []string
	lb := fakeLaib(t, &calls)
	defer lb.Close()
	h := Handler(Config{Laib: &laib.Client{URL: lb.URL, Token: "ops"}, Validate: StaticValidator([]string{"dev1"})})
	for _, p := range []struct{ method, path, body string }{
		{"POST", "/napi/ha/call", `{"entity_id":"lock.haustuer","service":"lock.unlock"}`},
		{"GET", "/napi/portal/state?entity_id=light.kueche", ""},
		{"GET", "/napi/portal/energy", ""},
		{"GET", "/napi/portal/active", ""},
		{"POST", "/napi/portal/watch", `{"entity_ids":[]}`},
		{"GET", "/napi/portal/events", ""},
	} {
		code, body := raw(t, h, p.method, p.path, "dev1", p.body)
		notFound(t, p.method+" "+p.path, code, body)
	}
}

// After HA drops the WebSocket (a restart), Haus resubscribes and resends
// what changed while it was away.
func TestResyncAfterReconnect(t *testing.T) {
	fake, h := house(t)
	srv := httptest.NewServer(h)
	defer srv.Close()
	raw(t, h, "POST", "/napi/portal/watch", "dev1", `{"entity_ids":["light.flur"]}`)
	resp, lines := stream(t, srv.URL, "dev1")
	defer resp.Body.Close()
	if f := readFrames(t, lines, 1, 2*time.Second); len(f) != 1 || !strings.Contains(f[0].data, `"state":"off"`) {
		t.Fatalf("first resync: %+v", f)
	}
	fake.DropAll()
	if !fake.WaitSubscribedExactly(0) {
		t.Fatal("the drop did not drop")
	}
	fake.Set("light.flur", "on", map[string]any{"friendly_name": "Flur"}) // no event: Haus is not listening
	if !fake.WaitSubscribed(1) {
		t.Fatal("Haus did not resubscribe")
	}
	if f := readFrames(t, lines, 1, 3*time.Second); len(f) != 1 || !strings.Contains(f[0].data, `"state":"on"`) {
		t.Fatalf("after the reconnect the missed change must arrive: %+v", f)
	}
}

// A sensor card goes out at most once per SensorEvery, and its last value
// always does.
func TestSensorThrottle(t *testing.T) {
	fake, h := houseWith(t, func(c *Config) { c.SensorEvery = 500 * time.Millisecond })
	srv := httptest.NewServer(h)
	defer srv.Close()
	attrs := map[string]any{"friendly_name": "Leistung", "device_class": "power", "unit_of_measurement": "W"}
	fake.Set("sensor.leistung", "100", attrs)
	raw(t, h, "POST", "/napi/portal/watch", "dev1", `{"entity_ids":["sensor.leistung"]}`)
	resp, lines := stream(t, srv.URL, "dev1")
	defer resp.Body.Close()
	if f := readFrames(t, lines, 1, 2*time.Second); len(f) != 1 || !strings.Contains(f[0].data, `"state":"100"`) {
		t.Fatalf("leading edge: %+v", f)
	}
	for _, v := range []string{"200", "300", "400"} {
		fake.Change("sensor.leistung", v, attrs)
	}
	f := readFrames(t, lines, 3, 1500*time.Millisecond)
	if len(f) != 1 || !strings.Contains(f[0].data, `"state":"400"`) {
		t.Fatalf("within the interval only the last value, once: %+v", f)
	}
}

// When the last stream leaves, Haus lets go of HA's subscription.
func TestTheLastStreamLetsGoOfHA(t *testing.T) {
	fake, h := house(t)
	srv := httptest.NewServer(h)
	defer srv.Close()
	r1, _ := stream(t, srv.URL, "dev1")
	r2, _ := stream(t, srv.URL, "dev2")
	if !fake.WaitSubscribed(1) {
		t.Fatal("never subscribed")
	}
	r1.Body.Close()
	time.Sleep(100 * time.Millisecond)
	if fake.Subscribed() != 1 {
		t.Fatal("one stream is still open: keep the subscription")
	}
	r2.Body.Close()
	if !fake.WaitSubscribedExactly(0) {
		t.Fatalf("still subscribed after the last stream left: %d", fake.Subscribed())
	}
}

// A device holds a few streams at most; a new one pushes out its oldest
// (a stale one the device has long given up on).
func TestStreamsPerDeviceAreCapped(t *testing.T) {
	_, h := house(t)
	srv := httptest.NewServer(h)
	defer srv.Close()
	var all []<-chan string
	for i := 0; i < 4; i++ {
		resp, lines := stream(t, srv.URL, "dev1")
		defer resp.Body.Close()
		all = append(all, lines)
		time.Sleep(50 * time.Millisecond)
	}
	if !ended(all[0], 2*time.Second) {
		t.Fatal("the oldest of four streams is still open")
	}
	if ended(all[3], 200*time.Millisecond) {
		t.Fatal("the newest stream was closed")
	}
}

func TestWatchWhenHAIsDown(t *testing.T) {
	fake, h := house(t)
	fake.Down(true)
	if code, body := raw(t, h, "POST", "/napi/portal/watch", "dev1", `{"entity_ids":["light.kueche"]}`); code != 503 || !strings.Contains(body, `"ok":false`) {
		t.Fatalf("watch with HA down → %d %s, want 503", code, body)
	}
}

func stream(t *testing.T, base, bearer string) (*http.Response, <-chan string) {
	t.Helper()
	req, _ := http.NewRequest("GET", base+"/napi/portal/events", nil)
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	lines := make(chan string, 100)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	return resp, lines
}

func ended(lines <-chan string, d time.Duration) bool {
	deadline := time.After(d)
	for {
		select {
		case _, ok := <-lines:
			if !ok {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

// HAUS_ENERGY_FLOW / HAUS_ENERGY_TOTALS name the entities; the friendly
// names are only the fallback.
func TestPortalEnergyFromConfig(t *testing.T) {
	fake, h := houseWith(t, func(c *Config) {
		c.Energy = EnergyMap{Flow: map[string]string{"pv": "sensor.inverter", "grid": "sensor.meter"},
			Totals: []EnergyTotal{{Label: "PV gesamt", Entity: "sensor.pv_kwh"}, {Label: "Bezug", Entity: "sensor.import_kwh"}}}
	})
	power := map[string]any{"device_class": "power", "unit_of_measurement": "W"}
	fake.Set("sensor.inverter", "2500", map[string]any{"friendly_name": "Inverter", "device_class": "power", "unit_of_measurement": "W"})
	fake.Set("sensor.meter", "-900", map[string]any{"friendly_name": "Meter", "device_class": "power", "unit_of_measurement": "W"})
	fake.Set("sensor.fridge", "80", power)
	fake.Set("sensor.pv_kwh", "999", map[string]any{"friendly_name": "PV lifetime", "device_class": "energy", "unit_of_measurement": "kWh"})
	fake.Set("sensor.import_kwh", "111", map[string]any{"friendly_name": "Import", "device_class": "energy", "unit_of_measurement": "kWh"})
	// a friendly name that would have matched the fallback must not count now
	fake.Set("sensor.alt", "1", map[string]any{"friendly_name": "Aktueller Hausverbrauch", "device_class": "power", "unit_of_measurement": "W"})
	_, body := raw(t, h, "GET", "/napi/portal/energy", "dev1", "")
	var out struct {
		Energy struct {
			Flow     []map[string]any `json:"flow"`
			Totals   []map[string]any `json:"totals"`
			Circuits []map[string]any `json:"circuits"`
		} `json:"energy"`
	}
	json.Unmarshal([]byte(body), &out)
	e := out.Energy
	if len(e.Flow) != 2 || e.Flow[0]["entity_id"] != "sensor.inverter" || e.Flow[0]["label"] != "PV" || e.Flow[0]["sense"] != "supply" ||
		e.Flow[1]["entity_id"] != "sensor.meter" || e.Flow[1]["label"] != "Netz" || e.Flow[1]["sense"] != "grid" || e.Flow[1]["state"] != "-900" {
		t.Fatalf("flow: %v", e.Flow)
	}
	if len(e.Totals) != 2 || e.Totals[0]["label"] != "PV gesamt" || e.Totals[0]["state"] != "999" || e.Totals[1]["label"] != "Bezug" {
		t.Fatalf("totals: %v", e.Totals)
	}
	for _, c := range e.Circuits {
		if c["entity_id"] == "sensor.inverter" || c["entity_id"] == "sensor.meter" {
			t.Fatalf("a flow entity is not a circuit: %v", e.Circuits)
		}
	}
	if len(e.Circuits) != 2 {
		t.Fatalf("circuits: %v", e.Circuits)
	}
}

func TestEnergyFromEnv(t *testing.T) {
	t.Setenv("HAUS_ENERGY_FLOW", "")
	t.Setenv("HAUS_ENERGY_TOTALS", "")
	if m, err := EnergyFromEnv(); err != nil || len(m.Flow) != 0 || len(m.Totals) != 0 {
		t.Fatalf("unset: %+v %v", m, err)
	}
	t.Setenv("HAUS_ENERGY_FLOW", "pv=sensor.a, house=sensor.b,grid=sensor.c,battery=sensor.d")
	t.Setenv("HAUS_ENERGY_TOTALS", "PV-Erzeugung=sensor.e, Batterie geladen=sensor.f")
	m, err := EnergyFromEnv()
	if err != nil || m.Flow["pv"] != "sensor.a" || m.Flow["battery"] != "sensor.d" || len(m.Totals) != 2 ||
		m.Totals[1] != (EnergyTotal{Label: "Batterie geladen", Entity: "sensor.f"}) {
		t.Fatalf("set: %+v %v", m, err)
	}
	for _, bad := range []string{"sun=sensor.a", "pv", "pv=not an entity"} {
		t.Setenv("HAUS_ENERGY_FLOW", bad)
		if _, err := EnergyFromEnv(); err == nil {
			t.Fatalf("%q must refuse to start", bad)
		}
	}
}

// haus#35: the widgets and the family's pages share one rule (package
// schalter). A media player pauses, goes on and steps its volume (haus#12),
// nothing more: no URL played on the speakers, no set volume, no mute, no
// turning off.
func TestHACallPausesAPlayerAndNothingMore(t *testing.T) {
	fake, h := house(t)
	fake.Set("media_player.wohnzimmer", "playing", map[string]any{"friendly_name": "Wohnzimmer", "supported_features": 1})
	for _, c := range []struct {
		body string
		want int
	}{
		{`{"entity_id":"media_player.wohnzimmer","service":"media_player.media_pause"}`, 200},
		{`{"entity_id":"media_player.wohnzimmer","service":"media_player.media_play"}`, 200},
		{`{"entity_id":"media_player.wohnzimmer","service":"media_player.volume_up"}`, 200},
		{`{"entity_id":"media_player.wohnzimmer","service":"media_player.volume_down"}`, 200},
		{`{"entity_id":"media_player.wohnzimmer","service":"media_player.volume_mute","data":{"is_volume_muted":true}}`, 400},
		{`{"entity_id":"media_player.wohnzimmer","service":"media_player.play_media","data":{"media_content_id":"http://x"}}`, 400},
		{`{"entity_id":"media_player.wohnzimmer","service":"media_player.volume_set","data":{"volume_level":1},"confirmed":true}`, 400},
		{`{"entity_id":"media_player.wohnzimmer","service":"media_player.turn_off"}`, 400},
	} {
		before := len(fake.Calls())
		code, body := raw(t, h, "POST", "/napi/ha/call", "dev1", c.body)
		calls := len(fake.Calls()) - before
		if code != c.want || (code == 200) != (calls == 1) || (code != 200 && !strings.Contains(body, "unsupported_service")) {
			t.Errorf("%s → %d %s, %d calls; want %d", c.body, code, body, calls, c.want)
		}
	}
}

// haus#89: a switch from the app names the person its device was paired
// by, with HA's context id, in Haus's log and Laib's journal.
func TestHACallNotesWhoItWas(t *testing.T) {
	var notes []map[string]any
	var mu sync.Mutex
	lb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Params struct {
				Name string         `json:"name"`
				Args map[string]any `json:"arguments"`
			} `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.Params.Name == "laib_note" {
			mu.Lock()
			notes = append(notes, req.Params.Args)
			mu.Unlock()
		}
		json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"content": []map[string]string{{"text": "ok"}}}})
	}))
	t.Cleanup(lb.Close)
	_, h := houseWith(t, func(c *Config) {
		c.Witness = &schalter.Witness{Laib: &laib.Client{URL: lb.URL, Token: "t"}}
		c.Whoami = func(b string) (string, bool) { return map[string]string{"dev1": "ben"}[b], b == "dev1" }
	})
	if code, body := raw(t, h, "POST", "/napi/ha/call", "dev1", `{"entity_id":"switch.kaffee","service":"switch.turn_off"}`); code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		n := len(notes)
		mu.Unlock()
		if n > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(notes) != 1 || notes[0]["text"] != "ha person=ben service=switch.turn_off entity=switch.kaffee context=ctx-1" {
		t.Fatalf("notes %v", notes)
	}
}
