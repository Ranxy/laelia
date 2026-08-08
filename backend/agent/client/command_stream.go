package client

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/laelia/backend/agent/executor"
	"github.com/Ranxy/laelia/backend/agent/workspace"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
)

const (
	cmdPingInterval = 15 * time.Second

	mergedTextDeltaFlushBytes = 4096

	// minSessionGap is the hard floor between drain sessions for one agent. It
	// prevents two agents from tight-looping each other into a wake storm —
	// the LLM's "silence is valid" guidance is the soft brake, this is the hard
	// one. A session that finishes faster than this gap waits out the remainder
	// before opening the next.
	minSessionGap = 1 * time.Second

	// beginSessionRetryWait is the backoff after a transient BeginSession
	// failure (e.g. a manager-side DB hiccup returning Internal). The pending
	// messages that triggered the wake are still queued server-side and the
	// manager will not re-wake, so the drain loop must retry BeginSession
	// proactively rather than wait for the next wake. A truly dead stream
	// surfaces via the receive pump and triggers a full reconnect independently.
	beginSessionRetryWait = 2 * time.Second

	// reanchorEveryTurns is the warm-turn cadence for periodic re-anchoring:
	// after this many consecutive warm turns without a compaction, the next
	// warm turn carries the identity anchor.
	reanchorEveryTurns = 10

	// contextWarningThreshold is the used/size ratio at or above which the turn
	// batch carries a context-window warning.
	contextWarningThreshold = 0.9

	// usageDropInferenceRatio is the used-token drop (vs the last observation)
	// that infers a context compaction finished when no direct event arrived.
	usageDropInferenceRatio = 0.3

	// contextQuietWindow is the no-agent-message quiet period required before a
	// usage drop is treated as a compaction rather than active generation.
	contextQuietWindow = 10 * time.Second
)

// compactionStaleTimeout is how long a CONTEXT_COMPACTION_STARTED may run
// without a matching FINISHED before the drain loop surfaces a WARNING
// ("Context compaction still running; no finish event observed"). It mirrors
// A var (not const) so tests can
// shrink the window.
var compactionStaleTimeout = 5 * time.Minute

type mergedText struct {
	builder    strings.Builder
	streamType v1pb.CommandOutput_StreamType
	started    bool
}

func (m *mergedText) append(streamType v1pb.CommandOutput_StreamType, text string) bool {
	if !m.started {
		m.started = true
		m.streamType = streamType
	}
	if streamType != m.streamType {
		return true
	}
	_, _ = m.builder.WriteString(text)
	return m.builder.Len() >= mergedTextDeltaFlushBytes
}

func (m *mergedText) flush(stream streamSender, commandID string, state *executor.LocalState) error {
	if !m.started {
		return nil
	}
	text := m.builder.String()
	m.builder.Reset()
	m.started = false
	if text == "" {
		return nil
	}
	event := executor.Event{
		SeqNo:      nextEventSeq(state),
		Type:       v1pb.CommandEventType_TEXT_DELTA,
		Summary:    text,
		Text:       text,
		StreamType: m.streamType,
		TextDelta: &v1pb.TextDeltaPayload{
			StreamType: m.streamType.String(),
			Content:    text,
		},
	}
	return sendCommandEvent(stream, commandID, &event)
}

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
	// agentID is the agent's bare server-assigned UUID (the agents/{agent} tail).
	// It keys the per-agent working dir and local state file under the machine's
	// namespace, is passed to the executor as Request.AgentID, and — as
	// Request.AgentResourceID — becomes LAELIA_AGENT, which the daemon and
	// chattools use as a bare id (e.g. agents/<id>/commands/<id>).
	agentID string
	// machineID is the bare UUID of the machine hosting this agent. It namespaces
	// the agent's on-disk state (~/.laelia/<machineID>/<agentID>/) and is passed
	// to the executor as Request.MachineID.
	machineID   string
	isExecuting atomic.Bool

	// drain loop coordination. wakeCh is buffered(1): a wake while one is
	// already pending is coalesced. beginRespCh carries the manager's reply
	// to a BeginSession. currentExecutor is the in-flight session runtime, set
	// by the drain loop and read by the receive goroutine for Cancel/permission.
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

// Start runs one command-stream lifecycle (mainLoop) and returns its terminal
// error. It deliberately does NOT retry internally: a dead bidi stream must
// surface to the caller (Client.Run's heartbeat loop, the "death fuse") so the
// whole agent connection is torn down and reconnected rather than the agent
// going deaf while its heartbeat stays healthy. The caller owns reconnect
// backoff.
func (c *commandStream) Start(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return nil
	default:
	}
	return c.mainLoop(ctx)
}

