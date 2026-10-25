package napi

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mdopp/haus/internal/caldav"
	"github.com/mdopp/haus/internal/ha"
	"github.com/mdopp/haus/internal/haushalt"
	"github.com/mdopp/haus/internal/laib"
	"github.com/mdopp/haus/internal/notes"
	"github.com/mdopp/haus/internal/paperless"
	"github.com/mdopp/haus/internal/persona"
)

// fakeLaib answers tools/call per verb with canned text and records calls.
func fakeLaib(t *testing.T, calls *[]string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer ops" && r.Header.Get("Authorization") != "Bearer human" {
			w.WriteHeader(401)
			return
		}
		var req struct {
			Params struct {
				Name string            `json:"name"`
				Args map[string]string `json:"arguments"`
			} `json:"params"`
		}
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &req)
		*calls = append(*calls, req.Params.Name+" "+req.Params.Args["args"]+" as "+strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		text, isErr := "", false
		switch req.Params.Name {
		case "laib_status":
			text = `laib: a note line first
{"ready":{"state":"bereit","services":2,"green":1,"yellow":0,"red":1,"lastHeard":"2026-10-10T20:00:00Z","drift":0,"operator":"stumm"},"services":[{"name":"caddy","state":"grün","units":{"caddy.service":"active"}},{"name":"media","state":"rot","units":{"media.service":"failed"},"detail":"health 502"}]}`
		case "laib_inbox":
			text = `[{"id":7,"actor":"pi","reason":"immich: neue Version","tier":"apply","status":"open","created":"2026-10-10T19:00:00Z","files":["services/immich/values.yml"],"class":"values"}]`
		case "laib_approve":
			if r.Header.Get("Authorization") != "Bearer human" {
				text, isErr = "ops may not approve", true
			} else {
				text = "approved #7"
			}
		case "laib_restart":
			text = "neu gestartet: media.service"
		default:
			text, isErr = "unknown tool", true
		}
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{"content": []map[string]string{{"type": "text", "text": text}}, "isError": isErr}})
	}))
}

