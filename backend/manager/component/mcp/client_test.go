package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/laelia/backend/manager/store"
)

func TestClientHTTPListToolsAndCallTool(t *testing.T) {
	var mu sync.Mutex
	var methods []string
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var req rpcRequest
		require.NoError(t, json.Unmarshal(body, &req))
		mu.Lock()
		methods = append(methods, req.Method)
		authHeader = r.Header.Get("Authorization")
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "sess-1")
			writeRPCResult(t, w, map[string]any{
				"protocolVersion": ProtocolVersion,
				"capabilities":    map[string]any{},
				"serverInfo":      map[string]any{"name": "test", "version": "1.0.0"},
			})
		case "tools/list":
			writeRPCResult(t, w, map[string]any{
				"tools": []map[string]any{
					{
						"name":        "do_it",
						"description": "does it",
						"inputSchema": map[string]any{"type": "object"},
					},
				},
			})
		case "tools/call":
			writeRPCResult(t, w, map[string]any{
				"content": []map[string]any{{"type": "text", "text": "ok"}},
				"isError": false,
			})
		default:
			// notifications/initialized: fire-and-forget, empty 200 body.
		}
	}))
	defer server.Close()

	client := New()
	serverMsg := &store.McpServerMessage{
		TransportType: "http",
		URL:           server.URL,
		Headers:       map[string]string{"Authorization": "Bearer secret"},
	}

	tools, err := client.ListTools(context.Background(), serverMsg)
	require.NoError(t, err)
	require.Len(t, tools, 1)
	assert.Equal(t, "do_it", tools[0].Name)
	assert.Equal(t, "does it", tools[0].Description)

	result, err := client.CallTool(context.Background(), serverMsg, "do_it", map[string]any{"a": 1})
	require.NoError(t, err)
	require.Len(t, result.Content, 1)
	assert.Equal(t, "text", result.Content[0].Type)
	assert.Equal(t, "ok", result.Content[0].Text)
	assert.False(t, result.IsError)

	mu.Lock()
	defer mu.Unlock()
	assert.Contains(t, methods, "initialize")
	assert.Contains(t, methods, "tools/list")
	assert.Contains(t, methods, "tools/call")
	assert.Equal(t, "Bearer secret", authHeader)
}

func TestClientSSEListTools(t *testing.T) {
	var mu sync.Mutex
	var methods []string
	var sessionID string
	mux := http.NewServeMux()
	mux.HandleFunc("/sse", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = fmt.Fprint(w, "event: endpoint\ndata: /messages?session_id=abc\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	})
	mux.HandleFunc("/messages", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var req rpcRequest
		require.NoError(t, json.Unmarshal(body, &req))
		mu.Lock()
		methods = append(methods, req.Method)
		sessionID = r.URL.Query().Get("session_id")
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "initialize":
			writeRPCResult(t, w, map[string]any{
				"protocolVersion": ProtocolVersion,
				"capabilities":    map[string]any{},
				"serverInfo":      map[string]any{"name": "test", "version": "1.0.0"},
			})
		case "tools/list":
			writeRPCResult(t, w, map[string]any{
				"tools": []map[string]any{
					{"name": "sse_tool", "description": "sse tool"},
				},
			})
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := New()
	serverMsg := &store.McpServerMessage{
		TransportType: "sse",
		URL:           server.URL + "/sse",
	}
	tools, err := client.ListTools(context.Background(), serverMsg)
	require.NoError(t, err)
	require.Len(t, tools, 1)
	assert.Equal(t, "sse_tool", tools[0].Name)

	mu.Lock()
	defer mu.Unlock()
	assert.Contains(t, methods, "initialize")
	assert.Contains(t, methods, "tools/list")
	assert.Equal(t, "abc", sessionID)
}

func writeRPCResult(t *testing.T, w http.ResponseWriter, result any) {
	t.Helper()
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"result":  result,
	})
}
