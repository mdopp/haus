// Package proxyfake is a fake of Laib's model proxy for tests (haus#13):
// GET /api/model-lease answers the lease as laib's contract has it (ADR
// 0011: state, model — the window —, alias, holder), and POST
// /v1/chat/completions answers an OpenAI chat completion whose content is
// Content. Anything else — taking or giving back the lease, loading a
// model — is recorded and answered 405, so a test can say it never
// happened.
package proxyfake

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"
)

// Lease is what GET /api/model-lease answers.
type Lease struct {
	State  string `json:"state"`  // none, preparing, ready
	Model  string `json:"model"`  // the window: shared, full, …; "" with none
	Alias  string `json:"alias"`  // the preset that answers
	Holder string `json:"holder"` // who holds it; "" with none
}

// Request is one request the fake saw.
type Request struct {
	Method, Path, Auth string
	Body               []byte
}

// Server is the fake.
type Server struct {
	*httptest.Server

	mu   sync.Mutex
	seen []Request

	// Lease is GET /api/model-lease's answer.
	Lease Lease
	// Content is the chat completion's message content, verbatim.
	Content string
	// Status, when set, answers every chat completion with it (409: the
	// model is not allowed in the window).
	Status int
	// Hang holds every chat completion that long.
	Hang time.Duration
}

// New starts a fake whose lease is the household model, loaded and ready
// in the shared window, and whose model keeps the first two hits.
func New(model string) *Server {
	s := &Server{Lease: Lease{State: "ready", Model: "shared", Alias: model, Holder: "chronicle"}, Content: `{"ids":[1,2]}`}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	return s
}

// Base is the OpenAI base, as HAUS_LLM_URL has it.
func (s *Server) Base() string { return s.URL + "/v1" }

// Seen returns the requests so far.
func (s *Server) Seen() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Request(nil), s.seen...)
}

// Chats returns the chat completion requests so far.
func (s *Server) Chats() []Request {
	var out []Request
	for _, r := range s.Seen() {
		if r.Method == http.MethodPost && r.Path == "/v1/chat/completions" {
			out = append(out, r)
		}
	}
	return out
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	s.seen = append(s.seen, Request{Method: r.Method, Path: r.URL.Path, Auth: r.Header.Get("Authorization"), Body: body})
	lease, content, status, hang := s.Lease, s.Content, s.Status, s.Hang
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/model-lease":
		json.NewEncoder(w).Encode(map[string]any{"state": lease.State, "model": lease.Model, "alias": lease.Alias, "holder": lease.Holder,
			"expires_at": nil, "last_renewed_at": nil, "renew_after": nil, "retry_after": 30})
	case r.Method == http.MethodPost && r.URL.Path == "/v1/chat/completions":
		if hang > 0 {
			select {
			case <-time.After(hang):
			case <-r.Context().Done():
				return
			}
		}
		if status != 0 {
			w.WriteHeader(status)
			json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "Modell im Modus nicht erlaubt.", "mode": lease.Model}})
			return
		}
		var req struct {
			Model string `json:"model"`
		}
		json.Unmarshal(body, &req)
		json.NewEncoder(w).Encode(map[string]any{"object": "chat.completion", "model": req.Model,
			"choices": []map[string]any{{"index": 0, "finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": content}}}})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}
