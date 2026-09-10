package dispatcher

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pkg/errors"

	"github.com/Ranxy/laelia/backend/common"
	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/manager/component/machinebuild"
	"github.com/Ranxy/laelia/backend/manager/store"
)

const (
	graceDBTimeout = 10 * time.Second
	watcherBufSize = 256
)

// MachineSendFunc is the raw send function for a machine's MachineChannel
// control stream (manager→machine direction).
type MachineSendFunc func(*v1pb.ManagerMachineStreamMessage) error

// MachineSession is the manager-side handle on a connected machine's
// MachineChannel control stream. The machine app authenticates once and holds
// this stream for its lifetime; command data flows over the unary
// UploadCommandData RPC and agent control interactions are routed through this
// stream, so no per-agent session is registered.
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
	// background goroutines (upload terminal cleanup). Stop cancels it and
	// waits on wg, so shutdown joins every dispatcher-spawned goroutine instead
	// of leaving the ping ticker running for the process lifetime.
	lifecycleCtx    context.Context
	lifecycleCancel context.CancelFunc
	wg              sync.WaitGroup
	// wgMu serializes wg.Add against wg.Wait. Stop may call Wait while a
	// terminal broadcast is concurrently arming a watcher-close goroutine;
	// guarding both operations avoids the WaitGroup "Add concurrent with
	// Wait" misuse.
	wgMu sync.Mutex

	// tracker records each agent's current in-flight drain command id. The
	// per-agent session registry is retired; this map is what survives of it —
	// it powers the conversation activity feed's "working on" link. Set at
	// BeginSession (mint) and cleared when the terminal result is acked.
	tracker commandTracker

	// pendingDiscovers correlates DiscoverProviders request/response round trips
	// over the machine control stream. Used by the unary RefreshAgentProviders RPC
	// to do a request/response round trip over the machine control stream.
	pendingDiscovers *pendingReplies[*v1pb.ProvidersDiscovered]
	// pendingModels correlates DiscoverModels request/response round trips over
	// the machine control stream to their waiting unary RefreshAgentModels calls.
	pendingModels *pendingReplies[*v1pb.ModelsDiscovered]

	// pendingWorkspace* correlate the workspace request/response round trips
	// over the machine control stream to their waiting
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
		pendingDiscovers:      newPendingReplies[*v1pb.ProvidersDiscovered](),
		pendingModels:         newPendingReplies[*v1pb.ModelsDiscovered](),
		pendingWorkspaceLists: newPendingReplies[*v1pb.WorkspaceListResponse](),
		pendingWorkspaceReads: newPendingReplies[*v1pb.WorkspaceReadResponse](),
		pendingMachineScans:   newPendingReplies[*v1pb.MachineWorkspaceScanResponse](),
		machineUpgrades:       make(map[int]*v1pb.UpgradeProgress),
		lifecycleCtx:          ctx,
		lifecycleCancel:       cancel,
	}
	d.activity = &activityAggregator{store: s, dispatcher: d}
	return d
}

// sendToMachine is the single machine-session send path: look up the connected
// machine session and deliver a control message, returning an error when the
// machine is offline.
func (d *Dispatcher) sendToMachine(machineID int, msg *v1pb.ManagerMachineStreamMessage) error {
	return d.registry.sendToMachine(machineID, msg)
}

// SendPromptReleaseNotice pushes a system-prompt release notice to an agent's
// machine control stream so it can inject the change into the current or next
// turn. Best-effort: if the machine is offline the notice is recovered on the
// next BeginSession via the prompt_version comparison.
func (d *Dispatcher) SendPromptReleaseNotice(agentID int, notice *v1pb.PromptReleaseNotice) error {
	if notice == nil {
		return nil
	}
	agent, err := d.resolveAgent(agentID)
	if err != nil {
		return err
	}
	return d.sendAgentControl(agent, &v1pb.AgentControlRequest{
		Control: &v1pb.AgentControlRequest_PromptNotice{PromptNotice: notice},
	})
}

