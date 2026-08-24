package dispatcher

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pkg/errors"

	"github.com/Ranxy/laelia/backend/common"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/manager/store"
)

const (
	gracePeriod    = 60 * time.Second
	graceDBTimeout = 10 * time.Second
	watcherBufSize = 256
)

type SendFunc func(*v1pb.ManagerStreamMessage) error

// MachineSendFunc is the raw send function for a machine's MachineChannel
// control stream (manager→machine direction).
type MachineSendFunc func(*v1pb.ManagerMachineStreamMessage) error

type AgentSession struct {
	agentID         int
	agentResourceID string
	// machineID is the id of the machine this agent's AgentChannel belongs to.
	// Set at RegisterAgent; 0 for legacy/unbound agents. Used by UnregisterMachine
	// to invalidate every agent session owned by a disconnecting machine.
	machineID    int
	currentCmdID string
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

// MachineSession is the manager-side handle on a connected machine's
// MachineChannel control stream. Mirrors AgentSession: the machine app
// authenticates once and holds this stream for its lifetime; per-agent
// AgentChannels register separately (keyed by agentID) but carry the machineID
// so a machine disconnect invalidates all of them.
type MachineSession struct {
	machineID         int
	machineResourceID string
	send              atomic.Pointer[MachineSendFunc]
	sendMu            sync.Mutex
	lastPingAt        time.Time
	connectedAt       time.Time
	mu                sync.Mutex // guards lastPingAt, connectedAt
}

func (s *MachineSession) deliver(msg *v1pb.ManagerMachineStreamMessage) error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	fn := s.send.Load()
	if fn == nil {
		return errors.New("machine session invalidated")
	}
	return (*fn)(msg)
}

