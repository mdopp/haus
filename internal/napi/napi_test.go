package napi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mdopp/haus/internal/laib"
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
func TestCompanionKeysFromLaib(t *testing.T) {
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
		House: persona.Persona{Display: "Joshua", Greeting: map[string]string{"de": "Hallo."}, Colors: map[string]string{"accent": "#3ddc84"}, Icon: []byte("<svg/>")}})
	code, p := get(t, h, "GET", "/napi/persona", "dev1", "")
	if code != 200 || p["display"] != "Joshua" || p["icon"] != "/napi/persona/icon.svg" {
		t.Fatalf("persona: %d %v", code, p)
	}
	if code, _ := get(t, h, "GET", "/napi/persona", "", ""); code != 401 {
		t.Fatalf("without a token: %d", code)
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

// Even without a token: the path is not there, whoever asks.
func TestUnknownNapiPathIsJSON404WithoutAToken(t *testing.T) {
	var calls []string
	lb := fakeLaib(t, &calls)
	defer lb.Close()
	h := Handler(Config{Laib: &laib.Client{URL: lb.URL, Token: "ops"}, Validate: StaticValidator([]string{"dev1"})})
	code, body := raw(t, h, "GET", "/napi/gibtsnicht", "", "")
	notFound(t, "GET /napi/gibtsnicht", code, body)
	code, body = raw(t, h, "GET", "/napi/persona/icon.svg", "dev1", "")
	notFound(t, "the persona icon without an icon", code, body)
}
