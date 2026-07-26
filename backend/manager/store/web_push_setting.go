package store

import (
	"context"
	"encoding/json"

	"github.com/pkg/errors"

	models "github.com/Ranxy/laelia/backend/generated-go/store"
)

// GetWebPushSetting returns the stored VAPID keypair. It never returns a nil
// payload: a missing row yields a zero-value config (treated as "not yet
// generated" by the boot-time initializer, which then generates and persists a
// fresh keypair).
func (s *Store) GetWebPushSetting(ctx context.Context) (*models.WebPushSetting, error) {
	setting, err := s.GetSettingV2(ctx, models.SettingName_WEB_PUSH_CONFIG)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get setting %v", models.SettingName_WEB_PUSH_CONFIG)
	}
	cfg := &models.WebPushSetting{}
	if setting == nil {
		return cfg, nil
	}
	if err := json.Unmarshal([]byte(setting.Value), cfg); err != nil {
		return nil, errors.Wrapf(err, "failed to unmarshal web push config")
	}
	return cfg, nil
}

// UpsertWebPushSetting stores the VAPID keypair.
func (s *Store) UpsertWebPushSetting(ctx context.Context, cfg *models.WebPushSetting) (*models.WebPushSetting, error) {
	value, err := json.Marshal(cfg)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to marshal web push config")
	}
	if _, err := s.UpsertSettingV2(ctx, &SetSettingMessage{
		Name:  models.SettingName_WEB_PUSH_CONFIG,
		Value: string(value),
	}); err != nil {
		return nil, errors.Wrapf(err, "failed to upsert web push config")
	}
	return cfg, nil
}
