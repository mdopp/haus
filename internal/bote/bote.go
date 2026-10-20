// Package bote relays a sentence from Haus's Sprechen field to the Bote
// (laib ADR 0016, ADR 0015's amendment): Hermes' OpenAI-compatible API
// server in the bote pod, one chat completion per sentence. Haus decides
// nothing (haus#1): it says who is writing and shows what comes back.
//
// Hermes' API server has no field for a person (it ignores OpenAI's
// `user`), so the person is named in a system line, which Hermes layers on
// top of the Bote's SOUL for that turn, and their memory scope is the
// X-Hermes-Session-Key header (gateway/platforms/api_server*.py).
package bote

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// Client is the Bote's API.
type Client struct {
	URL  string // the OpenAI base, http://host.containers.internal:8642/v1
	Key  string // Hermes' API_SERVER_KEY, as a bearer
	HTTP *http.Client
}

// FromEnv is the Bote from HAUS_BOTE_URL and HAUS_BOTE_KEY, or nil. Laib
// renders the address always and the key only while the Bote is installed
// (laib#215): no key, no Bote, quietly. A key without an http(s) address
// is no Bote either, said in the log; never a reason not to start.
func FromEnv() *Client {
	c := &Client{URL: strings.TrimRight(strings.TrimSpace(os.Getenv("HAUS_BOTE_URL")), "/"), Key: strings.TrimSpace(os.Getenv("HAUS_BOTE_KEY"))}
	if c.Key == "" {
		return nil
	}
	u, err := url.Parse(c.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		log.Printf("haus: HAUS_BOTE_KEY is set but HAUS_BOTE_URL is not an http(s)://…/v1 address: no Sprechen field")
		return nil
	}
	return c
}

// Person is who writes: Authelia's Remote-User and Remote-Name.
type Person struct {
	User, Name string
}

// ErrNoAnswer is a chat completion without text.
var ErrNoAnswer = errors.New("bote: the reply has no text")

// maxReply caps what is read of one reply.
const maxReply = 1 << 20

var noRedirects = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

// oneLine keeps a name on one line and short: it cannot add a line to what
// the Bote reads, nor a header.
func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		s = string(r[:n])
	}
	return s
}

// who is the system line that names the person: the login first, which
// Authelia vouches for; then, quoted, the display name, which a person may
// set themselves — without quotes or brackets of its own, so it cannot
// pass as another login.
func who(p Person) string {
	user := oneLine(p.User, 64)
	name := oneLine(strings.Map(func(r rune) rune {
		if strings.ContainsRune(`"'„“”‚‘’«»‹›()[]{}`, r) {
			return -1
		}
		return r
	}, p.Name), 80)
	line := "Diese Nachricht kommt aus dem Sprechen-Feld auf der Haus-Seite. Es schreibt die Anmeldung „" + user + "“"
	if name != "" && name != user {
		line += " (Name: „" + name + "“)"
	}
	return line + "."
}

// request is one chat completion for p's text; stream asks Hermes for the
// reply in pieces (OpenAI's server-sent events).
func (c *Client) request(ctx context.Context, p Person, text string, stream bool) (*http.Response, error) {
	msg := map[string]any{"messages": []map[string]string{
		{"role": "system", "content": who(p)}, {"role": "user", "content": text}}}
	if stream {
		msg["stream"] = true
	}
	body, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.Key)
	req.Header.Set("X-Hermes-Session-Key", "haus:"+oneLine(p.User, 64))
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	hc := c.HTTP
	if hc == nil {
		hc = noRedirects
	}
	return hc.Do(req)
}

// Ask sends text as p and returns the Bote's reply. Anything but a chat
// completion with text is an error; Hermes' words stay out of it.
func (c *Client) Ask(ctx context.Context, p Person, text string) (string, error) {
	resp, err := c.request(ctx, p, text, false)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	return completion(resp)
}

// completion reads one whole chat completion's text.
func completion(resp *http.Response) (string, error) {
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxReply+1))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("bote: HTTP %d", resp.StatusCode)
	}
	if len(raw) > maxReply {
		return "", fmt.Errorf("bote: the reply is over %d bytes", maxReply)
	}
	var ans struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &ans); err != nil || len(ans.Choices) == 0 {
		return "", fmt.Errorf("bote: not a chat completion")
	}
	reply := strings.TrimSpace(ans.Choices[0].Message.Content)
	if reply == "" {
		return "", ErrNoAnswer
	}
	return reply, nil
}

// Stream sends text as p and hands each piece of the reply to piece as it
// comes (stream: true, haus#63): a long answer shows while it is written,
// and a slow first word is told from a slow whole. It returns the whole
// reply, or what came before an error with that error. A Bote that
// answers in one piece anyway (no event stream) is one piece.
func (c *Client) Stream(ctx context.Context, p Person, text string, piece func(string)) (string, error) {
	resp, err := c.request(ctx, p, text, true)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if ct, _, _ := strings.Cut(resp.Header.Get("Content-Type"), ";"); resp.StatusCode != http.StatusOK || strings.TrimSpace(ct) != "text/event-stream" {
		reply, err := completion(resp)
		if err == nil && piece != nil {
			piece(reply)
		}
		return reply, err
	}
	var all strings.Builder
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64<<10), maxReply)
	for sc.Scan() {
		data, ok := strings.CutPrefix(sc.Text(), "data:")
		if !ok {
			continue // an event name, an id, a comment, the blank line between events
		}
		data = strings.TrimSpace(data)
		if data == "[DONE]" {
			break
		}
		var ev struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
			Error json.RawMessage `json:"error"`
		}
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			return strings.TrimSpace(all.String()), fmt.Errorf("bote: not a chat completion chunk")
		}
		if len(ev.Error) > 0 && string(ev.Error) != "null" {
			return strings.TrimSpace(all.String()), fmt.Errorf("bote: the run failed")
		}
		for _, ch := range ev.Choices {
			if ch.Delta.Content == "" {
				continue
			}
			if all.Len()+len(ch.Delta.Content) > maxReply {
				return strings.TrimSpace(all.String()), fmt.Errorf("bote: the reply is over %d bytes", maxReply)
			}
			if all.Len() == 0 { // the reply starts at its first word
				ch.Delta.Content = strings.TrimLeft(ch.Delta.Content, " \t\r\n")
				if ch.Delta.Content == "" {
					continue
				}
			}
			all.WriteString(ch.Delta.Content)
			if piece != nil {
				piece(ch.Delta.Content)
			}
		}
	}
	reply := strings.TrimSpace(all.String())
	if err := sc.Err(); err != nil {
		return reply, err
	}
	if reply == "" {
		return "", ErrNoAnswer
	}
	return reply, nil
}
