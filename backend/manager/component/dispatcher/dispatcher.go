package dispatcher

import (
	"context"
	"log/slog"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
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

// MachineSendFunc is the raw send function for a machine's MachineChannel
// control stream (manager→machine direction).
type MachineSendFunc func(*v1pb.ManagerMachineStreamMessage) error

// pendingReplies correlates request/response round trips over the bidi streams:
// a unary RPC registers a buffered channel keyed by request_id, the matching
// stream reply is delivered into it, and the unary RPC unblocks. Each pending
// set is typed, so a late or duplicated reply can never resolve the wrong RPC.
type pendingReplies[T proto.Message] struct {
	mu sync.Mutex
	m  map[string]chan T
}

func newPendingReplies[T proto.Message]() *pendingReplies[T] {
	return &pendingReplies[T]{m: make(map[string]chan T)}
}

// register creates a response channel keyed by requestID for an in-flight
// round trip. cancel must be called if the caller gives up waiting, to avoid
// leaking the entry.
func (p *pendingReplies[T]) register(requestID string) chan T {
	ch := make(chan T, 1)
	p.mu.Lock()
	p.m[requestID] = ch
	p.mu.Unlock()
	return ch
}

// cancel removes a pending entry without delivering a result. Safe to call
// after the reply arrived (it is a no-op in that case).
func (p *pendingReplies[T]) cancel(requestID string) {
	p.mu.Lock()
	delete(p.m, requestID)
	p.mu.Unlock()
}

// complete delivers a reply to the waiting caller and removes the pending
// entry. Called from the bidi receive loops when the machine app replies.
// Unknown request ids (late replies, already-cancelled callers) are dropped
// silently.
func (p *pendingReplies[T]) complete(requestID string, msg T) {
	p.mu.Lock()
	ch, ok := p.m[requestID]
	if ok {
		delete(p.m, requestID)
	}
	p.mu.Unlock()
	if ok {
		select {
		case ch <- msg:
		default:
		}
	}
}

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

type Dispatcher struct {
	store         *store.Store
	mu            sync.RWMutex
	sessions      map[int]*AgentSession
	machines      map[int]*MachineSession
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

	// pendingWorkspace* correlate the workspace request/response round trips
	// over the per-agent and machine control bidi streams to their waiting
	// unary RPCs (ListAgentWorkspace / ReadAgentWorkspaceFile /
	// ListMachineWorkspaces).
	pendingWorkspaceLists *pendingReplies[*v1pb.WorkspaceListResponse]
	pendingWorkspaceReads *pendingReplies[*v1pb.WorkspaceReadResponse]
	pendingMachineScans   *pendingReplies[*v1pb.MachineWorkspaceScanResponse]
}

func New(s *store.Store) *Dispatcher {
	ctx, cancel := context.WithCancel(context.Background())
	return &Dispatcher{
		store:                 s,
		sessions:              make(map[int]*AgentSession),
		machines:              make(map[int]*MachineSession),
		watchers:              make(map[string]map[chan *v1pb.CommandOutput]struct{}),
		eventWatchers:         make(map[string]map[chan *v1pb.CommandEvent]struct{}),
		pingInterval:          15 * time.Second,
		pingTimeout:           45 * time.Second,
		grace:                 make(map[int]map[string]context.CancelFunc),
		pendingDiscovers:      make(map[string]chan *v1pb.ProvidersDiscovered),
		pendingWorkspaceLists: newPendingReplies[*v1pb.WorkspaceListResponse](),
		pendingWorkspaceReads: newPendingReplies[*v1pb.WorkspaceReadResponse](),
		pendingMachineScans:   newPendingReplies[*v1pb.MachineWorkspaceScanResponse](),
		lifecycleCtx:          ctx,
		lifecycleCancel:       cancel,
	}
}

func (d *Dispatcher) RegisterAgent(_ context.Context, agentID int, machineID int, agentResourceID string, send SendFunc) *AgentSession {
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
		machineID:       machineID,
		connectedAt:     time.Now(),
		lastPingAt:      time.Now(),
	}
	fn := send
	sess.send.Store(&fn)

	d.sessions[agentID] = sess
	slog.Info("agent registered for command dispatch", "agentID", agentID, "machineID", machineID)

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
	d.teardownAgentSession(sess)
}

