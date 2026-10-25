// Package judge asks the house model which of a search's hits answer the
// question (haus#13): the memory library's tidy-up, optional and never
// blocking. The model is Laib's household model behind its model proxy
// (laib ADR 0011), asked only while it is the one in place — ready in the
// window, or the default with no window held: Haus reads the lease, never
// takes it, never asks for another
// model, so the tidy-up never makes the card swap. One call, temperature
// 0, the answer held to a JSON schema; anything else is an error and the
// caller keeps what it had.
package judge

import (
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

// Client is the house model behind Laib's model proxy.
type Client struct {
	URL   string // the proxy's OpenAI base, http://…/v1 (MODEL_URL's shape)
	Model string // the house model's alias, gemma-4-e4b
	Key   string // the model key, as a bearer
	HTTP  *http.Client
}

// FromEnv is the house model from HAUS_LLM_URL, HAUS_LLM_MODEL and
// HAUS_LLM_KEY, or nil: a box without the model role sets none of them,
// and then there is no tidy-up. One missing, or an address that is not
// http(s), is no tidy-up either, said in the log; never a reason not to
// start.
func FromEnv() *Client {
	c := &Client{URL: strings.TrimRight(strings.TrimSpace(os.Getenv("HAUS_LLM_URL")), "/"),
		Model: strings.TrimSpace(os.Getenv("HAUS_LLM_MODEL")), Key: strings.TrimSpace(os.Getenv("HAUS_LLM_KEY"))}
	if c.URL == "" && c.Model == "" && c.Key == "" {
		return nil
	}
	u, err := url.Parse(c.URL)
	if c.URL == "" || c.Model == "" || c.Key == "" || err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		log.Printf("haus: HAUS_LLM_URL, HAUS_LLM_MODEL and HAUS_LLM_KEY go together, the address as http(s)://…/v1: no tidy-up of the notes' hits")
		return nil
	}
	return c
}

// Item is one hit as the model reads it: the note's title and a passage.
type Item struct {
	Title, Text string
}

// ErrNotLoaded is the lease saying the house model is not the one loaded
// and ready: asking now could make the card load it.
var ErrNotLoaded = errors.New("judge: the house model is not the one loaded")

// maxAnswer caps what is read of one answer.
const maxAnswer = 1 << 20

var noRedirects = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

func (c *Client) do(ctx context.Context, method, u, key string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	hc := c.HTTP
	if hc == nil {
		hc = noRedirects
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer+1))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("judge: %s %s: HTTP %d", method, u, resp.StatusCode)
	}
	if len(out) > maxAnswer {
		return nil, fmt.Errorf("judge: %s: the answer is over %d bytes", u, maxAnswer)
	}
	return out, nil
}

// loaded reads the lease (GET, no key: it is public): the house model is
// the one answering, ready in a window or the default with none held.
func (c *Client) loaded(ctx context.Context) error {
	raw, err := c.do(ctx, http.MethodGet, strings.TrimSuffix(c.URL, "/v1")+"/api/model-lease", "", nil)
	if err != nil {
		return err
	}
	var l struct {
		State string `json:"state"`
		Alias string `json:"alias"`
	}
	if err := json.Unmarshal(raw, &l); err != nil {
		return fmt.Errorf("judge: the lease: %w", err)
	}
	// ready with the house model; or no window held and the house model the
	// default: nobody holds the card, so asking takes it from no one, and a
	// model not loaded yet only runs out the caller's budget
	if (l.State != "ready" && l.State != "none") || l.Alias != c.Model {
		return fmt.Errorf("%w (lease %q, %q)", ErrNotLoaded, l.State, l.Alias)
	}
	return nil
}

// system is the instruction, as the eval asked it (haus#13, judge.py).
const system = "Du prüfst Suchtreffer aus den Notizen einer Familie. Nenne nur die Notizen, die die Frage tatsächlich " +
	"beantworten oder die gesuchte Information enthalten. Notizen, die nur ein ähnliches Thema haben, gehören nicht dazu. " +
	"Passt keine, gib eine leere Liste zurück. Antworte nur mit JSON."

func prompt(question string, items []Item) string {
	var b strings.Builder
	b.WriteString("Frage: " + question + "\n\nNotizen:\n")
	for i, it := range items {
		fmt.Fprintf(&b, "[%d] %s — %s\n", i+1, it.Title, it.Text)
	}
	b.WriteString("\nWelche Notizen beantworten die Frage? Antworte als JSON: {\"ids\": [Nummern]}")
	return b.String()
}

