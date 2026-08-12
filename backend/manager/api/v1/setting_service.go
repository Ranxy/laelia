package v1

import (
	"context"
	"log/slog"
	"strings"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

	"google.golang.org/protobuf/proto"

	"github.com/Ranxy/laelia/backend/common/log"
	"github.com/Ranxy/laelia/backend/common/permission"
	models "github.com/Ranxy/laelia/backend/generated-go/store"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/laelia/backend/manager/component/iam"
	"github.com/Ranxy/laelia/backend/manager/component/mcp"
	"github.com/Ranxy/laelia/backend/manager/component/s3client"
	"github.com/Ranxy/laelia/backend/manager/config"
	"github.com/Ranxy/laelia/backend/manager/store"
)

// secretMaskPrefix is the sentinel prefix returned on Get and accepted (meaning
// "unchanged") on Update.
const secretMaskPrefix = "****"

// settingNamePrefix is the resource-name prefix of a setting.
const settingNamePrefix = "settings/"

// SettingService exposes workspace-level configuration. GetSetting/UpdateSetting
// are the unified resource-based accessors; the legacy per-setting RPCs are
// kept for compatibility.
type SettingService struct {
	v1connect.UnimplementedSettingServiceHandler
	store           *store.Store
	s3clientManager *s3client.Client
	profile         *config.Profile
	iam             *iam.Manager
}

func NewSettingService(s *store.Store, s3clientManager *s3client.Client, profile *config.Profile, iamManager *iam.Manager) *SettingService {
	return &SettingService{store: s, s3clientManager: s3clientManager, profile: profile, iam: iamManager}
}

// settingMeta describes one setting exposed through GetSetting/UpdateSetting.
type settingMeta struct {
	// adminOnly gates GetSetting: when true, only callers holding
	// laelia.settings.get may read; otherwise any authenticated user may.
	adminOnly bool
}

// exposedSettings is the registry of settings exposed through
// GetSetting/UpdateSetting. Add an entry here to expose a new setting; the
// typed payload conversion lives in convertV1ToStoreSetting and
// convertStoreToSettingValue.
var exposedSettings = map[models.SettingName]settingMeta{
	models.SettingName_S3_CONFIG:            {adminOnly: true},
	models.SettingName_LLM_AGENT_CONFIG:     {},
	models.SettingName_USER_MCP_CONFIG:      {},
	models.SettingName_WORKSPACE_PROFILE:    {adminOnly: true},
	models.SettingName_PASSWORD_RESTRICTION: {adminOnly: true},
	models.SettingName_SMTP_CONFIG:          {adminOnly: true},
}

// parseSettingName converts a resource name ("settings/s3_config") to the
// store SettingName enum.
func parseSettingName(name string) (models.SettingName, error) {
	if !strings.HasPrefix(name, settingNamePrefix) {
		return models.SettingName_SETTING_NAME_UNSPECIFIED, errors.Errorf("invalid setting name %q", name)
	}
	key := strings.TrimPrefix(name, settingNamePrefix)
	if key == "" || key != strings.ToLower(key) {
		return models.SettingName_SETTING_NAME_UNSPECIFIED, errors.Errorf("invalid setting name %q", name)
	}
	value, ok := models.SettingName_value[strings.ToUpper(key)]
	if !ok {
		return models.SettingName_SETTING_NAME_UNSPECIFIED, errors.Errorf("unknown setting %q", name)
	}
	return models.SettingName(value), nil
}

// formatSettingName converts a store SettingName enum to a resource name.
func formatSettingName(name models.SettingName) string {
	return settingNamePrefix + strings.ToLower(name.String())
}

// GetSetting reads one workspace setting by resource name. It is handler-gated
// (no permission annotation): llm_agent_config and user_mcp_config are
// readable by any authenticated user, all other settings require
// laelia.settings.get (admin).
func (s *SettingService) GetSetting(ctx context.Context, req *connect.Request[v1pb.GetSettingRequest]) (*connect.Response[v1pb.Setting], error) {
	name, err := parseSettingName(req.Msg.GetName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	meta, ok := exposedSettings[name]
	if !ok {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("unknown setting %q", req.Msg.GetName()))
	}
	if meta.adminOnly {
		if err := s.requireSettingsGet(ctx); err != nil {
			return nil, err
		}
	}
	payload, err := s.store.GetSettingValue(ctx, name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to get setting %q", req.Msg.GetName()))
	}
	value, err := convertStoreToSettingValue(name, payload)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&v1pb.Setting{Name: formatSettingName(name), Value: value}), nil
}

