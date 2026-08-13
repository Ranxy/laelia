//nolint:revive
package utils

import (
	"context"
	"testing"

	"github.com/Ranxy/laelia/backend/manager/store"

	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
)

// TestGetCallerIAMPolicyBindings covers the agent-aware binding resolver
// without a database: direct principal matches and the allUsers pseudo-member
// are resolved in-memory. Group expansion (which needs the store) is not
// exercised here.
func TestGetCallerIAMPolicyBindings(t *testing.T) {
	user := &store.UserMessage{ID: 7, Handle: "ran-user-7"}
	agent := &store.AgentMessage{ResourceID: "agent-9"}
	policy := &storepb.IamPolicy{
		Bindings: []*storepb.Binding{
			{Role: "roles/conversationMember", Members: []string{"users/ran-user-7"}},
			{Role: "roles/conversationOwner", Members: []string{"allUsers"}},
			{Role: "roles/agentEditor", Members: []string{"agents/agent-9"}},
		},
	}
	ctx := context.Background()

	userBindings := GetCallerIAMPolicyBindings(ctx, nil, user, nil, policy)
	if got, want := roles(userBindings), map[string]bool{"roles/conversationMember": true, "roles/conversationOwner": true}; !sameSet(got, want) {
		t.Errorf("user caller: got %v, want %v", got, want)
	}

	agentBindings := GetCallerIAMPolicyBindings(ctx, nil, nil, agent, policy)
	if got, want := roles(agentBindings), map[string]bool{"roles/conversationOwner": true, "roles/agentEditor": true}; !sameSet(got, want) {
		t.Errorf("agent caller: got %v, want %v", got, want)
	}

	if got := GetCallerIAMPolicyBindings(ctx, nil, nil, nil, policy); len(got) != 0 {
		t.Errorf("no caller: expected 0 bindings, got %v", roles(got))
	}
}

func roles(bindings []*storepb.Binding) map[string]bool {
	out := make(map[string]bool, len(bindings))
	for _, b := range bindings {
		out[b.Role] = true
	}
	return out
}

func sameSet(got, want map[string]bool) bool {
	if len(got) != len(want) {
		return false
	}
	for k := range got {
		if !want[k] {
			return false
		}
	}
	return true
}
