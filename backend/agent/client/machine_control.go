package client

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/pkg/errors"

	"google.golang.org/protobuf/types/known/timestamppb"

	daemonsrv "github.com/Ranxy/laelia/backend/agent/daemon"
	"github.com/Ranxy/laelia/backend/agent/executor"
	"github.com/Ranxy/laelia/backend/agent/home"
	"github.com/Ranxy/laelia/backend/agent/provider"
	"github.com/Ranxy/laelia/backend/agent/supervisor"
	"github.com/Ranxy/laelia/backend/agent/workspace"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
)

// runControlStream opens the machine-level MachineChannel bidi stream and pumps
// it for the lifetime of one connection. It sends MachineReady on open, pings
// on a ticker, pushes the startup background provider probe result, and
// replies to DiscoverProviders with a fresh host probe. On the receive side it
// drives the agent roster (AgentAssignment / ReloadAgentAssignment spawn or
// re-config a runner, RemoveAgent tears one down, AgentConfigUpdate hot-reloads
// a runner's ACP config) and routes per-agent control interactions
// (AgentControlRequest: cancel/steer/wake/prompt notice) and workspace
// requests to the named agent's runner. It returns when the stream ends or ctx
// is cancelled; the caller (Run) treats a non-nil return as a death signal
// that tears down the whole connection.
func (c *MachineClient) runControlStream(ctx context.Context, _ *daemonsrv.Server) error {
	c.mu.RLock()
	token := c.accessToken
	sessionID := c.sessionID
	c.mu.RUnlock()

	streamClient := v1connect.NewMachineStreamServiceClient(c.streamClient, c.managerURL)
	stream := streamClient.MachineChannel(ctx)
	stream.RequestHeader().Set("Authorization", "Bearer "+token)

	// sendStream serializes sends on the bidi stream (see streamSendMu) so the
	// ping loop, disconnect notice, DiscoverProviders reply, and the runner
	// replies routed through sendOnControlStream do not race.
	sendStream := func(msg *v1pb.MachineStreamMessage) error {
		c.streamSendMu.Lock()
		defer c.streamSendMu.Unlock()
		return stream.Send(msg)
	}
	c.setControlSend(sendStream)
	defer c.clearControlSend()

	if err := sendStream(&v1pb.MachineStreamMessage{
		Message: &v1pb.MachineStreamMessage_MachineReady{
			MachineReady: &v1pb.MachineReady{SessionId: sessionID},
		},
	}); err != nil {
		return err
	}

	pingTicker := time.NewTicker(machinePingInterval)
	defer pingTicker.Stop()

	var pingSeq int64
	errCh := make(chan error, 1)
	doneCh := make(chan struct{})
	defer close(doneCh)

	// Receive pump: drive the agent roster from manager pushes.
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
			case *v1pb.ManagerMachineStreamMessage_AgentAssignment:
				c.spawnOrUpdate(ctx, m.AgentAssignment)

			case *v1pb.ManagerMachineStreamMessage_ReloadAgentAssignment:
				// Full re-sync of one agent: drop any existing runner and spawn
				// fresh with the new assignment.
				if m.ReloadAgentAssignment != nil {
					c.stopRunner(m.ReloadAgentAssignment.GetAgentName())
					c.spawnOrUpdate(ctx, m.ReloadAgentAssignment.GetAssignment())
				}

			case *v1pb.ManagerMachineStreamMessage_RemoveAgent:
				c.stopRunner(m.RemoveAgent.GetAgentName())

			case *v1pb.ManagerMachineStreamMessage_RestartAgent:
				c.coldRestartAgent(m.RestartAgent.GetAgentName())

			case *v1pb.ManagerMachineStreamMessage_DeleteAgentWorkspace:
				// Tear down the runner and permanently delete the agent's workspace
				// on this machine. Runs inline (it is a fast directory remove).
				c.stopRunner(m.DeleteAgentWorkspace.GetAgentName())
				c.deleteAgentWorkspace(m.DeleteAgentWorkspace.GetAgentName())

			case *v1pb.ManagerMachineStreamMessage_AgentConfigUpdate:
				c.hotReloadAgentConfig(m.AgentConfigUpdate)

			case *v1pb.ManagerMachineStreamMessage_DiscoverProviders:
				// Probe the host on its own goroutine: a provider scan can take
				// tens of seconds, and running it inline would block the receive
				// pump, delaying AgentAssignment / RemoveAgent / AgentConfigUpdate
				// for the whole probe window.
				go c.handleDiscoverProviders(ctx, sendStream, m.DiscoverProviders.GetRequestId())

			case *v1pb.ManagerMachineStreamMessage_DiscoverModels:
				// Probe one provider's models with an env overlay off the receive
				// pump, like the full DiscoverProviders path.
				go handleDiscoverModels(ctx, sendStream, m.DiscoverModels)

			case *v1pb.ManagerMachineStreamMessage_MachineWorkspaceScanRequest:
				// Scanning the workspace root can take a while on a big disk;
				// run it off the receive pump.
				go c.handleMachineWorkspaceScan(ctx, sendStream, m.MachineWorkspaceScanRequest)

			case *v1pb.ManagerMachineStreamMessage_AgentControl:
				// Per-agent control interaction (cancel/steer/wake/prompt
				// notice), routed to the named agent's runner.
				c.handleAgentControl(m.AgentControl)

			case *v1pb.ManagerMachineStreamMessage_WorkspaceListRequest:
				// File reads run on their own goroutine: a slow disk must not
				// block the receive pump.
				go c.handleAgentWorkspaceList(m.WorkspaceListRequest)

			case *v1pb.ManagerMachineStreamMessage_WorkspaceReadRequest:
				go c.handleAgentWorkspaceRead(m.WorkspaceReadRequest)

			case *v1pb.ManagerMachineStreamMessage_Pong:
				// pong received, link acknowledged

			case *v1pb.ManagerMachineStreamMessage_UpgradeRequest:
				go c.handleUpgradeRequest(ctx, sendStream, m.UpgradeRequest)

			default:
				slog.Warn("unknown message type from manager on machine control stream")
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			// Announce a graceful disconnect before tearing the stream down.
			_ = sendStream(&v1pb.MachineStreamMessage{
				Message: &v1pb.MachineStreamMessage_DisconnectNotice{
					DisconnectNotice: &v1pb.MachineDisconnectNotice{Reason: "shutdown"},
				},
			})
			return nil
		case <-doneCh:
			return nil
		case err := <-errCh:
			return err
		case discovered := <-c.providerUpdateCh:
			// Startup background probe finished after the control stream opened
			// (or was queued before it opened). Push the fresh list to the manager
			// so machine.info.available_providers is updated without requiring a
			// manual RefreshMachineProviders round-trip.
			if err := sendStream(&v1pb.MachineStreamMessage{
				Message: &v1pb.MachineStreamMessage_ProvidersDiscovered{
					ProvidersDiscovered: &v1pb.ProvidersDiscovered{
						Providers: discoveredToProto(discovered, time.Now()),
					},
				},
			}); err != nil {
				return err
			}
		case <-pingTicker.C:
			pingSeq++
			if err := sendStream(&v1pb.MachineStreamMessage{
				Message: &v1pb.MachineStreamMessage_Ping{
					Ping: &v1pb.Ping{
						Seq:    pingSeq,
						SentAt: time.Now().UnixMilli(),
					},
				},
			}); err != nil {
				return err
			}
		}
	}
}

