package v1

import (
	"context"
	"log/slog"
	"strings"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

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

// settingHandler implements read/write for one setting name.
type settingHandler struct {
	// adminOnly gates GetSetting: when true, only callers holding
	// laelia.settings.get may read; otherwise any authenticated user may.
	adminOnly bool
	get       func(ctx context.Context) (*v1pb.SettingValue, error)
	update    func(ctx context.Context, value *v1pb.SettingValue) (*v1pb.SettingValue, error)
}

// settingHandlers is the registry of settings exposed through
// GetSetting/UpdateSetting. Add an entry here to expose a new setting.
func (s *SettingService) settingHandlers() map[models.SettingName]settingHandler {
	return map[models.SettingName]settingHandler{
		models.SettingName_S3_CONFIG: {
			adminOnly: true,
			get: func(ctx context.Context) (*v1pb.SettingValue, error) {
				cfg, err := s.store.GetS3ConfigSetting(ctx)
				if err != nil {
					return nil, err
				}
				cfg.SecretKey = maskSecret(cfg.SecretKey)
				return &v1pb.SettingValue{Value: &v1pb.SettingValue_S3Config{S3Config: cfg}}, nil
			},
			update: func(ctx context.Context, value *v1pb.SettingValue) (*v1pb.SettingValue, error) {
				cfg := value.GetS3Config()
				if cfg == nil {
					return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("s3_config value is required"))
				}
				// A masked secret means "leave unchanged": pull the stored
				// value so the caller doesn't have to re-enter the secret to
				// toggle a boolean.
				if strings.HasPrefix(cfg.SecretKey, secretMaskPrefix) {
					stored, err := s.store.GetS3ConfigSetting(ctx)
					if err != nil {
						return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get s3 config"))
					}
					cfg.SecretKey = stored.SecretKey
				}
				if _, err := s.store.UpsertS3ConfigSetting(ctx, cfg); err != nil {
					return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to upsert s3 config"))
				}
				if s.s3clientManager != nil {
					s.s3clientManager.Invalidate()
				}
				cfg.SecretKey = maskSecret(cfg.SecretKey)
				return &v1pb.SettingValue{Value: &v1pb.SettingValue_S3Config{S3Config: cfg}}, nil
			},
		},
		models.SettingName_LLM_AGENT_CONFIG: {
			adminOnly: false,
			get: func(ctx context.Context) (*v1pb.SettingValue, error) {
				cfg, err := s.store.GetLlmAgentConfigSetting(ctx)
				if err != nil {
					return nil, err
				}
				return &v1pb.SettingValue{Value: &v1pb.SettingValue_LlmAgentConfig{LlmAgentConfig: cfg}}, nil
			},
			update: func(ctx context.Context, value *v1pb.SettingValue) (*v1pb.SettingValue, error) {
				cfg := value.GetLlmAgentConfig()
				if cfg == nil {
					return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("llm_agent_config value is required"))
				}
				if _, err := s.store.UpsertLlmAgentConfigSetting(ctx, cfg); err != nil {
					return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to update llm agent config"))
				}
				return &v1pb.SettingValue{Value: &v1pb.SettingValue_LlmAgentConfig{LlmAgentConfig: cfg}}, nil
			},
		},
		models.SettingName_USER_MCP_CONFIG: {
			adminOnly: false,
			get: func(ctx context.Context) (*v1pb.SettingValue, error) {
				cfg, err := s.store.GetUserMcpConfigSetting(ctx)
				if err != nil {
					return nil, err
				}
				return &v1pb.SettingValue{Value: &v1pb.SettingValue_UserMcpConfig{UserMcpConfig: cfg}}, nil
			},
			update: func(ctx context.Context, value *v1pb.SettingValue) (*v1pb.SettingValue, error) {
				cfg := value.GetUserMcpConfig()
				if cfg == nil {
					return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("user_mcp_config value is required"))
				}
				if _, err := mcp.ParsePolicy(cfg.GetMcpIpPolicy()); err != nil {
					return nil, connect.NewError(connect.CodeInvalidArgument, err)
				}
				if _, err := s.store.UpsertUserMcpConfigSetting(ctx, cfg); err != nil {
					return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to update user mcp config"))
				}
				return &v1pb.SettingValue{Value: &v1pb.SettingValue_UserMcpConfig{UserMcpConfig: cfg}}, nil
			},
		},
		models.SettingName_WORKSPACE_PROFILE: {
			adminOnly: true,
			get: func(ctx context.Context) (*v1pb.SettingValue, error) {
				setting, err := s.store.GetWorkspaceGeneralSetting(ctx)
				if err != nil {
					return nil, err
				}
				return &v1pb.SettingValue{Value: &v1pb.SettingValue_WorkspaceProfile{WorkspaceProfile: setting}}, nil
			},
			update: func(ctx context.Context, value *v1pb.SettingValue) (*v1pb.SettingValue, error) {
				setting := value.GetWorkspaceProfile()
				if setting == nil {
					return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("workspace_profile value is required"))
				}
				normalizeWorkspaceGeneralSetting(setting)
				if err := validateWorkspaceGeneralSetting(setting); err != nil {
					return nil, connect.NewError(connect.CodeInvalidArgument, err)
				}
				if err := s.store.UpsertWorkspaceGeneralSetting(ctx, setting); err != nil {
					return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to update workspace general setting"))
				}
				return &v1pb.SettingValue{Value: &v1pb.SettingValue_WorkspaceProfile{WorkspaceProfile: setting}}, nil
			},
		},
		models.SettingName_PASSWORD_RESTRICTION: {
			adminOnly: true,
			get: func(ctx context.Context) (*v1pb.SettingValue, error) {
				setting, err := s.store.GetPasswordRestrictionSetting(ctx)
				if err != nil {
					return nil, err
				}
				return &v1pb.SettingValue{Value: &v1pb.SettingValue_PasswordRestriction{PasswordRestriction: setting}}, nil
			},
			update: func(ctx context.Context, value *v1pb.SettingValue) (*v1pb.SettingValue, error) {
				setting := value.GetPasswordRestriction()
				if setting == nil {
					return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("password_restriction value is required"))
				}
				if err := s.store.UpsertPasswordRestrictionSetting(ctx, setting); err != nil {
					return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to update password restriction setting"))
				}
				return &v1pb.SettingValue{Value: &v1pb.SettingValue_PasswordRestriction{PasswordRestriction: setting}}, nil
			},
		},
	}
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
	handler, ok := s.settingHandlers()[name]
	if !ok {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("unknown setting %q", req.Msg.GetName()))
	}
	if handler.adminOnly {
		if err := s.requireSettingsGet(ctx); err != nil {
			return nil, err
		}
	}
	value, err := handler.get(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to get setting %q", req.Msg.GetName()))
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
	handler, ok := s.settingHandlers()[name]
	if !ok {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("unknown setting %q", in.GetName()))
	}
	value, err := handler.update(ctx, in.GetValue())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&v1pb.Setting{Name: formatSettingName(name), Value: value}), nil
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

