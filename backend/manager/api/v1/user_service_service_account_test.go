package v1

import (
	"context"
	"testing"

	"connectrpc.com/connect"

	"github.com/Ranxy/laelia/backend/common/permission"
	"github.com/Ranxy/laelia/backend/manager/store"
)

// TestAuthorizeServiceAccountCreation covers the CreateUser gate that keeps
// service-account creation admin-only. Before the fix, the anonymous
// CreateUser RPC (self-service signup) accepted user_type=SERVICE_ACCOUNT and
// returned the freshly generated access key, which could immediately be
// exchanged for a JWT via Login - anonymous self-issued credentials.
func TestAuthorizeServiceAccountCreation(t *testing.T) {
	admin := &store.UserMessage{ID: 1, Email: "admin@example.com", Name: "admin"}
	plain := &store.UserMessage{ID: 2, Email: "bob@example.com", Name: "bob"}
	agent := &store.AgentMessage{ID: 3, ResourceID: "agents/agent-3", Name: "agent"}

	t.Run("anonymous denied", func(t *testing.T) {
		err := authorizeServiceAccountCreation(context.Background(), &fakeChecker{})
		if connect.CodeOf(err) != connect.CodePermissionDenied {
			t.Fatalf("anonymous caller must be denied, got %v", err)
		}
	})

	t.Run("agent denied", func(t *testing.T) {
		err := authorizeServiceAccountCreation(withAgent(context.Background(), agent), &fakeChecker{})
		if connect.CodeOf(err) != connect.CodePermissionDenied {
			t.Fatalf("agent caller must be denied, got %v", err)
		}
	})

	t.Run("plain member denied", func(t *testing.T) {
		err := authorizeServiceAccountCreation(withUser(context.Background(), plain), &fakeChecker{})
		if connect.CodeOf(err) != connect.CodePermissionDenied {
			t.Fatalf("plain member must be denied, got %v", err)
		}
	})

	t.Run("workspace admin allowed", func(t *testing.T) {
		err := authorizeServiceAccountCreation(withUser(context.Background(), admin), &fakeChecker{adminIDs: map[int]bool{1: true}})
		if err != nil {
			t.Fatalf("workspace admin must be allowed, got %v", err)
		}
	})

	t.Run("checker failure fails closed", func(t *testing.T) {
		checker := &fakeChecker{err: context.DeadlineExceeded}
		err := authorizeServiceAccountCreation(withUser(context.Background(), admin), checker)
		if connect.CodeOf(err) != connect.CodeInternal {
			t.Fatalf("checker failure must surface as CodeInternal, got %v", err)
		}
	})

	// Guard: UsersCreate must stay out of the member baseline, otherwise any
	// authenticated member could self-issue a service account credential.
	if store.GetPredefinedRole(store.WorkspaceMemberRole).Permissions[permission.UsersCreate] {
		t.Fatal("laelia.users.create must not be in the workspaceMember baseline")
	}
}