// handleUpgradeRequest forwards a manager-pushed self-upgrade to the local
// supervisor process and relays its progress back over the control stream as
// UpgradeProgress messages, polling until a terminal stage.
func (c *MachineClient) handleUpgradeRequest(ctx context.Context, send func(*v1pb.MachineStreamMessage) error, req *v1pb.UpgradeRequest) {
	if req == nil {
		return
	}
	slog.Info("upgrade requested by manager", "version", req.Version, "target", req.Target)

	sendProgress := func(stage, errMsg string) {
		if err := send(&v1pb.MachineStreamMessage{
			Message: &v1pb.MachineStreamMessage_UpgradeProgress{
				UpgradeProgress: &v1pb.UpgradeProgress{Version: req.Version, Stage: stage, Error: errMsg},
			},
		}); err != nil {
			slog.Error("failed to send upgrade progress", "error", err)
		}
	}

	st, err := supervisor.TriggerUpgrade(supervisor.UpgradeRequest{
		Version:    req.Version,
		Target:     req.Target,
		Sha256:     req.Sha256,
		ManagerURL: c.managerURL,
	})
	if err != nil {
		slog.Error("failed to trigger local upgrade", "error", err)
		sendProgress("failed", "no local supervisor available: "+err.Error())
		return
	}
	sendProgress(st.Stage, st.Error)

	for !st.Terminal() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
		st, err = supervisor.PollStatus()
		if err != nil {
			slog.Warn("lost contact with supervisor during upgrade; it continues in the background", "error", err)
			// The supervisor keeps going (the restart phases need no polling);
			// stop reporting rather than reporting a spurious failure.
			return
		}
		sendProgress(st.Stage, st.Error)
	}
	slog.Info("upgrade finished", "stage", st.Stage, "error", st.Error)
}

