package mcp

import (
	"net/netip"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
)

func mustAddr(t *testing.T, s string) netip.Addr {
	t.Helper()
	a, err := netip.ParseAddr(s)
	require.NoError(t, err)
	return a
}

func policy(t *testing.T, enabled bool, scope storepb.McpIpPolicy_Scope, allow, deny []string) *CompiledPolicy {
	t.Helper()
	cp, err := ParsePolicy(&storepb.McpIpPolicy{
		Enabled:    enabled,
		Scope:      scope,
		AllowCidrs: allow,
		DenyCidrs:  deny,
	})
	require.NoError(t, err)
	return cp
}

func TestCompiledPolicyDisabledAllowsEverything(t *testing.T) {
	cp := policy(t, false, storepb.McpIpPolicy_SCOPE_ALL, []string{"10.0.0.0/8"}, []string{"0.0.0.0/0"})
	reason, err := cp.Allowed(mustAddr(t, "10.1.2.3"))
	require.NoError(t, err)
	assert.Nil(t, reason)
}

func TestCompiledPolicyDenyListWinsOverAllowList(t *testing.T) {
	cp := policy(t, true, storepb.McpIpPolicy_SCOPE_ALL, []string{"10.0.0.0/8"}, []string{"10.0.0.0/16"})
	reason, err := cp.Allowed(mustAddr(t, "10.0.1.1"))
	require.NoError(t, err)
	require.NotNil(t, reason)
	assert.False(t, reason.DeniedByAllowList)
	assert.Equal(t, netip.MustParsePrefix("10.0.0.0/16"), reason.Prefix)

	reason, err = cp.Allowed(mustAddr(t, "10.1.1.1"))
	require.NoError(t, err)
	assert.Nil(t, reason)
}

func TestCompiledPolicyEmptyAllowListIsNoRestriction(t *testing.T) {
	cp := policy(t, true, storepb.McpIpPolicy_SCOPE_ALL, nil, []string{"127.0.0.0/8"})
	reason, err := cp.Allowed(mustAddr(t, "8.8.8.8"))
	require.NoError(t, err)
	assert.Nil(t, reason)

	reason, err = cp.Allowed(mustAddr(t, "127.0.0.1"))
	require.NoError(t, err)
	require.NotNil(t, reason)
	assert.False(t, reason.DeniedByAllowList)
}

func TestCompiledPolicyNonEmptyAllowListRestricts(t *testing.T) {
	cp := policy(t, true, storepb.McpIpPolicy_SCOPE_ALL, []string{"10.0.0.0/8", "172.16.0.0/12"}, nil)
	for _, allowed := range []string{"10.0.0.1", "172.16.0.1"} {
		reason, err := cp.Allowed(mustAddr(t, allowed))
		require.NoError(t, err)
		assert.Nil(t, reason, allowed)
	}
	reason, err := cp.Allowed(mustAddr(t, "192.168.1.1"))
	require.NoError(t, err)
	require.NotNil(t, reason)
	assert.True(t, reason.DeniedByAllowList)
}

func TestCompiledPolicyIPv6AndMapped(t *testing.T) {
	cp := policy(t, true, storepb.McpIpPolicy_SCOPE_ALL, nil, []string{"::1/128", "fc00::/7", "127.0.0.0/8"})

	reason, err := cp.Allowed(mustAddr(t, "::1"))
	require.NoError(t, err)
	require.NotNil(t, reason)
	assert.False(t, reason.DeniedByAllowList)

	reason, err = cp.Allowed(mustAddr(t, "fc00::1"))
	require.NoError(t, err)
	require.NotNil(t, reason)

	reason, err = cp.Allowed(mustAddr(t, "2001:4860:4860::8888"))
	require.NoError(t, err)
	assert.Nil(t, reason)

	// IPv4-mapped IPv6 must be normalized before matching.
	reason, err = cp.Allowed(mustAddr(t, "::ffff:127.0.0.1"))
	require.NoError(t, err)
	require.NotNil(t, reason)
	assert.Equal(t, mustAddr(t, "127.0.0.1"), reason.IP)
}

func TestParsePolicyRejectsInvalidCIDR(t *testing.T) {
	_, err := ParsePolicy(&storepb.McpIpPolicy{
		Enabled:    true,
		AllowCidrs: []string{"10.0.0.0/8", "not-a-cidr"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"not-a-cidr"`)

	_, err = ParsePolicy(&storepb.McpIpPolicy{
		Enabled:   true,
		DenyCidrs: []string{"10.0.0.0/33"},
	})
	require.Error(t, err)
}

func TestParsePolicyNormalizesAndDeduplicates(t *testing.T) {
	cp := policy(t, true, storepb.McpIpPolicy_SCOPE_ALL, []string{"10.0.0.1/8", "10.1.2.3/8"}, nil)
	require.Len(t, cp.allow, 1)
	assert.Equal(t, netip.MustParsePrefix("10.0.0.0/8"), cp.allow[0])
}

func TestParsePolicyListLimit(t *testing.T) {
	allow := make([]string, maxIPPolicyCIDRs+1)
	for i := range allow {
		allow[i] = "10.0.0.0/8"
	}
	_, err := ParsePolicy(&storepb.McpIpPolicy{Enabled: true, AllowCidrs: allow})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "limit")
}

func TestCompiledPolicyAppliesToScope(t *testing.T) {
	all := policy(t, true, storepb.McpIpPolicy_SCOPE_ALL, nil, nil)
	assert.True(t, all.AppliesTo(0))
	assert.True(t, all.AppliesTo(7))

	userOnly := policy(t, true, storepb.McpIpPolicy_SCOPE_USER_CREATED, nil, nil)
	assert.False(t, userOnly.AppliesTo(0))
	assert.True(t, userOnly.AppliesTo(7))

	unspecified := policy(t, true, storepb.McpIpPolicy_SCOPE_UNSPECIFIED, nil, nil)
	assert.False(t, unspecified.AppliesTo(0))
	assert.True(t, unspecified.AppliesTo(7))

	disabled := policy(t, false, storepb.McpIpPolicy_SCOPE_ALL, nil, nil)
	assert.False(t, disabled.AppliesTo(0))
	assert.False(t, disabled.AppliesTo(7))

	var nilPolicy *CompiledPolicy
	assert.False(t, nilPolicy.AppliesTo(7))
	reason, err := nilPolicy.Allowed(netip.MustParseAddr("1.2.3.4"))
	require.NoError(t, err)
	assert.Nil(t, reason)
}

func TestIPPolicyDenyReasonError(t *testing.T) {
	r := &IPPolicyDenyReason{IP: netip.MustParseAddr("10.0.0.1"), Prefix: netip.MustParsePrefix("10.0.0.0/8")}
	assert.True(t, strings.Contains(r.Error(), "denied"))

	r = &IPPolicyDenyReason{IP: netip.MustParseAddr("192.168.0.1"), DeniedByAllowList: true}
	assert.True(t, strings.Contains(r.Error(), "allow list"))
}