// UpdateSetting writes one workspace setting. Gated by the IAM interceptor on
// laelia.settings.update (admin).
func (s *SettingService) UpdateSetting(ctx context.Context, req *connect.Request[v1pb.UpdateSettingRequest]) (*connect.Response[v1pb.Setting], error) {
	in := req.Msg.GetSetting()
	if in == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("setting is required"))
	}
	name, err := parseSettingName(in.GetName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if _, ok := exposedSettings[name]; !ok {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("unknown setting %q", in.GetName()))
	}
	payload, err := convertV1ToStoreSetting(name, in.GetValue())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	after, err := s.prepareSettingUpdate(ctx, name, payload)
	if err != nil {
		return nil, err
	}
	if err := s.store.UpsertSettingValue(ctx, name, payload); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to update setting %q", in.GetName()))
	}
	if after != nil {
		after()
	}
	value, err := convertStoreToSettingValue(name, payload)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&v1pb.Setting{Name: formatSettingName(name), Value: value}), nil
}

// convertV1ToStoreSetting extracts the typed store payload from a v1 oneof.
func convertV1ToStoreSetting(name models.SettingName, value *v1pb.SettingValue) (proto.Message, error) {
	switch name {
	case models.SettingName_S3_CONFIG:
		if cfg := value.GetS3Config(); cfg != nil {
			return cfg, nil
		}
	case models.SettingName_LLM_AGENT_CONFIG:
		if cfg := value.GetLlmAgentConfig(); cfg != nil {
			return cfg, nil
		}
	case models.SettingName_USER_MCP_CONFIG:
		if cfg := value.GetUserMcpConfig(); cfg != nil {
			return cfg, nil
		}
	case models.SettingName_WORKSPACE_PROFILE:
		if cfg := value.GetWorkspaceProfile(); cfg != nil {
			return cfg, nil
		}
	case models.SettingName_PASSWORD_RESTRICTION:
		if cfg := value.GetPasswordRestriction(); cfg != nil {
			return cfg, nil
		}
	case models.SettingName_SMTP_CONFIG:
		if cfg := value.GetSmtpConfig(); cfg != nil {
			return cfg, nil
		}
	default:
	}
	return nil, errors.Errorf("%s value is required", strings.ToLower(name.String()))
}

// convertStoreToSettingValue wraps a store payload into the v1 oneof, applying
// API-representation transforms (the S3 secret is masked on read-back).
func convertStoreToSettingValue(name models.SettingName, payload proto.Message) (*v1pb.SettingValue, error) {
	switch name {
	case models.SettingName_S3_CONFIG:
		cfg, ok := payload.(*models.S3ConfigSetting)
		if !ok {
			return nil, errors.Errorf("unexpected payload type %T for setting %v", payload, name)
		}
		cfg.SecretKey = maskSecret(cfg.SecretKey)
		return &v1pb.SettingValue{Value: &v1pb.SettingValue_S3Config{S3Config: cfg}}, nil
	case models.SettingName_LLM_AGENT_CONFIG:
		cfg, ok := payload.(*models.LlmAgentConfigSetting)
		if !ok {
			return nil, errors.Errorf("unexpected payload type %T for setting %v", payload, name)
		}
		return &v1pb.SettingValue{Value: &v1pb.SettingValue_LlmAgentConfig{LlmAgentConfig: cfg}}, nil
	case models.SettingName_USER_MCP_CONFIG:
		cfg, ok := payload.(*models.UserMcpConfigSetting)
		if !ok {
			return nil, errors.Errorf("unexpected payload type %T for setting %v", payload, name)
		}
		return &v1pb.SettingValue{Value: &v1pb.SettingValue_UserMcpConfig{UserMcpConfig: cfg}}, nil
	case models.SettingName_WORKSPACE_PROFILE:
		cfg, ok := payload.(*models.WorkspaceProfileSetting)
		if !ok {
			return nil, errors.Errorf("unexpected payload type %T for setting %v", payload, name)
		}
		return &v1pb.SettingValue{Value: &v1pb.SettingValue_WorkspaceProfile{WorkspaceProfile: cfg}}, nil
	case models.SettingName_PASSWORD_RESTRICTION:
		cfg, ok := payload.(*models.PasswordRestrictionSetting)
		if !ok {
			return nil, errors.Errorf("unexpected payload type %T for setting %v", payload, name)
		}
		return &v1pb.SettingValue{Value: &v1pb.SettingValue_PasswordRestriction{PasswordRestriction: cfg}}, nil
	case models.SettingName_SMTP_CONFIG:
		cfg, ok := payload.(*models.SMTPSetting)
		if !ok {
			return nil, errors.Errorf("unexpected payload type %T for setting %v", payload, name)
		}
		cfg.Password = maskSecret(cfg.Password)
		return &v1pb.SettingValue{Value: &v1pb.SettingValue_SmtpConfig{SmtpConfig: cfg}}, nil
	default:
		return nil, errors.Errorf("unsupported setting %v", name)
	}
}