func get(t *testing.T, h http.Handler, method, path, bearer, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

// The Box keys the app's widgets read, under their one name /napi/box
// (haus#26, laib ADR 0022 §4).
func TestBoxKeysFromLaib(t *testing.T) {
	const prefix = "/napi/box"
	var calls []string
	srv := fakeLaib(t, &calls)
	defer srv.Close()
	ops := &laib.Client{URL: srv.URL, Token: "ops"}
	h := Handler(Config{Laib: ops, Validate: StaticValidator([]string{"dev1"})})

	if code, _ := get(t, h, "GET", prefix+"/home", "", ""); code != 401 {
		t.Fatalf("no token → %d", code)
	}
	if code, _ := get(t, h, "GET", prefix+"/home", "wrong", ""); code != 401 {
		t.Fatalf("wrong token → %d", code)
	}
	code, home := get(t, h, "GET", prefix+"/home", "dev1", "")
	if code != 200 || home["servicesUp"] != 1.0 || home["servicesFailed"] != 1.0 || home["pendingApprovals"] != 1.0 || home["operator"] != "stumm" {
		t.Fatalf("home: %d %v", code, home)
	}
	_, svcs := get(t, h, "GET", prefix+"/services", "dev1", "")
	rows := svcs["services"].([]any)
	if len(rows) != 2 || rows[1].(map[string]any)["activeState"] != "failed" || rows[0].(map[string]any)["health"] != "ok" {
		t.Fatalf("services: %v", rows)
	}
	_, aps := get(t, h, "GET", prefix+"/approvals", "dev1", "")
	a := aps["approvals"].([]any)[0].(map[string]any)
	if a["id"] != "7" || a["service"] != "immich" || a["title"] != "immich: neue Version" || a["status"] != "pending" || a["created_at"] != "2026-10-10T19:00:00Z" {
		t.Fatalf("approvals: %v", a)
	}
	_, ups := get(t, h, "GET", prefix+"/upgrades", "dev1", "")
	if len(ups["upgrades"].([]any)) != 0 {
		t.Fatalf("upgrades: %v", ups)
	}
	// haus#1: a phone cannot approve, whoever it belongs to — Laib's page is the one place
	if code, _ := get(t, h, "POST", prefix+"/approvals/7/approve", "dev1", ""); code != 403 {
		t.Fatalf("approve from a phone → %d, want 403", code)
	}
	if code, _ := get(t, h, "POST", prefix+"/services/media/operate", "dev1", `{"action":"start"}`); code != 400 {
		t.Fatalf("start must be refused, got %d", code)
	}
	if code, _ := get(t, h, "POST", prefix+"/services/media/operate", "dev1", `{"action":"restart"}`); code != 200 {
		t.Fatalf("restart → %d", code)
	}
	joined := strings.Join(calls, "\n")
	if strings.Contains(joined, "laib_approve") || !strings.Contains(joined, "laib_restart media as ops") {
		t.Fatalf("calls:\n%s", joined)
	}
}

func TestDenyIsNotHausEither(t *testing.T) {
	var calls []string
	srv := fakeLaib(t, &calls)
	defer srv.Close()
	h := Handler(Config{Laib: &laib.Client{URL: srv.URL, Token: "ops"}, Validate: StaticValidator([]string{"dev1"})})
	if code, _ := get(t, h, "POST", "/napi/box/approvals/7/deny", "dev1", ""); code != 403 {
		t.Fatalf("deny from a phone → %d, want 403", code)
	}
}

// haus#26, laib ADR 0022 §4: the old app's Box keys are gone, and a /napi
// path Haus does not serve is 404, passed on to nobody — even with a paired
// device's token. The 404 is JSON, as every refusal the app parses.
func TestTheOldPathsAreGone(t *testing.T) {
	var calls []string
	lb := fakeLaib(t, &calls)
	defer lb.Close()
	h := Handler(Config{Laib: &laib.Client{URL: lb.URL, Token: "ops"}, Validate: StaticValidator([]string{"dev1"})})
	old := "/napi/" + "service" + "bay"
	for _, p := range []struct{ method, path string }{
		{"GET", old + "/home"}, {"GET", old + "/services"}, {"GET", old + "/approvals"}, {"GET", old + "/upgrades"},
		{"POST", old + "/services/media/operate"}, {"POST", old + "/approvals/7/deny"},
		{"GET", "/napi/portal/nothing-here"}, {"GET", "/napi/defs/model"}, {"POST", "/napi/notifications/ack"},
	} {
		code, body := raw(t, h, p.method, p.path, "dev1", `{"action":"restart"}`)
		notFound(t, p.method+" "+p.path, code, body)
	}
	if len(calls) != 0 {
		t.Fatalf("Laib was asked: %v", calls)
	}
}

func TestWhoamiFromHausDevices(t *testing.T) {
	var calls []string
	lb := fakeLaib(t, &calls)
	defer lb.Close()
	h := Handler(Config{Laib: &laib.Client{URL: lb.URL, Token: "ops"},
		Validate: StaticValidator([]string{"haus_device_a"}),
		Whoami: func(b string) (string, bool) {
			if b == "haus_device_a" {
				return "mdopp", true
			}
			return "", false
		}})
	code, who := get(t, h, "GET", "/napi/whoami", "haus_device_a", "")
	if code != 200 || who["uid"] != "mdopp" {
		t.Fatalf("whoami: %d %v", code, who)
	}
	if code, _ := get(t, h, "GET", "/napi/whoami", "haus_device_other", ""); code != 401 {
		t.Fatalf("unknown device → %d, want 401", code)
	}
	if code, _ := get(t, h, "GET", "/napi/portal/energy", "haus_device_a", ""); code != 404 {
		t.Fatalf("portal without HA → %d, want 404", code)
	}
}

// laib#129, laib#130: the app takes the house persona's name, greeting,
// colours and icon from Haus, behind a paired device's token.
func TestTheAppReadsTheHousePersona(t *testing.T) {
	var calls []string
	lb := fakeLaib(t, &calls)
	defer lb.Close()
	h := Handler(Config{Laib: &laib.Client{URL: lb.URL, Token: "ops"}, Validate: StaticValidator([]string{"dev1"}),
		House: persona.Persona{Display: "Joshua", Languages: []string{"de"}, Greeting: map[string]string{"de": "Hallo."}, Colors: map[string]string{"accent": "#3ddc84"}}})
	// haus#58: the greeting belongs to the chat (an easter egg for its games), not the start screen
	code, p := get(t, h, "GET", "/napi/persona", "dev1", "")
	if code != 200 || p["display"] != "Joshua" || p["greeting"] != "" || p["accent"] != "#3ddc84" {
		t.Fatalf("persona: %d %v", code, p)
	}
	if code, p := get(t, h, "GET", "/napi/persona?surface=chat", "dev1", ""); code != 200 || p["greeting"] != "Hallo." {
		t.Fatalf("the chat's persona carries the greeting: %d %v", code, p)
	}
	if code, _ := get(t, h, "GET", "/napi/persona", "", ""); code != 401 {
		t.Fatalf("without a token: %d", code)
	}
}

// a persona as Laib renders it to /persona/<name>, with the icon files given
func personaDir(t *testing.T, icons map[string][]byte) string {
	t.Helper()
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "joshua"), 0o755)
	os.WriteFile(filepath.Join(dir, "joshua", "persona.json"), []byte(`{"name":"joshua","display":"Joshua","greeting":{"de":"Hallo."},"languages":["de"],"colors":{"accent":"#3ddc84","ground":"#0c120e"}}`), 0o644)
	for name, b := range icons {
		os.WriteFile(filepath.Join(dir, "joshua", name), b, 0o644)
	}
	return dir
}

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	img.Set(1, 1, color.NRGBA{0x3d, 0xdc, 0x84, 0xff})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// haus#28: a phone draws PNG without a library, so /napi hands it the
// persona's icon as PNG (icon.png next to icon.svg; Laib renders it, laib#191)
// and no SVG; the pages keep the SVG for browsers. A persona without a PNG,
// or with a file that is not one, has no icon on the phone.
func TestThePhoneGetsThePersonaIconAsPNG(t *testing.T) {
	var calls []string
	lb := fakeLaib(t, &calls)
	defer lb.Close()
	pic := tinyPNG(t)
	handler := func(icons map[string][]byte) http.Handler {
		return Handler(Config{Laib: &laib.Client{URL: lb.URL, Token: "ops"}, Validate: StaticValidator([]string{"dev1"}),
			House: persona.Load(personaDir(t, icons), "joshua")})
	}

	h := handler(map[string][]byte{"icon.svg": []byte("<svg/>"), "icon.png": pic})
	if code, p := get(t, h, "GET", "/napi/persona", "dev1", ""); code != 200 || p["display"] != "Joshua" || p["icon"] != "/napi/persona/icon.png" {
		t.Fatalf("persona: %d %v", code, p)
	}
	req := httptest.NewRequest("GET", "/napi/persona/icon.png", nil)
	req.Header.Set("Authorization", "Bearer dev1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/png" || !bytes.Equal(rec.Body.Bytes(), pic) {
		t.Fatalf("icon.png: %d %q %d bytes", rec.Code, rec.Header().Get("Content-Type"), rec.Body.Len())
	}
	if _, err := png.Decode(bytes.NewReader(rec.Body.Bytes())); err != nil {
		t.Fatalf("icon.png does not decode: %v", err)
	}
	code, body := raw(t, h, "GET", "/napi/persona/icon.svg", "dev1", "")
	notFound(t, "the SVG on /napi", code, body)

	for what, icons := range map[string]map[string][]byte{
		"an SVG only":       {"icon.svg": []byte("<svg/>")},
		"no icon":           {},
		"a PNG that is not": {"icon.png": []byte("<svg/>")},
	} {
		h := handler(icons)
		if _, p := get(t, h, "GET", "/napi/persona", "dev1", ""); p["icon"] != "" {
			t.Errorf("%s: icon %v, want none", what, p["icon"])
		}
		code, body := raw(t, h, "GET", "/napi/persona/icon.png", "dev1", "")
		notFound(t, what+": icon.png", code, body)
	}
}

// laibAnswering is a Laib whose tools answer the given text; any other
// tool is an error.
func laibAnswering(t *testing.T, texts map[string]string) *laib.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		text, ok := texts[req.Params.Name]
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{"content": []map[string]string{{"type": "text", "text": text}}, "isError": !ok}})
	}))
	t.Cleanup(srv.Close)
	return &laib.Client{URL: srv.URL, Token: "ops"}
}

