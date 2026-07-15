package iam

import (
	"context"
	"testing"

	"github.com/Ranxy/laelia/backend/common/permission"
	"github.com/Ranxy/laelia/backend/manager/store"
)

// newManagerWithoutStore builds a Manager whose store is never reached by the
// agent-caller path (the workspace policy is only loaded for user callers, and
// predefined roles resolve in-memory). This lets the baseline + predefined-role
// resolution be unit-tested without a database.
func newManagerWithoutStore() *Manager {
	return &Manager{store: nil}
}

func TestCheckPermissionAgentBaseline(t *testing.T) {
	m := newManagerWithoutStore()
	agent := &store.AgentMessage{ID: 9, ResourceID: "agent-9"}

	cases := []struct {
		perm permission.Permission
		want bool
	}{
		{permission.ConversationsRead, true}, // baseline workspaceMember
		{permission.ConversationsSend, true},
		{permission.CommandsWatch, true},
		{permission.AgentsEdit, true}, // baseline includes agents.edit (handler still gates)
		{permission.AgentsCreate, false},
		{permission.UsersUpdate, false},
		{permission.SettingsUpdate, false},
		{permission.RolesCreate, false},
	}
	for _, c := range cases {
		got, err := m.CheckPermission(context.Background(), c.perm, nil, agent, nil)
		if err != nil {
			t.Fatalf("perm %q: unexpected error %v", c.perm, err)
		}
		if got != c.want {
			t.Errorf("perm %q: got %v, want %v", c.perm, got, c.want)
		}
	}
}

func TestPredefinedRolesResolve(t *testing.T) {
	m := newManagerWithoutStore()
	// Predefined roles must resolve via rolePermissions without a store.
	for _, role := range []string{
		store.WorkspaceAdminRole,
		store.WorkspaceMemberRole,
		store.ConversationMemberRole,
		store.ConversationOwnerRole,
		store.AgentEditorRole,
	} {
		perms := m.rolePermissions(context.Background(), "roles/"+role)
		if perms == nil {
			t.Errorf("role %q resolved nil permissions", role)
		}
	}
}

func TestWorkspaceAdminIsSuperuser(t *testing.T) {
	admin := store.GetPredefinedRole(store.WorkspaceAdminRole).Permissions
	for _, p := range permission.AllPermissions() {
		if !admin[p] {
			t.Errorf("workspaceAdmin missing catalog permission %q", p)
		}
	}
}
