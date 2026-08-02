//nolint:revive
package utils

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/Ranxy/laelia/backend/common"
	"github.com/Ranxy/laelia/backend/common/log"
	"github.com/Ranxy/laelia/backend/manager/store"

	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
)

func validateIAMBinding(binding *storepb.Binding) bool {
	ok, err := common.EvalBindingCondition(binding.Condition.GetExpression(), time.Now())
	if err != nil {
		slog.Error("failed to eval binding condition", slog.String("expression", binding.Condition.GetExpression()), log.WithError(err))
		return false
	}
	return ok
}

// GetUsersByMember gets user messages by member.
// The member should in users/{uid} or groups/{email} format.
func GetUsersByMember(ctx context.Context, stores *store.Store, member string) []*store.UserMessage {
	var users []*store.UserMessage
	if strings.HasPrefix(member, common.UserNamePrefix) {
		user := getUserByIdentifier(ctx, stores, member)
		if user != nil {
			users = append(users, user)
		}
	} else if strings.HasPrefix(member, common.GroupPrefix) {
		group, err := stores.GetGroupByName(ctx, member)
		if err != nil {
			slog.Error("failed to get group", slog.String("group", member), log.WithError(err))
			return users
		}
		if group == nil {
			slog.Error("cannot found group", slog.String("group", member))
			return users
		}
		for _, member := range group.Payload.Members {
			user := getUserByIdentifier(ctx, stores, member.Member)
			if user != nil {
				users = append(users, user)
			}
		}
	}
	return users
}

// getUserByIdentifier gets user message by identifier.
// The identifier should in users/{uid} format.
func getUserByIdentifier(ctx context.Context, stores *store.Store, identifier string) *store.UserMessage {
	userUID, err := common.GetUserID(identifier)
	if err != nil {
		slog.Error("failed to parse user id", slog.String("user", identifier), log.WithError(err))
		return nil
	}
	user, err := stores.GetUserByID(ctx, userUID)
	if err != nil {
		slog.Error("failed to get user", slog.String("user", identifier), log.WithError(err))
		return nil
	}
	return user
}

// GetUserIAMPolicyBindings return the valid bindings for the user. It is the
// user-only face of the agent-aware GetCallerIAMPolicyBindings; the two share
// one binding-match implementation so user and agent callers cannot drift.
func GetUserIAMPolicyBindings(ctx context.Context, stores *store.Store, user *store.UserMessage, policies ...*storepb.IamPolicy) []*storepb.Binding {
	return GetCallerIAMPolicyBindings(ctx, stores, user, nil, policies...)
}

// GetCallerIAMPolicyBindings returns the valid bindings for the caller, which
// may be a user OR an agent. It is the agent-aware sibling of
// GetUserIAMPolicyBindings: a user caller matches users/{uid} members (and
// group-expanded members, and allUsers); an agent caller matches agents/{rid}
// members (and allUsers). Group expansion only applies to users (groups contain
// users, never agents). Returns nil when neither a user nor an agent is supplied.
func GetCallerIAMPolicyBindings(ctx context.Context, stores *store.Store, user *store.UserMessage, agent *store.AgentMessage, policies ...*storepb.IamPolicy) []*storepb.Binding {
	principal, isUser := callerPrincipalName(user, agent)
	if principal == "" {
		return nil
	}

	var bindings []*storepb.Binding
	for _, policy := range policies {
		for _, binding := range policy.Bindings {
			if !validateIAMBinding(binding) {
				continue
			}
			if bindingContainsCaller(ctx, stores, binding, principal, isUser, user) {
				bindings = append(bindings, binding)
			}
		}
	}
	return bindings
}

// callerPrincipalName returns the fully-qualified principal name for the caller
// and whether that principal is a user. Returns ("", false) when the caller is
// neither a user nor an agent.
func callerPrincipalName(user *store.UserMessage, agent *store.AgentMessage) (string, bool) {
	switch {
	case user != nil:
		return common.FormatUserUID(user.ID), true
	case agent != nil:
		return common.FormatAgentUID(agent.ResourceID), false
	default:
		return "", false
	}
}

// bindingContainsCaller reports whether the binding's member set contains the
// caller. A direct principal match or the allUsers pseudo-member always wins;
// group members are expanded only for user callers (groups hold users, not
// agents).
func bindingContainsCaller(ctx context.Context, stores *store.Store, binding *storepb.Binding, principal string, isUser bool, user *store.UserMessage) bool {
	for _, member := range binding.Members {
		if member == common.AllUsers || member == principal {
			return true
		}
	}
	if !isUser || user == nil {
		return false
	}
	for _, member := range binding.Members {
		if strings.HasPrefix(member, common.GroupPrefix) && MemberContainsUser(ctx, stores, member, user) {
			return true
		}
	}
	return false
}

// MemberContainsUser checks if a member (user or group) contains the specified user.
// The member should be in users/{uid} or groups/{email} format.
func MemberContainsUser(ctx context.Context, stores *store.Store, member string, user *store.UserMessage) bool {
	if member == common.AllUsers {
		return true
	}

	// Check if member is a user
	if strings.HasPrefix(member, common.UserNamePrefix) {
		memberUID, err := common.GetUserID(member)
		if err != nil {
			slog.Error("failed to parse user id", slog.String("member", member), log.WithError(err))
			return false
		}
		return memberUID == user.ID
	}

	// Check if member is a group
	if strings.HasPrefix(member, common.GroupPrefix) {
		group, err := stores.GetGroupByName(ctx, member)
		if err != nil {
			slog.Error("failed to get group", slog.String("group", member), log.WithError(err))
			return false
		}
		if group == nil {
			slog.Error("cannot find group", slog.String("group", member))
			return false
		}
		userIDFullName := common.FormatUserUID(user.ID)
		for _, groupMember := range group.Payload.Members {
			if userIDFullName == groupMember.Member {
				return true
			}
		}
	}

	return false
}

// GetUserRolesInIamPolicy returns the `uniq`ed roles of a user, including workspace roles and the roles in the projects.
// the condition of role binding is respected and evaluated with request.time=time.Now().
// the returned role name should in the roles/{id} format.
func GetUserRolesInIamPolicy(ctx context.Context, stores *store.Store, user *store.UserMessage, policies ...*storepb.IamPolicy) []string {
	var roles []string

	for _, policy := range policies {
		bindings := GetUserIAMPolicyBindings(ctx, stores, user, policy)
		for _, binding := range bindings {
			roles = append(roles, binding.Role)
		}
	}
	roles = Uniq(roles)

	return roles
}

// See GetUserRoles. The returned map key format is roles/{role}.
func GetUserFormattedRolesMap(ctx context.Context, stores *store.Store, user *store.UserMessage, projectPolicies ...*storepb.IamPolicy) map[string]bool {
	roles := GetUserRolesInIamPolicy(ctx, stores, user, projectPolicies...)

	rolesMap := make(map[string]bool)
	for _, role := range roles {
		rolesMap[role] = true
	}
	return rolesMap
}
