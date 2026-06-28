package ratelimit

import (
	"context"
	"testing"

	"connectrpc.com/connect"

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
