package napi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/mdopp/haus/internal/cards"
	"github.com/mdopp/haus/internal/ha"
)

// The house paths (haus#10), served from Home Assistant once HAUS_HA_URL is
// set. The shapes are the app's (data/ApiClient.kt); the two rules for
// /napi/ha/call (only actuator domains, sensitive actions want confirmed)
// are structural here.

var (
	nameRE = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

	// what a card may switch. `button` stays off on purpose: a door opener's
	// press unlatches the door and no confirm rule covers it.
	callDomains = set("light", "switch", "cover", "climate", "lock")
	// sensitive: these need confirmed:true, or the call is a 403
	sensitiveDomains      = set("lock", "alarm_control_panel")
	sensitiveServices     = set("open", "unlock", "alarm_disarm")
	sensitiveCoverClasses = set("garage", "door", "gate")
	// on a garage/door/gate cover (or one whose class is unknown) only these
	// are free; everything else, toggles and tilts and future services
	// included, needs confirmed
	coverFreeServices = set("close_cover", "stop_cover", "close_cover_tilt", "stop_cover_tilt")
	// a call names its one entity; data may not widen it to another target
	targetKeys = set("entity_id", "device_id", "area_id", "floor_id", "label_id", "target")

	actuatorDomains = set("light", "switch", "cover", "climate", "media_player", "lock")
	// the attributes the app's controls read off a card
	controlAttrs = []string{"supported_features", "brightness", "color_temp", "rgb_color", "color_mode", "supported_color_modes",
		"current_position", "temperature", "current_temperature", "hvac_action", "target_temp_step", "min_temp", "max_temp",
		"hvac_modes", "volume_level", "media_title", "media_artist"}

	historyRanges = map[string]time.Duration{"24h": 24 * time.Hour, "48h": 48 * time.Hour, "7d": 168 * time.Hour}
)

func set(xs ...string) map[string]bool {
	m := map[string]bool{}
	for _, x := range xs {
		m[x] = true
	}
	return m
}

// homeAPI serves the HA-backed paths.
type homeAPI struct {
	ha      *ha.Client
	sources EnergyMap // which entities the energy picture reads (energy.go)
}

func (h *homeAPI) routes(mux *http.ServeMux, auth func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("POST /napi/ha/call", auth(h.call))
	mux.HandleFunc("GET /napi/portal/state", auth(h.state))
	mux.HandleFunc("GET /napi/portal/energy", auth(h.energy))
	mux.HandleFunc("GET /napi/portal/entity-history", auth(h.history))
	mux.HandleFunc("GET /napi/portal/active", auth(h.active))
	mux.HandleFunc("GET /napi/portal/camera/{entity_id}/snapshot", auth(h.snapshot))
	mux.HandleFunc("GET /napi/portal/start/addable", auth(h.addable))
}

