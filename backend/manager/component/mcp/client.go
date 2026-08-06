// Package mcp implements the manager-side MCP gateway client: it connects to
// admin-configured MCP servers over streamable HTTP or SSE, lists their tools,
// and invokes allowlisted tools on behalf of agents. Transport configuration
// and header values never leave the manager.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	pkgerrors "github.com/pkg/errors"

	"github.com/Ranxy/laelia/backend/manager/store"
)

const (
	// ProtocolVersion is the MCP protocol version this client speaks.
	ProtocolVersion = "2025-06-18"
	// maxResultBytes bounds a single MCP response body.
	maxResultBytes = 512 * 1024
	// maxSSEEndpointWait bounds waiting for the SSE endpoint event.
	maxSSEEndpointWait = 10 * time.Second
)

// Tool is one tool advertised by an MCP server.
type Tool struct {
	Name        string         `json:"name"`
	Title       string         `json:"title,omitempty"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema,omitempty"`
}

// ContentBlock is one content block of a tool call result.
type ContentBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

// CallResult is a normalized tools/call result.
type CallResult struct {
	Content           []ContentBlock
	IsError           bool
	StructuredContent map[string]any
}

// Client speaks the minimal MCP JSON-RPC subset (initialize, tools/list,
// tools/call) over streamable HTTP and SSE transports.
type Client struct {
	httpClient *http.Client
	// sseClient has no total timeout: the SSE event stream is long-lived and
	// bounded by the caller's context instead.
	sseClient *http.Client
}

// New returns a Client with a bounded HTTP client.
func New() *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 25 * time.Second},
		sseClient:  &http.Client{},
	}
}

// ListTools returns the tool list of an MCP server.
func (c *Client) ListTools(ctx context.Context, server *store.McpServerMessage) ([]Tool, error) {
	switch server.TransportType {
	case "http":
		return c.httpListTools(ctx, server)
	case "sse":
		return c.sseListTools(ctx, server)
	default:
		return nil, pkgerrors.Errorf("unsupported mcp transport %q", server.TransportType)
	}
}

// CallTool invokes a tool on an MCP server.
func (c *Client) CallTool(ctx context.Context, server *store.McpServerMessage, toolName string, args map[string]any) (*CallResult, error) {
	switch server.TransportType {
	case "http":
		return c.httpCallTool(ctx, server, toolName, args)
	case "sse":
		return c.sseCallTool(ctx, server, toolName, args)
	default:
		return nil, pkgerrors.Errorf("unsupported mcp transport %q", server.TransportType)
	}
}

// --- streamable HTTP ---

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (c *Client) httpListTools(ctx context.Context, server *store.McpServerMessage) ([]Tool, error) {
	endpoint, err := url.Parse(server.URL)
	if err != nil {
		return nil, err
	}
	sessionID, err := c.httpInitialize(ctx, endpoint, server.Headers)
	if err != nil {
		return nil, err
	}
	result, err := c.httpRPC(ctx, endpoint, server.Headers, sessionID, "tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var list struct {
		Tools      []Tool `json:"tools"`
		NextCursor string `json:"nextCursor,omitempty"`
	}
	if err := json.Unmarshal(result, &list); err != nil {
		return nil, pkgerrors.Wrap(err, "decode tools/list result")
	}
	for cursor := list.NextCursor; cursor != ""; {
		page, err := c.httpRPC(ctx, endpoint, server.Headers, sessionID, "tools/list", map[string]any{"cursor": cursor})
		if err != nil {
			return nil, err
		}
		var next struct {
			Tools      []Tool `json:"tools"`
			NextCursor string `json:"nextCursor,omitempty"`
		}
		if err := json.Unmarshal(page, &next); err != nil {
			return nil, pkgerrors.Wrap(err, "decode tools/list page")
		}
		list.Tools = append(list.Tools, next.Tools...)
		cursor = next.NextCursor
	}
	return list.Tools, nil
}

func (c *Client) httpCallTool(ctx context.Context, server *store.McpServerMessage, toolName string, args map[string]any) (*CallResult, error) {
	endpoint, err := url.Parse(server.URL)
	if err != nil {
		return nil, err
	}
	sessionID, err := c.httpInitialize(ctx, endpoint, server.Headers)
	if err != nil {
		return nil, err
	}
	result, err := c.httpRPC(ctx, endpoint, server.Headers, sessionID, "tools/call", map[string]any{
		"name":      toolName,
		"arguments": args,
	})
	if err != nil {
		return nil, err
	}
	var raw struct {
		Content           []ContentBlock `json:"content"`
		IsError           bool           `json:"isError"`
		StructuredContent map[string]any `json:"structuredContent,omitempty"`
	}
	if err := json.Unmarshal(result, &raw); err != nil {
		return nil, pkgerrors.Wrap(err, "decode tools/call result")
	}
	return &CallResult{
		Content:           raw.Content,
		IsError:           raw.IsError,
		StructuredContent: raw.StructuredContent,
	}, nil
}

