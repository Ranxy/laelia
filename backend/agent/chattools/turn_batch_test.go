package chattools

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
)

// fakeBatchClient implements only the three CommandServiceClient methods
// BuildTurnBatch calls (ListChannelUpdates, GetChannel, ListConversationMessages)
// by embedding the interface and overriding those three; the rest stay nil and
// are never reached.
type fakeBatchClient struct {
	v1connect.CommandServiceClient
	updates  []*v1pb.ChannelUpdate
	channels map[string]*v1pb.Conversation
	messages map[string][]*v1pb.ChatMessage
	err      error // injected error for any call
}

func (f *fakeBatchClient) ListChannelUpdates(_ context.Context, _ *connect.Request[v1pb.ListChannelUpdatesRequest]) (*connect.Response[v1pb.ListChannelUpdatesResponse], error) {
	if f.err != nil {
		return nil, f.err
	}
	return connect.NewResponse(&v1pb.ListChannelUpdatesResponse{Updates: f.updates}), nil
}

func (f *fakeBatchClient) GetChannel(_ context.Context, req *connect.Request[v1pb.GetChannelRequest]) (*connect.Response[v1pb.Conversation], error) {
	if f.err != nil {
		return nil, f.err
	}
	conv, ok := f.channels[req.Msg.GetName()]
	if !ok {
		conv = &v1pb.Conversation{Name: req.Msg.GetName()}
	}
	return connect.NewResponse(conv), nil
}

func (f *fakeBatchClient) ListConversationMessages(_ context.Context, req *connect.Request[v1pb.ListConversationMessagesRequest]) (*connect.Response[v1pb.ListConversationMessagesResponse], error) {
	if f.err != nil {
		return nil, f.err
	}
	msgs := f.messages[req.Msg.GetConversation()]
	// Honor the page-size bound like the real store does.
	limit := int(req.Msg.GetPageSize())
	if limit > 0 && len(msgs) > limit {
		// Newest `limit` (the batch uses beforeVersion paging when count > limit).
		msgs = msgs[len(msgs)-limit:]
	}
	return connect.NewResponse(&v1pb.ListConversationMessagesResponse{Messages: msgs}), nil
}

func batchDeps(c *fakeBatchClient) Deps {
	return Deps{Client: c, Agent: "agents/rei"}
}

func mkMsg(name, content, sender string, st v1pb.SenderType) *v1pb.ChatMessage {
	return &v1pb.ChatMessage{
		Name:       name,
		Content:    content,
		SenderName: sender,
		SenderType: st,
		CreatedAt:  timestamppb.Now(),
	}
}

// TestBuildTurnBatch_EmptyReturnsReminderNudge guards the warm-reminder path: a
// turn opened for a due reminder (no unread channel) must return a non-empty
// nudge so a resumed turn still runs `reminder list-due` (the init prompt's
// step 0 is only sent once, at cold start).
func TestBuildTurnBatch_EmptyReturnsReminderNudge(t *testing.T) {
	out, err := BuildTurnBatch(context.Background(), batchDeps(&fakeBatchClient{}))
	require.NoError(t, err)
	assert.NotEmpty(t, out)
	assert.Contains(t, out, reminderNudge)
	assert.Contains(t, out, "No new channel messages")
}

// TestBuildTurnBatch_RendersTargetAndSender verifies the [target=... msg=...
// time=... type=...] header form for a DM (dm:@peer) and a channel (#title),
// including the system-sender label "@system".
func TestBuildTurnBatch_RendersTargetAndSender(t *testing.T) {
	dm := "conversations/dm-alice"
	ch := "conversations/img"
	c := &fakeBatchClient{
		updates: []*v1pb.ChannelUpdate{
			{Conversation: dm, NewMessageCount: 1, ProcessedVersion: 0, CurrentVersion: 1},
			{Conversation: ch, NewMessageCount: 1, ProcessedVersion: 0, CurrentVersion: 1},
		},
		channels: map[string]*v1pb.Conversation{
			dm: {Name: dm, Type: 1, OwnerName: "alice"},
			ch: {Name: ch, Type: 2, Title: "image"},
		},
		messages: map[string][]*v1pb.ChatMessage{
			dm: {mkMsg("conversations/dm-alice/messages/66390317", "@REI", "alice", v1pb.SenderType_SENDER_TYPE_USER)},
			ch: {mkMsg("conversations/img/messages/e5a69e1f", "ran converted a message to task #1", "system", v1pb.SenderType_SENDER_TYPE_SYSTEM)},
		},
	}

	out, err := BuildTurnBatch(context.Background(), batchDeps(c))
	require.NoError(t, err)
	assert.Contains(t, out, "New messages received:")
	assert.Contains(t, out, "[target=dm:@alice msg=66390317")
	assert.Contains(t, out, "type=human")
	assert.Contains(t, out, "@alice: @REI")
	assert.Contains(t, out, "[target=#image msg=e5a69e1f")
	assert.Contains(t, out, "type=system")
	assert.Contains(t, out, "@system:")
	assert.Contains(t, out, reminderNudge)
}