func (c *commandStream) mainLoop(ctx context.Context) error {
	token := c.getToken()
	if token == "" {
		_ = c.backoff.Wait(ctx)
		return nil
	}

	stream := c.client.AgentChannel(ctx)
	stream.RequestHeader().Set("Authorization", "Bearer "+token)

	ready := &v1pb.AgentStreamMessage{
		Message: &v1pb.AgentStreamMessage_AgentReady{
			AgentReady: &v1pb.AgentReady{
				AgentName: c.agentName,
				SessionId: c.getSessID(),
			},
		},
	}
	if state, err := executor.LoadLocalState(c.machineID, c.agentID); err != nil {
		slog.Warn("failed to load local command state", "error", err)
	} else if state != nil {
		ready.GetAgentReady().LastCommandId = state.CommandID
		ready.GetAgentReady().LastAckSeq = state.LastSeqSent
		ready.GetAgentReady().LastEventSeq = state.LastEventSeqSent
	}
	if err := stream.Send(ready); err != nil {
		return err
	}

	// Reset any stale in-flight session bookkeeping from a previous connection.
	// The previous connection's receive pump and drain loop have exited
	// (doneCh close / drainCancel) before this point, so replacing the fields
	// is safe.
	c.resetCrossConnectionState()

	// serializedSender guards Send: connect-go's Send is not safe to call
	// concurrently, and the workspace reply goroutines send alongside the ping
	// ticker and the drain loop.
	sender := &serializedSender{stream: stream}

	pingTicker := time.NewTicker(cmdPingInterval)
	defer pingTicker.Stop()

	var pingSeq int64

	errCh := make(chan error, 1)
	doneCh := make(chan struct{})
	defer close(doneCh)

	// Receive pump: dispatches manager messages. BeginSessionResponse goes to
	// the drain loop; NewMessages kicks the drain loop; Cancel/permission act
	// on the in-flight session.
	go func() {
		for {
			msg, err := stream.Receive()
			if err != nil {
				if err != io.EOF {
					select {
					case errCh <- err:
					case <-doneCh:
					}
				}
				return
			}

			switch m := msg.Message.(type) {
			case *v1pb.ManagerStreamMessage_BeginSessionResponse:
				resp := m.BeginSessionResponse
				select {
				case c.beginRespCh <- resp:
				case <-doneCh:
				}

			case *v1pb.ManagerStreamMessage_NewMessages:
				// Best-effort wake; the durable cursor recovers anything missed.
				c.wake()

			case *v1pb.ManagerStreamMessage_Cancel:
				if ex := c.getCurrentExecutor(); ex != nil {
					slog.Info("cancelling command", "commandID", m.Cancel.CommandId)
					ex.Cancel()
				}

			case *v1pb.ManagerStreamMessage_Pong:
				// pong received, link acknowledged

			case *v1pb.ManagerStreamMessage_PermissionDecision:
				d := m.PermissionDecision
				slog.Info("received permission decision", "commandID", d.CommandId, "optionID", d.OptionId)
				if ex := c.getCurrentExecutor(); ex != nil {
					if resolver, ok := ex.(executor.PermissionResolver); ok {
						resolver.ResolvePermission(d.OptionId)
					}
				}

			case *v1pb.ManagerStreamMessage_Steer:
				st := m.Steer
				slog.Info("received steer", "commandID", st.CommandId)
				if ex := c.getCurrentExecutor(); ex != nil {
					if resolver, ok := ex.(executor.SteerResolver); ok {
						resolver.Steer(st.Text)
					}
				}

			case *v1pb.ManagerStreamMessage_WorkspaceListRequest:
				// File reads run on their own goroutine: a slow disk must not
				// block the receive pump (BeginSession / NewMessages / Cancel).
				go c.handleWorkspaceList(ctx, sender, m.WorkspaceListRequest)

			case *v1pb.ManagerStreamMessage_WorkspaceReadRequest:
				go c.handleWorkspaceRead(ctx, sender, m.WorkspaceReadRequest)

			default:
				slog.Warn("unknown message type from manager")
			}
		}
	}()

	// Drain loop: the agent-first autonomous engine. On wake it opens sessions
	// (BeginSession) and runs each until the manager reports idle. Wakes that
	// arrive during a session are coalesced — the post-session BeginSession
	// picks up anything new via the server-side cursor comparison.
	drainCtx, drainCancel := context.WithCancel(ctx)
	defer drainCancel()
	go c.drainLoop(drainCtx, sender, doneCh)

	// Kick the drain loop once on connect so missed-offline messages are
	// discovered immediately (AgentReady already told the manager we're back).
	c.wake()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-doneCh:
			return nil
		case err := <-errCh:
			return err
		case <-pingTicker.C:
			pingSeq++
			ping := &v1pb.AgentStreamMessage{
				Message: &v1pb.AgentStreamMessage_Ping{
					Ping: &v1pb.Ping{
						Seq:    pingSeq,
						SentAt: time.Now().UnixMilli(),
					},
				},
			}
			if err := sender.Send(ping); err != nil {
				return err
			}
		}
	}
}

