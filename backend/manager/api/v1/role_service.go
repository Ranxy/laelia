package v1

import (
	"context"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"github.com/Ranxy/laelia/backend/common"
	"github.com/Ranxy/laelia/backend/common/permission"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/laelia/backend/manager/store"
)

// RoleService manages custom roles over the store's role CRUD. Predefined
// roles are read-only here: create/update/delete refuse a resource id that
// collides with a predefined role. Each RPC is gated by the IAM interceptor
// with the laelia.roles.* permissions.
type RoleService struct {
	v1connect.UnimplementedRoleServiceHandler
	store *store.Store
}

func NewRoleService(s *store.Store) *RoleService {
	return &RoleService{store: s}
}

func (s *RoleService) GetRole(ctx context.Context, req *connect.Request[v1pb.GetRoleRequest]) (*connect.Response[v1pb.Role], error) {
	resourceID, err := common.GetRoleID(req.Msg.GetName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	role, err := s.store.GetRole(ctx, &store.FindRoleMessage{ResourceID: &resourceID})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get role"))
	}
	if role == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("role %q not found", req.Msg.GetName()))
	}
	return connect.NewResponse(toV1Role(role)), nil
}

func (s *RoleService) ListRoles(ctx context.Context, _ *connect.Request[v1pb.ListRolesRequest]) (*connect.Response[v1pb.ListRolesResponse], error) {
	roles, err := s.store.ListRoles(ctx, &store.FindRoleMessage{})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to list roles"))
	}
	out := make([]*v1pb.Role, 0, len(roles))
	for _, role := range roles {
		out = append(out, toV1Role(role))
	}
	return connect.NewResponse(&v1pb.ListRolesResponse{Roles: out}), nil
}

func (s *RoleService) CreateRole(ctx context.Context, req *connect.Request[v1pb.CreateRoleRequest]) (*connect.Response[v1pb.Role], error) {
	in := req.Msg.GetRole()
	if in == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("role is required"))
	}
	resourceID, err := common.GetRoleID(in.GetName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrap(err, "role name must be roles/{id}"))
	}
	if resourceID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("role id is required"))
	}
	if store.IsPredefinedRole(resourceID) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("role %q is predefined and cannot be created", in.GetName()))
	}
	if in.GetTitle() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("role title is required"))
	}
	perms := toPermissionMap(in.GetPermissions())
	if err := validatePermissions(perms); err != nil {
		return nil, err
	}

	created, err := s.store.CreateRole(ctx, &store.RoleMessage{
		ResourceID:  resourceID,
		Name:        in.GetTitle(),
		Description: in.GetDescription(),
		Permissions: perms,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to create role"))
	}
	return connect.NewResponse(toV1Role(created)), nil
}

func (s *RoleService) UpdateRole(ctx context.Context, req *connect.Request[v1pb.UpdateRoleRequest]) (*connect.Response[v1pb.Role], error) {
	in := req.Msg.GetRole()
	if in == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("role is required"))
	}
	resourceID, err := common.GetRoleID(in.GetName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if store.IsPredefinedRole(resourceID) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("role %q is predefined and read-only", in.GetName()))
	}

	patch := &store.UpdateRoleMessage{ResourceID: resourceID}
	mask := req.Msg.GetUpdateMask()
	if mask == nil || len(mask.GetPaths()) == 0 {
		// No mask: replace all mutable fields.
		mask = &fieldmaskpb.FieldMask{Paths: []string{"title", "description", "permissions"}}
	}
	for _, path := range mask.GetPaths() {
		switch path {
		case "title":
			patch.Name = &in.Title
		case "description":
			patch.Description = &in.Description
		case "permissions":
			perms := toPermissionMap(in.GetPermissions())
			if err := validatePermissions(perms); err != nil {
				return nil, err
			}
			patch.Permissions = &perms
		default:
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("unknown update_mask path %q", path))
		}
	}

	updated, err := s.store.UpdateRole(ctx, patch)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to update role"))
	}
	if updated == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("role %q not found", in.GetName()))
	}
	return connect.NewResponse(toV1Role(updated)), nil
}

func (s *RoleService) DeleteRole(ctx context.Context, req *connect.Request[v1pb.DeleteRoleRequest]) (*connect.Response[emptypb.Empty], error) {
	resourceID, err := common.GetRoleID(req.Msg.GetName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if store.IsPredefinedRole(resourceID) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("role %q is predefined and read-only", req.Msg.GetName()))
	}
	if err := s.store.DeleteRole(ctx, resourceID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to delete role"))
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

// toV1Role maps a store RoleMessage to the v1 Role proto. Predefined is derived
// from the resource id so the frontend can render custom/predefined distinctly
// and disable edits on predefined rows.
func toV1Role(role *store.RoleMessage) *v1pb.Role {
	perms := make([]string, 0, len(role.Permissions))
	for p := range role.Permissions {
		perms = append(perms, p)
	}
	return &v1pb.Role{
		Name:        common.FormatRole(role.ResourceID),
		Title:       role.Name,
		Description: role.Description,
		Permissions: perms,
		Predefined:  store.IsPredefinedRole(role.ResourceID),
	}
}

// toPermissionMap converts a permission string slice into the set shape the
// store expects. Duplicates collapse.
func toPermissionMap(perms []string) map[permission.Permission]bool {
	m := make(map[permission.Permission]bool, len(perms))
	for _, p := range perms {
		m[p] = true
	}
	return m
}

// validatePermissions rejects unknown permission strings against the catalog so
// a custom role can never grant a capability that does not exist.
func validatePermissions(perms map[permission.Permission]bool) error {
	for p := range perms {
		if !permission.Exist(p) {
			return connect.NewError(connect.CodeInvalidArgument, errors.Errorf("unknown permission %q", p))
		}
	}
	return nil
}
