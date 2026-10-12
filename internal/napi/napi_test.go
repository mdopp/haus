package napi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mdopp/haus/internal/laib"
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

func TestCompanionKeysFromLaib(t *testing.T) {
	var calls []string
	srv := fakeLaib(t, &calls)
	defer srv.Close()
	ops := &laib.Client{URL: srv.URL, Token: "ops"}
	h := Handler(Config{Laib: ops, Validate: StaticValidator([]string{"dev1"})})

	if code, _ := get(t, h, "GET", "/napi/servicebay/home", "", ""); code != 401 {
		t.Fatalf("no token → %d", code)
	}
	if code, _ := get(t, h, "GET", "/napi/servicebay/home", "wrong", ""); code != 401 {
		t.Fatalf("wrong token → %d", code)
	}
	code, home := get(t, h, "GET", "/napi/servicebay/home", "dev1", "")
	if code != 200 || home["servicesUp"] != 1.0 || home["servicesFailed"] != 1.0 || home["pendingApprovals"] != 1.0 || home["operator"] != "stumm" {
		t.Fatalf("home: %d %v", code, home)
	}
	_, svcs := get(t, h, "GET", "/napi/servicebay/services", "dev1", "")
	rows := svcs["services"].([]any)
	if len(rows) != 2 || rows[1].(map[string]any)["activeState"] != "failed" || rows[0].(map[string]any)["health"] != "ok" {
		t.Fatalf("services: %v", rows)
	}
	_, aps := get(t, h, "GET", "/napi/servicebay/approvals", "dev1", "")
	a := aps["approvals"].([]any)[0].(map[string]any)
	if a["id"] != "7" || a["service"] != "immich" || a["title"] != "immich: neue Version" || a["status"] != "pending" || a["created_at"] != "2026-10-10T19:00:00Z" {
		t.Fatalf("approvals: %v", a)
	}
	_, ups := get(t, h, "GET", "/napi/servicebay/upgrades", "dev1", "")
	if len(ups["upgrades"].([]any)) != 0 {
		t.Fatalf("upgrades: %v", ups)
	}
	// haus#1: a phone cannot approve, whoever it belongs to — Laib's page is the one place
	if code, _ := get(t, h, "POST", "/napi/servicebay/approvals/7/approve", "dev1", ""); code != 403 {
		t.Fatalf("approve from a phone → %d, want 403", code)
	}
	if code, _ := get(t, h, "POST", "/napi/servicebay/services/media/operate", "dev1", `{"action":"start"}`); code != 400 {
		t.Fatalf("start must be refused, got %d", code)
	}
	if code, _ := get(t, h, "POST", "/napi/servicebay/services/media/operate", "dev1", `{"action":"restart"}`); code != 200 {
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
	if code, _ := get(t, h, "POST", "/napi/servicebay/approvals/7/deny", "dev1", ""); code != 403 {
		t.Fatalf("deny from a phone → %d, want 403", code)
	}
}

func TestSolarisValidatorAndProxy(t *testing.T) {
	var calls []string
	lb := fakeLaib(t, &calls)
	defer lb.Close()
	whoami := 0
	solaris := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/napi/whoami":
			whoami++
			if r.Header.Get("Authorization") == "Bearer sol_device_ok" {
				w.Write([]byte(`{"uid":"mdopp"}`))
			} else {
				w.WriteHeader(401)
			}
		case "/napi/portal/energy":
			w.Write([]byte(`{"pv":1234}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer solaris.Close()
	u, _ := url.Parse(solaris.URL)
	h := Handler(Config{Laib: &laib.Client{URL: lb.URL, Token: "ops"}, Validate: SolarisValidator(u, nil), Upstream: u})
	if code, _ := get(t, h, "GET", "/napi/servicebay/home", "sol_device_ok", ""); code != 200 {
		t.Fatalf("solaris-paired device → %d", code)
	}
	get(t, h, "GET", "/napi/servicebay/home", "sol_device_ok", "")
	if whoami != 1 {
		t.Fatalf("whoami asked %d times, want 1 (cached)", whoami)
	}
	if code, _ := get(t, h, "GET", "/napi/servicebay/home", "sol_device_no", ""); code != 401 {
		t.Fatalf("unknown device → %d", code)
	}
	code, energy := get(t, h, "GET", "/napi/portal/energy", "sol_device_ok", "")
	if code != 200 || energy["pv"] != 1234.0 {
		t.Fatalf("proxy: %d %v", code, energy)
	}
	_ = context.Background()
}

func TestWhoamiFromHausDevices(t *testing.T) {
	var calls []string
	lb := fakeLaib(t, &calls)
	defer lb.Close()
	h := Handler(Config{Laib: &laib.Client{URL: lb.URL, Token: "ops"},
		Validate: Chain(StaticValidator([]string{"sol_device_haus"})),
		Whoami: func(b string) (string, bool) {
			if b == "sol_device_haus" {
				return "mdopp", true
			}
			return "", false
		}})
	code, who := get(t, h, "GET", "/napi/whoami", "sol_device_haus", "")
	if code != 200 || who["uid"] != "mdopp" {
		t.Fatalf("whoami: %d %v", code, who)
	}
	if code, _ := get(t, h, "GET", "/napi/whoami", "sol_device_other", ""); code != 401 {
		t.Fatalf("unknown device without Solaris → %d, want 401", code)
	}
	if code, _ := get(t, h, "GET", "/napi/portal/energy", "sol_device_haus", ""); code != 404 {
		t.Fatalf("portal without Solaris → %d, want 404", code)
	}
}