// httpInitialize performs the initialize handshake and returns the optional
// Mcp-Session-Id header value.
func (c *Client) httpInitialize(ctx context.Context, endpoint *url.URL, headers map[string]string) (string, error) {
	initParams := map[string]any{
		"protocolVersion": ProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "laelia-manager",
			"version": "1.0.0",
		},
	}
	resp, err := c.doHTTP(ctx, endpoint, headers, "", "initialize", initParams)
	if err != nil {
		return "", err
	}
	if _, err := decodeRPCResponse(resp.Body, "initialize"); err != nil {
		_ = resp.Body.Close()
		return "", err
	}
	sessionID := resp.Header.Get("Mcp-Session-Id")
	_ = resp.Body.Close()
	// The initialized notification is fire-and-forget; a server that rejects it
	// still accepts the subsequent request.
	_, _ = c.httpRPC(ctx, endpoint, headers, sessionID, "notifications/initialized", map[string]any{})
	return sessionID, nil
}

// httpRPC sends one JSON-RPC request and decodes the result.
func (c *Client) httpRPC(ctx context.Context, endpoint *url.URL, headers map[string]string, sessionID, method string, params any) (json.RawMessage, error) {
	resp, err := c.doHTTP(ctx, endpoint, headers, sessionID, method, params)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return decodeRPCResponse(resp.Body, method)
}

func (c *Client) doHTTP(ctx context.Context, endpoint *url.URL, headers map[string]string, sessionID, method string, params any) (*http.Response, error) {
	body, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: 1, Method: method, Params: params})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		return nil, pkgerrors.Errorf("mcp http %s failed: status %d: %s", method, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return resp, nil
}

func decodeRPCResponse(body io.Reader, method string) (json.RawMessage, error) {
	raw, err := io.ReadAll(io.LimitReader(body, maxResultBytes))
	if err != nil {
		return nil, pkgerrors.Wrap(err, "read "+method+" response")
	}
	var resp rpcResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, pkgerrors.Wrap(err, "decode "+method+" response")
	}
	if resp.Error != nil {
		return nil, pkgerrors.Errorf("mcp %s failed: code %d: %s", method, resp.Error.Code, resp.Error.Message)
	}
	return resp.Result, nil
}

// --- SSE ---

// sseSession is one SSE transport session: the event stream plus the resolved
// messages endpoint.
type sseSession struct {
	body        io.ReadCloser
	reader      *bufio.Reader
	messagesURL *url.URL
	sessionID   string
	close       func()
}

func (c *Client) sseListTools(ctx context.Context, server *store.McpServerMessage) ([]Tool, error) {
	sess, err := c.sseOpen(ctx, server)
	if err != nil {
		return nil, err
	}
	defer sess.close()
	result, err := c.sseRPC(ctx, sess, "tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var list struct {
		Tools []Tool `json:"tools"`
	}
	if err := json.Unmarshal(result, &list); err != nil {
		return nil, pkgerrors.Wrap(err, "decode tools/list result")
	}
	return list.Tools, nil
}

func (c *Client) sseCallTool(ctx context.Context, server *store.McpServerMessage, toolName string, args map[string]any) (*CallResult, error) {
	sess, err := c.sseOpen(ctx, server)
	if err != nil {
		return nil, err
	}
	defer sess.close()
	result, err := c.sseRPC(ctx, sess, "tools/call", map[string]any{
		"name":      toolName,
		"arguments": args,
	})
	if err != nil {
		return nil, err
	}
	var raw struct {
		Content           []ContentBlock `json:"content"`
		IsError           bool           `json:"isError"`
		StructuredContent map[string]any `json:"structuredContent,omitempty"`
	}
	if err := json.Unmarshal(result, &raw); err != nil {
		return nil, pkgerrors.Wrap(err, "decode tools/call result")
	}
	return &CallResult{
		Content:           raw.Content,
		IsError:           raw.IsError,
		StructuredContent: raw.StructuredContent,
	}, nil
}

