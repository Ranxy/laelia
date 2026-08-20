package v1

import (
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

func TestValidateSendMessageContent(t *testing.T) {
	cases := []struct {
		name        string
		content     string
		attachments []*v1pb.Attachment
		wantErr     bool
	}{
		{
			name:    "text only",
			content: "hello",
		},
		{
			name:        "file only",
			attachments: []*v1pb.Attachment{{Id: "file-1"}},
		},
		{
			name:        "text and file",
			content:     "see attached",
			attachments: []*v1pb.Attachment{{Id: "file-1"}},
		},
		{
			name:    "empty message",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSendMessageContent(&v1pb.SendMessageRequest{
				Content:     tc.content,
				Attachments: tc.attachments,
			})
			if tc.wantErr {
				require.Error(t, err)
				require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
				return
			}
			require.NoError(t, err)
		})
	}
}