func answer(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func refuse(w http.ResponseWriter, code int, reason string) {
	answer(w, code, map[string]any{"ok": false, "error": reason})
}

// POST /napi/ha/call {entity_id, service, data?, confirmed?}
func (h *homeAPI) call(w http.ResponseWriter, r *http.Request) {
	var body struct {
		EntityID  string         `json:"entity_id"`
		Service   string         `json:"service"`
		Data      map[string]any `json:"data"`
		Confirmed bool           `json:"confirmed"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&body); err != nil {
		refuse(w, http.StatusBadRequest, "bad_json")
		return
	}
	domain, _, _ := strings.Cut(body.EntityID, ".")
	if !callDomains[domain] {
		refuse(w, http.StatusBadRequest, "unsupported_domain")
		return
	}
	if !ha.EntityRE.MatchString(body.EntityID) {
		refuse(w, http.StatusBadRequest, "invalid entity_id")
		return
	}
	sdomain, action, dotted := strings.Cut(body.Service, ".")
	if !dotted || !nameRE.MatchString(sdomain) || !nameRE.MatchString(action) {
		refuse(w, http.StatusBadRequest, "invalid service")
		return
	}
	if sdomain != domain {
		refuse(w, http.StatusBadRequest, "service domain does not match entity")
		return
	}
	for k := range body.Data {
		if targetKeys[k] {
			refuse(w, http.StatusBadRequest, "data may not name a target")
			return
		}
	}
	if !body.Confirmed && h.sensitive(r.Context(), body.EntityID, domain, action) {
		refuse(w, http.StatusForbidden, "sensitive_action")
		return
	}
	payload := map[string]any{}
	for k, v := range body.Data {
		payload[k] = v
	}
	payload["entity_id"] = body.EntityID
	if err := h.ha.Call(r.Context(), domain, action, payload); err != nil {
		log.Printf("haus: ha/call %s on %s: %v", body.Service, body.EntityID, err) // HA's detail stays here
		var he *ha.Error
		if errors.As(err, &he) && he.Status == http.StatusBadRequest {
			refuse(w, http.StatusBadRequest, "ha_rejected")
			return
		}
		refuse(w, http.StatusBadGateway, "ha_unavailable")
		return
	}
	answer(w, http.StatusOK, map[string]any{"ok": true})
}

// sensitive: any lock, an unlock or open anywhere, and on a cover whose
// device_class is garage, door or gate anything but closing and stopping.
// A cover whose class cannot be read (HA down, unknown entity, no class)
// counts as such a cover: fail closed.
func (h *homeAPI) sensitive(ctx context.Context, entityID, domain, action string) bool {
	if sensitiveDomains[domain] || sensitiveServices[action] {
		return true
	}
	if domain != "cover" || coverFreeServices[action] {
		return false
	}
	s, err := h.ha.State(ctx, entityID)
	if err != nil {
		return true
	}
	dc, _ := s.Attributes["device_class"].(string)
	if dc == "" {
		return true
	}
	return sensitiveCoverClasses[strings.ToLower(dc)]
}

// card is one entity as the app's widgets draw it (ApiClient.parseCardJson),
// with its own page on Haus for a tap (haus#28). An absent value is left
// out, never null: Android's optString reads a JSON null as the string
// "null".
func card(s ha.State) map[string]any {
	domain, _, _ := strings.Cut(s.EntityID, ".")
	if !cards.Domains[domain] {
		return nil
	}
	name, _ := s.Attributes["friendly_name"].(string)
	if name == "" {
		name = s.EntityID
	}
	c := map[string]any{"entity_id": s.EntityID, "name": name, "domain": domain, "state": s.State, "page": cards.ThingPage(s.EntityID)}
	if v := s.Attributes["device_class"]; v != nil {
		c["device_class"] = v
	}
	if v := s.Attributes["unit_of_measurement"]; v != nil {
		c["unit"] = v
	}
	if t, err := time.Parse(time.RFC3339Nano, s.LastUpdated); err == nil {
		c["updated_at_ms"] = t.UnixMilli()
	}
	for _, k := range controlAttrs {
		if v := s.Attributes[k]; v != nil {
			c[k] = v
		}
	}
	return c
}

// GET /napi/portal/state?entity_id= → {ok, card}
func (h *homeAPI) state(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("entity_id")
	if !ha.EntityRE.MatchString(id) {
		refuse(w, http.StatusBadRequest, "invalid entity_id")
		return
	}
	s, err := h.ha.State(r.Context(), id)
	switch {
	case errors.Is(err, ha.ErrNotFound):
		refuse(w, http.StatusNotFound, "unknown_entity")
		return
	case err != nil:
		refuse(w, http.StatusBadGateway, "ha_unavailable")
		return
	}
	c := card(s)
	if c == nil {
		refuse(w, http.StatusNotFound, "no_card")
		return
	}
	answer(w, http.StatusOK, map[string]any{"ok": true, "card": c})
}

// GET /napi/portal/entity-history?entity_id=&range=24h|48h|7d → {ok, history:[{t,state}]}
func (h *homeAPI) history(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("entity_id")
	if !ha.EntityRE.MatchString(id) {
		refuse(w, http.StatusBadRequest, "invalid entity_id")
		return
	}
	rng := r.URL.Query().Get("range")
	if rng == "" {
		rng = "24h"
	}
	span, ok := historyRanges[rng]
	if !ok {
		refuse(w, http.StatusBadRequest, "invalid range")
		return
	}
	end := time.Now().UTC().Truncate(time.Second)
	pts, err := h.ha.History(r.Context(), id, end.Add(-span), end)
	if err != nil {
		refuse(w, http.StatusBadGateway, "ha_unavailable")
		return
	}
	out := []map[string]any{}
	for _, p := range pts {
		t := p.LastChanged
		if t == "" {
			t = p.LastUpdated
		}
		if t != "" {
			out = append(out, map[string]any{"t": t, "state": p.State})
		}
	}
	answer(w, http.StatusOK, map[string]any{"ok": true, "history": out})
}

// GET /napi/portal/active → {ok, active:[card + room]}: the actuators that
// are on or open, ordered by room and name.
func (h *homeAPI) active(w http.ResponseWriter, r *http.Request) {
	states, err := h.ha.States(r.Context())
	if err != nil {
		refuse(w, http.StatusBadGateway, "ha_unavailable")
		return
	}
	// rooms only label, they never break a list: the last good map, empty at first
	actx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	reg, _ := h.ha.CachedRegistry(actx)
	rooms := reg.Rooms
	cancel()
	out := []map[string]any{}
	for _, s := range states {
		domain, _, _ := strings.Cut(s.EntityID, ".")
		if !actuatorDomains[domain] {
			continue
		}
		if st := strings.ToLower(s.State); st != "on" && st != "open" {
			continue
		}
		c := card(s)
		if c == nil {
			continue
		}
		c["room"] = rooms[s.EntityID]
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := strings.ToLower(out[i]["room"].(string)), strings.ToLower(out[j]["room"].(string))
		if ri != rj {
			return ri < rj
		}
		return strings.ToLower(out[i]["name"].(string)) < strings.ToLower(out[j]["name"].(string))
	})
	answer(w, http.StatusOK, map[string]any{"ok": true, "active": out})
}