// hotReloadAgentConfig updates one agent runner's ACP config in place; the next
// BeginSession picks it up. The runner is left running.
func (c *MachineClient) hotReloadAgentConfig(update *v1pb.AgentConfigUpdate) {
	if update == nil || update.GetAgentName() == "" {
		return
	}
	agentID := bareAgentID(update.GetAgentName())
	c.runnersMu.Lock()
	r, ok := c.runners[agentID]
	c.runnersMu.Unlock()
	if !ok {
		// The manager pushed a config update for an agent we are not currently
		// hosting (e.g. it was removed, or we have not yet received its
		// assignment). Drop it — the next assigned_agents resync or
		// AgentAssignment will carry the current config.
		slog.Warn("config update for unknown agent runner; ignoring", "agent", update.GetAgentName())
		return
	}
	r.setConfig(r.buildAcpConfig(&v1pb.AgentAssignment{
		AgentName:        update.GetAgentName(),
		AgentDisplayName: r.displayName,
		AcpConfig:        update.GetAcpConfig(),
	}))
	slog.Info("hot-reloaded agent ACP config", "agent", update.GetAgentName())
}

// handleDiscoverProviders re-probes the host and replies with the fresh
// provider list, correlated by the manager's request_id. It runs on its own
// goroutine from the receive pump; `send` is the shared, mutex-guarded stream
// sender so the reply does not race the ping loop's sends.
func (c *MachineClient) handleDiscoverProviders(ctx context.Context, send func(*v1pb.MachineStreamMessage) error, requestID string) {
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	discovered := c.refreshProviders(probeCtx)
	cancel()
	if err := send(&v1pb.MachineStreamMessage{
		Message: &v1pb.MachineStreamMessage_ProvidersDiscovered{
			ProvidersDiscovered: &v1pb.ProvidersDiscovered{
				RequestId: requestID,
				Providers: discoveredToProto(discovered, time.Now()),
			},
		},
	}); err != nil {
		slog.Error("failed to send providers_discovered", "requestID", requestID, "error", err)
	}
}

