// Package napi is the API the Homes app's widgets speak (mdopp/homes-android,
// its data/*.kt parsers; laib ADR 0022): the four Box keys under
// app.BoxPrefix served from Laib, operate and verdicts behind the right
// token, the house paths, the event stream and the camera from Home
// Assistant (home.go, live.go, camera.go), the tool widgets from the
// Haushalt source (tools.go), the capture upload (upload.go), the phone's
// message backups (sicherung.go), the phone's contacts mirrored
// (kontakte.go), the household's shared contacts and calendars for the
// phone's Homes account (gemeinsam.go), its positions passed on to Reitti (standort.go), a link shared
// to the app kept as a note (merken.go) and the assistant's name. Every other /napi path is a JSON 404 (noSuchPath).
package napi

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mdopp/homes/internal/app"
	"github.com/mdopp/homes/internal/assistent"
	"github.com/mdopp/homes/internal/caldav"
	"github.com/mdopp/homes/internal/favoriten"
	"github.com/mdopp/homes/internal/ha"
	"github.com/mdopp/homes/internal/haushalt"
	"github.com/mdopp/homes/internal/laib"
	"github.com/mdopp/homes/internal/modul"
	"github.com/mdopp/homes/internal/reitti"
	"github.com/mdopp/homes/internal/schalter"
	"github.com/mdopp/homes/internal/usage"
)

// Validator says whether a bearer is a paired device.
type Validator func(ctx context.Context, bearer string) bool

// Config wires the handler.
type Config struct {
	Laib     *laib.Client // the homes hand: status, inbox, journal, restart — nothing else
	Validate Validator
	Whoami   func(bearer string) (string, bool) // the paired devices: uid behind a bearer
	Witness  *schalter.Witness                  // who switched what (haus#89); nil: the log only
	HA       *ha.Client                         // Home Assistant for the house paths and the event stream (home.go, live.go); nil = 404
	Ping     time.Duration                      // the event stream's keepalive; 0 = 90s
	// SensorEvery: a sensor's card_state at most this often; 0 = 10s
	SensorEvery time.Duration
	Energy      EnergyMap // the energy entities (energy.go); empty = by friendly name
	// OhneEnergie: energie is switched off (ADR 0001 H6) while HA stays for
	// the house paths: /napi/portal/energy is 404
	OhneEnergie bool
	// Favorites: each person's ☆ things, the app's start screen first
	// (/napi/portal/favorites); nil = an empty list
	Favorites *favoriten.Store
	// Now is the clock for today's energy (tests); nil = time.Now
	Now func() time.Time
	// Haushalt: the backends of the tool widgets (tools.go); none set = 404
	Haushalt haushalt.Config
	// UploadDir: where /napi/upload stores (upload.go), the share's Paperless
	// consume folder; "" = 404
	UploadDir string
	// Sicherung: the message archives' inbox (sicherung.go, HOMES_ARCHIV_DIR,
	// haus#207); nil = every /napi/sicherung path answers 503 in words
	Sicherung *Sicherung
	// Reitti: the household's timeline, where /napi/standort passes the
	// app's points on with the person's token (standort.go, HOMES_REITTI_URL);
	// nil = /napi/standort answers 503 in words
	Reitti *reitti.Reitti
	// Konto: the household's shared contacts and calendars for the phone's
	// Homes account (gemeinsam.go, haus#218) and the live stream's frames
	// when they change; the Radicale account Haushalt's Calendar is on, its
	// own field so the account stays with the calendar off (ADR 0001 H5);
	// nil = /napi/gemeinsam answers 503 in words
	Konto *caldav.Client
	// SharedEvery: how often the live stream looks whether the household's
	// shared contacts and calendars changed (haus#223); 0 = 30 s
	SharedEvery time.Duration
	// Usage counts each paired phone's request by path (haus#50): which
	// widgets refresh how often, never which phone; nil counts nothing
	Usage *usage.Counter
	// Module: the box's areas (ADR 0001), GET /napi/kann; nil = 404
	Module *modul.Liste
}

