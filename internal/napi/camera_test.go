package napi

import (
	"bytes"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mdopp/haus/internal/laib"
)

// haus#10, slice 2: the camera widget (CameraWidgetConfigActivity.kt picks from
// /napi/portal/start/addable, ApiClient.getCameraSnapshot reads the frame).

var jpeg = append([]byte{0xff, 0xd8, 0xff, 0xe0, 0, 0x10, 'J', 'F', 'I', 'F', 0}, bytes.Repeat([]byte{0x42}, 64)...)

func TestCameraSnapshot(t *testing.T) {
	fake, h := house(t)
	fake.Set("camera.einfahrt", "idle", map[string]any{"friendly_name": "Einfahrt"})
	fake.SetCamera("camera.einfahrt", "image/jpeg", jpeg)
	req := httptest.NewRequest("GET", "/napi/portal/camera/camera.einfahrt/snapshot", nil)
	req.Header.Set("Authorization", "Bearer dev1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || !bytes.Equal(rec.Body.Bytes(), jpeg) || rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("snapshot: %d %q %q", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("the browser may not guess another type: %v", rec.Header())
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "private") {
		t.Fatalf("a camera frame is nobody else's to cache: %q", cc)
	}
	for _, c := range []struct {
		path string
		code int
		why  string
	}{
		{"/napi/portal/camera/light.kueche/snapshot", 400, "a light is no camera"},
		{"/napi/portal/camera/Camera.Einfahrt/snapshot", 400, "an entity id is lower case"},
		{"/napi/portal/camera/camera.ein%2Ffahrt/snapshot", 400, "no path in an entity id"},
		{"/napi/portal/camera/camera.gibtsnicht/snapshot", 404, "an unknown camera"},
	} {
		if code, body := raw(t, h, "GET", c.path, "dev1", ""); code != c.code {
			t.Errorf("%s: %d %s, want %d", c.why, code, body, c.code)
		}
	}
	for _, bearer := range []string{"", "wrong"} {
		if code, _ := raw(t, h, "GET", "/napi/portal/camera/camera.einfahrt/snapshot", bearer, ""); code != 401 {
			t.Errorf("snapshot with %q → %d, want 401", bearer, code)
		}
	}
}

func TestCameraSnapshotOnlyPassesAnImage(t *testing.T) {
	fake, h := house(t)
	fake.SetCamera("camera.html", "text/html", []byte("<script>alert(1)</script>"))
	if code, body := raw(t, h, "GET", "/napi/portal/camera/camera.html/snapshot", "dev1", ""); code != 502 || strings.Contains(body, "script") {
		t.Fatalf("not an image: %d %s", code, body)
	}
	defer func(n int64) { snapshotMax = n }(snapshotMax)
	snapshotMax = 32
	fake.SetCamera("camera.gross", "image/jpeg", jpeg)
	if code, body := raw(t, h, "GET", "/napi/portal/camera/camera.gross/snapshot", "dev1", ""); code != 502 || len(body) > 200 {
		t.Fatalf("larger than the cap: %d %d bytes", code, len(body))
	}
	fake.Down(true)
	if code, _ := raw(t, h, "GET", "/napi/portal/camera/camera.gross/snapshot", "dev1", ""); code != 502 {
		t.Fatalf("HA down: %d", code)
	}
}

// addable is what both pickers read: the device picker (actuators) and the
// camera picker (it keeps domain == camera). Grouped by room, the room-less last.
func TestStartAddable(t *testing.T) {
	fake, h := house(t)
	fake.Set("camera.einfahrt", "idle", map[string]any{"friendly_name": "Einfahrt"})
	fake.Set("camera.garten", "streaming", map[string]any{"friendly_name": "Garten"})
	fake.SetRegistries(
		[]map[string]any{{"area_id": "kueche", "name": "Küche"}, {"area_id": "aussen", "name": "Außen"}},
		nil,
		[]map[string]any{{"entity_id": "light.kueche", "area_id": "kueche"}, {"entity_id": "camera.einfahrt", "area_id": "aussen"}, {"entity_id": "camera.garten", "area_id": "aussen"}},
	)
	code, body := raw(t, h, "GET", "/napi/portal/start/addable", "dev1", "")
	if code != 200 {
		t.Fatalf("addable: %d %s", code, body)
	}
	out := decode(t, body)
	if out["ok"] != true {
		t.Fatalf("addable: %s", body)
	}
	if a, ok := out["automations"].([]any); !ok || len(a) != 0 {
		t.Fatalf("automations is an empty list: %s", body)
	}
	rooms, _ := out["rooms"].([]any)
	var order []string
	seen := map[string]map[string]any{}
	roomOf := map[string]string{}
	for _, r := range rooms {
		rm := r.(map[string]any)
		order = append(order, rm["room"].(string))
		for _, c := range rm["cards"].([]any) {
			cm := c.(map[string]any)
			seen[cm["entity_id"].(string)] = cm
			roomOf[cm["entity_id"].(string)] = rm["room"].(string)
		}
	}
	if strings.Join(order, "|") != "Außen|Küche|" {
		t.Fatalf("rooms by name, none last: %v", order)
	}
	for _, id := range []string{"camera.einfahrt", "camera.garten", "light.kueche", "light.flur", "switch.kaffee", "cover.garage", "lock.haustuer", "climate.bad"} {
		if seen[id] == nil {
			t.Errorf("%s missing", id)
		}
	}
	for _, id := range []string{"sensor.temp", "button.tuer"} {
		if seen[id] != nil {
			t.Errorf("%s is nothing a widget switches or shows", id)
		}
	}
	if c := seen["camera.einfahrt"]; c["domain"] != "camera" || c["name"] != "Einfahrt" || roomOf["camera.einfahrt"] != "Außen" {
		t.Fatalf("camera card: %v in %q", c, roomOf["camera.einfahrt"])
	}
	if c := seen["cover.garage"]; c["device_class"] != "garage" {
		t.Fatalf("device_class rides along: %v", c)
	}
	if _, ok := seen["light.flur"]["device_class"]; ok {
		t.Fatalf("no class is no key: %v", seen["light.flur"])
	}
	for _, bearer := range []string{"", "wrong"} {
		if code, _ := raw(t, h, "GET", "/napi/portal/start/addable", bearer, ""); code != 401 {
			t.Errorf("addable with %q → %d", bearer, code)
		}
	}
	fake.Down(true)
	if code, _ := raw(t, h, "GET", "/napi/portal/start/addable", "dev1", ""); code != 502 {
		t.Fatalf("HA down: %d", code)
	}
}

func TestWithoutHACameraAndAddableAre404(t *testing.T) {
	var calls []string
	lb := fakeLaib(t, &calls)
	defer lb.Close()
	h := Handler(Config{Laib: &laib.Client{URL: lb.URL, Token: "ops"}, Validate: StaticValidator([]string{"dev1"})})
	for _, p := range []string{"/napi/portal/camera/camera.einfahrt/snapshot", "/napi/portal/start/addable"} {
		if code, body := raw(t, h, "GET", p, "dev1", ""); code != 404 {
			t.Errorf("%s → %d %s, want 404", p, code, body)
		}
	}
}
