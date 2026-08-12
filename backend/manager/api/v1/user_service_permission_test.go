package v1

import (
	"context"
	"testing"

	"github.com/Ranxy/laelia/backend/common/permission"
	"github.com/Ranxy/laelia/backend/manager/component/iam"
	"github.com/Ranxy/laelia/backend/manager/store"
)

// TestUserUpdatePermission covers the self-or-permission authorization of
// UpdateUser: the target user can always update themselves (the IAM
// interceptor does not gate CUSTOM RPCs), any other caller must hold the
// workspace-scope laelia.users.update permission, and the checker failing
// closed must deny.
func TestUserUpdatePermission(t *testing.T) {
	me := &store.UserMessage{ID: 1, Email: "me@example.com", Name: "me"}
	other := &store.UserMessage{ID: 2, Email: "other@example.com", Name: "other"}

	t.Run("self always allowed", func(t *testing.T) {
		// nil manager would fail closed on any workspace lookup, so a pass
		// proves the self short-circuit runs first.
		ok, err := canUpdateUser(context.Background(), iam.NewManager(nil), me, me)
		if err != nil || !ok {
			t.Fatalf("self update must be allowed, ok=%v err=%v", ok, err)
		}
	})

	t.Run("other denied without permission", func(t *testing.T) {
		ok, err := canUpdateUser(context.Background(), &fakeChecker{}, me, other)
		if err != nil || ok {
			t.Fatalf("non-self update must be denied without the permission, ok=%v err=%v", ok, err)
		}
	})

	t.Run("admin granted via checker", func(t *testing.T) {
		checker := &fakeChecker{adminIDs: map[int]bool{1: true}}
		ok, err := canUpdateUser(context.Background(), checker, me, other)
		if err != nil || !ok {
			t.Fatalf("admin must be allowed to update others, ok=%v err=%v", ok, err)
		}
	})

	t.Run("nil caller denied", func(t *testing.T) {
		ok, err := canUpdateUser(context.Background(), iam.NewManager(nil), nil, me)
		if err != nil || ok {
			t.Fatalf("nil caller must be denied, ok=%v err=%v", ok, err)
		}
	})

	t.Run("nil target denied", func(t *testing.T) {
		ok, err := canUpdateUser(context.Background(), &fakeChecker{}, me, nil)
		if err != nil || ok {
			t.Fatalf("nil target must be denied, ok=%v err=%v", ok, err)
		}
	})
}

// TestUserDeletePermission covers the DeleteUser/UndeleteUser authorization:
// the caller must hold the workspace-scope laelia.users.delete permission
// (there is no self-service exception; deleting your own account is rejected
// by the handler), and the checker failing closed must deny.
func TestUserDeletePermission(t *testing.T) {
	me := &store.UserMessage{ID: 1, Email: "me@example.com", Name: "me"}

	t.Run("admin granted via checker", func(t *testing.T) {
		checker := &fakeChecker{adminIDs: map[int]bool{1: true}}
		ok, err := canDeleteUser(context.Background(), checker, me)
		if err != nil || !ok {
			t.Fatalf("admin must be allowed to delete users, ok=%v err=%v", ok, err)
		}
	})

	t.Run("plain member denied", func(t *testing.T) {
		ok, err := canDeleteUser(context.Background(), &fakeChecker{}, me)
		if err != nil || ok {
			t.Fatalf("plain member must be denied, ok=%v err=%v", ok, err)
		}
	})

	t.Run("nil caller denied", func(t *testing.T) {
		ok, err := canDeleteUser(context.Background(), iam.NewManager(nil), nil)
		if err != nil || ok {
			t.Fatalf("nil caller must be denied, ok=%v err=%v", ok, err)
		}
	})

	t.Run("checker error propagated", func(t *testing.T) {
		checker := &fakeChecker{err: context.DeadlineExceeded}
		if _, err := canDeleteUser(context.Background(), checker, me); err == nil {
			t.Fatal("checker error must be propagated")
		}
	})

	// Guard: UsersDelete must stay out of the member baseline, otherwise
	// delete/undelete become open to every workspace member.
	if store.GetPredefinedRole(store.WorkspaceMemberRole).Permissions[permission.UsersDelete] {
		t.Fatal("laelia.users.delete must not be in the workspaceMember baseline")
	}
}

// TestUserCreatePermission covers the UpdateUser allow_missing fallback: it
// requires the workspace-scope laelia.users.create permission so it cannot be
// used as an open user-creation backdoor by plain members.
func TestUserCreatePermission(t *testing.T) {
	me := &store.UserMessage{ID: 1, Email: "me@example.com", Name: "me"}

	admin := &fakeChecker{adminIDs: map[int]bool{1: true}}
	if ok, err := canCreateUser(context.Background(), admin, me); err != nil || !ok {
		t.Fatalf("admin must be allowed to create users, ok=%v err=%v", ok, err)
	}

	plain := &fakeChecker{}
	if ok, err := canCreateUser(context.Background(), plain, me); err != nil || ok {
		t.Fatalf("plain member must not create users via allow_missing, ok=%v err=%v", ok, err)
	}

	if ok, err := canCreateUser(context.Background(), iam.NewManager(nil), nil); err != nil || ok {
		t.Fatalf("nil caller must be denied, ok=%v err=%v", ok, err)
	}

	// Guard: UsersCreate must stay out of the member baseline, otherwise
	// allow_missing becomes an open user-creation backdoor.
	if store.GetPredefinedRole(store.WorkspaceMemberRole).Permissions[permission.UsersCreate] {
		t.Fatal("laelia.users.create must not be in the workspaceMember baseline")
	}
}
