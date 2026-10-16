// Package usage counts what the family uses, for usage-metrics
// (mdopp/usage-metrics, a Laib built-in): counts per (app, event, UTC day)
// and nothing else — never who, no device, no address, no content (owner
// decision 2026-10-16, haus#50). The one text that leaves is a sentence the
// voice control did not understand, without who said it (Miss), which
// usage-metrics keeps 30 days.
//
// Counting is a map increment under a mutex; the network is only in Flush,
// which Run calls every few minutes. usage-metrics not answering drops that
// batch and says so once in the log: a page never waits for it.
package usage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// App is Haus's own name in usage-metrics.
const App = "haus"

const (
	maxMisses = 100 // sentences waiting for one flush; more are dropped
	maxText   = 280 // usage-metrics' cap for a sentence, in characters
)

// usage-metrics' shapes for app and event (its ingest.go)
var (
	appRE   = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	eventRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)
)

type key struct{ app, event, day string }
type miss struct{ source, text, day string }

// Counter batches counts and sends them to one usage-metrics.
type Counter struct {
	URL, Token string
	HTTP       *http.Client
	Now        func() time.Time
	Log        func(format string, args ...any)
	Started    time.Time // since when this Haus counts, for the card

	mu     sync.Mutex
	counts map[key]int64
	misses []miss
	down   bool // the last flush failed; logged once until one works
}

// New counts for the usage-metrics at url with its bearer token.
func New(url, token string) *Counter {
	return &Counter{URL: strings.TrimRight(url, "/"), Token: token, Started: time.Now()}
}

// FromEnv reads HAUS_USAGE_URL and HAUS_USAGE_TOKEN; without both, Haus
// counts nothing (nil).
func FromEnv(getenv func(string) string) *Counter {
	u, tok := strings.TrimRight(getenv("HAUS_USAGE_URL"), "/"), getenv("HAUS_USAGE_TOKEN")
	if u == "" && tok == "" {
		return nil
	}
	if p, err := url.Parse(u); err != nil || (p.Scheme != "http" && p.Scheme != "https") || p.Host == "" || tok == "" {
		log.Printf("haus: HAUS_USAGE_URL %q and HAUS_USAGE_TOKEN (set: %v) are both needed: nothing is counted", u, tok != "")
		return nil
	}
	return New(u, tok)
}

func (c *Counter) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Counter) logf(format string, args ...any) {
	if c.Log != nil {
		c.Log(format, args...)
		return
	}
	log.Printf(format, args...)
}

// Count counts one Haus event today.
func (c *Counter) Count(event string) { c.CountIn(App, event) }

// CountIn counts one event of app today (the voice is app stimme). A name
// that is not usage-metrics' shape is dropped, so no free text rides in it.
func (c *Counter) CountIn(app, event string) {
	if c == nil || !appRE.MatchString(app) || !eventRE.MatchString(event) {
		return
	}
	k := key{app, event, c.now().UTC().Format(time.DateOnly)}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.counts == nil {
		c.counts = map[key]int64{}
	}
	c.counts[k]++
}

// Miss keeps a sentence nobody could answer, for source (stimme), to be
// sent with the next flush: the words alone, on one line, at most 280
// characters.
func (c *Counter) Miss(source, text string) {
	if c == nil || !appRE.MatchString(source) {
		return
	}
	text = strings.Join(strings.FieldsFunc(text, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }), " ")
	text = strings.ToValidUTF8(text, "")
	if utf8.RuneCountInString(text) > maxText {
		text = string([]rune(text)[:maxText])
	}
	if text == "" {
		return
	}
	m := miss{source, text, c.now().UTC().Format(time.DateOnly)}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.misses) < maxMisses {
		c.misses = append(c.misses, m)
	}
}

// Flush sends what was counted since the last flush. usage-metrics not
// answering drops it: one log line until a flush works again.
func (c *Counter) Flush(ctx context.Context) {
	if c == nil {
		return
	}
	c.mu.Lock()
	counts, misses := c.counts, c.misses
	c.counts, c.misses = nil, nil
	c.mu.Unlock()
	if len(counts) == 0 && len(misses) == 0 {
		return
	}
	err := c.send(ctx, counts, misses)
	c.mu.Lock()
	defer c.mu.Unlock()
	if err == nil {
		c.down = false
		return
	}
	if !c.down {
		c.logf("haus: usage-metrics: %v — the counts are dropped until it answers", err)
	}
	c.down = true
}