// haus#28: pendingUpdates is Laib's count of template updates waiting:
// `laib template sync --propose` files each as a proposal whose kind is
// template-update, whatever class the dial gave it (a public route makes
// it human). An update is counted and listed as an update only, never as
// an approval too: three updates and one other proposal are one approval
// and three updates. Haus decides none of them (haus#1).
func TestPendingUpdatesAreLaibsTemplateUpdates(t *testing.T) {
	lb := laibAnswering(t, map[string]string{
		"laib_status": `{"ready":{"state":"bereit"},"services":[{"name":"immich","state":"grün"}]}`,
		"laib_inbox": `[{"id":7,"actor":"pi","reason":"immich: mehr Speicher","tier":"apply","status":"open","created":"2026-10-10T19:00:00Z","files":["services/immich/values.yml"],"class":"values","kind":"values"},
{"id":8,"actor":"laib","reason":"immich: Vorlage 1.2 → 1.3","tier":"apply","status":"open","created":"2026-10-11T04:00:00Z","files":["services/immich/template.yml"],"class":"template-update","kind":"template-update"},
{"id":9,"actor":"laib","reason":"caddy: Vorlage 2.0 → 2.1","tier":"apply","status":"open","created":"2026-10-11T04:00:01Z","files":["services/caddy/template.yml"],"class":"human","kind":"template-update","reasons":["eine öffentliche Route"]},
{"id":10,"actor":"laib","reason":"paperless: Vorlage 3.0 → 3.1","tier":"apply","status":"open","created":"2026-10-11T04:00:02Z","files":["services/paperless/template.yml"],"class":"template-update","kind":"template-update"}]`,
	})
	h := Handler(Config{Laib: lb, Validate: StaticValidator([]string{"dev1"})})
	code, home := get(t, h, "GET", "/napi/box/home", "dev1", "")
	if code != 200 || home["pendingUpdates"] != 3.0 || home["pendingApprovals"] != 1.0 {
		t.Fatalf("home: %d %v, want 1 approval and 3 updates", code, home)
	}
	_, aps := get(t, h, "GET", "/napi/box/approvals", "dev1", "")
	if rows, _ := aps["approvals"].([]any); len(rows) != 1 || rows[0].(map[string]any)["id"] != "7" {
		t.Fatalf("approvals, the updates left out: %v", aps)
	}
	_, ups := get(t, h, "GET", "/napi/box/upgrades", "dev1", "")
	rows, _ := ups["upgrades"].([]any)
	if len(rows) != 3 {
		t.Fatalf("upgrades: %v", ups)
	}
	if u := rows[0].(map[string]any); u["id"] != "8" || u["service"] != "immich" || u["title"] != "immich: Vorlage 1.2 → 1.3" || u["created_at"] != "2026-10-11T04:00:00Z" {
		t.Fatalf("first upgrade: %v", u)
	}
	if u := rows[1].(map[string]any); u["id"] != "9" || u["service"] != "caddy" {
		t.Fatalf("second upgrade: %v", u)
	}
}

