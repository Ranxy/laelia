package ratelimit

import (
	"context"
	"testing"

	"connectrpc.com/connect"

	"github.com/Ranxy/laelia/backend/common"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

// newTestLimiter builds a RateLimiter whose per-IP bucket exhausts after a
// single call, while the global bucket stays generous so only the per-IP limit
// is exercised.
func newTestLimiter(t *testing.T, trustProxy bool) *RateLimiter {
	t.Helper()
	cfg := Config{
		GlobalRate:     1000,
		GlobalBurst:    1000,
		ConnectRate:    1.0 / 60.0, // ~0 tokens refill during the test
		ConnectBurst:   1,
		HeartbeatRate:  1,
		HeartbeatBurst: 1,
		LoginRate:      1,
		LoginBurst:     1,
		APIRate:        1,
		APIBurst:       1,
		TrustProxy:     trustProxy,
	}
	rl, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return rl
}

func anonymousReq(ip string) connect.AnyRequest {
	req := connect.NewRequest(&v1pb.HelloRequest{})
	req.Header().Set("X-Forwarded-For", ip)
	return req
}

// TestRateLimit_PerIPAppliesToAnonymous verifies that anonymous (unauthenticated)
// calls fall under a per-IP limiter instead of only the shared global budget.
// Two calls from the same IP exhaust the burst=1 per-IP bucket; a call from a
// different IP is served from its own bucket and succeeds.
func TestRateLimit_PerIPAppliesToAnonymous(t *testing.T) {
	rl := newTestLimiter(t, true)
	ctx := context.Background()
	pass := func(req connect.AnyRequest) error {
		_, err := rl.WrapUnary(func(_ context.Context, _ connect.AnyRequest) (connect.AnyResponse, error) {
			return nil, nil
		})(ctx, req)
		return err
	}

	// Same IP: first allowed, second exhausted by the per-IP bucket.
	if err := pass(anonymousReq("1.1.1.1")); err != nil {
		t.Fatalf("first call from 1.1.1.1 should pass, got %v", err)
	}
	if err := pass(anonymousReq("1.1.1.1")); err == nil {
		t.Fatal("second call from 1.1.1.1 should be rate-limited (per-IP), got nil")
	} else if code := connect.CodeOf(err); code != connect.CodeResourceExhausted {
		t.Fatalf("expected ResourceExhausted, got %s: %v", code, err)
	}

	// Different IP: separate bucket, not yet exhausted.
	if err := pass(anonymousReq("2.2.2.2")); err != nil {
		t.Fatalf("first call from 2.2.2.2 should pass (separate per-IP bucket), got %v", err)
	}
}

// TestRateLimit_AnonymousIgnoresHeadersWhenNotTrustingProxy ensures that when
// trustProxy is false, a client cannot spoof X-Forwarded-For to dodge the
// per-IP limiter: all anonymous calls key on the (empty) peer address.
func TestRateLimit_AnonymousIgnoresHeadersWhenNotTrustingProxy(t *testing.T) {
	rl := newTestLimiter(t, false)
	ctx := context.Background()
	pass := func(req connect.AnyRequest) error {
		_, err := rl.WrapUnary(func(_ context.Context, _ connect.AnyRequest) (connect.AnyResponse, error) {
			return nil, nil
		})(ctx, req)
		return err
	}

	// Both requests carry a spoofed X-Forwarded-For but trustProxy=false, so they
	// key on the empty peer address ("unknown") and share one bucket.
	if err := pass(anonymousReq("spoofed-a")); err != nil {
		t.Fatalf("first call should pass, got %v", err)
	}
	if err := pass(anonymousReq("spoofed-b")); err == nil {
		t.Fatal("second call should be rate-limited; spoofed XFF must be ignored when trustProxy=false")
	}
}

// testPrincipal is a context value that satisfies the identifier interface
// extractIdentifier asserts for, mirroring how the auth interceptor injects
// store.UserMessage / store.AgentMessage into the context.
type testPrincipal string

func (p testPrincipal) GetResourceID() string { return string(p) }

// newTieredLimiter builds a RateLimiter where the per-IP "connect" bucket is
// tight (burst 1) but the per-user and per-agent API buckets are generous
// (burst 5), so a call routed to the user/agent bucket is distinguishable from
// one routed to the per-IP bucket.
func newTieredLimiter(t *testing.T) *RateLimiter {
	t.Helper()
	cfg := Config{
		GlobalRate:     1000,
		GlobalBurst:    1000,
		ConnectRate:    1.0 / 60.0,
		ConnectBurst:   1,
		HeartbeatRate:  1000,
		HeartbeatBurst: 5,
		LoginRate:      1,
		LoginBurst:     1,
		APIRate:        1000,
		APIBurst:       5,
		TrustProxy:     false,
	}
	rl, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return rl
}

// TestRateLimit_AuthenticatedKeysOnUserNotIP is a regression test for the bug
// where every authenticated API call was misclassified as anonymous (because
// the limiter ran before auth and the context type lacked GetResourceID) and
// thus throttled by the tiny per-IP "connect" bucket — a few clicks -> 429.
// Here an authenticated user makes several generic API calls; they must all
// pass under the generous per-user bucket, even though the same-IP per-IP
// bucket would deny after a single call.
func TestRateLimit_AuthenticatedKeysOnUserNotIP(t *testing.T) {
	rl := newTieredLimiter(t)
	ctx := context.WithValue(context.Background(), common.UserContextKey, testPrincipal("users/42"))

	// Generic API procedure — not connect/login/heartbeat — so it hits the
	// default branch, which must key on the user principal, not source IP.
	req := connect.NewRequest(&v1pb.HelloRequest{})
	req.Header().Set("X-Forwarded-For", "9.9.9.9") // ignored: TrustProxy=false

	pass := func() error {
		_, err := rl.WrapUnary(func(_ context.Context, _ connect.AnyRequest) (connect.AnyResponse, error) {
			return nil, nil
		})(ctx, req)
		return err
	}

	// 4 calls: the per-IP "connect" bucket (burst 1) would deny calls 2-4, but
	// the per-user bucket (burst 5) admits all of them. This proves the call
	// is keyed on the user, not the IP.
	for i := 1; i <= 4; i++ {
		if err := pass(); err != nil {
			t.Fatalf("authenticated call %d should pass under the user bucket, got %v", i, err)
		}
	}

	// Sanity: the same IP with NO principal is anonymous and IS throttled by
	// the per-IP bucket — proving the buckets really are distinct and the
	// pass above was not just a global-bucket artifact.
	anonCtx := context.Background()
	anonReq := connect.NewRequest(&v1pb.HelloRequest{})
	anonReq.Header().Set("X-Forwarded-For", "9.9.9.9")
	if _, err := rl.WrapUnary(func(_ context.Context, _ connect.AnyRequest) (connect.AnyResponse, error) {
		return nil, nil
	})(anonCtx, anonReq); err != nil {
		t.Fatalf("first anonymous call should pass, got %v", err)
	}
	if _, err := rl.WrapUnary(func(_ context.Context, _ connect.AnyRequest) (connect.AnyResponse, error) {
		return nil, nil
	})(anonCtx, anonReq); err == nil {
		t.Fatal("second anonymous call from same IP should be rate-limited by the per-IP bucket, got nil")
	} else if code := connect.CodeOf(err); code != connect.CodeResourceExhausted {
		t.Fatalf("expected ResourceExhausted, got %s: %v", code, err)
	}
}

// TestRateLimit_AgentAPICallKeysOnAgent verifies agent-issued generic API calls
// (non-heartbeat) are keyed on the agent principal rather than falling through
// to the restrictive per-IP "connect" bucket.
func TestRateLimit_AgentAPICallKeysOnAgent(t *testing.T) {
	rl := newTieredLimiter(t)
	ctx := context.WithValue(context.Background(), common.AgentContextKey, testPrincipal("agents/abc"))
	req := connect.NewRequest(&v1pb.HelloRequest{})

	for i := 1; i <= 4; i++ {
		_, err := rl.WrapUnary(func(_ context.Context, _ connect.AnyRequest) (connect.AnyResponse, error) {
			return nil, nil
		})(ctx, req)
		if err != nil {
			t.Fatalf("agent API call %d should pass under the agent bucket, got %v", i, err)
		}
	}
}