// UnregisterAgentIf tears down the agent session only if sess is still the one
// registered for agentID. The AgentChannel handler uses this for its deferred
// cleanup so that, when a reconnect has replaced the session in the map, the
// old stream's teardown does not delete the new (live) session nor arm a grace
// timer against its in-flight command.
func (d *Dispatcher) UnregisterAgentIf(agentID int, sess *AgentSession) {
	d.mu.Lock()
	current, ok := d.sessions[agentID]
	if !ok || current != sess {
		d.mu.Unlock()
		return
	}
	delete(d.sessions, agentID)
	d.mu.Unlock()
	d.teardownAgentSession(sess)
}

func (d *Dispatcher) teardownAgentSession(sess *AgentSession) {
	sess.mu.Lock()
	cmdID := sess.currentCmdID
	sess.mu.Unlock()
	// Invalidate send so any concurrent deliver returns "agent session
	// invalidated" rather than writing to the closed stream.
	sess.send.Store(nil)

	slog.Info("agent unregistered from command dispatch", "agentID", sess.agentID)

	if cmdID != "" {
		d.startGracePeriod(sess.agentID, cmdID)
	}
}

func (d *Dispatcher) IsAgentConnected(agentID int) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	_, ok := d.sessions[agentID]
	return ok
}

// RegisterMachine registers a machine's MachineChannel control stream. A
// machine authenticates once and holds this stream for its lifetime; each of
// its agents opens a separate AgentChannel (registered via RegisterAgent with
// the matching machineID). Returns the session so the stream handler can wire
// up its receive loop.
func (d *Dispatcher) RegisterMachine(machineID int, machineResourceID string, send MachineSendFunc) *MachineSession {
	d.mu.Lock()
	defer d.mu.Unlock()

	if old, ok := d.machines[machineID]; ok {
		slog.Info("replacing existing machine session", "machineID", machineID)
		old.send.Store(nil)
	}

	sess := &MachineSession{
		machineID:         machineID,
		machineResourceID: machineResourceID,
		connectedAt:       time.Now(),
		lastPingAt:        time.Now(),
	}
	fn := send
	sess.send.Store(&fn)

	d.machines[machineID] = sess
	slog.Info("machine registered for control dispatch", "machineID", machineID)
	return sess
}

// UnregisterMachine tears down a machine's control stream AND every agent
// session owned by it. Each owned agent with an in-flight command gets a 60s
// grace period (→ FAILED if the agent does not reconnect). Machine reconnect
// re-registers every agent via RegisterAgent, which cancels each agent's grace
// timer — so no machine-scoped grace tracking is needed.
func (d *Dispatcher) UnregisterMachine(machineID int) {
	d.mu.Lock()
	machine, ok := d.machines[machineID]
	if !ok {
		d.mu.Unlock()
		return
	}
	delete(d.machines, machineID)
	owned := d.detachMachineAgentsLocked(machineID)
	d.mu.Unlock()
	d.teardownMachineSession(machine, owned)
}

// UnregisterMachineIf tears down the machine session only if sess is still the
// one registered for machineID. The MachineChannel handler uses this for its
// deferred cleanup so that, when a reconnect has replaced the session in the
// map, the old stream's teardown does not destroy the new (live) session and
// re-arming grace timers against its agents' in-flight commands.
func (d *Dispatcher) UnregisterMachineIf(machineID int, sess *MachineSession) {
	d.mu.Lock()
	current, ok := d.machines[machineID]
	if !ok || current != sess {
		d.mu.Unlock()
		return
	}
	delete(d.machines, machineID)
	owned := d.detachMachineAgentsLocked(machineID)
	d.mu.Unlock()
	d.teardownMachineSession(current, owned)
}

// detachMachineAgentsLocked removes and returns every AgentSession owned by
// machineID. Caller must hold d.mu.
func (d *Dispatcher) detachMachineAgentsLocked(machineID int) []*AgentSession {
	owned := make([]*AgentSession, 0)
	for _, sess := range d.sessions {
		if sess.machineID == machineID {
			owned = append(owned, sess)
			delete(d.sessions, sess.agentID)
		}
	}
	return owned
}

