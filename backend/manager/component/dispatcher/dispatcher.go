package dispatcher

import (
	"context"
	"log/slog"
	"sync"
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
	watcherBufSize = 256
)

type SendFunc func(*v1pb.ManagerStreamMessage) error

type AgentSession struct {
	agentID         int
	agentResourceID string
	currentCmdID    string
	send            SendFunc
	sendMu          sync.Mutex
	lastPingAt      time.Time
	connectedAt     time.Time
	mu              sync.Mutex
}

// Send sends a message to the agent over its bidi stream. It is safe for
// concurrent use (e.g. from the Phase 2 held-action re-prompt path).
func (s *AgentSession) Send(msg *v1pb.ManagerStreamMessage) error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	if s.send == nil {
		return errors.New("agent session invalidated")
	}
	return s.send(msg)
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
}

func New(s *store.Store) *Dispatcher {
	return &Dispatcher{
		store:         s,
		sessions:      make(map[int]*AgentSession),
		watchers:      make(map[string]map[chan *v1pb.CommandOutput]struct{}),
		eventWatchers: make(map[string]map[chan *v1pb.CommandEvent]struct{}),
		pingInterval:  15 * time.Second,
		pingTimeout:   45 * time.Second,
	}
}

func (d *Dispatcher) RegisterAgent(_ context.Context, agentID int, agentResourceID string, send SendFunc) *AgentSession {
	d.mu.Lock()
	defer d.mu.Unlock()

	if old, ok := d.sessions[agentID]; ok {
		slog.Info("replacing existing agent session", "agentID", agentID)
		old.mu.Lock()
		old.send = nil
		old.mu.Unlock()
	}

	sess := &AgentSession{
		agentID:         agentID,
		agentResourceID: agentResourceID,
		connectedAt:     time.Now(),
		lastPingAt:      time.Now(),
	}
	sess.send = func(msg *v1pb.ManagerStreamMessage) error {
		sess.sendMu.Lock()
		defer sess.sendMu.Unlock()
		return send(msg)
	}

	d.sessions[agentID] = sess
	slog.Info("agent registered for command dispatch", "agentID", agentID)

	// The agent drives its own work via BeginSession; the manager no longer
	// pushes commands on connect. The agent sends AgentReady (handled in the
	// bidi loop) and then its drain loop calls BeginSession as needed.
	return sess
}

func (d *Dispatcher) UnregisterAgent(agentID int) {
	d.mu.Lock()
	defer d.mu.Unlock()

	sess, ok := d.sessions[agentID]
	if !ok {
		return
	}

	sess.mu.Lock()
	cmdID := sess.currentCmdID
	sess.send = nil
	sess.mu.Unlock()

	delete(d.sessions, agentID)
	slog.Info("agent unregistered from command dispatch", "agentID", agentID)

	if cmdID != "" {
		go d.handleCommandGracePeriod(agentID, cmdID)
	}
}

func (d *Dispatcher) IsAgentConnected(agentID int) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	_, ok := d.sessions[agentID]
	return ok
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
	if !hasUpdates {
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

	sess.mu.Lock()
	send := sess.send
	sess.mu.Unlock()

	if send == nil {
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

	if err := send(msg); err != nil {
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

	sess.mu.Lock()
	send := sess.send
	sess.mu.Unlock()
	if send == nil {
		return
	}

	msg := &v1pb.ManagerStreamMessage{
		Message: &v1pb.ManagerStreamMessage_NewMessages{
			NewMessages: &v1pb.NewMessagesAvailable{},
		},
	}
	if err := send(msg); err != nil {
		slog.Warn("failed to send wake to agent", "agentID", agentID, "error", err)
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

	sess.mu.Lock()
	send := sess.send
	sess.mu.Unlock()

	if send == nil {
		return errors.New("agent session invalidated")
	}

	msg := &v1pb.ManagerStreamMessage{
		Message: &v1pb.ManagerStreamMessage_Cancel{
			Cancel: &v1pb.CancelMessage{
				CommandId: commandID,
			},
		},
	}

	if err := send(msg); err != nil {
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

	sess.mu.Lock()
	send := sess.send
	sess.mu.Unlock()

	if send == nil {
		return errors.New("agent session invalidated")
	}

	msg := &v1pb.ManagerStreamMessage{
		Message: &v1pb.ManagerStreamMessage_PermissionDecision{
			PermissionDecision: &v1pb.PermissionDecision{
				CommandId: commandID,
				OptionId:  optionID,
			},
		},
	}

	if err := send(msg); err != nil {
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

func (d *Dispatcher) StartPingMonitor() {
	go func() {
		ticker := time.NewTicker(d.pingInterval)
		defer ticker.Stop()

		for range ticker.C {
			d.checkSessionLiveness()
		}
	}()
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
		send := sess.send
		sess.mu.Unlock()

		if send == nil {
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

func (d *Dispatcher) handleCommandGracePeriod(agentID int, commandID string) {
	cmdUUID, err := uuid.Parse(commandID)
	if err != nil {
		return
	}

	time.Sleep(gracePeriod)

	d.mu.RLock()
	_, reconnected := d.sessions[agentID]
	d.mu.RUnlock()

	if reconnected {
		return
	}

	status := int32(v1pb.CommandStatus_FAILED)
	now := time.Now()
	if err := d.store.UpdateCommandStatus(context.Background(), cmdUUID, status, nil, &now, nil, nil, "agent disconnected during execution"); err != nil {
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