// resolveAgent loads one live (non-deleted, machine-bound) agent for control
// routing. The per-agent session registry is retired, so the store is the
// source of the agent→machine binding.
func (d *Dispatcher) resolveAgent(agentID int) (*store.AgentMessage, error) {
	if d.store == nil {
		return nil, errors.New("agent is not connected")
	}
	agent, err := d.store.GetAgent(d.lifecycleCtx, agentID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to load agent")
	}
	if agent == nil || agent.Deleted {
		return nil, errors.Errorf("agent %d not found", agentID)
	}
	if agent.MachineID == 0 {
		return nil, errors.New("agent is not bound to a machine")
	}
	return agent, nil
}

// HandlePromptReleaseNoticeAck records that an agent saw a prompt release
// notice, so the manager stops re-pushing it. A notice with a prompt_version
// (a real persona/team/owner change) also updates the confirmed version; a
// stale-machine notice (empty prompt_version) only clears the pending one so
// the agent is not told to upgrade on every turn, while the staleness signal
// keeps coming from the BeginSession prompt_version comparison.
func (d *Dispatcher) HandlePromptReleaseNoticeAck(ctx context.Context, agentID int, ack *v1pb.PromptReleaseNoticeAck) error {
	if ack == nil {
		return nil
	}
	if ack.GetPromptVersion() == "" {
		return d.store.ClearPendingPromptNotice(ctx, agentID)
	}
	return d.store.UpdateAgentPromptVersion(ctx, agentID, ack.GetPromptVersion())
}

// PushPromptReleaseNotice computes the agent's current composite prompt version
// and pushes a release notice to it so a running agent perceives the change
// immediately (steer) or on the next turn. Best-effort: an offline agent is
// recovered by the BeginSession prompt_version comparison.
func (d *Dispatcher) PushPromptReleaseNotice(ctx context.Context, agentID int) error {
	agent, err := d.store.GetAgent(ctx, agentID)
	if err != nil || agent == nil {
		return err
	}
	ownerDisplayName := ""
	if agent.OwnerID != 0 {
		if owner, err := d.store.GetUserByID(ctx, agent.OwnerID); err == nil && owner != nil {
			ownerDisplayName = owner.Name
		}
	}
	var teamCtx *v1pb.TeamContext
	if team, err := d.store.GetAgentTeamByAgentID(ctx, agentID); err == nil && team != nil {
		teamCtx = &v1pb.TeamContext{TeamPrompt: team.TeamPrompt}
	}
	promptVersion := buildPromptVersion(ownerDisplayName, teamCtx, agent)
	notice := &v1pb.PromptReleaseNotice{
		NoticeKey:     "prompt-" + promptVersion,
		Message:       "Your system prompt has been updated. Re-read the relevant sections before continuing.",
		PromptVersion: promptVersion,
	}
	if err := d.SendPromptReleaseNotice(agentID, notice); err != nil {
		// Agent offline: persist so the next BeginSession re-sends it.
		slog.Info("prompt release notice push failed; persisting for next BeginSession", "agentID", agentID, "error", err)
		return d.persistPendingPromptNotice(ctx, agentID, notice)
	}
	return nil
}

// persistPendingPromptNotice stores a prompt release notice in the agent info
// so it is re-sent on the next BeginSession until acked.
func (d *Dispatcher) persistPendingPromptNotice(ctx context.Context, agentID int, notice *v1pb.PromptReleaseNotice) error {
	if notice == nil {
		return nil
	}
	return d.store.SetPendingPromptNotice(ctx, agentID, &storepb.PendingPromptNotice{
		NoticeKey:     notice.GetNoticeKey(),
		Message:       notice.GetMessage(),
		PromptVersion: notice.GetPromptVersion(),
	})
}

