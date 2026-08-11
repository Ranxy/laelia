package v1

import (
	"context"
	"net/netip"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	models "github.com/Ranxy/laelia/backend/generated-go/store"
)

type fakeSettingReader struct {
	cfg *models.UserMcpConfigSetting
	err error
}

func (f *fakeSettingReader) GetUserMcpConfigSetting(context.Context) (*models.UserMcpConfigSetting, error) {
	return f.cfg, f.err
}

type fakeResolver struct {
	addrs map[string][]netip.Addr
	err   error
}

func (f *fakeResolver) LookupNetIP(_ context.Context, _ string, host string) ([]netip.Addr, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.addrs[host], nil
}

func ipPolicySetting(enabled bool, scope models.McpIpPolicy_Scope, allow, deny []string) *models.UserMcpConfigSetting {
	return &models.UserMcpConfigSetting{
		AllowUserMcpServers: true,
		McpIpPolicy: &models.McpIpPolicy{
			Enabled:    enabled,
			Scope:      scope,
			AllowCidrs: allow,
			DenyCidrs:  deny,
		},
	}
}

func validateTarget(t *testing.T, settings mcpSettingReader, serverURL string, isPersonal bool, resolver mcpTargetResolver) error {
	t.Helper()
	return validateMcpServerTargetWithResolver(context.Background(), settings, serverURL, isPersonal, resolver)
}

func TestValidateMcpServerTargetDenyList(t *testing.T) {
	settings := &fakeSettingReader{cfg: ipPolicySetting(true, models.McpIpPolicy_SCOPE_ALL, nil, []string{"10.0.0.0/8"})}
	resolver := &fakeResolver{addrs: map[string][]netip.Addr{
		"internal.example.com": {netip.MustParseAddr("10.1.2.3")},
	}}
	err := validateTarget(t, settings, "http://internal.example.com/mcp", true, resolver)
	require.Error(t, err)
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	assert.Contains(t, err.Error(), "internal.example.com")
	assert.Contains(t, err.Error(), "10.1.2.3")
	assert.Contains(t, err.Error(), "denied")
}

func TestValidateMcpServerTargetAllowListMiss(t *testing.T) {
	settings := &fakeSettingReader{cfg: ipPolicySetting(true, models.McpIpPolicy_SCOPE_ALL, []string{"10.0.0.0/8"}, nil)}
	resolver := &fakeResolver{addrs: map[string][]netip.Addr{
		"public.example.com": {netip.MustParseAddr("192.168.1.1")},
	}}
	err := validateTarget(t, settings, "http://public.example.com/mcp", true, resolver)
	require.Error(t, err)
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	assert.Contains(t, err.Error(), "allow list")
}

func TestValidateMcpServerTargetAnyOffendingRecordFailsClosed(t *testing.T) {
	settings := &fakeSettingReader{cfg: ipPolicySetting(true, models.McpIpPolicy_SCOPE_ALL, nil, []string{"169.254.0.0/16"})}
	resolver := &fakeResolver{addrs: map[string][]netip.Addr{
		"dual.example.com": {
			netip.MustParseAddr("93.184.216.34"),
			netip.MustParseAddr("169.254.169.254"),
		},
	}}
	err := validateTarget(t, settings, "https://dual.example.com", true, resolver)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "169.254.169.254")
}

func TestValidateMcpServerTargetIPLiteral(t *testing.T) {
	settings := &fakeSettingReader{cfg: ipPolicySetting(true, models.McpIpPolicy_SCOPE_ALL, nil, []string{"127.0.0.0/8"})}
	err := validateTarget(t, settings, "http://127.0.0.1:3000/mcp", true, &fakeResolver{})
	require.Error(t, err)
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	assert.Contains(t, err.Error(), "127.0.0.1")
}

func TestValidateMcpServerTargetResolveFailureWithAllowListRejects(t *testing.T) {
	settings := &fakeSettingReader{cfg: ipPolicySetting(true, models.McpIpPolicy_SCOPE_ALL, []string{"10.0.0.0/8"}, nil)}
	resolver := &fakeResolver{err: context.DeadlineExceeded}
	err := validateTarget(t, settings, "http://nope.invalid/mcp", true, resolver)
	require.Error(t, err)
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	assert.Contains(t, err.Error(), "cannot resolve")
}

func TestValidateMcpServerTargetResolveFailureDenyOnlyAllows(t *testing.T) {
	settings := &fakeSettingReader{cfg: ipPolicySetting(true, models.McpIpPolicy_SCOPE_ALL, nil, []string{"127.0.0.0/8"})}
	resolver := &fakeResolver{err: context.DeadlineExceeded}
	err := validateTarget(t, settings, "http://nope.invalid/mcp", true, resolver)
	require.NoError(t, err)
}

func TestValidateMcpServerTargetPolicyDisabledAllows(t *testing.T) {
	settings := &fakeSettingReader{cfg: ipPolicySetting(false, models.McpIpPolicy_SCOPE_ALL, nil, []string{"0.0.0.0/0"})}
	err := validateTarget(t, settings, "http://127.0.0.1/mcp", true, &fakeResolver{})
	require.NoError(t, err)
}

func TestValidateMcpServerTargetScopeUserCreatedSkipsWorkspaceServer(t *testing.T) {
	settings := &fakeSettingReader{cfg: ipPolicySetting(true, models.McpIpPolicy_SCOPE_USER_CREATED, nil, []string{"0.0.0.0/0"})}
	err := validateTarget(t, settings, "http://10.0.0.1/mcp", false, &fakeResolver{})
	require.NoError(t, err)
	err = validateTarget(t, settings, "http://10.0.0.1/mcp", true, &fakeResolver{})
	require.Error(t, err)
}

func TestValidateMcpServerTargetScopeAllCoversWorkspaceServer(t *testing.T) {
	settings := &fakeSettingReader{cfg: ipPolicySetting(true, models.McpIpPolicy_SCOPE_ALL, nil, []string{"10.0.0.0/8"})}
	err := validateTarget(t, settings, "http://10.0.0.1/mcp", false, &fakeResolver{})
	require.Error(t, err)
}