// prepareSettingUpdate validates and normalizes a request payload before the
// upsert, and returns an optional callback to run after the write succeeds
// (e.g. cache invalidation). Only settings with extra semantics are handled;
// the rest fall through as a no-op.
func (s *SettingService) prepareSettingUpdate(ctx context.Context, name models.SettingName, payload proto.Message) (func(), error) {
	switch name {
	case models.SettingName_S3_CONFIG:
		cfg, ok := payload.(*models.S3ConfigSetting)
		if !ok {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("unexpected payload type %T for setting %v", payload, name))
		}
		// A masked secret means "leave unchanged": pull the stored value so the
		// caller doesn't have to re-enter the secret to toggle a boolean.
		if strings.HasPrefix(cfg.SecretKey, secretMaskPrefix) {
			stored, err := s.store.GetS3ConfigSetting(ctx)
			if err != nil {
				return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get s3 config"))
			}
			cfg.SecretKey = stored.SecretKey
		}
		return func() {
			if s.s3clientManager != nil {
				s.s3clientManager.Invalidate()
			}
		}, nil
	case models.SettingName_USER_MCP_CONFIG:
		cfg, ok := payload.(*models.UserMcpConfigSetting)
		if !ok {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("unexpected payload type %T for setting %v", payload, name))
		}
		if _, err := mcp.ParsePolicy(cfg.GetMcpIpPolicy()); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
	case models.SettingName_WORKSPACE_PROFILE:
		setting, ok := payload.(*models.WorkspaceProfileSetting)
		if !ok {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("unexpected payload type %T for setting %v", payload, name))
		}
		normalizeWorkspaceGeneralSetting(setting)
		if err := validateWorkspaceGeneralSetting(setting); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
	case models.SettingName_SMTP_CONFIG:
		cfg, ok := payload.(*models.SMTPSetting)
		if !ok {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("unexpected payload type %T for setting %v", payload, name))
		}
		// A masked password means "leave unchanged": pull the stored value so
		// the caller doesn't have to re-enter the password to tweak the host.
		if strings.HasPrefix(cfg.Password, secretMaskPrefix) {
			stored, err := s.store.GetSMTPSetting(ctx)
			if err != nil {
				return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get SMTP setting"))
			}
			cfg.Password = stored.Password
		}
		if err := validateSMTPSetting(cfg); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
	default:
	}
	return nil, nil
}

// requireSettingsGet denies callers that do not hold laelia.settings.get.
func (s *SettingService) requireSettingsGet(ctx context.Context) error {
	user, _ := GetUserFromContext(ctx)
	ok, err := s.iam.CheckPermission(ctx, permission.SettingsGet, user, nil, nil)
	if err != nil {
		return connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to check permission"))
	}
	if !ok {
		return connect.NewError(connect.CodePermissionDenied, errors.New("you are not allowed to read this setting"))
	}
	return nil
}

// setupCheck reports whether one required-config item is fully configured.
type setupCheck func(ctx context.Context) (bool, error)

// setupChecks is the registry of required-config items the admin onboarding
// overlay surfaces. Add an entry here (plus a predicate) to extend the overlay;
// the frontend mirrors each id with its presentation (title/description/route).
func (s *SettingService) setupChecks() []struct {
	id    string
	check setupCheck
} {
	return []struct {
		id    string
		check setupCheck
	}{
		{"s3", s.checkS3Configured},
	}
}

// checkS3Configured reports whether S3 is fully usable: both endpoint and
// bucket must be set. This is stricter than the s3client "both empty" sentinel
// (component/s3client), which only catches the completely-unset case; for a
// "you still need to act" checklist a half-filled config must still count as
// unconfigured.
func (s *SettingService) checkS3Configured(ctx context.Context) (bool, error) {
	cfg, err := s.store.GetS3ConfigSetting(ctx)
	if err != nil {
		return false, err
	}
	return s3Configured(cfg), nil
}

