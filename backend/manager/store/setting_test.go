package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	models "github.com/Ranxy/laelia/backend/generated-go/store"
)

// TestUnmarshalSettingValue guards the typed decode shared by every setting
// accessor: a nil row yields the default payload, a stored row is decoded into
// it, and malformed JSON is an error.
func TestUnmarshalSettingValue(t *testing.T) {
	defaults := func() *models.S3ConfigSetting { return &models.S3ConfigSetting{UseSsl: true} }

	// Missing row -> default.
	got, err := unmarshalSettingValue(nil, defaults)
	require.NoError(t, err)
	assert.Equal(t, true, got.UseSsl)

	// Stored row -> decoded payload.
	got, err = unmarshalSettingValue(&SettingMessage{
		Name:  models.SettingName_S3_CONFIG,
		Value: `{"endpoint":"https://s3.example.com","bucket":"b"}`,
	}, defaults)
	require.NoError(t, err)
	assert.Equal(t, "https://s3.example.com", got.Endpoint)
	assert.Equal(t, "b", got.Bucket)

	// Malformed JSON -> error.
	_, err = unmarshalSettingValue(&SettingMessage{
		Name:  models.SettingName_S3_CONFIG,
		Value: `{not json`,
	}, defaults)
	assert.Error(t, err)
}

// TestSettingPayloadDefaults guards the type registry: every registered
// default factory yields a non-nil, typed payload, and the API-exposed
// settings all have an entry.
func TestSettingPayloadDefaults(t *testing.T) {
	for name, factory := range settingPayloadDefaults {
		require.NotNil(t, factory(), "default factory for %v", name)
	}

	require.Contains(t, settingPayloadDefaults, models.SettingName_S3_CONFIG)
	require.Contains(t, settingPayloadDefaults, models.SettingName_LLM_AGENT_CONFIG)
	require.Contains(t, settingPayloadDefaults, models.SettingName_USER_MCP_CONFIG)
	require.Contains(t, settingPayloadDefaults, models.SettingName_WORKSPACE_PROFILE)
	require.Contains(t, settingPayloadDefaults, models.SettingName_PASSWORD_RESTRICTION)
}

// TestRequireEmailVerification guards the nil-default semantics: an unset
// field (nil) means verification is enabled by default.
func TestRequireEmailVerification(t *testing.T) {
	assert.True(t, RequireEmailVerification(nil))
	assert.True(t, RequireEmailVerification(&models.WorkspaceProfileSetting{}))
	assert.True(t, RequireEmailVerification(&models.WorkspaceProfileSetting{RequireEmailVerification: boolPtr(true)}))
	assert.False(t, RequireEmailVerification(&models.WorkspaceProfileSetting{RequireEmailVerification: boolPtr(false)}))
}

func boolPtr(b bool) *bool { return &b }
