package v1

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"connectrpc.com/connect"
	pkgerrors "github.com/pkg/errors"

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
		return models.SettingName_SETTING_NAME_UNSPECIFIED, pkgerrors.Errorf("invalid setting name %q", name)
	}
	key := strings.TrimPrefix(name, settingNamePrefix)
	if key == "" || key != strings.ToLower(key) {
		return models.SettingName_SETTING_NAME_UNSPECIFIED, pkgerrors.Errorf("invalid setting name %q", name)
	}
	value, ok := models.SettingName_value[strings.ToUpper(key)]
	if !ok {
		return models.SettingName_SETTING_NAME_UNSPECIFIED, pkgerrors.Errorf("unknown setting %q", name)
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
		return nil, connect.NewError(connect.CodeInvalidArgument, pkgerrors.Errorf("unknown setting %q", req.Msg.GetName()))
	}
	if meta.adminOnly {
		if err := s.requireSettingsGet(ctx); err != nil {
			return nil, err
		}
	}
	payload, err := s.store.GetSettingValue(ctx, name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, pkgerrors.Wrapf(err, "failed to get setting %q", req.Msg.GetName()))
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
		return nil, connect.NewError(connect.CodeInvalidArgument, pkgerrors.New("setting is required"))
	}
	name, err := parseSettingName(in.GetName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if _, ok := exposedSettings[name]; !ok {
		return nil, connect.NewError(connect.CodeInvalidArgument, pkgerrors.Errorf("unknown setting %q", in.GetName()))
	}
	// The update mask is required (AIP-134): only the listed paths are written
	// to the stored payload, so callers never round-trip unrelated fields and
	// concurrent updates cannot clobber each other.
	if req.Msg.GetUpdateMask() == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, pkgerrors.New("update mask is required"))
	}
	after, err := s.updateSettingValue(ctx, name, req)
	if err != nil {
		return nil, err
	}
	value, err := convertStoreToSettingValue(name, after)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&v1pb.Setting{Name: formatSettingName(name), Value: value}), nil
}

// updateSettingValue dispatches to the per-setting merge path. Each path
// merges the mask-listed fields of the request payload into the stored value
// inside a single locked transaction and returns the resulting payload.
func (s *SettingService) updateSettingValue(ctx context.Context, name models.SettingName, req *connect.Request[v1pb.UpdateSettingRequest]) (proto.Message, error) {
	switch name {
	case models.SettingName_S3_CONFIG:
		return s.updateS3Config(ctx, req)
	case models.SettingName_LLM_AGENT_CONFIG:
		return s.updateLlmAgentConfig(ctx, req)
	case models.SettingName_USER_MCP_CONFIG:
		return s.updateUserMcpConfig(ctx, req)
	case models.SettingName_WORKSPACE_PROFILE:
		return s.updateWorkspaceProfile(ctx, req)
	case models.SettingName_PASSWORD_RESTRICTION:
		return s.updatePasswordRestriction(ctx, req)
	case models.SettingName_SMTP_CONFIG:
		return s.updateSMTPConfig(ctx, req)
	default:
		return nil, connect.NewError(connect.CodeInvalidArgument, pkgerrors.Errorf("unsupported setting %v", name))
	}
}

func (s *SettingService) updateWorkspaceProfile(ctx context.Context, req *connect.Request[v1pb.UpdateSettingRequest]) (proto.Message, error) {
	payload := req.Msg.GetSetting().GetValue().GetWorkspaceProfile()
	if payload == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, pkgerrors.New("workspace_profile value is required"))
	}
	after, err := s.store.UpdateSettingValueAtomic(ctx, models.SettingName_WORKSPACE_PROFILE, func(current proto.Message) (proto.Message, error) {
		setting, err := typedPayload[*models.WorkspaceProfileSetting](current, models.SettingName_WORKSPACE_PROFILE)
		if err != nil {
			return nil, err
		}
		if err := mergeWorkspaceProfilePaths(req.Msg.GetUpdateMask().GetPaths(), payload, setting); err != nil {
			return nil, err
		}
		normalizeWorkspaceGeneralSetting(setting)
		if err := validateWorkspaceGeneralSetting(setting); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		return setting, nil
	})
	if err != nil {
		return nil, asSettingUpdateError(err)
	}
	return after, nil
}

