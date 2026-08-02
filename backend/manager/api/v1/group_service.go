package v1

import (
	"context"
	"fmt"
	"strings"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"github.com/Ranxy/laelia/backend/common"
	"github.com/Ranxy/laelia/backend/common/permission"
	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/laelia/backend/manager/component/iam"
	"github.com/Ranxy/laelia/backend/manager/store"
)

// GroupService implements the group service. Groups are bindable in IAM
// policies (the engine expands group members at authorization time); group
// owners manage their own groups without workspace-level group permissions.
type GroupService struct {
	v1connect.UnimplementedGroupServiceHandler
	store *store.Store
	iam   *iam.Manager
}

// NewGroupService returns a new GroupService.
func NewGroupService(s *store.Store, iamManager *iam.Manager) *GroupService {
	return &GroupService{store: s, iam: iamManager}
}

// GetGroup gets a group.
func (s *GroupService) GetGroup(ctx context.Context, req *connect.Request[v1pb.GetGroupRequest]) (*connect.Response[v1pb.Group], error) {
	groupEmail, err := common.GetGroupEmail(req.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	group, err := s.store.GetGroup(ctx, groupEmail)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get group"))
	}
	if group == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("group %q not found", req.Msg.Name))
	}
	return connect.NewResponse(convertToV1Group(group)), nil
}

// BatchGetGroups gets groups in batch; missing groups are skipped.
func (s *GroupService) BatchGetGroups(ctx context.Context, req *connect.Request[v1pb.BatchGetGroupsRequest]) (*connect.Response[v1pb.BatchGetGroupsResponse], error) {
	response := &v1pb.BatchGetGroupsResponse{}
	for _, name := range req.Msg.Names {
		groupEmail, err := common.GetGroupEmail(name)
		if err != nil {
			continue
		}
		group, err := s.store.GetGroup(ctx, groupEmail)
		if err != nil || group == nil {
			continue
		}
		response.Groups = append(response.Groups, convertToV1Group(group))
	}
	return connect.NewResponse(response), nil
}

// ListGroups lists all groups.
func (s *GroupService) ListGroups(ctx context.Context, req *connect.Request[v1pb.ListGroupsRequest]) (*connect.Response[v1pb.ListGroupsResponse], error) {
	offset, err := parseLimitAndOffset(&pageSize{
		token:   req.Msg.PageToken,
		limit:   int(req.Msg.PageSize),
		maximum: 1000,
	})
	if err != nil {
		return nil, err
	}
	limitPlusOne := offset.limit + 1

	find := &store.FindGroupMessage{
		Limit:  &limitPlusOne,
		Offset: &offset.offset,
	}
	if err := parseGroupFilter(find, req.Msg.Filter); err != nil {
		return nil, err
	}

	groups, err := s.store.ListGroups(ctx, find)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to list groups"))
	}

	nextPageToken := ""
	if len(groups) == limitPlusOne {
		groups = groups[:offset.limit]
		if nextPageToken, err = offset.getNextPageToken(); err != nil {
			return nil, err
		}
	}

	response := &v1pb.ListGroupsResponse{NextPageToken: nextPageToken}
	for _, group := range groups {
		response.Groups = append(response.Groups, convertToV1Group(group))
	}
	return connect.NewResponse(response), nil
}

// CreateGroup creates a group. The request must carry at least one OWNER so a
// group never starts ownerless.
func (s *GroupService) CreateGroup(ctx context.Context, req *connect.Request[v1pb.CreateGroupRequest]) (*connect.Response[v1pb.Group], error) {
	groupEmail := strings.ToLower(strings.TrimSpace(req.Msg.GroupEmail))
	if groupEmail == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("group_email is required"))
	}
	if req.Msg.Group == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("group is required"))
	}
	if strings.TrimSpace(req.Msg.Group.Title) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("group title is required"))
	}
	payload, err := convertToGroupPayload(req.Msg.Group.Members)
	if err != nil {
		return nil, err
	}
	if !hasGroupOwner(payload) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("group must have at least one owner"))
	}
	if err := s.validateGroupMembers(ctx, payload); err != nil {
		return nil, err
	}

	group, err := s.store.CreateGroup(ctx, &store.GroupMessage{
		Email:       groupEmail,
		Title:       req.Msg.Group.Title,
		Description: req.Msg.Group.Description,
		Payload:     payload,
	})
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			return nil, connect.NewError(connect.CodeAlreadyExists, errors.Errorf("group %q already exists", groupEmail))
		}
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to create group"))
	}
	return connect.NewResponse(convertToV1Group(group)), nil
}

