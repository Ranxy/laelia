package executor

import (
	"time"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

// outputBufferSize bounds the in-flight channel between a Runtime and the
// stream pump that forwards chunks to the manager.
const outputBufferSize = 1024

type Request struct {
	CommandID        string
	Instruction      string
	Profile          string
	WorkingDir       string
	Env              map[string]string
	TimeoutSeconds   int32
	AllowDiff        bool
	ConversationID   string
	AgentResourceID  string
	AgentDisplayName string
	PrincipalID      string
	// DaemonSocket / SessionToken / BinaryDir configure the CLI the LLM shells
	// out to. The executor injects them into the ACP subprocess env so the
	// `laelia-agent message ...` / `laelia-agent command context` subcommands can
	// reach the local daemon (which holds the live access token) and find the
	// binary on PATH without any flags.
	DaemonSocket string
	SessionToken string
	BinaryDir    string
}

type Event struct {
	SeqNo int32
	Type  v1pb.CommandEventType

	Summary    string
	Text       string
	StreamType v1pb.CommandOutput_StreamType

	Timestamp time.Time

	Lifecycle           *v1pb.LifecyclePayload
	TextDelta           *v1pb.TextDeltaPayload
	ToolCallStarted     *v1pb.ToolCallStartedPayload
	ToolCallFinished    *v1pb.ToolCallFinishedPayload
	DiffEmitted         *v1pb.DiffEmittedPayload
	Warning             *v1pb.WarningPayload
	RawAcp              *v1pb.RawAcpPayload
	FinalSummary        *v1pb.FinalSummaryPayload
	PermissionRequested *v1pb.PermissionRequestedPayload
	PermissionTimedOut  *v1pb.PermissionTimedOutPayload
	PermissionDecided   *v1pb.PermissionDecidedPayload
}

type Runtime interface {
	Start()
	Cancel()
	OutputChannel() <-chan OutputChunk
	EventChannel() <-chan Event
	ResultChannel() <-chan Result
	Done() <-chan struct{}
}

type PermissionResolver interface {
	ResolvePermission(optionID string)
}