// Send sends a control message to the machine over its MachineChannel.
func (s *MachineSession) Send(msg *v1pb.ManagerMachineStreamMessage) error {
	return s.deliver(msg)
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

// Dispatcher routes control messages to connected agents/machines and fans
// out live command output/events. It must be constructed via New; the zero
// value is not usable because the registry, bus, and activity aggregator are
// nil until New initializes them.
type Dispatcher struct {
	store    *store.Store
	registry *sessionRegistry
	bus      *commandBus
	activity *activityAggregator
	// pingInterval/pingTimeout are kept on the facade until the liveness
	// monitor is extracted alongside the session registry.
	pingInterval time.Duration
	pingTimeout  time.Duration

	// lifecycleCtx is the parent context for the ping monitor and the
	// grace-period goroutines. Stop cancels it and waits on wg, so shutdown
	// joins every dispatcher-spawned goroutine instead of leaving the ping
	// ticker running for the process lifetime.
	lifecycleCtx    context.Context
	lifecycleCancel context.CancelFunc
	wg              sync.WaitGroup
	// wgMu serializes wg.Add against wg.Wait. Stop may call Wait while a
	// stream teardown is concurrently arming a grace goroutine; guarding both
	// operations avoids the WaitGroup "Add concurrent with Wait" misuse.
	wgMu sync.Mutex

	// grace tracks in-flight grace-period timers keyed by agent then command,
	// so a reconnect can cancel a pending "mark FAILED" timer for that agent
	// (the reconnect path reaps stale commands itself). Without this, a
	// reconnect racing the 60s timer could mark a command FAILED out from
	// under the new session.
	graceMu sync.Mutex
	grace   map[int]map[string]context.CancelFunc

	// pendingDiscovers correlates DiscoverProviders request/response round trips
	// over the bidi command stream. Used by the unary RefreshAgentProviders RPC
	// to do a request/response round trip over the bidi command stream.
	pendingDiscovers *pendingReplies[*v1pb.ProvidersDiscovered]

	// pendingWorkspace* correlate the workspace request/response round trips
	// over the per-agent and machine control bidi streams to their waiting
	// unary RPCs (ListAgentWorkspace / ReadAgentWorkspaceFile /
	// ListMachineWorkspaces).
	pendingWorkspaceLists *pendingReplies[*v1pb.WorkspaceListResponse]
	pendingWorkspaceReads *pendingReplies[*v1pb.WorkspaceReadResponse]
	pendingMachineScans   *pendingReplies[*v1pb.MachineWorkspaceScanResponse]

	// machineUpgrades holds the live (or last completed) self-upgrade progress
	// per machine id, reported by the machine over its control stream and read
	// by GetMachine for the frontend. Reset whenever the machine (re)connects.
	upgradeMu       sync.Mutex
	machineUpgrades map[int]*v1pb.UpgradeProgress
}

func New(s *store.Store) *Dispatcher {
	ctx, cancel := context.WithCancel(context.Background())
	registry := newSessionRegistry()
	d := &Dispatcher{
		store:                 s,
		registry:              registry,
		bus:                   newCommandBus(),
		pingInterval:          15 * time.Second,
		pingTimeout:           45 * time.Second,
		grace:                 make(map[int]map[string]context.CancelFunc),
		pendingDiscovers:      newPendingReplies[*v1pb.ProvidersDiscovered](),
		pendingWorkspaceLists: newPendingReplies[*v1pb.WorkspaceListResponse](),
		pendingWorkspaceReads: newPendingReplies[*v1pb.WorkspaceReadResponse](),
		pendingMachineScans:   newPendingReplies[*v1pb.MachineWorkspaceScanResponse](),
		machineUpgrades:       make(map[int]*v1pb.UpgradeProgress),
		lifecycleCtx:          ctx,
		lifecycleCancel:       cancel,
	}
	d.activity = &activityAggregator{store: s, registry: registry}
	return d
}

// sendToMachine is the single machine-session send path: look up the connected
// machine session and deliver a control message, returning an error when the
// machine is offline.
func (d *Dispatcher) sendToMachine(machineID int, msg *v1pb.ManagerMachineStreamMessage) error {
	return d.registry.sendToMachine(machineID, msg)
}

// sendToAgent is the single agent-session send path: look up the connected
// agent session and deliver a control message, returning an error when the
// agent is offline.
func (d *Dispatcher) sendToAgent(agentID int, msg *v1pb.ManagerStreamMessage) error {
	return d.registry.sendToAgent(agentID, msg)
}

// SendAgentAssignment pushes a new agent assignment to the machine so it opens
// an AgentChannel for that agent. Best-effort: if the machine is offline the
// agent is picked up from the assigned_agents list on the next ConnectMachine.
func (d *Dispatcher) SendAgentAssignment(machineID int, assignment *v1pb.AgentAssignment) error {
	return d.sendToMachine(machineID, &v1pb.ManagerMachineStreamMessage{
		Message: &v1pb.ManagerMachineStreamMessage_AgentAssignment{
			AgentAssignment: assignment,
		},
	})
}

// SendAgentConfigUpdate hot-reloads an agent's ACP config on its runner without
// restarting it (picked up at the next BeginSession).
func (d *Dispatcher) SendAgentConfigUpdate(machineID int, agentName string, cfg *v1pb.AgentACPConfig) error {
	return d.sendToMachine(machineID, &v1pb.ManagerMachineStreamMessage{
		Message: &v1pb.ManagerMachineStreamMessage_AgentConfigUpdate{
			AgentConfigUpdate: &v1pb.AgentConfigUpdate{
				AgentName: agentName,
				AcpConfig: cfg,
			},
		},
	})
}

// SendRemoveAgent tears down an agent's runner on the machine (used on
// DeleteAgent).
func (d *Dispatcher) SendRemoveAgent(machineID int, agentName string) error {
	return d.sendToMachine(machineID, &v1pb.ManagerMachineStreamMessage{
		Message: &v1pb.ManagerMachineStreamMessage_RemoveAgent{
			RemoveAgent: &v1pb.RemoveAgent{AgentName: agentName},
		},
	})
}

// SendReloadAgentAssignment re-syncs a single agent's full assignment (used
// after a display-name or config change to re-establish a runner).
func (d *Dispatcher) SendReloadAgentAssignment(machineID int, reload *v1pb.ReloadAgentAssignment) error {
	return d.sendToMachine(machineID, &v1pb.ManagerMachineStreamMessage{
		Message: &v1pb.ManagerMachineStreamMessage_ReloadAgentAssignment{
			ReloadAgentAssignment: reload,
		},
	})
}

// SendDiscoverProvidersToMachine asks a connected machine to re-probe its host
// providers and reply with ProvidersDiscovered. The reply resolves a pending
// discover registered via RegisterPendingDiscover (requestID is globally
// unique, so the existing agent-scoped pending map is reused).
func (d *Dispatcher) SendDiscoverProvidersToMachine(machineID int, requestID string) error {
	return d.sendToMachine(machineID, &v1pb.ManagerMachineStreamMessage{
		Message: &v1pb.ManagerMachineStreamMessage_DiscoverProviders{
			DiscoverProviders: &v1pb.DiscoverProviders{RequestId: requestID},
		},
	})
}

// SendPongToMachine replies to a machine Ping on its control stream.
func (d *Dispatcher) SendPongToMachine(machineID int) error {
	return d.sendToMachine(machineID, &v1pb.ManagerMachineStreamMessage{
		Message: &v1pb.ManagerMachineStreamMessage_Pong{
			Pong: &v1pb.Pong{},
		},
	})
}

// RegisterPendingDiscover creates a response channel keyed by requestID for an
// in-flight DiscoverProviders round trip. The caller sends the control message
// to the agent (via SendDiscoverProviders), then waits on the returned channel
// for the ProvidersDiscovered reply. CancelPendingDiscover must be called if
// the caller gives up waiting, to avoid leaking the entry.
func (d *Dispatcher) RegisterPendingDiscover(requestID string) chan *v1pb.ProvidersDiscovered {
	return d.pendingDiscovers.register(requestID)
}

// CancelPendingDiscover removes a pending discover entry without delivering a
// result. Safe to call after the reply arrived (it is a no-op in that case
// since the entry was already removed).
func (d *Dispatcher) CancelPendingDiscover(requestID string) {
	d.pendingDiscovers.cancel(requestID)
}

// CompletePendingDiscover delivers a ProvidersDiscovered reply to the waiting
// caller and removes the pending entry. Called from the bidi receive loop when
// the agent replies. Unknown request ids (late replies, already-cancelled
// callers) are dropped silently.
func (d *Dispatcher) CompletePendingDiscover(msg *v1pb.ProvidersDiscovered) {
	if msg == nil {
		return
	}
	d.pendingDiscovers.complete(msg.RequestId, msg)
}

// SendUpgradeRequest pushes a self-upgrade command to a connected machine's
// control stream. The machine's supervisor downloads the new binary from the
// manager, installs it, and restarts; progress flows back as UpgradeProgress
// messages recorded via RecordMachineUpgrade.
func (d *Dispatcher) SendUpgradeRequest(machineID int, req *v1pb.UpgradeRequest) error {
	return d.sendToMachine(machineID, &v1pb.ManagerMachineStreamMessage{
		Message: &v1pb.ManagerMachineStreamMessage_UpgradeRequest{
			UpgradeRequest: req,
		},
	})
}

// RecordMachineUpgrade stores the latest self-upgrade progress for a machine.
func (d *Dispatcher) RecordMachineUpgrade(machineID int, progress *v1pb.UpgradeProgress) {
	if progress == nil {
		return
	}
	d.upgradeMu.Lock()
	d.machineUpgrades[machineID] = progress
	d.upgradeMu.Unlock()
}

// MachineUpgradeStatus returns the recorded self-upgrade progress for a
// machine, or nil when none was reported since its last connect.
func (d *Dispatcher) MachineUpgradeStatus(machineID int) *v1pb.UpgradeProgress {
	d.upgradeMu.Lock()
	defer d.upgradeMu.Unlock()
	return d.machineUpgrades[machineID]
}

// SendDiscoverProviders sends a DiscoverProviders control message to the
// agent's active bidi stream. Returns an error if the agent has no active
// session (the frontend should show "agent offline").
func (d *Dispatcher) SendDiscoverProviders(agentID int, requestID string) error {
	return d.sendToAgent(agentID, &v1pb.ManagerStreamMessage{
		Message: &v1pb.ManagerStreamMessage_DiscoverProviders{
			DiscoverProviders: &v1pb.DiscoverProviders{RequestId: requestID},
		},
	})
}

// SendWorkspaceListRequest asks the agent daemon to list one directory level of
// its workspace. The reply resolves a pending entry registered via
// RegisterPendingWorkspaceList.
func (d *Dispatcher) SendWorkspaceListRequest(agentID int, requestID, dirPath string, includeHidden bool) error {
	return d.sendToAgent(agentID, &v1pb.ManagerStreamMessage{
		Message: &v1pb.ManagerStreamMessage_WorkspaceListRequest{
			WorkspaceListRequest: &v1pb.WorkspaceListRequest{
				RequestId:     requestID,
				DirPath:       dirPath,
				IncludeHidden: includeHidden,
			},
		},
	})
}

// SendWorkspaceReadRequest asks the agent daemon to read one workspace file for
// preview. The reply resolves a pending entry registered via
// RegisterPendingWorkspaceRead.
func (d *Dispatcher) SendWorkspaceReadRequest(agentID int, requestID, path string) error {
	return d.sendToAgent(agentID, &v1pb.ManagerStreamMessage{
		Message: &v1pb.ManagerStreamMessage_WorkspaceReadRequest{
			WorkspaceReadRequest: &v1pb.WorkspaceReadRequest{
				RequestId: requestID,
				Path:      path,
			},
		},
	})
}

// SendMachineWorkspaceScan asks a connected machine to summarize every
// per-agent workspace directory. The reply resolves a pending entry registered
// via RegisterPendingMachineWorkspaceScan.
func (d *Dispatcher) SendMachineWorkspaceScan(machineID int, requestID string) error {
	return d.sendToMachine(machineID, &v1pb.ManagerMachineStreamMessage{
		Message: &v1pb.ManagerMachineStreamMessage_MachineWorkspaceScanRequest{
			MachineWorkspaceScanRequest: &v1pb.MachineWorkspaceScanRequest{RequestId: requestID},
		},
	})
}

// RegisterPendingWorkspaceList creates a response channel for an in-flight
// ListAgentWorkspace round trip over the agent's bidi stream.
func (d *Dispatcher) RegisterPendingWorkspaceList(requestID string) chan *v1pb.WorkspaceListResponse {
	return d.pendingWorkspaceLists.register(requestID)
}

// CancelPendingWorkspaceList removes a pending workspace list entry without
// delivering a result.
func (d *Dispatcher) CancelPendingWorkspaceList(requestID string) {
	d.pendingWorkspaceLists.cancel(requestID)
}

// CompletePendingWorkspaceList delivers a WorkspaceListResponse to the waiting
// ListAgentWorkspace caller. Called from the AgentChannel receive loop.
func (d *Dispatcher) CompletePendingWorkspaceList(msg *v1pb.WorkspaceListResponse) {
	if msg == nil {
		return
	}
	d.pendingWorkspaceLists.complete(msg.RequestId, msg)
}

// RegisterPendingWorkspaceRead creates a response channel for an in-flight
// ReadAgentWorkspaceFile round trip over the agent's bidi stream.
func (d *Dispatcher) RegisterPendingWorkspaceRead(requestID string) chan *v1pb.WorkspaceReadResponse {
	return d.pendingWorkspaceReads.register(requestID)
}

// CancelPendingWorkspaceRead removes a pending workspace read entry without
// delivering a result.
func (d *Dispatcher) CancelPendingWorkspaceRead(requestID string) {
	d.pendingWorkspaceReads.cancel(requestID)
}

// CompletePendingWorkspaceRead delivers a WorkspaceReadResponse to the waiting
// ReadAgentWorkspaceFile caller. Called from the AgentChannel receive loop.
func (d *Dispatcher) CompletePendingWorkspaceRead(msg *v1pb.WorkspaceReadResponse) {
	if msg == nil {
		return
	}
	d.pendingWorkspaceReads.complete(msg.RequestId, msg)
}

// RegisterPendingMachineWorkspaceScan creates a response channel for an
// in-flight ListMachineWorkspaces round trip over the machine control stream.
func (d *Dispatcher) RegisterPendingMachineWorkspaceScan(requestID string) chan *v1pb.MachineWorkspaceScanResponse {
	return d.pendingMachineScans.register(requestID)
}

// CancelPendingMachineWorkspaceScan removes a pending machine scan entry
// without delivering a result.
func (d *Dispatcher) CancelPendingMachineWorkspaceScan(requestID string) {
	d.pendingMachineScans.cancel(requestID)
}

// CompletePendingMachineWorkspaceScan delivers a MachineWorkspaceScanResponse
// to the waiting ListMachineWorkspaces caller. Called from the MachineChannel
// receive loop.
func (d *Dispatcher) CompletePendingMachineWorkspaceScan(msg *v1pb.MachineWorkspaceScanResponse) {
	if msg == nil {
		return
	}
	d.pendingMachineScans.complete(msg.RequestId, msg)
}

// CurrentCommandID returns the command id the agent is currently running in its
// drain session, or "" if the agent has no in-flight session command. It is used
// to link a session's running command to the conversation the agent is working
// on, so the channel activity feed reflects in-progress work. The session
// command is created at BeginSession before the agent has chosen a channel, so
// the link is filled in when the agent reads a channel (commits to working on
// it) — see CommandService.ListConversationMessages.
func (d *Dispatcher) CurrentCommandID(agentID int) string {
	sess, ok := d.registry.getAgent(agentID)
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
	// A stopped agent must not run sessions: it stays idle and processes no
	// session messages until StartAgent re-enables it.
	if !agent.Enabled {
		slog.Info("agent is stopped; staying idle", "agent", agent.ResourceID)
		return &v1pb.BeginSessionResponse{Idle: true}, nil
	}

	// An agent must support an autonomous drain runtime (ACP or the bundled
	// non-ACP pi runtime) to run a session. An agent with neither stays idle —
	// it has no executor to process messages. The agent connection itself is
	// the primary gate; this is the server-side backstop.
	if capability := agent.Info.GetCapability(); capability == nil || (!capability.GetSupportsAcp() && !capability.GetSupportsPi()) {
		slog.Warn("agent is not runtime-capable; staying idle", "agent", agent.ResourceID)
		return &v1pb.BeginSessionResponse{Idle: true}, nil
	}

	cmd, err := d.store.CreateCommand(ctx, &store.CommandMessage{
		AgentID:     agentID,
		MachineID:   agent.MachineID,
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

	sess, ok := d.registry.getAgent(agentID)
	if ok {
		sess.mu.Lock()
		sess.currentCmdID = cmd.ID.String()
		sess.mu.Unlock()
	}

	slog.Info("agent session begun", "commandID", cmd.ID, "agentID", agentID)

	// Resolve the owner display name (empty for legacy agents with no owner) so
	// the agent client can inject it into the init/re-anchor prompt's Ownership &
	// Safety section. Sourced fresh each session so an ownership transfer takes
	// effect on the next drain turn.
	ownerDisplayName := ""
	if agent.OwnerID != 0 {
		if owner, err := d.store.GetUserByID(ctx, agent.OwnerID); err == nil && owner != nil {
			ownerDisplayName = owner.Name
		} else if err != nil {
			slog.Warn("failed to resolve agent owner", "agent", agent.ResourceID, "ownerID", agent.OwnerID, "error", err)
		}
	}

	// Resolve the agent's current team (an agent can belong to at most one
	// team) so the agent client can inject a "Your Team" section into its
	// cold-start prompt.
	var teamCtx *v1pb.TeamContext
	if team, err := d.store.GetAgentTeamByAgentID(ctx, agentID); err != nil {
		slog.Warn("failed to resolve agent team", "agent", agent.ResourceID, "error", err)
	} else if team != nil {
		teamCtx = &v1pb.TeamContext{
			TeamId:     common.FormatAgentTeamName(team.ResourceID),
			TeamName:   team.Title,
			TeamPrompt: team.TeamPrompt,
		}
		for _, m := range team.Members {
			if m.AgentID == agentID {
				if m.Role == store.AgentTeamRoleLeader {
					teamCtx.Role = "leader"
				} else {
					teamCtx.Role = "member"
				}
				teamCtx.Responsibility = m.Responsibility
				break
			}
		}
	}

	return &v1pb.BeginSessionResponse{
		CommandId:        cmd.ID.String(),
		AgentDisplayName: agent.Name,
		OwnerDisplayName: ownerDisplayName,
		Team:             teamCtx,
	}, nil
}

// agentStopped reports whether the agent has been stopped (StopAgent). A
// stopped agent is still connectable but must not process session messages;
// the notification methods skip delivery so it never begins a session.
func (d *Dispatcher) agentStopped(ctx context.Context, agentID int) bool {
	if d.store == nil {
		return false
	}
	agent, err := d.store.GetAgent(ctx, agentID)
	return err != nil || agent == nil || !agent.Enabled
}

// NotifyNewMessages pushes a NewMessagesAvailable hint to a connected agent so
// it knows the conversation has advanced (e.g. another participant posted).
// Phase 1 primarily calls this after assistant replies so multi-agent channels
// can be informed; the action-less agent-autonomy gate arrives in Phase 2.
func (d *Dispatcher) NotifyNewMessages(ctx context.Context, agentID int, conversationID string, version int64) {
	// A stopped agent must not be woken to process messages.
	if d.agentStopped(ctx, agentID) {
		return
	}
	sess, ok := d.registry.getAgent(agentID)
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
func (d *Dispatcher) NotifyWake(ctx context.Context, agentID int) {
	// A stopped agent must not be woken to check for work.
	if d.agentStopped(ctx, agentID) {
		return
	}
	sess, ok := d.registry.getAgent(agentID)
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
func (d *Dispatcher) NotifyThreadMention(ctx context.Context, agentID int, conversationID string, version int64, threadRootMessageID string) {
	// A stopped agent must not be woken to process a thread mention.
	if d.agentStopped(ctx, agentID) {
		return
	}
	sess, ok := d.registry.getAgent(agentID)
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
// in a conversation. It delegates to the activity aggregator.
func (d *Dispatcher) FetchConversationActivity(ctx context.Context, conversationID string) ([]*v1pb.AgentActivity, error) {
	return d.activity.FetchConversationActivity(ctx, conversationID)
}

// ---- Phase 2: Held Draft ----

func (d *Dispatcher) Subscribe(_ context.Context, commandID string) (chan *v1pb.CommandOutput, error) {
	return d.bus.subscribeOutput(commandID), nil
}

func (d *Dispatcher) Unsubscribe(commandID string, ch chan *v1pb.CommandOutput) {
	d.bus.unsubscribeOutput(commandID, ch)
}

func (d *Dispatcher) SubscribeEvents(_ context.Context, commandID string) (chan *v1pb.CommandEvent, error) {
	return d.bus.subscribeEvent(commandID), nil
}

func (d *Dispatcher) UnsubscribeEvents(commandID string, ch chan *v1pb.CommandEvent) {
	d.bus.unsubscribeEvent(commandID, ch)
}

func (d *Dispatcher) broadcast(commandID string, output *v1pb.CommandOutput) {
	d.bus.broadcast(commandID, output)
}

func (d *Dispatcher) broadcastEvent(commandID string, event *v1pb.CommandEvent) {
	d.bus.broadcastEvent(commandID, event)
}

// SendDeleteAgentWorkspace tears down an agent's runner and deletes its
// workspace directory on the machine (used on DeleteAgent). Best-effort: a
// machine that is offline misses the push, and the workspace is not reclaimed
// until a later explicit delete while the machine is connected.
func (d *Dispatcher) SendDeleteAgentWorkspace(machineID int, agentName string) error {
	return d.sendToMachine(machineID, &v1pb.ManagerMachineStreamMessage{
		Message: &v1pb.ManagerMachineStreamMessage_DeleteAgentWorkspace{
			DeleteAgentWorkspace: &v1pb.DeleteAgentWorkspace{AgentName: agentName},
		},
	})
}