// Pick asks which of items answer question, and returns their indexes,
// in items' order. It asks only when the lease says the house model is
// ready, or nobody holds the card and it is the default (else
// ErrNotLoaded), and only that model. A refusal
// (409: not allowed in the window), another status, an answer that is not
// {"ids":[…]} or names a number that is not an item, is an error. An
// empty list is the model's answer, not an error: what to do with it is
// the caller's.
func (c *Client) Pick(ctx context.Context, question string, items []Item) ([]int, error) {
	if err := c.loaded(ctx); err != nil {
		return nil, err
	}
	schema := map[string]any{"type": "object", "additionalProperties": false, "required": []string{"ids"},
		"properties": map[string]any{"ids": map[string]any{"type": "array", "items": map[string]any{"type": "integer", "minimum": 1, "maximum": len(items)}}}}
	body, err := json.Marshal(map[string]any{
		"model": c.Model, "temperature": 0, "max_tokens": 64,
		"messages":             []map[string]string{{"role": "system", "content": system}, {"role": "user", "content": prompt(question, items)}},
		"response_format":      map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": "auswahl", "schema": schema}},
		"chat_template_kwargs": map[string]any{"enable_thinking": false},
	})
	if err != nil {
		return nil, err
	}
	raw, err := c.do(ctx, http.MethodPost, c.URL+"/chat/completions", c.Key, body)
	if err != nil {
		return nil, err
	}
	var ans struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &ans); err != nil || len(ans.Choices) == 0 {
		return nil, fmt.Errorf("judge: not a chat completion: %v", err)
	}
	var picked struct {
		IDs *[]int `json:"ids"`
	}
	if err := json.Unmarshal([]byte(ans.Choices[0].Message.Content), &picked); err != nil || picked.IDs == nil {
		return nil, fmt.Errorf("judge: the answer is not {\"ids\":[…]}: %q", cut(ans.Choices[0].Message.Content))
	}
	keep := make([]bool, len(items))
	for _, id := range *picked.IDs {
		if id < 1 || id > len(items) {
			return nil, fmt.Errorf("judge: the answer names %d of %d", id, len(items))
		}
		keep[id-1] = true
	}
	out := []int{}
	for i, k := range keep {
		if k {
			out = append(out, i)
		}
	}
	return out, nil
}

func cut(s string) string {
	if r := []rune(s); len(r) > 80 {
		return string(r[:80]) + "…"
	}
	return s
}

// Gist is what the house model says of a page someone wants to keep
// (haus#117): what kind it is, its core in a few sentences, and tags.
type Gist struct {
	Art  string   `json:"art"`  // notiz, rezept, album, reise
	Kern string   `json:"kern"` // three or four German sentences
	Tags []string `json:"tags"` // up to four
}

const gistSystem = "Du fasst eine Webseite für die Notizen eines Haushalts zusammen. Antworte nur mit JSON. " +
	"art: rezept (ein Kochrezept), album (eine Rezension oder Vorstellung eines Musikalbums), reise (ein Reiseziel, eine Unterkunft, ein Reiseangebot) oder notiz (alles andere). " +
	"kern: der Kern der Seite in drei bis vier deutschen Sätzen, sachlich, ohne Werbung; bei einer Rezension das Urteil und warum. " +
	"tags: höchstens vier kurze deutsche Stichwörter in Kleinbuchstaben."

// Summarize asks the house model for a page's gist: its title and text,
// cut to what the model takes. Only while the house model is the one in
// place (ErrNotLoaded otherwise); an answer that is not the schema's is
// an error and the caller keeps the page's own words.
func (c *Client) Summarize(ctx context.Context, title, text string) (Gist, error) {
	if err := c.loaded(ctx); err != nil {
		return Gist{}, err
	}
	if r := []rune(text); len(r) > 6000 {
		text = string(r[:6000])
	}
	schema := map[string]any{"type": "object", "additionalProperties": false, "required": []string{"art", "kern", "tags"},
		"properties": map[string]any{
			"art":  map[string]any{"type": "string", "enum": []string{"notiz", "rezept", "album", "reise"}},
			"kern": map[string]any{"type": "string", "maxLength": 900},
			"tags": map[string]any{"type": "array", "maxItems": 4, "items": map[string]any{"type": "string", "maxLength": 30}}}}
	body, err := json.Marshal(map[string]any{
		"model": c.Model, "temperature": 0, "max_tokens": 400,
		"messages":             []map[string]string{{"role": "system", "content": gistSystem}, {"role": "user", "content": "Titel: " + title + "\n\n" + text}},
		"response_format":      map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": "zusammenfassung", "schema": schema}},
		"chat_template_kwargs": map[string]any{"enable_thinking": false},
	})
	if err != nil {
		return Gist{}, err
	}
	raw, err := c.do(ctx, http.MethodPost, c.URL+"/chat/completions", c.Key, body)
	if err != nil {
		return Gist{}, err
	}
	var ans struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &ans); err != nil || len(ans.Choices) == 0 {
		return Gist{}, fmt.Errorf("judge: not a chat completion: %v", err)
	}
	var g Gist
	if err := json.Unmarshal([]byte(ans.Choices[0].Message.Content), &g); err != nil || strings.TrimSpace(g.Kern) == "" {
		return Gist{}, fmt.Errorf("judge: the answer is no gist: %q", cut(ans.Choices[0].Message.Content))
	}
	return g, nil
}
