package executor

import (
	"strings"

	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

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
	// Timestamp is the agent-side wall-clock time when this chunk was produced.
	Timestamp *timestamppb.Timestamp
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
	// SessionID is the ACP session id this turn used (newly created on a cold
	// turn, resumed on a warm turn). The executor persists it to
	// acp-session.json itself; this field mirrors it back so callers/tests can
	// observe which path ran without re-reading the file.
	SessionID string
	// Resumed reports whether the turn resumed an existing ACP session (warm)
	// or created a new one (cold). Drives metrics/debugging.
	Resumed bool
	// Fingerprint is the session fingerprint the turn used (same inputs as
	// acp-session.json / pi-session.json). The runner compares it against the
	// persisted context state and resets stats when the config changed.
	Fingerprint string
	// ResumeFailures is the consecutive ACP ResumeSession failure count after
	// this turn's resume attempt (0 when none failed or after a successful
	// resume). The runner persists it into the context state.
	ResumeFailures int
}

// InputTooLargeGuidance is appended to a Result.ErrorMessage when the provider
// rejects the prompt because the input exceeds the context window, so the user
// sees an actionable recovery path instead of a generic error.
const InputTooLargeGuidance = "Reduce the current turn batch, or compact the session / start a new session before retrying."

// ClassifyInputTooLarge matches provider error text for an oversized prompt
// input (claude-code / opencode / pi wording varies). String matching is
// intentionally broad because providers do not share an error taxonomy.
func ClassifyInputTooLarge(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "input too large") ||
		strings.Contains(lower, "too many tokens") ||
		strings.Contains(lower, "context length") && strings.Contains(lower, "exceed") ||
		strings.Contains(lower, "maximum context") ||
		strings.Contains(lower, "token limit")
}
