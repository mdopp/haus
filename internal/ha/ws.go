package ha

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// HA's WebSocket API (/api/websocket): the registries and state_changed live
// only there. Stdlib only, so this is the small client side of RFC 6455 HA
// needs: one text message at a time, masked writes, ping/pong, close.

const maxMessage = 64 << 20 // a big house's entity registry is a few MB

type wsConn struct {
	nc  net.Conn
	br  *bufio.Reader
	wmu sync.Mutex
	id  int
}

func (c *Client) dial(ctx context.Context) (*wsConn, error) {
	u, err := url.Parse(strings.TrimRight(c.URL, "/") + "/api/websocket")
	if err != nil {
		return nil, err
	}
	host := u.Host
	if u.Port() == "" {
		if u.Scheme == "https" || u.Scheme == "wss" {
			host = net.JoinHostPort(u.Hostname(), "443")
		} else {
			host = net.JoinHostPort(u.Hostname(), "80")
		}
	}
	d := &net.Dialer{Timeout: 10 * time.Second}
	var nc net.Conn
	switch u.Scheme {
	case "http", "ws":
		nc, err = d.DialContext(ctx, "tcp", host)
	case "https", "wss":
		nc, err = (&tls.Dialer{NetDialer: d, Config: &tls.Config{ServerName: u.Hostname()}}).DialContext(ctx, "tcp", host)
	default:
		return nil, fmt.Errorf("ha: scheme %q", u.Scheme)
	}
	if err != nil {
		return nil, err
	}
	var k [16]byte
	rand.Read(k[:])
	key := base64.StdEncoding.EncodeToString(k[:])
	nc.SetDeadline(time.Now().Add(10 * time.Second))
	fmt.Fprintf(nc, "GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n",
		u.RequestURI(), u.Host, key)
	br := bufio.NewReader(nc)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		nc.Close()
		return nil, err
	}
	resp.Body.Close()
	sum := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	if resp.StatusCode != http.StatusSwitchingProtocols || resp.Header.Get("Sec-WebSocket-Accept") != base64.StdEncoding.EncodeToString(sum[:]) {
		nc.Close()
		return nil, fmt.Errorf("ha: websocket handshake: %s", resp.Status)
	}
	nc.SetDeadline(time.Time{})
	ws := &wsConn{nc: nc, br: br}
	if err := ws.auth(c.Token); err != nil {
		nc.Close()
		return nil, err
	}
	return ws, nil
}

func (w *wsConn) auth(token string) error {
	w.nc.SetDeadline(time.Now().Add(10 * time.Second))
	defer w.nc.SetDeadline(time.Time{})
	var m struct {
		Type string `json:"type"`
	}
	if err := w.readJSON(&m); err != nil || m.Type != "auth_required" {
		return fmt.Errorf("ha: websocket greeting %q: %v", m.Type, err)
	}
	if err := w.writeJSON(map[string]string{"type": "auth", "access_token": token}); err != nil {
		return err
	}
	if err := w.readJSON(&m); err != nil {
		return err
	}
	if m.Type != "auth_ok" {
		return fmt.Errorf("ha: websocket auth: %s", m.Type)
	}
	return nil
}

// send writes a command with the next id and returns that id.
func (w *wsConn) send(cmd map[string]any) (int, error) {
	w.wmu.Lock()
	w.id++
	id := w.id
	w.wmu.Unlock()
	cmd["id"] = id
	return id, w.writeJSON(cmd)
}

func (w *wsConn) writeJSON(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return w.writeFrame(0x1, b)
}

func (w *wsConn) writeFrame(op byte, p []byte) error {
	w.wmu.Lock()
	defer w.wmu.Unlock()
	hdr := []byte{0x80 | op}
	switch n := len(p); {
	case n < 126:
		hdr = append(hdr, 0x80|byte(n))
	case n < 1<<16:
		hdr = append(hdr, 0x80|126, byte(n>>8), byte(n))
	default:
		hdr = append(hdr, 0x80|127)
		hdr = binary.BigEndian.AppendUint64(hdr, uint64(n))
	}
	var key [4]byte
	rand.Read(key[:])
	hdr = append(hdr, key[:]...)
	masked := make([]byte, len(p))
	for i := range p {
		masked[i] = p[i] ^ key[i%4]
	}
	_, err := w.nc.Write(append(hdr, masked...))
	return err
}

