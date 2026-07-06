package dispatcher

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/manager/store"
)

const (
	gracePeriod    = 60 * time.Second
	graceDBTimeout = 10 * time.Second
	watcherBufSize = 256
)

type SendFunc func(*v1pb.ManagerStreamMessage) error

type AgentSession struct {
	agentID         int
	agentResourceID string
	currentCmdID    string
	// send is the raw bidi-stream send function. It is nil once the session is
	// invalidated (agent disconnected or replaced). Stored in an atomic pointer
	// so RegisterAgent/UnregisterAgent (writers) and deliver (reader) never race
	// on the field — previously `send` was written under sess.mu and read under
	// sendMu, a data race on the same field.
	send        atomic.Pointer[SendFunc]
	sendMu      sync.Mutex // serializes concurrent sends on the same bidi stream
	lastPingAt  time.Time
	connectedAt time.Time
	mu          sync.Mutex // guards currentCmdID, lastPingAt, connectedAt
}

// deliver sends msg to the agent, serializing concurrent sends on the stream
// and returning an error if the session has been invalidated. All outbound
// messages route through this single path so the underlying stream send is
// never called concurrently (gRPC bidi sends are not safe for concurrent use).
func (s *AgentSession) deliver(msg *v1pb.ManagerStreamMessage) error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	fn := s.send.Load()
	if fn == nil {
		return errors.New("agent session invalidated")
	}
	return (*fn)(msg)
}

// Send sends a message to the agent over its bidi stream. It is safe for
// concurrent use (e.g. from the Phase 2 held-action re-prompt path).
func (s *AgentSession) Send(msg *v1pb.ManagerStreamMessage) error {
	return s.deliver(msg)
}

// ClearCurrentCommand clears the session's current command id when it matches
// the given id. Used during reconnect cleanup to drop a stale in-flight command.
func (s *AgentSession) ClearCurrentCommand(commandID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.currentCmdID == commandID {
		s.currentCmdID = ""
	}
}

type Dispatcher struct {
	store         *store.Store
	mu            sync.RWMutex
	sessions      map[int]*AgentSession
	watchers      map[string]map[chan *v1pb.CommandOutput]struct{}
	eventWatchers map[string]map[chan *v1pb.CommandEvent]struct{}
	pingInterval  time.Duration
	pingTimeout   time.Duration

	// lifecycleCtx is the parent context for the ping monitor and the
	// grace-period goroutines. Stop cancels it and waits on wg, so shutdown
	// joins every dispatcher-spawned goroutine instead of leaving the ping
	// ticker running for the process lifetime.
	lifecycleCtx    context.Context
	lifecycleCancel context.CancelFunc
	wg              sync.WaitGroup

	// grace tracks in-flight grace-period timers keyed by agent then command,
	// so a reconnect can cancel a pending "mark FAILED" timer for that agent
	// (the reconnect path reaps stale commands itself). Without this, a
	// reconnect racing the 60s timer could mark a command FAILED out from
	// under the new session.
	graceMu sync.Mutex
	grace   map[int]map[string]context.CancelFunc

	// pendingDiscovers maps a DiscoverProviders request_id to the channel that
	// the matching ProvidersDiscovered reply will resolve. Used by the unary
	// RefreshAgentProviders RPC to do a request/response round trip over the
	// bidi command stream.
	discoverMu       sync.Mutex
	pendingDiscovers map[string]chan *v1pb.ProvidersDiscovered
}

func New(s *store.Store) *Dispatcher {
	ctx, cancel := context.WithCancel(context.Background())
	return &Dispatcher{
		store:            s,
		sessions:         make(map[int]*AgentSession),
		watchers:         make(map[string]map[chan *v1pb.CommandOutput]struct{}),
		eventWatchers:    make(map[string]map[chan *v1pb.CommandEvent]struct{}),
		pingInterval:     15 * time.Second,
		pingTimeout:      45 * time.Second,
		grace:            make(map[int]map[string]context.CancelFunc),
		pendingDiscovers: make(map[string]chan *v1pb.ProvidersDiscovered),
		lifecycleCtx:     ctx,
		lifecycleCancel:  cancel,
	}
}

