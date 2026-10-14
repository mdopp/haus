package napi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mdopp/haus/internal/ha"
)

// The house paths (haus#10), served from Home Assistant once HAUS_HA_URL is
// set. The shapes are the app's (data/ApiClient.kt); the two rules for
// /napi/ha/call are the companion contract's and stay structural here.

var (
	entityRE = regexp.MustCompile(`^[a-z_]+\.[a-z0-9_]+$`)
	nameRE   = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

	// what a card may switch. `button` stays off on purpose: a door opener's
	// press unlatches the door and no confirm rule covers it.
	callDomains = set("light", "switch", "cover", "climate", "lock")
	// sensitive: these need confirmed:true, or the call is a 403
	sensitiveDomains      = set("lock", "alarm_control_panel")
	sensitiveServices     = set("open", "unlock", "alarm_disarm")
	sensitiveCoverClasses = set("garage", "door", "gate")
	coverOpenServices     = set("open_cover", "toggle", "set_cover_position", "set_cover_tilt_position", "open_cover_tilt")
	// a call names its one entity; data may not widen it to another target
	targetKeys = set("entity_id", "device_id", "area_id", "floor_id", "label_id")

	cardDomains     = set("sensor", "binary_sensor", "cover", "light", "switch", "climate", "media_player", "lock")
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
	ha    *ha.Client
	areas areaCache
}

func (h *homeAPI) routes(mux *http.ServeMux, auth func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("POST /napi/ha/call", auth(h.call))
	mux.HandleFunc("GET /napi/portal/state", auth(h.state))
	mux.HandleFunc("GET /napi/portal/energy", auth(h.energy))
	mux.HandleFunc("GET /napi/portal/entity-history", auth(h.history))
	mux.HandleFunc("GET /napi/portal/active", auth(h.active))
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
	if !entityRE.MatchString(body.EntityID) {
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
		var he *ha.Error
		if errors.As(err, &he) {
			refuse(w, http.StatusBadRequest, he.Error())
			return
		}
		refuse(w, http.StatusBadGateway, "ha_unavailable")
		return
	}
	answer(w, http.StatusOK, map[string]any{"ok": true})
}

// sensitive: any lock, an unlock or open anywhere, and a cover open whose
// device_class is garage, door or gate. A cover whose class cannot be read
// (HA down, unknown entity, no class) is sensitive: fail closed.
func (h *homeAPI) sensitive(ctx context.Context, entityID, domain, action string) bool {
	if sensitiveDomains[domain] || sensitiveServices[action] {
		return true
	}
	if domain != "cover" || !coverOpenServices[action] {
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

// card is one entity as the app's widgets draw it (ApiClient.parseCardJson).
// An absent value is left out, never null: Android's optString reads a JSON
// null as the string "null".
func card(s ha.State) map[string]any {
	domain, _, _ := strings.Cut(s.EntityID, ".")
	if !cardDomains[domain] {
		return nil
	}
	name, _ := s.Attributes["friendly_name"].(string)
	if name == "" {
		name = s.EntityID
	}
	c := map[string]any{"entity_id": s.EntityID, "name": name, "domain": domain, "state": s.State}
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
	if !entityRE.MatchString(id) {
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

// The energy picture: the "now" flow from current-power sensors (W) matched
// by exact friendly name, the lifetime counters (kWh) as totals, every other
// power sensor as a circuit. The names are this house's HA naming.
var (
	energyFlow = []struct{ key, label, name, sense string }{
		{"pv", "PV", "Aktuell erzeugter PV-Strom", "supply"},
		{"house", "Haus", "Aktueller Hausverbrauch", "draw"},
		{"grid", "Netz", "Aktuelle Netz Leistung", "grid"},       // +W Bezug, −W Einspeisung
		{"battery", "Akku", "Aktuelle Akku Leistung", "battery"}, // −W entlädt, +W lädt
	}
	energyTotals = []struct{ name, label string }{
		{"PV Erzeugung", "PV-Erzeugung"},
		{"Netzeinspeisung", "Einspeisung"},
		{"Netzbezug", "Netzbezug"},
		{"Batterie laden", "Batterie geladen"},
		{"Batterie entladen", "Batterie entladen"},
	}
)

// GET /napi/portal/energy → {ok, energy:{flow, totals, circuits}}
func (h *homeAPI) energy(w http.ResponseWriter, r *http.Request) {
	states, err := h.ha.States(r.Context())
	if err != nil {
		refuse(w, http.StatusBadGateway, "ha_unavailable")
		return
	}
	flowKey := map[string]string{}
	for _, f := range energyFlow {
		flowKey[f.name] = f.key
	}
	totalLabel := map[string]string{}
	for _, t := range energyTotals {
		totalLabel[t.name] = t.label
	}
	flow := map[string]map[string]any{}
	totals := map[string]map[string]any{}
	circuits := []map[string]any{}
	for _, s := range states {
		if !strings.HasPrefix(s.EntityID, "sensor.") {
			continue
		}
		dc, _ := s.Attributes["device_class"].(string)
		dc = strings.ToLower(dc)
		if dc != "power" && dc != "energy" {
			continue
		}
		name, _ := s.Attributes["friendly_name"].(string)
		if name == "" {
			name = s.EntityID
		}
		spec := map[string]any{"entity_id": s.EntityID, "name": name, "state": s.State, "device_class": dc}
		if u := s.Attributes["unit_of_measurement"]; u != nil {
			spec["unit"] = u
		}
		key, isFlow := flowKey[name]
		switch {
		case dc == "power" && isFlow && flow[key] == nil:
			flow[key] = spec
		case dc == "energy" && totalLabel[name] != "":
			if totals[name] == nil {
				spec["label"] = totalLabel[name]
				totals[name] = spec
			}
		case dc == "power":
			circuits = append(circuits, spec)
		}
	}
	sort.SliceStable(circuits, func(i, j int) bool {
		return strings.ToLower(circuits[i]["name"].(string)) < strings.ToLower(circuits[j]["name"].(string))
	})
	out := map[string]any{"flow": []map[string]any{}, "totals": []map[string]any{}, "circuits": circuits}
	for _, f := range energyFlow {
		if s := flow[f.key]; s != nil {
			s["label"], s["sense"] = f.label, f.sense
			out["flow"] = append(out["flow"].([]map[string]any), s)
		}
	}
	for _, t := range energyTotals {
		if s := totals[t.name]; s != nil {
			out["totals"] = append(out["totals"].([]map[string]any), s)
		}
	}
	answer(w, http.StatusOK, map[string]any{"ok": true, "energy": out})
}

// GET /napi/portal/entity-history?entity_id=&range=24h|48h|7d → {ok, history:[{t,state}]}
func (h *homeAPI) history(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("entity_id")
	if !entityRE.MatchString(id) {
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
	rooms := h.areas.get(r.Context(), h.ha)
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

// areaCache holds entity → room for a few minutes. A failed read keeps the
// last good map (empty at first): rooms only label, they never break a list.
type areaCache struct {
	mu   sync.Mutex
	m    map[string]string
	next time.Time
}

func (a *areaCache) get(ctx context.Context, c *ha.Client) map[string]string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if time.Now().Before(a.next) {
		return a.m
	}
	m, err := c.Areas(ctx)
	if err != nil {
		a.next = time.Now().Add(30 * time.Second)
		return a.m
	}
	a.m, a.next = m, time.Now().Add(5*time.Minute)
	return a.m
}
