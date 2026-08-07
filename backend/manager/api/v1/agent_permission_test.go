package v1

import (
	"context"
	"testing"

	"github.com/Ranxy/laelia/backend/manager/component/iam"
	"github.com/Ranxy/laelia/backend/manager/store"
)

// TestAgentPermissionHelpers covers the owner short-circuit and the fail-closed
// paths of canEditAgent / canDeleteAgentWorkspace. The workspace-admin branch
// needs a store-backed iam.Manager (workspace IAM policy lookup), which is
// exercised by the interceptor tests; here a nil-store manager fails closed on
// any workspace lookup, so only the owner short-circuit can return true.
func TestAgentPermissionHelpers(t *testing.T) {
	owner := &store.UserMessage{ID: 1}
	agent := &store.AgentMessage{OwnerID: 1}
	s := &AgentService{iam: iam.NewManager(nil)}

	if !s.canEditAgent(context.Background(), owner, agent) {
		t.Error("owner must be able to edit own agent")
	}
	if s.canEditAgent(context.Background(), nil, agent) {
		t.Error("nil user must not edit")
	}

	if !isAgentOwner(owner, agent) {
		t.Error("owner must be recognized as agent owner")
	}
	if isAgentOwner(nil, agent) {
		t.Error("nil user must not be agent owner")
	}

	if canDeleteAgentWorkspace(context.Background(), nil, owner) {
		t.Error("nil manager must fail closed on workspace agents.edit")
	}
	if canDeleteAgentWorkspace(context.Background(), iam.NewManager(nil), nil) {
		t.Error("nil user must not hold workspace agents.edit")
	}
}
