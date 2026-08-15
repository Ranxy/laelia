package store

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// TestPlanActivityUpserts locks in how DM folding interacts with task/reminder
// threads. Plain top-level DM messages keep the single per-chat row (keyed by
// the conversation, no thread_root), but a task/reminder root or any thread
// reply in a DM must get a thread-rooted row keyed by the thread root —
// otherwise Activity opens the main channel list, which excludes thread
// replies, and the user only sees the outer system notification instead of the
// work happening inside the task.
func TestPlanActivityUpserts(t *testing.T) {
	msgID := uuid.New()
	convID := uuid.New()
	rootID := uuid.New()

	t.Run("plain DM message folds into the per-chat row", func(t *testing.T) {
		targets := planActivityUpserts(
			true, false, msgID, convID, uuid.Nil,
			map[int]int32{7: ActivityCategoryDirect},
			map[int]int32{},
		)
		require.Len(t, targets, 1)
		assert.Equal(t, convID, targets[0].key)
		assert.False(t, targets[0].root.Valid)
		assert.Equal(t, int32(ActivityCategoryDirect), targets[0].cats)
	})

	t.Run("DM task root gets a thread-rooted row", func(t *testing.T) {
		targets := planActivityUpserts(
			true, true, msgID, convID, msgID,
			map[int]int32{7: ActivityCategoryMention},
			map[int]int32{7: ActivityCategoryTask},
		)
		require.Len(t, targets, 1)
		assert.Equal(t, msgID, targets[0].key)
		assert.True(t, targets[0].root.Valid)
		assert.Equal(t, msgID, targets[0].root.UUID)
		assert.Equal(t, int32(ActivityCategoryTask|ActivityCategoryMention), targets[0].cats)
	})

	t.Run("DM thread reply merges a reply mention into the folded row", func(t *testing.T) {
		targets := planActivityUpserts(
			true, true, msgID, convID, rootID,
			map[int]int32{7: ActivityCategoryMention},
			map[int]int32{7: ActivityCategoryTask | ActivityCategoryThread},
		)
		require.Len(t, targets, 1)
		assert.Equal(t, rootID, targets[0].key)
		assert.True(t, targets[0].root.Valid)
		assert.Equal(t, rootID, targets[0].root.UUID)
		assert.Equal(t, int32(ActivityCategoryTask|ActivityCategoryThread|ActivityCategoryMention), targets[0].cats)
	})

	t.Run("channel task root keeps the same thread-rooted row shape", func(t *testing.T) {
		targets := planActivityUpserts(
			false, true, msgID, convID, msgID,
			map[int]int32{},
			map[int]int32{7: ActivityCategoryTask},
		)
		require.Len(t, targets, 1)
		assert.Equal(t, msgID, targets[0].key)
		assert.True(t, targets[0].root.Valid)
		assert.Equal(t, msgID, targets[0].root.UUID)
		assert.Equal(t, int32(ActivityCategoryTask), targets[0].cats)
	})
}

// TestExcludeSenderFromActivity locks in the sender-exclusion contract: a user
// is dropped from MENTION and THREAD (self-notifications) but keeps TASK and
// REMINDER, so the creator of a task/reminder still sees it in their own
// activity feed (mirroring agent-created tasks, where the owner sees the task
// because the agent sender is never in the user sets).
func TestExcludeSenderFromActivity(t *testing.T) {
	t.Run("task creator keeps TASK", func(t *testing.T) {
		mention := map[int]int32{7: ActivityCategoryMention}
		thread := map[int]int32{7: ActivityCategoryTask}
		excludeSenderFromActivity(mention, thread, 7)
		assert.Equal(t, int32(ActivityCategoryTask), thread[7],
			"the task creator must keep TASK activity")
		_, ok := mention[7]
		assert.False(t, ok, "self-mention must be dropped")
	})

	t.Run("reminder creator keeps REMINDER", func(t *testing.T) {
		mention := map[int]int32{}
		thread := map[int]int32{7: ActivityCategoryReminder}
		excludeSenderFromActivity(mention, thread, 7)
		assert.Equal(t, int32(ActivityCategoryReminder), thread[7],
			"the reminder creator must keep REMINDER activity")
	})

	t.Run("thread reply drops THREAD", func(t *testing.T) {
		mention := map[int]int32{}
		thread := map[int]int32{7: ActivityCategoryThread}
		excludeSenderFromActivity(mention, thread, 7)
		_, ok := thread[7]
		assert.False(t, ok, "a user must not get THREAD activity for their own reply")
	})

	t.Run("task reply keeps TASK but drops THREAD", func(t *testing.T) {
		mention := map[int]int32{}
		thread := map[int]int32{7: ActivityCategoryTask | ActivityCategoryThread}
		excludeSenderFromActivity(mention, thread, 7)
		assert.Equal(t, int32(ActivityCategoryTask), thread[7],
			"a reply to one's own task keeps TASK but drops THREAD")
	})

	t.Run("other users are untouched", func(t *testing.T) {
		mention := map[int]int32{8: ActivityCategoryMention}
		thread := map[int]int32{8: ActivityCategoryTask}
		excludeSenderFromActivity(mention, thread, 7)
		assert.Equal(t, int32(ActivityCategoryMention), mention[8])
		assert.Equal(t, int32(ActivityCategoryTask), thread[8])
	})
}
