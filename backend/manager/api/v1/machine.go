package v1

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/laelia/backend/common"
	"github.com/Ranxy/laelia/backend/common/log"
	"github.com/Ranxy/laelia/backend/common/permission"
	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/laelia/backend/manager/api/auth"
	"github.com/Ranxy/laelia/backend/manager/component/dispatcher"
	"github.com/Ranxy/laelia/backend/manager/component/iam"
	"github.com/Ranxy/laelia/backend/manager/component/state"
	"github.com/Ranxy/laelia/backend/manager/config"
	"github.com/Ranxy/laelia/backend/manager/store"
)

// machineRefreshTokenDuration is how long a machine refresh token stays
// valid. The refresh token is a durable, multi-use reconnection credential
// (not single-use-rotated on every reconnect), so its lifetime must cover
// long downtime: a desktop powered off over a weekend, a laptop on a trip,
// a host offline for patching. 90d bounds a stolen token's value while
// keeping manual rotation quarterly.
const machineRefreshTokenDuration = 90 * 24 * time.Hour

// machineRefreshRotateWindow is how close to expiry a refresh token must be
// before RefreshMachineToken mints a replacement (rolling renewal). Inside
// the window the old token is left ACTIVE — it expires on its own within the
// window, so a thief holding the pre-renewal token has at most this long. The
// common reconnect (outside the window) reuses the same token and never
// consumes it, so a lost refresh response is safely retryable.
const machineRefreshRotateWindow = 10 * 24 * time.Hour

// MachineService implements MachineService: management RPCs (admin/IAM) for
// machines and the machine-side authentication RPCs the machine app calls to
// register itself and stay connected. A machine authenticates once with a
// registration token (bootstrap) and then hosts one or more agents, each
// running its own AgentChannel over the machine's access token.
type MachineService struct {
	v1connect.UnimplementedMachineServiceHandler
	store      *store.Store
	secret     string
	profile    *config.Profile
	stateCfg   *state.State
	dispatcher *dispatcher.Dispatcher
	iam        *iam.Manager
}

func NewMachineService(s *store.Store, secret string, profile *config.Profile, stateCfg *state.State, d *dispatcher.Dispatcher, iamManager *iam.Manager) *MachineService {
	return &MachineService{
		store:      s,
		secret:     secret,
		profile:    profile,
		stateCfg:   stateCfg,
		dispatcher: d,
		iam:        iamManager,
	}
}

func (s *MachineService) CreateMachine(ctx context.Context, req *connect.Request[v1pb.CreateMachineRequest]) (*connect.Response[v1pb.CreateMachineResponse], error) {
	if req.Msg.Machine == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("machine must be set"))
	}
	if req.Msg.Machine.Title == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("machine title must be set"))
	}

	creatorID := 0
	if user, _ := GetUserFromContext(ctx); user != nil {
		creatorID = user.ID
	}

	created, err := s.store.CreateMachine(ctx, &store.MachineMessage{
		Name:         req.Msg.Machine.Title,
		TokenVersion: 1,
		Info:         &storepb.MachineInfo{},
		Status:       &storepb.MachineStatus{},
		CreatedBy:    creatorID,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to create machine, error: %v", err))
	}

	// Mint a single-use registration (bootstrap) token the machine app presents
	// on first connect. Mirrors agent bootstrap: 7-day validity, consumed once
	// ConnectMachine succeeds.
	registrationToken, err := auth.GenerateMachineToken(created.Name, created.ResourceID, created.TokenVersion, auth.TokenTypeBootstrap, s.profile.Mode, s.secret, bootstrapTokenDuration)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate machine token, error: %v", err))
	}
	if err := s.store.CreateMachineToken(ctx, &store.MachineTokenMessage{
		MachineID:   created.ID,
		TokenHash:   hashToken(registrationToken),
		TokenType:   storepb.MachineTokenType_MACHINE_BOOTSTRAP,
		TokenFamily: created.ResourceID,
		State:       storepb.MachineTokenState_MACHINE_TOKEN_ACTIVE,
		ExpiresAt:   time.Now().Add(bootstrapTokenDuration),
		CreatedBy:   "system",
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to store machine token, error: %v", err))
	}

	return connect.NewResponse(&v1pb.CreateMachineResponse{
		Machine:           convertToMachine(created),
		RegistrationToken: registrationToken,
	}), nil
}

