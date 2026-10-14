// Package mcpserver is the same small MCP server laib's own tools speak:
// JSON-RPC over POST, the streamable-HTTP transport's request side, with
// initialize, tools/list and tools/call.
//
// Keep in sync with laib internal/mcpserver. It differs on purpose in three
// places: the bearer is not optional (no token refuses everyone, never
// open), a notification gets 202 without a body, as the transport says,
// and an argument whose schema says string but which is not one is a
// type error before the tool runs.
package mcpserver

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Tool is one callable.
type Tool struct {
	Name        string
	Description string
	Schema      map[string]any // JSON schema of the arguments
	Call        func(ctx context.Context, args map[string]any) (string, error)
}

// Server describes the MCP endpoint.
type Server struct {
	Name         string
	Version      string
	Instructions string
	Token        string // the one bearer that may call; empty refuses everyone
	Tools        []Tool
}

type request struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

func reply(w http.ResponseWriter, id json.RawMessage, result any, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	m := map[string]any{"jsonrpc": "2.0", "id": id}
	if msg != "" {
		m["error"] = map[string]any{"code": code, "message": msg}
	} else {
		m["result"] = result
	}
	json.NewEncoder(w).Encode(m)
}

func (s *Server) allowed(r *http.Request) bool {
	got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return ok && s.Token != "" && subtle.ConstantTimeCompare([]byte(got), []byte(s.Token)) == 1
}

// Handler serves the endpoint.
func (s *Server) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.allowed(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		var req request
		if err := json.Unmarshal(body, &req); err != nil {
			reply(w, nil, nil, -32700, "parse error")
			return
		}
		if strings.HasPrefix(req.Method, "notifications/") {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		switch req.Method {
		case "initialize":
			reply(w, req.ID, map[string]any{
				"protocolVersion": "2025-03-26",
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": s.Name, "version": s.Version},
				"instructions":    s.Instructions,
			}, 0, "")
		case "ping":
			reply(w, req.ID, map[string]any{}, 0, "")
		case "tools/list":
			tools := []map[string]any{}
			for _, t := range s.Tools {
				tools = append(tools, map[string]any{"name": t.Name, "description": t.Description, "inputSchema": t.Schema})
			}
			reply(w, req.ID, map[string]any{"tools": tools}, 0, "")
		case "tools/call":
			var p struct {
				Name string         `json:"name"`
				Args map[string]any `json:"arguments"`
			}
			json.Unmarshal(req.Params, &p)
			for _, t := range s.Tools {
				if t.Name != p.Name {
					continue
				}
				if p.Args == nil {
					p.Args = map[string]any{}
				}
				var out string
				err := checkTypes(t.Schema, p.Args)
				if err == nil {
					out, err = t.Call(r.Context(), p.Args)
				}
				isErr := err != nil
				if isErr {
					out = err.Error()
				}
				reply(w, req.ID, map[string]any{"content": []map[string]string{{"type": "text", "text": out}}, "isError": isErr}, 0, "")
				return
			}
			reply(w, req.ID, nil, -32602, "unknown tool: "+p.Name)
		default:
			reply(w, req.ID, nil, -32601, "method not found: "+req.Method)
		}
	}
}

// checkTypes refuses an argument the schema types as string that is not
// one (null counts as absent).
func checkTypes(schema map[string]any, args map[string]any) error {
	props, _ := schema["properties"].(map[string]any)
	for k, v := range args {
		p, _ := props[k].(map[string]any)
		if v == nil || p == nil || p["type"] != "string" {
			continue
		}
		switch v.(type) {
		case string:
		case float64:
			return fmt.Errorf("%s: erwarte Text, nicht eine Zahl", k)
		case bool:
			return fmt.Errorf("%s: erwarte Text, nicht ja/nein", k)
		case []any:
			return fmt.Errorf("%s: erwarte Text, nicht eine Liste", k)
		default:
			return fmt.Errorf("%s: erwarte Text", k)
		}
	}
	return nil
}

// Str reads a string argument.
func Str(args map[string]any, k string) string {
	v, _ := args[k].(string)
	return strings.TrimSpace(v)
}
