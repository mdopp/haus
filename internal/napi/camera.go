package napi

import (
	"context"
	"errors"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mdopp/haus/internal/ha"
)

// The camera widget (haus#10): its picker reads /napi/portal/start/addable
// and keeps domain == camera (CameraWidgetConfigActivity.kt); the device
// picker and the app-icon menu read the same list for the actuators. The
// widget then polls a frame (ApiClient.getCameraSnapshot).

// snapshotMax caps one camera frame; the widget downscales to 640 px anyway.
var snapshotMax int64 = 8 << 20

// what a picker offers: the actuators a device widget switches, and cameras
var addableDomains = set("light", "switch", "cover", "climate", "media_player", "lock", "camera")

// GET /napi/portal/camera/{entity_id}/snapshot → the frame's bytes, camera.* only.
func (h *homeAPI) snapshot(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("entity_id")
	if !entityRE.MatchString(id) || !strings.HasPrefix(id, "camera.") {
		refuse(w, http.StatusBadRequest, "invalid entity_id")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), rowsTimeout)
	defer cancel()
	img, ctype, err := h.ha.Camera(ctx, id, snapshotMax)
	switch {
	case errors.Is(err, ha.ErrNotFound):
		refuse(w, http.StatusNotFound, "unknown_entity")
		return
	case err != nil:
		log.Printf("haus: camera %s: %v", id, err)
		refuse(w, http.StatusBadGateway, "ha_unavailable")
		return
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Content-Length", strconv.Itoa(len(img)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// the widget polls; a burst of refreshes may share a frame, nobody else may keep it
	w.Header().Set("Cache-Control", "private, max-age=5")
	w.Write(img)
}

// GET /napi/portal/start/addable → {ok, rooms:[{room, cards:[{entity_id, name,
// domain, state, device_class?}]}], automations:[]}: rooms by name, the
// entities without one last, cards by name.
func (h *homeAPI) addable(w http.ResponseWriter, r *http.Request) {
	states, err := h.ha.States(r.Context())
	if err != nil {
		refuse(w, http.StatusBadGateway, "ha_unavailable")
		return
	}
	// rooms only label, they never break the list: the shared registry cache (#12), empty at first
	actx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	reg, _ := h.ha.CachedRegistry(actx)
	cancel()
	areas := reg.Rooms
	rooms := map[string][]map[string]any{}
	for _, s := range states {
		domain, _, _ := strings.Cut(s.EntityID, ".")
		if !addableDomains[domain] || !entityRE.MatchString(s.EntityID) {
			continue
		}
		name, _ := s.Attributes["friendly_name"].(string)
		if name == "" {
			name = s.EntityID
		}
		c := map[string]any{"entity_id": s.EntityID, "name": name, "domain": domain, "state": s.State}
		if dc, _ := s.Attributes["device_class"].(string); dc != "" {
			c["device_class"] = dc
		}
		room := areas[s.EntityID]
		rooms[room] = append(rooms[room], c)
	}
	names := make([]string, 0, len(rooms))
	for k := range rooms {
		names = append(names, k)
	}
	sort.Slice(names, func(i, j int) bool {
		if (names[i] == "") != (names[j] == "") {
			return names[j] == ""
		}
		return strings.ToLower(names[i]) < strings.ToLower(names[j])
	})
	out := []map[string]any{}
	for _, room := range names {
		cards := rooms[room]
		sort.SliceStable(cards, func(i, j int) bool {
			return strings.ToLower(cards[i]["name"].(string)) < strings.ToLower(cards[j]["name"].(string))
		})
		out = append(out, map[string]any{"room": room, "cards": cards})
	}
	answer(w, http.StatusOK, map[string]any{"ok": true, "rooms": out, "automations": []any{}})
}