func (d *Dispatcher) RegisterAgent(_ context.Context, agentID int, agentResourceID string, send SendFunc) *AgentSession {
	d.mu.Lock()
	defer d.mu.Unlock()

	if old, ok := d.sessions[agentID]; ok {
		slog.Info("replacing existing agent session", "agentID", agentID)
		// Invalidate the previous session's send so in-flight deliver calls
		// error out instead of writing to the torn-down stream. The atomic
		// store is race-free against concurrent deliver readers.
		old.send.Store(nil)
	}

	// The agent reconnected: cancel any pending grace-period "mark FAILED"
	// timers for its in-flight commands. The reconnect path (handleAgentReady)
	// reaps stale RUNNING commands itself, so a dangling 60s timer is redundant
	// and racy (it could mark a command FAILED out from under the new session).
	d.cancelGraceForAgent(agentID)

	sess := &AgentSession{
		agentID:         agentID,
		agentResourceID: agentResourceID,
		connectedAt:     time.Now(),
		lastPingAt:      time.Now(),
	}
	fn := send
	sess.send.Store(&fn)

	d.sessions[agentID] = sess
	slog.Info("agent registered for command dispatch", "agentID", agentID)

	// The agent drives its own work via BeginSession; the manager no longer
	// pushes commands on connect. The agent sends AgentReady (handled in the
	// bidi loop) and then its drain loop calls BeginSession as needed.
	return sess
}

func (d *Dispatcher) UnregisterAgent(agentID int) {
	d.mu.Lock()
	sess, ok := d.sessions[agentID]
	if !ok {
		d.mu.Unlock()
		return
	}
	delete(d.sessions, agentID)
	d.mu.Unlock()

	sess.mu.Lock()
	cmdID := sess.currentCmdID
	sess.mu.Unlock()
	// Invalidate send so any concurrent deliver returns "agent session
	// invalidated" rather than writing to the closed stream.
	sess.send.Store(nil)

	slog.Info("agent unregistered from command dispatch", "agentID", agentID)

	if cmdID != "" {
		d.startGracePeriod(agentID, cmdID)
	}
}

func (d *Dispatcher) IsAgentConnected(agentID int) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	_, ok := d.sessions[agentID]
	return ok
}

// RegisterPendingDiscover creates a response channel keyed by requestID for an
// in-flight DiscoverProviders round trip. The caller sends the control message
// to the agent (via SendDiscoverProviders), then waits on the returned channel
// for the ProvidersDiscovered reply. CancelPendingDiscover must be called if
// the caller gives up waiting, to avoid leaking the entry.
func (d *Dispatcher) RegisterPendingDiscover(requestID string) chan *v1pb.ProvidersDiscovered {
	ch := make(chan *v1pb.ProvidersDiscovered, 1)
	d.discoverMu.Lock()
	d.pendingDiscovers[requestID] = ch
	d.discoverMu.Unlock()
	return ch
}

// CancelPendingDiscover removes a pending discover entry without delivering a
// result. Safe to call after the reply arrived (it is a no-op in that case
// since the entry was already removed).
func (d *Dispatcher) CancelPendingDiscover(requestID string) {
	d.discoverMu.Lock()
	delete(d.pendingDiscovers, requestID)
	d.discoverMu.Unlock()
}

// CompletePendingDiscover delivers a ProvidersDiscovered reply to the waiting
// caller and removes the pending entry. Called from the bidi receive loop when
// the agent replies. Unknown request ids (late replies, already-cancelled
// callers) are dropped silently.
func (d *Dispatcher) CompletePendingDiscover(msg *v1pb.ProvidersDiscovered) {
	if msg == nil {
		return
	}
	d.discoverMu.Lock()
	ch, ok := d.pendingDiscovers[msg.RequestId]
	if ok {
		delete(d.pendingDiscovers, msg.RequestId)
	}
	d.discoverMu.Unlock()
	if ok {
		select {
		case ch <- msg:
		default:
		}
	}
}

// SendDiscoverProviders sends a DiscoverProviders control message to the
// agent's active bidi stream. Returns an error if the agent has no active
// session (the frontend should show "agent offline").
func (d *Dispatcher) SendDiscoverProviders(agentID int, requestID string) error {
	d.mu.RLock()
	sess, ok := d.sessions[agentID]
	d.mu.RUnlock()
	if !ok {
		return errors.New("agent is not connected")
	}
	return sess.Send(&v1pb.ManagerStreamMessage{
		Message: &v1pb.ManagerStreamMessage_DiscoverProviders{
			DiscoverProviders: &v1pb.DiscoverProviders{RequestId: requestID},
		},
	})
}

