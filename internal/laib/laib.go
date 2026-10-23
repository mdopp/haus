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

// Zuhause is box.yml's zuhause (laib#561): the household's address, all
// of it optional; Laib never geocodes, the coordinates come from a person.
type Zuhause struct {
	Street    string   `json:"street,omitempty"`
	Postcode  string   `json:"postcode,omitempty"`
	City      string   `json:"city,omitempty"`
	Country   string   `json:"country,omitempty"`
	Latitude  *float64 `json:"latitude,omitempty"`
	Longitude *float64 `json:"longitude,omitempty"`
	Elevation *int     `json:"elevation,omitempty"`
}

// Proposed is laib_zuhause set's answer: the proposal a human approves.
type Proposed struct {
	Proposal int    `json:"proposal"`
	Message  string `json:"message"`
}

// ZuhauseShow is the address as Laib has it; empty when none is set.
func (c *Client) ZuhauseShow(ctx context.Context) (Zuhause, error) {
	var out Zuhause
	text, err := c.CallTyped(ctx, "zuhause", map[string]any{"action": "show"})
	if err != nil {
		return out, err
	}
	err = decodeJSON("zuhause", text, &out)
	return out, err
}

// ZuhauseSet proposes the whole block: fields holds street, postcode,
// city, country, latitude, longitude, elevation as strings, and one left
// out is removed. Laib files a class-human proposal; nothing changes
// until a human approves it on Laib's page.
func (c *Client) ZuhauseSet(ctx context.Context, fields map[string]string) (Proposed, error) {
	args := map[string]any{"action": "set"}
	for k, v := range fields {
		if v != "" {
			args[k] = v
		}
	}
	var out Proposed
	text, err := c.CallTyped(ctx, "zuhause", args)
	if err != nil {
		return out, err
	}
	err = decodeJSON("zuhause", text, &out)
	return out, err
}

// ErrNoScan is a box without the scan service (laib#568): Laib knows the
// verb, but nothing provides scan, or it has written no state yet.
var ErrNoScan = errors.New("laib: no scan service")

// Scan is the scan service's status file as `laib scan status` prints it
// (laib#568): its state in a word and, for each state, what goes with it.
type Scan struct {
	At      string  `json:"at"`
	Detail  string  `json:"detail"`  // the service's own sentence
	Zustand string  `json:"zustand"` // bereit | scannt | wartet-auf-rest | trennt | legt-ab | fertig | fehler
	Seite   int     `json:"seite"`   // scannt: the page being scanned
	Blatt   int     `json:"blatt"`   // wartet-auf-rest: the sheet to put the rest in from
	Scanner bool    `json:"scanner"` // a SANE scanner: „Stapel scannen“ is there
	Eingang string  `json:"eingang"` // the inbox folder on the share, scan-eingang
	Stapel  string  `json:"stapel"`
	Letzte  []Stack `json:"letzte"` // the last stacks, newest first
	// the scanner's setup (laib#571), kept until the next search or check
	Suche    ScanSuche    `json:"suche"`
	Pruefung ScanPruefung `json:"pruefung"`
}

// ScanSuche is the last search for scanners on the network.
type ScanSuche struct {
	Zustand      string `json:"zustand"` // läuft | fertig | fehler; "": none yet
	At           string `json:"at"`
	Detail       string `json:"detail"`       // a sentence for the family (laib#589)
	Einzelheiten string `json:"einzelheiten"` // the raw error behind it, for an admin
	Gefragt      int    `json:"gefragt"`      // how many neighbours on the LAN were asked directly
	Funde        []struct {
		Geraet string `json:"geraet"` // the SANE device, for einrichten as it is
		Name   string `json:"name"`
	} `json:"funde"`
}

// ScanPruefung is the last check of the scanner: opened without paper, or
// one probe sheet read and never filed.
type ScanPruefung struct {
	Geraet       string `json:"geraet"`
	OK           bool   `json:"ok"`
	At           string `json:"at"`           // "": none yet
	Detail       string `json:"detail"`       // a sentence for the family (laib#589)
	Einzelheiten string `json:"einzelheiten"` // SANE's, the socket's or a tool's own words
	Probe        bool   `json:"probe"`
}

// Stack is one finished stack.
type Stack struct {
	Stapel  string `json:"stapel"`
	Ende    string `json:"ende"`   // fertig | abgebrochen | fehler
	Quelle  string `json:"quelle"` // scanner | ordner
	Datei   string `json:"datei"`
	At      string `json:"at"`
	Seiten  int    `json:"seiten"`
	Leer    int    `json:"leer"`
	Meldung string `json:"meldung"` // the closing message as the person got it
	// Einzelheiten is the raw error of a stack gone wrong (laib#589)
	Einzelheiten string `json:"einzelheiten"`
	Dokumente    []struct {
		Titel string `json:"titel"`
		Datei string `json:"datei"` // its original file name in Paperless
	} `json:"dokumente"`
}

// ScanStatus reads what the scanner does now.
func (c *Client) ScanStatus(ctx context.Context) (Scan, error) {
	var out Scan
	text, err := c.CallTyped(ctx, "scan", map[string]any{"action": "status"})
	if err != nil {
		return out, scanErr(err)
	}
	err = decodeJSON("scan", text, &out)
	return out, err
}

// ScanOrder passes one order on (start, weiter, abschliessen, verwerfen);
// person is whom the closing message is for, "" for the box's default.
// Laib only writes the order; the service decides whether it fits.
func (c *Client) ScanOrder(ctx context.Context, action, person string) (string, error) {
	args := map[string]any{"action": action}
	if person != "" {
		args["person"] = person
	}
	text, err := c.CallTyped(ctx, "scan", args)
	if err != nil {
		return "", scanErr(err)
	}
	var out struct {
		Message string `json:"message"`
	}
	if decodeJSON("scan", text, &out) == nil {
		text = out.Message
	}
	return strings.TrimSpace(text), nil
}

// scanErr is Laib's refusal for a box without the service ("Der Dienst
// scan ist nicht eingerichtet.", "… hat noch keinen Zustand geschrieben.")
// as ErrNoScan.
func scanErr(err error) error {
	if msg := err.Error(); !errors.Is(err, ErrNoTool) && (strings.Contains(msg, "nicht eingerichtet") || strings.Contains(msg, "noch keinen Zustand")) {
		return fmt.Errorf("%w: %s", ErrNoScan, msg)
	}
	return err
}

// ScanEinrichten proposes the scanner (laib#571): without the service, the
// scan template with geraet (the owner approves); with it, its SCANNER.
// geraet may be "" only without the service. Laib's dial decides.
func (c *Client) ScanEinrichten(ctx context.Context, geraet string) (Proposed, error) {
	args := map[string]any{"action": "einrichten"}
	if geraet != "" {
		args["geraet"] = geraet
	}
	var out Proposed
	text, err := c.CallTyped(ctx, "scan", args)
	if err != nil {
		return out, err
	}
	err = decodeJSON("scan", text, &out)
	return out, err
}