func (d *Dispatcher) teardownMachineSession(machine *MachineSession, owned []*AgentSession) {
	machine.send.Store(nil)

	for _, sess := range owned {
		sess.mu.Lock()
		cmdID := sess.currentCmdID
		sess.mu.Unlock()
		sess.send.Store(nil)
		if cmdID != "" {
			d.startGracePeriod(sess.agentID, cmdID)
		}
	}
	slog.Info("machine unregistered from control dispatch", "machineID", machine.machineID, "agents", len(owned))
}

func (d *Dispatcher) IsMachineConnected(machineID int) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	_, ok := d.machines[machineID]
	return ok
}

// SendAgentAssignment pushes a new agent assignment to the machine so it opens
// an AgentChannel for that agent. Best-effort: if the machine is offline the
// agent is picked up from the assigned_agents list on the next ConnectMachine.
func (d *Dispatcher) SendAgentAssignment(machineID int, assignment *v1pb.AgentAssignment) error {
	d.mu.RLock()
	sess, ok := d.machines[machineID]
	d.mu.RUnlock()
	if !ok {
		return errors.New("machine is not connected")
	}
	return sess.Send(&v1pb.ManagerMachineStreamMessage{
		Message: &v1pb.ManagerMachineStreamMessage_AgentAssignment{
			AgentAssignment: assignment,
		},
	})
}

