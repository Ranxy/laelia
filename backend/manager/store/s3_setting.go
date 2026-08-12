package store

import (
	"context"

	models "github.com/Ranxy/laelia/backend/generated-go/store"
)

// GetS3ConfigSetting returns the S3 connection config. It never returns a nil
// payload: a missing row yields the registered default (treated as
// "unconfigured" by the S3 client component).
func (s *Store) GetS3ConfigSetting(ctx context.Context) (*models.S3ConfigSetting, error) {
	return getSettingPayload[*models.S3ConfigSetting](ctx, s, models.SettingName_S3_CONFIG)
}

// UpsertS3ConfigSetting stores the S3 connection config.
func (s *Store) UpsertS3ConfigSetting(ctx context.Context, cfg *models.S3ConfigSetting) (*models.S3ConfigSetting, error) {
	return upsertSettingValue(ctx, s, models.SettingName_S3_CONFIG, cfg)
}