// CurrentCommandID returns the command id the agent is currently running in its
// drain session, or "" if the agent has no in-flight session command. It is used
// to link a session's running command to the conversation the agent is working
// on, so the channel activity feed reflects in-progress work. The session
// command is created at BeginSession before the agent has chosen a channel, so
// the link is filled in when the agent reads a channel (commits to working on
// it) — see CommandService.ListConversationMessages.
func (d *Dispatcher) CurrentCommandID(agentID int) string {
	d.mu.RLock()
	sess, ok := d.sessions[agentID]
	d.mu.RUnlock()
	if !ok {
		return ""
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return sess.currentCmdID
}

// HandleBeginSession serves an agent's request to start a new autonomous
// processing session. The manager checks the agent's durable per-channel
// cursors: if no conversation has room_version beyond the cursor, it replies
// idle=true and the agent stays idle. Otherwise it creates a RUNNING command
// (the session's execution/event anchor, linked to a conversation later via
// AckProcessedVersion) and replies with its command_id.
func (d *Dispatcher) HandleBeginSession(ctx context.Context, agentID int) (*v1pb.BeginSessionResponse, error) {
	hasUpdates, err := d.store.HasUpdates(ctx, agentID)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to check channel updates")
	}
	hasReminders, err := d.store.HasDueReminders(ctx, agentID)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to check due reminders")
	}
	if !hasUpdates && !hasReminders {
		return &v1pb.BeginSessionResponse{Idle: true}, nil
	}

	agent, err := d.store.GetAgent(ctx, agentID)
	if err != nil || agent == nil {
		return nil, errors.New("agent not found")
	}

	// An agent must support ACP tasks to run an autonomous drain session. A
	// non-ACP agent stays idle (it has no executor to process messages); the
	// agent connection itself is the primary gate, this is the server-side backstop.
	if capability := agent.Info.GetCapability(); capability == nil || !capability.GetSupportsAcp() {
		slog.Warn("agent is not ACP-capable; staying idle", "agent", agent.ResourceID)
		return &v1pb.BeginSessionResponse{Idle: true}, nil
	}

	cmd, err := d.store.CreateCommand(ctx, &store.CommandMessage{
		AgentID:     agentID,
		PrincipalID: 1,  // system bot; the session is agent-initiated, not user-scoped
		Instruction: "", // the agent-first prompt is supplied by the agent client
		Status:      int32(v1pb.CommandStatus_RUNNING),
	})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create session command")
	}
	cmd.AgentResourceID = agent.ResourceID

	now := time.Now()
	if err := d.store.UpdateCommandStatus(ctx, cmd.ID, int32(v1pb.CommandStatus_RUNNING), &now, nil, nil, nil, ""); err != nil {
		slog.Error("failed to mark session command RUNNING", "commandID", cmd.ID, "error", err)
	}

	d.mu.RLock()
	sess, ok := d.sessions[agentID]
	d.mu.RUnlock()
	if ok {
		sess.mu.Lock()
		sess.currentCmdID = cmd.ID.String()
		sess.mu.Unlock()
	}

	slog.Info("agent session begun", "commandID", cmd.ID, "agentID", agentID)
	return &v1pb.BeginSessionResponse{CommandId: cmd.ID.String(), AgentDisplayName: agent.Name}, nil
}

// NotifyNewMessages pushes a NewMessagesAvailable hint to a connected agent so
// it knows the conversation has advanced (e.g. another participant posted).
// Phase 1 primarily calls this after assistant replies so multi-agent channels
// can be informed; the action-less agent-autonomy gate arrives in Phase 2.
func (d *Dispatcher) NotifyNewMessages(_ context.Context, agentID int, conversationID string, version int64) {
	d.mu.RLock()
	sess, ok := d.sessions[agentID]
	d.mu.RUnlock()

	if !ok {
		return
	}

	msg := &v1pb.ManagerStreamMessage{
		Message: &v1pb.ManagerStreamMessage_NewMessages{
			NewMessages: &v1pb.NewMessagesAvailable{
				ConversationIds: []string{conversationID},
				Versions:        []int64{version},
			},
		},
	}

	if err := sess.deliver(msg); err != nil {
		slog.Warn("failed to send NewMessagesAvailable", "agentID", agentID, "error", err)
	}
}

