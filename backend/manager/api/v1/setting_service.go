package v1

import (
	"context"
	"log/slog"
	"strings"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

	"github.com/Ranxy/laelia/backend/common/log"
	models "github.com/Ranxy/laelia/backend/generated-go/store"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/laelia/backend/manager/component/mcp"
	"github.com/Ranxy/laelia/backend/manager/component/s3client"
	"github.com/Ranxy/laelia/backend/manager/config"
	"github.com/Ranxy/laelia/backend/manager/store"
)

// secretMaskPrefix is the sentinel prefix returned on Get and accepted (meaning
// "unchanged") on Update.
const secretMaskPrefix = "****"

// SettingService exposes workspace-level configuration. Both RPCs are gated by
// the IAM interceptor (laelia.settings.get / laelia.settings.update), which only
// the workspaceAdmin role holds, so they are admin-only in effect.
type SettingService struct {
	v1connect.UnimplementedSettingServiceHandler
	store           *store.Store
	s3clientManager *s3client.Client
	profile         *config.Profile
}

func NewSettingService(s *store.Store, s3clientManager *s3client.Client, profile *config.Profile) *SettingService {
	return &SettingService{store: s, s3clientManager: s3clientManager, profile: profile}
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
