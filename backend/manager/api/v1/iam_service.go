package v1

import (
	"context"
	"slices"
	"strings"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	exprpb "google.golang.org/genproto/googleapis/type/expr"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/Ranxy/laelia/backend/common"
	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/laelia/backend/manager/store"
	"github.com/Ranxy/laelia/backend/manager/utils"
)

// IamService exposes the workspace and per-agent IAM policies for management.
// Get reads the full policy; Set replaces it whole, guarded by an etag returned
// by Get. Each RPC is gated by the IAM interceptor with laelia.iam.getPolicy /
// setPolicy. Set validates every binding before writing: roles must resolve,
// chat-role labels are rejected (they are chat-membership markers, not IAM
// bindings), workspace/agent-only roles are scoped to their policy, and
// members must be well-formed principal names.
type IamService struct {
	v1connect.UnimplementedIamServiceHandler
	store *store.Store
}

func NewIamService(s *store.Store) *IamService {
	return &IamService{store: s}
}

func (s *IamService) GetWorkspaceIamPolicy(ctx context.Context, _ *connect.Request[v1pb.GetWorkspaceIamPolicyRequest]) (*connect.Response[v1pb.IamPolicyView], error) {
	p, err := s.store.GetWorkspaceIamPolicy(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get workspace iam policy"))
	}
	return connect.NewResponse(toIamPolicyView(p)), nil
}

func (s *IamService) SetWorkspaceIamPolicy(ctx context.Context, req *connect.Request[v1pb.SetWorkspaceIamPolicyRequest]) (*connect.Response[v1pb.IamPolicyView], error) {
	if err := validateIamPolicy(ctx, s.store, req.Msg.GetPolicy(), false /* agentScoped */); err != nil {
		return nil, err
	}
	// The workspace must keep at least one active end-user admin after the
	// write; otherwise a single Set could permanently lock everyone out.
	ok, err := hasActiveWorkspaceAdmin(ctx, s.store, req.Msg.GetPolicy(), 0)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to check workspace admin"))
	}
	if !ok {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("workspace must have at least one active admin"))
	}
	oldPolicy, err := s.store.GetWorkspaceIamPolicy(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get workspace iam policy"))
	}
	p, err := s.store.SetWorkspaceIamPolicy(ctx, req.Msg.GetPolicy(), req.Msg.GetEtag())
	if err != nil {
		return nil, translateSetIamError(err)
	}
	recordIamPolicyChange(ctx, "workspaces/-", findIamPolicyDeltas(oldPolicy.Policy, p.Policy))
	return connect.NewResponse(toIamPolicyView(p)), nil
}

func (s *IamService) GetAgentIamPolicy(ctx context.Context, req *connect.Request[v1pb.GetAgentIamPolicyRequest]) (*connect.Response[v1pb.IamPolicyView], error) {
	if _, err := common.GetAgentResourceID(req.Msg.GetName()); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	p, err := s.store.GetAgentIamPolicy(ctx, req.Msg.GetName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get agent iam policy"))
	}
	return connect.NewResponse(toIamPolicyView(p)), nil
}

func (s *IamService) SetAgentIamPolicy(ctx context.Context, req *connect.Request[v1pb.SetAgentIamPolicyRequest]) (*connect.Response[v1pb.IamPolicyView], error) {
	if _, err := common.GetAgentResourceID(req.Msg.GetName()); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if err := validateIamPolicy(ctx, s.store, req.Msg.GetPolicy(), true /* agentScoped */); err != nil {
		return nil, err
	}
	oldPolicy, err := s.store.GetAgentIamPolicy(ctx, req.Msg.GetName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get agent iam policy"))
	}
	p, err := s.store.SetAgentIamPolicy(ctx, req.Msg.GetName(), req.Msg.GetPolicy(), req.Msg.GetEtag())
	if err != nil {
		return nil, translateSetIamError(err)
	}
	recordIamPolicyChange(ctx, req.Msg.GetName(), findIamPolicyDeltas(oldPolicy.Policy, p.Policy))
	return connect.NewResponse(toIamPolicyView(p)), nil
}

// toIamPolicyView wraps a store IamPolicyMessage into the v1 view, carrying the
// etag the client must round-trip on its next Set.
func toIamPolicyView(p *store.IamPolicyMessage) *v1pb.IamPolicyView {
	if p == nil || p.Policy == nil {
		return &v1pb.IamPolicyView{Policy: nil, Etag: ""}
	}
	return &v1pb.IamPolicyView{Policy: p.Policy, Etag: p.Etag}
}

// translateSetIamError maps store errors to connect codes: an etag mismatch is
// Aborted (so the client re-fetches and retries); anything else is Internal.
func translateSetIamError(err error) error {
	if errors.Is(err, store.ErrPolicyEtagMismatch) {
		return connect.NewError(connect.CodeAborted, errors.New("iam policy changed since last read; re-fetch and retry"))
	}
	return connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to set iam policy"))
}

