package dispatcher

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/encoding/protojson"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/manager/store"
)

const (
	gracePeriod    = 60 * time.Second
	watcherBufSize = 256
)

type SendFunc func(*v1pb.ManagerCommandMessage) error

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
		pingTimeout:   45 * time.Second, // allow 3 missed pings
	}
}

func (d *Dispatcher) RegisterAgent(_ context.Context, agentID int, agentResourceID string, send SendFunc) *AgentSession {
	d.mu.Lock()
	defer d.mu.Unlock()

	if old, ok := d.sessions[agentID]; ok {
		slog.Info("replacing existing agent session", "agentID", agentID)
		old.mu.Lock()
		old.send = nil // invalidate old session's send function
		old.mu.Unlock()
	}

	sess := &AgentSession{
		agentID:         agentID,
		agentResourceID: agentResourceID,
		connectedAt:     time.Now(),
		lastPingAt:      time.Now(),
	}
	sess.send = func(msg *v1pb.ManagerCommandMessage) error {
		sess.sendMu.Lock()
		defer sess.sendMu.Unlock()
		return send(msg)
	}

	d.sessions[agentID] = sess

	slog.Info("agent registered for command dispatch", "agentID", agentID)
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

func (d *Dispatcher) DispatchCommand(ctx context.Context, cmd *store.CommandMessage) error {
	d.mu.RLock()
	sess, ok := d.sessions[cmd.AgentID]
	d.mu.RUnlock()

	if !ok {
		return errors.New("agent not connected")
	}

	sess.mu.Lock()
	if sess.currentCmdID != "" {
		sess.mu.Unlock()
		return nil // agent is busy, command stays PENDING in DB
	}
	if sess.send == nil {
		sess.mu.Unlock()
		return errors.New("agent session invalidated")
	}
	send := sess.send
	sess.mu.Unlock()

	msg := &v1pb.ManagerCommandMessage{
		Message: &v1pb.ManagerCommandMessage_CommandRequest{
			CommandRequest: &v1pb.CommandRequest{
				CommandId:      cmd.ID.String(),
				Command:        cmd.Command,
				Env:            parseEnvJSON(cmd.Env),
				WorkingDir:     cmd.WorkingDir,
				TimeoutSeconds: cmd.TimeoutSeconds,
				ExecutorKind:   v1pb.ExecutorKind(cmd.ExecutorKind),
				Instruction:    cmd.Instruction,
				Profile:        cmd.Profile,
				AllowDiff:      cmd.AllowDiff,
				Source:         v1pb.CommandSource(cmd.SourceType),
				PrincipalId:    fmt.Sprintf("%d", cmd.PrincipalID),
			},
		},
	}

	if err := send(msg); err != nil {
		d.UnregisterAgent(cmd.AgentID)
		return errors.Wrapf(err, "failed to send command to agent")
	}

	sess.mu.Lock()
	sess.currentCmdID = cmd.ID.String()
	sess.mu.Unlock()

	now := time.Now()
	if err := d.store.UpdateCommandStatus(ctx, cmd.ID, 2, &now, nil, nil, nil, ""); err != nil {
		slog.Error("failed to update command status to RUNNING", "commandID", cmd.ID, "error", err)
	}

	slog.Info("command dispatched to agent", "commandID", cmd.ID, "agentID", cmd.AgentID)
	return nil
}

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

	msg := &v1pb.ManagerCommandMessage{
		Message: &v1pb.ManagerCommandMessage_Cancel{
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

	msg := &v1pb.ManagerCommandMessage{
		Message: &v1pb.ManagerCommandMessage_PermissionDecision{
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
			// watcher too slow, drop
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
			// watcher too slow, drop
		}
	}
}

func (d *Dispatcher) HandleProgress(ctx context.Context, _ int, progress *v1pb.CommandProgress) error {
	if err := d.store.AppendCommandOutput(ctx, uuid.MustParse(progress.CommandId), progress.SeqNo, int32(progress.Type), progress.Content); err != nil {
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

	cmd, cmdLoadErr := d.store.GetCommand(ctx, cmdID)
	if cmdLoadErr != nil {
		slog.Warn("failed to load command for result handling", "commandID", cmdID, "error", cmdLoadErr)
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

	if cmd != nil && cmd.SourceType == 2 && cmd.ConversationID != nil && result.FinalSummary != "" {
		if _, msgErr := d.store.CreateChatMessage(ctx, &store.ChatMessage{
			ConversationID: *cmd.ConversationID,
			PrincipalID:    cmd.PrincipalID,
			Role:           2, // ASSISTANT
			Content:        result.FinalSummary,
			CommandID:      uuid.NullUUID{UUID: cmdID, Valid: true},
		}); msgErr != nil {
			slog.Error("failed to create assistant chat message", "commandID", cmdID, "error", msgErr)
		}
	}

	output := &v1pb.CommandOutput{
		CommandId: result.CommandId,
		Type:      v1pb.CommandOutput_SYSTEM,
		Content:   formatResultMessage(result),
		SeqNo:     result.LastSeqNo + 1,
	}
	d.broadcast(result.CommandId, output)

	// close watchers after a short delay so they can consume the final message
	go func() {
		time.Sleep(100 * time.Millisecond)
		d.closeWatchers(result.CommandId)
		d.closeEventWatchers(result.CommandId)
	}()

	slog.Info("command completed", "commandID", result.CommandId, "exitCode", result.ExitCode, "duration_ms", result.DurationMs)

	// dispatch next pending command if available
	d.TryDispatchNext(ctx, agentID)
	return nil
}

func (d *Dispatcher) TryDispatchNext(ctx context.Context, agentID int) {
	d.mu.RLock()
	sess, ok := d.sessions[agentID]
	d.mu.RUnlock()
	if !ok {
		return
	}

	sess.mu.Lock()
	busy := sess.currentCmdID != ""
	sess.mu.Unlock()
	if busy {
		return
	}

	cmd, err := d.store.GetNextPendingCommand(ctx, agentID)
	if err != nil {
		slog.Error("failed to get next pending command", "agentID", agentID, "error", err)
		return
	}
	if cmd == nil {
		return
	}

	if err := d.DispatchCommand(ctx, cmd); err != nil {
		slog.Error("failed to dispatch next command", "commandID", cmd.ID, "error", err)
	}
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

// StartPingMonitor starts a goroutine that periodically checks agent liveness.
// If an agent has not sent a Ping within pingTimeout, it is unregistered.
// This goroutine does NOT send any messages on the bidi stream (to avoid
// concurrent writes with the handler goroutine). Ping/Pong responses are
// handled exclusively by the AgentCommandService handler.
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

func parseEnvJSON(_ string) map[string]string {
	return nil
}

func formatResultMessage(result *v1pb.CommandResult) string {
	if result.ErrorMessage != "" {
		return result.ErrorMessage
	}
	return ""
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
