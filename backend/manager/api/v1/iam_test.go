package v1

import (
	"context"
	"errors"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"google.golang.org/genproto/googleapis/type/expr"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/Ranxy/laelia/backend/common"
	"github.com/Ranxy/laelia/backend/common/permission"
	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
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
			name:    "baseline perm + non-admin user passes",
			ctx:     withUser(iamCtx(common.AuthMethodIAM, string(permission.ConversationsList), false), plainUser),
			checker: adminChecker,
			wantErr: false,
		},
		{
			name:    "baseline perm + agent passes",
			ctx:     withAgent(iamCtx(common.AuthMethodIAM, string(permission.ConversationsList), false), agent),
			checker: adminChecker,
			wantErr: false,
		},
		{
			name:     "per-resource perm + non-admin user denied without resource",
			ctx:      withUser(iamCtx(common.AuthMethodIAM, string(permission.AgentsEdit), false), plainUser),
			checker:  adminChecker,
			wantErr:  true,
			wantCode: connect.CodePermissionDenied,
		},
		{
			name:     "per-resource perm + agent denied without resource",
			ctx:      withAgent(iamCtx(common.AuthMethodIAM, string(permission.AgentsEdit), false), agent),
			checker:  adminChecker,
			wantErr:  true,
			wantCode: connect.CodePermissionDenied,
		},
		{
			name:     "conversations.read + non-admin user denied without resource",
			ctx:      withUser(iamCtx(common.AuthMethodIAM, string(permission.ConversationsRead), false), plainUser),
			checker:  adminChecker,
			wantErr:  true,
			wantCode: connect.CodePermissionDenied,
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
			err := in.authorize(tt.ctx, nil, "/laelia.v1.Test/Test")
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

// TestAuthorizeDenialDetail verifies that a denied IAM-gated request carries a
// PermissionDeniedDetail with the method, the required permission, and any
// resource the check ran against.
func TestAuthorizeDenialDetail(t *testing.T) {
	plainUser := &store.UserMessage{ID: 8, Email: "bob@example.com", Name: "bob"}
	checker := &fakeChecker{}
	ctx := withUser(iamCtx(common.AuthMethodIAM, string(permission.AgentsCreate), false), plainUser)
	in := newIAMInterceptorWithChecker(checker)

	err := in.authorize(ctx, nil, "/laelia.v1.AgentService/CreateAgent")
	connErr, ok := err.(*connect.Error)
	if !ok {
		t.Fatalf("expected *connect.Error, got %T: %v", err, err)
	}
	if connErr.Code() != connect.CodePermissionDenied {
		t.Fatalf("expected PermissionDenied, got %s", connErr.Code())
	}

	found := false
	for _, d := range connErr.Details() {
		msg, err := d.Value()
		if err != nil {
			continue
		}
		if detail, ok := msg.(*v1pb.PermissionDeniedDetail); ok {
			found = true
			if detail.GetMethod() != "/laelia.v1.AgentService/CreateAgent" {
				t.Errorf("unexpected method %q", detail.GetMethod())
			}
			if len(detail.GetRequiredPermissions()) != 1 || detail.GetRequiredPermissions()[0] != string(permission.AgentsCreate) {
				t.Errorf("unexpected required permissions %v", detail.GetRequiredPermissions())
			}
		}
	}
	if !found {
		t.Fatal("expected PermissionDeniedDetail on the denied error")
	}
}

// TestFindIamPolicyDeltas verifies ADD/REMOVE detection across member-role
// condition triples and deterministic ordering.
func TestFindIamPolicyDeltas(t *testing.T) {
	cond := &expr.Expr{Expression: `request.time < timestamp("2026-12-31T00:00:00Z")`}
	oldPolicy := &storepb.IamPolicy{
		Bindings: []*storepb.Binding{
			{Role: "roles/editor", Members: []string{"users/101", "users/102"}},
			{Role: "roles/viewer", Members: []string{"users/103"}, Condition: cond},
		},
	}
	newPolicy := &storepb.IamPolicy{
		Bindings: []*storepb.Binding{
			{Role: "roles/editor", Members: []string{"users/101"}},
			{Role: "roles/viewer", Members: []string{"users/103"}, Condition: cond},
			{Role: "roles/reader", Members: []string{"allUsers"}},
		},
	}

	deltas := findIamPolicyDeltas(oldPolicy, newPolicy)
	if len(deltas) != 2 {
		t.Fatalf("expected 2 deltas (remove 102, add allUsers), got %d: %+v", len(deltas), deltas)
	}
	byMember := map[string]*v1pb.BindingDelta{}
	for _, d := range deltas {
		byMember[d.GetMember()] = d
	}
	if d := byMember["users/102"]; d == nil || d.GetAction() != v1pb.BindingDelta_REMOVE || d.GetRole() != "roles/editor" {
		t.Errorf("expected remove users/102 from roles/editor, got %+v", d)
	}
	if d := byMember["allUsers"]; d == nil || d.GetAction() != v1pb.BindingDelta_ADD || d.GetRole() != "roles/reader" {
		t.Errorf("expected add allUsers to roles/reader, got %+v", d)
	}
	// Unchanged triple (users/103 + condition) must not produce a delta.
	if d := byMember["users/103"]; d != nil {
		t.Errorf("unexpected delta for unchanged binding %+v", d)
	}
}

// TestServiceDataAuditFields verifies that an IamPolicyChange attached by a
// handler is surfaced as resource + JSON payload for the audit record.
func TestServiceDataAuditFields(t *testing.T) {
	a, err := anypb.New(&v1pb.IamPolicyChange{
		Resource: "agents/agent-1",
		BindingDeltas: []*v1pb.BindingDelta{
			{Action: v1pb.BindingDelta_ADD, Member: "users/101", Role: "roles/editor"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	resource, payload := serviceDataAuditFields(a)
	if resource != "agents/agent-1" {
		t.Errorf("unexpected resource %q", resource)
	}
	if payload == "" || !strings.Contains(payload, `"bindingDeltas"`) {
		t.Errorf("unexpected payload %q", payload)
	}

	if resource, payload := serviceDataAuditFields(nil); resource != "" || payload != "" {
		t.Errorf("nil Any should yield empty fields, got %q %q", resource, payload)
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
