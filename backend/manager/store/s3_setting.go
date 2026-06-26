package store

import (
	"context"
	"encoding/json"

	"github.com/pkg/errors"

	models "github.com/Ranxy/laelia/backend/generated-go/store"
)

// GetS3ConfigSetting returns the S3 connection config. It never returns a nil
// payload: a missing row yields a zero-value config (treated as "unconfigured"
// by the S3 client component).
func (s *Store) GetS3ConfigSetting(ctx context.Context) (*models.S3ConfigSetting, error) {
	setting, err := s.GetSettingV2(ctx, models.SettingName_S3_CONFIG)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get setting %v", models.SettingName_S3_CONFIG)
	}
	cfg := &models.S3ConfigSetting{UseSsl: true}
	if setting == nil {
		return cfg, nil
	}
	if err := json.Unmarshal([]byte(setting.Value), cfg); err != nil {
		return nil, errors.Wrapf(err, "failed to unmarshal s3 config")
	}
	return cfg, nil
}

// UpsertS3ConfigSetting stores the S3 connection config.
func (s *Store) UpsertS3ConfigSetting(ctx context.Context, cfg *models.S3ConfigSetting) (*models.S3ConfigSetting, error) {
	value, err := json.Marshal(cfg)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to marshal s3 config")
	}
	if _, err := s.UpsertSettingV2(ctx, &SetSettingMessage{
		Name:  models.SettingName_S3_CONFIG,
		Value: string(value),
	}); err != nil {
		return nil, errors.Wrapf(err, "failed to upsert s3 config")
	}
	return cfg, nil
}