// NotifyWake sends an empty NewMessagesAvailable to a connected agent as a
// best-effort "check for work" tick. The agent's drain loop responds by calling
// BeginSession, which authoritatively checks the per-channel cursors; the wake
// itself carries no payload. Used on reconnect and (via NotifyNewMessages) when
// any message lands in a conversation the agent is a member of.
func (d *Dispatcher) NotifyWake(_ context.Context, agentID int) {
	d.mu.RLock()
	sess, ok := d.sessions[agentID]
	d.mu.RUnlock()
	if !ok {
		return
	}

	msg := &v1pb.ManagerStreamMessage{
		Message: &v1pb.ManagerStreamMessage_NewMessages{
			NewMessages: &v1pb.NewMessagesAvailable{},
		},
	}
	if err := sess.deliver(msg); err != nil {
		slog.Warn("failed to send wake to agent", "agentID", agentID, "error", err)
	}
}

// NotifyThreadMention pushes a NewMessagesAvailable hint to a connected agent
// that is subscribed to a thread, carrying the thread root id so the agent can
// go straight to thread check/read. Best-effort like NotifyNewMessages: the
// agent's durable cursor (advanced via ListThreadUpdates + AckProcessedVersion)
// is the source of truth, so a missed wake is recovered on reconnect.
func (d *Dispatcher) NotifyThreadMention(_ context.Context, agentID int, conversationID string, version int64, threadRootMessageID string) {
	d.mu.RLock()
	sess, ok := d.sessions[agentID]
	d.mu.RUnlock()
	if !ok {
		return
	}

	msg := &v1pb.ManagerStreamMessage{
		Message: &v1pb.ManagerStreamMessage_NewMessages{
			NewMessages: &v1pb.NewMessagesAvailable{
				ConversationIds:     []string{conversationID},
				Versions:            []int64{version},
				ThreadRootMessageId: threadRootMessageID,
			},
		},
	}
	if err := sess.deliver(msg); err != nil {
		slog.Warn("failed to send thread mention wake", "agentID", agentID, "error", err)
	}
}

// FetchConversationActivity returns the execution status of every agent member
// in a conversation. It combines member list, connection state, and running
// command events to derive a human-readable status per agent.
func (d *Dispatcher) FetchConversationActivity(ctx context.Context, conversationID string) ([]*v1pb.AgentActivity, error) {
	convUUID, err := uuid.Parse(conversationID)
	if err != nil {
		return nil, errors.Wrapf(err, "invalid conversation id")
	}

	members, err := d.store.ListConversationMembers(ctx, convUUID)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to list conversation members")
	}

	// Collect agent members: member_id is the agent resource ID.
	type agentEntry struct {
		resourceID string
		name       string
		id         int
	}
	var agents []agentEntry
	var agentIDs []int
	for _, m := range members {
		if m.MemberType != store.MemberTypeAgent {
			continue
		}
		ag, agErr := d.store.GetAgentByResourceID(ctx, m.MemberID)
		if agErr != nil || ag == nil {
			continue
		}
		agents = append(agents, agentEntry{resourceID: ag.ResourceID, name: ag.Name, id: ag.ID})
		agentIDs = append(agentIDs, ag.ID)
	}

	// Batch-query running commands for these agents in this conversation.
	running, runErr := d.store.GetRunningCommandsForConversation(ctx, agentIDs, convUUID)
	if runErr != nil {
		return nil, errors.Wrapf(runErr, "failed to get running commands")
	}
	runningByAgent := make(map[int]*store.RunningCommandInfo, len(running))
	for _, r := range running {
		runningByAgent[r.AgentID] = r
	}

	// Build activity entries.
	activities := make([]*v1pb.AgentActivity, 0, len(agents))
	for _, ag := range agents {
		act := &v1pb.AgentActivity{
			AgentId:     ag.resourceID,
			DisplayName: ag.name,
			Status:      "idle",
		}

		d.mu.RLock()
		sess, connected := d.sessions[ag.id]
		d.mu.RUnlock()

		if !connected {
			act.Status = "offline"
			activities = append(activities, act)
			continue
		}

		rci, hasRunning := runningByAgent[ag.id]
		if !hasRunning {
			activities = append(activities, act) // stays "idle"
			continue
		}

		// Derive status from the latest command event.
		switch rci.EventType {
		case 0:
			act.Status = "starting"
		case int32(v1pb.CommandEventType_LIFECYCLE):
			act.Status = "starting"
		case int32(v1pb.CommandEventType_TEXT_DELTA):
			act.Status = "output"
		case int32(v1pb.CommandEventType_TOOL_CALL_STARTED):
			if rci.Summary.Valid {
				act.Status = rci.Summary.String
				act.ToolName = rci.Summary.String
			} else {
				act.Status = "tool"
			}
		case int32(v1pb.CommandEventType_TOOL_CALL_FINISHED):
			act.Status = "thinking"
		default:
			act.Status = "starting"
		}

		// Suppress idle for active agents that might have a stale session.
		sess.mu.Lock()
		if sess.currentCmdID == "" {
			act.Status = "idle"
		}
		sess.mu.Unlock()

		activities = append(activities, act)
	}

	return activities, nil
}