// handleDiscoverModels probes one provider's models with an env overlay (the
// agent's custom_env, e.g. CODEX_HOME) and replies with ModelsDiscovered. It
// runs off the receive pump; failures are reported in the reply's error field
// rather than killing the stream.
func handleDiscoverModels(ctx context.Context, send func(*v1pb.MachineStreamMessage) error, req *v1pb.DiscoverModels) {
	if req == nil {
		return
	}
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	models, _, _, err := provider.Default().ProbeModelOptions(probeCtx, req.GetProvider(), envOverlayFromMap(req.GetEnv()))
	cancel()

	var errMsg string
	if err != nil {
		errMsg = err.Error()
		slog.Warn("model discovery probe failed", "provider", req.GetProvider(), "requestID", req.GetRequestId(), "error", err)
	}
	out := make([]*v1pb.AgentModelOption, 0, len(models))
	for _, m := range models {
		out = append(out, &v1pb.AgentModelOption{
			Value:       m.Value,
			Name:        m.Name,
			Description: m.Description,
		})
	}
	if err := send(&v1pb.MachineStreamMessage{
		Message: &v1pb.MachineStreamMessage_ModelsDiscovered{
			ModelsDiscovered: &v1pb.ModelsDiscovered{
				RequestId: req.GetRequestId(),
				Provider:  req.GetProvider(),
				Models:    out,
				Error:     errMsg,
			},
		},
	}); err != nil {
		slog.Error("failed to send models_discovered", "requestID", req.GetRequestId(), "error", err)
	}
}

// envOverlayFromMap flattens a KEY=VALUE map into a slice of "KEY=VALUE" entries
// suitable for provider.WithProbeEnv.
func envOverlayFromMap(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	out := make([]string, 0, len(env))
	for key, value := range env {
		out = append(out, key+"="+value)
	}
	return out
}

// handleMachineWorkspaceScan summarizes every per-agent workspace directory
// under <data root>/<machineID>/ and replies. The machine state file
// (<data root>/machine.json) lives directly under the data root, outside the
// scanned root, so it is never reported.
func (c *MachineClient) handleMachineWorkspaceScan(_ context.Context, send func(*v1pb.MachineStreamMessage) error, req *v1pb.MachineWorkspaceScanRequest) {
	if req == nil {
		return
	}
	root := home.Join(c.machineID)
	summaries, err := workspace.Scan(root)
	if err != nil {
		slog.Warn("machine workspace scan failed", "machineID", c.machineID, "error", err)
	}
	protoSummaries := make([]*v1pb.MachineWorkspaceSummary, 0, len(summaries))
	for _, sm := range summaries {
		var lastModified *timestamppb.Timestamp
		if !sm.LastModified.IsZero() {
			lastModified = timestamppb.New(sm.LastModified)
		}
		protoSummaries = append(protoSummaries, &v1pb.MachineWorkspaceSummary{
			DirectoryName:  sm.DirectoryName,
			TotalSizeBytes: sm.TotalSizeBytes,
			LastModified:   lastModified,
			FileCount:      sm.FileCount,
		})
	}
	_ = send(&v1pb.MachineStreamMessage{
		Message: &v1pb.MachineStreamMessage_MachineWorkspaceScanResponse{
			MachineWorkspaceScanResponse: &v1pb.MachineWorkspaceScanResponse{
				RequestId:  req.RequestId,
				Workspaces: protoSummaries,
			},
		},
	})
}

// setControlSend installs the current MachineChannel send function (per
// connection) and clearControlSend drops it; sendOnControlStream is the
// accessor runner-side replies use.
func (c *MachineClient) setControlSend(send func(*v1pb.MachineStreamMessage) error) {
	c.controlSendMu.Lock()
	c.controlSend = send
	c.controlSendMu.Unlock()
}

func (c *MachineClient) clearControlSend() {
	c.setControlSend(nil)
}

// sendOnControlStream sends one message on the machine's current MachineChannel,
// or an error when the control stream is not connected. The underlying send is
// already serialized (streamSendMu), so this only guards the pointer.
func (c *MachineClient) sendOnControlStream(msg *v1pb.MachineStreamMessage) error {
	c.controlSendMu.Lock()
	send := c.controlSend
	c.controlSendMu.Unlock()
	if send == nil {
		return errors.New("machine control stream is not connected")
	}
	return send(msg)
}

