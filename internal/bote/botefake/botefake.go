// Package botefake is a fake of the Bote's API for tests (haus#12): Hermes'
// OpenAI-compatible API server as the bote pod serves it on 8642. POST
// /v1/chat/completions with the right bearer answers a chat completion
// whose content is Reply; a wrong or missing bearer is Hermes' 401; a
// failed run is Hermes' 502 envelope. Every request is recorded, so a test
// can say who Haus said was speaking, and that nothing was sent.
package botefake

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"
)

// Request is one request the fake saw.
type Request struct {
	Method, Path, Auth string
	SessionKey         string // X-Hermes-Session-Key: whose memory scope
	Body               []byte
}

// Messages is the request's chat messages, role and content.
func (r Request) Messages() []map[string]string {
	var b struct {
		Messages []map[string]string `json:"messages"`
	}
	json.Unmarshal(r.Body, &b)
	return b.Messages
}

// Server is the fake.
type Server struct {
	*httptest.Server
	Key string // API_SERVER_KEY

	mu   sync.Mutex
	seen []Request

	// Reply is the assistant's content, verbatim.
	Reply string
	// Status, when set, answers every chat completion with it and Hermes'
	// error envelope (502: the run produced no answer).
	Status int
	// Hang holds every chat completion that long.
	Hang time.Duration
}

// New starts a fake Bote whose API key is key.
func New(key string) *Server {
	s := &Server{Key: key, Reply: "Erledigt: das Licht im Flur ist an."}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	return s
}

// Base is the OpenAI base, as HAUS_BOTE_URL has it.
func (s *Server) Base() string { return s.URL + "/v1" }

// Seen returns the requests so far.
func (s *Server) Seen() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Request(nil), s.seen...)
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	s.seen = append(s.seen, Request{Method: r.Method, Path: r.URL.Path, Auth: r.Header.Get("Authorization"),
		SessionKey: r.Header.Get("X-Hermes-Session-Key"), Body: body})
	reply, status, hang := s.Reply, s.Status, s.Hang
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if r.Header.Get("Authorization") != "Bearer "+s.Key {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "Invalid API key", "type": "invalid_request_error", "code": "invalid_api_key"}})
		return
	}
	if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if hang > 0 {
		select {
		case <-time.After(hang):
		case <-r.Context().Done():
			return
		}
	}
	if status != 0 {
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "Agent run did not produce a response.",
			"type": "server_error", "code": "agent_incomplete"}})
		return
	}
	w.Header().Set("X-Hermes-Session-Id", "sess-1")
	json.NewEncoder(w).Encode(map[string]any{"id": "chatcmpl-1", "object": "chat.completion", "created": 1, "model": "hermes-agent",
		"choices": []map[string]any{{"index": 0, "finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": reply}}},
		"usage":   map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2}})
}
