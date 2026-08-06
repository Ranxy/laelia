package permission

import (
	"encoding/json"
	"os"
	"testing"
)

// TestCatalogMatchesSource guards the single-source invariant: the generated
// catalog (constants + AllPermissions) must exactly match permission.json, so
// a hand edit of the generated file cannot silently add or drop a permission.
func TestCatalogMatchesSource(t *testing.T) {
	data, err := os.ReadFile("permission.json")
	if err != nil {
		t.Fatal(err)
	}
	var c struct {
		Permissions []struct {
			Name string `json:"name"`
			ID   string `json:"id"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	if len(c.Permissions) != len(AllPermissions()) {
		t.Fatalf("catalog size mismatch: json=%d generated=%d", len(c.Permissions), len(AllPermissions()))
	}
	seen := map[string]bool{}
	for _, p := range c.Permissions {
		seen[p.ID] = true
		if !Exist(p.ID) {
			t.Errorf("permission %q from permission.json is missing from the generated catalog", p.ID)
		}
	}
	for _, p := range AllPermissions() {
		if !seen[p] {
			t.Errorf("permission %q exists in the generated catalog but not in permission.json", p)
		}
	}
	if Exist("laelia.does.not.exist") {
		t.Error("unknown permission must not exist")
	}
}

// TestManageMembersIsResourceScoped guards the interceptor contract: the
// manageMembers permission is authorized per-conversation (via the caller's chat
// role or an agent's owner-follow), so the IAM interceptor must resolve the
// request's conversation resource for it.
func TestManageMembersIsResourceScoped(t *testing.T) {
	if !IsResourceScoped(ConversationsManageMembers) {
		t.Error("conversations.manageMembers must be resource-scoped")
	}
}