// s3Configured is the pure predicate behind checkS3Configured, extracted so the
// "both fields required" contract can be unit-tested without a database.
func s3Configured(cfg *models.S3ConfigSetting) bool {
	return cfg.Endpoint != "" && cfg.Bucket != ""
}

func (s *SettingService) GetSetupStatus(ctx context.Context, _ *connect.Request[v1pb.GetSetupStatusRequest]) (*connect.Response[v1pb.GetSetupStatusResponse], error) {
	items := make([]*v1pb.SetupItem, 0, len(s.setupChecks()))
	for _, c := range s.setupChecks() {
		ok, err := c.check(ctx)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to check setup item %q", c.id))
		}
		items = append(items, &v1pb.SetupItem{Id: c.id, Configured: ok})
	}
	return connect.NewResponse(&v1pb.GetSetupStatusResponse{Items: items}), nil
}

func (s *SettingService) GetDebugConfig(_ context.Context, _ *connect.Request[v1pb.GetDebugConfigRequest]) (*connect.Response[v1pb.GetDebugConfigResponse], error) {
	enabled := s.profile.RuntimeDebug.Load()
	return connect.NewResponse(&v1pb.GetDebugConfigResponse{Enabled: enabled}), nil
}

func (s *SettingService) UpdateDebugConfig(_ context.Context, req *connect.Request[v1pb.UpdateDebugConfigRequest]) (*connect.Response[v1pb.UpdateDebugConfigResponse], error) {
	enabled := req.Msg.GetEnabled()
	s.profile.RuntimeDebug.Store(enabled)

	if enabled {
		log.LogLevel.Set(slog.LevelDebug)
	} else {
		log.LogLevel.Set(slog.LevelInfo)
	}

	return connect.NewResponse(&v1pb.UpdateDebugConfigResponse{Enabled: enabled}), nil
}

// GetWorkspaceInfo returns the workspace signup policy for the
// unauthenticated sign-in/sign-up pages. No auth required.
func (s *SettingService) GetWorkspaceInfo(ctx context.Context, _ *connect.Request[v1pb.GetWorkspaceInfoRequest]) (*connect.Response[v1pb.GetWorkspaceInfoResponse], error) {
	setting, err := s.store.GetWorkspaceGeneralSetting(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get workspace general setting"))
	}
	return connect.NewResponse(&v1pb.GetWorkspaceInfoResponse{
		DisallowSignup:           setting.DisallowSignup,
		EnforceIdentityDomain:    setting.EnforceIdentityDomain,
		Domains:                  setting.Domains,
		RequireEmailVerification: store.RequireEmailVerification(setting),
	}), nil
}

// normalizeWorkspaceGeneralSetting cleans the domain list in place: trims
// whitespace, strips a leading "@", lowercases, and dedupes.
func normalizeWorkspaceGeneralSetting(setting *models.WorkspaceProfileSetting) {
	seen := make(map[string]struct{}, len(setting.Domains))
	domains := setting.Domains[:0]
	for _, d := range setting.Domains {
		d = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(d), "@"))
		if d == "" {
			continue
		}
		if _, ok := seen[d]; ok {
			continue
		}
		seen[d] = struct{}{}
		domains = append(domains, d)
	}
	setting.Domains = domains
}

// validateWorkspaceGeneralSetting rejects malformed domain entries.
func validateWorkspaceGeneralSetting(setting *models.WorkspaceProfileSetting) error {
	for _, d := range setting.Domains {
		if strings.ContainsAny(d, "@/ \t") {
			return errors.Errorf("invalid domain %q", d)
		}
		if d != strings.ToLower(d) {
			return errors.Errorf("domain %q must be lowercase", d)
		}
	}
	return nil
}

// validateSMTPSetting rejects SMTP configs that could never deliver mail.
func validateSMTPSetting(cfg *models.SMTPSetting) error {
	if cfg.GetHost() == "" {
		return errors.Errorf("SMTP host is required")
	}
	if cfg.GetFrom() == "" {
		return errors.Errorf("SMTP from address is required")
	}
	if cfg.GetPort() < 0 || cfg.GetPort() > 65535 {
		return errors.Errorf("SMTP port %d is out of range", cfg.GetPort())
	}
	return nil
}

// maskSecret returns a masked form of the secret for read-back. An empty secret
// stays empty so the frontend can tell "not yet set" from "set but hidden".
func maskSecret(secret string) string {
	if secret == "" {
		return ""
	}
	if len(secret) <= 4 {
		return secretMaskPrefix
	}
	return secretMaskPrefix + secret[len(secret)-4:]
}
