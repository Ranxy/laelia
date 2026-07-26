package server

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"
	"github.com/pkg/errors"

	"github.com/Ranxy/laelia/backend/common"
	models "github.com/Ranxy/laelia/backend/generated-go/store"
	"github.com/Ranxy/laelia/backend/manager/component/webpush"
	"github.com/Ranxy/laelia/backend/manager/store"
)

func (s *Server) initializeSetting(ctx context.Context) error {
	// secretLength is the length for the secret used to sign the JWT auto token.
	const secretLength = 32

	// initial branding
	_, firstTimeOnboarding, err := s.store.CreateSettingIfNotExistV2(ctx, &store.SettingMessage{
		Name:  models.SettingName_BRANDING_LOGO,
		Value: "",
	})
	if err != nil {
		return err
	}

	// initial JWT token
	secret, err := common.RandomString(secretLength)
	if err != nil {
		return errors.Wrap(err, "failed to generate random JWT secret")
	}
	if _, _, err := s.store.CreateSettingIfNotExistV2(ctx, &store.SettingMessage{
		Name:  models.SettingName_AUTH_SECRET,
		Value: secret,
	}); err != nil {
		return err
	}

	// initial workspace
	if _, _, err := s.store.CreateSettingIfNotExistV2(ctx, &store.SettingMessage{
		Name:  models.SettingName_WORKSPACE_ID,
		Value: uuid.New().String(),
	}); err != nil {
		return err
	}

	// Init password validation
	passwordSettingValue, err := json.Marshal(&models.PasswordRestrictionSetting{
		MinLength:                         8,
		RequireNumber:                     false,
		RequireLetter:                     false,
		RequireUppercaseLetter:            false,
		RequireSpecialCharacter:           false,
		RequireResetPasswordForFirstLogin: false,
	})
	if err != nil {
		return errors.Wrap(err, "failed to marshal initial password validation setting")
	}
	if _, _, err := s.store.CreateSettingIfNotExistV2(ctx, &store.SettingMessage{
		Name:  models.SettingName_PASSWORD_RESTRICTION,
		Value: string(passwordSettingValue),
	}); err != nil {
		return err
	}

	// initial workspace profile setting
	workspaceProfileSetting, err := s.store.GetSettingV2(ctx, models.SettingName_WORKSPACE_PROFILE)
	if err != nil {
		return err
	}

	workspaceProfilePayload := &models.WorkspaceProfileSetting{
		ExternalUrl:            s.profile.ExternalURL,
		EnableMetricCollection: true, // Default to enabled for new installations
	}
	if workspaceProfileSetting != nil {
		workspaceProfilePayload = new(models.WorkspaceProfileSetting)
		if err := json.Unmarshal([]byte(workspaceProfileSetting.Value), workspaceProfilePayload); err != nil {
			return err
		}
		if s.profile.ExternalURL != "" {
			workspaceProfilePayload.ExternalUrl = s.profile.ExternalURL
		}
	}

	bytes, err := json.Marshal(workspaceProfilePayload)
	if err != nil {
		return err
	}

	if _, err := s.store.UpsertSettingV2(ctx, &store.SetSettingMessage{
		Name:  models.SettingName_WORKSPACE_PROFILE,
		Value: string(bytes),
	}); err != nil {
		return err
	}

	if firstTimeOnboarding {
		// Only grant workspace member role to allUsers at the first time.
		if _, err := s.store.PatchWorkspaceIamPolicy(ctx, &store.PatchIamPolicyMessage{
			Member: common.AllUsers,
			Roles: []string{
				common.FormatRole(common.WorkspaceMember),
			},
		}); err != nil {
			return err
		}
	}

	// Seed a default (empty) S3 config so GetS3ConfigSetting never returns a
	// missing row; upload/download report "s3 not configured" until an admin
	// fills in endpoint+bucket.
	s3ConfigValue, err := json.Marshal(&models.S3ConfigSetting{UseSsl: true})
	if err != nil {
		return errors.Wrap(err, "failed to marshal initial s3 config setting")
	}
	if _, _, err := s.store.CreateSettingIfNotExistV2(ctx, &store.SettingMessage{
		Name:  models.SettingName_S3_CONFIG,
		Value: string(s3ConfigValue),
	}); err != nil {
		return err
	}

	// Web Push VAPID keypair: auto-generate on first boot and persist, so a
	// self-hosted deployment needs no env config. Rotating the keys later
	// invalidates every existing push subscription, so once generated the values
	// must stay stable — only generate when the row is absent or empty.
	return s.initWebPushSetting(ctx)
}

// initWebPushSetting ensures a VAPID keypair exists in the setting table. When
// the public or private key is missing it generates a fresh keypair and stores
// it; otherwise the existing keypair is left untouched. The subject defaults to
// the workspace ExternalURL when it is an http(s) URL, else a stable mailto:.
func (s *Server) initWebPushSetting(ctx context.Context) error {
	cfg, err := s.store.GetWebPushSetting(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to read web push setting")
	}
	if cfg.GetPublicKey() != "" && cfg.GetPrivateKey() != "" {
		return nil
	}

	privateKey, publicKey, err := webpush.GenerateKeys()
	if err != nil {
		return errors.Wrap(err, "failed to generate VAPID keys")
	}
	subject := s.profile.ExternalURL
	if !strings.HasPrefix(subject, "http://") && !strings.HasPrefix(subject, "https://") {
		subject = "mailto:laelia@localhost"
	}
	if _, err := s.store.UpsertWebPushSetting(ctx, &models.WebPushSetting{
		PublicKey:  publicKey,
		PrivateKey: privateKey,
		Subject:    subject,
	}); err != nil {
		return errors.Wrap(err, "failed to persist VAPID keys")
	}
	return nil
}
