// Package napi is the companion API the Android widgets speak (the app's
// SbApiClient.kt and ApiClient.kt): the four box keys under
// legacy.NapiPrefix served from Laib, operate and verdicts behind the right
// token, the house paths from Home Assistant (home.go, live.go, haus#10),
// and every other /napi path passed through the migration bridge
// (bridge.go) until Haus serves it (laib#28).
package napi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mdopp/haus/internal/ha"
	"github.com/mdopp/haus/internal/laib"
	"github.com/mdopp/haus/internal/legacy"
	"github.com/mdopp/haus/internal/persona"
)

// Validator says whether a bearer is a paired device.
type Validator func(ctx context.Context, bearer string) bool

// Config wires the handler.
type Config struct {
	Laib     *laib.Client // the haus hand: status, inbox, journal, restart — nothing else
	Validate Validator
	Bridge   *url.URL                           // the migration bridge (bridge.go), for every other /napi path; nil = 404
	Whoami   func(bearer string) (string, bool) // Haus's own devices: uid behind a bearer; others go through the bridge
	House    persona.Persona                    // the house persona the app shows (laib#129, #130)
	HA       *ha.Client                         // Home Assistant for the house paths (home.go, live.go); nil = they stay on the bridge
	Ping     time.Duration                      // the event stream's keepalive; 0 = 90s
	// SensorEvery: a sensor's card_state at most this often; 0 = 10s
	SensorEvery time.Duration
	Energy      EnergyMap // the energy entities (energy.go); empty = by friendly name
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
	// the house persona for the app: name, greeting, colours, icon (laib#130)
	mux.HandleFunc("GET /napi/persona", auth(func(w http.ResponseWriter, r *http.Request) {
		p := c.House
		if p.Display == "" {
			p = persona.Default()
		}
		icon := ""
		if len(p.Icon) > 0 {
			icon = "/napi/persona/icon.svg"
		}
		writeJSON(w, map[string]any{"name": p.Name, "display": p.Display, "greeting": p.Greet(r.URL.Query().Get("lang")),
			"accent": p.Accent(), "ground": p.Ground(), "icon": icon})
	}))
	mux.HandleFunc("GET /napi/persona/icon.svg", auth(func(w http.ResponseWriter, r *http.Request) {
		if len(c.House.Icon) == 0 {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Write(c.House.Icon)
	}))
	mux.HandleFunc("GET "+legacy.NapiPrefix+"/home", auth(func(w http.ResponseWriter, r *http.Request) {
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
	mux.HandleFunc("GET "+legacy.NapiPrefix+"/services", auth(func(w http.ResponseWriter, r *http.Request) {
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
	mux.HandleFunc("GET "+legacy.NapiPrefix+"/approvals", auth(func(w http.ResponseWriter, r *http.Request) {
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
	mux.HandleFunc("GET "+legacy.NapiPrefix+"/upgrades", auth(func(w http.ResponseWriter, r *http.Request) {
		// Laib's images update themselves (podman auto-update) and templates
		// sync by timer; there is no pending-upgrade list to show yet.
		writeJSON(w, map[string]any{"upgrades": []any{}})
	}))
	mux.HandleFunc("POST "+legacy.NapiPrefix+"/services/{name}/operate", auth(func(w http.ResponseWriter, r *http.Request) {
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
	mux.HandleFunc("POST "+legacy.NapiPrefix+"/approvals/{id}/approve", verdict)
	mux.HandleFunc("POST "+legacy.NapiPrefix+"/approvals/{id}/deny", verdict)
	mux.HandleFunc("GET /napi/whoami", func(w http.ResponseWriter, r *http.Request) {
		bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if c.Whoami != nil {
			if uid, ok := c.Whoami(bearer); ok {
				writeJSON(w, map[string]any{"uid": uid, "via": "haus"})
				return
			}
		}
		if c.Bridge != nil {
			httputil.NewSingleHostReverseProxy(c.Bridge).ServeHTTP(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
	})
	if c.HA != nil { // haus#10: the house paths are Haus's own
		(&homeAPI{ha: c.HA, sources: c.Energy}).routes(mux, auth)
		if c.Bridge == nil { // the app's one stream also carries notices and approvals only the bridge has (live.go)
			newLive(c.HA, c.Ping, c.SensorEvery).routes(mux, auth)
		}
	}
	if c.Bridge != nil {
		proxy := httputil.NewSingleHostReverseProxy(c.Bridge)
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
