package dispatcher

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/laelia/backend/common"
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

	// NOTE: PENDING command draining is deferred until the agent sends
	// AgentReady (see DispatchPending), preserving the legacy ordering where
	// the agent signals readiness before the manager dispatches work.
	return sess
}

// DispatchPending triggers a best-effort drain of PENDING commands for an
// agent. It is invoked after the agent sends AgentReady and (idempotently)
// after each HandleResult completes.
func (d *Dispatcher) DispatchPending(ctx context.Context, agentID int) {
	d.dispatchNextPending(ctx, agentID)
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

// EnqueueCommand dispatches a freshly-created PENDING command immediately if the
// agent is connected and idle; otherwise it relies on the persisted PENDING row
// being picked up later by RegisterAgent's drain or by HandleResult's
// dispatchNextPending call. This replaces the removed agent_inbox creation.
func (d *Dispatcher) EnqueueCommand(ctx context.Context, cmd *store.CommandMessage) error {
	d.mu.RLock()
	sess, ok := d.sessions[cmd.AgentID]
	d.mu.RUnlock()

	if !ok {
		slog.Info("agent not connected; command remains PENDING until reconnect", "commandID", cmd.ID, "agentID", cmd.AgentID)
		return nil
	}

	sess.mu.Lock()
	busy := sess.currentCmdID != ""
	sess.mu.Unlock()

	if busy {
		// Another command is executing; it will be picked up via dispatchNextPending after HandleResult.
		return nil
	}

	if err := d.DispatchCommand(ctx, cmd); err != nil {
		slog.Warn("failed to dispatch command immediately; leaving PENDING for retry", "commandID", cmd.ID, "error", err)
		return nil
	}
	return nil
}

// dispatchNextPending loads the oldest PENDING command for an agent and
// dispatches it. It is a no-op if the agent is unknown, busy, or has no pending
// commands. Used by RegisterAgent and HandleResult to drain the queue.
func (d *Dispatcher) dispatchNextPending(ctx context.Context, agentID int) {
	d.mu.RLock()
	sess, ok := d.sessions[agentID]
	d.mu.RUnlock()

	if !ok {
		return
	}

	sess.mu.Lock()
	if sess.currentCmdID != "" {
		sess.mu.Unlock()
		return
	}
	sess.mu.Unlock()

	cmds, err := d.store.ListPendingCommandsByAgent(ctx, agentID)
	if err != nil {
		slog.Error("failed to list pending commands", "agentID", agentID, "error", err)
		return
	}
	if len(cmds) == 0 {
		return
	}

	if err := d.DispatchCommand(ctx, cmds[0]); err != nil {
		slog.Warn("failed to dispatch next pending command", "commandID", cmds[0].ID, "error", err)
	}
}

func (d *Dispatcher) DispatchCommand(ctx context.Context, cmd *store.CommandMessage) error {
	d.mu.RLock()
	sess, ok := d.sessions[cmd.AgentID]
	d.mu.RUnlock()

	if !ok {
		return errors.New("agent not connected")
	}

	sess.mu.Lock()
	if sess.send == nil {
		sess.mu.Unlock()
		return errors.New("agent session invalidated")
	}
	send := sess.send
	sess.mu.Unlock()

	convID := ""
	if cmd.ConversationID != nil {
		convID = cmd.ConversationID.String()
	}

	msg := &v1pb.ManagerStreamMessage{
		Message: &v1pb.ManagerStreamMessage_CommandRequest{
			CommandRequest: &v1pb.CommandRequest{
				CommandId:        cmd.ID.String(),
				Instruction:      cmd.Instruction,
				Profile:          cmd.Profile,
				WorkingDir:       cmd.WorkingDir,
				TimeoutSeconds:   cmd.TimeoutSeconds,
				Env:              parseEnvJSON(cmd.Env),
				AllowDiff:        cmd.AllowDiff,
				PrincipalId:      fmt.Sprintf("%d", cmd.PrincipalID),
				ConversationId:   convID,
				ReplyToMessageId: "",
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

// HandlePullMessages serves a PullMessages request from an agent, returning the
// chat messages newer than afterVersion together with the current conversation
// version. The agent uses the returned current_version as its cursor going
// forward (and, in Phase 2, as base_version for SubmitAction).
func (d *Dispatcher) HandlePullMessages(ctx context.Context, _ int, conversationID string, afterVersion int64) (*v1pb.MessageSnapshot, error) {
	convUUID, err := uuid.Parse(conversationID)
	if err != nil {
		return nil, errors.Wrapf(err, "invalid conversation id")
	}

	msgs, err := d.store.GetMessagesAfterVersion(ctx, convUUID, afterVersion)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get messages after version")
	}

	currentVersion, err := d.store.GetConversationVersion(ctx, convUUID)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get current conversation version")
	}

	snapshot := &v1pb.MessageSnapshot{
		CurrentVersion: currentVersion,
	}
	for _, m := range msgs {
		snapshot.Messages = append(snapshot.Messages, ConvertChatMessageToV1(m))
	}
	return snapshot, nil
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

// HandleSubmitAction processes an agent's SubmitAction request. It performs the
// Held Draft version check: if base_version matches the current conversation
// version the action is committed (command created + dispatched); otherwise it
// is held for the agent to resolve via ResolveHeldAction.
func (d *Dispatcher) HandleSubmitAction(ctx context.Context, agentID int, req *v1pb.SubmitAction) (*v1pb.ActionResponse, error) {
	convUUID, err := uuid.Parse(req.ConversationId)
	if err != nil {
		return nil, errors.Wrapf(err, "invalid conversation id")
	}

	currentVersion, err := d.store.GetConversationVersion(ctx, convUUID)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get conversation version")
	}

	// Version match: commit immediately.
	if req.BaseVersion == currentVersion {
		cmd, cmdErr := d.createCommandFromAction(ctx, agentID, req, convUUID)
		if cmdErr != nil {
			return nil, cmdErr
		}
		return &v1pb.ActionResponse{
			ActionId:       uuid.New().String(),
			Committed:      true,
			CommandId:      cmd.ID.String(),
			CurrentVersion: currentVersion,
		}, nil
	}

	// Version mismatch: hold the action.
	newMsgs, msgErr := d.store.GetMessagesAfterVersion(ctx, convUUID, req.BaseVersion)
	if msgErr != nil {
		return nil, errors.Wrapf(msgErr, "failed to get new messages for held action")
	}

	ha, haErr := d.store.CreateHeldAction(ctx, agentID, convUUID, req, req.BaseVersion, currentVersion)
	if haErr != nil {
		return nil, errors.Wrapf(haErr, "failed to create held action")
	}

	resp := &v1pb.ActionResponse{
		ActionId:       ha.ID.String(),
		Committed:      false,
		CurrentVersion: currentVersion,
	}
	for _, m := range newMsgs {
		resp.NewMessages = append(resp.NewMessages, ConvertChatMessageToV1(m))
	}
	return resp, nil
}

// HandleResolveHeldAction processes an agent's resolution of a previously held
// action. REVISE returns without creating a command (the agent re-pulls and
// re-submits). SEND_AS_IS and FORCE_SEND create and dispatch the command.
// DISCARD simply marks the action resolved.
func (d *Dispatcher) HandleResolveHeldAction(ctx context.Context, agentID int, req *v1pb.ResolveHeldAction) (*v1pb.ManagerStreamMessage, error) {
	actionUUID, err := uuid.Parse(req.ActionId)
	if err != nil {
		return nil, errors.Wrapf(err, "invalid action id")
	}

	switch req.Resolution {
	case v1pb.ActionResolution_REVISE:
		if err := d.store.ResolveHeldAction(ctx, actionUUID, int32(req.Resolution), uuid.NullUUID{}); err != nil {
			return nil, errors.Wrapf(err, "failed to resolve held action")
		}
		// Agent will re-PullMessages and re-SubmitAction; nothing to send now.
		return nil, nil

	case v1pb.ActionResolution_SEND_AS_IS, v1pb.ActionResolution_FORCE_SEND:
		// Parse the original SubmitAction from the held action record.
		actions, lookupErr := d.store.GetHeldActionsByAgent(ctx, agentID)
		if lookupErr != nil {
			return nil, errors.Wrapf(lookupErr, "failed to look up held actions")
		}
		var ha *store.HeldAction
		for _, a := range actions {
			if a.ID == actionUUID {
				ha = a
				break
			}
		}
		if ha == nil {
			return nil, errors.Errorf("held action %s not found", req.ActionId)
		}

		var submitReq v1pb.SubmitAction
		if unmarshalErr := common.ProtojsonUnmarshaler.Unmarshal([]byte(ha.ActionJSON), &submitReq); unmarshalErr != nil {
			return nil, errors.Wrapf(unmarshalErr, "failed to unmarshal held submit action")
		}

		cmd, cmdErr := d.createCommandFromAction(ctx, agentID, &submitReq, ha.ConversationID)
		if cmdErr != nil {
			return nil, cmdErr
		}

		cmdUUID := uuid.NullUUID{UUID: cmd.ID, Valid: true}
		if err := d.store.ResolveHeldAction(ctx, actionUUID, int32(req.Resolution), cmdUUID); err != nil {
			return nil, errors.Wrapf(err, "failed to resolve held action")
		}

		return &v1pb.ManagerStreamMessage{
			Message: &v1pb.ManagerStreamMessage_CommandRequest{
				CommandRequest: &v1pb.CommandRequest{
					CommandId:        cmd.ID.String(),
					Instruction:      cmd.Instruction,
					Profile:          cmd.Profile,
					WorkingDir:       cmd.WorkingDir,
					TimeoutSeconds:   cmd.TimeoutSeconds,
					Env:              parseEnvJSON(cmd.Env),
					AllowDiff:        cmd.AllowDiff,
					PrincipalId:      fmt.Sprintf("%d", cmd.PrincipalID),
					ConversationId:   ha.ConversationID.String(),
					ReplyToMessageId: submitReq.ReplyToMessageId,
				},
			},
		}, nil

	case v1pb.ActionResolution_DISCARD:
		if err := d.store.ResolveHeldAction(ctx, actionUUID, int32(req.Resolution), uuid.NullUUID{}); err != nil {
			return nil, errors.Wrapf(err, "failed to resolve held action")
		}
		return nil, nil

	default:
		return nil, errors.Errorf("unknown action resolution: %v", req.Resolution)
	}
}

// createCommandFromAction creates a PENDING command from a SubmitAction and
// enqueues it for dispatch. It is the shared path for both committed (version
// match) and force-resolved (SEND_AS_IS / FORCE_SEND) submissions.
func (d *Dispatcher) createCommandFromAction(ctx context.Context, agentID int, req *v1pb.SubmitAction, convUUID uuid.UUID) (*store.CommandMessage, error) {
	agent, err := d.store.GetAgent(ctx, agentID)
	if err != nil || agent == nil {
		return nil, errors.New("agent not found")
	}

	principalID := 1 // default to system bot; the caller can override
	envBytes, _ := json.Marshal(req.Env)

	cmd := &store.CommandMessage{
		AgentID:        agentID,
		PrincipalID:    principalID,
		Command:        "",
		Instruction:    req.Instruction,
		Profile:        req.Profile,
		AllowDiff:      req.AllowDiff,
		Status:         1, // PENDING
		Env:            string(envBytes),
		WorkingDir:     req.WorkingDir,
		TimeoutSeconds: req.TimeoutSeconds,
		ConversationID: &convUUID,
	}

	created, err := d.store.CreateCommand(ctx, cmd)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create command from action")
	}
	created.AgentResourceID = agent.ResourceID

	if err := d.EnqueueCommand(ctx, created); err != nil {
		slog.Warn("failed to enqueue command from action", "commandID", created.ID, "error", err)
	}
	return created, nil
}

// GetHeldActionsForAgent returns the held actions (state=HELD) for an agent.
// Used by the API handler during AgentReady to re-prompt the agent.
func (d *Dispatcher) GetHeldActionsForAgent(ctx context.Context, agentID int) ([]*store.HeldAction, error) {
	return d.store.GetHeldActionsByAgent(ctx, agentID)
}

// StartExpireHeldActions starts a background goroutine that scans for expired
// held actions every minute and marks them state=EXPIRED.
func (d *Dispatcher) StartExpireHeldActions() {
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			n, err := d.store.ExpireHeldActions(context.Background())
			if err != nil {
				slog.Error("failed to expire held actions", "error", err)
			} else if n > 0 {
				slog.Info("expired held actions", "count", n)
			}
		}
	}()
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

	// Agent is now idle; dispatch the next PENDING command for this agent.
	go d.dispatchNextPending(context.Background(), agentID)
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

func parseEnvJSON(_ string) map[string]string {
	return nil
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