func (s *SettingService) updateSMTPConfig(ctx context.Context, req *connect.Request[v1pb.UpdateSettingRequest]) (proto.Message, error) {
	payload := req.Msg.GetSetting().GetValue().GetSmtpConfig()
	if payload == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, pkgerrors.New("smtp_config value is required"))
	}
	// A masked password means "leave unchanged": pull the stored value so the
	// caller doesn't have to re-enter the password to tweak the host.
	if strings.HasPrefix(payload.GetPassword(), secretMaskPrefix) {
		stored, err := s.store.GetSMTPSetting(ctx)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, pkgerrors.Wrap(err, "failed to get SMTP setting"))
		}
		payload.Password = stored.Password
	}
	after, err := s.store.UpdateSettingValueAtomic(ctx, models.SettingName_SMTP_CONFIG, func(current proto.Message) (proto.Message, error) {
		cfg, err := typedPayload[*models.SMTPSetting](current, models.SettingName_SMTP_CONFIG)
		if err != nil {
			return nil, err
		}
		if err := mergeSMTPConfigPaths(req.Msg.GetUpdateMask().GetPaths(), payload, cfg); err != nil {
			return nil, err
		}
		if err := validateSMTPSetting(cfg); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		return cfg, nil
	})
	if err != nil {
		return nil, asSettingUpdateError(err)
	}
	return after, nil
}

func (s *SettingService) updateS3Config(ctx context.Context, req *connect.Request[v1pb.UpdateSettingRequest]) (proto.Message, error) {
	payload := req.Msg.GetSetting().GetValue().GetS3Config()
	if payload == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, pkgerrors.New("s3_config value is required"))
	}
	// A masked secret means "leave unchanged": pull the stored value so the
	// caller doesn't have to re-enter the secret to toggle a boolean.
	if strings.HasPrefix(payload.GetSecretKey(), secretMaskPrefix) {
		stored, err := s.store.GetS3ConfigSetting(ctx)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, pkgerrors.Wrap(err, "failed to get s3 config"))
		}
		payload.SecretKey = stored.SecretKey
	}
	after, err := s.store.UpdateSettingValueAtomic(ctx, models.SettingName_S3_CONFIG, func(current proto.Message) (proto.Message, error) {
		cfg, err := typedPayload[*models.S3ConfigSetting](current, models.SettingName_S3_CONFIG)
		if err != nil {
			return nil, err
		}
		return cfg, mergeS3ConfigPaths(req.Msg.GetUpdateMask().GetPaths(), payload, cfg)
	})
	if err != nil {
		return nil, asSettingUpdateError(err)
	}
	if s.s3clientManager != nil {
		s.s3clientManager.Invalidate()
	}
	return after, nil
}

func (s *SettingService) updateLlmAgentConfig(ctx context.Context, req *connect.Request[v1pb.UpdateSettingRequest]) (proto.Message, error) {
	payload := req.Msg.GetSetting().GetValue().GetLlmAgentConfig()
	if payload == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, pkgerrors.New("llm_agent_config value is required"))
	}
	after, err := s.store.UpdateSettingValueAtomic(ctx, models.SettingName_LLM_AGENT_CONFIG, func(current proto.Message) (proto.Message, error) {
		cfg, err := typedPayload[*models.LlmAgentConfigSetting](current, models.SettingName_LLM_AGENT_CONFIG)
		if err != nil {
			return nil, err
		}
		return cfg, mergeLlmAgentConfigPaths(req.Msg.GetUpdateMask().GetPaths(), payload, cfg)
	})
	if err != nil {
		return nil, asSettingUpdateError(err)
	}
	return after, nil
}

func (s *SettingService) updateUserMcpConfig(ctx context.Context, req *connect.Request[v1pb.UpdateSettingRequest]) (proto.Message, error) {
	payload := req.Msg.GetSetting().GetValue().GetUserMcpConfig()
	if payload == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, pkgerrors.New("user_mcp_config value is required"))
	}
	after, err := s.store.UpdateSettingValueAtomic(ctx, models.SettingName_USER_MCP_CONFIG, func(current proto.Message) (proto.Message, error) {
		cfg, err := typedPayload[*models.UserMcpConfigSetting](current, models.SettingName_USER_MCP_CONFIG)
		if err != nil {
			return nil, err
		}
		if err := mergeUserMcpConfigPaths(req.Msg.GetUpdateMask().GetPaths(), payload, cfg); err != nil {
			return nil, err
		}
		if _, err := mcp.ParsePolicy(cfg.GetMcpIpPolicy()); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		return cfg, nil
	})
	if err != nil {
		return nil, asSettingUpdateError(err)
	}
	return after, nil
}