func (s *MachineService) ListMachines(ctx context.Context, req *connect.Request[v1pb.ListMachinesRequest]) (*connect.Response[v1pb.ListMachinesResponse], error) {
	offset, err := parseLimitAndOffset(&pageSize{
		token:   req.Msg.PageToken,
		limit:   int(req.Msg.PageSize),
		maximum: 1000,
	})
	if err != nil {
		return nil, err
	}

	// Visibility is per-machine (creator or laelia.machines.createAgent on the
	// machine), so the whole roster is fetched and filtered in the handler and
	// then paginated in memory. Machine counts are small (a workspace has a
	// handful of hosts), so this stays cheap.
	machines, err := s.store.ListMachines(ctx, &store.FindMachineMessage{ShowDeleted: req.Msg.ShowDeleted})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to list machines, error: %v", err))
	}

	caller, _ := GetUserFromContext(ctx)
	visible := make([]*store.MachineMessage, 0, len(machines))
	for _, m := range machines {
		if canSeeMachine(ctx, s.iam, caller, m) {
			visible = append(visible, m)
		}
	}

	start := offset.offset
	if start > len(visible) {
		start = len(visible)
	}
	end := start + offset.limit
	if end > len(visible) {
		end = len(visible)
	}
	page := visible[start:end]

	nextPageToken := ""
	if end < len(visible) {
		if nextPageToken, err = offset.getNextPageToken(); err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to marshal next page token, error: %v", err))
		}
	}

	// One batched count query for the whole page instead of a ListAgents query
	// per row, so a page of N machines costs 2 queries, not N+1.
	machineIDs := make([]int, 0, len(page))
	for _, m := range page {
		machineIDs = append(machineIDs, m.ID)
	}
	agentCounts, err := s.store.CountAgentsByMachine(ctx, machineIDs)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to count machine agents, error: %v", err))
	}

	// machines.edit / machines.delete are workspace-scope permissions, so the
	// IAM lookups are done once for the whole page; per machine only the
	// creator comparison varies (a page of N machines stays 2 queries, not N+1).
	canEdit := s.canEditMachine(ctx, caller)
	canDelete := canDeleteMachineByPermission(ctx, s.iam, caller)
	resp := &v1pb.ListMachinesResponse{NextPageToken: nextPageToken}
	for _, m := range page {
		summary := convertToMachineSummary(m, agentCounts[m.ID])
		summary.CanEdit = canEdit
		summary.CanManage = canEdit || isMachineCreator(caller, m)
		summary.CanDelete = canDelete || isMachineCreator(caller, m)
		resp.Machines = append(resp.Machines, summary)
	}
	return connect.NewResponse(resp), nil
}

// isMachineCreator reports whether the caller created the machine.
func isMachineCreator(user *store.UserMessage, machine *store.MachineMessage) bool {
	return user != nil && machine.CreatedBy != 0 && machine.CreatedBy == user.ID
}

// canDeleteMachineByPermission reports whether the caller holds
// laelia.machines.delete (workspace-scope). A lookup failure is fail-closed.
func canDeleteMachineByPermission(ctx context.Context, im *iam.Manager, user *store.UserMessage) bool {
	if user == nil || im == nil {
		return false
	}
	ok, err := im.CheckPermission(ctx, permission.MachinesDelete, user, nil, nil)
	if err != nil {
		return false
	}
	return ok
}

// canDeleteMachine reports whether the caller may delete the machine: its
// creator or a holder of laelia.machines.delete (workspace-scope).
func canDeleteMachine(ctx context.Context, im *iam.Manager, user *store.UserMessage, machine *store.MachineMessage) bool {
	if isMachineCreator(user, machine) {
		return true
	}
	return canDeleteMachineByPermission(ctx, im, user)
}

// canSeeMachine reports whether the caller may see a machine: its creator or a
// principal who may create agents on it (laelia.machines.createAgent on the
// machine — workspace admins via the workspace-scoped permission, or
// roles/machineAgentCreator bound in the machine's IAM policy). Visibility
// deliberately equals the create-agent rule: a user granted "who can create
// agents" on a machine may see it. Fail-closed: a nil caller, nil manager, or
// a lookup error denies.
func canSeeMachine(ctx context.Context, im *iam.Manager, user *store.UserMessage, machine *store.MachineMessage) bool {
	if user == nil || im == nil {
		return false
	}
	ok, err := canCreateAgentOnMachine(ctx, im, user, machine)
	return err == nil && ok
}

func (s *MachineService) GetMachine(ctx context.Context, req *connect.Request[v1pb.GetMachineRequest]) (*connect.Response[v1pb.Machine], error) {
	resourceID, err := common.GetMachineResourceID(req.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	machine, err := s.store.GetMachineByResourceID(ctx, resourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to get machine, error: %v", err))
	}
	if machine == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("machine %s not found", resourceID))
	}
	caller, _ := GetUserFromContext(ctx)
	// An invisible machine is indistinguishable from a missing one (NotFound),
	// so existence is not leaked to users without access.
	if !canSeeMachine(ctx, s.iam, caller, machine) {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("machine %s not found", resourceID))
	}
	out := convertToMachine(machine)
	out.CanEdit = s.canEditMachine(ctx, caller)
	// Visibility is granted by the create-agent rule, so a visible machine
	// always allows agent creation for this caller.
	out.CanCreateAgent = true
	out.CanManage = isMachineAdmin(ctx, s.iam, caller, machine)
	return connect.NewResponse(out), nil
}

