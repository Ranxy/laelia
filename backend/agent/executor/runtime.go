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
	Profile          string
	WorkingDir       string
	Env              map[string]string
	TimeoutSeconds   int32
	AllowDiff        bool
	ConversationID   string
	AgentResourceID  string
	AgentDisplayName string
	// AgentID is the agent's stable server-assigned UUID (parsed from the
	// agents/{id} tail). It keys the per-agent working dir and the persistent
	// ACP session-state file (acp-session.json) that lets drain turns resume
	// the same ACP session instead of cold-starting. Distinct from
	// AgentResourceID, which is the same bare id carried as LAELIA_AGENT.
	AgentID string
	// MachineID is the resource id (uuid) of the machine hosting this agent.
	// A machine hosts many agents on one host, so it namespaces each agent's
	// on-disk state (~/.laelia/<machineID>/<agentID>/): working dir, ACP
	// session-state, and command-state. Empty only in unit tests that don't
	// touch the filesystem.
	MachineID string
	// TurnPrompt is the "New messages received:" bounded batch the LLM is
	// prompted with this turn. On a cold turn (no reusable ACP session) the
	// executor prepends the full init prompt (buildPrompt) and then the batch;
	// on a warm turn (resumed session) only the batch is sent — the init prompt
	// lives in the resumed session history. Empty means "no new work surfaced
	// this turn" (cold start with an idle inbox), in which case the executor
	// sends the init prompt alone so the agent is primed for future turns.
	TurnPrompt string
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
