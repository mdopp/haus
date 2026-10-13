package napi

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"
)

// The Solaris bridge (laib ADR 0015, ADR 0021 rule 8). Solaris is retired;
// until Haus owns everything it did for the phone, two things go through it:
// phones paired with Solaris keep working (their tokens are checked there),
// and /napi paths Haus does not serve yet are passed to it. It ends when
// every phone is paired through Haus and Haus serves every /napi path the app
// uses (haus#5). SOLARIS_URL unset means no bridge.
func BridgeFromEnv() (*url.URL, Validator, error) {
	raw := os.Getenv("SOLARIS_URL")
	if raw == "" {
		return nil, nil, nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("SOLARIS_URL: %w", err)
	}
	return u, SolarisValidator(u, nil), nil
}

// SolarisValidator asks Solaris whether the bearer is one of its paired
// devices (GET /napi/whoami), cached briefly.
func SolarisValidator(upstream *url.URL, h *http.Client) Validator {
	type hit struct {
		ok  bool
		exp time.Time
	}
	var mu sync.Mutex
	cache := map[string]hit{}
	if h == nil {
		h = &http.Client{Timeout: 10 * time.Second}
	}
	return func(ctx context.Context, bearer string) bool {
		mu.Lock()
		if c, ok := cache[bearer]; ok && time.Now().Before(c.exp) {
			mu.Unlock()
			return c.ok
		}
		mu.Unlock()
		req, _ := http.NewRequestWithContext(ctx, "GET", upstream.ResolveReference(&url.URL{Path: "/napi/whoami"}).String(), nil)
		req.Header.Set("Authorization", "Bearer "+bearer)
		resp, err := h.Do(req)
		ok := err == nil && resp.StatusCode == 200
		if resp != nil {
			resp.Body.Close()
		}
		mu.Lock()
		cache[bearer] = hit{ok: ok, exp: time.Now().Add(5 * time.Minute)}
		mu.Unlock()
		return ok
	}
}