// notFound: an unknown or unconfigured /napi path answers the one JSON 404
// (haus#26): the app parses JSON errors.
func notFound(t *testing.T, what string, code int, body string) {
	t.Helper()
	if code != 404 || strings.TrimSpace(body) != `{"ok":false,"reason":"not_found"}` {
		t.Errorf("%s → %d %q, want 404 {\"ok\":false,\"reason\":\"not_found\"}", what, code, body)
	}
}

// Auth comes before the 404: without a paired device every /napi path is
// 401, so a stranger learns nothing about which backends are set. With
// one, a path Haus does not serve is the JSON 404.
func TestUnknownNapiPathWantsADeviceFirst(t *testing.T) {
	var calls []string
	lb := fakeLaib(t, &calls)
	defer lb.Close()
	h := Handler(Config{Laib: &laib.Client{URL: lb.URL, Token: "ops"}, Validate: StaticValidator([]string{"dev1"})})
	for _, p := range []string{"/napi/gibtsnicht", "/napi/portal/energy", "/napi/defs/tool", "/napi/upload"} {
		for _, bearer := range []string{"", "wrong"} {
			if code, body := raw(t, h, "GET", p, bearer, ""); code != 401 {
				t.Errorf("GET %s with %q → %d %s, want 401", p, bearer, code, body)
			}
		}
		code, body := raw(t, h, "GET", p, "dev1", "")
		notFound(t, "GET "+p, code, body)
	}
	code, body := raw(t, h, "GET", "/napi/persona/icon.png", "dev1", "")
	notFound(t, "the persona icon without an icon", code, body)
}

