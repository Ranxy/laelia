package v1

import (
	"context"
	"testing"

	"github.com/Ranxy/laelia/backend/manager/component/iam"
	"github.com/Ranxy/laelia/backend/manager/store"
)

// TestMachinePermissionHelpers covers the creator short-circuit and the
// fail-closed paths of isMachineAdmin / canDeleteMachine. The workspace-admin
// branch needs a store-backed iam.Manager (workspace IAM policy lookup), which
// is exercised by the interceptor tests; here a nil-store manager fails closed
// on any workspace lookup, so only the creator short-circuit can return true.
func TestMachinePermissionHelpers(t *testing.T) {
	creator := &store.UserMessage{ID: 1}
	machine := &store.MachineMessage{CreatedBy: 1}
	m := iam.NewManager(nil)

	if !isMachineAdmin(context.Background(), m, creator, machine) {
		t.Error("creator must be machine admin for own machine")
	}
	if isMachineAdmin(context.Background(), m, nil, machine) {
		t.Error("nil user must not be machine admin")
	}

	if !canDeleteMachine(context.Background(), m, creator, machine) {
		t.Error("creator must be able to delete own machine")
	}
	if canDeleteMachine(context.Background(), m, nil, machine) {
		t.Error("nil user must not delete")
	}
}

// TestCanSeeMachine covers the creator short-circuit and the fail-closed paths
// of canSeeMachine. The create-agent permission branch (workspace admin or a
// roles/machineAgentCreator binding on the machine's IAM policy) needs a
// store-backed iam.Manager, which is exercised by the interceptor tests; here
// a nil-store manager cannot run a permission lookup, so only the creator
// short-circuit can return true.
func TestCanSeeMachine(t *testing.T) {
	creator := &store.UserMessage{ID: 1}
	machine := &store.MachineMessage{CreatedBy: 1}
	m := iam.NewManager(nil)

	if !canSeeMachine(context.Background(), m, creator, machine) {
		t.Error("creator must see own machine")
	}
	if canSeeMachine(context.Background(), m, nil, machine) {
		t.Error("nil user must not see machine")
	}
	if canSeeMachine(context.Background(), nil, creator, machine) {
		t.Error("nil manager must not see machine")
	}
}
