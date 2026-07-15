package v1

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect"

	"github.com/Ranxy/laelia/backend/common"
	"github.com/Ranxy/laelia/backend/common/permission"
	"github.com/Ranxy/laelia/backend/manager/component/iam"
	"github.com/Ranxy/laelia/backend/manager/store"
)

// fakeChecker is a PermissionChecker that reproduces the Phase 1 behavior of
// iam.Manager without a database: any authenticated principal gets the
// workspaceMember baseline (the implicit allUsers->workspaceMember binding),
// and a user flagged as admin additionally gets every permission
// (roles/workspaceAdmin). Agents never get admin-tier permissions. This lets
// the interceptor's wiring (auth context handling, error codes, CUSTOM bypass)
// be unit-tested in isolation from the IAM store.
type fakeChecker struct {
	adminIDs map[int]bool
	err      error
}

func (f *fakeChecker) CheckPermission(_ context.Context, p permission.Permission, user *store.UserMessage, _ *store.AgentMessage, _ *iam.ResourceRef) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	// Baseline: every authenticated principal gets the workspaceMember set.
	baseline := store.GetPredefinedRole(store.WorkspaceMemberRole).Permissions
	if baseline[p] {
		return true, nil
	}
	// Admin users additionally get the workspaceAdmin (superuser) set.
	if user != nil && f.adminIDs[user.ID] {
		return store.GetPredefinedRole(store.WorkspaceAdminRole).Permissions[p], nil
	}
	// Agents never get anything beyond the baseline.
	return false, nil
}

func withAuthContext(ctx context.Context, authCtx *common.AuthContext) context.Context {
	return context.WithValue(ctx, common.AuthContextKey, authCtx)
}

func withUser(ctx context.Context, u *store.UserMessage) context.Context {
	return context.WithValue(ctx, common.UserContextKey, u)
}

func withAgent(ctx context.Context, a *store.AgentMessage) context.Context {
	return context.WithValue(ctx, common.AgentContextKey, a)
}

func iamCtx(authMethod common.AuthMethod, perm string, allowNoCred bool) context.Context {
	return withAuthContext(context.Background(), &common.AuthContext{
		AuthMethod:             authMethod,
		Permission:             perm,
		AllowWithoutCredential: allowNoCred,
	})
}

func TestAuthorize(t *testing.T) {
	adminUser := &store.UserMessage{ID: 7, Email: "admin@example.com", Name: "admin"}
	plainUser := &store.UserMessage{ID: 8, Email: "bob@example.com", Name: "bob"}
	agent := &store.AgentMessage{ID: 9, ResourceID: "agents/agent-9", Name: "agent"}

	adminChecker := &fakeChecker{adminIDs: map[int]bool{7: true}}

	tests := []struct {
		name     string
		ctx      context.Context
		checker  PermissionChecker
		wantErr  bool
		wantCode connect.Code
	}{
		{
			name:    "no auth context is not gated",
			ctx:     context.Background(),
			checker: adminChecker,
			wantErr: false,
		},
		{
			name:    "allow_without_credential is not gated",
			ctx:     iamCtx(common.AuthMethodIAM, string(permission.AgentsCreate), true),
			checker: adminChecker,
			wantErr: false,
		},
		{
			name:    "CUSTOM auth method is not gated",
			ctx:     withUser(iamCtx(common.AuthMethodCustom, "", false), plainUser),
			checker: adminChecker,
			wantErr: false,
		},
		{
			name:    "IAM with empty permission is not gated",
			ctx:     withUser(iamCtx(common.AuthMethodIAM, "", false), plainUser),
			checker: adminChecker,
			wantErr: false,
		},
		{
			name:    "admin perm + admin user passes",
			ctx:     withUser(iamCtx(common.AuthMethodIAM, string(permission.AgentsCreate), false), adminUser),
			checker: adminChecker,
			wantErr: false,
		},
		{
			name:     "admin perm + non-admin user denied",
			ctx:      withUser(iamCtx(common.AuthMethodIAM, string(permission.AgentsCreate), false), plainUser),
			checker:  adminChecker,
			wantErr:  true,
			wantCode: connect.CodePermissionDenied,
		},
		{
			name:     "admin perm + agent denied",
			ctx:      withAgent(iamCtx(common.AuthMethodIAM, string(permission.AgentsCreate), false), agent),
			checker:  adminChecker,
			wantErr:  true,
			wantCode: connect.CodePermissionDenied,
		},
		{
			name:    "agent-edit member perm + non-admin user passes (handler enforces creator-or-admin)",
			ctx:     withUser(iamCtx(common.AuthMethodIAM, string(permission.AgentsEdit), false), plainUser),
			checker: adminChecker,
			wantErr: false,
		},
		{
			name:    "agent-edit member perm + agent passes (handler denies)",
			ctx:     withAgent(iamCtx(common.AuthMethodIAM, string(permission.AgentsEdit), false), agent),
			checker: adminChecker,
			wantErr: false,
		},
		{
			name:    "member perm + non-admin user passes",
			ctx:     withUser(iamCtx(common.AuthMethodIAM, string(permission.ConversationsRead), false), plainUser),
			checker: adminChecker,
			wantErr: false,
		},
		{
			name:    "member perm + agent passes",
			ctx:     withAgent(iamCtx(common.AuthMethodIAM, string(permission.ConversationsRead), false), agent),
			checker: adminChecker,
			wantErr: false,
		},
		{
			name:     "IAM perm + no caller unauthenticated",
			ctx:      iamCtx(common.AuthMethodIAM, string(permission.ConversationsRead), false),
			checker:  adminChecker,
			wantErr:  true,
			wantCode: connect.CodeUnauthenticated,
		},
		{
			name:     "checker error surfaces as internal",
			ctx:      withUser(iamCtx(common.AuthMethodIAM, string(permission.AgentsCreate), false), plainUser),
			checker:  &fakeChecker{err: errors.New("db down")},
			wantErr:  true,
			wantCode: connect.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := newIAMInterceptorWithChecker(tt.checker)
			err := in.authorize(tt.ctx, nil)
			if !tt.wantErr {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error code %s, got nil", tt.wantCode)
			}
			connErr, ok := err.(*connect.Error)
			if !ok {
				t.Fatalf("expected *connect.Error, got %T: %v", err, err)
			}
			if connErr.Code() != tt.wantCode {
				t.Fatalf("expected code %s, got %s: %v", tt.wantCode, connErr.Code(), err)
			}
		})
	}
}

// TestWorkspaceAdminCoversMemberTier guards against accidentally dropping
// member-tier permissions from the admin set: admins must be able to do
// everything a member can. workspaceAdmin is the superuser role (union of all
// permissions), so it must be a superset of workspaceMember.
func TestWorkspaceAdminCoversMemberTier(t *testing.T) {
	admin := store.GetPredefinedRole(store.WorkspaceAdminRole).Permissions
	member := store.GetPredefinedRole(store.WorkspaceMemberRole).Permissions
	for perm := range member {
		if !admin[perm] {
			t.Errorf("workspaceAdmin missing member-tier permission %q", perm)
		}
	}
}