func (c *Counter) send(ctx context.Context, counts map[key]int64, misses []miss) error {
	for k, n := range counts {
		if err := c.post(ctx, "/ingest", map[string]any{"app": k.app, "event": k.event, "day": k.day, "count": n}); err != nil {
			return err
		}
	}
	for _, m := range misses {
		if err := c.post(ctx, "/miss", map[string]any{"source": m.source, "text": m.text, "day": m.day}); err != nil {
			var full *statusError
			if errors.As(err, &full) && full.code == http.StatusTooManyRequests {
				continue // usage-metrics' daily cap for sentences: this one is not kept
			}
			return err
		}
	}
	return nil
}

type statusError struct {
	path string
	code int
}

func (e *statusError) Error() string { return e.path + " answered " + strconv.Itoa(e.code) }

func (c *Counter) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 10 * time.Second}
}

func (c *Counter) post(ctx context.Context, path string, body any) error {
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := c.client().Do(req)
	if err != nil {
		return errors.New(path + ": does not answer")
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return &statusError{path, resp.StatusCode}
	}
	return nil
}

// Run flushes every interval until ctx ends, then once more, briefly.
func (c *Counter) Run(ctx context.Context, every time.Duration) {
	if c == nil {
		return
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			last, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			c.Flush(last)
			cancel()
			return
		case <-t.C:
			fctx, cancel := context.WithTimeout(ctx, every/2)
			c.Flush(fctx)
			cancel()
		}
	}
}

// Summary is usage-metrics' GET /summary.
type Summary struct {
	Days      int      `json:"days"`
	From      string   `json:"from"`
	Total     int64    `json:"total"`
	KnownApps []string `json:"knownApps"`
	Apps      []struct {
		App    string `json:"app"`
		Total  int64  `json:"total"`
		Events []struct {
			Event string `json:"event"`
			Count int64  `json:"count"`
		} `json:"events"`
	} `json:"apps"`
}

// Count is one event's count in the window; 0 when it is not there.
func (s Summary) Count(app, event string) int64 {
	for _, a := range s.Apps {
		if a.App != app {
			continue
		}
		for _, e := range a.Events {
			if e.Event == event {
				return e.Count
			}
		}
	}
	return 0
}

// Events is app's events in the window, by name.
func (s Summary) Events(app string) map[string]int64 {
	out := map[string]int64{}
	for _, a := range s.Apps {
		if a.App == app {
			for _, e := range a.Events {
				out[e.Event] += e.Count
			}
		}
	}
	return out
}

// Known says whether app ever reported, whatever the window.
func (s Summary) Known(app string) bool {
	for _, a := range s.KnownApps {
		if a == app {
			return true
		}
	}
	return false
}

// MissList is usage-metrics' GET /misses.
type MissList struct {
	Days   int    `json:"days"`
	From   string `json:"from"`
	Total  int64  `json:"total"`
	Misses []struct {
		Source  string `json:"source"`
		Text    string `json:"text"`
		Count   int64  `json:"count"`
		LastDay string `json:"lastDay"`
	} `json:"misses"`
}

// Summary reads the counts of the last days.
func (c *Counter) Summary(ctx context.Context, days int) (Summary, error) {
	var s Summary
	err := c.read(ctx, "/summary", days, &s)
	return s, err
}

// Misses reads the sentences nobody could answer in the last days.
func (c *Counter) Misses(ctx context.Context, days int) (MissList, error) {
	var m MissList
	err := c.read(ctx, "/misses", days, &m)
	return m, err
}

func (c *Counter) read(ctx context.Context, path string, days int, out any) error {
	if c == nil {
		return errors.New("usage: not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL+path+"?days="+strconv.Itoa(days), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := c.client().Do(req)
	if err != nil {
		return fmt.Errorf("usage-metrics %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return &statusError{path, resp.StatusCode}
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