// ---- Phase 2: Held Draft ----

func (d *Dispatcher) CancelCommand(_ context.Context, agentID int, commandID string) error {
	d.mu.RLock()
	sess, ok := d.sessions[agentID]
	d.mu.RUnlock()

	if !ok {
		return errors.New("agent not connected")
	}

	msg := &v1pb.ManagerStreamMessage{
		Message: &v1pb.ManagerStreamMessage_Cancel{
			Cancel: &v1pb.CancelMessage{
				CommandId: commandID,
			},
		},
	}

	if err := sess.deliver(msg); err != nil {
		slog.Error("failed to send cancel to agent", "error", err)
		return errors.Wrapf(err, "failed to send cancel to agent")
	}

	slog.Info("cancel sent to agent", "commandID", commandID, "agentID", agentID)
	return nil
}

func (d *Dispatcher) RespondPermission(_ context.Context, agentID int, commandID, optionID string) error {
	d.mu.RLock()
	sess, ok := d.sessions[agentID]
	d.mu.RUnlock()

	if !ok {
		return errors.New("agent not connected")
	}

	msg := &v1pb.ManagerStreamMessage{
		Message: &v1pb.ManagerStreamMessage_PermissionDecision{
			PermissionDecision: &v1pb.PermissionDecision{
				CommandId: commandID,
				OptionId:  optionID,
			},
		},
	}

	if err := sess.deliver(msg); err != nil {
		slog.Error("failed to send permission decision to agent", "error", err)
		return errors.Wrapf(err, "failed to send permission decision to agent")
	}

	slog.Info("permission decision sent to agent", "commandID", commandID, "optionID", optionID, "agentID", agentID)
	return nil
}

func (d *Dispatcher) Subscribe(_ context.Context, commandID string) (chan *v1pb.CommandOutput, error) {
	ch := make(chan *v1pb.CommandOutput, watcherBufSize)

	d.mu.Lock()
	if d.watchers[commandID] == nil {
		d.watchers[commandID] = make(map[chan *v1pb.CommandOutput]struct{})
	}
	d.watchers[commandID][ch] = struct{}{}
	d.mu.Unlock()

	return ch, nil
}

func (d *Dispatcher) Unsubscribe(commandID string, ch chan *v1pb.CommandOutput) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if watchers, ok := d.watchers[commandID]; ok {
		delete(watchers, ch)
		close(ch)
		if len(watchers) == 0 {
			delete(d.watchers, commandID)
		}
	}
}

func (d *Dispatcher) SubscribeEvents(_ context.Context, commandID string) (chan *v1pb.CommandEvent, error) {
	ch := make(chan *v1pb.CommandEvent, watcherBufSize)

	d.mu.Lock()
	if d.eventWatchers[commandID] == nil {
		d.eventWatchers[commandID] = make(map[chan *v1pb.CommandEvent]struct{})
	}
	d.eventWatchers[commandID][ch] = struct{}{}
	d.mu.Unlock()

	return ch, nil
}

func (d *Dispatcher) UnsubscribeEvents(commandID string, ch chan *v1pb.CommandEvent) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if watchers, ok := d.eventWatchers[commandID]; ok {
		delete(watchers, ch)
		close(ch)
		if len(watchers) == 0 {
			delete(d.eventWatchers, commandID)
		}
	}
}

func (d *Dispatcher) broadcast(commandID string, output *v1pb.CommandOutput) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	for ch := range d.watchers[commandID] {
		select {
		case ch <- output:
		default:
		}
	}
}

