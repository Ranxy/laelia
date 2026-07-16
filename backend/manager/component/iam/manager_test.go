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
		// Workspace-scope baseline perms (roles/workspaceMember) granted to any
		// authenticated principal, including agents.
		{permission.ConversationsList, true},
		{permission.ConversationsCreate, true},
		{permission.AgentsGet, true},
		{permission.CommandsWatch, true},
		{permission.CommandsCancel, true},
		{permission.RemindersList, true},
		{permission.FilesDownload, true},
		// Per-resource perms are NOT in the baseline: conversations.read/send are
		// granted by the caller's chat role on a specific conversation, and
		// agents.edit by the workspaceAdmin role (or a custom role bound on the
		// agent). With no resource ref they are denied.
		{permission.ConversationsRead, false},
		{permission.ConversationsSend, false},
		{permission.AgentsEdit, false},
		// Review perms are not baseline; only via workspaceAdmin.
		{permission.ConversationsReviewAll, false},
		// Admin-tier workspace perms are not baseline.
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
	// Only the two workspace tiers are predefined; they must resolve via
	// rolePermissions without a store.
	for _, role := range []string{
		store.WorkspaceAdminRole,
		store.WorkspaceMemberRole,
	} {
		perms := m.rolePermissions(context.Background(), "roles/"+role)
		if perms == nil {
			t.Errorf("predefined role %q resolved nil permissions", role)
		}
	}
	// The removed roles (agentEditor, the reviewers) and the chat-membership
	// markers (conversation*) must NOT be predefined — they should not resolve
	// in-memory. Checked via GetPredefinedRole so no store instance is needed.
	for _, role := range []string{
		store.ConversationMemberRole,
		store.ConversationAdminRole,
		store.ConversationOwnerRole,
		store.AgentEditorRole,
		store.AgentDMReviewerRole,
		store.OversightReviewerRole,
	} {
		if store.GetPredefinedRole(role) != nil {
			t.Errorf("role %q must not be predefined", role)
		}
	}
}

// TestChatRolePermissionsResolve checks the chat-membership marker→permission
// maps used by the engine's conversation branch. These are not predefined roles
// (they do not resolve via rolePermissions) but chatRolePermissions must still
// grant the right per-conversation capabilities.
func TestChatRolePermissionsResolve(t *testing.T) {
	cases := []struct {
		role int32
		perm permission.Permission
		want bool
	}{
		{store.MemberRoleMember, permission.ConversationsRead, true},
		{store.MemberRoleMember, permission.ConversationsManage, false},
		{store.MemberRoleAdmin, permission.ConversationsManage, true},
		{store.MemberRoleOwner, permission.ConversationsManage, true},
		{0, permission.ConversationsRead, false},
	}
	for _, c := range cases {
		perms := chatRolePermissions(c.role)
		got := perms != nil && perms[c.perm]
		if got != c.want {
			t.Errorf("chat role %d perm %q: got %v, want %v", c.role, c.perm, got, c.want)
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
