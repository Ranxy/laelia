package dispatcher

import (
	"testing"

	"github.com/google/uuid"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

func progressEntry(streamType v1pb.CommandOutput_StreamType) *v1pb.UploadCommandDataEntry {
	return &v1pb.UploadCommandDataEntry{
		CommandId: uuid.NewString(),
		Kind:      v1pb.UploadEntryKind_UPLOAD_ENTRY_KIND_PROGRESS,
		SeqNo:     7,
		Payload: &v1pb.UploadCommandDataEntry_Progress{
			Progress: &v1pb.CommandProgress{
				Type:    streamType,
				Content: "chunk",
				SeqNo:   7,
			},
		},
	}
}

// TestConvertUploadEntryAcceptsAssistantProgress guards the regression where an
// upper bound of SYSTEM poison-rejected ASSISTANT progress, silently dropping
// every assistant row the ACP/thread runtimes emit.
func TestConvertUploadEntryAcceptsAssistantProgress(t *testing.T) {
	for _, streamType := range []v1pb.CommandOutput_StreamType{
		v1pb.CommandOutput_STDOUT,
		v1pb.CommandOutput_STDERR,
		v1pb.CommandOutput_SYSTEM,
		v1pb.CommandOutput_ASSISTANT,
	} {
		se, reason := convertUploadEntry(progressEntry(streamType))
		if reason != "" {
			t.Fatalf("stream type %v must be accepted, got rejection %q", streamType, reason)
		}
		if se.StreamType != int32(streamType) {
			t.Errorf("stream type %v: got stored %d, want %d", streamType, se.StreamType, int32(streamType))
		}
		if se.Content != "chunk" {
			t.Errorf("stream type %v: got content %q, want %q", streamType, se.Content, "chunk")
		}
	}
}

func TestConvertUploadEntryRejectsUnknownProgressType(t *testing.T) {
	for _, streamType := range []v1pb.CommandOutput_StreamType{
		v1pb.CommandOutput_STREAM_TYPE_UNSPECIFIED,
		v1pb.CommandOutput_StreamType(99),
	} {
		se, reason := convertUploadEntry(progressEntry(streamType))
		if se != nil {
			t.Errorf("stream type %v must be rejected, got entry %+v", streamType, se)
		}
		if reason == "" {
			t.Errorf("stream type %v must carry a rejection reason", streamType)
		}
	}
}