func (d *Dispatcher) broadcastEvent(commandID string, event *v1pb.CommandEvent) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	for ch := range d.eventWatchers[commandID] {
		select {
		case ch <- event:
		default:
		}
	}
}

func (d *Dispatcher) HandleProgress(ctx context.Context, _ int, progress *v1pb.CommandProgress) error {
	commanID, err := uuid.Parse(progress.GetCommandId())
	if err != nil {
		return errors.Wrap(err, "progress commandId parse failed")
	}

	if err := d.store.AppendCommandOutput(ctx, commanID, progress.SeqNo, int32(progress.Type), progress.Content); err != nil {
		return errors.Wrapf(err, "failed to store command output")
	}

	output := &v1pb.CommandOutput{
		CommandId: progress.CommandId,
		Type:      progress.Type,
		Content:   progress.Content,
		SeqNo:     progress.SeqNo,
		// CommandProgress carries no timestamp; stamp the live broadcast with
		// now so the frontend timeline can order it against tool-call events
		// (which carry their own timestamp). Without this, streamed outputs sort
		// to the top (zero ts) and tool cards sink to the bottom. Historical
		// replay (WatchCommand) reads created_at from the DB, so this only
		// affects the live path.
		Timestamp: timestamppb.Now(),
	}

	d.broadcast(progress.CommandId, output)
	return nil
}

func (d *Dispatcher) HandleEvent(ctx context.Context, event *v1pb.CommandEvent) error {
	cmdID, err := uuid.Parse(event.CommandId)
	if err != nil {
		return errors.Wrapf(err, "invalid command ID in event")
	}

	payloadJSON := "{}"
	data, err := marshalEventPayload(event)
	if err != nil {
		return errors.Wrapf(err, "failed to marshal command event payload")
	}
	if data != nil {
		payloadJSON = string(data)
	}

	if err := d.store.AppendCommandEvent(ctx, &store.CommandEventMessage{
		CommandID:   cmdID,
		SeqNo:       event.SeqNo,
		EventType:   int32(event.Type),
		Summary:     event.Summary,
		PayloadJSON: payloadJSON,
	}); err != nil {
		return errors.Wrapf(err, "failed to store command event")
	}

	if err := d.store.UpdateCommandAckSeq(ctx, cmdID, event.SeqNo); err != nil {
		slog.Error("failed to update command ack seq from event", "commandID", event.CommandId, "error", err)
	}

	d.broadcastEvent(event.CommandId, event)
	return nil
}

func (d *Dispatcher) HandleResult(ctx context.Context, agentID int, result *v1pb.CommandResult) error {
	cmdID, err := uuid.Parse(result.CommandId)
	if err != nil {
		return errors.Wrapf(err, "invalid command ID in result")
	}

	d.mu.RLock()
	sess, ok := d.sessions[agentID]
	d.mu.RUnlock()

	if ok {
		sess.mu.Lock()
		if sess.currentCmdID == result.CommandId {
			sess.currentCmdID = ""
		}
		sess.mu.Unlock()
	}

	status := int32(v1pb.CommandStatus_COMPLETED)
	errorMsg := result.ErrorMessage
	if result.ExitCode != 0 {
		status = int32(v1pb.CommandStatus_FAILED)
	}

	now := time.Now()
	completedAt := &now
	durationMs := result.DurationMs
	exitCode := result.ExitCode

	if err := d.store.UpdateCommandStatus(ctx, cmdID, status, nil, completedAt, &exitCode, &durationMs, errorMsg); err != nil {
		return errors.Wrapf(err, "failed to update command result")
	}

	if err := d.store.UpdateCommandAckSeq(ctx, cmdID, result.LastSeqNo); err != nil {
		slog.Error("failed to update ack seq", "commandID", cmdID, "error", err)
	}

	resultJSON := ""
	if result.Result != nil {
		data, err := protojson.Marshal(result.Result)
		if err != nil {
			slog.Error("failed to marshal command result struct", "commandID", result.CommandId, "error", err)
		} else {
			resultJSON = string(data)
		}
	}
	if err := d.store.UpdateCommandResultSummary(ctx, cmdID, result.FinalSummary, resultJSON); err != nil {
		slog.Error("failed to update command result summary", "commandID", cmdID, "error", err)
	}

	output := &v1pb.CommandOutput{
		CommandId: result.CommandId,
		Type:      v1pb.CommandOutput_SYSTEM,
		Content:   formatResultMessage(result),
		SeqNo:     result.LastSeqNo + 1,
		Timestamp: timestamppb.Now(),
	}
	d.broadcast(result.CommandId, output)

	go func() {
		time.Sleep(100 * time.Millisecond)
		d.closeWatchers(result.CommandId)
		d.closeEventWatchers(result.CommandId)
	}()

	slog.Info("command completed", "commandID", result.CommandId, "exitCode", result.ExitCode, "duration_ms", result.DurationMs)

	// The agent's autonomous drain loop decides whether to open another
	// session (BeginSession will report idle if no channel has updates), so
	// the manager no longer pushes the next command here.
	return nil
}

