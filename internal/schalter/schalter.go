// Package schalter is the one rule for what Haus switches in Home
// Assistant (haus#10, haus#35): the widgets' /napi/ha/call and the
// family's pages both call Do, so neither can be wider than the other.
// Only actuator domains; a call names its one entity; sensitive actions
// (any lock, an unlock or open anywhere, a garage, door or gate cover
// doing anything but closing or stopping) want confirmed; a media player
// only pauses, goes on and steps its volume.
package schalter

import (
	"context"
	"net/http"
	"regexp"
	"strings"

	"github.com/mdopp/haus/internal/ha"
)

var (
	nameRE = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

	// what a card may switch. `button` stays off on purpose: a door opener's
	// press unlatches the door and no confirm rule covers it.
	domains = set("light", "switch", "cover", "climate", "lock", "media_player")
	// a domain listed here does only these services; the rest are refused
	only = map[string]map[string]bool{
		// Start's "Pause" (haus#35), Medien's Weiter, Leiser, Lauter
		// (haus#12); playing a URL on the speakers, setting a volume,
		// muting, stopping, a source, turning a player off stay out
		"media_player": set("media_pause", "media_play", "volume_down", "volume_up"),
	}
	// sensitive: these need confirmed, or the call is refused
	sensitiveDomains      = set("lock", "alarm_control_panel")
	sensitiveServices     = set("open", "unlock", "alarm_disarm")
	sensitiveCoverClasses = set("garage", "door", "gate")
	// on a garage/door/gate cover (or one whose class is unknown) only these
	// are free; everything else, toggles and tilts and future services
	// included, needs confirmed
	coverFreeServices = set("close_cover", "stop_cover", "close_cover_tilt", "stop_cover_tilt")
	// a call names its one entity; data may not widen it to another target
	targetKeys = set("entity_id", "device_id", "area_id", "floor_id", "label_id", "target")
)

func set(xs ...string) map[string]bool {
	m := map[string]bool{}
	for _, x := range xs {
		m[x] = true
	}
	return m
}

// Refusal is a call the rule does not allow: the HTTP status and the
// reason the app reads (unsupported_domain, sensitive_action, …).
type Refusal struct {
	Code   int
	Reason string
}

func (r *Refusal) Error() string { return r.Reason }

func refuse(code int, reason string) error { return &Refusal{code, reason} }

// Do checks one call against the rule and, allowed, makes it: service
// ("light.turn_off") on entityID, with data. A *Refusal when the rule
// says no — HA hears nothing then; HA's own error when it does not
// answer.
func Do(ctx context.Context, c *ha.Client, entityID, service string, data map[string]any, confirmed bool) error {
	domain, _, _ := strings.Cut(entityID, ".")
	if !domains[domain] {
		return refuse(http.StatusBadRequest, "unsupported_domain")
	}
	if !ha.EntityRE.MatchString(entityID) {
		return refuse(http.StatusBadRequest, "invalid entity_id")
	}
	sdomain, action, dotted := strings.Cut(service, ".")
	if !dotted || !nameRE.MatchString(sdomain) || !nameRE.MatchString(action) {
		return refuse(http.StatusBadRequest, "invalid service")
	}
	if sdomain != domain {
		return refuse(http.StatusBadRequest, "service domain does not match entity")
	}
	if allowed, ok := only[domain]; ok && !allowed[action] {
		return refuse(http.StatusBadRequest, "unsupported_service")
	}
	for k := range data {
		if targetKeys[k] {
			return refuse(http.StatusBadRequest, "data may not name a target")
		}
	}
	if !confirmed && sensitive(ctx, c, entityID, domain, action) {
		return refuse(http.StatusForbidden, "sensitive_action")
	}
	payload := map[string]any{}
	for k, v := range data {
		payload[k] = v
	}
	payload["entity_id"] = entityID
	return c.Call(ctx, domain, action, payload)
}

// sensitive: any lock, an unlock or open anywhere, and on a cover whose
// device_class is garage, door or gate anything but closing and stopping.
// A cover whose class cannot be read (HA down, unknown entity, no class)
// counts as such a cover: fail closed.
func sensitive(ctx context.Context, c *ha.Client, entityID, domain, action string) bool {
	if sensitiveDomains[domain] || sensitiveServices[action] {
		return true
	}
	if domain != "cover" || coverFreeServices[action] {
		return false
	}
	s, err := c.State(ctx, entityID)
	if err != nil {
		return true
	}
	dc, _ := s.Attributes["device_class"].(string)
	if dc == "" {
		return true
	}
	return sensitiveCoverClasses[strings.ToLower(dc)]
}
