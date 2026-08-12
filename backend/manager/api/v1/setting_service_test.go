package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	models "github.com/Ranxy/laelia/backend/generated-go/store"
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

// TestSettingHandlersRegistry guards the dispatch table: every setting exposed
// through GetSetting/UpdateSetting is registered, and the member-readable
// settings (llm_agent_config, user_mcp_config) are not admin-gated while the
// rest are.
func TestSettingHandlersRegistry(t *testing.T) {
	s := &SettingService{}
	handlers := s.settingHandlers()

	require.Contains(t, handlers, models.SettingName_S3_CONFIG)
	require.Contains(t, handlers, models.SettingName_LLM_AGENT_CONFIG)
	require.Contains(t, handlers, models.SettingName_USER_MCP_CONFIG)
	require.Contains(t, handlers, models.SettingName_WORKSPACE_PROFILE)
	require.Contains(t, handlers, models.SettingName_PASSWORD_RESTRICTION)

	assert.True(t, handlers[models.SettingName_S3_CONFIG].adminOnly)
	assert.False(t, handlers[models.SettingName_LLM_AGENT_CONFIG].adminOnly)
	assert.False(t, handlers[models.SettingName_USER_MCP_CONFIG].adminOnly)
	assert.True(t, handlers[models.SettingName_WORKSPACE_PROFILE].adminOnly)
	assert.True(t, handlers[models.SettingName_PASSWORD_RESTRICTION].adminOnly)
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