// chatRoleLabels are conversation_member chat-role-map markers. They are never
// valid IAM bindings: chat access is membership-based, resolved from
// conversation_member, not from the policy table. Rejecting them here keeps the
// UI from corrupting the engine's chat-role map.
var chatRoleLabels = map[string]bool{
	store.ConversationMemberRole: true,
	store.ConversationAdminRole:  true,
	store.ConversationOwnerRole:  true,
}

// workspaceOnlyRoles are only meaningful on the workspace IAM policy.
var workspaceOnlyRoles = map[string]bool{
	store.WorkspaceAdminRole:  true,
	store.WorkspaceMemberRole: true,
}

// validateIamPolicy checks every binding before a Set. agentScoped reports
// whether the policy is attached to an agent (true) or the workspace (false).
// It rejects unknown roles, chat-role labels (which are chat-membership
// markers, never IAM bindings), workspace-scoped roles bound on an agent, and
// malformed member strings — so a management UI cannot corrupt the engine.
func validateIamPolicy(ctx context.Context, s *store.Store, policy *storepb.IamPolicy, agentScoped bool) error {
	if policy == nil {
		return nil // an empty policy is a valid "clear all" write.
	}
	for _, binding := range policy.GetBindings() {
		resourceID, err := common.GetRoleID(binding.GetRole())
		if err != nil {
			return connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid role %q", binding.GetRole()))
		}
		if chatRoleLabels[resourceID] {
			return connect.NewError(connect.CodeInvalidArgument, errors.Errorf("role %q is a chat-membership marker and cannot be bound in an IAM policy", binding.GetRole()))
		}
		if workspaceOnlyRoles[resourceID] && agentScoped {
			return connect.NewError(connect.CodeInvalidArgument, errors.Errorf("role %q is workspace-scoped and cannot be bound on an agent", binding.GetRole()))
		}
		role, err := s.GetRoleSnapshot(ctx, resourceID)
		if err != nil {
			return connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to resolve role %q", binding.GetRole()))
		}
		if role == nil {
			return connect.NewError(connect.CodeInvalidArgument, errors.Errorf("unknown role %q", binding.GetRole()))
		}
		if _, err := common.ValidateIAMBindingConditionExpr(binding.GetCondition()); err != nil {
			return connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid condition for role %q", binding.GetRole()))
		}
		for _, member := range binding.GetMembers() {
			if err := validateMember(member); err != nil {
				return err
			}
			if err := validateMemberExists(ctx, s, member); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateMemberExists checks that a binding member names a real, active
// principal: a user that is not soft-deleted, an existing group, or an
// existing agent. allUsers is always valid. A binding to a missing or deleted
// principal would silently never match, so it is rejected at write time.
func validateMemberExists(ctx context.Context, s *store.Store, member string) error {
	switch {
	case member == common.AllUsers:
		return nil
	case strings.HasPrefix(member, common.UserNamePrefix):
		userID, err := common.GetUserID(member)
		if err != nil {
			return connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid member %q", member))
		}
		user, err := s.GetUserByID(ctx, userID)
		if err != nil {
			return connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to look up member %q", member))
		}
		if user == nil || user.MemberDeleted {
			return connect.NewError(connect.CodeInvalidArgument, errors.Errorf("member %q does not exist or is deleted", member))
		}
	case strings.HasPrefix(member, common.GroupPrefix):
		groupEmail, err := common.GetGroupEmail(member)
		if err != nil {
			return connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid member %q", member))
		}
		group, err := s.GetGroup(ctx, groupEmail)
		if err != nil {
			return connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to look up member %q", member))
		}
		if group == nil {
			return connect.NewError(connect.CodeInvalidArgument, errors.Errorf("member %q does not exist", member))
		}
	case strings.HasPrefix(member, common.AgentNamePrefix):
		agentID, err := common.GetAgentResourceID(member)
		if err != nil {
			return connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid member %q", member))
		}
		agent, err := s.GetAgentByResourceID(ctx, agentID)
		if err != nil {
			return connect.NewError(connect.CodeInternal, errors.Wrapf(err, "failed to look up member %q", member))
		}
		if agent == nil {
			return connect.NewError(connect.CodeInvalidArgument, errors.Errorf("member %q does not exist", member))
		}
	default:
		// Unreachable: validateMember rejects unknown prefixes before existence
		// checks run. Fail closed if this ever changes.
		return connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid member %q", member))
	}
	return nil
}

// hasActiveWorkspaceAdmin reports whether policy grants roles/workspaceAdmin to
// at least one active end user, either directly, through a group, or via
// allUsers. excludeUserID skips one user's direct binding (used by the
// delete/leave last-admin guards); it must be 0 when no exclusion is wanted.
func hasActiveWorkspaceAdmin(ctx context.Context, s *store.Store, policy *storepb.IamPolicy, excludeUserID int) (bool, error) {
	workspaceAdminRole := common.FormatRole(common.WorkspaceAdmin)
	excludedMember := common.FormatUserUID(excludeUserID)

	for _, binding := range policy.GetBindings() {
		if binding.GetRole() != workspaceAdminRole {
			continue
		}
		for _, member := range binding.GetMembers() {
			if excludeUserID != 0 && member == excludedMember {
				continue
			}
			if member == common.AllUsers {
				userStat, err := s.StatUsers(ctx)
				if err != nil {
					return false, err
				}
				activeEndUserCount := 0
				for _, stat := range userStat {
					if !stat.Deleted && stat.Type == storepb.PrincipalType_END_USER {
						activeEndUserCount = stat.Count
						break
					}
				}
				if excludeUserID != 0 {
					return activeEndUserCount > 1, nil
				}
				return activeEndUserCount > 0, nil
			}
			users := utils.GetUsersByMember(ctx, s, member)
			for _, user := range users {
				if !user.MemberDeleted && user.Type == storepb.PrincipalType_END_USER {
					return true, nil
				}
			}
		}
	}
	return false, nil
}

// recordIamPolicyChange attaches the resource and binding deltas of a
// successful SetIamPolicy call to the audit record the interceptor writes.
// When no audit setter is registered (e.g. the call did not go through the
// v1 chain), it is a no-op.
func recordIamPolicyChange(ctx context.Context, resource string, deltas []*v1pb.BindingDelta) {
	setServiceData, ok := common.GetSetServiceDataFromContext(ctx)
	if !ok {
		return
	}
	a, err := anypb.New(&v1pb.IamPolicyChange{
		Resource:      resource,
		BindingDeltas: deltas,
	})
	if err != nil {
		return
	}
	setServiceData(a)
}

// bindingKey identifies a member-role-condition triple within a policy.
type bindingKey struct {
	member string
	role   string
	cond   string
}

// indexPolicy builds the set of binding keys and remembers the condition
// message for each key so deltas can carry it.
func indexPolicy(policy *storepb.IamPolicy) (map[bindingKey]bool, map[bindingKey]*exprpb.Expr) {
	keys := make(map[bindingKey]bool)
	conds := make(map[bindingKey]*exprpb.Expr)
	for _, binding := range policy.GetBindings() {
		for _, member := range binding.GetMembers() {
			key := bindingKey{member: member, role: binding.GetRole()}
			if binding.GetCondition() != nil {
				b, err := protojson.Marshal(binding.GetCondition())
				if err == nil {
					key.cond = string(b)
				}
				conds[key] = binding.GetCondition()
			}
			keys[key] = true
		}
	}
	return keys, conds
}

// findIamPolicyDeltas computes the member-role-condition changes from
// oldPolicy to newPolicy, with a deterministic order (member, role, action).
func findIamPolicyDeltas(oldPolicy, newPolicy *storepb.IamPolicy) []*v1pb.BindingDelta {
	oldKeys, oldConds := indexPolicy(oldPolicy)
	newKeys, newConds := indexPolicy(newPolicy)

	var deltas []*v1pb.BindingDelta
	for key := range newKeys {
		if !oldKeys[key] {
			deltas = append(deltas, &v1pb.BindingDelta{
				Action:    v1pb.BindingDelta_ADD,
				Member:    key.member,
				Role:      key.role,
				Condition: newConds[key],
			})
		}
	}
	for key := range oldKeys {
		if !newKeys[key] {
			deltas = append(deltas, &v1pb.BindingDelta{
				Action:    v1pb.BindingDelta_REMOVE,
				Member:    key.member,
				Role:      key.role,
				Condition: oldConds[key],
			})
		}
	}
	slices.SortFunc(deltas, func(a, b *v1pb.BindingDelta) int {
		if a.GetMember() != b.GetMember() {
			return strings.Compare(a.GetMember(), b.GetMember())
		}
		if a.GetRole() != b.GetRole() {
			return strings.Compare(a.GetRole(), b.GetRole())
		}
		return int(a.GetAction()) - int(b.GetAction())
	})
	return deltas
}

// validateMember reports whether a binding member is a well-formed principal
// name: allUsers, users/{uid}, groups/{email}, or agents/{rid}.
func validateMember(member string) error {
	if member == common.AllUsers {
		return nil
	}
	switch {
	case strings.HasPrefix(member, common.UserNamePrefix):
		if _, err := common.GetUserID(member); err != nil {
			return connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid member %q", member))
		}
	case strings.HasPrefix(member, common.GroupPrefix):
		if _, err := common.GetGroupEmail(member); err != nil {
			return connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid member %q", member))
		}
	case strings.HasPrefix(member, common.AgentNamePrefix):
		if _, err := common.GetAgentResourceID(member); err != nil {
			return connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid member %q", member))
		}
	default:
		return connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid member %q (want users/{id}, groups/{email}, agents/{id}, or allUsers)", member))
	}
	return nil
}
