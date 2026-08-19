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

// TestParseActivityName guards the "users/{handle}/activities/{message_id}"
// parser used by MarkActivityDone: a well-formed name yields the owning user
// handle and the message UUID, while malformed names (wrong prefix, missing
// segment, non-UUID message id) are rejected. The ownership check itself
// (handle must equal the caller's) lives in the handler; this only covers the
// parse.
func TestParseActivityName(t *testing.T) {
	msgID := uuid.New()
	handle, mid, err := parseActivityName("users/ran-user-1/activities/" + msgID.String())
	require.NoError(t, err)
	assert.Equal(t, "ran-user-1", handle)
	assert.Equal(t, msgID, mid)

	bad := []string{
		"",
		"users/ran-user-1",            // missing activities segment
		"users/ran-user-1/activities", // missing message id
		"channels/42/activities/" + msgID.String(), // wrong user prefix
		"users/ran-user-1/activities/not-a-uuid",   // non-UUID message id
	}
	for _, name := range bad {
		_, _, err := parseActivityName(name)
		assert.Error(t, err, "expected error for %q", name)
	}
}

// TestStoreToV1ActivityState guards the state derivation: DONE takes precedence
// over READ, READ over UNREAD, and the thread_root / read_at / done_at timestamps
// are emitted only when valid. The name carries the viewer's handle so the
// frontend can echo it straight back to MarkActivityDone.
func TestStoreToV1ActivityState(t *testing.T) {
	viewer := 7
	viewerHandle := "ran-user-1"
	msgID := uuid.New()
	convID := uuid.New()
	rootID := uuid.New()

	t.Run("unread by default", func(t *testing.T) {
		a := storeToV1Activity(&store.Activity{
			PrincipalID:    viewer,
			MessageID:      msgID,
			ConversationID: convID,
			SenderType:     store.SenderTypeUser,
		}, viewerHandle)
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
		}, viewerHandle)
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
		}, viewerHandle)
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
		}, viewerHandle)
		assert.Equal(t, rootID.String(), a.ThreadRoot)
		assert.Equal(t, "conversations/"+convID.String(), a.Conversation)
		assert.Equal(t, "conversations/"+convID.String()+"/messages/"+msgID.String(), a.Message)
		assert.Equal(t, "users/ran-user-1/activities/"+rootID.String(), a.Name)
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
		}, viewerHandle)
		assert.Equal(t, "users/ran-user-1/activities/"+msgID.String(), a.Name)
		assert.Equal(t, "conversations/"+convID.String()+"/messages/"+msgID.String(), a.Message)
		assert.Equal(t, "", a.ThreadRoot)
	})
}

// TestMergeMentions guards the union/dedup contract of mergeMentions:
// server-parsed and client mentions are unioned by type:id (first seen wins).
// Self-mentions are NOT dropped here — they are kept so the frontend can render
// @self as a badge; activity generation (generateActivityRows) skips the sender
// so a user never gets a MENTION activity for their own message.
func TestMergeMentions(t *testing.T) {
	// Realistic data: parseContentMentions sets Name to the display text
	// (normally the member's display name), while the client picker may send
	// the handle. Dedup is by type:id so a single Mention per member carries
	// both the handle (Id) and the display text (Name).
	parsed := []*v1pb.Mention{
		{Type: "user", Id: "alice-user-1", Name: "Alice"},
		{Type: "agent", Id: "bot-agent-1", Name: "Bot"},
	}
	client := []*v1pb.Mention{
		{Type: "user", Id: "alice-user-1", Name: "alice-user-1"}, // picker: same member → deduped
		{Type: "user", Id: "bob-user-2", Name: "bob-user-2"},
	}

	merged := mergeMentions(parsed, client)
	assert.Len(t, merged, 3, "dedup by type:id keeps 3 distinct members")
	assert.Equal(t, "Alice", merged[0].Name, "first-seen (server) display name wins on dedup")
	assert.Equal(t, "bot-agent-1", merged[1].Id)
	assert.Equal(t, "bob-user-2", merged[2].Id)

	// Same member, different text tokens (handle + display name) → one entry,
	// with the server-parsed display name preserved.
	handleAndName := mergeMentions(
		[]*v1pb.Mention{{Type: "agent", Id: "jane-agent-1", Name: "jane"}},
		[]*v1pb.Mention{{Type: "agent", Id: "jane-agent-1", Name: "jane-agent-1"}},
	)
	assert.Len(t, handleAndName, 1, "same member with different tokens dedups to one entry")
	assert.Equal(t, "jane", handleAndName[0].Name)

	// Self-mention is KEPT (not dropped) so the frontend can render it; the
	// activity layer is responsible for not notifying the sender.
	withSelf := mergeMentions(parsed, []*v1pb.Mention{{Type: "user", Id: "alice-user-1", Name: "alice-user-1"}})
	found := false
	for _, m := range withSelf {
		if m.Type == "user" && m.Id == "alice-user-1" {
			found = true
		}
	}
	assert.True(t, found, "self-mention must be kept for rendering, not dropped")

	// Agent mention with a user's handle is kept too.
	agentSelf := mergeMentions(nil, []*v1pb.Mention{{Type: "agent", Id: "alice-user-1", Name: "alice-user-1"}})
	assert.Len(t, agentSelf, 1, "agent mention kept")

	// Nil-safe.
	assert.Empty(t, mergeMentions(nil, nil))
}

// sqlNullTime returns a valid sql.NullTime for tests that just need "set".
func sqlNullTime(t *testing.T) sql.NullTime {
	t.Helper()
	return sql.NullTime{Time: timestamppb.Now().AsTime(), Valid: true}
}