// SendAgentConfigUpdate hot-reloads an agent's ACP config on its runner without
// restarting it (picked up at the next BeginSession).
func (d *Dispatcher) SendAgentConfigUpdate(machineID int, agentName string, cfg *v1pb.AgentACPConfig) error {
	d.mu.RLock()
	sess, ok := d.machines[machineID]
	d.mu.RUnlock()
	if !ok {
		return errors.New("machine is not connected")
	}
	return sess.Send(&v1pb.ManagerMachineStreamMessage{
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
	d.mu.RLock()
	sess, ok := d.machines[machineID]
	d.mu.RUnlock()
	if !ok {
		return errors.New("machine is not connected")
	}
	return sess.Send(&v1pb.ManagerMachineStreamMessage{
		Message: &v1pb.ManagerMachineStreamMessage_RemoveAgent{
			RemoveAgent: &v1pb.RemoveAgent{AgentName: agentName},
		},
	})
}

// SendReloadAgentAssignment re-syncs a single agent's full assignment (used
// after a display-name or config change to re-establish a runner).
func (d *Dispatcher) SendReloadAgentAssignment(machineID int, reload *v1pb.ReloadAgentAssignment) error {
	d.mu.RLock()
	sess, ok := d.machines[machineID]
	d.mu.RUnlock()
	if !ok {
		return errors.New("machine is not connected")
	}
	return sess.Send(&v1pb.ManagerMachineStreamMessage{
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
	d.mu.RLock()
	sess, ok := d.machines[machineID]
	d.mu.RUnlock()
	if !ok {
		return errors.New("machine is not connected")
	}
	return sess.Send(&v1pb.ManagerMachineStreamMessage{
		Message: &v1pb.ManagerMachineStreamMessage_DiscoverProviders{
			DiscoverProviders: &v1pb.DiscoverProviders{RequestId: requestID},
		},
	})
}

// HandleMachinePing records a machine heartbeat ping.
func (d *Dispatcher) HandleMachinePing(machineID int, _ *v1pb.Ping) {
	d.mu.RLock()
	sess, ok := d.machines[machineID]
	d.mu.RUnlock()
	if ok {
		sess.mu.Lock()
		sess.lastPingAt = time.Now()
		sess.mu.Unlock()
	}
}

// SendPongToMachine replies to a machine Ping on its control stream.
func (d *Dispatcher) SendPongToMachine(machineID int) error {
	d.mu.RLock()
	sess, ok := d.machines[machineID]
	d.mu.RUnlock()
	if !ok {
		return errors.New("machine is not connected")
	}
	return sess.Send(&v1pb.ManagerMachineStreamMessage{
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

// SendWorkspaceListRequest asks the agent daemon to list one directory level of
// its workspace. The reply resolves a pending entry registered via
// RegisterPendingWorkspaceList.
func (d *Dispatcher) SendWorkspaceListRequest(agentID int, requestID, dirPath string, includeHidden bool) error {
	d.mu.RLock()
	sess, ok := d.sessions[agentID]
	d.mu.RUnlock()
	if !ok {
		return errors.New("agent is not connected")
	}
	return sess.Send(&v1pb.ManagerStreamMessage{
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
	d.mu.RLock()
	sess, ok := d.sessions[agentID]
	d.mu.RUnlock()
	if !ok {
		return errors.New("agent is not connected")
	}
	return sess.Send(&v1pb.ManagerStreamMessage{
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
	d.mu.RLock()
	sess, ok := d.machines[machineID]
	d.mu.RUnlock()
	if !ok {
		return errors.New("machine is not connected")
	}
	return sess.Send(&v1pb.ManagerMachineStreamMessage{
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

	d.mu.RLock()
	sess, ok := d.sessions[agentID]
	d.mu.RUnlock()
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
	return &v1pb.BeginSessionResponse{CommandId: cmd.ID.String(), AgentDisplayName: agent.Name, OwnerDisplayName: ownerDisplayName}, nil
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
		case int32(v1pb.CommandEventType_CONTEXT_COMPACTION_STARTED):
			act.Status = "compacting"
		case int32(v1pb.CommandEventType_CONTEXT_COMPACTION_FINISHED), int32(v1pb.CommandEventType_CONTEXT_USAGE_UPDATE):
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

// SteerCommand injects a follow-up message into the in-flight turn of a
// running command. It is best-effort: executors without mid-turn steering
// support ignore the message.
func (d *Dispatcher) SteerCommand(_ context.Context, agentID int, commandID, text string) error {
	d.mu.RLock()
	sess, ok := d.sessions[agentID]
	d.mu.RUnlock()

	if !ok {
		return errors.New("agent not connected")
	}

	msg := &v1pb.ManagerStreamMessage{
		Message: &v1pb.ManagerStreamMessage_Steer{
			Steer: &v1pb.SteerMessage{
				CommandId: commandID,
				Text:      text,
			},
		},
	}

	if err := sess.deliver(msg); err != nil {
		slog.Error("failed to send steer to agent", "error", err)
		return errors.Wrapf(err, "failed to send steer to agent")
	}

	slog.Info("steer sent to agent", "commandID", commandID, "agentID", agentID)
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

	// TOKEN_USAGE is additionally denormalized into command_token_usage so
	// agent/principal/time aggregates stay cheap. Failure must not break the
	// event stream: the standalone table is derived data, the event row above
	// is the source of truth.
	if event.Type == v1pb.CommandEventType_TOKEN_USAGE {
		if usage := event.GetTokenUsage(); usage != nil {
			if err := d.store.RecordCommandTokenUsage(ctx, &store.CommandTokenUsageMessage{
				CommandID:        cmdID,
				InputTokens:      usage.InputTokens,
				OutputTokens:     usage.OutputTokens,
				CacheReadTokens:  usage.CacheReadTokens,
				CacheWriteTokens: usage.CacheWriteTokens,
				TotalTokens:      usage.TotalTokens,
			}); err != nil {
				slog.Error("failed to record command token usage", "commandID", event.CommandId, "error", err)
			}
		}
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
	machines := make([]*MachineSession, 0, len(d.machines))
	for _, m := range d.machines {
		machines = append(machines, m)
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

	for _, m := range machines {
		m.mu.Lock()
		idle := now.Sub(m.lastPingAt)
		machineID := m.machineID
		m.mu.Unlock()

		if m.send.Load() == nil {
			continue
		}

		if idle > d.pingTimeout {
			slog.Warn("machine ping timeout, unregistering",
				"machineID", machineID,
				"idle", idle,
				"timeout", d.pingTimeout)
			d.UnregisterMachine(machineID)
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
		PrincipalId:   strconv.Itoa(m.PrincipalID),
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
	case v1pb.CommandEventType_CONTEXT_COMPACTION_STARTED, v1pb.CommandEventType_CONTEXT_COMPACTION_FINISHED:
		return protojson.Marshal(event.GetContextCompaction())
	case v1pb.CommandEventType_CONTEXT_USAGE_UPDATE:
		return protojson.Marshal(event.GetContextUsage())
	case v1pb.CommandEventType_TOKEN_USAGE:
		return protojson.Marshal(event.GetTokenUsage())
	default:
		return nil, nil
	}
}
