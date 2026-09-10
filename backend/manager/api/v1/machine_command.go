package v1

import (
	"context"
	"io"
	"log/slog"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/laelia/backend/manager/component/dispatcher"
	"github.com/Ranxy/laelia/backend/manager/store"
)

// MachineStreamService implements MachineStreamService: the machine-level
// control and data plane. A machine authenticates once (machine access token,
// resolved by the auth interceptor into MachineContextKey). Over MachineChannel
// the manager pushes agent-roster changes (AgentAssignment / RemoveAgent /
// AgentConfigUpdate / ReloadAgentAssignment), provider-discovery and workspace
// requests, and per-agent control interactions; the machine reports readiness,
// pings, discovery results, and graceful disconnect. Command data flows over
// the unary UploadCommandData RPC and the drain loop pulls work through the
// unary BeginSession RPC — the per-agent bidi stream is retired.
type MachineStreamService struct {
	v1connect.UnimplementedMachineStreamServiceHandler
	store      *store.Store
	dispatcher *dispatcher.Dispatcher
}

func NewMachineStreamService(s *store.Store, d *dispatcher.Dispatcher) *MachineStreamService {
	return &MachineStreamService{store: s, dispatcher: d}
}

// UploadCommandData is the machine→manager command data plane: progress /
// events / results no longer travel the per-agent bidi stream but arrive in
// idempotent unary batches from the machine's uploader. The machine must be
// ONLINE (same gate as MachineChannel: a force-disconnected machine must
// re-ConnectMachine first); per-entry ownership is checked against the
// command's machine binding inside the dispatcher's store transaction.
func (s *MachineStreamService) UploadCommandData(
	ctx context.Context,
	req *connect.Request[v1pb.UploadCommandDataRequest],
) (*connect.Response[v1pb.UploadCommandDataResponse], error) {
	machine, ok := GetMachineFromContext(ctx)
	if !ok || machine == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, nil)
	}
	if machine.Status == nil || machine.Status.GetState() != storepb.MachineStatus_ONLINE {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.Errorf("machine %s is not online", machine.ResourceID))
	}

	resp, err := s.dispatcher.ApplyCommandUpload(ctx, machine.ID, req.Msg.GetEntries())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to apply command upload batch"))
	}
	return connect.NewResponse(resp), nil
}

// BeginSession is the agent drain loop's unary pull of its next unit of work
// (retired AgentChannel.BeginSession). agent_name binds the request to an
// agent the authenticated machine hosts; the reply carries the command to run
// or idle=true.
func (s *MachineStreamService) BeginSession(
	ctx context.Context,
	req *connect.Request[v1pb.BeginSessionRequest],
) (*connect.Response[v1pb.BeginSessionResponse], error) {
	machine, ok := GetMachineFromContext(ctx)
	if !ok || machine == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, nil)
	}
	if machine.Status == nil || machine.Status.GetState() != storepb.MachineStatus_ONLINE {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.Errorf("machine %s is not online", machine.ResourceID))
	}
	agentID, err := s.dispatcher.ResolveAgentByName(machine.ID, req.Msg.GetAgentName())
	if err != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, err)
	}
	resp, err := s.dispatcher.HandleBeginSession(ctx, agentID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to handle begin session"))
	}
	return connect.NewResponse(resp), nil
}