func (s *SettingService) GetS3Config(ctx context.Context, _ *connect.Request[v1pb.GetS3ConfigRequest]) (*connect.Response[v1pb.GetS3ConfigResponse], error) {
	cfg, err := s.store.GetS3ConfigSetting(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get s3 config"))
	}
	cfg.SecretKey = maskSecret(cfg.SecretKey)
	return connect.NewResponse(&v1pb.GetS3ConfigResponse{Config: cfg}), nil
}

func (s *SettingService) UpdateS3Config(ctx context.Context, req *connect.Request[v1pb.UpdateS3ConfigRequest]) (*connect.Response[v1pb.UpdateS3ConfigResponse], error) {
	in := req.Msg.GetConfig()
	if in == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("config is required"))
	}

	// A masked secret means "leave unchanged": pull the stored value so the
	// caller doesn't have to re-enter the secret to toggle a boolean.
	if strings.HasPrefix(in.SecretKey, secretMaskPrefix) {
		stored, err := s.store.GetS3ConfigSetting(ctx)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get s3 config"))
		}
		in.SecretKey = stored.SecretKey
	}

	if _, err := s.store.UpsertS3ConfigSetting(ctx, in); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to upsert s3 config"))
	}
	if s.s3clientManager != nil {
		s.s3clientManager.Invalidate()
	}

	in.SecretKey = maskSecret(in.SecretKey)
	return connect.NewResponse(&v1pb.UpdateS3ConfigResponse{Config: in}), nil
}

// GetLlmAgentConfig reads the workspace LLM agent configuration. It is
// handler-gated (no permission annotation): the agent create/edit forms read
// the toggle for any authenticated user.
func (s *SettingService) GetLlmAgentConfig(ctx context.Context, _ *connect.Request[v1pb.GetLlmAgentConfigRequest]) (*connect.Response[v1pb.GetLlmAgentConfigResponse], error) {
	cfg, err := s.store.GetLlmAgentConfigSetting(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get llm agent config"))
	}
	return connect.NewResponse(&v1pb.GetLlmAgentConfigResponse{Config: cfg}), nil
}

