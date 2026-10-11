// Package napi is the companion API the Android widgets speak
// (servicebay docs/COMPANION_APP.md, solaris-android SbApiClient.kt): the
// four ServiceBay keys served from Laib, operate and verdicts behind the
// right token, and every other /napi path passed through to Solaris until
// its portal moves here (laib#28).
package napi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mdopp/haus/internal/laib"
)

// Validator says whether a bearer is a paired device.
type Validator func(ctx context.Context, bearer string) bool

// Config wires the handler.
type Config struct {
	Laib     *laib.Client // the haus hand: status, inbox, journal, restart — nothing else
	Validate Validator
	Upstream *url.URL                           // Solaris, for every other /napi path; nil = 404
	Whoami   func(bearer string) (string, bool) // Haus's own devices: uid behind a bearer; others fall through to Solaris
}

// Handler serves /napi/*.
func Handler(c Config) http.Handler {
	mux := http.NewServeMux()
	auth := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if bearer == "" || c.Validate == nil || !c.Validate(r.Context(), bearer) {
				w.Header().Set("WWW-Authenticate", "Bearer")
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			next(w, r)
		}
	}
	mux.HandleFunc("GET /napi/servicebay/home", auth(func(w http.ResponseWriter, r *http.Request) {
		ready, rows, err := c.Laib.Status(r.Context())
		if err != nil {
			fail(w, err)
			return
		}
		open, _ := c.Laib.Inbox(r.Context())
		up, failed, down := 0, 0, 0
		for _, s := range rows {
			switch s.State {
			case "grün":
				up++
			case "rot":
				failed++
			default:
				down++
			}
		}
		writeJSON(w, map[string]any{
			"servicesUp": up, "servicesFailed": failed, "servicesDown": down,
			"pendingApprovals": len(open), "pendingUpdates": 0,
			"state": ready.State, "drift": ready.Drift, "lastHeard": ready.LastHeard, "operator": ready.Operator,
		})
	}))
	mux.HandleFunc("GET /napi/servicebay/services", auth(func(w http.ResponseWriter, r *http.Request) {
		_, rows, err := c.Laib.Status(r.Context())
		if err != nil {
			fail(w, err)
			return
		}
		out := []map[string]string{}
		for _, s := range rows {
			active, sub, health := "active", "running", "ok"
			switch s.State {
			case "rot":
				active, sub, health = "failed", "failed", "failed"
			case "gelb":
				health = "degraded"
			}
			out = append(out, map[string]string{"name": s.Name, "activeState": active, "subState": sub, "health": health, "detail": s.Detail})
		}
		writeJSON(w, map[string]any{"services": out})
	}))
	mux.HandleFunc("GET /napi/servicebay/approvals", auth(func(w http.ResponseWriter, r *http.Request) {
		ps, err := c.Laib.Inbox(r.Context())
		if err != nil {
			fail(w, err)
			return
		}
		out := []map[string]any{}
		for _, p := range ps {
			out = append(out, map[string]any{
				"id": strconv.Itoa(p.ID), "service": serviceOf(p.Files), "title": p.Reason,
				"description": p.Tier + " · " + p.Class + " · " + strings.Join(p.Files, ", ") + " · von " + p.Actor,
				"created_at":  p.Created.UTC().Format(time.RFC3339), "status": "pending",
			})
		}
		writeJSON(w, map[string]any{"approvals": out})
	}))
	mux.HandleFunc("GET /napi/servicebay/upgrades", auth(func(w http.ResponseWriter, r *http.Request) {
		// Laib's images update themselves (podman auto-update) and templates
		// sync by timer; there is no pending-upgrade list to show yet.
		writeJSON(w, map[string]any{"upgrades": []any{}})
	}))
	mux.HandleFunc("POST /napi/servicebay/services/{name}/operate", auth(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Action string `json:"action"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		name := r.PathValue("name")
		if body.Action != "restart" {
			http.Error(w, `{"error":"Laib knows restart only; start and stop are state changes that go through a proposal"}`, http.StatusBadRequest)
			return
		}
		if _, err := c.Laib.Restart(r.Context(), name); err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "name": name, "action": "restart"})
	}))
	// Verdicts are not Haus's (haus#1, ADR 0012): the one approval surface is
	// Laib's own page behind Authelia; phones decide, if ever, through the
	// relay. The app's buttons get a plain answer.
	verdict := auth(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"Haus entscheidet nichts: Freigeben und Ablehnen gibt es nur auf der Laib-Seite (laib.<domain>/inbox)"}`, http.StatusForbidden)
	})
	mux.HandleFunc("POST /napi/servicebay/approvals/{id}/approve", verdict)
	mux.HandleFunc("POST /napi/servicebay/approvals/{id}/deny", verdict)
	mux.HandleFunc("GET /napi/whoami", func(w http.ResponseWriter, r *http.Request) {
		bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if c.Whoami != nil {
			if uid, ok := c.Whoami(bearer); ok {
				writeJSON(w, map[string]any{"uid": uid, "via": "haus"})
				return
			}
		}
		if c.Upstream != nil {
			httputil.NewSingleHostReverseProxy(c.Upstream).ServeHTTP(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
	})
	if c.Upstream != nil {
		proxy := httputil.NewSingleHostReverseProxy(c.Upstream)
		proxy.FlushInterval = 100 * time.Millisecond // /napi/portal/events streams
		mux.Handle("/napi/", proxy)
	}
	return mux
}

func serviceOf(files []string) string {
	for _, f := range files {
		if strings.HasPrefix(f, "services/") {
			parts := strings.Split(f, "/")
			if len(parts) > 1 {
				return parts[1]
			}
		}
	}
	return ""
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadGateway)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

// SolarisValidator asks Solaris whether the bearer is one of its paired
// devices (GET /napi/whoami), cached briefly. Pairing stays in Solaris
// until Haus owns it; the app changes a hostname and nothing else.
func SolarisValidator(upstream *url.URL, h *http.Client) Validator {
	type hit struct {
		ok  bool
		exp time.Time
	}
	var mu sync.Mutex
	cache := map[string]hit{}
	if h == nil {
		h = &http.Client{Timeout: 10 * time.Second}
	}
	return func(ctx context.Context, bearer string) bool {
		mu.Lock()
		if c, ok := cache[bearer]; ok && time.Now().Before(c.exp) {
			mu.Unlock()
			return c.ok
		}
		mu.Unlock()
		req, _ := http.NewRequestWithContext(ctx, "GET", upstream.ResolveReference(&url.URL{Path: "/napi/whoami"}).String(), nil)
		req.Header.Set("Authorization", "Bearer "+bearer)
		resp, err := h.Do(req)
		ok := err == nil && resp.StatusCode == 200
		if resp != nil {
			resp.Body.Close()
		}
		mu.Lock()
		cache[bearer] = hit{ok: ok, exp: time.Now().Add(5 * time.Minute)}
		mu.Unlock()
		return ok
	}
}

// Chain accepts a bearer when any validator does.
func Chain(vs ...Validator) Validator {
	return func(ctx context.Context, bearer string) bool {
		for _, v := range vs {
			if v != nil && v(ctx, bearer) {
				return true
			}
		}
		return false
	}
}

// StaticValidator accepts a fixed set of tokens (a trial box without Solaris).
func StaticValidator(tokens []string) Validator {
	set := map[string]bool{}
	for _, t := range tokens {
		if t = strings.TrimSpace(t); t != "" {
			set[t] = true
		}
	}
	return func(_ context.Context, bearer string) bool { return set[bearer] }
}
