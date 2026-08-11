package mcp

import (
	"fmt"
	"net/netip"
	"strings"

	"github.com/pkg/errors"

	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
)

const (
	// maxIPPolicyCIDRs bounds the combined allow/deny list size, protecting the
	// manager from pathological policy payloads.
	maxIPPolicyCIDRs = 500
)

// IPPolicyDenyReason describes why a target address was rejected by a
// CompiledPolicy.
type IPPolicyDenyReason struct {
	// IP is the offending address (already Unmap()-normalized).
	IP netip.Addr
	// DeniedByAllowList is true when the address matched none of a non-empty
	// allow list; false when it matched the deny list.
	DeniedByAllowList bool
	// Prefix is the matched deny prefix (only when DeniedByAllowList is false).
	Prefix netip.Prefix
}

func (r *IPPolicyDenyReason) Error() string {
	if r == nil {
		return ""
	}
	if r.DeniedByAllowList {
		return fmt.Sprintf("%s is not in the MCP IP policy allow list", r.IP)
	}
	return fmt.Sprintf("%s is denied by MCP IP policy prefix %s", r.IP, r.Prefix)
}

// CompiledPolicy is the parsed, normalized form of a storepb.McpIpPolicy.
// Zero-value policies (enabled=false) allow everything.
type CompiledPolicy struct {
	enabled bool
	scope   storepb.McpIpPolicy_Scope
	allow   []netip.Prefix
	deny    []netip.Prefix
}

// ParsePolicy validates and compiles a policy. Invalid CIDR entries (or more
// than maxIPPolicyCIDRs entries in total) are rejected with an error naming
// the offending entry.
func ParsePolicy(p *storepb.McpIpPolicy) (*CompiledPolicy, error) {
	cp := &CompiledPolicy{}
	if p == nil || !p.GetEnabled() {
		return cp, nil
	}
	if len(p.GetAllowCidrs())+len(p.GetDenyCidrs()) > maxIPPolicyCIDRs {
		return nil, errors.Errorf("MCP IP policy exceeds the limit of %d CIDR entries", maxIPPolicyCIDRs)
	}
	var err error
	cp.enabled = true
	cp.scope = p.GetScope()
	if cp.allow, err = parseCIDRList(p.GetAllowCidrs(), "allow"); err != nil {
		return nil, err
	}
	if cp.deny, err = parseCIDRList(p.GetDenyCidrs(), "deny"); err != nil {
		return nil, err
	}
	return cp, nil
}

// parseCIDRList parses and normalizes one list, deduplicating masked prefixes.
func parseCIDRList(cidrs []string, listName string) ([]netip.Prefix, error) {
	out := make([]netip.Prefix, 0, len(cidrs))
	seen := make(map[netip.Prefix]bool, len(cidrs))
	for _, raw := range cidrs {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		p, err := netip.ParsePrefix(raw)
		if err != nil {
			return nil, errors.Errorf("invalid MCP IP policy %s CIDR %q", listName, raw)
		}
		p = p.Masked()
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out, nil
}

// Allowed reports whether the address passes the policy. IPv4-mapped IPv6
// addresses are normalized via Unmap() before matching. A nil reason means
// allowed.
func (cp *CompiledPolicy) Allowed(addr netip.Addr) (*IPPolicyDenyReason, error) {
	if cp == nil || !cp.enabled {
		return nil, nil
	}
	addr = addr.Unmap()
	for _, p := range cp.deny {
		if p.Contains(addr) {
			return &IPPolicyDenyReason{IP: addr, Prefix: p}, nil
		}
	}
	if len(cp.allow) > 0 {
		matched := false
		for _, p := range cp.allow {
			if p.Contains(addr) {
				matched = true
				break
			}
		}
		if !matched {
			return &IPPolicyDenyReason{IP: addr, DeniedByAllowList: true}, nil
		}
	}
	return nil, nil
}

// HasAllowRestriction reports whether a non-empty allow list restricts the
// allowed address space. It is used to decide fail-closed behavior when a
// hostname cannot be resolved.
func (cp *CompiledPolicy) HasAllowRestriction() bool {
	return cp != nil && cp.enabled && len(cp.allow) > 0
}

// AppliesTo reports whether the policy applies to the server with the given
// owner id (0 = workspace-global server, non-zero = personal server).
func (cp *CompiledPolicy) AppliesTo(ownerID int64) bool {
	if cp == nil || !cp.enabled {
		return false
	}
	switch cp.scope {
	case storepb.McpIpPolicy_SCOPE_ALL:
		return true
	default:
		// SCOPE_UNSPECIFIED is treated as SCOPE_USER_CREATED (conservative).
		return ownerID != 0
	}
}
