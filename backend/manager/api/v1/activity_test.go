package v1

import (
	"database/sql"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/manager/store"
)

// TestParseActivityName guards the "users/{uid}/activities/{message_id}" parser
// used by MarkActivityDone: a well-formed name yields the owning user id and the
// message UUID, while malformed names (wrong prefix, missing segment, non-numeric
// uid, non-UUID message id) are rejected. The ownership check itself (uid must
// equal the caller's id) lives in the handler; this only covers the parse.
func TestParseActivityName(t *testing.T) {
	msgID := uuid.New()
	uid, mid, err := parseActivityName("users/42/activities/" + msgID.String())
	require.NoError(t, err)
	assert.Equal(t, 42, uid)
	assert.Equal(t, msgID, mid)

	bad := []string{
		"",
		"users/42",            // missing activities segment
		"users/42/activities", // missing message id
		"channels/42/activities/" + msgID.String(), // wrong user prefix
		"users/abc/activities/" + msgID.String(),   // non-numeric uid
		"users/42/activities/not-a-uuid",           // non-UUID message id
	}
	for _, name := range bad {
		_, _, err := parseActivityName(name)
		assert.Error(t, err, "expected error for %q", name)
	}
}

// TestStoreToV1ActivityState guards the state derivation: DONE takes precedence
// over READ, READ over UNREAD, and the thread_root / read_at / done_at timestamps
// are emitted only when valid. The name carries the viewer's uid so the frontend
// can echo it straight back to MarkActivityDone.
func TestStoreToV1ActivityState(t *testing.T) {
	viewer := 7
	msgID := uuid.New()
	convID := uuid.New()
	rootID := uuid.New()

	t.Run("unread by default", func(t *testing.T) {
		a := storeToV1Activity(&store.Activity{
			PrincipalID:    viewer,
			MessageID:      msgID,
			ConversationID: convID,
			SenderType:     store.SenderTypeUser,
		}, viewer)
		assert.Equal(t, v1pb.ActivityState_ACTIVITY_STATE_UNREAD, a.State)
		assert.Nil(t, a.ReadAt)
		assert.Nil(t, a.DoneAt)
		assert.Equal(t, "", a.ThreadRoot)
	})

	t.Run("read when read_at set", func(t *testing.T) {
		a := storeToV1Activity(&store.Activity{
			PrincipalID:    viewer,
			MessageID:      msgID,
			ConversationID: convID,
			SenderType:     store.SenderTypeAgent,
			ReadAt:         sqlNullTime(t),
		}, viewer)
		assert.Equal(t, v1pb.ActivityState_ACTIVITY_STATE_READ, a.State)
		assert.NotNil(t, a.ReadAt)
	})

	t.Run("done takes precedence over read", func(t *testing.T) {
		a := storeToV1Activity(&store.Activity{
			PrincipalID:    viewer,
			MessageID:      msgID,
			ConversationID: convID,
			SenderType:     store.SenderTypeSystem,
			ReadAt:         sqlNullTime(t),
			Done:           true,
			DoneAt:         sqlNullTime(t),
		}, viewer)
		assert.Equal(t, v1pb.ActivityState_ACTIVITY_STATE_DONE, a.State)
		assert.NotNil(t, a.DoneAt)
	})

	t.Run("folded thread row: name is the stable key, message is the latest", func(t *testing.T) {
		// A folded TASK/REMINDER/THREAD row is keyed by the thread root
		// (activity_key = rootID) but points at the latest message (msgID). The
		// name carries the stable key so a client's held reference survives bumps;
		// message carries the latest reply to locate.
		a := storeToV1Activity(&store.Activity{
			PrincipalID:         viewer,
			ActivityKey:         rootID,
			MessageID:           msgID,
			ConversationID:      convID,
			SenderType:          store.SenderTypeUser,
			ThreadRootMessageID: uuid.NullUUID{UUID: rootID, Valid: true},
		}, viewer)
		assert.Equal(t, rootID.String(), a.ThreadRoot)
		assert.Equal(t, "conversations/"+convID.String(), a.Conversation)
		assert.Equal(t, "conversations/"+convID.String()+"/messages/"+msgID.String(), a.Message)
		assert.Equal(t, "users/7/activities/"+rootID.String(), a.Name)
	})

	t.Run("mention row: name and message both the mentioning message", func(t *testing.T) {
		// A MENTION is keyed by its own message id (never folded), so name and
		// message carry the same id.
		a := storeToV1Activity(&store.Activity{
			PrincipalID:    viewer,
			ActivityKey:    msgID,
			MessageID:      msgID,
			ConversationID: convID,
			SenderType:     store.SenderTypeUser,
		}, viewer)
		assert.Equal(t, "users/7/activities/"+msgID.String(), a.Name)
		assert.Equal(t, "conversations/"+convID.String()+"/messages/"+msgID.String(), a.Message)
		assert.Equal(t, "", a.ThreadRoot)
	})
}

// TestMergeMentions guards the union/dedup/self-drop contract of mergeMentions:
// server-parsed and client mentions are unioned by type:id (first seen wins),
// and a self-mention (type=="user" with the caller's own id) is dropped so a
// user never generates a MENTION activity for their own message.
func TestMergeMentions(t *testing.T) {
	parsed := []*v1pb.Mention{{Type: "user", Id: "10", Name: "Alice"}, {Type: "agent", Id: "agent-1", Name: "Bot"}}
	client := []*v1pb.Mention{{Type: "user", Id: "10", Name: "Alice Dup"}, {Type: "user", Id: "20", Name: "Bob"}}

	merged := mergeMentions(parsed, client, 0)
	assert.Len(t, merged, 3, "dedup by type:id keeps 3 distinct members")
	assert.Equal(t, "Alice", merged[0].Name, "first-seen display name wins on dedup")
	assert.Equal(t, "agent-1", merged[1].Id)
	assert.Equal(t, "20", merged[2].Id)

	// Self-mention dropped when selfUserID matches a user mention.
	withSelf := mergeMentions(parsed, []*v1pb.Mention{{Type: "user", Id: "10"}}, 10)
	for _, m := range withSelf {
		assert.False(t, m.Type == "user" && m.Id == "10", "self-mention must be dropped")
	}

	// Agent mention with the caller's user id is NOT dropped (self-drop is user-only).
	agentSelf := mergeMentions(nil, []*v1pb.Mention{{Type: "agent", Id: "10"}}, 10)
	assert.Len(t, agentSelf, 1, "agent mention is not a self-mention even if id collides")

	// Nil-safe.
	assert.Empty(t, mergeMentions(nil, nil, 0))
}

// sqlNullTime returns a valid sql.NullTime for tests that just need "set".
func sqlNullTime(t *testing.T) sql.NullTime {
	t.Helper()
	return sql.NullTime{Time: timestamppb.Now().AsTime(), Valid: true}
}
