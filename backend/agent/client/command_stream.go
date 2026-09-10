package client

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/Ranxy/laelia/backend/agent/executor"
	"github.com/Ranxy/laelia/backend/agent/outbox"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
)

// streamSender abstracts the agent bidi stream for send serialization.
// connect-go's Send is not safe to call concurrently, and the workspace reply
// goroutines send alongside the ping ticker and the drain loop, so mainLoop
// wraps the raw stream in serializedSender.
type streamSender interface {
	Send(*v1pb.AgentStreamMessage) error
}

// serializedSender serializes Send calls on the underlying stream.
type serializedSender struct {
	mu     sync.Mutex
	stream streamSender
}

func (s *serializedSender) Send(msg *v1pb.AgentStreamMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stream.Send(msg)
}

// commandStream owns one agent's AgentChannel lifecycle and the in-flight
// drain-turn bookkeeping shared by the connector, drain runner, and message
// router. The connection loop lives in stream_connector.go, the drain/execution
// orchestration in drain_runner.go, context tracking in context_observer.go,
// and manager-message dispatch in message_router.go.
type commandStream struct {
	client       v1connect.AgentStreamServiceClient
	managerURL   string
	backoff      *ExponentialBackoff
	getToken     func() string
	getSessID    func() string
	getAcpConfig func() *executor.ACPConfig
	socketPath   string
	sessionToken string
	binaryDir    string
	// agentName is the agent's full resource name (agents/{agent}), carried
	// in-stream as AgentReady.agent_name so the manager can bind this AgentChannel
	// to the agent. It is NOT used as LAELIA_AGENT — that is the bare agentID.
	agentName string
	// agentID is the agent's bare handle (the agents/{handle} tail, e.g.
	// "rei-agent-1"). It keys the per-agent working dir and local state file
	// under the machine's namespace, is passed to the executor as
	// Request.AgentID, and — as Request.AgentResourceID — becomes
	// LAELIA_AGENT, which the daemon and chattools use as a bare id (e.g.
	// agents/<id>/commands/<id>).
	agentID string
	// machineID is the bare UUID of the machine hosting this agent. It namespaces
	// the agent's on-disk state (<data root>/<machineID>/<agentID>/) and is passed
	// to the executor as Request.MachineID.
	machineID   string
	isExecuting atomic.Bool

	// sink is the turn loop's report sink: the agent's durable outbox, and
	// uploader drains it to the manager through UploadCommandData. All three
	// are wired by the runner (nil in direct turn-loop tests).
	sink     turnSink
	uploader *outbox.Uploader
	ob       *outbox.Outbox

	// drain loop coordination. wakeCh is buffered(1): a wake while one is
	// already pending is coalesced. beginRespCh carries the manager's reply
	// to a BeginSession. currentExecutor is the in-flight session runtime, set
	// by the drain loop and read by the receive goroutine for Cancel.
	wakeCh            chan struct{}
	beginRespCh       chan *v1pb.BeginSessionResponse
	currentExecutor   executor.Runtime
	currentExecutorMu sync.Mutex

	// inFlightDone is non-nil while a drain turn is executing and is closed by
	// endInFlight when the turn ends. CancelInFlight snapshots it so a caller
	// (the runner's config hot-reload) can wait for the dying turn to finish
	// before an action that would race it (e.g. restarting the pi session).
	inFlightMu   sync.Mutex
	inFlightDone chan struct{}

	// cancelReason, when set by CancelInFlight, overrides the runtime's generic
	// cancellation error in the result the manager receives, so a coordinated
	// cancel surfaces an explicit cause (e.g. "config reloaded mid-turn")
	// instead of "context canceled".
	cancelReasonMu sync.Mutex
	cancelReason   string

	// newSessionRuntime builds the runtime for a drain session. It defaults to
	// buildRuntime (real ACP) and is overridable in tests and by the runner
	// (pi / ACP branch).
	newSessionRuntime func(req executor.Request) (executor.Runtime, error)
	// buildTurnBatch renders the "New messages received:" batch that opens a
	// drain turn, using the auth-bearing CommandServiceClient the daemon exposes.
	// Nil in tests (the test supplies TurnPrompt directly on the request).
	buildTurnBatch func(ctx context.Context) (string, error)

	// pendingPromptNotice is a manager-pushed prompt release notice that could
	// not be steered into the in-flight turn. It is consumed by the next
	// runSession (prepended to the turn) and acked. Guarded by promptNoticeMu
	// because the receive pump writes it and the drain loop reads it.
	promptNoticeMu      sync.Mutex
	pendingPromptNotice *v1pb.PromptReleaseNotice
	// steeredPromptVersion records the prompt_version of a notice that was
	// successfully steered into the current turn (pi path). The next runSession
	// treats it as already confirmed so applyPromptVersion does not inject a
	// duplicate notice.
	steeredPromptVersion string
}

// setPendingPromptNotice queues a prompt release notice for the next drain turn.
func (c *commandStream) setPendingPromptNotice(n *v1pb.PromptReleaseNotice) {
	if n == nil {
		return
	}
	c.promptNoticeMu.Lock()
	defer c.promptNoticeMu.Unlock()
	c.pendingPromptNotice = n
}

// takePendingPromptNotice returns and clears the queued prompt release notice.
func (c *commandStream) takePendingPromptNotice() *v1pb.PromptReleaseNotice {
	c.promptNoticeMu.Lock()
	defer c.promptNoticeMu.Unlock()
	n := c.pendingPromptNotice
	c.pendingPromptNotice = nil
	return n
}

