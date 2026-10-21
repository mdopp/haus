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
	Kind    string    `json:"kind,omitempty"`    // the edit's shape; template-update is an update waiting
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

// ErrNoTool is a tool this Laib does not have, or does not give the haus
// hand: an older Laib, or a policy.yml without the grant. A page says so
// plainly instead of failing.
var ErrNoTool = errors.New("laib: no such tool for haus")

// Call runs one tool and returns its text; a tool error is an error.
func (c *Client) Call(ctx context.Context, verb, args string) (string, error) {
	return c.call(ctx, verb, map[string]string{"args": args})
}

// CallTyped runs one tool with typed arguments (laib#290): each field
// its own JSON value, no command line to split.
func (c *Client) CallTyped(ctx context.Context, verb string, args map[string]any) (string, error) {
	if args == nil {
		args = map[string]any{}
	}
	return c.call(ctx, verb, args)
}

func (c *Client) call(ctx context.Context, verb string, arguments any) (string, error) {
	if c.Token == "" {
		return "", errors.New("laib: no token")
	}
	h := c.HTTP
	if h == nil {
		h = &http.Client{Timeout: 30 * time.Second}
	}
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": "laib_" + verb, "arguments": arguments}})
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
		if noTool(text) {
			return "", fmt.Errorf("%w: %s", ErrNoTool, strings.TrimSpace(text))
		}
		return "", errors.New("laib " + verb + ": " + strings.TrimSpace(text))
	}
	return text, nil
}

// noTool is Laib's answer for a tool it does not know ("unknown tool
// laib_x") or does not give this hand ("haus may not x").
func noTool(text string) bool {
	text = strings.TrimSpace(text)
	return strings.HasPrefix(text, "unknown tool ") || strings.Contains(text, " may not ")
}

func (c *Client) callJSON(ctx context.Context, verb, args string, v any) error {
	text, err := c.Call(ctx, verb, args)
	if err != nil {
		return err
	}
	return decodeJSON(verb, text, v)
}

func decodeJSON(verb, text string, v any) error {
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

// Invite is one person to invite (laib#360): an LLDAP account without a
// password, in its group; Authelia mails the set-password link, or Laib
// hands it back when the box has no mail.
type Invite struct {
	User   string
	First  string
	Last   string
	Email  string
	Group  string // family (Laib refuses admins from the haus hand, laib#362); empty on a resend
	Resend bool
}

// Invited is what Laib did with an invitation.
type Invited struct {
	User    string `json:"user"`
	Mailed  bool   `json:"mailed"`
	Link    string `json:"link,omitempty"` // only without mail: the admin passes it on
	Expires string `json:"expires"`
	Message string `json:"message"` // one German sentence for the admin
}

// Pending is an invited person who has not set a password yet.
type Pending struct {
	User      string `json:"user"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	InvitedAt string `json:"invited_at"`
}

// PersonInvite invites one person, or sends the link again.
func (c *Client) PersonInvite(ctx context.Context, in Invite) (Invited, error) {
	args := map[string]any{"user": in.User, "first": in.First, "last": in.Last, "email": in.Email}
	if in.Group != "" {
		args["group"] = in.Group
	}
	if in.Resend {
		args["resend"] = true
	}
	var out Invited
	text, err := c.CallTyped(ctx, "person_invite", args)
	if err != nil {
		return out, err
	}
	err = decodeJSON("person_invite", text, &out)
	return out, err
}

// PersonPending lists who is invited and has no password yet.
func (c *Client) PersonPending(ctx context.Context) ([]Pending, error) {
	var out []Pending
	text, err := c.CallTyped(ctx, "person_pending", nil)
	if err != nil {
		return nil, err
	}
	err = decodeJSON("person_pending", text, &out)
	return out, err
}

// Abo is one calendar laib's kalender mirrors into Radicale (laib#495):
// a Google calendar's secret iCal address (Kind google) or a public ICS
// (abo). Laib keeps the address; no answer carries it.
type Abo struct {
	Person string `json:"person"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Title  string `json:"title"`
	Since  string `json:"since"`
}

// Abos is laib_calendar's answer: one German sentence, and the list.
type Abos struct {
	Message string `json:"message"`
	Abos    []Abo  `json:"abos"`
}

func (c *Client) calendar(ctx context.Context, args map[string]any) (Abos, error) {
	var out Abos
	text, err := c.CallTyped(ctx, "calendar", args)
	if err != nil {
		return out, err
	}
	err = decodeJSON("calendar", text, &out)
	return out, err
}

// CalendarSubscribe hands an address to laib, once: as the tool's stdin,
// never as an argument, and never back (haus#66).
func (c *Client) CalendarSubscribe(ctx context.Context, a Abo, address string) (Abos, error) {
	return c.calendar(ctx, map[string]any{"action": "subscribe", "person": a.Person, "name": a.Name, "kind": a.Kind, "title": a.Title, "stdin": address + "\n"})
}

// CalendarUnsubscribe is Trennen: laib forgets the address, the next run
// removes the copy.
func (c *Client) CalendarUnsubscribe(ctx context.Context, person, name string) (Abos, error) {
	return c.calendar(ctx, map[string]any{"action": "unsubscribe", "person": person, "name": name})
}

// CalendarList is every subscription, without addresses.
func (c *Client) CalendarList(ctx context.Context) ([]Abo, error) {
	out, err := c.calendar(ctx, map[string]any{"action": "list"})
	return out.Abos, err
}
