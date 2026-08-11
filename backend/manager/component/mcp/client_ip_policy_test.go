package mcp

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"testing"
	"time"

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

func TestClientDialFallsBackToApprovedAddress(t *testing.T) {
	// The first resolved address (::1) is denied; the second (127.0.0.1) is
	// approved. The dial must fall back to the approved address instead of
	// failing on the first one.
	mcpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18","capabilities":{},"serverInfo":{"name":"t","version":"1"}}}`))
	}))
	defer mcpServer.Close()
	port := mcpServer.Listener.Addr().(*net.TCPAddr).Port

	client := New()
	client.lookupIP = func(_ context.Context, host string) ([]netip.Addr, error) {
		require.Equal(t, "test.local", host)
		return []netip.Addr{netip.MustParseAddr("::1"), netip.MustParseAddr("127.0.0.1")}, nil
	}
	client.SetIPPolicy(ipPolicyFunc(t, compilePolicy(t, []string{"127.0.0.0/8"}, []string{"::1/128"})))
	tools, err := client.ListTools(context.Background(), &store.McpServerMessage{
		TransportType: "http",
		URL:           fmt.Sprintf("http://test.local:%d", port),
	})
	require.NoError(t, err)
	assert.Empty(t, tools)
}

func TestClientHTTPProxyAddressNotPolicyChecked(t *testing.T) {
	// The proxy address (127.0.0.2) is inside the deny list, but it must not
	// be policy-checked: with a proxy, DialContext sees the proxy address,
	// and the target itself is checked at request time. The dial of the
	// unreachable proxy fails with a connection error, not a policy error.
	mcpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	defer mcpServer.Close()
	port := mcpServer.Listener.Addr().(*net.TCPAddr).Port
	proxyURL, err := url.Parse("http://127.0.0.2:1")
	require.NoError(t, err)

	client := New()
	client.httpClient = &http.Client{
		Timeout:   25 * time.Second,
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
	}
	client.lookupIP = func(_ context.Context, host string) ([]netip.Addr, error) {
		require.Equal(t, "test.local", host)
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}
	client.SetIPPolicy(ipPolicyFunc(t, compilePolicy(t, []string{"127.0.0.1/32"}, []string{"127.0.0.2/32"})))
	_, err = client.ListTools(context.Background(), &store.McpServerMessage{
		TransportType: "http",
		URL:           fmt.Sprintf("http://test.local:%d", port),
	})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "blocked by the MCP IP policy")
}

func TestClientRedirectTargetPolicyChecked(t *testing.T) {
	// The initial target (127.0.0.1) is approved but the redirect target
	// (127.0.0.2) is not; the redirect must be blocked.
	redirected, err := net.Listen("tcp", "127.0.0.2:0")
	if err != nil {
		t.Skipf("cannot bind 127.0.0.2: %v", err)
	}
	redirectedPort := redirected.Addr().(*net.TCPAddr).Port
	redirectedServer := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	})}
	go func() { _ = redirectedServer.Serve(redirected) }()
	defer redirectedServer.Close()

	mcpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, fmt.Sprintf("http://127.0.0.2:%d/", redirectedPort), http.StatusFound)
	}))
	defer mcpServer.Close()

	client := New()
	client.SetIPPolicy(ipPolicyFunc(t, compilePolicy(t, []string{"127.0.0.1/32"}, nil)))
	_, err = client.ListTools(context.Background(), &store.McpServerMessage{
		TransportType: "http",
		URL:           mcpServer.URL,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "blocked by the MCP IP policy")
}

func TestClientGuardedClientCachedPerServer(t *testing.T) {
	client := New()
	client.SetIPPolicy(ipPolicyFunc(t, compilePolicy(t, nil, nil)))
	server := &store.McpServerMessage{TransportType: "http", URL: "http://example.test:1"}
	first := client.httpClientFor(server)
	second := client.httpClientFor(server)
	assert.Same(t, first, second)
	// A different server URL gets its own transport.
	other := &store.McpServerMessage{TransportType: "http", URL: "http://other.test:1"}
	assert.NotSame(t, first, client.httpClientFor(other))
}

func TestClientTargetCacheScopedPerServer(t *testing.T) {
	// Two servers share one hostname; the workspace server (policy scope
	// does not apply) is allowed first, but the user-created server must
	// still be checked and blocked. The resolved-target cache must not leak
	// the workspace server's verdict to the personal one.
	mcpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18","capabilities":{},"serverInfo":{"name":"t","version":"1"}}}`))
	}))
	defer mcpServer.Close()
	port := mcpServer.Listener.Addr().(*net.TCPAddr).Port

	client := New()
	client.lookupIP = func(_ context.Context, host string) ([]netip.Addr, error) {
		require.Equal(t, "test.local", host)
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}
	policy, err := ParsePolicy(&storepb.McpIpPolicy{
		Enabled:   true,
		Scope:     storepb.McpIpPolicy_SCOPE_USER_CREATED,
		DenyCidrs: []string{"127.0.0.0/8"},
	})
	require.NoError(t, err)
	client.SetIPPolicy(func(_ context.Context, server *store.McpServerMessage, ip netip.Addr) (bool, error) {
		if !policy.AppliesTo(server.OwnerID) {
			return true, nil
		}
		reason, err := policy.Allowed(ip)
		if err != nil {
			return false, err
		}
		return reason == nil, nil
	})

	workspace := &store.McpServerMessage{
		ResourceID:    "ws-server",
		TransportType: "http",
		URL:           fmt.Sprintf("http://test.local:%d", port),
	}
	_, err = client.ListTools(context.Background(), workspace)
	require.NoError(t, err)

	personal := &store.McpServerMessage{
		ResourceID:    "personal-server",
		OwnerID:       1,
		TransportType: "http",
		URL:           fmt.Sprintf("http://test.local:%d", port),
	}
	_, err = client.ListTools(context.Background(), personal)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "blocked by the MCP IP policy")
}
