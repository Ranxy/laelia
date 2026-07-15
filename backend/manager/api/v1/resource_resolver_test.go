package v1

import (
	"testing"

	"connectrpc.com/connect"

	models "github.com/Ranxy/laelia/backend/generated-go/store"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

func TestResolveResource(t *testing.T) {
	cases := []struct {
		name string
		req  connect.AnyRequest
		want *resourceWant
	}{
		{
			name: "get channel resolves conversation from name",
			req:  connect.NewRequest(&v1pb.GetChannelRequest{Name: "conversations/abc"}),
			want: &resourceWant{ResourceType: models.Policy_CONVERSATION, Name: "conversations/abc"},
		},
		{
			name: "list messages resolves conversation from conversation field",
			req:  connect.NewRequest(&v1pb.ListConversationMessagesRequest{Conversation: "conversations/abc"}),
			want: &resourceWant{ResourceType: models.Policy_CONVERSATION, Name: "conversations/abc"},
		},
		{
			name: "update channel resolves conversation from nested conversation.name",
			req: connect.NewRequest(&v1pb.UpdateChannelRequest{
				Conversation: &v1pb.Conversation{Name: "conversations/abc"},
			}),
			want: &resourceWant{ResourceType: models.Policy_CONVERSATION, Name: "conversations/abc"},
		},
		{
			name: "get command resolves the parent agent from a command name",
			req:  connect.NewRequest(&v1pb.GetCommandRequest{Name: "agents/a/commands/c"}),
			want: &resourceWant{ResourceType: models.Policy_AGENT, Name: "agents/a"},
		},
		{
			name: "nested message name resolves the parent conversation",
			req:  connect.NewRequest(&v1pb.GetChannelRequest{Name: "conversations/abc/messages/42"}),
			want: &resourceWant{ResourceType: models.Policy_CONVERSATION, Name: "conversations/abc"},
		},
		{
			name: "list channels has no resource",
			req:  connect.NewRequest(&v1pb.ListChannelsRequest{}),
			want: nil,
		},
		{
			name: "nil request has no resource",
			req:  nil,
			want: nil,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := resolveResource(c.req)
			if c.want == nil {
				if got != nil {
					t.Fatalf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("expected %+v, got nil", c.want)
			}
			if got.ResourceType != c.want.ResourceType || got.Name != c.want.Name {
				t.Fatalf("expected %+v, got %+v", c.want, got)
			}
		})
	}
}

type resourceWant struct {
	ResourceType models.Policy_Resource
	Name         string
}
