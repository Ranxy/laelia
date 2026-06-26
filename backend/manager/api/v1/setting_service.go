package v1

import (
	"context"
	"strings"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/laelia/backend/manager/component/s3client"
	"github.com/Ranxy/laelia/backend/manager/store"
)

// secretMaskPrefix is the sentinel prefix returned on Get and accepted (meaning
// "unchanged") on Update.
const secretMaskPrefix = "****"

// SettingService exposes workspace-level configuration. Both RPCs are
// admin-only: a non-admin receives PermissionDenied.
type SettingService struct {
	v1connect.UnimplementedSettingServiceHandler
	store           *store.Store
	s3clientManager *s3client.Client
}

func NewSettingService(s *store.Store, s3clientManager *s3client.Client) *SettingService {
	return &SettingService{store: s, s3clientManager: s3clientManager}
}

func (s *SettingService) GetS3Config(ctx context.Context, _ *connect.Request[v1pb.GetS3ConfigRequest]) (*connect.Response[v1pb.GetS3ConfigResponse], error) {
	if err := s.requireAdmin(ctx); err != nil {
		return nil, err
	}
	cfg, err := s.store.GetS3ConfigSetting(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get s3 config"))
	}
	cfg.SecretKey = maskSecret(cfg.SecretKey)
	return connect.NewResponse(&v1pb.GetS3ConfigResponse{Config: cfg}), nil
}

func (s *SettingService) UpdateS3Config(ctx context.Context, req *connect.Request[v1pb.UpdateS3ConfigRequest]) (*connect.Response[v1pb.UpdateS3ConfigResponse], error) {
	if err := s.requireAdmin(ctx); err != nil {
		return nil, err
	}
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

func (s *SettingService) requireAdmin(ctx context.Context) error {
	user, ok := GetUserFromContext(ctx)
	if !ok || user == nil {
		return connect.NewError(connect.CodeUnauthenticated, errors.New("not authenticated"))
	}
	isAdmin, err := isUserWorkspaceAdmin(ctx, s.store, user)
	if err != nil {
		return connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to check workspace admin"))
	}
	if !isAdmin {
		return connect.NewError(connect.CodePermissionDenied, errors.New("workspace admin only"))
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
