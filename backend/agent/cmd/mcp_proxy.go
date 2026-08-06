package cmd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"strings"

	pkgerrors "github.com/pkg/errors"
	"github.com/spf13/cobra"
)

const (
	mcpProtocolVersion = "2025-06-18"
	maxMcpProxyLine    = 1 << 20
)

func init() {
	rootCmd.AddCommand(mcpProxyCmd)
}

// mcp-proxy is an MCP stdio server exposing the managed MCP tools assigned to
// the calling agent. It is spawned by the ACP runtime as an MCP server and
// forwards tools/list / tools/call to the local daemon, which holds the live
// machine access token. Identity comes from the daemon-injected env vars.
var mcpProxyCmd = &cobra.Command{
	Use:   "mcp-proxy",
	Short: "MCP stdio proxy for server-managed MCP tools (spawned by the ACP runtime)",
	Args:  cobra.NoArgs,
	RunE: func(_ *cobra.Command, _ []string) error {
		return runMcpProxy()
	},
}

type mcpRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type mcpRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *mcpRPCError    `json:"error,omitempty"`
}

type mcpCatalogTool struct {
	McpServerID       string          `json:"mcpServerId"`
	ServerName        string          `json:"serverName"`
	ServerDescription string          `json:"serverDescription,omitempty"`
	ToolName          string          `json:"toolName"`
	RuntimeName       string          `json:"runtimeName"`
	Title             string          `json:"title,omitempty"`
	Description       string          `json:"description,omitempty"`
	InputSchema       json.RawMessage `json:"inputSchema,omitempty"`
	ConfigVersion     int64           `json:"configVersion"`
	AssignmentVersion int64           `json:"assignmentVersion"`
}

type mcpContentBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

type mcpCallResult struct {
	Content           []mcpContentBlock `json:"content"`
	IsError           bool              `json:"isError"`
	StructuredContent json.RawMessage   `json:"structuredContent,omitempty"`
}

func runMcpProxy() error {
	return runMcpProxyIO(os.Stdin, os.Stdout)
}

func runMcpProxyIO(stdin io.Reader, stdout io.Writer) error {
	agent := os.Getenv("LAELIA_AGENT")
	socket := os.Getenv("LAELIA_DAEMON_SOCKET")
	token := os.Getenv("LAELIA_SESSION_TOKEN")
	if agent == "" || socket == "" || token == "" {
		return errors.New("mcp-proxy: LAELIA_AGENT / LAELIA_DAEMON_SOCKET / LAELIA_SESSION_TOKEN are required")
	}

	reader := bufio.NewReaderSize(stdin, 64*1024)
	writer := bufio.NewWriter(stdout)
	encoder := json.NewEncoder(writer)
	ctx := context.Background()

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(line) > maxMcpProxyLine {
			_ = encoder.Encode(mcpRPCResponse{JSONRPC: "2.0", Error: &mcpRPCError{Code: -32600, Message: "request too large"}})
			_ = writer.Flush()
			continue
		}
		var req mcpRPCRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			_ = encoder.Encode(mcpRPCResponse{JSONRPC: "2.0", Error: &mcpRPCError{Code: -32700, Message: "parse error"}})
			_ = writer.Flush()
			continue
		}
		result, rpcErr := handleMcpProxyRPC(ctx, agent, socket, token, req)
		if rpcErr != nil {
			_ = encoder.Encode(mcpRPCResponse{JSONRPC: "2.0", ID: req.ID, Error: rpcErr})
		} else if req.Method != "notifications/initialized" {
			_ = encoder.Encode(mcpRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: result})
		}
		_ = writer.Flush()
	}
}

