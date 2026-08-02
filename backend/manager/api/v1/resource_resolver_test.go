package v1

import (
	"reflect"
	"testing"

	"github.com/Ranxy/laelia/backend/manager/component/iam"

	models "github.com/Ranxy/laelia/backend/generated-go/store"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

func TestResolveResources(t *testing.T) {
	cases := []struct {
		name string
		msg  any
		want []*iam.ResourceRef
	}{
		{
			name: "get channel resolves conversation from name",
			msg:  &v1pb.GetChannelRequest{Name: "conversations/abc"},
			want: []*iam.ResourceRef{{ResourceType: models.Policy_CONVERSATION, Name: "conversations/abc"}},
		},
		{
			name: "list messages resolves conversation from conversation field",
			msg:  &v1pb.ListConversationMessagesRequest{Conversation: "conversations/abc"},
			want: []*iam.ResourceRef{{ResourceType: models.Policy_CONVERSATION, Name: "conversations/abc"}},
		},
		{
			name: "update channel resolves conversation from nested conversation.name",
			msg:  &v1pb.UpdateChannelRequest{Conversation: &v1pb.Conversation{Name: "conversations/abc"}},
			want: []*iam.ResourceRef{{ResourceType: models.Policy_CONVERSATION, Name: "conversations/abc"}},
		},
		{
			name: "get command resolves the command object",
			msg:  &v1pb.GetCommandRequest{Name: "agents/a/commands/c"},
			want: []*iam.ResourceRef{{ResourceType: models.Policy_COMMAND, Name: "agents/a/commands/c"}},
		},
		{
			name: "watch command resolves the command object",
			msg:  &v1pb.WatchCommandRequest{Name: "agents/a/commands/c"},
			want: []*iam.ResourceRef{{ResourceType: models.Policy_COMMAND, Name: "agents/a/commands/c"}},
		},
		{
			name: "get reminder resolves the reminder object",
			msg:  &v1pb.GetReminderRequest{Name: "reminders/42"},
			want: []*iam.ResourceRef{{ResourceType: models.Policy_REMINDER, Name: "reminders/42"}},
		},
		{
			name: "download file resolves the file object from a bare id",
			msg:  &v1pb.DownloadFileRequest{Id: "8f14e45f-ea56-4f6d-9a1a-2f0b1c2d3e4f"},
			want: []*iam.ResourceRef{{ResourceType: models.Policy_FILE, Name: "files/8f14e45f-ea56-4f6d-9a1a-2f0b1c2d3e4f"}},
		},
		{
			name: "list files resolves conversation",
			msg:  &v1pb.ListFilesRequest{Conversation: "conversations/abc"},
			want: []*iam.ResourceRef{{ResourceType: models.Policy_CONVERSATION, Name: "conversations/abc"}},
		},
		{
			name: "nested message name normalizes the parent conversation",
			msg:  &v1pb.GetChannelRequest{Name: "conversations/abc/messages/42"},
			want: []*iam.ResourceRef{{ResourceType: models.Policy_CONVERSATION, Name: "conversations/abc"}},
		},
		{
			name: "send message resolves conversation",
			msg:  &v1pb.SendMessageRequest{Conversation: "conversations/abc"},
			want: []*iam.ResourceRef{{ResourceType: models.Policy_CONVERSATION, Name: "conversations/abc"}},
		},
		{
			name: "list channels has no resource",
			msg:  &v1pb.ListChannelsRequest{},
			want: nil,
		},
		{
			name: "nil message has no resource",
			msg:  nil,
			want: nil,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := resolveResources(c.msg)
			if len(c.want) == 0 {
				if len(got) != 0 {
					t.Fatalf("expected no resources, got %+v", got)
				}
				return
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("expected %+v, got %+v", c.want, got)
			}
		})
	}
}

func TestDedupeRefs(t *testing.T) {
	in := []*iam.ResourceRef{
		{ResourceType: models.Policy_CONVERSATION, Name: "conversations/abc"},
		{ResourceType: models.Policy_CONVERSATION, Name: "conversations/abc"},
		{ResourceType: models.Policy_AGENT, Name: "agents/a"},
	}
	got := dedupeRefs(in)
	if len(got) != 2 {
		t.Fatalf("expected 2 refs, got %d: %+v", len(got), got)
	}
	if got[0].Name != "conversations/abc" || got[1].Name != "agents/a" {
		t.Fatalf("unexpected order/content: %+v", got)
	}
}
