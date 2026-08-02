package store

import (
	"testing"

	"github.com/Ranxy/laelia/backend/common"
	models "github.com/Ranxy/laelia/backend/generated-go/store"
)

func mustIamPolicy(t *testing.T, payload string) *models.IamPolicy {
	t.Helper()
	p := &models.IamPolicy{}
	if err := common.ProtojsonUnmarshaler.Unmarshal([]byte(payload), p); err != nil {
		t.Fatalf("failed to parse IAM policy: %v", err)
	}
	return p
}

// TestConversationMemberName locks in the IAM binding member format for
// conversation members: users/{principalID} for users and agents/{resourceID}
// for agents. The engine and the policy writer must agree on this format.
func TestConversationMemberName(t *testing.T) {
	if got, want := conversationMemberName(MemberTypeUser, "101"), "users/101"; got != want {
		t.Errorf("user member name: got %q, want %q", got, want)
	}
	if got, want := conversationMemberName(MemberTypeAgent, "agent-9"), "agents/agent-9"; got != want {
		t.Errorf("agent member name: got %q, want %q", got, want)
	}
}

// TestConversationRoleNameRoundTrip locks in the mapping between chat role
// values (1/2/3) and IAM binding role names.
func TestConversationRoleNameRoundTrip(t *testing.T) {
	for _, role := range []int32{MemberRoleOwner, MemberRoleAdmin, MemberRoleMember} {
		name := conversationRoleName(role)
		if conversationRoleFromName(name) != role {
			t.Errorf("role %d did not round-trip through %q", role, name)
		}
	}
	if got := conversationRoleFromName("roles/custom"); got != 0 {
		t.Errorf("custom role must not map to a chat role, got %d", got)
	}
}

// TestPolicyContainsMember verifies the membership predicate over a
// conversation IAM policy.
func TestPolicyContainsMember(t *testing.T) {
	policy := &IamPolicyMessage{
		Policy: mustIamPolicy(t, `{"bindings":[{"role":"roles/conversationOwner","members":["users/1"]},{"role":"roles/conversationMember","members":["users/2","agents/a"]}]}`),
	}
	for _, member := range []string{"users/1", "users/2", "agents/a"} {
		if !policyContainsMember(policy, member) {
			t.Errorf("expected policy to contain %q", member)
		}
	}
	if policyContainsMember(policy, "users/3") {
		t.Error("policy must not contain users/3")
	}
}