func (s *SettingService) updatePasswordRestriction(ctx context.Context, req *connect.Request[v1pb.UpdateSettingRequest]) (proto.Message, error) {
	payload := req.Msg.GetSetting().GetValue().GetPasswordRestriction()
	if payload == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, pkgerrors.New("password_restriction value is required"))
	}
	after, err := s.store.UpdateSettingValueAtomic(ctx, models.SettingName_PASSWORD_RESTRICTION, func(current proto.Message) (proto.Message, error) {
		cfg, err := typedPayload[*models.PasswordRestrictionSetting](current, models.SettingName_PASSWORD_RESTRICTION)
		if err != nil {
			return nil, err
		}
		return cfg, mergePasswordRestrictionPaths(req.Msg.GetUpdateMask().GetPaths(), payload, cfg)
	})
	if err != nil {
		return nil, asSettingUpdateError(err)
	}
	return after, nil
}

// asSettingUpdateError unwraps a connect error returned from a merge/validate
// callback; any other error becomes an internal error.
func asSettingUpdateError(err error) error {
	if connectErr, ok := errors.AsType[*connect.Error](err); ok {
		return connectErr
	}
	return connect.NewError(connect.CodeInternal, pkgerrors.Wrap(err, "failed to update setting"))
}

// mergeSettingPaths validates mask paths against the setting's path prefix and
// applies each listed field via apply. An empty paths list means "update all
// fields" (AIP-134). Unknown paths are rejected.
func mergeSettingPaths(paths []string, prefix string, allPaths []string, apply func(field string) error) error {
	if len(paths) == 0 {
		paths = allPaths
	}
	for _, path := range paths {
		field, ok := strings.CutPrefix(path, prefix)
		if !ok {
			return connect.NewError(connect.CodeInvalidArgument, pkgerrors.Errorf("invalid update mask path %q", path))
		}
		if err := apply(field); err != nil {
			return err
		}
	}
	return nil
}

var workspaceProfilePaths = []string{
	"value.workspace_profile.external_url",
	"value.workspace_profile.disallow_signup",
	"value.workspace_profile.require_2fa",
	"value.workspace_profile.token_duration",
	"value.workspace_profile.maximum_role_expiration",
	"value.workspace_profile.domains",
	"value.workspace_profile.enforce_identity_domain",
	"value.workspace_profile.disallow_password_signin",
	"value.workspace_profile.enable_metric_collection",
	"value.workspace_profile.require_email_verification",
	"value.workspace_profile.disallow_user_create_machine",
}

func mergeWorkspaceProfilePaths(paths []string, src, dst *models.WorkspaceProfileSetting) error {
	return mergeSettingPaths(paths, "value.workspace_profile.", workspaceProfilePaths, func(field string) error {
		switch field {
		case "external_url":
			dst.ExternalUrl = src.GetExternalUrl()
		case "disallow_signup":
			dst.DisallowSignup = src.GetDisallowSignup()
		case "require_2fa":
			dst.Require_2Fa = src.GetRequire_2Fa()
		case "token_duration":
			dst.TokenDuration = src.TokenDuration
		case "maximum_role_expiration":
			dst.MaximumRoleExpiration = src.MaximumRoleExpiration
		case "domains":
			dst.Domains = src.GetDomains()
		case "enforce_identity_domain":
			dst.EnforceIdentityDomain = src.GetEnforceIdentityDomain()
		case "disallow_password_signin":
			dst.DisallowPasswordSignin = src.GetDisallowPasswordSignin()
		case "enable_metric_collection":
			dst.EnableMetricCollection = src.GetEnableMetricCollection()
		case "require_email_verification":
			dst.RequireEmailVerification = src.RequireEmailVerification
		case "disallow_user_create_machine":
			dst.DisallowUserCreateMachine = src.GetDisallowUserCreateMachine()
		default:
			return invalidMaskPath("value.workspace_profile." + field)
		}
		return nil
	})
}

var smtpConfigPaths = []string{
	"value.smtp_config.host",
	"value.smtp_config.port",
	"value.smtp_config.username",
	"value.smtp_config.password",
	"value.smtp_config.from",
	"value.smtp_config.use_tls",
}

func mergeSMTPConfigPaths(paths []string, src, dst *models.SMTPSetting) error {
	return mergeSettingPaths(paths, "value.smtp_config.", smtpConfigPaths, func(field string) error {
		switch field {
		case "host":
			dst.Host = src.GetHost()
		case "port":
			dst.Port = src.GetPort()
		case "username":
			dst.Username = src.GetUsername()
		case "password":
			dst.Password = src.GetPassword()
		case "from":
			dst.From = src.GetFrom()
		case "use_tls":
			dst.UseTls = src.GetUseTls()
		default:
			return invalidMaskPath("value.smtp_config." + field)
		}
		return nil
	})
}