// recordSteeredPromptVersion marks a prompt_version as already delivered via
// same-turn steering so the next drain turn does not re-inject it.
func (c *commandStream) recordSteeredPromptVersion(v string) {
	c.promptNoticeMu.Lock()
	defer c.promptNoticeMu.Unlock()
	c.steeredPromptVersion = v
}

// takeSteeredPromptVersion returns and clears the recorded steered version.
func (c *commandStream) takeSteeredPromptVersion() string {
	c.promptNoticeMu.Lock()
	defer c.promptNoticeMu.Unlock()
	v := c.steeredPromptVersion
	c.steeredPromptVersion = ""
	return v
}

func newCommandStream(httpClient *http.Client, managerURL, socketPath, sessionToken, binaryDir, agentName, agentID, machineID string) *commandStream {
	c := &commandStream{
		client:       v1connect.NewAgentStreamServiceClient(httpClient, managerURL),
		managerURL:   managerURL,
		backoff:      NewExponentialBackoff(defaultRetryBaseWait, defaultRetryMaxWait),
		socketPath:   socketPath,
		sessionToken: sessionToken,
		binaryDir:    binaryDir,
		agentName:    agentName,
		agentID:      agentID,
		machineID:    machineID,
		wakeCh:       make(chan struct{}, 1),
		beginRespCh:  make(chan *v1pb.BeginSessionResponse, 1),
	}
	c.newSessionRuntime = c.buildRuntime
	return c
}

// wake signals the drain loop that new messages may be available. It is
// best-effort and non-blocking: the durable per-channel cursor is the source of
// truth, so a dropped wake just means the next BeginSession discovers the work.
func (c *commandStream) wake() {
	select {
	case c.wakeCh <- struct{}{}:
	default:
	}
}

// resetCrossConnectionState clears stale in-flight session bookkeeping left
// over from a previous connection so a BeginSessionResponse that arrived but
// was never consumed (the drain loop's ctx cancelled mid-begin) cannot persist
// into the next connection and be consumed by its first beginSession. The
// caller guarantees the prior connection's receive pump and drain loop have
// exited, so replacing the channel fields is safe.
func (c *commandStream) resetCrossConnectionState() {
	c.setCurrentExecutor(nil)
	c.endInFlight()
	c.beginRespCh = make(chan *v1pb.BeginSessionResponse, 1)
	c.wakeCh = make(chan struct{}, 1)
}

func (c *commandStream) setCurrentExecutor(ex executor.Runtime) {
	c.currentExecutorMu.Lock()
	c.currentExecutor = ex
	c.currentExecutorMu.Unlock()
}

func (c *commandStream) getCurrentExecutor() executor.Runtime {
	c.currentExecutorMu.Lock()
	defer c.currentExecutorMu.Unlock()
	return c.currentExecutor
}

// beginInFlight marks a drain turn as executing: it raises isExecuting (kept
// for the existing idle probe) and installs a fresh inFlightDone that
// endInFlight closes when the turn ends. Callers pair every begin with a defer
// to endInFlight.
func (c *commandStream) beginInFlight() {
	c.inFlightMu.Lock()
	c.inFlightDone = make(chan struct{})
	c.inFlightMu.Unlock()
	c.isExecuting.Store(true)
	// Clear any cancel reason left over from a prior turn that ended via a path
	// which never consumed takeCancelReason (ctx.Done / send-error early
	// returns), so a stale reason cannot mislabel THIS turn's result.
	c.setCancelReason("")
}

// endInFlight clears the in-flight mark and closes the inFlightDone channel so
// any CancelInFlight waiter unblocks. Idempotent: a second call (e.g.
// resetCrossConnectionState after the turn already ended) finds no done and is
// a no-op.
func (c *commandStream) endInFlight() {
	c.isExecuting.Store(false)
	c.inFlightMu.Lock()
	done := c.inFlightDone
	c.inFlightDone = nil
	c.inFlightMu.Unlock()
	if done != nil {
		close(done)
	}
}

// InFlight reports whether a drain turn is currently executing.
func (c *commandStream) InFlight() bool {
	return c.isExecuting.Load()
}

// CancelInFlight cancels the in-flight drain turn, recording reason as the
// failure cause so the manager sees an explicit error instead of a generic
// cancellation. It returns the turn's done channel (closed when the turn ends)
// and whether a turn was actually in flight and cancelled. The caller may wait
// on the channel (bounded) before taking an action that would race the dying
// turn. No-op (returns false) when no turn is in flight.
func (c *commandStream) CancelInFlight(reason string) (<-chan struct{}, bool) {
	if !c.isExecuting.Load() {
		return nil, false
	}
	c.inFlightMu.Lock()
	done := c.inFlightDone
	c.inFlightMu.Unlock()
	if done == nil {
		return nil, false
	}
	c.setCancelReason(reason)
	if ex := c.getCurrentExecutor(); ex != nil {
		ex.Cancel()
	}
	return done, true
}

func (c *commandStream) setCancelReason(reason string) {
	c.cancelReasonMu.Lock()
	c.cancelReason = reason
	c.cancelReasonMu.Unlock()
}

// takeCancelReason returns and clears the pending cancel reason. runCommand
// consumes it after the runtime reports its result, overriding a generic
// cancellation error with the coordinated cause.
func (c *commandStream) takeCancelReason() string {
	c.cancelReasonMu.Lock()
	reason := c.cancelReason
	c.cancelReason = ""
	c.cancelReasonMu.Unlock()
	return reason
}

func maxSeq(current int32, next int32) int32 {
	if next > current {
		return next
	}
	return current
}

func nextEventSeq(state *executor.LocalState) int32 {
	state.LastEventSeqSent++
	return state.LastEventSeqSent
}
