// Package werkstatt reads the Werkstatt's open tickets for the Box page
// (haus#15): what pi works (pi:ready) and what waits for Claude
// (needs-claude), from the public GitHub API. Read-only, no token: the
// repos are public, and the list is cached so a reload costs nothing.
package werkstatt

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// Labels are the Werkstatt's two queues, in the order the card shows them.
var Labels = []string{"pi:ready", "needs-claude"}

// Ticket is one open issue in a queue.
type Ticket struct {
	Repo   string
	Number int
	Title  string
	URL    string
	Label  string
}

// Tickets lists the open tickets of Repos; API is GitHub's REST base.
type Tickets struct {
	API   string
	Repos []string
	HTTP  *http.Client

	mu    sync.Mutex
	at    time.Time
	cache []Ticket
}

// cacheFor keeps GitHub's unauthenticated limit (60 an hour) far away.
const cacheFor = 5 * time.Minute

// FromEnv reads HAUS_WERKSTATT_REPOS (owner/name, comma-separated); none, no card.
func FromEnv() *Tickets {
	var repos []string
	for _, r := range strings.Split(os.Getenv("HAUS_WERKSTATT_REPOS"), ",") {
		if r = strings.TrimSpace(r); r != "" {
			repos = append(repos, r)
		}
	}
	if len(repos) == 0 {
		return nil
	}
	api := strings.TrimRight(os.Getenv("HAUS_GITHUB_API"), "/")
	if api == "" {
		api = "https://api.github.com"
	}
	return &Tickets{API: api, Repos: repos}
}

// Open returns the open tickets of every repo and queue, cached.
func (t *Tickets) Open(ctx context.Context) ([]Ticket, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cache != nil && time.Since(t.at) < cacheFor {
		return t.cache, nil
	}
	h := t.HTTP
	if h == nil {
		h = &http.Client{Timeout: 5 * time.Second}
	}
	out := []Ticket{}
	for _, repo := range t.Repos {
		for _, label := range Labels {
			u := fmt.Sprintf("%s/repos/%s/issues?state=open&per_page=20&labels=%s", strings.TrimRight(t.API, "/"), repo, url.QueryEscape(label))
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
			if err != nil {
				return nil, err
			}
			req.Header.Set("Accept", "application/vnd.github+json")
			resp, err := h.Do(req)
			if err != nil {
				return nil, err
			}
			var issues []struct {
				Number  int    `json:"number"`
				Title   string `json:"title"`
				HTMLURL string `json:"html_url"`
			}
			err = json.NewDecoder(resp.Body).Decode(&issues)
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return nil, fmt.Errorf("github %s: %d", repo, resp.StatusCode)
			}
			if err != nil {
				return nil, err
			}
			for _, i := range issues {
				out = append(out, Ticket{Repo: repo, Number: i.Number, Title: i.Title, URL: i.HTMLURL, Label: label})
			}
		}
	}
	t.cache, t.at = out, time.Now()
	return out, nil
}
