package store

import (
	"strings"
	"testing"
)

// TestGetConversationMembershipSQL locks in two invariants of
// GetConversationMembership that together prevent a regression of the NULL-scan
// bug (non-member callers got "converting NULL to int32 is unsupported" → 500
// instead of 403, and the non-member reviewAgentDM override was unreachable):
//
//  1. The query LEFT JOINs conversation_member so a non-member yields a row with
//     NULL member_role (not ErrNoRows). An INNER JOIN would make non-members
//     surface as 404/500.
//  2. member_role is the first selected column, so it is the value scanned into
//     the sql.NullInt32 receiver in GetConversationMembership (NULL → 0,
//     the "0 when not a member" contract).
//
// The scan-into-NullInt32 itself is enforced by review of
// GetConversationMembership; this string guard protects the query shape that
// produces the NULL the scan must tolerate.
func TestGetConversationMembershipSQL(t *testing.T) {
	if !strings.Contains(getConversationMembershipSQL, "LEFT JOIN") {
		t.Fatal("GetConversationMembership must LEFT JOIN conversation_member so non-members yield a row with NULL member_role")
	}
	if !strings.HasPrefix(strings.TrimSpace(getConversationMembershipSQL), "SELECT cm.member_role, c.type") {
		t.Fatal("member_role must be the first selected column so it scans into the sql.NullInt32 receiver")
	}
}
