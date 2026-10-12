// Package laib is Haus's client for the box: Laib's MCP endpoint, one tool
// call per verb, JSON back. Two tokens: the ops hand reads and restarts,
// the human token approves and rejects (ADR 0012: Haus never decides, it
// delivers the admin's verdict).
package laib

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client talks to one Laib.
type Client struct {
	URL   string // http://host.containers.internal:5900
	Token string
	HTTP  *http.Client
}

// Readiness is `laib status --json`.ready.
type Readiness struct {
	State     string `json:"state"`
	Services  int    `json:"services"`
	Green     int    `json:"green"`
	Yellow    int    `json:"yellow"`
	Red       int    `json:"red"`
	LastHeard string `json:"lastHeard"`
	Lock      string `json:"lock,omitempty"`
	Drift     int    `json:"drift"`
	Operator  string `json:"operator"` // aus | startet | läuft | stumm | antwortet nicht | fehlt
	// the three guarantees (haus#2)
	LastBackup struct {
		At    string `json:"at"`
		Repos int    `json:"repos"`
		OK    bool   `json:"ok"`
	} `json:"lastBackup"`
	LastChange struct {
		Proposal   int    `json:"proposal"`
		Summary    string `json:"summary"`
		At         string `json:"at"`
		Reversible bool   `json:"reversible"`
	} `json:"lastChange"`
}

// Service is one row of `laib status --json`.services.
type Service struct {
	Name   string            `json:"name"`
	State  string            `json:"state"` // grün | gelb | rot
	Units  map[string]string `json:"units"`
	Detail string            `json:"detail,omitempty"`
}

// Proposal is one inbox entry.
type Proposal struct {
	ID      int       `json:"id"`
	Actor   string    `json:"actor"`
	Reason  string    `json:"reason"`
	Tier    string    `json:"tier"`
	Status  string    `json:"status"`
	Created time.Time `json:"created"`
	Files   []string  `json:"files"`
	Class   string    `json:"class"`
	Reasons []string  `json:"reasons,omitempty"` // why only a human may allow it (laib#34)
}

// Entry is one journal line.
type Entry struct {
	T       time.Time `json:"t"`
	Actor   string    `json:"actor"`
	Verb    string    `json:"verb"`
	Service string    `json:"service,omitempty"`
	Reason  string    `json:"reason,omitempty"`
	Result  string    `json:"result"`
	Detail  string    `json:"detail,omitempty"`
}

// Call runs one tool and returns its text; a tool error is an error.
func (c *Client) Call(ctx context.Context, verb, args string) (string, error) {
	if c.Token == "" {
		return "", errors.New("laib: no token")
	}
	h := c.HTTP
	if h == nil {
		h = &http.Client{Timeout: 30 * time.Second}
	}
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": "laib_" + verb, "arguments": map[string]string{"args": args}}})
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(c.URL, "/")+"/mcp", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := h.Do(req)
	if err != nil {
		return "", fmt.Errorf("laib: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("laib: %s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}
	var env struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return "", fmt.Errorf("laib: bad reply: %w", err)
	}
	if env.Error != nil {
		return "", errors.New("laib: " + env.Error.Message)
	}
	text := ""
	if len(env.Result.Content) > 0 {
		text = env.Result.Content[0].Text
	}
	if env.Result.IsError {
		return "", errors.New("laib " + verb + ": " + strings.TrimSpace(text))
	}
	return text, nil
}

func (c *Client) callJSON(ctx context.Context, verb, args string, v any) error {
	text, err := c.Call(ctx, verb, args)
	if err != nil {
		return err
	}
	// a verb may print a note line before the JSON; take the JSON from its first brace
	i := strings.IndexAny(text, "{[")
	if i < 0 {
		return fmt.Errorf("laib %s: no JSON in %q", verb, text)
	}
	return json.Unmarshal([]byte(text[i:]), v)
}

// Status is readiness plus the service rows.
func (c *Client) Status(ctx context.Context) (Readiness, []Service, error) {
	var out struct {
		Ready    Readiness `json:"ready"`
		Services []Service `json:"services"`
	}
	err := c.callJSON(ctx, "status", "", &out)
	return out.Ready, out.Services, err
}

// Inbox lists the open proposals.
func (c *Client) Inbox(ctx context.Context) ([]Proposal, error) {
	var out []Proposal
	err := c.callJSON(ctx, "inbox", "", &out)
	return out, err
}

// Journal lists entries since a duration like "24h".
func (c *Client) Journal(ctx context.Context, since string) ([]Entry, error) {
	var out []Entry
	err := c.callJSON(ctx, "journal", "--since "+since, &out)
	return out, err
}

// Approve, Reject and Restart pass the verdict or the action on.
func (c *Client) Approve(ctx context.Context, id int) (string, error) {
	return c.Call(ctx, "approve", fmt.Sprint(id))
}
func (c *Client) Reject(ctx context.Context, id int) (string, error) {
	return c.Call(ctx, "reject", fmt.Sprint(id))
}
func (c *Client) Restart(ctx context.Context, service string) (string, error) {
	return c.Call(ctx, "restart", service)
}
