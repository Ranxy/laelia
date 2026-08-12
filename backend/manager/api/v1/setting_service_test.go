package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	models "github.com/Ranxy/laelia/backend/generated-go/store"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

// TestParseSettingName guards the "settings/{setting}" resource-name parser
// used by GetSetting/UpdateSetting: a well-formed name maps to the store
// SettingName enum, while malformed or unknown names are rejected.
func TestParseSettingName(t *testing.T) {
	cases := []struct {
		name string
		want models.SettingName
	}{
		{"settings/s3_config", models.SettingName_S3_CONFIG},
		{"settings/llm_agent_config", models.SettingName_LLM_AGENT_CONFIG},
		{"settings/user_mcp_config", models.SettingName_USER_MCP_CONFIG},
		{"settings/workspace_profile", models.SettingName_WORKSPACE_PROFILE},
		{"settings/password_restriction", models.SettingName_PASSWORD_RESTRICTION},
	}
	for _, c := range cases {
		got, err := parseSettingName(c.name)
		require.NoError(t, err, "parse %q", c.name)
		assert.Equal(t, c.want, got, "parse %q", c.name)
	}

	bad := []string{
		"",
		"s3_config",                // missing settings/ prefix
		"settings/",                // empty setting segment
		"settings/unknown_setting", // not in the enum
		"settings/S3_CONFIG",       // uppercase is not canonical
		"settings/s3-config",       // hyphen is not accepted
		"settings/s3_config/extra", // trailing segment
	}
	for _, name := range bad {
		_, err := parseSettingName(name)
		assert.Error(t, err, "expected error for %q", name)
	}
}

// TestFormatSettingName guards the inverse mapping: every exposed setting
// round-trips through parse/format.
func TestFormatSettingName(t *testing.T) {
	assert.Equal(t, "settings/s3_config", formatSettingName(models.SettingName_S3_CONFIG))
	assert.Equal(t, "settings/workspace_profile", formatSettingName(models.SettingName_WORKSPACE_PROFILE))
}

// TestExposedSettings guards the exposure table: every setting exposed through
// GetSetting/UpdateSetting is registered, and the member-readable settings
// (llm_agent_config, user_mcp_config) are not admin-gated while the rest are.
func TestExposedSettings(t *testing.T) {
	require.Contains(t, exposedSettings, models.SettingName_S3_CONFIG)
	require.Contains(t, exposedSettings, models.SettingName_LLM_AGENT_CONFIG)
	require.Contains(t, exposedSettings, models.SettingName_USER_MCP_CONFIG)
	require.Contains(t, exposedSettings, models.SettingName_WORKSPACE_PROFILE)
	require.Contains(t, exposedSettings, models.SettingName_PASSWORD_RESTRICTION)

	assert.True(t, exposedSettings[models.SettingName_S3_CONFIG].adminOnly)
	assert.False(t, exposedSettings[models.SettingName_LLM_AGENT_CONFIG].adminOnly)
	assert.False(t, exposedSettings[models.SettingName_USER_MCP_CONFIG].adminOnly)
	assert.True(t, exposedSettings[models.SettingName_WORKSPACE_PROFILE].adminOnly)
	assert.True(t, exposedSettings[models.SettingName_PASSWORD_RESTRICTION].adminOnly)
}

// TestConvertV1ToStoreSetting guards the request-side conversion: the oneof
// branch must match the setting name, and the returned payload has the typed
// store shape.
func TestConvertV1ToStoreSetting(t *testing.T) {
	cases := []struct {
		name  models.SettingName
		value func() *v1pb.SettingValue
	}{
		{models.SettingName_S3_CONFIG, func() *v1pb.SettingValue {
			return &v1pb.SettingValue{Value: &v1pb.SettingValue_S3Config{S3Config: &models.S3ConfigSetting{Endpoint: "e"}}}
		}},
		{models.SettingName_LLM_AGENT_CONFIG, func() *v1pb.SettingValue {
			return &v1pb.SettingValue{Value: &v1pb.SettingValue_LlmAgentConfig{LlmAgentConfig: &models.LlmAgentConfigSetting{}}}
		}},
		{models.SettingName_USER_MCP_CONFIG, func() *v1pb.SettingValue {
			return &v1pb.SettingValue{Value: &v1pb.SettingValue_UserMcpConfig{UserMcpConfig: &models.UserMcpConfigSetting{}}}
		}},
		{models.SettingName_WORKSPACE_PROFILE, func() *v1pb.SettingValue {
			return &v1pb.SettingValue{Value: &v1pb.SettingValue_WorkspaceProfile{WorkspaceProfile: &models.WorkspaceProfileSetting{}}}
		}},
		{models.SettingName_PASSWORD_RESTRICTION, func() *v1pb.SettingValue {
			return &v1pb.SettingValue{Value: &v1pb.SettingValue_PasswordRestriction{PasswordRestriction: &models.PasswordRestrictionSetting{}}}
		}},
	}
	for _, c := range cases {
		t.Run(c.name.String(), func(t *testing.T) {
			payload, err := convertV1ToStoreSetting(c.name, c.value())
			require.NoError(t, err)
			assert.IsType(t, payloadTypeFor(c.name), payload)
		})
	}

	// A mismatched oneof branch is rejected.
	_, err := convertV1ToStoreSetting(models.SettingName_S3_CONFIG, &v1pb.SettingValue{
		Value: &v1pb.SettingValue_LlmAgentConfig{LlmAgentConfig: &models.LlmAgentConfigSetting{}},
	})
	assert.Error(t, err)
	// An empty oneof is rejected.
	_, err = convertV1ToStoreSetting(models.SettingName_S3_CONFIG, &v1pb.SettingValue{})
	assert.Error(t, err)
}

