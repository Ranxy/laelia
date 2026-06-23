package executor

import (
	"google.golang.org/protobuf/types/known/structpb"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

// OutputChunk is a single stdout/stderr/system line emitted by a Runtime and
// streamed back to the manager as CommandProgress. Its SeqNo is per-command and
// monotonically increasing; the manager de-duplicates via the command_output
// unique index.
type OutputChunk struct {
	StreamType v1pb.CommandOutput_StreamType
	Content    string
	SeqNo      int32
}

// Result is the terminal outcome of a Runtime execution and is streamed back
// to the manager as CommandResult, which the dispatcher turns into an
// assistant chat_message (Phase 1) via CreateChatMessageBumpVersion.
type Result struct {
	ExitCode     int32
	DurationMs   int64
	ErrorMessage string
	LastSeqNo    int32
	FinalSummary string
	Result       *structpb.Struct
}
