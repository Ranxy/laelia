package v1

import (
	"testing"

	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/manager/store"
)

// TestConvertToGroupPayload verifies member role conversion and rejection of
// unspecified roles.
func TestConvertToGroupPayload(t *testing.T) {
	payload, err := convertToGroupPayload([]*v1pb.GroupMember{
		{Member: "users/101", Role: v1pb.GroupMemberRole_OWNER},
		{Member: "users/102", Role: v1pb.GroupMemberRole_MEMBER},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !hasGroupOwner(payload) {
		t.Fatal("expected payload to have an owner")
	}
	if len(payload.GetMembers()) != 2 {
		t.Fatalf("expected 2 members, got %d", len(payload.GetMembers()))
	}

	if _, err := convertToGroupPayload([]*v1pb.GroupMember{
		{Member: "users/101", Role: v1pb.GroupMemberRole_GROUP_MEMBER_ROLE_UNSPECIFIED},
	}); err == nil {
		t.Fatal("expected unspecified role to be rejected")
	}

	if _, err := convertToGroupPayload([]*v1pb.GroupMember{
		{Member: "users/101", Role: v1pb.GroupMemberRole_OWNER},
		{Member: "users/101", Role: v1pb.GroupMemberRole_MEMBER},
	}); err == nil {
		t.Fatal("expected duplicate member to be rejected")
	}

	if _, err := convertToGroupPayload([]*v1pb.GroupMember{
		{Member: "", Role: v1pb.GroupMemberRole_OWNER},
	}); err == nil {
		t.Fatal("expected empty member to be rejected")
	}
}

// TestHasGroupOwner verifies the last-owner guard input.
func TestHasGroupOwner(t *testing.T) {
	noOwner := &storepb.GroupPayload{Members: []*storepb.GroupMember{{Member: "users/1", Role: storepb.GroupMember_MEMBER}}}
	if hasGroupOwner(noOwner) {
		t.Fatal("expected no owner")
	}
}

// TestIsGroupOwner verifies the owner-of-record check used by the group
// management handlers.
func TestIsGroupOwner(t *testing.T) {
	group := &store.GroupMessage{
		Payload: &storepb.GroupPayload{Members: []*storepb.GroupMember{
			{Member: "users/7", Role: storepb.GroupMember_OWNER},
			{Member: "users/8", Role: storepb.GroupMember_MEMBER},
		}},
	}
	owner := &store.UserMessage{ID: 7}
	member := &store.UserMessage{ID: 8}
	if !isGroupOwner(owner, group) {
		t.Fatal("expected user 7 to be owner")
	}
	if isGroupOwner(member, group) {
		t.Fatal("expected user 8 not to be owner")
	}
	if isGroupOwner(nil, group) {
		t.Fatal("nil user must not be owner")
	}
}

// TestParseGroupFilter verifies the supported equality filters build a
// parameterized SQL filter and unknown fields are rejected.
func TestParseGroupFilter(t *testing.T) {
	find := &store.FindGroupMessage{}
	if err := parseGroupFilter(find, `title = "Engineering"`); err != nil {
		t.Fatal(err)
	}
	if find.Filter == nil || find.Filter.Where != "name = $1" {
		t.Fatalf("unexpected filter: %+v", find.Filter)
	}
	if err := parseGroupFilter(find, `project = "projects/x"`); err == nil {
		t.Fatal("expected unsupported field to be rejected")
	}
}