// UpdateGroup updates a group. The group owner or a caller holding
// laelia.groups.update may update; external (SCIM/IdP-synced) groups are
// read-only.
func (s *GroupService) UpdateGroup(ctx context.Context, req *connect.Request[v1pb.UpdateGroupRequest]) (*connect.Response[v1pb.Group], error) {
	if req.Msg.Group == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("group is required"))
	}
	groupEmail, err := common.GetGroupEmail(req.Msg.Group.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	group, err := s.store.GetGroup(ctx, groupEmail)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get group"))
	}
	if group == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("group %q not found", req.Msg.Group.Name))
	}
	if group.Payload.Source != "" {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.Errorf("group %q is synced from an external source and is read-only", req.Msg.Group.Name))
	}
	user, ok := GetUserFromContext(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
	}
	ok, err = s.canManageGroup(ctx, user, group, permission.GroupsUpdate)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.Errorf("only the group owner or a caller with %q can update this group", permission.GroupsUpdate))
	}

	mask := req.Msg.UpdateMask
	if mask == nil || len(mask.Paths) == 0 {
		mask = &fieldmaskpb.FieldMask{Paths: []string{"title", "description", "members"}}
	}
	patch := &store.UpdateGroupMessage{}
	for _, path := range mask.Paths {
		switch path {
		case "title":
			if strings.TrimSpace(req.Msg.Group.Title) == "" {
				return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("group title must not be empty"))
			}
			patch.Title = &req.Msg.Group.Title
		case "description":
			patch.Description = &req.Msg.Group.Description
		case "members":
			payload, err := convertToGroupPayload(req.Msg.Group.Members)
			if err != nil {
				return nil, err
			}
			if !hasGroupOwner(payload) {
				return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("group must keep at least one owner"))
			}
			if err := s.validateGroupMembers(ctx, payload); err != nil {
				return nil, err
			}
			patch.Payload = payload
		default:
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("unsupported update path %q", path))
		}
	}

	updated, err := s.store.UpdateGroup(ctx, groupEmail, patch)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to update group"))
	}
	return connect.NewResponse(convertToV1Group(updated)), nil
}

// DeleteGroup deletes a group. The group owner or a caller holding
// laelia.groups.delete may delete. Existing IAM bindings referencing the group
// become no-ops (the engine resolves a missing group to no members).
func (s *GroupService) DeleteGroup(ctx context.Context, req *connect.Request[v1pb.DeleteGroupRequest]) (*connect.Response[emptypb.Empty], error) {
	groupEmail, err := common.GetGroupEmail(req.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	group, err := s.store.GetGroup(ctx, groupEmail)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get group"))
	}
	if group == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("group %q not found", req.Msg.Name))
	}
	if group.Payload.Source != "" {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.Errorf("group %q is synced from an external source and is read-only", req.Msg.Name))
	}
	user, ok := GetUserFromContext(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
	}
	ok, err = s.canManageGroup(ctx, user, group, permission.GroupsDelete)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.Errorf("only the group owner or a caller with %q can delete this group", permission.GroupsDelete))
	}
	if err := s.store.DeleteGroup(ctx, groupEmail); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to delete group"))
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

// canManageGroup reports whether the caller may manage the group: a group
// OWNER, or a caller holding the given workspace-level group permission.
func (s *GroupService) canManageGroup(ctx context.Context, user *store.UserMessage, group *store.GroupMessage, perm permission.Permission) (bool, error) {
	if isGroupOwner(user, group) {
		return true, nil
	}
	return s.iam.CheckPermission(ctx, perm, user, nil, nil)
}