// sseOpen establishes an SSE event stream, performs initialize, and returns a
// session bound to the messages endpoint.
func (c *Client) sseOpen(ctx context.Context, server *store.McpServerMessage) (*sseSession, error) {
	base, err := url.Parse(server.URL)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	for name, value := range server.Headers {
		req.Header.Set(name, value)
	}
	//nolint:bodyclose // SSE response body is the long-lived event stream, closed by sseSession.close
	resp, err := c.sseClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = resp.Body.Close()
		return nil, pkgerrors.Errorf("mcp sse connect failed: status %d", resp.StatusCode)
	}
	sess := &sseSession{
		body:   resp.Body,
		reader: bufio.NewReaderSize(resp.Body, 64*1024),
	}
	sess.close = func() { _ = resp.Body.Close() }

	endpoint, sessionID, err := c.sseWaitEndpoint(ctx, sess)
	if err != nil {
		sess.close()
		return nil, err
	}
	sess.messagesURL = base.ResolveReference(endpoint)
	sess.sessionID = sessionID

	initParams := map[string]any{
		"protocolVersion": ProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "laelia-manager",
			"version": "1.0.0",
		},
	}
	if _, err := c.sseRPC(ctx, sess, "initialize", initParams); err != nil {
		sess.close()
		return nil, err
	}
	c.sseNotify(ctx, sess, "notifications/initialized", map[string]any{})
	return sess, nil
}

// sseNotify posts a JSON-RPC notification and does not wait for a response.
func (c *Client) sseNotify(ctx context.Context, sess *sseSession, method string, params any) {
	body, err := json.Marshal(rpcRequest{JSONRPC: "2.0", Method: method, Params: params})
	if err != nil {
		return
	}
	messagesURL := *sess.messagesURL
	query := messagesURL.Query()
	if sess.sessionID != "" {
		query.Set("session_id", sess.sessionID)
	}
	messagesURL.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, messagesURL.String(), bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := c.sseClient.Do(req)
	if err != nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResultBytes))
	_ = resp.Body.Close()
}

// sseWaitEndpoint reads SSE events until the endpoint event arrives.
func (*Client) sseWaitEndpoint(ctx context.Context, sess *sseSession) (*url.URL, string, error) {
	deadline := time.Now().Add(maxSSEEndpointWait)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, "", ctx.Err()
		default:
		}
		event, data, err := sseNextEvent(sess.reader)
		if err != nil {
			return nil, "", pkgerrors.Wrap(err, "read sse endpoint event")
		}
		if event != "endpoint" || data == "" {
			continue
		}
		endpoint, err := url.Parse(data)
		if err != nil {
			return nil, "", pkgerrors.Wrapf(err, "invalid sse endpoint %q", data)
		}
		return endpoint, endpoint.Query().Get("session_id"), nil
	}
	return nil, "", pkgerrors.New("timed out waiting for sse endpoint event")
}

// sseRPC posts one JSON-RPC request to the messages endpoint and waits for the
// matching response, either in the HTTP response body or over the event stream.
func (c *Client) sseRPC(ctx context.Context, sess *sseSession, method string, params any) (json.RawMessage, error) {
	body, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: 1, Method: method, Params: params})
	if err != nil {
		return nil, err
	}
	messagesURL := *sess.messagesURL
	query := messagesURL.Query()
	if sess.sessionID != "" {
		query.Set("session_id", sess.sessionID)
	}
	messagesURL.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, messagesURL.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	postResp, err := c.sseClient.Do(req)
	if err != nil {
		return nil, err
	}
	if postResp.StatusCode < 200 || postResp.StatusCode >= 300 {
		_ = postResp.Body.Close()
		return nil, pkgerrors.Errorf("mcp sse %s failed: status %d", method, postResp.StatusCode)
	}
	raw, _ := io.ReadAll(io.LimitReader(postResp.Body, maxResultBytes))
	_ = postResp.Body.Close()
	if len(bytes.TrimSpace(raw)) > 0 {
		var resp rpcResponse
		if err := json.Unmarshal(raw, &resp); err == nil && (resp.Result != nil || resp.Error != nil) {
			if resp.Error != nil {
				return nil, pkgerrors.Errorf("mcp %s failed: code %d: %s", method, resp.Error.Code, resp.Error.Message)
			}
			return resp.Result, nil
		}
	}

	// The server may answer asynchronously over the event stream.
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		event, data, err := sseNextEvent(sess.reader)
		if err != nil {
			return nil, pkgerrors.Wrap(err, "read sse "+method+" response")
		}
		if event != "message" || data == "" {
			continue
		}
		var resp rpcResponse
		if err := json.Unmarshal([]byte(data), &resp); err != nil {
			continue
		}
		if resp.ID != 1 {
			continue
		}
		if resp.Error != nil {
			return nil, pkgerrors.Errorf("mcp %s failed: code %d: %s", method, resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	}
}

// sseNextEvent reads one SSE event (event/data lines) from the stream.
func sseNextEvent(reader *bufio.Reader) (event, data string, err error) {
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF && line == "" {
				return "", "", io.ErrUnexpectedEOF
			}
			return "", "", err
		}
		line = strings.TrimRight(line, "\r\n")
		switch {
		case line == "":
			if event != "" || data != "" {
				return event, data, nil
			}
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data += strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		default:
		}
	}
}