// TestBuildTurnBatch_ChannelBoundSummarizesOverflow guards the no-silent-drop
// invariant: a 6th unread channel (beyond turnBatchMaxChannels=5) must be listed
// as an unread count rather than dropped.
func TestBuildTurnBatch_ChannelBoundSummarizesOverflow(t *testing.T) {
	const n = turnBatchMaxChannels + 1
	updates := make([]*v1pb.ChannelUpdate, 0, n)
	channels := make(map[string]*v1pb.Conversation, n)
	messages := make(map[string][]*v1pb.ChatMessage, n)
	for i := 0; i < n; i++ {
		name := "conversations/c" + itoa(i)
		updates = append(updates, &v1pb.ChannelUpdate{Conversation: name, NewMessageCount: 1, ProcessedVersion: 0, CurrentVersion: 1})
		channels[name] = &v1pb.Conversation{Name: name, Type: 2, Title: "c" + itoa(i)}
		messages[name] = []*v1pb.ChatMessage{mkMsg(name+"/messages/1", "hi", "u", v1pb.SenderType_SENDER_TYPE_USER)}
	}
	c := &fakeBatchClient{updates: updates, channels: channels, messages: messages}

	out, err := BuildTurnBatch(context.Background(), batchDeps(c))
	require.NoError(t, err)
	assert.Contains(t, out, "bounded startup batch")
	assert.Contains(t, out, "#c"+itoa(n-1)+": 1 unread", "the overflow channel must be listed, not dropped")
}

// TestBuildTurnBatch_MessageBoundSummarizesOverflow guards the per-channel
// message bound: a channel with more new messages than turnBatchMaxMessages=3
// must surface the newest 3 and list the channel as still-unread (no silent drop).
func TestBuildTurnBatch_MessageBoundSummarizesOverflow(t *testing.T) {
	const conv = "conversations/chatty"
	const count = turnBatchMaxMessages + 2 // 5 new, bound is 3
	all := make([]*v1pb.ChatMessage, 0, count)
	for i := 0; i < count; i++ {
		all = append(all, mkMsg(conv+"/messages/"+itoa(i), "m"+itoa(i), "u", v1pb.SenderType_SENDER_TYPE_USER))
	}
	c := &fakeBatchClient{
		updates:  []*v1pb.ChannelUpdate{{Conversation: conv, NewMessageCount: int32(count), ProcessedVersion: 0, CurrentVersion: int64(count)}},
		channels: map[string]*v1pb.Conversation{conv: {Name: conv, Type: 2, Title: "chatty"}},
		messages: map[string][]*v1pb.ChatMessage{conv: all},
	}

	out, err := BuildTurnBatch(context.Background(), batchDeps(c))
	require.NoError(t, err)
	// Newest 3 (m2, m3, m4) surfaced; oldest (m0, m1) not in full lines.
	assert.Contains(t, out, "m2")
	assert.Contains(t, out, "m4")
	assert.NotContains(t, out, "m0\n", "oldest beyond bound should not be surfaced as a full line")
	assert.Contains(t, out, "#chatty: "+itoa(count)+" unread", "channel must be listed as still-unread")
}

func itoa(i int) string {
	const digits = "0123456789"
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{digits[i%10]}, b...)
		i /= 10
	}
	return string(b)
}
