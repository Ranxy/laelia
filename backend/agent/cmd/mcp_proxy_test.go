package cmd

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMcpProxyRoundTrip(t *testing.T) {
	socketDir := t.TempDir()
	socketPath := filepath.Join(socketDir, "daemon.sock")
	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	defer listener.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/mcp/tools", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Agent string `json:"agent"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		require.Equal(t, "agents/abc", body.Agent)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"tools": [{
				"mcpServerId": "mcpServers/srv-1",
				"serverName": "GitHub",
				"serverDescription": "GitHub tools",
				"toolName": "do_it",
				"runtimeName": "r123_do_it",
				"description": "does it",
				"inputSchema": {"type": "object"},
				"configVersion": 1,
				"assignmentVersion": 2
			}]
		}`))
	})
	mux.HandleFunc("/mcp/call", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, "agents/abc", body["agent"])
		require.Equal(t, "mcpServers/srv-1", body["mcp_server_id"])
		require.Equal(t, "do_it", body["tool_name"])
		require.EqualValues(t, 1, body["expected_config_version"])
		require.EqualValues(t, 2, body["expected_assignment_version"])
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"ok"}],"isError":false}`))
	})
	go func() { _ = http.Serve(listener, mux) }()

	t.Setenv("LAELIA_AGENT", "agents/abc")
	t.Setenv("LAELIA_DAEMON_SOCKET", socketPath)
	t.Setenv("LAELIA_SESSION_TOKEN", "session-token")

	stdinReader, stdinWriter := io.Pipe()
	var stdout bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- runMcpProxyIO(stdinReader, &stdout)
	}()

	_, err = stdinWriter.Write([]byte(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n" +
			`{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n" +
			`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"r123_do_it","arguments":{"x":1}}}` + "\n",
	))
	require.NoError(t, err)
	require.NoError(t, stdinWriter.Close())
	require.NoError(t, <-done)

	scanner := bufio.NewScanner(strings.NewReader(stdout.String()))
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	require.Len(t, lines, 3)

	var initResp map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &initResp))
	serverInfo, ok := initResp["result"].(map[string]any)["serverInfo"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "laelia-managed-mcp", serverInfo["name"])

	var listResp struct {
		Result struct {
			Tools []map[string]any `json:"tools"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal([]byte(lines[1]), &listResp))
	require.Len(t, listResp.Result.Tools, 1)
	require.Equal(t, "r123_do_it", listResp.Result.Tools[0]["name"])
	require.Equal(t, "GitHub - GitHub tools: does it", listResp.Result.Tools[0]["description"])

	var callResp struct {
		Result struct {
			Content []map[string]any `json:"content"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal([]byte(lines[2]), &callResp))
	require.Len(t, callResp.Result.Content, 1)
	require.Equal(t, "ok", callResp.Result.Content[0]["text"])

	_ = os.Remove(socketPath)
}