// canEditMachine reports whether the caller holds laelia.machines.edit.
// Machines are workspace-scoped (no per-machine IAM policy), so the check is a
// workspace-baseline lookup: only workspaceAdmin holds machines.edit (via the
// all-permissions union). A lookup failure is fail-closed.
func (s *MachineService) canEditMachine(ctx context.Context, user *store.UserMessage) bool {
	if user == nil || s.iam == nil {
		return false
	}
	ok, err := s.iam.CheckPermission(ctx, permission.MachinesEdit, user, nil, nil)
	if err != nil {
		return false
	}
	return ok
}

// isMachineAdmin reports whether the caller may manage a machine: the machine's
// creator or a workspace admin (laelia.machines.edit). Shared by the machine
// policy handlers, the token/management RPCs, and the can_manage field on
// GetMachine and ListMachines.
func isMachineAdmin(ctx context.Context, im *iam.Manager, user *store.UserMessage, machine *store.MachineMessage) bool {
	if isMachineCreator(user, machine) {
		return true
	}
	if user == nil || im == nil {
		return false
	}
	ok, err := im.CheckPermission(ctx, permission.MachinesEdit, user, nil, nil)
	if err != nil {
		return false
	}
	return ok
}

func (s *MachineService) DeleteMachine(ctx context.Context, req *connect.Request[v1pb.DeleteMachineRequest]) (*connect.Response[emptypb.Empty], error) {
	resourceID, err := common.GetMachineResourceID(req.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	machine, err := s.store.GetMachineByResourceID(ctx, resourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to get machine, error: %v", err))
	}
	if machine == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("machine %s not found", resourceID))
	}
	user, _ := GetUserFromContext(ctx)
	if !canDeleteMachine(ctx, s.iam, user, machine) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("only the machine's creator or a workspace admin can delete this machine"))
	}

	// Atomically soft-delete iff the machine hosts no live agents, so a
	// concurrent CreateAgent cannot slip into the gap between the agent-count
	// check and the soft-delete (agents are bound by machine_id and a soft
	// delete would otherwise orphan them). ok=false means the machine was not
	// found, already deleted, or still hosts agents; re-fetch to distinguish.
	ok, err := s.store.DeleteMachineIfNoAgents(ctx, resourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to delete machine, error: %v", err))
	}
	if !ok {
		current, err := s.store.GetMachineByResourceID(ctx, resourceID)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to get machine, error: %v", err))
		}
		if current == nil {
			return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("machine %s not found", resourceID))
		}
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.Errorf("machine %s still hosts agent(s); delete them first", resourceID))
	}
	// Tidy up the machine's IAM policy row (who may create agents on it). Best
	// effort: the machine is already deleted, so a cleanup failure must not turn
	// a successful delete into an error the client would retry into a NotFound.
	if err := s.store.DeletePolicyV2(ctx, &store.PolicyMessage{
		ResourceType: storepb.Policy_MACHINE,
		Resource:     common.FormatMachineUID(resourceID),
		Type:         storepb.Policy_IAM,
	}); err != nil {
		slog.Warn("failed to clean up machine iam policy", slog.String("machine", resourceID), log.WithError(err))
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

// RotateMachineToken bumps the machine's token_version, revokes every machine
// token, terminates all sessions, and mints a fresh single-use registration
// token. The machine app must re-ConnectMachine with the new registration token
// and re-open all its agent runners; in-flight commands fail in the grace window.
func (s *MachineService) RotateMachineToken(ctx context.Context, req *connect.Request[v1pb.RotateMachineTokenRequest]) (*connect.Response[v1pb.RotateMachineTokenResponse], error) {
	resourceID, err := common.GetMachineResourceID(req.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	machine, err := s.store.GetMachineByResourceID(ctx, resourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to get machine, error: %v", err))
	}
	if machine == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("machine %s not found", resourceID))
	}
	user, _ := GetUserFromContext(ctx)
	if !isMachineAdmin(ctx, s.iam, user, machine) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("only the machine's creator or a workspace admin can rotate this machine's token"))
	}

	newTokenVersion := machine.TokenVersion + 1
	newTokenFamily := fmt.Sprintf("%s:v%d", machine.ResourceID, newTokenVersion)

	registrationToken, err := auth.GenerateMachineTokenWithFamily(machine.Name, machine.ResourceID, newTokenVersion, auth.TokenTypeBootstrap, newTokenFamily, s.profile.Mode, s.secret, bootstrapTokenDuration)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate machine token, error: %v", err))
	}

	// Bump the version (invalidates old tokens), revoke every existing token,
	// and store the new bootstrap — atomically, in one transaction. A failure
	// leaves the machine on its current credentials so it is never tokenless;
	// the admin can retry. Session teardown + dispatcher unregister below are
	// best-effort and outside the transaction.
	nowRotated := time.Now()
	if _, err := s.store.RotateMachineTokens(ctx, machine, newTokenVersion, nowRotated, &store.MachineTokenMessage{
		MachineID:   machine.ID,
		TokenHash:   hashToken(registrationToken),
		TokenType:   storepb.MachineTokenType_MACHINE_BOOTSTRAP,
		TokenFamily: newTokenFamily,
		State:       storepb.MachineTokenState_MACHINE_TOKEN_ACTIVE,
		ExpiresAt:   time.Now().Add(bootstrapTokenDuration),
		CreatedBy:   "system",
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to rotate machine tokens, error: %v", err))
	}

	terminateReason := "token_rotated"
	if req.Msg.Reason != "" {
		terminateReason = req.Msg.Reason
	}
	if err := s.store.TerminateAllMachineSessions(ctx, machine.ID, terminateReason); err != nil {
		// non-fatal: the version bump + token revocation already invalidate the
		// old connection; a stale session row is cosmetic.
		slog.Info("non-fatal failure terminating sessions during token rotation", "machineID", machine.ID, "error", err)
	}
	if s.dispatcher != nil {
		s.dispatcher.UnregisterMachine(machine.ID)
	}

	return connect.NewResponse(&v1pb.RotateMachineTokenResponse{
		RegistrationToken: registrationToken,
	}), nil
}

