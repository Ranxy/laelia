package main

import (
	"context"
	"fmt"
	"time"

	"github.com/Ranxy/laelia/backend/common"
	models "github.com/Ranxy/laelia/backend/generated-go/store"
	"github.com/Ranxy/laelia/backend/manager/store"
	"golang.org/x/crypto/bcrypt"
)

// seedUsers writes the preset test users into the database and grants the
// admin user the workspaceAdmin role. It reuses the store package so it goes
// through the exact same code paths as the API. It is idempotent: existing
// users are left untouched.
func seedUsers(ctx context.Context, pgURL string, users []seedUser) error {
	st, err := store.New(ctx, pgURL, false)
	if err != nil {
		return fmt.Errorf("failed to open store for seeding: %w", err)
	}
	defer st.Close()

	var admin *store.UserMessage
	for _, u := range users {
		existing, err := st.GetUserByEmail(ctx, u.Email)
		if err != nil {
			return fmt.Errorf("failed to look up user %q: %w", u.Email, err)
		}
		if existing != nil {
			if u.Admin {
				admin = existing
			}
			continue
		}

		hash, err := bcrypt.GenerateFromPassword([]byte(u.Password), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("failed to hash password for %q: %w", u.Email, err)
		}
		verifiedAt := time.Now()
		created, err := st.CreateUser(ctx, &store.UserMessage{
			Name:            u.Name,
			Email:           u.Email,
			Type:            models.PrincipalType_END_USER,
			PasswordHash:    string(hash),
			EmailVerifiedAt: &verifiedAt,
		})
		if err != nil {
			return fmt.Errorf("failed to create user %q: %w", u.Email, err)
		}
		if u.Admin {
			admin = created
		}
	}

	if admin == nil {
		return fmt.Errorf("no admin user to grant workspaceAdmin role")
	}
	if _, err := st.PatchWorkspaceIamPolicy(ctx, &store.PatchIamPolicyMessage{
		Member: common.FormatUserHandle(admin.Handle),
		Roles:  []string{common.FormatRole(common.WorkspaceAdmin)},
	}); err != nil {
		return fmt.Errorf("failed to grant workspaceAdmin to %q: %w", admin.Email, err)
	}
	return nil
}
