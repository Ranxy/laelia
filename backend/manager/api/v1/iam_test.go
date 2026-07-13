package v1

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect"

	"github.com/Ranxy/laelia/backend/common"
	"github.com/Ranxy/laelia/backend/manager/store"
)

func TestPermissionsForCaller(t *testing.T) {
	admin := permissionsForCaller(true, false)
	if !admin[PermAgentCreate] {
		t.Errorf("admin should have admin-tier permission %q", PermAgentCreate)
	}
	if !admin[PermConversationRead] {
		t.Errorf("admin should have member-tier permission %q", PermConversationRead)
	}

	member := permissionsForCaller(false, false)
	if member[PermAgentCreate] {
		t.Errorf("non-admin user must not have admin-tier permission %q", PermAgentCreate)
	}
	if !member[PermConversationRead] {
		t.Errorf("non-admin user should have member-tier permission %q", PermConversationRead)
	}
	// Agent-edit RPCs are member-tier so a non-admin creator reaches the handler;
	// the handler (requireAgentEditor) enforces creator-or-admin.
	if !member[PermAgentEdit] {
		t.Errorf("non-admin user should have member-tier permission %q", PermAgentEdit)
	}

	agent := permissionsForCaller(false, true)
	if agent[PermAgentCreate] {
		t.Errorf("agent must not have admin-tier permission %q", PermAgentCreate)
	}
	if agent[PermUserUpdate] {
		t.Errorf("agent must not have admin-tier permission %q", PermUserUpdate)
	}
	if !agent[PermConversationRead] {
		t.Errorf("agent should have member-tier permission %q", PermConversationRead)
	}
	// Agents receive the agent-edit member-tier perm at the interceptor, but the
	// handler denies them (user == nil) — the tier grants passage, not rights.
	if !agent[PermAgentEdit] {
		t.Errorf("agent should have member-tier permission %q", PermAgentEdit)
	}

	// An admin flag must not upgrade an agent to admin-tier (agents are never
	// workspace admins); the isAgent branch takes precedence.
	agentAdmin := permissionsForCaller(true, true)
	if agentAdmin[PermAgentCreate] {
		t.Error("agent caller must never receive admin-tier permissions, even if isAdmin=true")
	}
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

func iamCtx(authMethod common.AuthMethod, permission string, allowNoCred bool) context.Context {
	return withAuthContext(context.Background(), &common.AuthContext{
		AuthMethod:             authMethod,
		Permission:             permission,
		AllowWithoutCredential: allowNoCred,
	})
}

func TestAuthorize(t *testing.T) {
	adminUser := &store.UserMessage{ID: 7, Email: "admin@example.com", Name: "admin"}
	plainUser := &store.UserMessage{ID: 8, Email: "bob@example.com", Name: "bob"}
	agent := &store.AgentMessage{ID: 9, ResourceID: "agents/agent-9", Name: "agent"}

	tests := []struct {
		name     string
		ctx      context.Context
		isAdmin  AdminCheckFunc
		wantErr  bool
		wantCode connect.Code
	}{
		{
			name:    "no auth context is not gated",
			ctx:     context.Background(),
			isAdmin: func(context.Context, *store.UserMessage) (bool, error) { return false, nil },
			wantErr: false,
		},
		{
			name:    "allow_without_credential is not gated",
			ctx:     iamCtx(common.AuthMethodIAM, PermAgentCreate, true),
			isAdmin: func(context.Context, *store.UserMessage) (bool, error) { return false, nil },
			wantErr: false,
		},
		{
			name:    "CUSTOM auth method is not gated",
			ctx:     withUser(iamCtx(common.AuthMethodCustom, "", false), plainUser),
			isAdmin: func(context.Context, *store.UserMessage) (bool, error) { return false, nil },
			wantErr: false,
		},
		{
			name:    "IAM with empty permission is not gated",
			ctx:     withUser(iamCtx(common.AuthMethodIAM, "", false), plainUser),
			isAdmin: func(context.Context, *store.UserMessage) (bool, error) { return false, nil },
			wantErr: false,
		},
		{
			name:    "admin perm + admin user passes",
			ctx:     withUser(iamCtx(common.AuthMethodIAM, PermAgentCreate, false), adminUser),
			isAdmin: func(context.Context, *store.UserMessage) (bool, error) { return true, nil },
			wantErr: false,
		},
		{
			name:     "admin perm + non-admin user denied",
			ctx:      withUser(iamCtx(common.AuthMethodIAM, PermAgentCreate, false), plainUser),
			isAdmin:  func(context.Context, *store.UserMessage) (bool, error) { return false, nil },
			wantErr:  true,
			wantCode: connect.CodePermissionDenied,
		},
		{
			name:     "admin perm + agent denied",
			ctx:      withAgent(iamCtx(common.AuthMethodIAM, PermAgentCreate, false), agent),
			isAdmin:  func(context.Context, *store.UserMessage) (bool, error) { return false, nil },
			wantErr:  true,
			wantCode: connect.CodePermissionDenied,
		},
		{
			name:    "agent-edit member perm + non-admin user passes (handler enforces creator-or-admin)",
			ctx:     withUser(iamCtx(common.AuthMethodIAM, PermAgentEdit, false), plainUser),
			isAdmin: func(context.Context, *store.UserMessage) (bool, error) { return false, nil },
			wantErr: false,
		},
		{
			name:    "agent-edit member perm + agent passes (handler denies)",
			ctx:     withAgent(iamCtx(common.AuthMethodIAM, PermAgentEdit, false), agent),
			isAdmin: func(context.Context, *store.UserMessage) (bool, error) { return false, nil },
			wantErr: false,
		},
		{
			name:    "member perm + non-admin user passes",
			ctx:     withUser(iamCtx(common.AuthMethodIAM, PermConversationRead, false), plainUser),
			isAdmin: func(context.Context, *store.UserMessage) (bool, error) { return false, nil },
			wantErr: false,
		},
		{
			name:    "member perm + agent passes",
			ctx:     withAgent(iamCtx(common.AuthMethodIAM, PermConversationRead, false), agent),
			isAdmin: func(context.Context, *store.UserMessage) (bool, error) { return false, nil },
			wantErr: false,
		},
		{
			name:     "IAM perm + no caller unauthenticated",
			ctx:      iamCtx(common.AuthMethodIAM, PermConversationRead, false),
			isAdmin:  func(context.Context, *store.UserMessage) (bool, error) { return false, nil },
			wantErr:  true,
			wantCode: connect.CodeUnauthenticated,
		},
		{
			name:     "admin check error surfaces as internal",
			ctx:      withUser(iamCtx(common.AuthMethodIAM, PermAgentCreate, false), plainUser),
			isAdmin:  func(context.Context, *store.UserMessage) (bool, error) { return false, errors.New("db down") },
			wantErr:  true,
			wantCode: connect.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := newIAMInterceptorWithAdminCheck(tt.isAdmin)
			err := in.authorize(tt.ctx)
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

// TestAdminPermissionsCoverMemberTier is a guard against accidentally dropping
// member-tier permissions from the admin set: admins must be able to do
// everything a member can.
func TestAdminPermissionsCoverMemberTier(t *testing.T) {
	admin := permissionsForCaller(true, false)
	for perm := range memberPermissions {
		if !admin[perm] {
			t.Errorf("admin set missing member-tier permission %q", perm)
		}
	}
}
