package v1

import (
	"context"
	"log/slog"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Ranxy/laelia/backend/common"
	"github.com/Ranxy/laelia/backend/common/log"
	"github.com/Ranxy/laelia/backend/common/permission"
	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
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
// stay connected. A machine authenticates through the device-code flow
// (DeviceService): the manager mints its refresh token at approval time and
// the machine reconnects with access tokens issued by RefreshMachineToken.
// Each machine hosts one or more agents, each running its own AgentChannel
// over the machine's access token.
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
	find := &store.FindMachineMessage{ShowDeleted: req.Msg.ShowDeleted}
	if req.Msg.Provisioner != "" {
		provisionerID, err := s.provisionerIDByName(ctx, req.Msg.Provisioner)
		if err != nil {
			return nil, err
		}
		find.ProvisionerID = &provisionerID
	}
	machines, err := s.store.ListMachines(ctx, find)
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
		summary := s.convertToMachineSummary(ctx, m, agentCounts[m.ID])
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

// provisionerIDByName resolves a provisioners/{id} resource name to a live
// (non-deleted) provisioner row's numeric id, mirroring ProvisionerService's
// getProvisionerByName. Used by ListMachines when a caller narrows the listing
// to one provisioner.
func (s *MachineService) provisionerIDByName(ctx context.Context, name string) (int, error) {
	resourceID, err := common.GetProvisionerResourceID(name)
	if err != nil {
		return 0, connect.NewError(connect.CodeInvalidArgument, err)
	}
	provisioner, err := s.store.GetProvisionerByResourceID(ctx, resourceID)
	if err != nil {
		return 0, connect.NewError(connect.CodeInternal, errors.Errorf("failed to get provisioner: %v", err))
	}
	if provisioner == nil || provisioner.Deleted {
		return 0, connect.NewError(connect.CodeNotFound, errors.Errorf("provisioner %s not found", resourceID))
	}
	return provisioner.ID, nil
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
	out := s.convertToMachine(ctx, machine)
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

	// Provisioned machines tear their workload down asynchronously: record the
	// DEPROVISIONING phase BEFORE the soft-delete (a deleted row would no
	// longer be picked up as a live update, but the reconnect replay does scan
	// deleted rows for deprovision jobs), then soft-delete, then push — the row
	// deletion is never blocked on the teardown (design §6.4).
	var provisioner *store.ProvisionerMessage
	if machine.ProvisionerID != 0 {
		provisioner, err = s.store.GetProvisioner(ctx, machine.ProvisionerID)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to get machine provisioner, error: %v", err))
		}
		if provisioner != nil && !provisioner.Deleted && machine.Provisioning != nil {
			next := cloneProvisioningStatus(machine.Provisioning)
			next.Phase = storepb.ProvisioningPhase_PROVISIONING_PHASE_DEPROVISIONING
			if err := s.store.UpdateMachineProvisioning(ctx, machine.ID, next); err != nil {
				return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to record deprovisioning, error: %v", err))
			}
		} else {
			// The provisioner is gone (deleted last): the workload is orphaned
			// in the cluster and visible to the cluster admin; nothing to push.
			provisioner = nil
		}
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

	// Push the teardown job best-effort: a provisioner that is offline misses
	// it, and the replay on its next connect completes the cleanup.
	if provisioner != nil && s.dispatcher != nil && s.dispatcher.IsProvisionerConnected(provisioner.ID) {
		if err := s.dispatcher.SendDeprovisionMachineJob(provisioner.ID, &v1pb.DeprovisionMachineJob{
			Machine:  common.FormatMachineUID(resourceID),
			KeepData: provisioner.Status.GetRetainData(),
		}); err != nil {
			slog.Warn("failed to push deprovision job; it will replay on reconnect",
				slog.String("machine", resourceID), log.WithError(err))
		}
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

// UpdateMachine renames a machine (title). Authorized for the machine's
// creator or a holder of laelia.machines.edit (workspace-scope), matching
// DeleteMachine. Used by the frontend confirm/rename step of the device-code
// create-machine flow.
func (s *MachineService) UpdateMachine(ctx context.Context, req *connect.Request[v1pb.UpdateMachineRequest]) (*connect.Response[v1pb.Machine], error) {
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
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("only the machine's creator or a workspace admin can update this machine"))
	}

	patch := &store.UpdateMachineMessage{}
	if req.Msg.Title != "" {
		patch.Name = &req.Msg.Title
	}
	if patch.Name == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("title must be set"))
	}
	updated, err := s.store.UpdateMachine(ctx, machine, patch)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to update machine, error: %v", err))
	}
	out := s.convertToMachine(ctx, updated)
	out.CanEdit = true // caller just proved edit authorization
	out.CanManage = true
	return connect.NewResponse(out), nil
}

// TransferMachineOwnership reassigns the machine to another user. The caller
// must be the machine's creator or a workspace admin; the transfer is
// unilateral and effective immediately. The machine keeps running and its
// tokens are NOT revoked — the new owner simply gains control (including the
// right to approve the machine's re-authentication). Mirrors
// TransferAgentOwnership.
func (s *MachineService) TransferMachineOwnership(ctx context.Context, req *connect.Request[v1pb.TransferMachineOwnershipRequest]) (*connect.Response[v1pb.TransferMachineOwnershipResponse], error) {
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
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("only the machine's creator or a workspace admin can transfer ownership"))
	}

	newOwnerHandle, err := common.GetUserHandle(req.Msg.NewOwner)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid new_owner %q: %v", req.Msg.NewOwner, err))
	}
	ownerHandle := resolveUserHandle(ctx, s.store, machine.CreatedBy)
	if newOwnerHandle == ownerHandle {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("new_owner is already the machine's owner"))
	}
	target, err := s.store.GetUserByHandle(ctx, newOwnerHandle)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to look up new owner, error: %v", err))
	}
	if target == nil || target.MemberDeleted {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("new owner user %s not found", req.Msg.NewOwner))
	}
	newOwnerID := target.ID

	updated, err := s.store.UpdateMachine(ctx, machine, &store.UpdateMachineMessage{CreatedBy: &newOwnerID})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to transfer machine ownership, error: %v", err))
	}
	out := s.convertToMachine(ctx, updated)
	out.CanEdit = true // caller just proved edit authorization
	out.CanManage = true
	return connect.NewResponse(&v1pb.TransferMachineOwnershipResponse{Machine: out}), nil
}

// RevokeMachineToken bumps the token_version and revokes every token + session
// without issuing a new token. The machine cannot reconnect until its owner
// re-runs `laelia-machine setup` on the host (device re-auth of the existing
// machine).

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
	// Provisioned machines are managed by a provisioner that recreates the
	// workload; force-disconnecting would leave it in a state the provisioner
	// does not reconcile. The provisioner owns the connection lifecycle.
	if machine.ProvisionerID != 0 {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("cannot force-disconnect a provisioned machine; the provisioner manages its connection"))
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

// ---- converters ----
