package store

import (
	"context"
	"encoding/json"

	"github.com/pkg/errors"

	models "github.com/Ranxy/laelia/backend/generated-go/store"
)

// GetLlmAgentConfigSetting returns the workspace LLM agent configuration. It
// never returns a nil payload: a missing row yields the default (self-provided
// api keys enabled).
func (s *Store) GetLlmAgentConfigSetting(ctx context.Context) (*models.LlmAgentConfigSetting, error) {
	setting, err := s.GetSettingV2(ctx, models.SettingName_LLM_AGENT_CONFIG)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get setting %v", models.SettingName_LLM_AGENT_CONFIG)
	}
	cfg := &models.LlmAgentConfigSetting{AllowUserSelfProvidedKeys: true}
	if setting == nil {
		return cfg, nil
	}
	if err := json.Unmarshal([]byte(setting.Value), cfg); err != nil {
		return nil, errors.Wrapf(err, "failed to unmarshal llm agent config")
	}
	return cfg, nil
}

// UpsertLlmAgentConfigSetting stores the workspace LLM agent configuration.
func (s *Store) UpsertLlmAgentConfigSetting(ctx context.Context, cfg *models.LlmAgentConfigSetting) (*models.LlmAgentConfigSetting, error) {
	value, err := json.Marshal(cfg)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to marshal llm agent config")
	}
	if _, err := s.UpsertSettingV2(ctx, &SetSettingMessage{
		Name:  models.SettingName_LLM_AGENT_CONFIG,
		Value: string(value),
	}); err != nil {
		return nil, errors.Wrapf(err, "failed to upsert llm agent config")
	}
	return cfg, nil
}
