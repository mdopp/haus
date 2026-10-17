// Package usagefake is a fake usage-metrics for tests (mdopp/usage-metrics:
// POST /ingest, POST /miss, GET /summary, GET /misses, the bearer token).
// It keeps every request body it got, so a test can say what never left
// Haus. Never a live box.
package usagefake

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
)

// Count is one (app, event, day) counter, summed over the ingests.
type Count struct{ App, Event, Day string }

// Miss is one unanswered sentence as it arrived.
type Miss struct{ Source, Text, Day string }

// Fake is the fake.
type Fake struct {
	*httptest.Server
	Token string

	mu      sync.Mutex
	counts  map[Count]int64
	misses  []Miss
	bodies  []string
	down    bool
	summary any
	missOut any
	asks    []string
}

// New starts a fake that accepts Bearer token.
func New(token string) *Fake {
	f := &Fake{Token: token, counts: map[Count]int64{}}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	return f
}

// Down makes every request answer 503.
func (f *Fake) Down(down bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.down = down
}

// Counts is every counter received so far.
func (f *Fake) Counts() map[Count]int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[Count]int64{}
	for k, v := range f.counts {
		out[k] = v
	}
	return out
}

// Misses is every sentence received so far, in order.
func (f *Fake) Misses() []Miss {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Miss(nil), f.misses...)
}

// Bodies is every request body received, raw.
func (f *Fake) Bodies() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.bodies...)
}

// Asked is every GET path with its query, in order.
func (f *Fake) Asked() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.asks...)
}

// SetSummary is what GET /summary answers (marshalled as given).
func (f *Fake) SetSummary(v any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.summary = v
}

// SetMisses is what GET /misses answers (marshalled as given).
func (f *Fake) SetMisses(v any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.missOut = v
}

func (f *Fake) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down {
		http.Error(w, "down", http.StatusServiceUnavailable)
		return
	}
	if r.Header.Get("Authorization") != "Bearer "+f.Token {
		http.Error(w, `{"error":"a valid bearer token is required"}`, http.StatusUnauthorized)
		return
	}
	body, _ := io.ReadAll(r.Body)
	if len(body) > 0 {
		f.bodies = append(f.bodies, string(body))
	}
	w.Header().Set("Content-Type", "application/json")
	switch r.Method + " " + r.URL.Path {
	case "POST /ingest":
		var in struct {
			App, Event, Day string
			Count           int64
		}
		dec := json.NewDecoder(bytes.NewReader(body))
		dec.DisallowUnknownFields()
		if dec.Decode(&in) != nil || in.App == "" || in.Event == "" || in.Day == "" || in.Count < 1 {
			http.Error(w, `{"error":"not an increment"}`, http.StatusBadRequest)
			return
		}
		k := Count{in.App, in.Event, in.Day}
		f.counts[k] += in.Count
		json.NewEncoder(w).Encode(map[string]any{"app": in.App, "event": in.Event, "day": in.Day, "total": f.counts[k]})
	case "POST /miss":
		var in struct{ Source, Text, Day string }
		dec := json.NewDecoder(bytes.NewReader(body))
		dec.DisallowUnknownFields()
		if dec.Decode(&in) != nil || in.Source == "" || in.Text == "" || in.Day == "" {
			http.Error(w, `{"error":"not a miss"}`, http.StatusBadRequest)
			return
		}
		f.misses = append(f.misses, Miss{in.Source, in.Text, in.Day})
		json.NewEncoder(w).Encode(map[string]any{"source": in.Source, "day": in.Day, "count": 1})
	case "GET /summary":
		f.asks = append(f.asks, r.URL.RequestURI())
		v := f.summary
		if v == nil {
			v = map[string]any{"days": 30, "from": "", "total": 0, "apps": []any{}, "knownApps": []any{}}
		}
		json.NewEncoder(w).Encode(v)
	case "GET /misses":
		f.asks = append(f.asks, r.URL.RequestURI())
		v := f.missOut
		if v == nil {
			v = map[string]any{"days": 30, "from": "", "total": 0, "misses": []any{}}
		}
		json.NewEncoder(w).Encode(v)
	default:
		http.Error(w, `{"error":"no such path"}`, http.StatusNotFound)
	}
}