func (d *Dispatcher) HandlePing(agentID int, _ *v1pb.Ping) {
	d.mu.RLock()
	sess, ok := d.sessions[agentID]
	d.mu.RUnlock()

	if ok {
		sess.mu.Lock()
		sess.lastPingAt = time.Now()
		sess.mu.Unlock()
	}
}

// StartPingMonitor launches the liveness ticker. It runs until Stop cancels
// the dispatcher's lifecycle context, and is tracked on the dispatcher's
// WaitGroup so shutdown joins it. Previously the goroutine had no context and
// no join, so it ran for the whole process lifetime with no way to stop it.
func (d *Dispatcher) StartPingMonitor() {
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		ticker := time.NewTicker(d.pingInterval)
		defer ticker.Stop()

		for {
			select {
			case <-d.lifecycleCtx.Done():
				return
			case <-ticker.C:
				d.checkSessionLiveness()
			}
		}
	}()
}

// Stop cancels the dispatcher's lifecycle context (ping monitor + any
// in-flight grace goroutines) and waits for them to exit. Idempotent.
func (d *Dispatcher) Stop() {
	d.lifecycleCancel()
	d.wg.Wait()
}

func (d *Dispatcher) checkSessionLiveness() {
	d.mu.RLock()
	sessions := make([]*AgentSession, 0, len(d.sessions))
	for _, sess := range d.sessions {
		sessions = append(sessions, sess)
	}
	d.mu.RUnlock()

	now := time.Now()
	for _, sess := range sessions {
		sess.mu.Lock()
		idle := now.Sub(sess.lastPingAt)
		agentID := sess.agentID
		sess.mu.Unlock()

		// Skip invalidated sessions (send is an atomic pointer now, not
		// guarded by mu).
		if sess.send.Load() == nil {
			continue
		}

		if idle > d.pingTimeout {
			slog.Warn("agent ping timeout, unregistering",
				"agentID", agentID,
				"idle", idle,
				"timeout", d.pingTimeout)
			d.UnregisterAgent(agentID)
		}
	}
}

// startGracePeriod arms a cancellable 60s timer that, if it fires, marks the
// given command FAILED. The timer is tracked in d.grace so a reconnect can
// cancel it (the reconnect path reaps stale commands itself). The goroutine
// is tracked on the dispatcher's WaitGroup so Stop joins it.
func (d *Dispatcher) startGracePeriod(agentID int, commandID string) {
	ctx, cancel := context.WithCancel(d.lifecycleCtx)

	d.graceMu.Lock()
	cmds := d.grace[agentID]
	if cmds == nil {
		cmds = make(map[string]context.CancelFunc)
		d.grace[agentID] = cmds
	}
	cmds[commandID] = cancel
	d.graceMu.Unlock()

	d.wg.Add(1)
	go d.handleCommandGracePeriod(ctx, agentID, commandID)
}

// cancelGraceForAgent cancels every pending grace timer for an agent. Called
// on reconnect so a dangling 60s "mark FAILED" does not race the new session.
func (d *Dispatcher) cancelGraceForAgent(agentID int) {
	d.graceMu.Lock()
	cmds := d.grace[agentID]
	delete(d.grace, agentID)
	d.graceMu.Unlock()
	for _, cancel := range cmds {
		cancel()
	}
}

// finishGrace removes a grace timer's entry once its goroutine exits.
func (d *Dispatcher) finishGrace(agentID int, commandID string) {
	d.graceMu.Lock()
	defer d.graceMu.Unlock()
	if cmds := d.grace[agentID]; cmds != nil {
		delete(cmds, commandID)
		if len(cmds) == 0 {
			delete(d.grace, agentID)
		}
	}
}

