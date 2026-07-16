package v1

import (
	"context"
	"strings"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

	"github.com/Ranxy/laelia/backend/common"
	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/laelia/backend/manager/store"
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
	p, err := s.store.SetWorkspaceIamPolicy(ctx, req.Msg.GetPolicy(), req.Msg.GetEtag())
	if err != nil {
		return nil, translateSetIamError(err)
	}
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
	p, err := s.store.SetAgentIamPolicy(ctx, req.Msg.GetName(), req.Msg.GetPolicy(), req.Msg.GetEtag())
	if err != nil {
		return nil, translateSetIamError(err)
	}
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
		for _, member := range binding.GetMembers() {
			if err := validateMember(member); err != nil {
				return err
			}
		}
	}
	return nil
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