// drainLoop is the agent-first autonomous engine. It waits for a wake, then
// repeatedly opens a session (BeginSession) and runs it until the manager
// reports no channel has updates (idle), at which point it waits for the next
// wake. One session processes one channel; the outer loop opens the next.
func (c *commandStream) drainLoop(ctx context.Context, stream streamSender, doneCh <-chan struct{}) {
	var lastSessionStart time.Time
	for {
	START:
		select {
		case <-ctx.Done():
			return
		case <-doneCh:
			return
		case <-c.wakeCh:
		}

		// Drain until idle: each BeginSession that reports a channel opens a
		// session; an idle response ends this drain pass.
		for {
			select {
			case <-ctx.Done():
				return
			case <-doneCh:
				return
			default:
			}

			if !lastSessionStart.IsZero() {
				if gap := time.Since(lastSessionStart); gap < minSessionGap {
					select {
					case <-time.After(minSessionGap - gap):
					case <-ctx.Done():
						return
					case <-doneCh:
						return
					}
				}
			}

			resp, err := c.beginSession(ctx, stream, doneCh)
			if err != nil {
				// Do NOT exit the drain loop: a transient BeginSession error
				// (e.g. a manager-side DB hiccup) would otherwise deafen the
				// agent until the whole machine reconnects. The wake that
				// started this pass already fired and won't re-fire, so back off
				// and retry BeginSession proactively. A dead stream is caught
				// separately by the receive pump and drives a full reconnect.
				slog.Warn("drain loop: begin session failed, backing off before retry", "error", err)
				select {
				case <-time.After(beginSessionRetryWait):
				case <-ctx.Done():
					return
				case <-doneCh:
					return
				}
				continue
			}
			if resp.Idle {
				goto START
			}

			lastSessionStart = time.Now()
			c.runSession(ctx, stream, resp.CommandId, resp.AgentDisplayName, resp.OwnerDisplayName)
		}
	}
}