// RevokeMachineToken bumps the token_version and revokes every token + session
// without issuing a new registration token. The machine app cannot reconnect
// until an admin rotates the token again.
func (s *MachineService) RevokeMachineToken(ctx context.Context, req *connect.Request[v1pb.RevokeMachineTokenRequest]) (*connect.Response[v1pb.RevokeMachineTokenResponse], error) {
	resourceID, err := common.GetMachineResourceID(req.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	machine, err := s.store.GetMachineByResourceID(ctx, resourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to get machine, error: %v", err))
	}
	if machine == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("machine %s not found", resourceID))
	}
	user, _ := GetUserFromContext(ctx)
	if !isMachineAdmin(ctx, s.iam, user, machine) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("only the machine's creator or a workspace admin can revoke this machine's tokens"))
	}

	newTokenVersion := machine.TokenVersion + 1
	nowRotated := time.Now()
	if _, err := s.store.UpdateMachine(ctx, machine, &store.UpdateMachineMessage{
		TokenVersion:       &newTokenVersion,
		LastTokenRotatedAt: &nowRotated,
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to update machine token version, error: %v", err))
	}

	if err := s.store.RevokeAllMachineTokens(ctx, machine.ID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to revoke machine tokens, error: %v", err))
	}

	terminateReason := "token_revoked"
	if req.Msg.Reason != "" {
		terminateReason = req.Msg.Reason
	}
	if err := s.store.TerminateAllMachineSessions(ctx, machine.ID, terminateReason); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to terminate machine sessions, error: %v", err))
	}
	if s.dispatcher != nil {
		s.dispatcher.UnregisterMachine(machine.ID)
	}

	return connect.NewResponse(&v1pb.RevokeMachineTokenResponse{}), nil
}

// ForceDisconnectMachine terminates all machine sessions, marks the machine
// OFFLINE, and tears down the dispatcher's machine + agent sessions (failing
// in-flight commands after the 60s grace).
func (s *MachineService) ForceDisconnectMachine(ctx context.Context, req *connect.Request[v1pb.ForceDisconnectMachineRequest]) (*connect.Response[emptypb.Empty], error) {
	resourceID, err := common.GetMachineResourceID(req.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	machine, err := s.store.GetMachineByResourceID(ctx, resourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to get machine, error: %v", err))
	}
	if machine == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("machine %s not found", resourceID))
	}
	user, _ := GetUserFromContext(ctx)
	if !isMachineAdmin(ctx, s.iam, user, machine) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("only the machine's creator or a workspace admin can force-disconnect this machine"))
	}

	reason := "admin_forced"
	if req.Msg.Reason != "" {
		reason = req.Msg.Reason
	}
	if err := s.store.TerminateAllMachineSessions(ctx, machine.ID, reason); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to terminate machine sessions, error: %v", err))
	}

	if _, err := s.store.UpdateMachine(ctx, machine, &store.UpdateMachineMessage{
		Status: &storepb.MachineStatus{State: storepb.MachineStatus_OFFLINE},
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to update machine status, error: %v", err))
	}

	if s.dispatcher != nil {
		s.dispatcher.UnregisterMachine(machine.ID)
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (s *MachineService) ListMachineAgents(ctx context.Context, req *connect.Request[v1pb.ListMachineAgentsRequest]) (*connect.Response[v1pb.ListMachineAgentsResponse], error) {
	resourceID, err := common.GetMachineResourceID(req.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	machine, err := s.store.GetMachineByResourceID(ctx, resourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to get machine, error: %v", err))
	}
	if machine == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("machine %s not found", resourceID))
	}
	caller, _ := GetUserFromContext(ctx)
	// Same visibility rule as GetMachine: an invisible machine is
	// indistinguishable from a missing one.
	if !canSeeMachine(ctx, s.iam, caller, machine) {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("machine %s not found", resourceID))
	}

	agents, err := s.store.ListAgents(ctx, &store.FindAgentMessage{MachineID: &machine.ID})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to list machine agents, error: %v", err))
	}

	// can_delete is the cheap subset of canEditAgent: one workspace-scope
	// agents.edit lookup for the whole roster plus the per-row owner comparison
	// (per-agent policy bindings are not consulted, so a custom role bound on
	// the agent may still delete server-side while the UI hides the button).
	canDelete := canDeleteAgentWorkspace(ctx, s.iam, caller)
	resp := &v1pb.ListMachineAgentsResponse{}
	for _, agent := range agents {
		summary := convertToAgentSummary(agent, agentReachable(s.dispatcher, agent.ID, agent.MachineID))
		summary.CanDelete = canDelete || isAgentOwner(caller, agent)
		resp.Agents = append(resp.Agents, summary)
	}
	return connect.NewResponse(resp), nil
}