// A device token counts only after the literal "Bearer ": a bare token, or
// another scheme, is no device.
func TestTheTokenWantsTheBearerScheme(t *testing.T) {
	var calls []string
	lb := fakeLaib(t, &calls)
	defer lb.Close()
	h := Handler(Config{Laib: &laib.Client{URL: lb.URL, Token: "ops"}, Validate: StaticValidator([]string{"dev1"}),
		Whoami: func(b string) (string, bool) { return "anna", b == "dev1" }})
	for _, auth := range []string{"dev1", "Basic dev1", "Bearerdev1", "bearer dev1"} {
		for _, p := range []string{"/napi/box/home", "/napi/whoami", "/napi/gibtsnicht"} {
			req := httptest.NewRequest("GET", p, nil)
			req.Header.Set("Authorization", auth)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != 401 {
				t.Errorf("GET %s with Authorization %q → %d, want 401", p, auth, rec.Code)
			}
		}
	}
	if code, _ := raw(t, h, "GET", "/napi/whoami", "dev1", ""); code != 200 {
		t.Fatalf("whoami with Bearer dev1 → %d", code)
	}
}

// Every route Haus registers under /napi wants a paired device: built with
// every backend set, each one answers 401 without a token and with a wrong
// one. The routes are read from the source, so a new route without a row
// here fails too.
func TestEveryNapiRouteWantsADevice(t *testing.T) {
	var calls []string
	lb := fakeLaib(t, &calls)
	defer lb.Close()
	nd, err := notes.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dead := "http://127.0.0.1:1"
	dav := &caldav.Client{URL: dead, User: "u", Password: "p"}
	h := Handler(Config{Laib: &laib.Client{URL: lb.URL, Token: "ops"}, Validate: StaticValidator([]string{"dev1"}),
		HA:        &ha.Client{URL: dead, Token: "ha"},
		Haushalt:  haushalt.Config{Calendar: dav, Contacts: dav, Documents: &paperless.Client{URL: dead, Token: "pl"}, Notes: nd},
		UploadDir: t.TempDir(),
		House:     persona.Load(personaDir(t, map[string][]byte{"icon.svg": []byte("<svg/>"), "icon.png": tinyPNG(t)}), "joshua")})
	routes := map[string]bool{
		"GET /napi/persona": true, "GET /napi/persona/icon.png": true, "GET /napi/whoami": true,
		"GET /napi/box/home": true, "GET /napi/box/services": true, "GET /napi/box/approvals": true, "GET /napi/box/upgrades": true,
		"POST /napi/box/services/{name}/operate": true, "POST /napi/box/approvals/{id}/approve": true, "POST /napi/box/approvals/{id}/deny": true,
		"POST /napi/ha/call": true, "GET /napi/portal/state": true, "GET /napi/portal/energy": true, "GET /napi/portal/entity-history": true,
		"GET /napi/portal/active": true, "GET /napi/portal/camera/{entity_id}/snapshot": true, "GET /napi/portal/start/addable": true,
		"POST /napi/portal/watch": true, "GET /napi/portal/events": true,
		"GET /napi/defs/tool": true, "POST /napi/action-callback": true, "GET /napi/portal/tasks": true, "GET /napi/portal/contacts": true,
		"GET /napi/portal/documents": true, "GET /napi/portal/notes": true, "POST /napi/upload": true, "POST /napi/merken": true,
	}
	// what the source registers: "METHOD /napi/…" and "METHOD "+prefix+"/…" (prefix is /napi/box)
	re := regexp.MustCompile(`HandleFunc\("([A-Z]+) (?:(/napi/[^"]*)"|"\+prefix\+"([^"]*)")`)
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, _ := os.ReadFile(f)
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			p := m[2]
			if p == "" {
				p = "/napi/box" + m[3]
			}
			if !routes[m[1]+" "+p] {
				t.Errorf("%s registers %s %s, which this table does not cover", f, m[1], p)
			}
		}
	}
	fill := strings.NewReplacer("{name}", "media", "{id}", "7", "{entity_id}", "camera.einfahrt")
	for route := range routes {
		method, path, _ := strings.Cut(route, " ")
		path = fill.Replace(path)
		for _, bearer := range []string{"", "wrong"} {
			if code, body := raw(t, h, method, path, bearer, "{}"); code != 401 {
				t.Errorf("%s %s with %q → %d %s, want 401", method, path, bearer, code, body)
			}
		}
	}
	if len(calls) != 0 {
		t.Fatalf("Laib was asked without a device: %v", calls)
	}
}
