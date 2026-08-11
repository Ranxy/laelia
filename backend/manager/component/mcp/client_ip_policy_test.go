package mcp

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
	"github.com/Ranxy/laelia/backend/manager/store"
)

func ipPolicyFunc(t *testing.T, cp *CompiledPolicy) IPPolicyFunc {
	t.Helper()
	return func(_ context.Context, _ *store.McpServerMessage, ip netip.Addr) (bool, error) {
		reason, err := cp.Allowed(ip)
		if err != nil {
			return false, err
		}
		return reason == nil, nil
	}
}

func compilePolicy(t *testing.T, allow, deny []string) *CompiledPolicy {
	t.Helper()
	cp, err := ParsePolicy(&storepb.McpIpPolicy{
		Enabled:    true,
		Scope:      storepb.McpIpPolicy_SCOPE_ALL,
		AllowCidrs: allow,
		DenyCidrs:  deny,
	})
	require.NoError(t, err)
	return cp
}

func TestClientIPPolicyBlocksLoopback(t *testing.T) {
	mcpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	defer mcpServer.Close()

	client := New()
	client.SetIPPolicy(ipPolicyFunc(t, compilePolicy(t, nil, []string{"127.0.0.0/8"})))
	_, err := client.ListTools(context.Background(), &store.McpServerMessage{
		TransportType: "http",
		URL:           mcpServer.URL,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "blocked by the MCP IP policy")
}

func TestClientIPPolicyAllowListLetsThrough(t *testing.T) {
	mcpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18","capabilities":{},"serverInfo":{"name":"t","version":"1"}}}`))
	}))
	defer mcpServer.Close()

	client := New()
	client.SetIPPolicy(ipPolicyFunc(t, compilePolicy(t, []string{"127.0.0.0/8"}, nil)))
	tools, err := client.ListTools(context.Background(), &store.McpServerMessage{
		TransportType: "http",
		URL:           mcpServer.URL,
	})
	require.NoError(t, err)
	assert.Empty(t, tools)
}

func TestClientIPPolicyBlocksHostnameResolvingToDeniedIP(t *testing.T) {
	mcpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	defer mcpServer.Close()
	port := mcpServer.Listener.Addr().(*net.TCPAddr).Port

	client := New()
	client.SetIPPolicy(ipPolicyFunc(t, compilePolicy(t, nil, []string{"127.0.0.0/8", "::1/128"})))
	_, err := client.ListTools(context.Background(), &store.McpServerMessage{
		TransportType: "http",
		URL:           fmt.Sprintf("http://localhost:%d", port),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "blocked by the MCP IP policy")
}

func TestClientIPPolicyCoversSSE(t *testing.T) {
	mcpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
	}))
	defer mcpServer.Close()

	client := New()
	client.SetIPPolicy(ipPolicyFunc(t, compilePolicy(t, nil, []string{"127.0.0.0/8"})))
	_, err := client.ListTools(context.Background(), &store.McpServerMessage{
		TransportType: "sse",
		URL:           mcpServer.URL,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "blocked by the MCP IP policy")
}

func TestClientNoPolicyStillConnects(t *testing.T) {
	mcpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18","capabilities":{},"serverInfo":{"name":"t","version":"1"}}}`))
	}))
	defer mcpServer.Close()

	tools, err := New().ListTools(context.Background(), &store.McpServerMessage{
		TransportType: "http",
		URL:           mcpServer.URL,
	})
	require.NoError(t, err)
	assert.Empty(t, tools)
}
