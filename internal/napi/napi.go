// Package napi is the API the Haus app's widgets speak (mdopp/haus-android,
// its data/*.kt parsers; laib ADR 0022): the four Box keys under
// app.BoxPrefix served from Laib, operate and verdicts behind the right
// token, the house paths, the event stream and the camera from Home
// Assistant (home.go, live.go, camera.go), the tool widgets from the
// Haushalt source (tools.go), the capture upload (upload.go) and the house
// persona. Every other /napi path is a JSON 404 (noSuchPath).
package napi

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mdopp/haus/internal/app"
	"github.com/mdopp/haus/internal/ha"
	"github.com/mdopp/haus/internal/haushalt"
	"github.com/mdopp/haus/internal/laib"
	"github.com/mdopp/haus/internal/persona"
)

// Validator says whether a bearer is a paired device.
type Validator func(ctx context.Context, bearer string) bool

// Config wires the handler.
type Config struct {
	Laib     *laib.Client // the haus hand: status, inbox, journal, restart — nothing else
	Validate Validator
	Whoami   func(bearer string) (string, bool) // the paired devices: uid behind a bearer
	House    persona.Persona                    // the house persona the app shows (laib#129, #130)
	HA       *ha.Client                         // Home Assistant for the house paths and the event stream (home.go, live.go); nil = 404
	Ping     time.Duration                      // the event stream's keepalive; 0 = 90s
	// SensorEvery: a sensor's card_state at most this often; 0 = 10s
	SensorEvery time.Duration
	Energy      EnergyMap // the energy entities (energy.go); empty = by friendly name
	// Haushalt: the backends of the tool widgets (tools.go); none set = 404
	Haushalt haushalt.Config
	// UploadDir: where /napi/upload stores (upload.go), the share's Paperless
	// consume folder; "" = 404
	UploadDir string
}

// Handler serves /napi/*.
func Handler(c Config) http.Handler {
	mux := http.NewServeMux()
	auth := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			bearer, ok := bearerOf(r)
			if !ok || c.Validate == nil || !c.Validate(r.Context(), bearer) {
				w.Header().Set("WWW-Authenticate", "Bearer")
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			next(w, r)
		}
	}
	// the house persona for the app: name, greeting, colours, icon (laib#130).
	// The icon is PNG, which a phone draws without a library (haus#28); the
	// SVG is the pages' (/persona/icon.svg), for browsers.
	mux.HandleFunc("GET /napi/persona", auth(func(w http.ResponseWriter, r *http.Request) {
		p := c.House
		if p.Display == "" {
			p = persona.Default()
		}
		icon := ""
		if len(p.IconPNG) > 0 {
			icon = "/napi/persona/icon.png"
		}
		writeJSON(w, map[string]any{"name": p.Name, "display": p.Display, "greeting": p.Greet(r.URL.Query().Get("lang")),
			"accent": p.Accent(), "ground": p.Ground(), "icon": icon})
	}))
	mux.HandleFunc("GET /napi/persona/icon.png", auth(func(w http.ResponseWriter, r *http.Request) {
		if len(c.House.IconPNG) == 0 {
			noSuchPath(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write(c.House.IconPNG)
	}))
	// the Box keys (laib ADR 0022 §4: one name, app.BoxPrefix)
	prefix := app.BoxPrefix
	mux.HandleFunc("GET "+prefix+"/home", auth(func(w http.ResponseWriter, r *http.Request) {
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
			"pendingApprovals": len(open) - len(updates(open)), "pendingUpdates": len(updates(open)),
			"state": ready.State, "drift": ready.Drift, "lastHeard": ready.LastHeard, "operator": ready.Operator,
		})
	}))
	mux.HandleFunc("GET "+prefix+"/services", auth(func(w http.ResponseWriter, r *http.Request) {
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
	mux.HandleFunc("GET "+prefix+"/approvals", auth(func(w http.ResponseWriter, r *http.Request) {
		ps, err := c.Laib.Inbox(r.Context())
		if err != nil {
			fail(w, err)
			return
		}
		out := []map[string]any{}
		for _, p := range ps {
			if isUpdate(p) {
				continue // listed under upgrades
			}
			row := proposalRow(p)
			row["status"] = "pending"
			out = append(out, row)
		}
		writeJSON(w, map[string]any{"approvals": out})
	}))
	mux.HandleFunc("GET "+prefix+"/upgrades", auth(func(w http.ResponseWriter, r *http.Request) {
		ps, err := c.Laib.Inbox(r.Context())
		if err != nil {
			fail(w, err)
			return
		}
		out := []map[string]any{}
		for _, p := range updates(ps) {
			out = append(out, proposalRow(p))
		}
		writeJSON(w, map[string]any{"upgrades": out})
	}))
	mux.HandleFunc("POST "+prefix+"/services/{name}/operate", auth(func(w http.ResponseWriter, r *http.Request) {
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
	mux.HandleFunc("POST "+prefix+"/approvals/{id}/approve", verdict)
	mux.HandleFunc("POST "+prefix+"/approvals/{id}/deny", verdict)
	mux.HandleFunc("GET /napi/whoami", func(w http.ResponseWriter, r *http.Request) {
		if bearer, ok := bearerOf(r); ok && c.Whoami != nil {
			if uid, ok := c.Whoami(bearer); ok {
				writeJSON(w, map[string]any{"uid": uid, "via": "haus"})
				return
			}
		}
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
	})
	if c.HA != nil { // the house paths and the event stream, from Home Assistant
		(&homeAPI{ha: c.HA, sources: c.Energy}).routes(mux, auth)
		newLive(c.HA, c.Ping, c.SensorEvery).routes(mux, auth)
	}
	if t := newTools(c.Haushalt); t != nil { // the tool widgets from the Haushalt source
		t.routes(mux, auth)
	}
	if c.UploadDir != "" { // the camera capture into the consume folder
		newUploads(c.UploadDir).routes(mux, auth)
	}
	// every other /napi path, passed on to nobody; a device first, so a
	// stranger cannot tell which backends are set
	mux.HandleFunc("/napi/", auth(noSuchPath))
	return mux
}

// bearerOf is the token after the literal "Bearer "; anything else is none.
func bearerOf(r *http.Request) (string, bool) {
	b, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return b, ok && b != ""
}

// noSuchPath is the one 404 of /napi: a path Haus does not serve, or whose
// backend is not set. JSON, as every refusal the app parses.
func noSuchPath(w http.ResponseWriter, r *http.Request) {
	answer(w, http.StatusNotFound, map[string]any{"ok": false, "reason": "not_found"})
}

// isUpdate: a template update (haus#28). `laib template sync --propose`
// files one proposal per update, its kind template-update whatever class
// the dial gave it. It waits in Laib's inbox like any proposal, but the Box
// counts and lists it as an update only, never as an approval too; Haus
// decides neither (haus#1).
func isUpdate(p laib.Proposal) bool { return p.Kind == "template-update" }

// updates are the template updates among the open proposals.
func updates(ps []laib.Proposal) []laib.Proposal {
	var out []laib.Proposal
	for _, p := range ps {
		if isUpdate(p) {
			out = append(out, p)
		}
	}
	return out
}

// proposalRow is one inbox entry as the Box keys list it.
func proposalRow(p laib.Proposal) map[string]any {
	return map[string]any{
		"id": strconv.Itoa(p.ID), "service": serviceOf(p.Files), "title": p.Reason,
		"description": p.Tier + " · " + p.Class + " · " + strings.Join(p.Files, ", ") + " · von " + p.Actor,
		"created_at":  p.Created.UTC().Format(time.RFC3339),
	}
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