// beginSession sends a BeginSession message and waits for the manager's reply.
// Returns a non-idle response with a command_id to run, or idle=true when no
// channel has updates.
func (c *commandStream) beginSession(ctx context.Context, stream streamSender, doneCh <-chan struct{}) (*v1pb.BeginSessionResponse, error) {
	if err := stream.Send(&v1pb.AgentStreamMessage{
		Message: &v1pb.AgentStreamMessage_BeginSession{
			BeginSession: &v1pb.BeginSession{},
		},
	}); err != nil {
		return nil, err
	}

	select {
	case resp := <-c.beginRespCh:
		return resp, nil
	case <-doneCh:
		return nil, io.EOF
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// contextObserver applies the context state machine (design doc L1/L2) to the
// events flowing through one command on the runCommand goroutine: it records
// usage observations, infers compaction from usage drops, runs the compaction
// watchdog, and folds compaction events into the per-agent ContextState. All
// mutations happen on the pump goroutine, so no locking is needed. Extra events
// it emits (inferred compaction finish, stale warning) use the same LocalState
// sequence counter as the pump.
type contextObserver struct {
	state      *executor.ContextState
	stream     streamSender
	commandID  string
	localState *executor.LocalState

	// watchdogCh receives one signal when a compaction has been active for
	// compactionStaleTimeout without a finish event.
	watchdogCh chan struct{}
	timer      *time.Timer
	// lastAgentChunkAt is the last time an agent_message_chunk raw event was
	// observed; a usage drop while the agent is actively generating is not a
	// compaction.
	lastAgentChunkAt time.Time
}

func newContextObserver(state *executor.ContextState, stream streamSender, commandID string, localState *executor.LocalState) *contextObserver {
	return &contextObserver{
		state:      state,
		stream:     stream,
		commandID:  commandID,
		localState: localState,
		watchdogCh: make(chan struct{}, 1),
	}
}

func (o *contextObserver) startWatchdog() {
	o.stopWatchdog()
	o.timer = time.AfterFunc(compactionStaleTimeout, func() {
		select {
		case o.watchdogCh <- struct{}{}:
		default:
		}
	})
}

func (o *contextObserver) stopWatchdog() {
	if o.timer != nil {
		o.timer.Stop()
		o.timer = nil
	}
}

// observe applies context-state updates for one forwarded event and emits any
// derived events (inferred compaction finish). It is a no-op when context
// tracking is disabled (state nil).
func (o *contextObserver) observe(event *executor.Event) error {
	if o.state == nil {
		return nil
	}
	switch event.Type {
	case v1pb.CommandEventType_CONTEXT_COMPACTION_STARTED:
		o.state.Compaction.Active = true
		o.state.Compaction.LastStartAt = time.Now()
		o.startWatchdog()
	case v1pb.CommandEventType_CONTEXT_COMPACTION_FINISHED:
		o.onCompactionFinished()
	case v1pb.CommandEventType_CONTEXT_USAGE_UPDATE:
		if event.ContextUsage == nil {
			return nil
		}
		prevUsed := o.state.Usage.Used
		o.state.Usage = executor.ContextUsage{
			Size:      event.ContextUsage.Size,
			Used:      event.ContextUsage.Used,
			UpdatedAt: time.Now(),
		}
		if !o.state.Compaction.Active && o.inferCompaction(prevUsed, event.ContextUsage.Used) {
			return o.emitInferredCompaction()
		}
	case v1pb.CommandEventType_RAW_ACP:
		if event.Summary == "agent_message_chunk" {
			o.lastAgentChunkAt = time.Now()
		}
	default:
	}
	return nil
}

// inferCompaction reports whether the observed used-token drop looks like a
// context compaction: > usageDropInferenceRatio below the previous observation
// and not while the agent is actively streaming message chunks.
func (o *contextObserver) inferCompaction(prevUsed, used int64) bool {
	if prevUsed <= 0 || used < 0 {
		return false
	}
	drop := prevUsed - used
	if drop <= 0 || float64(drop)/float64(prevUsed) <= usageDropInferenceRatio {
		return false
	}
	if !o.lastAgentChunkAt.IsZero() && time.Since(o.lastAgentChunkAt) < contextQuietWindow {
		return false
	}
	return true
}

func (o *contextObserver) onCompactionFinished() {
	o.stopWatchdog()
	o.state.Compaction.Active = false
	o.state.Compaction.Count++
	o.state.Compaction.LastAt = time.Now()
	o.state.NeedsReanchor = true
	o.state.Session.Turns = 0
}

// emitInferredCompaction reports a compaction that was detected from a usage
// drop (no direct agent event) and applies the same finish state as a direct
// event.
func (o *contextObserver) emitInferredCompaction() error {
	o.onCompactionFinished()
	event := executor.Event{
		SeqNo:   nextEventSeq(o.localState),
		Type:    v1pb.CommandEventType_CONTEXT_COMPACTION_FINISHED,
		Summary: "Context compaction finished (inferred from usage drop)",
		ContextCompaction: &v1pb.ContextCompactionPayload{
			Inferred: true,
		},
	}
	return sendCommandEvent(o.stream, o.commandID, &event)
}

// onWatchdog surfaces the stale-compaction
func (o *contextObserver) onWatchdog() error {
	msg := "Context compaction still running; no finish event observed"
	event := executor.Event{
		SeqNo:   nextEventSeq(o.localState),
		Type:    v1pb.CommandEventType_WARNING,
		Summary: msg,
		Warning: &v1pb.WarningPayload{Message: msg},
	}
	return sendCommandEvent(o.stream, o.commandID, &event)
}

// reanchorPrompt decides whether this turn carries the identity anchor and
// consumes the decision state: NeedsReanchor (set after a compaction) or the
// periodic warm-turn threshold. The anchor is only actually prepended on warm
// turns by the executor; a cold turn re-sends the full init prompt, so
// consuming the decision either way is correct.
func reanchorPrompt(ctxState *executor.ContextState, name, ownerDisplayName string) string {
	if ctxState == nil {
		return ""
	}
	if !ctxState.NeedsReanchor && ctxState.Session.Turns < reanchorEveryTurns {
		return ""
	}
	ctxState.NeedsReanchor = false
	ctxState.Session.Turns = 0
	return executor.BuildReanchorPrompt(name, ownerDisplayName)
}

// appendContextWarning appends the context-window warning to the turn batch
// when the last observed usage is at or above contextWarningThreshold.
func appendContextWarning(prompt string, ctxState *executor.ContextState) string {
	if prompt == "" || ctxState == nil || ctxState.Usage.Size <= 0 {
		return prompt
	}
	ratio := ctxState.UsageRatio()
	if ratio < contextWarningThreshold {
		return prompt
	}
	pct := int(math.Round(ratio * 100))
	warning := fmt.Sprintf(
		"Context warning: your context window is ~%d%% full (%d/%d tokens). Prefer concise replies; write durable knowledge to MEMORY.md.",
		pct, ctxState.Usage.Used, ctxState.Usage.Size,
	)
	return strings.TrimRight(prompt, "\n") + "\n\n" + warning
}

// persistContextState folds the completed turn into the context state (warm
// turn count, fingerprint-change reset) and saves it. In-turn observations
// (usage/compaction) are persisted even when result is nil (failed turn).
func (c *commandStream) persistContextState(ctxState *executor.ContextState, result *executor.Result) {
	if ctxState == nil {
		return
	}
	if result != nil {
		if result.Fingerprint != "" {
			if ctxState.Fingerprint != "" && ctxState.Fingerprint != result.Fingerprint {
				ctxState.ResetForFingerprint(result.Fingerprint)
			} else {
				ctxState.Fingerprint = result.Fingerprint
			}
		}
		if result.Resumed {
			ctxState.Session.Turns++
		} else {
			ctxState.Session.Turns = 0
			ctxState.Session.ColdStarts++
		}
		// The ACP executor owns the resume-failure counter (it increments on
		// each failed ResumeSession and resets after the warning); mirror its
		// final value so this save is the single writer for the file.
		ctxState.Session.ResumeFailures = result.ResumeFailures
	}
	if err := executor.SaveContextState(c.machineID, c.agentID, ctxState); err != nil {
		slog.Warn("failed to persist context state", "agent", c.agentID, "error", err)
	}
}

// runSession executes one drain session: it builds the agent-first runtime
// (fixed prompt) and pumps progress/events/result over the bidi stream via
// runCommand. The agent itself decides which channel to process and how, by
// shelling out to the `laelia-machine` CLI over the local daemon. Blocking:
// returns when the session finishes.
func (c *commandStream) runSession(ctx context.Context, stream streamSender, commandID string, agentDisplayName, ownerDisplayName string) {
	// Per-agent context state drives re-anchor / usage-warning decisions for
	// this turn and is updated from the events below. A load failure disables
	// context tracking for the turn (never blocks work).
	ctxState, err := executor.LoadContextState(c.machineID, c.agentID)
	if err != nil {
		slog.Warn("failed to load context state; context tracking disabled for turn", "commandID", commandID, "error", err)
		ctxState = nil
	} else if ctxState == nil {
		// First observed turn: start with an empty state so observations and
		// decisions below have a place to accumulate.
		ctxState = &executor.ContextState{}
	}

	// Owner-change force re-anchor: a warm session's init prompt (which names the
	// owner) lives in the session history, so an ownership transfer is invisible
	// to the agent until a cold start or re-anchor. Comparing the manager's fresh
	// owner against the last one this session re-anchored with catches the change
	// on the very next warm turn, so the old owner's authority ends promptly.
	if ctxState != nil && ownerDisplayName != "" && ctxState.OwnerDisplayName != ownerDisplayName {
		ctxState.NeedsReanchor = true
	}
	if ctxState != nil {
		ctxState.OwnerDisplayName = ownerDisplayName
	}

	// Build the "New messages received:" bounded batch that opens this turn. It
	// is the user message the LLM is prompted with (the init prompt is sent only
	// once, at cold start, and inherited via session resume on warm turns).
	turnPrompt := ""
	if c.buildTurnBatch != nil {
		if batch, err := c.buildTurnBatch(ctx); err != nil {
			slog.Warn("failed to build turn batch; proceeding with empty batch", "commandID", commandID, "error", err)
		} else {
			turnPrompt = batch
		}
	}
	turnPrompt = appendContextWarning(turnPrompt, ctxState)

	name := agentDisplayName
	if name == "" {
		name = c.agentID
	}
	req := executor.Request{
		CommandID:        commandID,
		TurnPrompt:       turnPrompt,
		AgentDisplayName: agentDisplayName,
		OwnerDisplayName: ownerDisplayName,
		ReanchorPrompt:   reanchorPrompt(ctxState, name, ownerDisplayName),
	}

	runtime, err := c.newSessionRuntime(req)
	if err != nil {
		slog.Error("failed to build drain session runtime", "commandID", commandID, "error", err)
		if sendErr := sendCommandResult(stream, &v1pb.CommandResult{
			CommandId:    commandID,
			ExitCode:     -1,
			ErrorMessage: err.Error(),
			LastSeqNo:    -1,
		}); sendErr != nil {
			slog.Error("failed to send drain session failure result", "commandID", commandID, "error", sendErr)
		}
		c.persistContextState(ctxState, nil)
		return
	}

	c.setCurrentExecutor(runtime)
	defer c.setCurrentExecutor(nil)
	c.beginInFlight()
	defer c.endInFlight()

	result := c.runCommand(ctx, runtime, stream, req, ctxState)
	c.persistContextState(ctxState, result)
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

func (c *commandStream) runCommand(
	ctx context.Context,
	runtime executor.Runtime,
	stream streamSender,
	req executor.Request,
	ctxState *executor.ContextState,
) *executor.Result {
	commandID := req.CommandID
	state := &executor.LocalState{
		CommandID:        commandID,
		ExecutorKind:     "ACP",
		Status:           "running",
		StartedAt:        time.Now().UnixMilli(),
		LastSeqSent:      0,
		LastEventSeqSent: 0,
	}
	if err := executor.SaveLocalState(c.machineID, c.agentID, state); err != nil {
		slog.Warn("failed to persist local command state", "commandID", commandID, "error", err)
	}
	observer := newContextObserver(ctxState, stream, commandID, state)
	defer observer.stopWatchdog()

	resultSent := false
	defer func() {
		if resultSent {
			return
		}
		runtime.Cancel()
		_ = executor.ClearLocalState(c.machineID, c.agentID)
		_ = sendCommandResult(stream, &v1pb.CommandResult{
			CommandId:    commandID,
			ExitCode:     -1,
			ErrorMessage: "agent stream send failure",
			LastSeqNo:    state.LastSeqSent,
		})
	}()

	runtime.Start()
	startSeq := nextEventSeq(state)
	if err := sendCommandEvent(stream, commandID, &executor.Event{
		SeqNo:   startSeq,
		Type:    v1pb.CommandEventType_LIFECYCLE,
		Summary: "command started",
		Lifecycle: &v1pb.LifecyclePayload{
			ExecutorKind: "ACP",
			Profile:      req.Profile,
		},
	}); err != nil {
		slog.Error("failed to send command start event", "commandID", commandID, "error", err)
		return nil
	}
	if err := executor.SaveLocalState(c.machineID, c.agentID, state); err != nil {
		slog.Warn("failed to persist local command state", "commandID", commandID, "error", err)
	}

	var merged mergedText

	for {
		select {
		case <-ctx.Done():
			return nil

		case <-runtime.Done():
			_ = merged.flush(stream, commandID, state)

			// DrainOutput flushes any output/events the runtime produced while
			// the consumer was busy sending the result, mutating state so
			// LastSeqSent/LastEventSeqSent reflect exactly what was forwarded.
			drainOutput(ctx, runtime, stream, commandID, state, &merged, observer)

			_ = merged.flush(stream, commandID, state)

			result := <-runtime.ResultChannel()
			result.LastSeqNo = state.LastSeqSent
			// A coordinated cancel (e.g. config hot-reload) overrides the
			// runtime's generic cancellation error with an explicit cause so
			// the manager reports the reload, not "context canceled". Only
			// override a FAILED turn: a turn that finished successfully
			// (ExitCode 0) before the cancel took effect must not be mislabeled
			// as a reload failure (which could trigger a retry and duplicate
			// side effects).
			if reason := c.takeCancelReason(); reason != "" && result.ExitCode != 0 {
				result.ErrorMessage = reason
			}
			resultSent = true
			if err := sendCommandResult(stream, &v1pb.CommandResult{
				CommandId:    commandID,
				ExitCode:     result.ExitCode,
				DurationMs:   result.DurationMs,
				ErrorMessage: result.ErrorMessage,
				LastSeqNo:    result.LastSeqNo,
				FinalSummary: result.FinalSummary,
				Result:       result.Result,
			}); err != nil {
				slog.Error("failed to send command result", "commandID", commandID, "error", err)
			} else {
				slog.Info("command result sent", "commandID", commandID, "exitCode", result.ExitCode)
			}
			_ = executor.ClearLocalState(c.machineID, c.agentID)
			return &result

		case <-observer.watchdogCh:
			if err := observer.onWatchdog(); err != nil {
				slog.Error("failed to send compaction watchdog warning", "commandID", commandID, "error", err)
				return nil
			}

		case event, ok := <-runtime.EventChannel():
			if !ok {
				continue
			}
			event.SeqNo = nextEventSeq(state)
			if err := sendCommandEvent(stream, commandID, &event); err != nil {
				slog.Error("failed to send command event", "commandID", commandID, "error", err)
				return nil
			}
			if err := observer.observe(&event); err != nil {
				slog.Error("failed to send derived context event", "commandID", commandID, "error", err)
				return nil
			}
			if err := executor.SaveLocalState(c.machineID, c.agentID, state); err != nil {
				slog.Warn("failed to persist local command state", "commandID", commandID, "error", err)
			}

		case chunk, ok := <-runtime.OutputChannel():
			if !ok {
				continue
			}
			if err := sendCommandProgress(stream, commandID, chunk); err != nil {
				slog.Error("failed to send command progress", "commandID", commandID, "error", err)
				return nil
			}
			state.LastSeqSent = maxSeq(state.LastSeqSent, chunk.SeqNo)

			if merged.append(chunk.StreamType, chunk.Content) {
				if err := merged.flush(stream, commandID, state); err != nil {
					slog.Error("failed to send merged text delta", "commandID", commandID, "error", err)
					return nil
				}
				_ = merged.append(chunk.StreamType, chunk.Content)
			}
			if err := executor.SaveLocalState(c.machineID, c.agentID, state); err != nil {
				slog.Warn("failed to persist local command state", "commandID", commandID, "error", err)
			}
		}
	}
}

// drainOutput forwards any output chunks and events the runtime still has
// buffered after Done() fired, mutating state so LastSeqSent/LastEventSeqSent
// reflect exactly what was sent. It drains until both channels close (the
// runtime closes them in its deferred teardown), with ctx as a backstop so a
// runtime that never closes cannot wedge the consumer. Previously it only
// drained OutputChannel via a non-blocking `default` (dropping queued events
// and any output produced after the peek) and wrote event seq numbers against
// a throwaway LocalState, leaving state.LastEventSeqSent stale/rolled back.
func drainOutput(
	ctx context.Context,
	runtime executor.Runtime,
	stream streamSender,
	commandID string,
	state *executor.LocalState,
	merged *mergedText,
	observer *contextObserver,
) {
	outputClosed, eventClosed := false, false
	for !outputClosed || !eventClosed {
		select {
		case <-ctx.Done():
			_ = merged.flush(stream, commandID, state)
			return
		case chunk, ok := <-runtime.OutputChannel():
			if !ok {
				outputClosed = true
				continue
			}
			if err := sendCommandProgress(stream, commandID, chunk); err != nil {
				slog.Error("failed to send command progress", "commandID", commandID, "error", err)
				_ = merged.flush(stream, commandID, state)
				return
			}
			state.LastSeqSent = maxSeq(state.LastSeqSent, chunk.SeqNo)
			if merged.append(chunk.StreamType, chunk.Content) {
				_ = merged.flush(stream, commandID, state)
				_ = merged.append(chunk.StreamType, chunk.Content)
			}
		case event, ok := <-runtime.EventChannel():
			if !ok {
				eventClosed = true
				continue
			}
			event.SeqNo = nextEventSeq(state)
			if err := sendCommandEvent(stream, commandID, &event); err != nil {
				slog.Error("failed to send command event", "commandID", commandID, "error", err)
				_ = merged.flush(stream, commandID, state)
				return
			}
			if observer != nil {
				if err := observer.observe(&event); err != nil {
					slog.Error("failed to send derived context event", "commandID", commandID, "error", err)
					_ = merged.flush(stream, commandID, state)
					return
				}
			}
		}
	}
	_ = merged.flush(stream, commandID, state)
}

func (c *commandStream) buildRuntime(req executor.Request) (executor.Runtime, error) {
	return executor.NewACP(req, c.getAcpConfig())
}

func sendCommandProgress(stream streamSender, commandID string, chunk executor.OutputChunk) error {
	return stream.Send(&v1pb.AgentStreamMessage{
		Message: &v1pb.AgentStreamMessage_Progress{
			Progress: &v1pb.CommandProgress{
				CommandId: commandID,
				Type:      chunk.StreamType,
				Content:   chunk.Content,
				SeqNo:     chunk.SeqNo,
			},
		},
	})
}

func sendCommandEvent(stream streamSender, commandID string, event *executor.Event) error {
	ce := &v1pb.CommandEvent{
		CommandId: commandID,
		SeqNo:     event.SeqNo,
		Type:      event.Type,
		Summary:   event.Summary,
		Timestamp: timestamppb.New(time.Now()),
	}

	switch event.Type {
	case v1pb.CommandEventType_LIFECYCLE:
		ce.Payload = &v1pb.CommandEvent_Lifecycle{Lifecycle: event.Lifecycle}
	case v1pb.CommandEventType_TEXT_DELTA:
		ce.Payload = &v1pb.CommandEvent_TextDelta{TextDelta: event.TextDelta}
	case v1pb.CommandEventType_TOOL_CALL_STARTED:
		ce.Payload = &v1pb.CommandEvent_ToolCallStarted{ToolCallStarted: event.ToolCallStarted}
	case v1pb.CommandEventType_TOOL_CALL_FINISHED:
		ce.Payload = &v1pb.CommandEvent_ToolCallFinished{ToolCallFinished: event.ToolCallFinished}
	case v1pb.CommandEventType_DIFF_EMITTED:
		ce.Payload = &v1pb.CommandEvent_DiffEmitted{DiffEmitted: event.DiffEmitted}
	case v1pb.CommandEventType_WARNING:
		ce.Payload = &v1pb.CommandEvent_Warning{Warning: event.Warning}
	case v1pb.CommandEventType_RAW_ACP:
		ce.Payload = &v1pb.CommandEvent_RawAcp{RawAcp: event.RawAcp}
	case v1pb.CommandEventType_FINAL_SUMMARY:
		ce.Payload = &v1pb.CommandEvent_FinalSummary{FinalSummary: event.FinalSummary}
	case v1pb.CommandEventType_PERMISSION_REQUESTED:
		ce.Payload = &v1pb.CommandEvent_PermissionRequested{PermissionRequested: event.PermissionRequested}
	case v1pb.CommandEventType_PERMISSION_TIMED_OUT:
		ce.Payload = &v1pb.CommandEvent_PermissionTimedOut{PermissionTimedOut: event.PermissionTimedOut}
	case v1pb.CommandEventType_PERMISSION_DECIDED:
		ce.Payload = &v1pb.CommandEvent_PermissionDecided{PermissionDecided: event.PermissionDecided}
	case v1pb.CommandEventType_CONTEXT_COMPACTION_STARTED, v1pb.CommandEventType_CONTEXT_COMPACTION_FINISHED:
		ce.Payload = &v1pb.CommandEvent_ContextCompaction{ContextCompaction: event.ContextCompaction}
	case v1pb.CommandEventType_CONTEXT_USAGE_UPDATE:
		ce.Payload = &v1pb.CommandEvent_ContextUsage{ContextUsage: event.ContextUsage}
	default:
	}

	return stream.Send(&v1pb.AgentStreamMessage{
		Message: &v1pb.AgentStreamMessage_Event{Event: ce},
	})
}

func sendCommandResult(stream streamSender, result *v1pb.CommandResult) error {
	return stream.Send(&v1pb.AgentStreamMessage{
		Message: &v1pb.AgentStreamMessage_Result{
			Result: result,
		},
	})
}

// handleWorkspaceList lists one directory level of this agent's workspace and
// replies over the agent stream. The manager gates this by owner/admin
// permission; the workspace package enforces the never-visible/secret policy.
func (c *commandStream) handleWorkspaceList(_ context.Context, sender streamSender, req *v1pb.WorkspaceListRequest) {
	if req == nil {
		return
	}
	entries, err := workspace.List(executor.AgentWorkingDir(c.machineID, c.agentID), req.DirPath, req.IncludeHidden)
	if err != nil {
		slog.Warn("workspace list failed", "agentID", c.agentID, "dirPath", req.DirPath, "error", err)
	}
	protoEntries := make([]*v1pb.WorkspaceEntry, 0, len(entries))
	for _, e := range entries {
		var modifiedAt *timestamppb.Timestamp
		if !e.ModifiedAt.IsZero() {
			modifiedAt = timestamppb.New(e.ModifiedAt)
		}
		protoEntries = append(protoEntries, &v1pb.WorkspaceEntry{
			Name:        e.Name,
			Path:        e.Path,
			IsDirectory: e.IsDir,
			Size:        e.Size,
			ModifiedAt:  modifiedAt,
			IsHidden:    e.IsHidden,
		})
	}
	_ = sender.Send(&v1pb.AgentStreamMessage{
		Message: &v1pb.AgentStreamMessage_WorkspaceListResponse{
			WorkspaceListResponse: &v1pb.WorkspaceListResponse{
				RequestId: req.RequestId,
				Entries:   protoEntries,
			},
		},
	})
}

// handleWorkspaceRead previews one workspace file and replies over the agent
// stream. Refusals (sensitive file, too large, directory) come back in the
// response's error field; OS failures are logged and returned as errors too.
func (c *commandStream) handleWorkspaceRead(_ context.Context, sender streamSender, req *v1pb.WorkspaceReadRequest) {
	if req == nil {
		return
	}
	result, err := workspace.Read(executor.AgentWorkingDir(c.machineID, c.agentID), req.Path)
	if err != nil {
		slog.Warn("workspace read failed", "agentID", c.agentID, "path", req.Path, "error", err)
		result.Error = err.Error()
	}
	_ = sender.Send(&v1pb.AgentStreamMessage{
		Message: &v1pb.AgentStreamMessage_WorkspaceReadResponse{
			WorkspaceReadResponse: &v1pb.WorkspaceReadResponse{
				RequestId: req.RequestId,
				Content:   result.Content,
				Binary:    result.Binary,
				Size:      result.Size,
				MimeType:  result.MimeType,
				Encoding:  result.Encoding,
				Error:     result.Error,
			},
		},
	})
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