var s3ConfigPaths = []string{
	"value.s3_config.endpoint",
	"value.s3_config.region",
	"value.s3_config.bucket",
	"value.s3_config.access_key",
	"value.s3_config.secret_key",
	"value.s3_config.force_path_style",
	"value.s3_config.use_ssl",
}

func mergeS3ConfigPaths(paths []string, src, dst *models.S3ConfigSetting) error {
	return mergeSettingPaths(paths, "value.s3_config.", s3ConfigPaths, func(field string) error {
		switch field {
		case "endpoint":
			dst.Endpoint = src.GetEndpoint()
		case "region":
			dst.Region = src.GetRegion()
		case "bucket":
			dst.Bucket = src.GetBucket()
		case "access_key":
			dst.AccessKey = src.GetAccessKey()
		case "secret_key":
			dst.SecretKey = src.GetSecretKey()
		case "force_path_style":
			dst.ForcePathStyle = src.GetForcePathStyle()
		case "use_ssl":
			dst.UseSsl = src.GetUseSsl()
		default:
			return invalidMaskPath("value.s3_config." + field)
		}
		return nil
	})
}

var llmAgentConfigPaths = []string{
	"value.llm_agent_config.allow_user_self_provided_keys",
}

func mergeLlmAgentConfigPaths(paths []string, src, dst *models.LlmAgentConfigSetting) error {
	return mergeSettingPaths(paths, "value.llm_agent_config.", llmAgentConfigPaths, func(field string) error {
		switch field {
		case "allow_user_self_provided_keys":
			dst.AllowUserSelfProvidedKeys = src.GetAllowUserSelfProvidedKeys()
		default:
			return invalidMaskPath("value.llm_agent_config." + field)
		}
		return nil
	})
}

var userMcpConfigPaths = []string{
	"value.user_mcp_config.allow_user_mcp_servers",
	"value.user_mcp_config.mcp_ip_policy",
}

func mergeUserMcpConfigPaths(paths []string, src, dst *models.UserMcpConfigSetting) error {
	return mergeSettingPaths(paths, "value.user_mcp_config.", userMcpConfigPaths, func(field string) error {
		switch field {
		case "allow_user_mcp_servers":
			dst.AllowUserMcpServers = src.GetAllowUserMcpServers()
		case "mcp_ip_policy":
			dst.McpIpPolicy = src.McpIpPolicy
		default:
			return invalidMaskPath("value.user_mcp_config." + field)
		}
		return nil
	})
}

var passwordRestrictionPaths = []string{
	"value.password_restriction.min_length",
	"value.password_restriction.require_number",
	"value.password_restriction.require_letter",
	"value.password_restriction.require_uppercase_letter",
	"value.password_restriction.require_special_character",
	"value.password_restriction.require_reset_password_for_first_login",
	"value.password_restriction.password_rotation",
}

func mergePasswordRestrictionPaths(paths []string, src, dst *models.PasswordRestrictionSetting) error {
	return mergeSettingPaths(paths, "value.password_restriction.", passwordRestrictionPaths, func(field string) error {
		switch field {
		case "min_length":
			dst.MinLength = src.GetMinLength()
		case "require_number":
			dst.RequireNumber = src.GetRequireNumber()
		case "require_letter":
			dst.RequireLetter = src.GetRequireLetter()
		case "require_uppercase_letter":
			dst.RequireUppercaseLetter = src.GetRequireUppercaseLetter()
		case "require_special_character":
			dst.RequireSpecialCharacter = src.GetRequireSpecialCharacter()
		case "require_reset_password_for_first_login":
			dst.RequireResetPasswordForFirstLogin = src.GetRequireResetPasswordForFirstLogin()
		case "password_rotation":
			dst.PasswordRotation = src.PasswordRotation
		default:
			return invalidMaskPath("value.password_restriction." + field)
		}
		return nil
	})
}

// typedPayload asserts the payload has the expected concrete type. The store
// payload factory registered for the setting guarantees it, so a mismatch is a
// programming error rather than a user error.
func typedPayload[T proto.Message](current proto.Message, name models.SettingName) (T, error) {
	typed, ok := current.(T)
	if !ok {
		return *new(T), pkgerrors.Errorf("unexpected payload type %T for setting %v", current, name)
	}
	return typed, nil
}

// invalidMaskPath builds the InvalidArgument error for an unknown mask path.
func invalidMaskPath(path string) error {
	return connect.NewError(connect.CodeInvalidArgument, pkgerrors.Errorf("invalid update mask path %q", path))
}