// RefreshMachineProviders asks the machine app to re-probe its host for
// installed LLM agent providers + models, then persists the fresh result into
// machine.info.available_providers and returns it. Requires the machine to be
// online (the probe runs on the machine's host, reached via MachineChannel).
func (s *MachineService) RefreshMachineProviders(ctx context.Context, req *connect.Request[v1pb.RefreshMachineProvidersRequest]) (*connect.Response[v1pb.RefreshMachineProvidersResponse], error) {
	resourceID, err := common.GetMachineResourceID(req.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	machine, err := s.store.GetMachineByResourceID(ctx, resourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if machine == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("machine %s not found", resourceID))
	}
	user, _ := GetUserFromContext(ctx)
	if !isMachineAdmin(ctx, s.iam, user, machine) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("only the machine's creator or a workspace admin can refresh this machine's providers"))
	}
	if s.dispatcher == nil || !s.dispatcher.IsMachineConnected(machine.ID) {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("machine is not connected; cannot probe providers"))
	}

	requestID := uuid.NewString()
	replyCh := s.dispatcher.RegisterPendingDiscover(requestID)
	defer s.dispatcher.CancelPendingDiscover(requestID)

	if err := s.dispatcher.SendDiscoverProvidersToMachine(machine.ID, requestID); err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.Wrap(err, "failed to request provider discovery"))
	}

	select {
	case msg := <-replyCh:
		if msg == nil {
			return nil, connect.NewError(connect.CodeInternal, errors.New("provider discovery returned no result"))
		}
		patchInfo := cloneStoreMachineInfo(machine.Info)
		patchInfo.AvailableProviders = convertToStoreProviders(msg.Providers)
		if _, err := s.store.UpdateMachine(ctx, machine, &store.UpdateMachineMessage{Info: patchInfo}); err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to persist discovered providers"))
		}
		return connect.NewResponse(&v1pb.RefreshMachineProvidersResponse{
			Providers: convertToV1Providers(patchInfo.AvailableProviders),
		}), nil
	case <-time.After(60 * time.Second):
		return nil, connect.NewError(connect.CodeDeadlineExceeded, errors.New("timed out waiting for provider discovery"))
	case <-ctx.Done():
		return nil, connect.NewError(connect.CodeDeadlineExceeded, ctx.Err())
	}
}

// ListMachineWorkspaces summarizes every per-agent workspace directory on a
// machine's host. Requires the machine creator or a workspace admin
// (isMachineAdmin, matching Machine.can_manage) and an online machine.
func (s *MachineService) ListMachineWorkspaces(ctx context.Context, req *connect.Request[v1pb.ListMachineWorkspacesRequest]) (*connect.Response[v1pb.ListMachineWorkspacesResponse], error) {
	resourceID, err := common.GetMachineResourceID(req.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	machine, err := s.store.GetMachineByResourceID(ctx, resourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if machine == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("machine %s not found", resourceID))
	}
	user, _ := GetUserFromContext(ctx)
	if !isMachineAdmin(ctx, s.iam, user, machine) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("workspace access requires machine creator or admin permission"))
	}
	if s.dispatcher == nil || !s.dispatcher.IsMachineConnected(machine.ID) {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("machine is not connected; cannot scan workspaces"))
	}

	requestID := uuid.NewString()
	replyCh := s.dispatcher.RegisterPendingMachineWorkspaceScan(requestID)
	defer s.dispatcher.CancelPendingMachineWorkspaceScan(requestID)

	if err := s.dispatcher.SendMachineWorkspaceScan(machine.ID, requestID); err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.Wrap(err, "failed to request workspace scan"))
	}

	select {
	case msg := <-replyCh:
		if msg == nil {
			return nil, connect.NewError(connect.CodeInternal, errors.New("workspace scan returned no result"))
		}
		return connect.NewResponse(&v1pb.ListMachineWorkspacesResponse{Workspaces: msg.Workspaces}), nil
	case <-time.After(60 * time.Second):
		return nil, connect.NewError(connect.CodeDeadlineExceeded, errors.New("timed out waiting for workspace scan"))
	case <-ctx.Done():
		return nil, connect.NewError(connect.CodeDeadlineExceeded, ctx.Err())
	}
}

