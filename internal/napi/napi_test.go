package napi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mdopp/homes/internal/assistent"
	"github.com/mdopp/homes/internal/caldav"
	"github.com/mdopp/homes/internal/ha"
	"github.com/mdopp/homes/internal/haushalt"
	"github.com/mdopp/homes/internal/laib"
	"github.com/mdopp/homes/internal/notes"
	"github.com/mdopp/homes/internal/paperless"
	"github.com/mdopp/homes/internal/reitti"
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

func TestDenyIsNotHomesEither(t *testing.T) {
	var calls []string
	srv := fakeLaib(t, &calls)
	defer srv.Close()
	h := Handler(Config{Laib: &laib.Client{URL: srv.URL, Token: "ops"}, Validate: StaticValidator([]string{"dev1"})})
	if code, _ := get(t, h, "POST", "/napi/box/approvals/7/deny", "dev1", ""); code != 403 {
		t.Fatalf("deny from a phone → %d, want 403", code)
	}
}

// haus#26, laib ADR 0022 §4: the old app's Box keys are gone, and a /napi
// path Homes does not serve is 404, passed on to nobody — even with a paired
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

func TestWhoamiFromHomesDevices(t *testing.T) {
	var calls []string
	lb := fakeLaib(t, &calls)
	defer lb.Close()
	h := Handler(Config{Laib: &laib.Client{URL: lb.URL, Token: "ops"},
		Validate: StaticValidator([]string{"homes_device_a"}),
		Whoami: func(b string) (string, bool) {
			if b == "homes_device_a" {
				return "mdopp", true
			}
			return "", false
		}})
	code, who := get(t, h, "GET", "/napi/whoami", "homes_device_a", "")
	if code != 200 || who["uid"] != "mdopp" {
		t.Fatalf("whoami: %d %v", code, who)
	}
	if code, _ := get(t, h, "GET", "/napi/whoami", "homes_device_other", ""); code != 401 {
		t.Fatalf("unknown device → %d, want 401", code)
	}
	if code, _ := get(t, h, "GET", "/napi/portal/energy", "homes_device_a", ""); code != 404 {
		t.Fatalf("portal without HA → %d, want 404", code)
	}
}

// laib#1021: the old Haus app (cloud.dopp.haus) asks which app this box
// speaks to and, as paket is not its own, tells the person to install
// Homes. Behind a paired device's token, the old app's haus_device_ one too.
func TestTheOldAppLearnsAboutHomes(t *testing.T) {
	var calls []string
	lb := fakeLaib(t, &calls)
	defer lb.Close()
	h := Handler(Config{Laib: &laib.Client{URL: lb.URL, Token: "ops"}, Validate: StaticValidator([]string{"haus_device_alt"})})
	code, p := get(t, h, "GET", "/napi/app", "haus_device_alt", "")
	want := map[string]any{"name": "Homes", "paket": "cloud.dopp.homes", "quelle": "https://github.com/mdopp/homes-android"}
	if code != 200 || len(p) != len(want) {
		t.Fatalf("/napi/app: %d %v", code, p)
	}
	for k, v := range want {
		if p[k] != v {
			t.Errorf("/napi/app %s = %v, want %v", k, p[k], v)
		}
	}
	if code, _ := get(t, h, "GET", "/napi/app", "", ""); code != 401 {
		t.Fatalf("without a token: %d", code)
	}
	if code, _ := get(t, h, "GET", "/napi/app", "haus_device_fremd", ""); code != 401 {
		t.Fatalf("an unknown token: %d", code)
	}
	if code, _ := get(t, h, "POST", "/napi/app", "haus_device_alt", ""); code == 200 {
		t.Fatalf("POST /napi/app: %d", code)
	}
}