func (d *Dispatcher) handleCommandGracePeriod(ctx context.Context, agentID int, commandID string) {
	defer d.wg.Done()
	defer d.finishGrace(agentID, commandID)

	cmdUUID, err := uuid.Parse(commandID)
	if err != nil {
		return
	}

	// A cancellable timer instead of a bare time.Sleep: a reconnect cancels
	// this context via cancelGraceForAgent, so the timer does not mark a
	// command FAILED out from under the new session.
	select {
	case <-ctx.Done():
		return
	case <-time.After(gracePeriod):
	}

	// Belt-and-suspenders: if the agent reconnected between the timer firing
	// and here, the reconnect path reaps the stale command — leave it alone.
	d.mu.RLock()
	_, reconnected := d.sessions[agentID]
	d.mu.RUnlock()
	if reconnected {
		return
	}

	// Bound the DB call so a hung Postgres does not accumulate blocked grace
	// goroutines. Previously this used a bare context.Background() with no
	// deadline.
	dbCtx, cancel := context.WithTimeout(d.lifecycleCtx, graceDBTimeout)
	defer cancel()
	status := int32(v1pb.CommandStatus_FAILED)
	now := time.Now()
	if err := d.store.UpdateCommandStatus(dbCtx, cmdUUID, status, nil, &now, nil, nil, "agent disconnected during execution"); err != nil {
		slog.Error("failed to mark command as failed after grace period", "commandID", commandID, "error", err)
	}

	d.closeWatchers(commandID)
	slog.Warn("command marked as FAILED after grace period", "commandID", commandID, "agentID", agentID)
}

func (d *Dispatcher) closeWatchers(commandID string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	for ch := range d.watchers[commandID] {
		close(ch)
	}
	delete(d.watchers, commandID)
}

func (d *Dispatcher) closeEventWatchers(commandID string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	for ch := range d.eventWatchers[commandID] {
		close(ch)
	}
	delete(d.eventWatchers, commandID)
}

func formatResultMessage(result *v1pb.CommandResult) string {
	if result.ErrorMessage != "" {
		return result.ErrorMessage
	}
	return ""
}

func ConvertChatMessageToV1(m *store.ChatMessage) *v1pb.ChatMessage {
	cm := &v1pb.ChatMessage{
		Name:          m.ID.String(),
		Conversation:  m.ConversationID.String(),
		PrincipalName: m.PrincipalName,
		Role:          m.Role,
		Content:       m.Content,
		CreatedAt:     timestamppb.New(m.CreatedAt),
		SenderName:    m.AgentName,
		SenderType:    v1pb.SenderType(m.SenderType),
		RoomVersion:   m.RoomVersion,
		Mentions:      m.Mentions,
	}
	if m.CommandID.Valid {
		cm.CommandId = m.CommandID.UUID.String()
	}
	if m.SenderType != store.SenderTypeAgent {
		cm.SenderName = m.PrincipalName
	}
	return cm
}

func marshalEventPayload(event *v1pb.CommandEvent) ([]byte, error) {
	switch event.Type {
	case v1pb.CommandEventType_LIFECYCLE:
		return protojson.Marshal(event.GetLifecycle())
	case v1pb.CommandEventType_TEXT_DELTA:
		return protojson.Marshal(event.GetTextDelta())
	case v1pb.CommandEventType_TOOL_CALL_STARTED:
		return protojson.Marshal(event.GetToolCallStarted())
	case v1pb.CommandEventType_TOOL_CALL_FINISHED:
		return protojson.Marshal(event.GetToolCallFinished())
	case v1pb.CommandEventType_DIFF_EMITTED:
		return protojson.Marshal(event.GetDiffEmitted())
	case v1pb.CommandEventType_WARNING:
		return protojson.Marshal(event.GetWarning())
	case v1pb.CommandEventType_RAW_ACP:
		return protojson.Marshal(event.GetRawAcp())
	case v1pb.CommandEventType_FINAL_SUMMARY:
		return protojson.Marshal(event.GetFinalSummary())
	case v1pb.CommandEventType_PERMISSION_REQUESTED:
		return protojson.Marshal(event.GetPermissionRequested())
	case v1pb.CommandEventType_PERMISSION_TIMED_OUT:
		return protojson.Marshal(event.GetPermissionTimedOut())
	case v1pb.CommandEventType_PERMISSION_DECIDED:
		return protojson.Marshal(event.GetPermissionDecided())
	default:
		return nil, nil
	}
}
