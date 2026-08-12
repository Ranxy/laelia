package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/pkg/errors"

	models "github.com/Ranxy/laelia/backend/generated-go/store"
)

// userMcpConfigCacheTTL bounds how long the parsed USER_MCP_CONFIG row is
// reused without a database read. The MCP gateway checks the setting on every
// tool call, so the cache avoids a query per call; a policy change takes
// effect within this window.
const userMcpConfigCacheTTL = 30 * time.Second

// GetUserMcpConfigSetting returns whether users may configure personal MCP
// servers plus the MCP target IP policy. It never returns a nil payload: a
// missing row yields the registered default (personal MCP servers enabled,
// policy off).
func (s *Store) GetUserMcpConfigSetting(ctx context.Context) (*models.UserMcpConfigSetting, error) {
	s.userMcpConfigMu.Lock()
	setting := s.userMcpConfigSetting
	fresh := setting != nil && time.Since(s.userMcpConfigCachedAt) < userMcpConfigCacheTTL
	s.userMcpConfigMu.Unlock()
	if !fresh {
		var err error
		setting, err = s.GetSettingV2(ctx, models.SettingName_USER_MCP_CONFIG)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to get setting %v", models.SettingName_USER_MCP_CONFIG)
		}
		s.userMcpConfigMu.Lock()
		s.userMcpConfigSetting = setting
		s.userMcpConfigCachedAt = time.Now()
		s.userMcpConfigMu.Unlock()
	}

	return unmarshalSettingValue(setting, func() *models.UserMcpConfigSetting {
		return &models.UserMcpConfigSetting{AllowUserMcpServers: true}
	})
}

// UpsertUserMcpConfigSetting stores whether users may configure personal MCP
// servers and refreshes the short-TTL cache so the new value applies
// immediately.
func (s *Store) UpsertUserMcpConfigSetting(ctx context.Context, cfg *models.UserMcpConfigSetting) (*models.UserMcpConfigSetting, error) {
	value, err := json.Marshal(cfg)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to marshal user mcp config")
	}
	setting, err := s.UpsertSettingV2(ctx, &SetSettingMessage{
		Name:  models.SettingName_USER_MCP_CONFIG,
		Value: string(value),
	})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to upsert user mcp config")
	}
	s.userMcpConfigMu.Lock()
	s.userMcpConfigSetting = setting
	s.userMcpConfigCachedAt = time.Now()
	s.userMcpConfigMu.Unlock()
	return cfg, nil
}