// UpdateLlmAgentConfig updates the workspace LLM agent configuration. Gated by
// the IAM interceptor on laelia.settings.update (admin).
func (s *SettingService) UpdateLlmAgentConfig(ctx context.Context, req *connect.Request[v1pb.UpdateLlmAgentConfigRequest]) (*connect.Response[v1pb.UpdateLlmAgentConfigResponse], error) {
	in := req.Msg.GetConfig()
	if in == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("config is required"))
	}
	if _, err := s.store.UpsertLlmAgentConfigSetting(ctx, in); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to update llm agent config"))
	}
	return connect.NewResponse(&v1pb.UpdateLlmAgentConfigResponse{Config: in}), nil
}

// GetUserMcpConfig reads whether users may configure personal MCP servers. It
// is handler-gated (no permission annotation): the personal MCP settings page
// and agent profile render the toggle state for any authenticated user.
func (s *SettingService) GetUserMcpConfig(ctx context.Context, _ *connect.Request[v1pb.GetUserMcpConfigRequest]) (*connect.Response[v1pb.GetUserMcpConfigResponse], error) {
	cfg, err := s.store.GetUserMcpConfigSetting(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get user mcp config"))
	}
	return connect.NewResponse(&v1pb.GetUserMcpConfigResponse{Config: cfg}), nil
}

// UpdateUserMcpConfig updates whether users may configure personal MCP
// servers. Gated by the IAM interceptor on laelia.settings.update (admin).
func (s *SettingService) UpdateUserMcpConfig(ctx context.Context, req *connect.Request[v1pb.UpdateUserMcpConfigRequest]) (*connect.Response[v1pb.UpdateUserMcpConfigResponse], error) {
	in := req.Msg.GetConfig()
	if in == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("config is required"))
	}
	if _, err := mcp.ParsePolicy(in.GetMcpIpPolicy()); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if _, err := s.store.UpsertUserMcpConfigSetting(ctx, in); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to update user mcp config"))
	}
	return connect.NewResponse(&v1pb.UpdateUserMcpConfigResponse{Config: in}), nil
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

// GetWorkspaceGeneralSetting reads the workspace general setting (signup
// policy, email suffix restriction, ...). Gated by the IAM interceptor on
// laelia.settings.get (admin).
func (s *SettingService) GetWorkspaceGeneralSetting(ctx context.Context, _ *connect.Request[v1pb.GetWorkspaceGeneralSettingRequest]) (*connect.Response[v1pb.GetWorkspaceGeneralSettingResponse], error) {
	setting, err := s.store.GetWorkspaceGeneralSetting(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get workspace general setting"))
	}
	return connect.NewResponse(&v1pb.GetWorkspaceGeneralSettingResponse{Setting: setting}), nil
}

// UpdateWorkspaceGeneralSetting updates the workspace general setting. Gated
// by the IAM interceptor on laelia.settings.update (admin).
func (s *SettingService) UpdateWorkspaceGeneralSetting(ctx context.Context, req *connect.Request[v1pb.UpdateWorkspaceGeneralSettingRequest]) (*connect.Response[v1pb.UpdateWorkspaceGeneralSettingResponse], error) {
	in := req.Msg.GetSetting()
	if in == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("setting is required"))
	}
	normalizeWorkspaceGeneralSetting(in)
	if err := validateWorkspaceGeneralSetting(in); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if err := s.store.UpsertWorkspaceGeneralSetting(ctx, in); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to update workspace general setting"))
	}
	return connect.NewResponse(&v1pb.UpdateWorkspaceGeneralSettingResponse{Setting: in}), nil
}

// GetWorkspaceInfo returns the workspace signup policy for the
// unauthenticated sign-in/sign-up pages. No auth required.
func (s *SettingService) GetWorkspaceInfo(ctx context.Context, _ *connect.Request[v1pb.GetWorkspaceInfoRequest]) (*connect.Response[v1pb.GetWorkspaceInfoResponse], error) {
	setting, err := s.store.GetWorkspaceGeneralSetting(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get workspace general setting"))
	}
	return connect.NewResponse(&v1pb.GetWorkspaceInfoResponse{
		DisallowSignup:        setting.DisallowSignup,
		EnforceIdentityDomain: setting.EnforceIdentityDomain,
		Domains:               setting.Domains,
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
