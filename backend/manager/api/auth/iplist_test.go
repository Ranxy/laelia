package auth

import (
	"net/http"
	"testing"

	"connectrpc.com/connect"
)

// TestExtractSourceIP_RespectsTrustProxy verifies that client-supplied forwarding
// headers are only honored when trustProxy is true, and that the raw TCP peer
// address is used otherwise — so the source IP is always populated (never the
// empty string that previously disabled agent IP allowlists and audit logging).
func TestExtractSourceIP_RespectsTrustProxy(t *testing.T) {
	withHeaders := func() http.Header {
		h := http.Header{}
		h.Set("X-Forwarded-For", "5.6.7.8, 9.10.11.12")
		h.Set("X-Real-IP", "5.6.7.8")
		return h
	}
	empty := func() http.Header { return http.Header{} }
	realIponly := func() http.Header {
		h := http.Header{}
		h.Set("X-Real-IP", "5.6.7.8")
		return h
	}

	cases := []struct {
		name       string
		header     http.Header
		trustProxy bool
		remoteAddr string
		want       string
	}{
		{"trustProxy false ignores headers, uses peer", withHeaders(), false, "1.2.3.4:5678", "1.2.3.4"},
		{"trustProxy false with no peer yields empty", withHeaders(), false, "", ""},
		{"trustProxy true uses leftmost XFF", withHeaders(), true, "1.2.3.4:5678", "5.6.7.8"},
		{"trustProxy true X-Real-IP when no XFF", realIponly(), true, "1.2.3.4:5678", "5.6.7.8"},
		{"trustProxy true falls back to peer when no headers", empty(), true, "1.2.3.4:5678", "1.2.3.4"},
		{"trustProxy true peer host without port", empty(), true, "peer-host", "peer-host"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractSourceIP(tc.header, tc.remoteAddr, tc.trustProxy)
			if got != tc.want {
				t.Fatalf("extractSourceIP(trustProxy=%v, remoteAddr=%q) = %q, want %q", tc.trustProxy, tc.remoteAddr, got, tc.want)
			}
		})
	}
}

// TestExtractSourceIP_RealIPOnlyUnderTrustProxy guards the specific spoofing
// vector in the old implementation: X-Real-IP was trusted unconditionally. It
// must now be ignored when trustProxy is false.
func TestExtractSourceIP_RealIPOnlyUnderTrustProxy(t *testing.T) {
	h := http.Header{}
	h.Set("X-Real-IP", "spoofed-ip")

	if got := extractSourceIP(h, "1.2.3.4:5678", false); got != "1.2.3.4" {
		t.Fatalf("X-Real-IP must be ignored when trustProxy=false, got %q", got)
	}
	if got := extractSourceIP(h, "1.2.3.4:5678", true); got != "spoofed-ip" {
		t.Fatalf("X-Real-IP must be honored when trustProxy=true, got %q", got)
	}
}

// TestValidateAgentIP_FailClosedOnEmptyWhenAllowlistSet verifies that under the
// Strict (allowlist-enforcement) policy, an unavailable source IP is rejected
// rather than silently allowed. Previously the empty-sourceIP short-circuit
// made the entire allowlist non-functional whenever the source IP could not be
// resolved.
func TestValidateAgentIP_FailClosedOnEmptyWhenAllowlistSet(t *testing.T) {
	cases := []struct {
		name       string
		reportedIP string
		sourceIP   string
		policy     IPValidationPolicy
		wantErr    bool
		wantCode   connect.Code
	}{
		{"off allows everything", "1.1.1.1", "", IPValidationOff, false, 0},
		{"strict empty sourceIP fails closed", "1.1.1.1", "", IPValidationStrict, true, connect.CodePermissionDenied},
		{"strict matching IPs pass", "1.1.1.1", "1.1.1.1", IPValidationStrict, false, 0},
		{"strict mismatch denied", "1.1.1.1", "2.2.2.2", IPValidationStrict, true, connect.CodePermissionDenied},
		{"strict empty reportedIP passes (cannot compare)", "", "2.2.2.2", IPValidationStrict, false, 0},
		{"warn empty sourceIP passes (advisory)", "1.1.1.1", "", IPValidationWarn, false, 0},
		{"warn mismatch passes (advisory)", "1.1.1.1", "2.2.2.2", IPValidationWarn, false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateAgentIP(tc.reportedIP, tc.sourceIP, tc.policy)
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error code %s, got nil", tc.wantCode)
			}
			connErr, ok := err.(*connect.Error)
			if !ok {
				t.Fatalf("expected *connect.Error, got %T: %v", err, err)
			}
			if connErr.Code() != tc.wantCode {
				t.Fatalf("expected code %s, got %s: %v", tc.wantCode, connErr.Code(), err)
			}
		})
	}
}
