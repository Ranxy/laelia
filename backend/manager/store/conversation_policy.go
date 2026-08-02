package store

import (
	"context"
	"database/sql"

	"github.com/google/uuid"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/Ranxy/laelia/backend/common"
	models "github.com/Ranxy/laelia/backend/generated-go/store"
)

// conversationPolicyResource is the policy-table resource name for a
// conversation: "conversations/{uuid}".
func conversationPolicyResource(convID uuid.UUID) string {
	return common.FormatConversationName(convID.String())
}

// conversationMemberName formats a conversation_member key as the IAM binding
// member string: users/{principalID} for users, agents/{resourceID} for agents.
func conversationMemberName(memberType int32, memberID string) string {
	if memberType == MemberTypeAgent {
		return common.FormatAgentUID(memberID)
	}
	return common.UserNamePrefix + memberID
}

// conversationRoleName maps a chat role value to its IAM binding role name.
func conversationRoleName(role int32) string {
	switch role {
	case MemberRoleOwner:
		return common.FormatRole(ConversationOwnerRole)
	case MemberRoleAdmin:
		return common.FormatRole(ConversationAdminRole)
	default:
		return common.FormatRole(ConversationMemberRole)
	}
}

// conversationRoleFromName maps an IAM binding role name back to a chat role
// value, or 0 when the role is not one of the built-in conversation roles.
func conversationRoleFromName(role string) int32 {
	switch role {
	case common.FormatRole(ConversationOwnerRole):
		return MemberRoleOwner
	case common.FormatRole(ConversationAdminRole):
		return MemberRoleAdmin
	case common.FormatRole(ConversationMemberRole):
		return MemberRoleMember
	default:
		return 0
	}
}

// GetConversationIamPolicy returns the IAM policy attached to a conversation.
// An absent policy is returned as an empty IamPolicyMessage, not an error.
func (s *Store) GetConversationIamPolicy(ctx context.Context, convID uuid.UUID) (*IamPolicyMessage, error) {
	resourceType := models.Policy_CONVERSATION
	resource := conversationPolicyResource(convID)
	return s.getIamPolicy(ctx, &FindPolicyMessage{
		ResourceType: &resourceType,
		Resource:     &resource,
	})
}

// getConversationPolicyForUpdate loads the conversation policy inside a
// transaction with a row lock so concurrent membership patches cannot lose
// updates. A missing row yields an empty policy.
func getConversationPolicyForUpdate(ctx context.Context, tx *sql.Tx, resource string) (*models.IamPolicy, error) {
	var payload []byte
	err := tx.QueryRowContext(ctx, `
		SELECT payload FROM policy
		WHERE resource_type = $1 AND resource = $2 AND type = $3
		FOR UPDATE
	`, models.Policy_CONVERSATION.String(), resource, models.Policy_IAM.String()).Scan(&payload)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return &models.IamPolicy{}, nil
		}
		return nil, errors.Wrap(err, "failed to load conversation policy")
	}
	p := &models.IamPolicy{}
	if err := common.ProtojsonUnmarshaler.Unmarshal(payload, p); err != nil {
		return nil, errors.Wrapf(err, "failed to unmarshal conversation policy %q", resource)
	}
	return p, nil
}

// upsertConversationPolicyTx inserts or replaces the conversation IAM policy
// inside the caller's transaction.
func upsertConversationPolicyTx(ctx context.Context, tx *sql.Tx, resource string, policy *models.IamPolicy) error {
	payload, err := marshalIamPolicy(policy)
	if err != nil {
		return err
	}
	_, err = upsertPolicyV2Impl(ctx, tx, &PolicyMessage{
		ResourceType:      models.Policy_CONVERSATION,
		Resource:          resource,
		Type:              models.Policy_IAM,
		Payload:           string(payload),
		InheritFromParent: false,
		Enforce:           true,
	})
	return err
}

// patchConversationMemberRolesTx sets the member's conversation roles to
// exactly roles (IAM role names such as "roles/conversationAdmin"). It is the
// single write primitive for membership changes: add, remove, and role moves
// all funnel through it inside a transaction.
func patchConversationMemberRolesTx(ctx context.Context, tx *sql.Tx, convID uuid.UUID, memberType int32, memberID string, roles []string) error {
	resource := conversationPolicyResource(convID)
	policy, err := getConversationPolicyForUpdate(ctx, tx, resource)
	if err != nil {
		return err
	}
	applyIamPolicyPatch(policy, &PatchIamPolicyMessage{
		Member: conversationMemberName(memberType, memberID),
		Roles:  roles,
	})
	return upsertConversationPolicyTx(ctx, tx, resource, policy)
}

// conversationPolicyCacheKey is the policy-cache key for a conversation.
func conversationPolicyCacheKey(convID uuid.UUID) string {
	return getPolicyCacheKey(models.Policy_CONVERSATION, conversationPolicyResource(convID), models.Policy_IAM)
}

// invalidateConversationPolicyCache drops the cached conversation policy after
// a transaction commits.
func (s *Store) invalidateConversationPolicyCache(convID uuid.UUID) {
	s.policyCache.Remove(conversationPolicyCacheKey(convID))
}

// marshalIamPolicy serializes an IAM policy to protojson bytes.
func marshalIamPolicy(policy *models.IamPolicy) ([]byte, error) {
	return protojson.Marshal(policy)
}