func (s *MachineStreamService) MachineChannel(
	ctx context.Context,
	stream *connect.BidiStream[v1pb.MachineStreamMessage, v1pb.ManagerMachineStreamMessage],
) error {
	machine, ok := GetMachineFromContext(ctx)
	if !ok || machine == nil {
		return connect.NewError(connect.CodeUnauthenticated, nil)
	}
	// Reject control streams for machines that are not ONLINE (e.g. KICKED by
	// ForceDisconnectMachine or OFFLINE). A machine may only (re)open its
	// control stream after a successful ConnectMachine, which flips state to
	// ONLINE; this prevents a non-cooperative machine from re-opening the
	// stream with a still-valid access token to bypass a force-disconnect
	// without re-connecting.
	if machine.Status == nil || machine.Status.GetState() != storepb.MachineStatus_ONLINE {
		return connect.NewError(connect.CodePermissionDenied, errors.Errorf("machine %s is not online", machine.ResourceID))
	}

	sendFunc := func(msg *v1pb.ManagerMachineStreamMessage) error {
		return stream.Send(msg)
	}

	sess := s.dispatcher.RegisterMachine(machine.ID, machine.ResourceID, sendFunc)
	// Identity-aware teardown: if a reconnect replaced this session before the
	// old stream ends, do not destroy the new (live) session.
	defer s.dispatcher.UnregisterMachineIf(machine.ID, sess)

	// Drain this machine's queued control interactions (cancel/steer issued
	// while offline) in issue order (design §3.5: control issued offline takes
	// effect only after the machine comes back).
	s.dispatcher.DispatchPendingAgentControl(machine.ID)

	slog.Info("machine control stream connected", "machineID", machine.ID, "resourceID", machine.ResourceID)

	for {
		msg, err := stream.Receive()
		if err != nil {
			if err == io.EOF {
				slog.Info("machine control stream closed", "machineID", machine.ID)
				return nil
			}
			return err
		}

		switch m := msg.Message.(type) {
		case *v1pb.MachineStreamMessage_MachineReady:
			// The machine echoes the session id ConnectMachine minted; nothing
			// to persist (the session row is already ACTIVE). Acknowledged for
			// log correlation only.
			slog.Info("machine ready", "machineID", machine.ID, "sessionID", m.MachineReady.GetSessionId())

		case *v1pb.MachineStreamMessage_Ping:
			s.dispatcher.HandleMachinePing(machine.ID, m.Ping)
			if err := s.dispatcher.SendPongToMachine(machine.ID); err != nil {
				slog.Error("failed to send pong to machine", "machineID", machine.ID, "error", err)
			}

		case *v1pb.MachineStreamMessage_ProvidersDiscovered:
			if m.ProvidersDiscovered.GetRequestId() != "" {
				// Completes a pending RefreshMachineProviders round-trip; the
				// DiscoverProviders request is correlated by request_id.
				s.dispatcher.CompletePendingDiscover(m.ProvidersDiscovered)
				break
			}
			// Unsolicited push from the machine's startup background probe: the
			// machine connects immediately (so it shows ONLINE) and reports the
			// discovered providers as soon as the scan finishes. Persist them so
			// the UI does not need a manual refresh to see providers. Use a
			// detached timeout so a concurrently closing stream cannot cancel the
			// persistence halfway.
			updateCtx, updateCancel := context.WithTimeout(context.Background(), 10*time.Second)
			current, err := s.store.GetMachine(updateCtx, machine.ID)
			if err != nil {
				updateCancel()
				slog.Error("failed to load machine for provider update", "machineID", machine.ID, "error", err)
				break
			}
			if current == nil {
				updateCancel()
				slog.Warn("machine disappeared before provider update", "machineID", machine.ID)
				break
			}
			patchInfo := cloneStoreMachineInfo(current.Info)
			patchInfo.AvailableProviders = convertToStoreProviders(m.ProvidersDiscovered.GetProviders())
			if _, err := s.store.UpdateMachine(updateCtx, current, &store.UpdateMachineMessage{Info: patchInfo}); err != nil {
				updateCancel()
				slog.Error("failed to persist machine provider update", "machineID", machine.ID, "error", err)
				break
			}
			updateCancel()

		case *v1pb.MachineStreamMessage_MachineWorkspaceScanResponse:
			s.dispatcher.CompletePendingMachineWorkspaceScan(m.MachineWorkspaceScanResponse)

		case *v1pb.MachineStreamMessage_ModelsDiscovered:
			// Completes a pending RefreshAgentModels round-trip; the
			// DiscoverModels request is correlated by request_id.
			s.dispatcher.CompletePendingModels(m.ModelsDiscovered)

		case *v1pb.MachineStreamMessage_UpgradeProgress:
			s.dispatcher.RecordMachineUpgrade(machine.ID, m.UpgradeProgress)
			slog.Info("machine upgrade progress", "machineID", machine.ID, "version", m.UpgradeProgress.GetVersion(), "stage", m.UpgradeProgress.GetStage(), "error", m.UpgradeProgress.GetError())

		case *v1pb.MachineStreamMessage_WorkspaceListResponse:
			// Completes a pending ListAgentWorkspace round-trip; correlated by
			// request_id.
			s.dispatcher.CompletePendingWorkspaceList(m.WorkspaceListResponse)

		case *v1pb.MachineStreamMessage_WorkspaceReadResponse:
			// Completes a pending ReadAgentWorkspaceFile round-trip.
			s.dispatcher.CompletePendingWorkspaceRead(m.WorkspaceReadResponse)

		case *v1pb.MachineStreamMessage_MachineMetricsResponse:
			// Completes a pending GetMachineMetrics round-trip.
			s.dispatcher.CompletePendingMetrics(m.MachineMetricsResponse)

		case *v1pb.MachineStreamMessage_PromptReleaseNoticeAck:
			ack := m.PromptReleaseNoticeAck
			agentID, err := s.dispatcher.ResolveAgentByName(machine.ID, ack.GetAgentName())
			if err != nil {
				slog.Warn("prompt release notice ack from an unhosted agent", "machineID", machine.ID, "agentName", ack.GetAgentName())
				break
			}
			if err := s.dispatcher.HandlePromptReleaseNoticeAck(ctx, agentID, ack); err != nil {
				slog.Warn("failed to record prompt release notice ack", "machineID", machine.ID, "error", err)
			}

		case *v1pb.MachineStreamMessage_DisconnectNotice:
			slog.Info("machine announced graceful disconnect", "machineID", machine.ID, "reason", m.DisconnectNotice.GetReason())
			return nil

		default:
			slog.Warn("unknown machine stream message type", "machineID", machine.ID)
		}
	}
}
