// Package hafake is a fake Home Assistant for tests: the REST calls Haus
// makes (/api/states, /api/services, /api/history/period) and the WebSocket
// (/api/websocket: auth, subscribe_events, the three registries). Its frame
// code is written for the server side on purpose, so it shares nothing with
// the client it tests. Never a live box.
package hafake

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Call is one service call HA received.
type Call struct {
	Domain, Service string
	Data            map[string]any
}

// HA is the fake.
type HA struct {
	*httptest.Server
	Token string

	mu       sync.Mutex
	states   map[string]map[string]any
	order    []string
	calls    []Call
	history  map[string][]map[string]any
	queries  []url.Values
	starts   []string
	down     bool
	callFail int
	areas    []map[string]any
	devices  []map[string]any
	entities []map[string]any
	subs     []*conn
}

// New starts a fake HA that accepts Bearer token.
func New(token string) *HA {
	h := &HA{Token: token, states: map[string]map[string]any{}, history: map[string][]map[string]any{}}
	h.Server = httptest.NewServer(http.HandlerFunc(h.serve))
	return h
}

// Set stores an entity's state (no event).
func (h *HA) Set(entityID, state string, attrs map[string]any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.set(entityID, state, attrs)
}

func (h *HA) set(entityID, state string, attrs map[string]any) map[string]any {
	if attrs == nil {
		attrs = map[string]any{}
	}
	if _, ok := h.states[entityID]; !ok {
		h.order = append(h.order, entityID)
	}
	s := map[string]any{"entity_id": entityID, "state": state, "attributes": attrs,
		"last_updated": "2026-10-14T10:00:00.123456+00:00", "last_changed": "2026-10-14T10:00:00.123456+00:00"}
	h.states[entityID] = s
	return s
}

// Change stores a state and sends state_changed to every subscriber.
func (h *HA) Change(entityID, state string, attrs map[string]any) {
	h.mu.Lock()
	s := h.set(entityID, state, attrs)
	subs := append([]*conn(nil), h.subs...)
	h.mu.Unlock()
	for _, c := range subs {
		c.send(map[string]any{"id": c.subID, "type": "event", "event": map[string]any{
			"event_type": "state_changed", "data": map[string]any{"entity_id": entityID, "new_state": s}}})
	}
}

// Subscribed is how many WebSocket clients hold a state_changed subscription.
func (h *HA) Subscribed() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

// WaitSubscribed blocks until n clients subscribe, or 5s pass.
func (h *HA) WaitSubscribed(n int) bool {
	for i := 0; i < 500; i++ {
		if h.Subscribed() >= n {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// Calls are the service calls received so far.
func (h *HA) Calls() []Call {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]Call(nil), h.calls...)
}

// SetHistory sets what /api/history/period answers for an entity.
func (h *HA) SetHistory(entityID string, points []map[string]any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.history[entityID] = points
}

// HistoryQueries are the history requests: the start (path) and the query.
func (h *HA) HistoryQueries() ([]string, []url.Values) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.starts...), append([]url.Values(nil), h.queries...)
}

// SetRegistries sets the area, device and entity registries.
func (h *HA) SetRegistries(areas, devices, entities []map[string]any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.areas, h.devices, h.entities = areas, devices, entities
}

// FailCalls makes service calls answer status (0: succeed again).
func (h *HA) FailCalls(status int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.callFail = status
}

// DropAll closes every WebSocket connection, as an HA restart does.
func (h *HA) DropAll() {
	h.mu.Lock()
	subs := append([]*conn(nil), h.subs...)
	h.mu.Unlock()
	for _, c := range subs {
		c.nc.Close()
	}
}