// ConnectMachine is the machine app's first (registration token) or subsequent
// (access token) connection. On success it mints the machine's access + refresh
// tokens (bootstrap path only), creates a machine session, marks the machine
// ONLINE, and returns the full list of agents the machine must host — the
// machine app opens one AgentChannel per entry immediately and on every
// reconnect.
func (s *MachineService) ConnectMachine(ctx context.Context, req *connect.Request[v1pb.ConnectMachineRequest]) (*connect.Response[v1pb.ConnectMachineResponse], error) {
	machine, ok := GetMachineFromContext(ctx)
	tokenFamily := ""
	bootstrapTokenID := 0
	if !ok || machine == nil {
		if req.Msg.RegistrationToken == "" {
			return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("machine not authenticated and no registration token provided"))
		}
		authResult, err := s.authenticateMachineRegistrationToken(req.Msg.RegistrationToken)
		if err != nil {
			return nil, err
		}
		machine = authResult.machine
		tokenFamily = authResult.tokenFamily
		bootstrapTokenID = authResult.tokenID
		// Consume the single-use registration token atomically BEFORE any
		// state mutation. The conditional UPDATE (state=ACTIVE → CONSUMED) is
		// the serialization point: two concurrent ConnectMachine calls with the
		// same registration token race here, and only the winner (rows-affected
		// == 1) proceeds to mint tokens and create a session. The loser gets
		// Unauthenticated and must not have created a session.
		consumed, err := s.store.ConsumeMachineToken(ctx, bootstrapTokenID)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to consume registration token, error: %v", err))
		}
		if !consumed {
			return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("registration token is no longer active (consumed or revoked)"))
		}
	}
	if tokenFamily == "" {
		tokenFamily = machine.ResourceID
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
		SessionID:   sessionID,
		MachineID:   machine.ID,
		TokenFamily: tokenFamily,
		State:       "ACTIVE",
		SourceIP:    sourceIP,
		Fingerprint: req.Msg.Fingerprint,
		ConnectedAt: now,
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to create machine session, error: %v", err))
	}

	// Mint the machine's initial access + refresh tokens only on the bootstrap
	// (first connect) path. On reconnect the machine authenticated via an
	// access token it already holds from RefreshMachineToken.
	accessToken := ""
	refreshToken := ""
	accessTokenExpiresAt := time.Time{}
	if bootstrapTokenID != 0 {
		accessToken, err = auth.GenerateMachineTokenWithSession(updated.Name, updated.ResourceID, updated.TokenVersion, auth.TokenTypeAccess, sessionID, s.profile.Mode, s.secret, accessTokenDuration)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate access token, error: %v", err))
		}
		refreshToken, err = auth.GenerateMachineTokenWithSession(updated.Name, updated.ResourceID, updated.TokenVersion, auth.TokenTypeRefresh, "", s.profile.Mode, s.secret, machineRefreshTokenDuration)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate refresh token, error: %v", err))
		}
		if err := s.store.CreateMachineToken(ctx, &store.MachineTokenMessage{
			MachineID:   machine.ID,
			TokenHash:   hashToken(refreshToken),
			TokenType:   storepb.MachineTokenType_MACHINE_REFRESH,
			TokenFamily: tokenFamily,
			State:       storepb.MachineTokenState_MACHINE_TOKEN_ACTIVE,
			Fingerprint: req.Msg.Fingerprint,
			ExpiresAt:   time.Now().Add(machineRefreshTokenDuration),
		}); err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to store refresh token, error: %v", err))
		}
		accessTokenExpiresAt = time.Now().Add(accessTokenDuration)

		// The registration token was already consumed atomically at the top of
		// ConnectMachine (the single-use serialization point); nothing to do
		// here on success.
	}

	// Resync the full agent roster: the machine app opens an AgentChannel for
	// every agent bound to this machine, on first connect and every reconnect.
	assigned, err := s.buildAssignedAgents(ctx, machine.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to list assigned agents"))
	}

	resp := &v1pb.ConnectMachineResponse{
		SessionId:      sessionID,
		InitialStatus:  convertToV1MachineStatus(updated.Status, updated.Deleted),
		AssignedAgents: assigned,
	}
	if accessToken != "" {
		resp.AccessToken = accessToken
		resp.RefreshToken = refreshToken
		resp.AccessTokenExpiresAt = timestamppb.New(accessTokenExpiresAt)
	}
	return connect.NewResponse(resp), nil
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
// mismatch (an admin RotateMachineToken bumps the version and revokes the
// family); a CONSUMED/REVOKED token still triggers family revocation as a
// safety net.
func (s *MachineService) RefreshMachineToken(ctx context.Context, req *connect.Request[v1pb.RefreshMachineTokenRequest]) (*connect.Response[v1pb.RefreshMachineTokenResponse], error) {
	refreshTokenStr := req.Msg.RefreshToken
	if refreshTokenStr == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("refresh token is required"))
	}

	claims, err := auth.ParseMachineToken(refreshTokenStr, s.secret)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.Wrap(err, "invalid refresh token"))
	}
	if claims.TokenType != auth.TokenTypeRefresh {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.Errorf("expected refresh token, got %s", claims.TokenType))
	}

	tokenHash := hashToken(refreshTokenStr)
	storedToken, err := s.store.GetMachineTokenByHash(ctx, tokenHash)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to look up refresh token, error: %v", err))
	}
	if storedToken == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("invalid refresh token"))
	}

	switch action := machineRefreshReuseAction(storedToken.State); action {
	case refreshActionProceed:
	case refreshActionRevokeFamily:
		// A CONSUMED or REVOKED refresh token being presented again. The
		// multi-use flow never consumes a refresh token, so reaching here means
		// either an artifact of the old single-use flow, an admin
		// Revoke/RotateMachineToken, or genuine theft — revoke the family and
		// reject in all cases.
		if err := s.store.RevokeMachineTokenFamily(ctx, storedToken.TokenFamily); err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to revoke token family, error: %v", err))
		}
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("refresh token reuse detected, token family revoked"))
	default:
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("invalid refresh token state"))
	}

	if time.Now().After(storedToken.ExpiresAt) {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("refresh token expired"))
	}

	if req.Msg.Fingerprint != "" && storedToken.Fingerprint != "" && req.Msg.Fingerprint != storedToken.Fingerprint {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("fingerprint mismatch, possible token theft detected"))
	}

	machine, err := s.store.GetMachine(ctx, storedToken.MachineID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to get machine, error: %v", err))
	}
	if machine == nil || machine.Deleted {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("machine not found or deactivated"))
	}
	if claims.TokenVersion != machine.TokenVersion {
		if err := s.store.RevokeMachineTokenFamily(ctx, storedToken.TokenFamily); err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to revoke stale token family, error: %v", err))
		}
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("refresh token version mismatch"))
	}

	accessToken, err := auth.GenerateMachineTokenWithSession(machine.Name, machine.ResourceID, machine.TokenVersion, auth.TokenTypeAccess, "", s.profile.Mode, s.secret, accessTokenDuration)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate access token, error: %v", err))
	}

	// Multi-use: reuse the same refresh token across reconnects. Mint a
	// replacement only when the current one is within the rolling-renewal
	// window of expiry, so the credential self-renews without manual rotation
	// and the machine never dies from expiry as long as it reconnects within
	// the window. The old token is left ACTIVE and expires on its own within
	// the window (a pre-renewal thief is bounded by it); it is not consumed, so
	// a lost renewal response is safely retryable.
	newRefreshToken := ""
	if time.Until(storedToken.ExpiresAt) < machineRefreshRotateWindow {
		newRefreshToken, err = auth.GenerateMachineTokenWithSession(machine.Name, machine.ResourceID, machine.TokenVersion, auth.TokenTypeRefresh, "", s.profile.Mode, s.secret, machineRefreshTokenDuration)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate refresh token, error: %v", err))
		}
		if err := s.store.CreateMachineToken(ctx, &store.MachineTokenMessage{
			MachineID:   machine.ID,
			TokenHash:   hashToken(newRefreshToken),
			TokenType:   storepb.MachineTokenType_MACHINE_REFRESH,
			TokenFamily: storedToken.TokenFamily,
			State:       storepb.MachineTokenState_MACHINE_TOKEN_ACTIVE,
			Fingerprint: req.Msg.Fingerprint,
			ExpiresAt:   time.Now().Add(machineRefreshTokenDuration),
		}); err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to store new refresh token, error: %v", err))
		}
	}

	return connect.NewResponse(&v1pb.RefreshMachineTokenResponse{
		AccessToken:          accessToken,
		RefreshToken:         newRefreshToken,
		AccessTokenExpiresAt: timestamppb.New(time.Now().Add(accessTokenDuration)),
	}), nil
}