// commandStreamFor returns the named agent's command stream, or nil when the
// machine is not hosting the agent (or its runner has not started yet).
func (c *MachineClient) commandStreamFor(agentName string) *commandStream {
	c.runnersMu.Lock()
	r, ok := c.runners[bareAgentID(agentName)]
	c.runnersMu.Unlock()
	if !ok || r == nil {
		return nil
	}
	return r.currentCommandStream()
}

// steerer is the optional in-turn message injection capability a runtime may
// implement (pi does; ACP does not). Steer delivers a notice into the running
// turn; it must be non-blocking and best-effort.
type steerer interface {
	Steer(text string) error
}

// buildSteerNotice renders the content-free inbox notice steered into a running
// turn. The agent pulls the real messages itself via `laelia-machine message
// check` / `thread check`; the notice only says that something arrived (and
// whether it is a thread reply), never the payload.
func buildSteerNotice(nm *v1pb.NewMessagesAvailable) string {
	if nm != nil && nm.ThreadRootMessageId != "" {
		return "[Laelia inbox notice: new reply in a thread you follow. Run `laelia-machine thread check` at a natural breakpoint.]"
	}
	count := 0
	if nm != nil {
		count = len(nm.ConversationIds)
	}
	if count <= 1 {
		return "[Laelia inbox notice: new messages arrived. Run `laelia-machine message check` at a natural breakpoint.]"
	}
	return fmt.Sprintf("[Laelia inbox notice: new messages arrived in %d conversations. Run `laelia-machine message check` at a natural breakpoint.]", count)
}

// handleAgentControl delivers one per-agent control interaction to the named
// agent's runner. Interactions are scoped to the command they name: a queued
// cancel/steer that races a turn change must not act on a newer turn (the
// manager's pending-control dispatcher already filtered commands that are
// terminal on its side, but a turn could have ended in between).
func (c *MachineClient) handleAgentControl(req *v1pb.AgentControlRequest) {
	if req == nil || req.GetAgentName() == "" {
		return
	}
	cs := c.commandStreamFor(req.GetAgentName())
	if cs == nil {
		slog.Warn("agent control for an agent this machine is not hosting; ignoring", "agent", req.GetAgentName())
		return
	}

	switch ctrl := req.Control.(type) {
	case *v1pb.AgentControlRequest_Cancel:
		cancelMsg := ctrl.Cancel
		if cancelMsg == nil {
			return
		}
		if current := cs.currentCommand(); current != cancelMsg.GetCommandId() {
			slog.Info("cancel for a command not in flight; ignoring",
				"agent", req.GetAgentName(), "commandID", cancelMsg.GetCommandId(), "inFlight", current)
			return
		}
		if ex := cs.getCurrentExecutor(); ex != nil {
			slog.Info("cancelling command", "commandID", cancelMsg.GetCommandId())
			ex.Cancel()
		}

	case *v1pb.AgentControlRequest_Steer:
		st := ctrl.Steer
		if st == nil {
			return
		}
		if current := cs.currentCommand(); current != st.GetCommandId() {
			slog.Info("steer for a command not in flight; ignoring",
				"agent", req.GetAgentName(), "commandID", st.GetCommandId())
			return
		}
		slog.Info("received steer", "commandID", st.GetCommandId())
		if ex := cs.getCurrentExecutor(); ex != nil {
			if resolver, ok := ex.(executor.SteerResolver); ok {
				resolver.Steer(st.GetText())
			}
		}

	case *v1pb.AgentControlRequest_Wake:
		// Best-effort wake; the durable cursor recovers anything missed.
		cs.wake()
		// Same-turn steering: when the in-flight runtime supports it (pi),
		// push a content-free notice into the running turn so the agent
		// reacts now instead of waiting for the turn to end. Any failure
		// (non-pi runtime, turn about to end, queue full) falls back to the
		// wake above.
		if ex := cs.getCurrentExecutor(); ex != nil {
			if s, ok := ex.(steerer); ok {
				if err := s.Steer(buildSteerNotice(ctrl.Wake)); err != nil {
					slog.Debug("same-turn steer failed; post-turn wake is the fallback", "error", err)
				}
			}
		}

	case *v1pb.AgentControlRequest_PromptNotice:
		c.deliverPromptNotice(cs, ctrl.PromptNotice)

	default:
		slog.Warn("unknown agent control interaction; ignoring", "agent", req.GetAgentName())
	}
}

