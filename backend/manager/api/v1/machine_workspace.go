package v1

import (
	"context"
	"time"

	"connectrpc.com/connect"
	"github.com/Ranxy/laelia/backend/agent/pi"
	"github.com/Ranxy/laelia/backend/common"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/manager/component/machinebuild"
	"github.com/Ranxy/laelia/backend/manager/store"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/emptypb"
)

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
	resp := &v1pb.ListMachineAgentsResponse{}
	for _, agent := range agents {
		summary := convertToAgentSummary(ctx, s.store, agent, agentReachable(s.dispatcher, agent.ID, agent.MachineID))
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

// RefreshMachineModels probes one provider's models on this machine using the
// given (draft) ACP config's custom_env, so the add-agent form's model picker
// reflects a custom env (e.g. CODEX_HOME) before the agent exists. The result
// is NOT persisted; it is returned to the caller for this session only. The
// probe runs on the machine's host, reached via the MachineChannel.
func (s *MachineService) RefreshMachineModels(ctx context.Context, req *connect.Request[v1pb.RefreshMachineModelsRequest]) (*connect.Response[v1pb.RefreshMachineModelsResponse], error) {
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
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("only the machine's creator or a workspace admin can refresh this machine's models"))
	}
	if s.dispatcher == nil || !s.dispatcher.IsMachineConnected(machine.ID) {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("machine is not connected; cannot probe models"))
	}
	cfg := req.Msg.AcpConfig
	if cfg == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("acp_config is required to refresh models"))
	}
	providerID := cfg.Provider
	if providerID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("acp_config.provider is required to refresh models"))
	}
	if !knownProviderID(providerID) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid acp_config.provider %q", providerID))
	}
	if providerID == "custom" || pi.IsPiProvider(providerID) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("provider %q does not support on-host model probing", providerID))
	}

	requestID := uuid.NewString()
	replyCh := s.dispatcher.RegisterPendingModels(requestID)
	defer s.dispatcher.CancelPendingModels(requestID)

	if err := s.dispatcher.SendDiscoverModelsToMachine(machine.ID, providerID, cfg.GetCustomEnv(), requestID); err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.Wrap(err, "failed to request model discovery"))
	}

	select {
	case msg := <-replyCh:
		if msg == nil {
			return nil, connect.NewError(connect.CodeInternal, errors.New("model discovery returned no result"))
		}
		if msg.Error != "" {
			return connect.NewResponse(&v1pb.RefreshMachineModelsResponse{
				Provider: providerID,
				Error:    msg.Error,
			}), nil
		}
		return connect.NewResponse(&v1pb.RefreshMachineModelsResponse{
			Provider: providerID,
			Models:   msg.Models,
		}), nil
	case <-time.After(60 * time.Second):
		return nil, connect.NewError(connect.CodeDeadlineExceeded, errors.New("timed out waiting for model discovery"))
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

// machineDownloadTarget maps the machine's reported os/arch to the build
// target name used by the embedded machine manifest (mirrors
// scripts/build-embedded-machines.sh's target matrix).
func machineDownloadTarget(osName, arch string) string {
	switch osName + "/" + arch {
	case "linux/amd64":
		return "linux-x64"
	case "windows/amd64":
		return "windows-x64"
	case "darwin/arm64":
		return "darwin-arm64"
	default:
		return osName + "-" + arch
	}
}

// UpgradeMachine pushes a self-upgrade command to an online machine. The
// machine's supervisor process downloads the new binary from this manager,
// verifies the manifest checksums, installs it, and restarts itself. The RPC
// returns as soon as the command is delivered; the frontend follows progress
// through Machine.upgrade_status.
func (s *MachineService) UpgradeMachine(ctx context.Context, req *connect.Request[v1pb.UpgradeMachineRequest]) (*connect.Response[emptypb.Empty], error) {
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
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("only the machine's creator or a workspace admin can upgrade this machine"))
	}
	if s.dispatcher == nil || !s.dispatcher.IsMachineConnected(machine.ID) {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("machine is not connected; cannot upgrade"))
	}

	latest := machinebuild.LatestVersion()
	current := machine.Info.GetVersion()
	if !machinebuild.UpgradeAvailable(current, latest) {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("machine is already up to date"))
	}
	target := machineDownloadTarget(machine.Info.GetOs(), machine.Info.GetArch())
	entry, ok := machinebuild.GetTarget(target)
	if !ok {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.Errorf("no embedded machine binary for target %q", target))
	}

	if err := s.dispatcher.SendUpgradeRequest(machine.ID, &v1pb.UpgradeRequest{
		Version: latest,
		Target:  target,
		Sha256:  entry.Gz.Sha256,
	}); err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.Wrap(err, "failed to send upgrade request"))
	}
	s.dispatcher.RecordMachineUpgrade(machine.ID, &v1pb.UpgradeProgress{Version: latest, Stage: "requested"})
	return connect.NewResponse(&emptypb.Empty{}), nil
}
