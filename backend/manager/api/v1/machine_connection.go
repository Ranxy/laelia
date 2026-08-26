package v1

import (
	"context"
	"log/slog"
	"time"

	"connectrpc.com/connect"
	"github.com/Ranxy/laelia/backend/common"
	"github.com/Ranxy/laelia/backend/common/log"
	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/manager/api/auth"
	"github.com/Ranxy/laelia/backend/manager/component/machinebuild"
	"github.com/Ranxy/laelia/backend/manager/store"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ConnectMachine registers a machine session. The machine authenticates via
// its access token (minted by RefreshMachineToken from the refresh token the
// device approval issued); there is no bootstrap/registration path anymore.
// The response carries the session id, the machine's initial status, and the
// full agent roster the machine app must host.
func (s *MachineService) ConnectMachine(ctx context.Context, req *connect.Request[v1pb.ConnectMachineRequest]) (*connect.Response[v1pb.ConnectMachineResponse], error) {
	machine, ok := GetMachineFromContext(ctx)
	if !ok || machine == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("machine not authenticated"))
	}

	sessionID := generateRandomString(sessionIDLength)

	now := time.Now()
	nowSec := now.Unix()

	patch := &store.UpdateMachineMessage{
		Status: &storepb.MachineStatus{
			State:           storepb.MachineStatus_ONLINE,
			ConnectedAt:     nowSec,
			LastHeartbeatAt: nowSec,
			ActiveSessionId: sessionID,
		},
	}
	if req.Msg.Info != nil {
		patch.Info = convertToStoreMachineInfo(req.Msg.Info)
		// The machine now connects before its slow provider scan finishes. Keep
		// any previously discovered providers until the background probe pushes a
		// fresh list, so a reconnect does not temporarily wipe machine.info.
		if len(patch.Info.AvailableProviders) == 0 && machine.Info != nil && len(machine.Info.AvailableProviders) > 0 {
			patch.Info.AvailableProviders = machine.Info.AvailableProviders
		}
	} else {
		patch.Info = &storepb.MachineInfo{}
	}

	updated, err := s.store.UpdateMachine(ctx, machine, patch)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to update machine on connect, error: %v", err))
	}

	if err := s.store.TerminateAllMachineSessions(ctx, machine.ID, "replaced"); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to terminate existing sessions, error: %v", err))
	}

	sourceIP := ""
	if ip, ok := common.GetSourceIPFromContext(ctx); ok {
		sourceIP = ip
	}
	reportedIP := ""
	if req.Msg.Info != nil {
		reportedIP = req.Msg.Info.Ip
	}
	if err := auth.ValidateAgentIP(reportedIP, sourceIP, auth.IPValidationWarn); err != nil {
		return nil, err
	}

	if err := s.store.CreateMachineSession(ctx, &store.MachineSessionMessage{
		SessionID:    sessionID,
		MachineID:    machine.ID,
		TokenFamily:  machine.ResourceID,
		State:        "ACTIVE",
		SourceIP:     sourceIP,
		Fingerprint:  req.Msg.Fingerprint,
		AgentVersion: req.Msg.Info.GetVersion(),
		ConnectedAt:  now,
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to create machine session, error: %v", err))
	}

	// Resync the full agent roster: the machine app opens an AgentChannel for
	// every agent bound to this machine, on first connect and every reconnect.
	assigned, err := s.buildAssignedAgents(ctx, machine.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to list assigned agents"))
	}

	// If the machine's bundled static prompt is out of date relative to the
	// manager's expected version, tell every agent on it to upgrade so running
	// agents perceive the system-prompt change.
	if req.Msg.Info != nil {
		reported := req.Msg.Info.GetPromptBundleVersion()
		expected := machinebuild.LatestPromptBundleVersion()
		if reported != "" && expected != "" && reported != expected {
			notice := &v1pb.PromptReleaseNotice{
				NoticeKey: "prompt-bundle-" + expected,
				Message:   "The system prompt has been updated on the server, but this machine's bundled system prompt is out of date. Upgrade laelia-machine to receive the latest instructions.",
				// Deliberately no prompt_version: a stale-machine notice does not
				// mean the agent has actually seen the new prompt, so it must not
				// be recorded as a confirmed version (which would mask the
				// staleness signal).
			}
			if pushErr := s.dispatcher.PushPromptReleaseNoticeToMachine(ctx, machine.ID, notice); pushErr != nil {
				slog.Info("best-effort stale prompt bundle notice push skipped", "machineID", machine.ID, "error", pushErr)
			}
		}
	}

	return connect.NewResponse(&v1pb.ConnectMachineResponse{
		SessionId:      sessionID,
		InitialStatus:  convertToV1MachineStatus(updated.Status, updated.Deleted),
		AssignedAgents: assigned,
	}), nil
}

