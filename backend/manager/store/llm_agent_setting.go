package store

import (
	"context"

	models "github.com/Ranxy/laelia/backend/generated-go/store"
)

// GetLlmAgentConfigSetting returns the workspace LLM agent configuration. It
// never returns a nil payload: a missing row yields the registered default
// (self-provided api keys enabled).
func (s *Store) GetLlmAgentConfigSetting(ctx context.Context) (*models.LlmAgentConfigSetting, error) {
	return getSettingPayload[*models.LlmAgentConfigSetting](ctx, s, models.SettingName_LLM_AGENT_CONFIG)
}

// UpsertLlmAgentConfigSetting stores the workspace LLM agent configuration.
func (s *Store) UpsertLlmAgentConfigSetting(ctx context.Context, cfg *models.LlmAgentConfigSetting) (*models.LlmAgentConfigSetting, error) {
	return upsertSettingValue(ctx, s, models.SettingName_LLM_AGENT_CONFIG, cfg)
}
