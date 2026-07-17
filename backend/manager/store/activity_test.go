package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestUpsertActivityFolding locks in the thread-folding contract. The guard runs
// on the SQL text (no DB), matching the convention of TestCreateChatMessageBumpVersionSQL:
//   - the ON CONFLICT key is (principal_id, activity_key), so a thread root and its
//     replies (which share the root as activity_key) fold into one row, while a
//     mention (keyed by its own message_id) stays separate.
//   - categories are OR-merged, never overwritten, so a multi-category row keeps
//     every flag.
//   - a genuinely newer message (room_version > stored) bumps the row to the latest
//     message and re-surfaces it as UNREAD by clearing read_at / done / done_at —
//     including resurrecting a Marked-Done row when a new reply arrives. An
//     identical re-run (room_version not newer) leaves read_at/done untouched.
func TestUpsertActivityFolding(t *testing.T) {
	assert.Contains(t, upsertActivitySQL, "ON CONFLICT (principal_id, activity_key) DO UPDATE",
		"UpsertActivity must upsert on the (principal_id, activity_key) PK so a thread folds to one row")
	assert.Contains(t, upsertActivitySQL, "categories = activity.categories | EXCLUDED.categories",
		"UpsertActivity must OR-merge categories, not overwrite, so a multi-category row keeps all flags")
	assert.Contains(t, upsertActivitySQL, "message_id = EXCLUDED.message_id",
		"a folded row must advance its message pointer to the latest message")
	assert.Contains(t, upsertActivitySQL, "WHEN EXCLUDED.room_version > activity.room_version THEN NULL ELSE activity.read_at END",
		"a newer reply must clear read_at (re-surface as UNREAD); an identical re-run must not")
	assert.Contains(t, upsertActivitySQL, "WHEN EXCLUDED.room_version > activity.room_version THEN false ELSE activity.done END",
		"a newer reply must resurrect a Marked-Done row (done=false); an identical re-run must not")
}

// TestMarkActivityDoneScoping guards the two scoping invariants of MarkActivityDone:
//
//  1. principal_id scoping (WHERE principal_id = $1) — a caller can only mark its
//     own row; cross-user marking is prevented at the store layer too, not only
//     by the handler's uid check.
//  2. done = false scoping (idempotent) — marking an already-done row affects 0
//     rows, which surfaces as ErrActivityNotFound rather than resurrecting it.
//
// The row is keyed by activity_key (the stable identity — message id for mentions,
// thread root for folded rows), so a client's held name stays valid after bumps.
func TestMarkActivityDoneScoping(t *testing.T) {
	assert.Contains(t, markActivityDoneSQL, "WHERE principal_id = $1 AND activity_key = $2 AND done = false",
		"MarkActivityDone must scope by principal_id + activity_key and only touch not-done rows")
	assert.Contains(t, markActivityDoneSQL, "SET done = true, done_at = now()",
		"MarkActivityDone must set done + done_at together")
}

// TestMarkConversationActivitiesReadVersionScoping guards the read-sync contract:
// only unread, not-done rows at or below the read cursor flip to READ. The
// done=false guard means a dismissed row is never resurrected as READ; the
// room_version <= bound means a reply newer than the cursor stays UNREAD.
func TestMarkConversationActivitiesReadVersionScoping(t *testing.T) {
	assert.Contains(t, markConversationActivitiesReadSQL, "AND read_at IS NULL",
		"MarkConversationActivitiesRead must only touch unread rows (idempotent)")
	assert.Contains(t, markConversationActivitiesReadSQL, "AND done = false",
		"MarkConversationActivitiesRead must never resurrect a DONE row as READ")
	assert.Contains(t, markConversationActivitiesReadSQL, "AND room_version <= $3",
		"MarkConversationActivitiesRead must scope by the read cursor so newer replies stay unread")
	assert.Contains(t, markConversationActivitiesReadSQL, "WHERE principal_id = $1",
		"MarkConversationActivitiesRead must scope by the owning user")
}

// TestActivityReadStateClause locks in the four read-state filters, including
// the Unspecified default (all visible, done=false) and the Done precedence
// (Done rows are excluded from every non-Done view via done=false).
func TestActivityReadStateClause(t *testing.T) {
	cases := []struct {
		state int32
		want  string
	}{
		{ActivityStateUnread, " AND a.done = false AND a.read_at IS NULL"},
		{ActivityStateRead, " AND a.done = false AND a.read_at IS NOT NULL"},
		{ActivityStateDone, " AND a.done = true"},
		{ActivityStateUnspecified, " AND a.done = false"},
		{99, " AND a.done = false"}, // unknown falls back to the default
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, activityReadStateClause(tc.state))
	}
}