// buildAssignedAgents returns the AgentAssignment for every agent bound to the
// machine, in the order the machine app should open their AgentChannels.
func (s *MachineService) buildAssignedAgents(ctx context.Context, machineID int) ([]*v1pb.AgentAssignment, error) {
	agents, err := s.store.ListAgents(ctx, &store.FindAgentMessage{MachineID: &machineID})
	if err != nil {
		return nil, err
	}
	out := make([]*v1pb.AgentAssignment, 0, len(agents))
	for _, agent := range agents {
		// Stopped agents are not assigned: their runner must not be hosted by the
		// machine app. They rejoin the roster on the next resync after StartAgent.
		if !agent.Enabled {
			continue
		}
		acp, err := resolveAcpConfigForDaemon(ctx, s.store, convertToV1AgentACPConfig(agent.Info.GetAcpConfig()))
		if err != nil {
			slog.Warn("failed to resolve acp config for assigned agent", "agent", agent.ResourceID, log.WithError(err))
		}
		out = append(out, &v1pb.AgentAssignment{
			AgentName:        common.FormatAgentUID(agent.ResourceID),
			AgentDisplayName: agent.Name,
			AcpConfig:        acp,
		})
	}
	return out, nil
}

func (s *MachineService) MachineHeartbeat(ctx context.Context, req *connect.Request[v1pb.MachineHeartbeatRequest]) (*connect.Response[v1pb.MachineHeartbeatResponse], error) {
	machine, ok := GetMachineFromContext(ctx)
	if !ok || machine == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("machine not authenticated"))
	}

	// The session id is mandatory: it binds the heartbeat to a concrete ACTIVE
	// session. Without this check a machine whose session was KICKED by
	// ForceDisconnectMachine/RevokeMachineToken could keep heartbeating with an
	// empty session id, flipping status back to ONLINE and even minting a fresh
	// access token — defeating the admin's force-disconnect.
	if req.Msg.SessionId == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("session id is required"))
	}
	session, err := s.store.GetMachineSession(ctx, req.Msg.SessionId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to get machine session, error: %v", err))
	}
	if session == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("session not found"))
	}
	if session.State != "ACTIVE" {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.Errorf("session is %s (replaced or terminated); reconnect via ConnectMachine", session.State))
	}
	if session.MachineID != machine.ID {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("session does not belong to this machine"))
	}
	if err := s.store.TouchMachineSession(ctx, req.Msg.SessionId); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to touch machine session, error: %v", err))
	}

	nowSec := time.Now().Unix()
	activeSessionID := machine.Status.GetActiveSessionId()
	if _, err := s.store.UpdateMachine(ctx, machine, &store.UpdateMachineMessage{
		Status: &storepb.MachineStatus{
			State:           storepb.MachineStatus_ONLINE,
			LastHeartbeatAt: nowSec,
			ConnectedAt:     machine.Status.GetConnectedAt(),
			ActiveSessionId: activeSessionID,
		},
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to update machine heartbeat, error: %v", err))
	}

	resp := &v1pb.MachineHeartbeatResponse{
		NextHeartbeatAt: timestamppb.New(time.Now().Add(30 * time.Second)),
	}

	// Refresh the access token if it is close to expiry, so the machine app
	// stays connected without a full reconnect.
	if expiresAt, ok := common.GetAccessTokenExpiresAtFromContext(ctx); ok && expiresAt > 0 {
		if time.Now().Unix() >= expiresAt-int64(accessTokenDuration.Seconds()/3) {
			if newAccessToken, err := auth.GenerateMachineTokenWithSession(machine.Name, machine.ResourceID, machine.TokenVersion, auth.TokenTypeAccess, req.Msg.SessionId, s.profile.Mode, s.secret, accessTokenDuration); err == nil {
				resp.AccessToken = newAccessToken
				resp.AccessTokenExpiresAt = timestamppb.New(time.Now().Add(accessTokenDuration))
			}
		}
	}

	return connect.NewResponse(resp), nil
}

func (s *MachineService) MachineDisconnect(ctx context.Context, req *connect.Request[v1pb.MachineDisconnectRequest]) (*connect.Response[emptypb.Empty], error) {
	machine, ok := GetMachineFromContext(ctx)
	if !ok || machine == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("machine not authenticated"))
	}

	reason := "machine_shutdown"
	if req.Msg.Reason != "" {
		reason = req.Msg.Reason
	}
	if req.Msg.SessionId != "" {
		if err := s.store.TerminateMachineSession(ctx, req.Msg.SessionId, reason); err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to terminate machine session, error: %v", err))
		}
	}

	if _, err := s.store.UpdateMachine(ctx, machine, &store.UpdateMachineMessage{
		Status: &storepb.MachineStatus{
			State:           storepb.MachineStatus_OFFLINE,
			ActiveSessionId: "",
		},
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to update machine status, error: %v", err))
	}

	if s.dispatcher != nil {
		s.dispatcher.UnregisterMachine(machine.ID)
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

// RefreshMachineToken reissues a machine access token using the persisted
// refresh token. Unlike a single-use refresh-token rotation, the machine
// refresh token is a durable, MULTI-USE reconnection credential: the common
// reconnect reuses the same refresh token (no consumption, no new row), so a
// lost refresh response — e.g. a manager hard-killed mid-request — is safely
// retryable (the same token is presented again, the server does not treat the
// retry as theft). Only when the token is within machineRefreshRotateWindow of
// expiry does the server mint a replacement (rolling renewal), leaving the
// old token to expire on its own. Theft is detected by fingerprint binding
// (rejected if presented from a different machine) and by token-version
// mismatch (a device-approval rotation bumps the version and revokes the
// family); a CONSUMED/REVOKED token still triggers family revocation as a
// safety net.