func machineRefreshReuseAction(state storepb.MachineTokenState) refreshAction {
	switch state {
	case storepb.MachineTokenState_MACHINE_TOKEN_ACTIVE:
		return refreshActionProceed
	case storepb.MachineTokenState_MACHINE_TOKEN_CONSUMED, storepb.MachineTokenState_MACHINE_TOKEN_REVOKED:
		return refreshActionRevokeFamily
	default:
		return refreshActionInvalid
	}
}

type machineRegistrationAuthResult struct {
	machine     *store.MachineMessage
	tokenFamily string
	tokenID     int
}

// authenticateMachineRegistrationToken verifies a registration (bootstrap)
// token presented to ConnectMachine: JWT signature, token_type=BOOTSTRAP, the
// machine exists + is not deleted + token_version matches, and the stored token
// row is ACTIVE and not expired. Mirrors authenticateBootstrapToken.
func (s *MachineService) authenticateMachineRegistrationToken(tokenStr string) (*machineRegistrationAuthResult, error) {
	claims, err := auth.ParseMachineToken(tokenStr, s.secret)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.Wrap(err, "invalid registration token"))
	}
	if claims.TokenType != auth.TokenTypeBootstrap {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.Errorf("expected registration token, got %s", claims.TokenType))
	}

	machine, err := s.store.GetMachineByResourceID(context.Background(), claims.Subject)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to find machine: %v", err))
	}
	if machine == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.Errorf("machine %s not found", claims.Subject))
	}
	if machine.Deleted {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.Errorf("machine %s has been deactivated", claims.Subject))
	}
	if machine.TokenVersion != claims.TokenVersion {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("machine token version mismatch"))
	}

	storedToken, err := s.store.GetMachineTokenByHash(context.Background(), hashToken(tokenStr))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to look up token: %v", err))
	}
	if storedToken == nil || storedToken.State != storepb.MachineTokenState_MACHINE_TOKEN_ACTIVE {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("registration token is not active"))
	}
	if time.Now().After(storedToken.ExpiresAt) {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("registration token expired"))
	}

	tokenFamily := claims.TokenFamily
	if tokenFamily == "" {
		tokenFamily = claims.Subject
	}
	return &machineRegistrationAuthResult{machine: machine, tokenFamily: tokenFamily, tokenID: storedToken.ID}, nil
}