// refreshed is the event a /napi request counts as: its pattern's path,
// the wildcards left out ("GET /napi/portal/camera/{entity_id}/snapshot"
// is napi.portal.camera.snapshot), so no entity or name rides along.
func refreshed(pattern string) string {
	_, path, _ := strings.Cut(pattern, " ")
	var parts []string
	for _, p := range strings.Split(strings.TrimPrefix(path, "/napi/"), "/") {
		if p != "" && !strings.HasPrefix(p, "{") {
			parts = append(parts, p)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "napi." + strings.Join(parts, ".")
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
			if r.Pattern != "/napi/" { // a path Homes does not serve is no widget's
				c.Usage.Count(refreshed(r.Pattern))
			}
			next(w, r)
		}
	}
	// the assistant's name, the one thing the app takes from the box about
	// it: its colours are fixed in the app, its icon is the app's own
	// ({"name":"Homes"}).
	mux.HandleFunc("GET /napi/assistent", auth(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"name": assistent.Name()})
	}))
	// which app this box's Homes speaks to (laib#1021): the old Haus app
	// (cloud.dopp.haus) compares paket with its own package and, when they
	// differ, tells the person in German to install Homes from quelle and
	// to remove Homes once Homes is set up.
	mux.HandleFunc("GET /napi/app", auth(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"name": "Homes", "paket": app.Paket, "quelle": app.Quelle})
	}))
	// Bridge for app builds before homes-android's assistent change (dev.249 and older), which read
	// the name from /napi/persona: the same name in that shape, the fixed colours, no icon. Remove
	// once every paired phone runs an app that reads /napi/assistent.
	mux.HandleFunc("GET /napi/persona", auth(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"name": "haus", "display": assistent.Name(), "greeting": "",
			"accent": "#3ddc84", "ground": "#0c120e", "icon": ""})
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
			out = append(out, map[string]string{"name": s.Name, "title": s.Titel(), "beschreibung": s.Beschreibung, "activeState": active, "subState": sub, "health": health, "detail": s.Detail})
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
	// Verdicts are not Homes' (haus#1, ADR 0012): the one approval surface is
	// Laib's own page behind Authelia; phones decide, if ever, through the
	// relay. The app's buttons get a plain answer.
	verdict := auth(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"Homes entscheidet nichts: Freigeben und Ablehnen gibt es nur auf der Laib-Seite (laib.<domain>/inbox)"}`, http.StatusForbidden)
	})
	mux.HandleFunc("POST "+prefix+"/approvals/{id}/approve", verdict)
	mux.HandleFunc("POST "+prefix+"/approvals/{id}/deny", verdict)
	mux.HandleFunc("GET /napi/kann", auth(c.kann))                 // what this box can do, for the app (ADR 0001 §2)
	mux.HandleFunc("POST /napi/kann/anfordern", auth(c.anfordern)) // a wish for what it cannot yet (§10)
	mux.HandleFunc("GET /napi/whoami", func(w http.ResponseWriter, r *http.Request) {
		if bearer, ok := bearerOf(r); ok && c.Whoami != nil {
			if uid, ok := c.Whoami(bearer); ok {
				writeJSON(w, map[string]any{"uid": uid, "via": "homes"})
				return
			}
		}
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
	})
	// the event stream is the core's (ADR 0001 H6): the house's frames with
	// Home Assistant, the account's gemeinsam frames with konto, pings always
	lv := newLive(c.HA, c.Ping, c.SensorEvery)
	if dav := c.Konto; dav != nil {
		lv.shared, lv.sharedEvery = dav.FamilyVersions, c.SharedEvery
	}
	mux.HandleFunc("GET /napi/portal/events", auth(lv.events))
	c.house(mux, auth, lv)
	if t := newTools(c.Haushalt); t != nil { // the tool widgets from the Haushalt source
		t.routes(mux, auth)
	}
	if c.UploadDir != "" { // the camera capture into the consume folder
		newUploads(c.UploadDir).routes(mux, auth)
	}
	c.Sicherung.routes(mux, auth, c.Whoami) // the phone's message backups (haus#207)
	// every other /napi path, passed on to nobody; a device first, so a
	// stranger cannot tell which backends are set
	mux.HandleFunc("POST /napi/merken", auth(c.merken)) // Share → Homes (haus#120)
	// the phone's contacts, mirrored into the person's Handy-Kontakte (laib ADR 0029)
	k := &kontakte{dav: c.Haushalt.Contacts, whoami: c.Whoami, busy: map[string]bool{}}
	mux.HandleFunc("POST /napi/kontakte", auth(k.post))
	// the household's shared contacts and calendars for the phone's Homes account (haus#218)
	(&gemeinsam{dav: c.Konto, whoami: c.Whoami, loc: c.Haushalt.Loc, now: c.Now}).routes(mux, auth)
	// the phone's positions, passed on to the person's Reitti (owner 2026-11-02)
	(&standort{r: c.Reitti, whoami: c.Whoami}).routes(mux, auth)
	mux.HandleFunc("/napi/", auth(noSuchPath))
	return mux
}

// house mounts the house paths and what the stream watches, from Home
// Assistant (geraete), and the energy picture (energie, unless it is off).
func (c Config) house(mux *http.ServeMux, auth func(http.HandlerFunc) http.HandlerFunc, lv *live) {
	if c.HA == nil {
		return
	}
	h := &homeAPI{ha: c.HA, sources: c.Energy, witness: c.Witness, whoami: c.Whoami, favs: c.Favorites, loc: c.Haushalt.Loc, now: c.Now}
	h.routes(mux, auth)
	mux.HandleFunc("POST /napi/portal/watch", auth(lv.watch))
	if !c.OhneEnergie {
		mux.HandleFunc("GET /napi/portal/energy", auth(h.energy))
	}
}

// bearerOf is the token after the literal "Bearer "; anything else is none.
func bearerOf(r *http.Request) (string, bool) {
	b, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return b, ok && b != ""
}

// noSuchPath is the one 404 of /napi: a path Homes does not serve, or whose
// backend is not set. JSON, as every refusal the app parses.
func noSuchPath(w http.ResponseWriter, r *http.Request) {
	answer(w, http.StatusNotFound, map[string]any{"ok": false, "reason": "not_found"})
}

// isUpdate: a template update (haus#28). `laib template sync --propose`
// files one proposal per update, its kind template-update whatever class
// the dial gave it. It waits in Laib's inbox like any proposal, but the Box
// counts and lists it as an update only, never as an approval too; Homes
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