// TestConvertStoreToSettingValue guards the response-side conversion: the
// oneof branch matches the setting name, and the S3 secret is masked on
// read-back.
func TestConvertStoreToSettingValue(t *testing.T) {
	value, err := convertStoreToSettingValue(models.SettingName_S3_CONFIG, &models.S3ConfigSetting{SecretKey: "abc12345"})
	require.NoError(t, err)
	assert.Equal(t, secretMaskPrefix+"2345", value.GetS3Config().SecretKey)

	value, err = convertStoreToSettingValue(models.SettingName_WORKSPACE_PROFILE, &models.WorkspaceProfileSetting{Domains: []string{"a.com"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"a.com"}, value.GetWorkspaceProfile().Domains)

	// A payload of the wrong type for the name is rejected, not panicked.
	_, err = convertStoreToSettingValue(models.SettingName_S3_CONFIG, &models.WorkspaceProfileSetting{})
	assert.Error(t, err)
	_, err = convertStoreToSettingValue(models.SettingName_SETTING_NAME_UNSPECIFIED, &models.S3ConfigSetting{})
	assert.Error(t, err)
}

// payloadTypeFor mirrors the store payload type registered for a setting name.
func payloadTypeFor(name models.SettingName) any {
	switch name {
	case models.SettingName_S3_CONFIG:
		return &models.S3ConfigSetting{}
	case models.SettingName_LLM_AGENT_CONFIG:
		return &models.LlmAgentConfigSetting{}
	case models.SettingName_USER_MCP_CONFIG:
		return &models.UserMcpConfigSetting{}
	case models.SettingName_WORKSPACE_PROFILE:
		return &models.WorkspaceProfileSetting{}
	case models.SettingName_PASSWORD_RESTRICTION:
		return &models.PasswordRestrictionSetting{}
	default:
		return nil
	}
}

// TestPrepareSettingUpdate guards the per-setting validation/normalization:
// workspace_profile domains are normalized in place, and a malformed MCP
// policy is rejected.
func TestPrepareSettingUpdate(t *testing.T) {
	s := &SettingService{}
	profile := &models.WorkspaceProfileSetting{Domains: []string{" Example.com ", "@example.com"}}
	after, err := s.prepareSettingUpdate(t.Context(), models.SettingName_WORKSPACE_PROFILE, profile)
	require.NoError(t, err)
	assert.Nil(t, after)
	assert.Equal(t, []string{"example.com"}, profile.Domains)

	_, err = s.prepareSettingUpdate(t.Context(), models.SettingName_USER_MCP_CONFIG, &models.UserMcpConfigSetting{
		McpIpPolicy: &models.McpIpPolicy{Enabled: true, DenyCidrs: []string{"not-a-cidr"}},
	})
	assert.Error(t, err)

	after, err = s.prepareSettingUpdate(t.Context(), models.SettingName_LLM_AGENT_CONFIG, &models.LlmAgentConfigSetting{})
	require.NoError(t, err)
	assert.Nil(t, after)
}

// TestMaskSecret guards the read-back masking contract: an empty secret stays
// empty (so the frontend can tell "not yet set" from "set but hidden"), a short
// secret collapses to the bare prefix, and a longer secret keeps only the last
// four characters behind the prefix.
func TestMaskSecret(t *testing.T) {
	assert.Equal(t, "", maskSecret(""))
	assert.Equal(t, secretMaskPrefix, maskSecret("abcd"))
	assert.Equal(t, secretMaskPrefix+"5678", maskSecret("12345678"))
}

// TestS3Configured guards the setup-checklist predicate: both endpoint and
// bucket must be set for S3 to count as configured.
func TestS3Configured(t *testing.T) {
	assert.False(t, s3Configured(&models.S3ConfigSetting{}))
	assert.False(t, s3Configured(&models.S3ConfigSetting{Endpoint: "https://s3.example.com"}))
	assert.False(t, s3Configured(&models.S3ConfigSetting{Bucket: "b"}))
	assert.True(t, s3Configured(&models.S3ConfigSetting{Endpoint: "https://s3.example.com", Bucket: "b"}))
}

// TestNormalizeWorkspaceGeneralSetting guards the domain-list cleaning: trim,
// strip a leading "@", lowercase, and dedupe, dropping empty entries.
func TestNormalizeWorkspaceGeneralSetting(t *testing.T) {
	setting := &models.WorkspaceProfileSetting{Domains: []string{
		" Example.com ", "@example.com", "EXAMPLE.COM", "", "sub.example.com",
	}}
	normalizeWorkspaceGeneralSetting(setting)
	assert.Equal(t, []string{"example.com", "sub.example.com"}, setting.Domains)
}

// TestValidateWorkspaceGeneralSetting guards the domain validation: entries
// containing "@", "/", whitespace, or uppercase letters are rejected.
func TestValidateWorkspaceGeneralSetting(t *testing.T) {
	assert.NoError(t, validateWorkspaceGeneralSetting(&models.WorkspaceProfileSetting{Domains: []string{"example.com"}}))
	assert.Error(t, validateWorkspaceGeneralSetting(&models.WorkspaceProfileSetting{Domains: []string{"a@b.com"}}))
	assert.Error(t, validateWorkspaceGeneralSetting(&models.WorkspaceProfileSetting{Domains: []string{"a/b.com"}}))
	assert.Error(t, validateWorkspaceGeneralSetting(&models.WorkspaceProfileSetting{Domains: []string{"a b.com"}}))
	assert.Error(t, validateWorkspaceGeneralSetting(&models.WorkspaceProfileSetting{Domains: []string{"Example.com"}}))
}