// deliverPromptNotice injects a prompt release notice into the in-flight turn
// when the runtime is steerable (and acks immediately — only the pi runtime
// reports delivery success, so only it may mark the version as seen), or
// queues it for the next drain turn and wakes the loop.
func (*MachineClient) deliverPromptNotice(cs *commandStream, notice *v1pb.PromptReleaseNotice) {
	if cs == nil || notice == nil {
		return
	}
	if ex := cs.getCurrentExecutor(); ex != nil {
		if s, ok := ex.(steerer); ok {
			if err := s.Steer(notice.GetMessage()); err == nil {
				cs.recordSteeredPromptVersion(notice.GetPromptVersion())
				cs.ackPromptNotice(notice)
				return
			}
		}
	}
	// Fallback: queue for the next drain turn and wake so it is picked up
	// promptly.
	cs.setPendingPromptNotice(notice)
	cs.wake()
}

// handleAgentWorkspaceList lists one directory level of the named agent's
// workspace and replies on the machine control stream. The manager gates this
// by owner/admin permission; the workspace package enforces the
// never-visible/secret policy.
func (c *MachineClient) handleAgentWorkspaceList(req *v1pb.WorkspaceListRequest) {
	if req == nil || req.GetAgentName() == "" {
		return
	}
	entries, err := workspace.List(executor.AgentWorkingDir(c.machineID, bareAgentID(req.GetAgentName())), req.GetDirPath(), req.GetIncludeHidden())
	if err != nil {
		slog.Warn("workspace list failed", "agent", req.GetAgentName(), "dirPath", req.GetDirPath(), "error", err)
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
	if err := c.sendOnControlStream(&v1pb.MachineStreamMessage{
		Message: &v1pb.MachineStreamMessage_WorkspaceListResponse{
			WorkspaceListResponse: &v1pb.WorkspaceListResponse{
				RequestId: req.GetRequestId(),
				Entries:   protoEntries,
			},
		},
	}); err != nil {
		slog.Warn("failed to send workspace list response", "agent", req.GetAgentName(), "requestID", req.GetRequestId(), "error", err)
	}
}

// handleAgentWorkspaceRead previews one workspace file and replies on the
// machine control stream. Refusals (sensitive file, too large, directory) come
// back in the response's error field; OS failures are logged and returned as
// errors too.
func (c *MachineClient) handleAgentWorkspaceRead(req *v1pb.WorkspaceReadRequest) {
	if req == nil || req.GetAgentName() == "" {
		return
	}
	result, err := workspace.Read(executor.AgentWorkingDir(c.machineID, bareAgentID(req.GetAgentName())), req.GetPath())
	if err != nil {
		slog.Warn("workspace read failed", "agent", req.GetAgentName(), "path", req.GetPath(), "error", err)
		result.Error = err.Error()
	}
	if err := c.sendOnControlStream(&v1pb.MachineStreamMessage{
		Message: &v1pb.MachineStreamMessage_WorkspaceReadResponse{
			WorkspaceReadResponse: &v1pb.WorkspaceReadResponse{
				RequestId: req.GetRequestId(),
				Content:   result.Content,
				Binary:    result.Binary,
				Size:      result.Size,
				MimeType:  result.MimeType,
				Encoding:  result.Encoding,
				Error:     result.Error,
			},
		},
	}); err != nil {
		slog.Warn("failed to send workspace read response", "agent", req.GetAgentName(), "requestID", req.GetRequestId(), "error", err)
	}
}