// convertStoreToSettingValue wraps a store payload into the v1 oneof, applying
// API-representation transforms (the S3 secret is masked on read-back).
func convertStoreToSettingValue(name models.SettingName, payload proto.Message) (*v1pb.SettingValue, error) {
	switch name {
	case models.SettingName_S3_CONFIG:
		cfg, ok := payload.(*models.S3ConfigSetting)
		if !ok {
			return nil, pkgerrors.Errorf("unexpected payload type %T for setting %v", payload, name)
		}
		cfg.SecretKey = maskSecret(cfg.SecretKey)
		return &v1pb.SettingValue{Value: &v1pb.SettingValue_S3Config{S3Config: cfg}}, nil
	case models.SettingName_LLM_AGENT_CONFIG:
		cfg, ok := payload.(*models.LlmAgentConfigSetting)
		if !ok {
			return nil, pkgerrors.Errorf("unexpected payload type %T for setting %v", payload, name)
		}
		return &v1pb.SettingValue{Value: &v1pb.SettingValue_LlmAgentConfig{LlmAgentConfig: cfg}}, nil
	case models.SettingName_USER_MCP_CONFIG:
		cfg, ok := payload.(*models.UserMcpConfigSetting)
		if !ok {
			return nil, pkgerrors.Errorf("unexpected payload type %T for setting %v", payload, name)
		}
		return &v1pb.SettingValue{Value: &v1pb.SettingValue_UserMcpConfig{UserMcpConfig: cfg}}, nil
	case models.SettingName_WORKSPACE_PROFILE:
		cfg, ok := payload.(*models.WorkspaceProfileSetting)
		if !ok {
			return nil, pkgerrors.Errorf("unexpected payload type %T for setting %v", payload, name)
		}
		return &v1pb.SettingValue{Value: &v1pb.SettingValue_WorkspaceProfile{WorkspaceProfile: cfg}}, nil
	case models.SettingName_PASSWORD_RESTRICTION:
		cfg, ok := payload.(*models.PasswordRestrictionSetting)
		if !ok {
			return nil, pkgerrors.Errorf("unexpected payload type %T for setting %v", payload, name)
		}
		return &v1pb.SettingValue{Value: &v1pb.SettingValue_PasswordRestriction{PasswordRestriction: cfg}}, nil
	case models.SettingName_SMTP_CONFIG:
		cfg, ok := payload.(*models.SMTPSetting)
		if !ok {
			return nil, pkgerrors.Errorf("unexpected payload type %T for setting %v", payload, name)
		}
		cfg.Password = maskSecret(cfg.Password)
		return &v1pb.SettingValue{Value: &v1pb.SettingValue_SmtpConfig{SmtpConfig: cfg}}, nil
	default:
		return nil, pkgerrors.Errorf("unsupported setting %v", name)
	}
}

// requireSettingsGet denies callers that do not hold laelia.settings.get.
func (s *SettingService) requireSettingsGet(ctx context.Context) error {
	user, _ := GetUserFromContext(ctx)
	ok, err := s.iam.CheckPermission(ctx, permission.SettingsGet, user, nil, nil)
	if err != nil {
		return connect.NewError(connect.CodeInternal, pkgerrors.Wrap(err, "failed to check permission"))
	}
	if !ok {
		return connect.NewError(connect.CodePermissionDenied, pkgerrors.New("you are not allowed to read this setting"))
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
			return nil, connect.NewError(connect.CodeInternal, pkgerrors.Wrapf(err, "failed to check setup item %q", c.id))
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
		return nil, connect.NewError(connect.CodeInternal, pkgerrors.Wrap(err, "failed to get workspace general setting"))
	}
	return connect.NewResponse(&v1pb.GetWorkspaceInfoResponse{
		DisallowSignup:            setting.DisallowSignup,
		EnforceIdentityDomain:     setting.EnforceIdentityDomain,
		Domains:                   setting.Domains,
		RequireEmailVerification:  store.RequireEmailVerification(setting),
		DisallowUserCreateMachine: setting.DisallowUserCreateMachine,
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
			return pkgerrors.Errorf("invalid domain %q", d)
		}
		if d != strings.ToLower(d) {
			return pkgerrors.Errorf("domain %q must be lowercase", d)
		}
	}
	return nil
}

// validateSMTPSetting rejects SMTP configs that could never deliver mail.
func validateSMTPSetting(cfg *models.SMTPSetting) error {
	if cfg.GetHost() == "" {
		return pkgerrors.Errorf("SMTP host is required")
	}
	if cfg.GetFrom() == "" {
		return pkgerrors.Errorf("SMTP from address is required")
	}
	if cfg.GetPort() < 0 || cfg.GetPort() > 65535 {
		return pkgerrors.Errorf("SMTP port %d is out of range", cfg.GetPort())
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