// ---- converters ----

func convertToMachine(m *store.MachineMessage) *v1pb.Machine {
	state := v1pb.State_ACTIVE
	if m.Deleted {
		state = v1pb.State_DELETED
	}
	out := &v1pb.Machine{
		Name:      common.FormatMachineUID(m.ResourceID),
		State:     state,
		Title:     m.Name,
		Info:      convertToV1MachineInfo(m.Info),
		Status:    convertToV1MachineStatus(m.Status, m.Deleted),
		CreatedAt: timestamppb.New(m.CreatedAt),
	}
	if m.CreatedBy != 0 {
		out.CreatedBy = common.FormatUserUID(m.CreatedBy)
	}
	return out
}

func convertToMachineSummary(m *store.MachineMessage, agentCount int) *v1pb.MachineSummary {
	state := v1pb.State_ACTIVE
	if m.Deleted {
		state = v1pb.State_DELETED
	}
	return &v1pb.MachineSummary{
		Name:       common.FormatMachineUID(m.ResourceID),
		State:      state,
		Title:      m.Name,
		Status:     convertToV1MachineStatus(m.Status, m.Deleted),
		AgentCount: int32(agentCount),
	}
}

func convertToV1MachineInfo(info *storepb.MachineInfo) *v1pb.MachineInfo {
	if info == nil {
		return nil
	}
	return &v1pb.MachineInfo{
		Hostname:           info.Hostname,
		Os:                 info.Os,
		Arch:               info.Arch,
		Ip:                 info.Ip,
		Version:            info.Version,
		Labels:             info.Labels,
		Capability:         convertToV1AgentCapability(info.Capability),
		AvailableProviders: convertToV1Providers(info.AvailableProviders),
	}
}

func convertToStoreMachineInfo(info *v1pb.MachineInfo) *storepb.MachineInfo {
	if info == nil {
		return nil
	}
	return &storepb.MachineInfo{
		Hostname:           info.Hostname,
		Os:                 info.Os,
		Arch:               info.Arch,
		Ip:                 info.Ip,
		Version:            info.Version,
		Labels:             info.Labels,
		Capability:         convertToStoreAgentCapability(info.Capability),
		AvailableProviders: convertToStoreProviders(info.AvailableProviders),
	}
}

func convertToV1MachineStatus(status *storepb.MachineStatus, deleted bool) *v1pb.MachineStatus {
	if status == nil {
		return nil
	}
	var lastHeartbeatTime *timestamppb.Timestamp
	if status.LastHeartbeatAt > 0 {
		lastHeartbeatTime = timestamppb.New(time.Unix(status.LastHeartbeatAt, 0))
	}
	var connectedTime *timestamppb.Timestamp
	if status.ConnectedAt > 0 {
		connectedTime = timestamppb.New(time.Unix(status.ConnectedAt, 0))
	}
	return &v1pb.MachineStatus{
		State:             computeMachineConnectionState(status, deleted),
		LastHeartbeatTime: lastHeartbeatTime,
		ConnectedTime:     connectedTime,
		ErrorMessage:      status.ErrorMessage,
		ActiveSessionId:   status.ActiveSessionId,
	}
}

func computeMachineConnectionState(status *storepb.MachineStatus, deleted bool) v1pb.MachineStatus_ConnectionState {
	if status.State == storepb.MachineStatus_ERROR {
		return v1pb.MachineStatus_ERROR
	}
	if status.State == storepb.MachineStatus_KICKED {
		return v1pb.MachineStatus_KICKED
	}
	if deleted {
		return v1pb.MachineStatus_OFFLINE
	}
	threshold := time.Now().Unix() - agentOfflineThresholdSeconds
	if status.LastHeartbeatAt >= threshold {
		return v1pb.MachineStatus_ONLINE
	}
	return v1pb.MachineStatus_OFFLINE
}

// cloneStoreMachineInfo returns a deep copy of info safe to mutate before a
// partial UpdateMachine, or a fresh empty MachineInfo when info is nil.
func cloneStoreMachineInfo(info *storepb.MachineInfo) *storepb.MachineInfo {
	if info == nil {
		return &storepb.MachineInfo{}
	}
	cloned := proto.Clone(info)
	patchInfo, ok := cloned.(*storepb.MachineInfo)
	if !ok || patchInfo == nil {
		return &storepb.MachineInfo{}
	}
	return patchInfo
}