// WaitSubscribedExactly blocks until exactly n clients subscribe, or 5s pass.
func (h *HA) WaitSubscribedExactly(n int) bool {
	for i := 0; i < 500; i++ {
		if h.Subscribed() == n {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// Down makes every REST call answer 500.
func (h *HA) Down(down bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.down = down
}

func (h *HA) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/websocket" {
		h.websocket(w, r)
		return
	}
	if r.Header.Get("Authorization") != "Bearer "+h.Token {
		http.Error(w, "401: Unauthorized", http.StatusUnauthorized)
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.down {
		http.Error(w, "500: boom", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == "GET" && r.URL.Path == "/api/":
		w.Write([]byte(`{"message":"API running."}`))
	case r.Method == "GET" && r.URL.Path == "/api/states":
		out := []map[string]any{}
		for _, id := range h.order {
			out = append(out, h.states[id])
		}
		json.NewEncoder(w).Encode(out)
	case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/api/states/"):
		s, ok := h.states[strings.TrimPrefix(r.URL.Path, "/api/states/")]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message":"Entity not found."}`))
			return
		}
		json.NewEncoder(w).Encode(s)
	case r.Method == "POST" && strings.HasPrefix(r.URL.Path, "/api/services/"):
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/services/"), "/")
		if len(parts) != 2 {
			http.NotFound(w, r)
			return
		}
		if h.callFail != 0 {
			http.Error(w, "service failed: some detail", h.callFail)
			return
		}
		var data map[string]any
		json.NewDecoder(r.Body).Decode(&data)
		h.calls = append(h.calls, Call{Domain: parts[0], Service: parts[1], Data: data})
		w.Write([]byte(`[]`))
	case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/api/history/period/"):
		h.starts = append(h.starts, strings.TrimPrefix(r.URL.Path, "/api/history/period/"))
		h.queries = append(h.queries, r.URL.Query())
		pts, ok := h.history[r.URL.Query().Get("filter_entity_id")]
		if !ok {
			w.Write([]byte(`[]`))
			return
		}
		json.NewEncoder(w).Encode([][]map[string]any{pts})
	default:
		http.NotFound(w, r)
	}
}

// --- the WebSocket, server side (RFC 6455) ---

type conn struct {
	nc    net.Conn
	br    *bufio.Reader
	wmu   sync.Mutex
	subID any
}

func (c *conn) send(v any) {
	b, _ := json.Marshal(v)
	c.wmu.Lock()
	defer c.wmu.Unlock()
	hdr := []byte{0x81}
	switch n := len(b); {
	case n < 126:
		hdr = append(hdr, byte(n))
	case n < 1<<16:
		hdr = append(hdr, 126, byte(n>>8), byte(n))
	default:
		hdr = append(hdr, 127)
		hdr = binary.BigEndian.AppendUint64(hdr, uint64(n))
	}
	c.nc.Write(append(hdr, b...))
}

// read returns one client text message (clients must mask).
func (c *conn) read() ([]byte, error) {
	var msg []byte
	for {
		var h [2]byte
		if _, err := io.ReadFull(c.br, h[:]); err != nil {
			return nil, err
		}
		op, masked, n := h[0]&0x0f, h[1]&0x80 != 0, uint64(h[1]&0x7f)
		switch n {
		case 126:
			var e [2]byte
			if _, err := io.ReadFull(c.br, e[:]); err != nil {
				return nil, err
			}
			n = uint64(binary.BigEndian.Uint16(e[:]))
		case 127:
			var e [8]byte
			if _, err := io.ReadFull(c.br, e[:]); err != nil {
				return nil, err
			}
			n = binary.BigEndian.Uint64(e[:])
		}
		if !masked {
			return nil, io.ErrUnexpectedEOF // a client frame must be masked
		}
		var key [4]byte
		if _, err := io.ReadFull(c.br, key[:]); err != nil {
			return nil, err
		}
		p := make([]byte, n)
		if _, err := io.ReadFull(c.br, p); err != nil {
			return nil, err
		}
		for i := range p {
			p[i] ^= key[i%4]
		}
		switch op {
		case 0x8:
			return nil, io.EOF
		case 0x9:
			c.wmu.Lock()
			c.nc.Write(append([]byte{0x8a, byte(len(p))}, p...))
			c.wmu.Unlock()
			continue
		case 0xa:
			continue
		}
		msg = append(msg, p...)
		if h[0]&0x80 != 0 {
			return msg, nil
		}
	}
}

func (h *HA) websocket(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("Sec-WebSocket-Key")
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") || key == "" {
		http.Error(w, "not a websocket", http.StatusBadRequest)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "no hijack", 500)
		return
	}
	nc, brw, err := hj.Hijack()
	if err != nil {
		return
	}
	defer nc.Close()
	sum := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	brw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " +
		base64.StdEncoding.EncodeToString(sum[:]) + "\r\n\r\n")
	brw.Flush()
	c := &conn{nc: nc, br: brw.Reader}
	defer func() {
		h.mu.Lock()
		for i, s := range h.subs {
			if s == c {
				h.subs = append(h.subs[:i], h.subs[i+1:]...)
				break
			}
		}
		h.mu.Unlock()
	}()
	c.send(map[string]any{"type": "auth_required", "ha_version": "2026.10.0"})
	authed := false
	for {
		b, err := c.read()
		if err != nil {
			return
		}
		var m map[string]any
		if json.Unmarshal(b, &m) != nil {
			return
		}
		if !authed {
			if m["type"] != "auth" || m["access_token"] != h.Token {
				c.send(map[string]any{"type": "auth_invalid", "message": "Invalid access token"})
				return
			}
			authed = true
			c.send(map[string]any{"type": "auth_ok", "ha_version": "2026.10.0"})
			continue
		}
		h.mu.Lock()
		var result any
		switch m["type"] {
		case "subscribe_events":
			if m["event_type"] == "state_changed" {
				c.subID = m["id"]
				h.subs = append(h.subs, c)
			}
		case "config/area_registry/list":
			result = h.areas
		case "config/device_registry/list":
			result = h.devices
		case "config/entity_registry/list":
			result = h.entities
		}
		h.mu.Unlock()
		if result == nil {
			result = []any{}
		}
		if m["type"] == "subscribe_events" {
			result = nil
		}
		c.send(map[string]any{"id": m["id"], "type": "result", "success": true, "result": result})
	}
}
