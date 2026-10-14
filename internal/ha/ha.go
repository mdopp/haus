// Package ha is Haus's client for Home Assistant: its REST API for states,
// history and service calls, its WebSocket for state_changed and the area
// registry. URL and long-lived token come from the environment (HAUS_HA_URL,
// HAUS_HA_TOKEN), never from a file in the repo. Haus reads and relays; the
// rules about what may be switched live in internal/napi (haus#10).
package ha

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Client talks to one Home Assistant.
type Client struct {
	URL   string // http://host.containers.internal:8123
	Token string // a long-lived access token
	HTTP  *http.Client
}

// FromEnv reads HAUS_HA_URL and HAUS_HA_TOKEN. Unset URL means no HA: the
// house paths stay on the migration bridge.
func FromEnv() (*Client, error) {
	u := strings.TrimRight(os.Getenv("HAUS_HA_URL"), "/")
	if u == "" {
		return nil, nil
	}
	if p, err := url.Parse(u); err != nil || (p.Scheme != "http" && p.Scheme != "https") || p.Host == "" {
		return nil, fmt.Errorf("HAUS_HA_URL %q: want http(s)://host[:port]", u)
	}
	tok := os.Getenv("HAUS_HA_TOKEN")
	if tok == "" {
		return nil, errors.New("HAUS_HA_URL is set but HAUS_HA_TOKEN is not (a long-lived token from HA's profile page)")
	}
	return &Client{URL: u, Token: tok}, nil
}

// State is one entity's state as HA reports it.
type State struct {
	EntityID    string         `json:"entity_id"`
	State       string         `json:"state"`
	Attributes  map[string]any `json:"attributes"`
	LastUpdated string         `json:"last_updated"`
	LastChanged string         `json:"last_changed"`
}

// ErrNotFound is HA's 404 for an entity.
var ErrNotFound = errors.New("ha: unknown entity")

// Error is an HA answer >= 400.
type Error struct {
	Status int
	Detail string
}

func (e *Error) Error() string { return fmt.Sprintf("HA %d: %s", e.Status, e.Detail) }

var defaultHTTP = &http.Client{Timeout: 15 * time.Second}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return defaultHTTP
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	u := strings.TrimRight(c.URL, "/") + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return &Error{Status: resp.StatusCode, Detail: strings.TrimSpace(string(b))}
	}
	if out == nil {
		io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Ping asks whether HA's API answers (GET /api/).
func (c *Client) Ping(ctx context.Context) error {
	return c.do(ctx, "GET", "/api/", nil, nil, nil)
}

// State reads one entity (ErrNotFound when HA does not know it).
func (c *Client) State(ctx context.Context, entityID string) (State, error) {
	var s State
	err := c.do(ctx, "GET", "/api/states/"+url.PathEscape(entityID), nil, nil, &s)
	var he *Error
	if errors.As(err, &he) && he.Status == http.StatusNotFound {
		return s, ErrNotFound
	}
	return s, err
}

// States reads every entity in one call.
func (c *Client) States(ctx context.Context) ([]State, error) {
	var out []State
	return out, c.do(ctx, "GET", "/api/states", nil, nil, &out)
}

// Call runs domain.service with payload (which names the target).
func (c *Client) Call(ctx context.Context, domain, service string, payload map[string]any) error {
	return c.do(ctx, "POST", "/api/services/"+url.PathEscape(domain)+"/"+url.PathEscape(service), nil, payload, nil)
}

// History is one entity's states between start and end, oldest first, in
// HA's minimal form (state and last_changed per point).
func (c *Client) History(ctx context.Context, entityID string, start, end time.Time) ([]State, error) {
	q := url.Values{"filter_entity_id": {entityID}, "end_time": {end.UTC().Format(time.RFC3339)},
		"minimal_response": {"true"}, "no_attributes": {"true"}}
	var out [][]State
	if err := c.do(ctx, "GET", "/api/history/period/"+start.UTC().Format(time.RFC3339), q, nil, &out); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return []State{}, nil
	}
	return out[0], nil
}
