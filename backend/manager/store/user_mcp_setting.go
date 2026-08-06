package store

import (
	"context"
	"encoding/json"

	"github.com/pkg/errors"

	models "github.com/Ranxy/laelia/backend/generated-go/store"
)

// GetUserMcpConfigSetting returns whether users may configure personal MCP
// servers. It never returns a nil payload: a missing row yields the default
// (personal MCP servers enabled).
func (s *Store) GetUserMcpConfigSetting(ctx context.Context) (*models.UserMcpConfigSetting, error) {
	setting, err := s.GetSettingV2(ctx, models.SettingName_USER_MCP_CONFIG)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get setting %v", models.SettingName_USER_MCP_CONFIG)
	}
	cfg := &models.UserMcpConfigSetting{AllowUserMcpServers: true}
	if setting == nil {
		return cfg, nil
	}
	if err := json.Unmarshal([]byte(setting.Value), cfg); err != nil {
		return nil, errors.Wrapf(err, "failed to unmarshal user mcp config")
	}
	return cfg, nil
}

// UpsertUserMcpConfigSetting stores whether users may configure personal MCP
// servers.
func (s *Store) UpsertUserMcpConfigSetting(ctx context.Context, cfg *models.UserMcpConfigSetting) (*models.UserMcpConfigSetting, error) {
	value, err := json.Marshal(cfg)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to marshal user mcp config")
	}
	if _, err := s.UpsertSettingV2(ctx, &SetSettingMessage{
		Name:  models.SettingName_USER_MCP_CONFIG,
		Value: string(value),
	}); err != nil {
		return nil, errors.Wrapf(err, "failed to upsert user mcp config")
	}
	return cfg, nil
}