// validateGroupMembers checks that every member is a well-formed, existing,
// non-deleted user.
func (s *GroupService) validateGroupMembers(ctx context.Context, payload *storepb.GroupPayload) error {
	for _, m := range payload.GetMembers() {
		userID, err := common.GetUserID(m.GetMember())
		if err != nil {
			return connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid group member %q", m.GetMember()))
		}
		user, err := s.store.GetUserByID(ctx, userID)
		if err != nil {
			return connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to look up group member %q", m.GetMember()))
		}
		if user == nil || user.MemberDeleted {
			return connect.NewError(connect.CodeInvalidArgument, errors.Errorf("group member %q does not exist or is deleted", m.GetMember()))
		}
	}
	return nil
}

// convertToGroupPayload converts v1 members to the store payload, validating
// member format and roles.
func convertToGroupPayload(members []*v1pb.GroupMember) (*storepb.GroupPayload, error) {
	payload := &storepb.GroupPayload{}
	for _, m := range members {
		storeMember := &storepb.GroupMember{Member: m.GetMember()}
		switch m.GetRole() {
		case v1pb.GroupMemberRole_OWNER:
			storeMember.Role = storepb.GroupMember_OWNER
		case v1pb.GroupMemberRole_MEMBER:
			storeMember.Role = storepb.GroupMember_MEMBER
		default:
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("unsupported group member role %v", m.GetRole()))
		}
		payload.Members = append(payload.Members, storeMember)
	}
	return payload, nil
}

// hasGroupOwner reports whether the payload contains at least one OWNER.
func hasGroupOwner(payload *storepb.GroupPayload) bool {
	for _, m := range payload.GetMembers() {
		if m.GetRole() == storepb.GroupMember_OWNER {
			return true
		}
	}
	return false
}

// isGroupOwner reports whether the user is an OWNER of the group.
func isGroupOwner(user *store.UserMessage, group *store.GroupMessage) bool {
	if user == nil {
		return false
	}
	userName := common.FormatUserUID(user.ID)
	for _, m := range group.Payload.GetMembers() {
		if m.GetMember() == userName && m.GetRole() == storepb.GroupMember_OWNER {
			return true
		}
	}
	return false
}

// convertToV1Group maps a store group to the v1 API shape.
func convertToV1Group(group *store.GroupMessage) *v1pb.Group {
	out := &v1pb.Group{
		Name:        common.FormatGroupEmail(group.Email),
		Email:       group.Email,
		Title:       group.Title,
		Description: group.Description,
		Source:      group.Payload.GetSource(),
	}
	for _, m := range group.Payload.GetMembers() {
		role := v1pb.GroupMemberRole_GROUP_MEMBER_ROLE_UNSPECIFIED
		switch m.GetRole() {
		case storepb.GroupMember_OWNER:
			role = v1pb.GroupMemberRole_OWNER
		case storepb.GroupMember_MEMBER:
			role = v1pb.GroupMemberRole_MEMBER
		default:
			// Unspecified roles map to unspecified.
		}
		out.Members = append(out.Members, &v1pb.GroupMember{Member: m.GetMember(), Role: role})
	}
	return out
}

// parseGroupFilter translates the simple CEL filter (title/email equality)
// into a parameterized SQL filter.
func parseGroupFilter(find *store.FindGroupMessage, filter string) error {
	if filter == "" {
		return nil
	}
	expressions, err := ParseFilter(filter)
	if err != nil {
		return connect.NewError(connect.CodeInvalidArgument, err)
	}
	where := make([]string, 0, len(expressions))
	var args []any
	for _, e := range expressions {
		if e.Operator != ComparatorTypeEqual {
			return connect.NewError(connect.CodeInvalidArgument, errors.Errorf("unsupported operator %q for group filter", e.Operator))
		}
		switch e.Key {
		case "title":
			where = append(where, fmt.Sprintf("name = $%d", len(args)+1))
			args = append(args, e.Value)
		case "email":
			where = append(where, fmt.Sprintf("email = $%d", len(args)+1))
			args = append(args, e.Value)
		default:
			return connect.NewError(connect.CodeInvalidArgument, errors.Errorf("unsupported filter field %q", e.Key))
		}
	}
	find.Filter = &store.ListResourceFilter{Where: strings.Join(where, " AND "), Args: args}
	return nil
}