// The app takes the assistant's name from Homes, behind a paired device's
// token; nothing else (its colours are fixed in the app, its icon is its
// launcher icon). Laib sets the name once (HOMES_ASSISTANT), "Homes" without it.
func TestTheAppReadsTheAssistantsName(t *testing.T) {
	var calls []string
	lb := fakeLaib(t, &calls)
	defer lb.Close()
	h := Handler(Config{Laib: &laib.Client{URL: lb.URL, Token: "ops"}, Validate: StaticValidator([]string{"dev1"})})
	t.Cleanup(func() { assistent.Set("") })
	for set, want := range map[string]string{"": "Homes", "Ada": "Ada"} {
		assistent.Set(set)
		code, p := get(t, h, "GET", "/napi/assistent", "dev1", "")
		if code != 200 || p["name"] != want || len(p) != 1 {
			t.Fatalf("set %q: %d %v, want {name: %s}", set, code, p, want)
		}
	}
	if code, _ := get(t, h, "GET", "/napi/assistent", "", ""); code != 401 {
		t.Fatalf("without a token: %d", code)
	}
	// an app from before (dev.249) still asks /napi/persona: it gets the same name, the fixed look, no icon
	assistent.Set("")
	if code, p := get(t, h, "GET", "/napi/persona", "dev1", ""); code != 200 || p["display"] != "Homes" || p["accent"] != "#3ddc84" || p["ground"] != "#0c120e" || p["icon"] != "" {
		t.Fatalf("/napi/persona for an older app: %d %v", code, p)
	}
	if code, _ := get(t, h, "GET", "/napi/persona", "", ""); code != 401 {
		t.Fatalf("/napi/persona without a token: %d", code)
	}
	code, body := raw(t, h, "GET", "/napi/persona/icon.png", "dev1", "")
	notFound(t, "/napi/persona/icon.png", code, body)
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
// and three updates. Homes decides none of them (haus#1).
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
// one, a path Homes does not serve is the JSON 404.
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

// Every route Homes registers under /napi wants a paired device: built with
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
		UploadDir: t.TempDir(), MusicInbox: t.TempDir(), Sicherung: NewSicherung(t.TempDir(), t.TempDir()), Reitti: &reitti.Reitti{URL: dead, Dir: t.TempDir()}})
	routes := map[string]bool{"POST /napi/musik": true,
		"GET /napi/assistent": true, "GET /napi/persona": true, "GET /napi/whoami": true,
		"GET /napi/box/home": true, "GET /napi/box/services": true, "GET /napi/box/approvals": true, "GET /napi/box/upgrades": true,
		"POST /napi/box/services/{name}/operate": true, "POST /napi/box/approvals/{id}/approve": true, "POST /napi/box/approvals/{id}/deny": true,
		"POST /napi/ha/call": true, "GET /napi/portal/state": true, "GET /napi/portal/energy": true, "GET /napi/portal/entity-history": true,
		"GET /napi/portal/active": true, "GET /napi/portal/camera/{entity_id}/snapshot": true, "GET /napi/portal/start/addable": true, "GET /napi/portal/favorites": true,
		"POST /napi/portal/watch": true, "GET /napi/portal/events": true,
		"GET /napi/defs/tool": true, "POST /napi/action-callback": true, "GET /napi/portal/tasks": true, "GET /napi/portal/contacts": true,
		"GET /napi/portal/documents": true, "GET /napi/portal/notes": true, "POST /napi/upload": true, "POST /napi/merken": true,
		"GET /napi/sicherung/stand": true, "POST /napi/sicherung/fehlend": true, "POST /napi/sicherung/uploads": true,
		"HEAD /napi/sicherung/uploads/{id}": true, "PATCH /napi/sicherung/uploads/{id}": true,
		"GET /napi/sicherung/schluessel": true, "POST /napi/kontakte": true,
		"GET /napi/sicherung/letzte": true, "GET /napi/sicherung/letzte/{quelle}/{pfad...}": true,
		"GET /napi/gemeinsam/kontakte": true, "GET /napi/gemeinsam/kalender": true,
		"GET /napi/standort": true, "POST /napi/standort": true, "GET /napi/app": true, "GET /napi/kann": true, "POST /napi/kann/anfordern": true, "GET /napi/einrichtung": true, "POST /napi/einrichtung/apps": true,
		"POST /napi/standort/import": true, "HEAD /napi/standort/import/{id}": true, "PATCH /napi/standort/import/{id}": true, "GET /napi/standort/import/{id}": true,
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
	fill := strings.NewReplacer("{quelle}/{pfad...}", "signal/x.backup", "{name}", "media", "{id}", "7", "{entity_id}", "camera.einfahrt")
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
