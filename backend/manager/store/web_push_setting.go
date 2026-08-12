package store

import (
	"context"

	models "github.com/Ranxy/laelia/backend/generated-go/store"
)

// GetWebPushSetting returns the stored VAPID keypair. It never returns a nil
// payload: a missing row yields the registered default (treated as "not yet
// generated" by the boot-time initializer, which then generates and persists a
// fresh keypair).
func (s *Store) GetWebPushSetting(ctx context.Context) (*models.WebPushSetting, error) {
	return getSettingPayload[*models.WebPushSetting](ctx, s, models.SettingName_WEB_PUSH_CONFIG)
}

// UpsertWebPushSetting stores the VAPID keypair.
func (s *Store) UpsertWebPushSetting(ctx context.Context, cfg *models.WebPushSetting) (*models.WebPushSetting, error) {
	return upsertSettingValue(ctx, s, models.SettingName_WEB_PUSH_CONFIG, cfg)
}