// PushPromptReleaseNoticeToMachine pushes a prompt release notice to every
// enabled agent bound to a machine. Used when a machine reports a static prompt
// bundle version that differs from the manager's expected version, so running
// agents on a stale machine are told to upgrade.
func (d *Dispatcher) PushPromptReleaseNoticeToMachine(ctx context.Context, machineID int, notice *v1pb.PromptReleaseNotice) error {
	if notice == nil {
		return nil
	}
	agents, err := d.store.ListAgents(ctx, &store.FindAgentMessage{MachineID: &machineID})
	if err != nil {
		return err
	}
	for _, agent := range agents {
		if agent == nil || !agent.Enabled {
			continue
		}
		if err := d.SendPromptReleaseNotice(agent.ID, notice); err != nil {
			slog.Warn("best-effort prompt release notice push skipped for agent; persisting", "agentID", agent.ID, "error", err)
			if perr := d.persistPendingPromptNotice(ctx, agent.ID, notice); perr != nil {
				slog.Warn("failed to persist pending prompt notice", "agentID", agent.ID, "error", perr)
			}
		}
	}
	return nil
}

// SendAgentAssignment pushes a new agent assignment to the machine so it opens
// a runner for that agent. Best-effort: if the machine is offline the
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

// SendRestartAgent asks the machine to force a cold restart of one agent:
// clear its persisted LLM session state and restart its long-lived runtime so
// the next turn starts from a fresh cold start.
func (d *Dispatcher) SendRestartAgent(machineID int, agentName string) error {
	return d.sendToMachine(machineID, &v1pb.ManagerMachineStreamMessage{
		Message: &v1pb.ManagerMachineStreamMessage_RestartAgent{
			RestartAgent: &v1pb.RestartAgent{AgentName: agentName},
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
// caller and removes the pending entry. Called from the MachineChannel
// receive loop when the agent replies. Unknown request ids (late replies,
// already-cancelled callers) are dropped silently.
func (d *Dispatcher) CompletePendingDiscover(msg *v1pb.ProvidersDiscovered) {
	if msg == nil {
		return
	}
	d.pendingDiscovers.complete(msg.RequestId, msg)
}

// SendDiscoverModelsToMachine asks a connected machine to probe one provider's
// models with an env overlay (the agent's custom_env) and reply with
// ModelsDiscovered. The reply resolves a pending models entry registered via
// RegisterPendingModels.
func (d *Dispatcher) SendDiscoverModelsToMachine(machineID int, providerID string, env map[string]string, requestID string) error {
	return d.sendToMachine(machineID, &v1pb.ManagerMachineStreamMessage{
		Message: &v1pb.ManagerMachineStreamMessage_DiscoverModels{
			DiscoverModels: &v1pb.DiscoverModels{
				RequestId: requestID,
				Provider:  providerID,
				Env:       env,
			},
		},
	})
}

// RegisterPendingModels creates a response channel keyed by requestID for an
// in-flight DiscoverModels round trip. CancelPendingModels must be called if
// the caller gives up waiting, to avoid leaking the entry.
func (d *Dispatcher) RegisterPendingModels(requestID string) chan *v1pb.ModelsDiscovered {
	return d.pendingModels.register(requestID)
}

// CancelPendingModels removes a pending models entry without delivering a
// result. Safe to call after the reply arrived (it is a no-op in that case).
func (d *Dispatcher) CancelPendingModels(requestID string) {
	d.pendingModels.cancel(requestID)
}

// CompletePendingModels delivers a ModelsDiscovered reply to the waiting caller
// and removes the pending entry. Called from the MachineChannel receive loop.
// Unknown request ids (late replies, already-cancelled callers) are dropped.
func (d *Dispatcher) CompletePendingModels(msg *v1pb.ModelsDiscovered) {
	if msg == nil {
		return
	}
	d.pendingModels.complete(msg.RequestId, msg)
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

// SendDiscoverProviders asks the agent's machine to re-probe its host for
// installed LLM agent providers. One machine-scoped catalog serves every
// hosted agent, so the probe travels on the machine control stream. Returns an
// error when the machine is not connected (the frontend should show "agent
// offline").
func (d *Dispatcher) SendDiscoverProviders(agentID int, requestID string) error {
	if d.store == nil {
		return errors.New("agent is not connected")
	}
	agent, err := d.store.GetAgent(context.Background(), agentID)
	if err != nil || agent == nil {
		return errors.Wrap(err, "failed to load agent for provider discovery")
	}
	return d.SendDiscoverProvidersToMachine(agent.MachineID, requestID)
}

// SendWorkspaceListRequest asks the agent's machine to list one directory level
// of the agent's workspace. The reply resolves a pending entry registered via
// RegisterPendingWorkspaceList.
func (d *Dispatcher) SendWorkspaceListRequest(agentID int, requestID, dirPath string, includeHidden bool) error {
	agent, err := d.resolveAgent(agentID)
	if err != nil {
		return err
	}
	return d.sendWorkspaceListRequestTo(agent, requestID, dirPath, includeHidden)
}

// sendWorkspaceListRequestTo routes one workspace listing request to the
// agent's machine. Split from SendWorkspaceListRequest so the routing shape
// (agent_name on the machine stream) is unit-testable without a store.
func (d *Dispatcher) sendWorkspaceListRequestTo(agent *store.AgentMessage, requestID, dirPath string, includeHidden bool) error {
	return d.sendToMachine(agent.MachineID, &v1pb.ManagerMachineStreamMessage{
		Message: &v1pb.ManagerMachineStreamMessage_WorkspaceListRequest{
			WorkspaceListRequest: &v1pb.WorkspaceListRequest{
				RequestId:     requestID,
				DirPath:       dirPath,
				IncludeHidden: includeHidden,
				AgentName:     common.FormatAgentUID(agent.ResourceID),
			},
		},
	})
}

// SendWorkspaceReadRequest asks the agent's machine to read one workspace file
// for preview. The reply resolves a pending entry registered via
// RegisterPendingWorkspaceRead.
func (d *Dispatcher) SendWorkspaceReadRequest(agentID int, requestID, path string) error {
	agent, err := d.resolveAgent(agentID)
	if err != nil {
		return err
	}
	return d.sendWorkspaceReadRequestTo(agent, requestID, path)
}

// sendWorkspaceReadRequestTo routes one workspace file read to the agent's
// machine. Split from SendWorkspaceReadRequest for the same reason as its list
// counterpart.
func (d *Dispatcher) sendWorkspaceReadRequestTo(agent *store.AgentMessage, requestID, path string) error {
	return d.sendToMachine(agent.MachineID, &v1pb.ManagerMachineStreamMessage{
		Message: &v1pb.ManagerMachineStreamMessage_WorkspaceReadRequest{
			WorkspaceReadRequest: &v1pb.WorkspaceReadRequest{
				RequestId: requestID,
				Path:      path,
				AgentName: common.FormatAgentUID(agent.ResourceID),
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
// ListAgentWorkspace round trip over the machine control stream.
func (d *Dispatcher) RegisterPendingWorkspaceList(requestID string) chan *v1pb.WorkspaceListResponse {
	return d.pendingWorkspaceLists.register(requestID)
}

// CancelPendingWorkspaceList removes a pending workspace list entry without
// delivering a result.
func (d *Dispatcher) CancelPendingWorkspaceList(requestID string) {
	d.pendingWorkspaceLists.cancel(requestID)
}

// CompletePendingWorkspaceList delivers a WorkspaceListResponse to the waiting
// ListAgentWorkspace caller. Called from the MachineChannel receive loop.
func (d *Dispatcher) CompletePendingWorkspaceList(msg *v1pb.WorkspaceListResponse) {
	if msg == nil {
		return
	}
	d.pendingWorkspaceLists.complete(msg.RequestId, msg)
}

// RegisterPendingWorkspaceRead creates a response channel for an in-flight
// ReadAgentWorkspaceFile round trip over the machine control stream.
func (d *Dispatcher) RegisterPendingWorkspaceRead(requestID string) chan *v1pb.WorkspaceReadResponse {
	return d.pendingWorkspaceReads.register(requestID)
}

// CancelPendingWorkspaceRead removes a pending workspace read entry without
// delivering a result.
func (d *Dispatcher) CancelPendingWorkspaceRead(requestID string) {
	d.pendingWorkspaceReads.cancel(requestID)
}

// CompletePendingWorkspaceRead delivers a WorkspaceReadResponse to the waiting
// ReadAgentWorkspaceFile caller. Called from the MachineChannel receive loop.
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
	return d.tracker.get(agentID)
}

// HandleBeginSession serves an agent's request to start a new autonomous
// processing session — the drain loop's pull of its next unit of work. If no
// conversation has room_version beyond the agent's durable cursor (and no
// reminder is due) the reply is idle=true and the agent stays idle; otherwise
// a RUNNING command is minted (the session's execution/event anchor, linked to
// a conversation later via AckProcessedVersion) and its id is returned.
//
// A leftover RUNNING command is deliberately NOT resumed, reaped, or rejected
// here (design §3.6 rule 5): the per-agent outbox barrier guarantees the
// previous turn's records — including its terminal — are uploaded and acked
// before this pull arrives, so a RUNNING row at this point belongs to another
// machine (cross-machine reassignment) or to a machine that has not finished
// uploading its terminal. Minting proceeds normally; the stale-command reaper
// closes a truly lost command after the machine-loss grace and the
// late-result regrade rules keep both paths consistent.
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

	now := time.Now()
	if err := d.store.UpdateCommandStatus(ctx, cmd.ID, int32(v1pb.CommandStatus_RUNNING), &now, nil, nil, nil, ""); err != nil {
		slog.Error("failed to mark session command RUNNING", "commandID", cmd.ID, "error", err)
	}

	d.tracker.set(agentID, cmd.ID.String())

	slog.Info("agent session begun", "commandID", cmd.ID, "agentID", agentID)

	return d.sessionResponse(ctx, agent, cmd.ID.String())
}

// sessionResponse builds the BeginSessionResponse shared by the fresh-session
// and resume paths: agent identity, owner, team, and prompt-version context.
func (d *Dispatcher) sessionResponse(ctx context.Context, agent *store.AgentMessage, commandID string) (*v1pb.BeginSessionResponse, error) {
	agentID := agent.ID
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
		CommandId:           commandID,
		AgentDisplayName:    agent.Name,
		OwnerDisplayName:    ownerDisplayName,
		Team:                teamCtx,
		PromptVersion:       buildPromptVersion(ownerDisplayName, teamCtx, agent),
		PromptReleaseNotice: pendingPromptNoticeToV1(agent.Info.GetPendingPromptNotice()),
	}, nil
}

// pendingPromptNoticeToV1 converts a stored pending prompt notice to the v1
// wire form, or nil when there is none.
func pendingPromptNoticeToV1(p *storepb.PendingPromptNotice) *v1pb.PromptReleaseNotice {
	if p == nil {
		return nil
	}
	return &v1pb.PromptReleaseNotice{
		NoticeKey:     p.GetNoticeKey(),
		Message:       p.GetMessage(),
		PromptVersion: p.GetPromptVersion(),
	}
}

// buildPromptVersion derives the composite prompt version the manager expects
// for an agent: "<static_expected>.<dynamic_hash>". The static part is the
// machine binary's embedded prompt bundle version (empty in dev builds that do
// not embed machines); the dynamic part is a hash of the agent's
// persona/team/owner. The agent client compares this against its locally
// confirmed version to decide whether to re-anchor / cold-start / notify.
func buildPromptVersion(ownerDisplayName string, team *v1pb.TeamContext, agent *store.AgentMessage) string {
	static := machinebuild.LatestPromptBundleVersion()
	h := sha256.New()
	persona := ""
	if agent != nil && agent.Info != nil {
		if acp := agent.Info.GetAcpConfig(); acp != nil {
			persona = acp.GetPersonaPrompt()
		}
	}
	teamPrompt := ""
	if team != nil {
		teamPrompt = team.TeamPrompt
	}
	_, _ = h.Write([]byte(persona + "\x00" + teamPrompt + "\x00" + ownerDisplayName))
	dynamic := hex.EncodeToString(h.Sum(nil))[:16]
	return static + "." + dynamic
}

// NotifyNewMessages pushes a NewMessagesAvailable hint to an agent whose
// machine is connected, so it knows the conversation has advanced (e.g.
// another participant posted). Best-effort: a dropped wake is recovered by the
// next BeginSession's cursor comparison.
func (d *Dispatcher) NotifyNewMessages(ctx context.Context, agentID int, conversationID string, version int64) {
	d.notifyAgent(ctx, agentID, &v1pb.AgentControlRequest{
		Control: &v1pb.AgentControlRequest_Wake{
			Wake: &v1pb.NewMessagesAvailable{
				ConversationIds: []string{conversationID},
				Versions:        []int64{version},
			},
		},
	})
}

// notifyAgent pushes one per-agent control interaction through the agent's
// machine control stream. Silent skips for anything that cannot receive it: a
// stopped/deleted agent must not be woken, and an offline machine's wake is
// recovered by the next BeginSession's cursor comparison (a notice by the
// prompt-version comparison).
func (d *Dispatcher) notifyAgent(ctx context.Context, agentID int, req *v1pb.AgentControlRequest) {
	if d.store == nil {
		return
	}
	agent, err := d.store.GetAgent(ctx, agentID)
	if err != nil || agent == nil || agent.Deleted || agent.MachineID == 0 || !agent.Enabled {
		return
	}
	if !d.IsMachineConnected(agent.MachineID) {
		return
	}
	if err := d.sendAgentControl(agent, req); err != nil {
		slog.Warn("failed to deliver agent control push", "agentID", agentID, "error", err)
	}
}

// sendAgentControl routes one agent control interaction to the agent's
// machine control stream, filling in the agent name the machine-side router
// dispatches on. The machine must be connected; the store resolves the
// agent→machine binding (the per-agent session registry is retired).
func (d *Dispatcher) sendAgentControl(agent *store.AgentMessage, req *v1pb.AgentControlRequest) error {
	req.AgentName = common.FormatAgentUID(agent.ResourceID)
	return d.sendToMachine(agent.MachineID, &v1pb.ManagerMachineStreamMessage{
		Message: &v1pb.ManagerMachineStreamMessage_AgentControl{AgentControl: req},
	})
}

// NotifyWake sends an empty NewMessagesAvailable to a connected agent as a
// best-effort "check for work" tick. The agent's drain loop responds by calling
// BeginSession, which authoritatively checks the per-channel cursors; the wake
// itself carries no payload. Used (via NotifyNewMessages) when any message
// lands in a conversation the agent is a member of.
func (d *Dispatcher) NotifyWake(ctx context.Context, agentID int) {
	d.notifyAgent(ctx, agentID, &v1pb.AgentControlRequest{
		Control: &v1pb.AgentControlRequest_Wake{Wake: &v1pb.NewMessagesAvailable{}},
	})
}

// NotifyThreadMention pushes a NewMessagesAvailable hint to a connected agent
// that is subscribed to a thread, carrying the thread root id so the agent can
// go straight to thread check/read. Best-effort like NotifyNewMessages: the
// agent's durable cursor (advanced via ListThreadUpdates + AckProcessedVersion)
// is the source of truth, so a missed wake is recovered on reconnect.
func (d *Dispatcher) NotifyThreadMention(ctx context.Context, agentID int, conversationID string, version int64, threadRootMessageID string) {
	d.notifyAgent(ctx, agentID, &v1pb.AgentControlRequest{
		Control: &v1pb.AgentControlRequest_Wake{
			Wake: &v1pb.NewMessagesAvailable{
				ConversationIds:     []string{conversationID},
				Versions:            []int64{version},
				ThreadRootMessageId: threadRootMessageID,
			},
		},
	})
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
