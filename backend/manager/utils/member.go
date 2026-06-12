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
		groupEmail, err := common.GetGroupEmail(member)
		if err != nil {
			slog.Error("failed to parse group email", slog.String("group", member), log.WithError(err))
			return users
		}
		group, err := stores.GetGroup(ctx, groupEmail)
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

// GetUserIAMPolicyBindings return the valid bindings for the user.
func GetUserIAMPolicyBindings(ctx context.Context, stores *store.Store, user *store.UserMessage, policies ...*storepb.IamPolicy) []*storepb.Binding {
	userIDFullName := common.FormatUserUID(user.ID)

	var bindings []*storepb.Binding

	for _, policy := range policies {
		for _, binding := range policy.Bindings {
			if !validateIAMBinding(binding) {
				continue
			}

			hasUser := false
			for _, member := range binding.Members {
				if member == common.AllUsers {
					hasUser = true
					break
				}
				if userIDFullName == member {
					hasUser = true
					break
				}
				if strings.HasPrefix(member, common.GroupPrefix) {
					groupEmail, err := common.GetGroupEmail(member)
					if err != nil {
						slog.Error("failed to parse group email", slog.String("group", member), log.WithError(err))
						continue
					}
					group, err := stores.GetGroup(ctx, groupEmail)
					if err != nil {
						slog.Error("failed to get group", slog.String("group", member), log.WithError(err))
						continue
					}
					if group == nil {
						slog.Error("cannot found group", slog.String("group", member))
						continue
					}
					for _, member := range group.Payload.Members {
						if userIDFullName == member.Member {
							hasUser = true
							break
						}
					}
					if hasUser {
						break
					}
				}
			}
			if hasUser {
				bindings = append(bindings, binding)
			}
		}
	}
	return bindings
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
		groupEmail, err := common.GetGroupEmail(member)
		if err != nil {
			slog.Error("failed to parse group email", slog.String("group", member), log.WithError(err))
			return false
		}
		group, err := stores.GetGroup(ctx, groupEmail)
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