func handleMcpProxyRPC(ctx context.Context, agent, socket, token string, req mcpRPCRequest) (any, *mcpRPCError) {
	switch req.Method {
	case "initialize":
		return map[string]any{
			"protocolVersion": mcpProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo": map[string]any{
				"name":    "laelia-managed-mcp",
				"version": "1.0.0",
			},
		}, nil
	case "notifications/initialized":
		return nil, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		catalog, err := daemonMcpTools(ctx, agent, socket, token)
		if err != nil {
			return nil, &mcpRPCError{Code: -32603, Message: "failed to list managed MCP tools: " + err.Error()}
		}
		tools := make([]map[string]any, 0, len(catalog))
		for _, tool := range catalog {
			serverLabel := tool.ServerName
			if tool.ServerDescription != "" {
				serverLabel += " - " + tool.ServerDescription
			}
			entry := map[string]any{
				"name":        tool.RuntimeName,
				"title":       tool.Title,
				"description": serverLabel + ": " + tool.Description,
			}
			if len(tool.InputSchema) > 0 {
				entry["inputSchema"] = json.RawMessage(tool.InputSchema)
			}
			tools = append(tools, entry)
		}
		return map[string]any{"tools": tools}, nil
	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments,omitempty"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, &mcpRPCError{Code: -32602, Message: "invalid params"}
		}
		catalog, err := daemonMcpTools(ctx, agent, socket, token)
		if err != nil {
			return nil, &mcpRPCError{Code: -32603, Message: "failed to list managed MCP tools: " + err.Error()}
		}
		var tool *mcpCatalogTool
		for i := range catalog {
			if catalog[i].RuntimeName == params.Name {
				tool = &catalog[i]
				break
			}
		}
		if tool == nil {
			return nil, &mcpRPCError{Code: -32602, Message: "tool is not currently assigned to this agent"}
		}
		args := json.RawMessage("{}")
		if len(params.Arguments) > 0 {
			args = params.Arguments
		}
		result, err := daemonMcpCall(ctx, agent, socket, token, tool, args)
		if err != nil {
			return nil, &mcpRPCError{Code: -32603, Message: "managed MCP tool call failed: " + err.Error()}
		}
		return result, nil
	default:
		return nil, &mcpRPCError{Code: -32601, Message: "method not found"}
	}
}

func daemonMcpTools(ctx context.Context, agent, socket, token string) ([]mcpCatalogTool, error) {
	var resp struct {
		Tools []mcpCatalogTool `json:"tools"`
	}
	if err := daemonHTTP(ctx, socket, token, http.MethodGet, "/mcp/tools", map[string]any{"agent": agent}, &resp); err != nil {
		return nil, err
	}
	return resp.Tools, nil
}

func daemonMcpCall(ctx context.Context, agent, socket, token string, tool *mcpCatalogTool, args json.RawMessage) (*mcpCallResult, error) {
	body := map[string]any{
		"agent":                       agent,
		"mcp_server_id":               tool.McpServerID,
		"tool_name":                   tool.ToolName,
		"arguments":                   json.RawMessage(args),
		"expected_config_version":     tool.ConfigVersion,
		"expected_assignment_version": tool.AssignmentVersion,
	}
	var resp mcpCallResult
	if err := daemonHTTP(ctx, socket, token, http.MethodPost, "/mcp/call", body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// daemonHTTP calls the local daemon over its unix socket. The daemon returns
// chattools.Response envelopes on failure (HTTP 200), so failures are decoded
// before the target payload.
func daemonHTTP(ctx context.Context, socket, token, method, path string, body any, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	//nolint:revive // fake URL; the custom transport dials the unix socket
	req, err := http.NewRequestWithContext(ctx, method, "http://unix"+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", socket)
			},
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	rawResp, err := io.ReadAll(io.LimitReader(resp.Body, maxMcpProxyLine))
	if err != nil {
		return err
	}
	var envelope struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rawResp, &envelope); err == nil && envelope.Code != "" {
		return pkgerrors.Errorf("%s: %s", envelope.Code, envelope.Message)
	}
	if len(rawResp) == 0 {
		return nil
	}
	return json.Unmarshal(rawResp, out)
}
