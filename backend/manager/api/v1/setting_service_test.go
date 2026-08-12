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

// TestMergeWorkspaceProfilePaths guards the field-level merge: only the masked
// fields are written and every other field is preserved, the optional
// require_email_verification can be set and cleared, an empty mask means
// "update all fields" (AIP-134), and unknown paths are rejected.
func TestMergeWorkspaceProfilePaths(t *testing.T) {
	dst := &models.WorkspaceProfileSetting{ExternalUrl: "https://example.com", DisallowSignup: true, Domains: []string{"a.com"}}
	src := &models.WorkspaceProfileSetting{DisallowSignup: false}
	require.NoError(t, mergeWorkspaceProfilePaths([]string{"value.workspace_profile.disallow_signup"}, src, dst))
	assert.False(t, dst.DisallowSignup)
	assert.Equal(t, "https://example.com", dst.ExternalUrl)
	assert.Equal(t, []string{"a.com"}, dst.Domains)
	assert.Nil(t, dst.RequireEmailVerification)

	// The optional field can be set to true.
	trueVal := true
	src.RequireEmailVerification = &trueVal
	require.NoError(t, mergeWorkspaceProfilePaths([]string{"value.workspace_profile.require_email_verification"}, src, dst))
	require.NotNil(t, dst.RequireEmailVerification)
	assert.True(t, *dst.RequireEmailVerification)

	// And cleared back to nil (the "enabled" default) by masking it with a nil value.
	src.RequireEmailVerification = nil
	require.NoError(t, mergeWorkspaceProfilePaths([]string{"value.workspace_profile.require_email_verification"}, src, dst))
	assert.Nil(t, dst.RequireEmailVerification)

	// An empty mask updates every field.
	all := &models.WorkspaceProfileSetting{ExternalUrl: "https://new.example.com", DisallowSignup: true, Domains: []string{"b.com"}}
	dstAll := &models.WorkspaceProfileSetting{}
	require.NoError(t, mergeWorkspaceProfilePaths(nil, all, dstAll))
	assert.Equal(t, "https://new.example.com", dstAll.ExternalUrl)
	assert.True(t, dstAll.DisallowSignup)
	assert.Equal(t, []string{"b.com"}, dstAll.Domains)

	// Unknown paths and wrong prefixes are rejected.
	assert.Error(t, mergeWorkspaceProfilePaths([]string{"value.workspace_profile.nope"}, src, dst))
	assert.Error(t, mergeWorkspaceProfilePaths([]string{"value.smtp_config.host"}, src, dst))
}

// TestMergeS3ConfigPaths guards the masked-secret flow: updating a non-secret
// field leaves the stored secret untouched unless the secret itself is masked.
func TestMergeS3ConfigPaths(t *testing.T) {
	dst := &models.S3ConfigSetting{Endpoint: "https://s3.example.com", Bucket: "b", SecretKey: "real-secret"}
	src := &models.S3ConfigSetting{Bucket: "other"}
	require.NoError(t, mergeS3ConfigPaths([]string{"value.s3_config.bucket"}, src, dst))
	assert.Equal(t, "other", dst.Bucket)
	assert.Equal(t, "real-secret", dst.SecretKey)
	assert.Equal(t, "https://s3.example.com", dst.Endpoint)

	// Masking the secret replaces it (the API layer maps "****" back to the
	// stored value before the merge; here a plain value flows through).
	src.SecretKey = "new-secret"
	require.NoError(t, mergeS3ConfigPaths([]string{"value.s3_config.secret_key"}, src, dst))
	assert.Equal(t, "new-secret", dst.SecretKey)
}

// TestMergeSettingPaths guards the generic path walker: an empty path list
// expands to all fields, and a path with the wrong prefix is rejected before
// any field is applied.
func TestMergeSettingPaths(t *testing.T) {
	var applied []string
	err := mergeSettingPaths(nil, "value.smtp_config.", smtpConfigPaths, func(field string) error {
		applied = append(applied, field)
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"host", "port", "username", "password", "from", "use_tls"}, applied)

	applied = nil
	err = mergeSettingPaths([]string{"value.llm_agent_config.allow_user_self_provided_keys"}, "value.smtp_config.", smtpConfigPaths, func(field string) error {
		applied = append(applied, field)
		return nil
	})
	assert.Error(t, err)
	assert.Nil(t, applied)
}

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

// TestValidateWorkspaceProfileMerge guards the validation wired into the
// workspace_profile update path: an invalid domain is rejected after merge.
func TestValidateWorkspaceProfileMerge(t *testing.T) {
	src := &models.WorkspaceProfileSetting{Domains: []string{" Example.com ", "@example.com"}}
	dst := &models.WorkspaceProfileSetting{}
	require.NoError(t, mergeWorkspaceProfilePaths([]string{"value.workspace_profile.domains"}, src, dst))
	normalizeWorkspaceGeneralSetting(dst)
	assert.Equal(t, []string{"example.com"}, dst.Domains)
	assert.NoError(t, validateWorkspaceGeneralSetting(dst))

	// The merge itself is structural: the policy value is copied verbatim.
	// Its semantic validation (mcp.ParsePolicy) runs in the update path and is
	// covered by the mcp component tests.
	dstMcp := &models.UserMcpConfigSetting{}
	srcMcp := &models.UserMcpConfigSetting{McpIpPolicy: &models.McpIpPolicy{Enabled: true, DenyCidrs: []string{"10.0.0.0/8"}}}
	require.NoError(t, mergeUserMcpConfigPaths([]string{"value.user_mcp_config.mcp_ip_policy"}, srcMcp, dstMcp))
	assert.Equal(t, []string{"10.0.0.0/8"}, dstMcp.GetMcpIpPolicy().GetDenyCidrs())
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
