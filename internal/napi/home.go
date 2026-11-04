package napi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/mdopp/homes/internal/cards"
	"github.com/mdopp/homes/internal/favoriten"
	"github.com/mdopp/homes/internal/ha"
	"github.com/mdopp/homes/internal/schalter"
)

// The house paths (haus#10), served from Home Assistant once HOMES_HA_URL is
// set. The shapes are the app's (data/ApiClient.kt); what /napi/ha/call
// may switch is package schalter's one rule, the family's pages' too
// (haus#35).

var (
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
	sources EnergyMap                          // which entities the energy picture reads (energy.go)
	witness *schalter.Witness                  // who switched what (haus#89); nil: the log only
	whoami  func(bearer string) (string, bool) // the person behind the device's bearer
	favs    *favoriten.Store                   // each person's ☆ things (haus#35); nil: none
	loc     *time.Location                     // the house's midnight for today's energy; nil: time.Local
	now     func() time.Time                   // the clock; nil: time.Now
}

func (h *homeAPI) routes(mux *http.ServeMux, auth func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("POST /napi/ha/call", auth(h.call))
	mux.HandleFunc("GET /napi/portal/state", auth(h.state))
	mux.HandleFunc("GET /napi/portal/entity-history", auth(h.history))
	mux.HandleFunc("GET /napi/portal/active", auth(h.active))
	mux.HandleFunc("GET /napi/portal/camera/{entity_id}/snapshot", auth(h.snapshot))
	mux.HandleFunc("GET /napi/portal/start/addable", auth(h.addable))
	mux.HandleFunc("GET /napi/portal/favorites", auth(h.favorites))
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
	said, err := schalter.Do(r.Context(), h.ha, body.EntityID, body.Service, body.Data, body.Confirmed)
	var no *schalter.Refusal
	switch {
	case err == nil:
		person := ""
		if bearer, ok := bearerOf(r); ok && h.whoami != nil {
			person, _ = h.whoami(bearer)
		}
		h.witness.Saw(person, body.Service, body.EntityID, said)
		answer(w, http.StatusOK, map[string]any{"ok": true})
	case errors.As(err, &no):
		refuse(w, no.Code, no.Reason)
	default:
		log.Printf("homes: ha/call %s on %s: %v", body.Service, body.EntityID, err) // HA's detail stays here
		var he *ha.Error
		if errors.As(err, &he) && he.Status == http.StatusBadRequest {
			refuse(w, http.StatusBadRequest, "ha_rejected")
			return
		}
		refuse(w, http.StatusBadGateway, "ha_unavailable")
	}
}

// card is one entity as the app's widgets draw it (ApiClient.parseCardJson),
// with its own page on Homes for a tap (haus#28). An absent value is left
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

// GET /napi/portal/favorites → {ok, favorites:[card]}: the things the
// device's person pinned with ☆ (haus#35), in their order, as the app's
// start screen shows them first. A pinned thing HA no longer has, or one
// Homes draws no card for, is left out; a person with none, or no store, is
// an empty list. A store that does not read is a 500, never "none".
func (h *homeAPI) favorites(w http.ResponseWriter, r *http.Request) {
	person := ""
	if bearer, ok := bearerOf(r); ok && h.whoami != nil {
		person, _ = h.whoami(bearer)
	}
	out := []map[string]any{}
	if h.favs == nil || person == "" {
		answer(w, http.StatusOK, map[string]any{"ok": true, "favorites": out})
		return
	}
	ids, err := h.favs.For(person)
	if err != nil {
		log.Printf("homes: favorites of %s: %v", person, err)
		refuse(w, http.StatusInternalServerError, "favorites_unreadable")
		return
	}
	if len(ids) == 0 {
		answer(w, http.StatusOK, map[string]any{"ok": true, "favorites": out})
		return
	}
	states, err := h.ha.States(r.Context())
	if err != nil {
		refuse(w, http.StatusBadGateway, "ha_unavailable")
		return
	}
	byID := make(map[string]ha.State, len(states))
	for _, s := range states {
		byID[s.EntityID] = s
	}
	for _, id := range ids {
		s, ok := byID[id]
		if !ok {
			continue
		}
		if c := card(s); c != nil {
			out = append(out, c)
		}
	}
	answer(w, http.StatusOK, map[string]any{"ok": true, "favorites": out})
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