func (w *wsConn) readJSON(v any) error {
	b, err := w.read()
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// read returns the next complete text or binary message.
func (w *wsConn) read() ([]byte, error) {
	var msg []byte
	for {
		var h [2]byte
		if _, err := io.ReadFull(w.br, h[:]); err != nil {
			return nil, err
		}
		fin, op, n := h[0]&0x80 != 0, h[0]&0x0f, uint64(h[1]&0x7f)
		switch n {
		case 126:
			var e [2]byte
			if _, err := io.ReadFull(w.br, e[:]); err != nil {
				return nil, err
			}
			n = uint64(binary.BigEndian.Uint16(e[:]))
		case 127:
			var e [8]byte
			if _, err := io.ReadFull(w.br, e[:]); err != nil {
				return nil, err
			}
			n = binary.BigEndian.Uint64(e[:])
		}
		if n+uint64(len(msg)) > maxMessage {
			return nil, errors.New("ha: websocket message too large")
		}
		var key []byte
		if h[1]&0x80 != 0 {
			key = make([]byte, 4)
			if _, err := io.ReadFull(w.br, key); err != nil {
				return nil, err
			}
		}
		p := make([]byte, n)
		if _, err := io.ReadFull(w.br, p); err != nil {
			return nil, err
		}
		if key != nil {
			for i := range p {
				p[i] ^= key[i%4]
			}
		}
		switch op {
		case 0x8: // close
			w.writeFrame(0x8, nil)
			return nil, io.EOF
		case 0x9: // ping
			if err := w.writeFrame(0xa, p); err != nil {
				return nil, err
			}
			continue
		case 0xa: // pong
			continue
		}
		msg = append(msg, p...)
		if fin {
			return msg, nil
		}
	}
}

func (w *wsConn) close() error { return w.nc.Close() }

// closeOn closes the connection when ctx ends, so a blocked read returns.
func (w *wsConn) closeOn(ctx context.Context) (stop func()) {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			w.nc.Close()
		case <-done:
		}
	}()
	return func() { close(done) }
}

// result reads until the result for id and decodes it into out.
func (w *wsConn) result(id int, out any) error {
	for {
		var m struct {
			ID      int             `json:"id"`
			Type    string          `json:"type"`
			Success bool            `json:"success"`
			Result  json.RawMessage `json:"result"`
			Error   struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := w.readJSON(&m); err != nil {
			return err
		}
		if m.ID != id || m.Type != "result" {
			continue
		}
		if !m.Success {
			return fmt.Errorf("ha: command %d: %s", id, m.Error.Message)
		}
		if out == nil || len(m.Result) == 0 {
			return nil
		}
		return json.Unmarshal(m.Result, out)
	}
}

// Areas maps every entity to its room's name: the entity's own area, else
// its device's. Entities without one are absent.
func (c *Client) Areas(ctx context.Context) (map[string]string, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
	}
	w, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}
	defer w.close()
	defer w.closeOn(ctx)()
	var areas []struct {
		AreaID string `json:"area_id"`
		Name   string `json:"name"`
	}
	var devices []struct {
		ID     string `json:"id"`
		AreaID string `json:"area_id"`
	}
	var entities []struct {
		EntityID string `json:"entity_id"`
		AreaID   string `json:"area_id"`
		DeviceID string `json:"device_id"`
	}
	for _, q := range []struct {
		cmd string
		out any
	}{{"config/area_registry/list", &areas}, {"config/device_registry/list", &devices}, {"config/entity_registry/list", &entities}} {
		id, err := w.send(map[string]any{"type": q.cmd})
		if err != nil {
			return nil, err
		}
		if err := w.result(id, q.out); err != nil {
			return nil, err
		}
	}
	name := map[string]string{}
	for _, a := range areas {
		if a.AreaID != "" {
			name[a.AreaID] = a.Name
			if a.Name == "" {
				name[a.AreaID] = a.AreaID
			}
		}
	}
	devArea := map[string]string{}
	for _, d := range devices {
		devArea[d.ID] = d.AreaID
	}
	out := map[string]string{}
	for _, e := range entities {
		area := e.AreaID
		if area == "" {
			area = devArea[e.DeviceID]
		}
		if n := name[area]; n != "" && e.EntityID != "" {
			out[e.EntityID] = n
		}
	}
	return out, nil
}

// Subscribe calls fn with every new state HA reports (state_changed) until
// ctx ends or the connection drops; the caller reconnects. An HA-level ping
// every 30s, and a read deadline, notice a dead connection.
func (c *Client) Subscribe(ctx context.Context, fn func(State)) error {
	return c.SubscribeSynced(ctx, nil, fn)
}

// SubscribeSynced is Subscribe, and calls synced (in its own goroutine)
// once HA has confirmed the subscription: from then on no change is
// missed, so that is the moment to read what changed while away.
func (c *Client) SubscribeSynced(ctx context.Context, synced func(), fn func(State)) error {
	w, err := c.dial(ctx)
	if err != nil {
		return err
	}
	defer w.close()
	defer w.closeOn(ctx)()
	subID, err := w.send(map[string]any{"type": "subscribe_events", "event_type": "state_changed"})
	if err != nil {
		return err
	}
	ping := time.NewTicker(30 * time.Second)
	defer ping.Stop()
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-ping.C:
				if _, err := w.send(map[string]any{"type": "ping"}); err != nil {
					return
				}
			}
		}
	}()
	for {
		w.nc.SetReadDeadline(time.Now().Add(90 * time.Second))
		var m struct {
			ID      int    `json:"id"`
			Type    string `json:"type"`
			Success *bool  `json:"success"`
			Event   struct {
				Data struct {
					EntityID string `json:"entity_id"`
					NewState *State `json:"new_state"`
				} `json:"data"`
			} `json:"event"`
		}
		if err := w.readJSON(&m); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		switch {
		case m.Type == "result" && m.ID == subID && m.Success != nil && !*m.Success:
			return errors.New("ha: subscribe_events refused")
		case m.Type == "result" && m.ID == subID && m.Success != nil && synced != nil:
			go synced()
		case m.Type == "event" && m.Event.Data.NewState != nil:
			s := *m.Event.Data.NewState
			if s.EntityID == "" {
				s.EntityID = m.Event.Data.EntityID
			}
			fn(s)
		}
	}
}
