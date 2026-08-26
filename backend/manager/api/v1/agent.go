package v1

import (
	"context"
	"log/slog"
	"regexp"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Ranxy/laelia/backend/agent/executor"
	"github.com/Ranxy/laelia/backend/agent/pi"
	"github.com/Ranxy/laelia/backend/common"
	"github.com/Ranxy/laelia/backend/common/log"
	"github.com/Ranxy/laelia/backend/common/permission"
	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/laelia/backend/manager/component/dispatcher"
	"github.com/Ranxy/laelia/backend/manager/component/iam"
	"github.com/Ranxy/laelia/backend/manager/component/s3client"
	"github.com/Ranxy/laelia/backend/manager/component/state"
	"github.com/Ranxy/laelia/backend/manager/config"
	"github.com/Ranxy/laelia/backend/manager/store"
)

const (
	agentOfflineThresholdSeconds = 60
	accessTokenDuration          = 15 * time.Minute
	refreshTokenDuration         = 24 * time.Hour
	bootstrapTokenDuration       = 7 * 24 * time.Hour
	refreshTokenReuseWindow      = 30 * time.Second
	sessionIDLength              = 32
)

// envVarNameRegex matches a valid environment variable name.
var envVarNameRegex = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type AgentService struct {
	v1connect.UnimplementedAgentServiceHandler
	store          *store.Store
	secret         string
	profile        *config.Profile
	stateCfg       *state.State
	dispatcher     *dispatcher.Dispatcher
	iam            *iam.Manager
	s3client       *s3client.Client
	consumedTimers map[int]*time.Timer
	consumedMu     sync.Mutex
}

func NewAgentService(store *store.Store, secret string, profile *config.Profile, stateCfg *state.State, d *dispatcher.Dispatcher, iamManager *iam.Manager, s3clientManager *s3client.Client) *AgentService {
	return &AgentService{
		store:          store,
		secret:         secret,
		profile:        profile,
		stateCfg:       stateCfg,
		dispatcher:     d,
		iam:            iamManager,
		s3client:       s3clientManager,
		consumedTimers: make(map[int]*time.Timer),
	}
}

func (s *AgentService) CreateAgent(ctx context.Context, req *connect.Request[v1pb.CreateAgentRequest]) (*connect.Response[v1pb.CreateAgentResponse], error) {
	if req.Msg.Agent == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("agent must be set"))
	}
	if req.Msg.Agent.Title == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("agent title must be set"))
	}

	// An agent is bound to exactly one machine (agent.machine_id NOT NULL): the
	// machine app hosts the agent's drain loop, so there is no per-agent process
	// or token. CreateAgent therefore requires a machine parent and pushes an
	// AgentAssignment to the owning machine's MachineChannel so the machine app
	// opens an AgentChannel for the new agent immediately. If the machine is
	// offline the push is best-effort (logged, not queued): the next
	// ConnectMachine resyncs the full roster from the DB.
	if req.Msg.Agent.Machine == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("agent.machine (parent machine) must be set"))
	}
	machineResourceID, err := common.GetMachineResourceID(req.Msg.Agent.Machine)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	machine, err := s.store.GetMachineByResourceID(ctx, machineResourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to get parent machine, error: %v", err))
	}
	if machine == nil || machine.Deleted {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("parent machine %s not found", machineResourceID))
	}

	// CreateAgent is handler-gated (the proto carries no permission annotation):
	// the machine's creator, a workspace admin, or a principal bound to
	// roles/machineAgentCreator on the machine's IAM policy may create agents on
	// it. The creator becomes the agent's owner.
	user, _ := GetUserFromContext(ctx)
	if user == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
	}
	canCreate, err := canCreateAgentOnMachine(ctx, s.iam, user, machine)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to check agent creation permission"))
	}
	if !canCreate {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("you are not allowed to create an agent on this machine"))
	}
	creatorID := user.ID
	ownerID := creatorID

	// ACP config is admin-owned. CreateAgent may carry an initial acp_config
	// (provider/model/persona/env) so an agent can be fully configured at
	// creation time instead of requiring a second visit to the agent profile.
	// When provided, validate it against the parent machine's discovered
	// providers (provider must be runnable on the host; model is required when
	// the provider exposes a model config option) and derive the capability from
	// it. When absent, fall back to the minimal default (allow_env only) and let
	// the admin configure the agent later.
	var storedAcpConfig *storepb.AgentACPConfig
	var capability *v1pb.AgentCapability
	if reqACP := req.Msg.Agent.GetInfo().GetAcpConfig(); reqACP != nil && !isEmptyAgentACPConfig(reqACP) {
		if err := validateAgentACPConfig(reqACP, s.machineAvailableProviders(ctx, machine.ID)); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		if reqACP.GetGlobalProvider() != "" {
			if err := s.validateGlobalProviderReference(ctx, user, reqACP); err != nil {
				return nil, err
			}
		}
		// Legacy inline api_provider/api_key is gated by the workspace toggle (or
		// agents.edit). When the toggle is off, only admins may self-provide a key.
		if reqACP.GetGlobalProvider() == "" && reqACP.Provider == pi.BuiltinPiProvider {
			if !s.canUseInlineAPIKeyAtCreate(ctx, user) {
				return nil, connect.NewError(connect.CodePermissionDenied, errors.New("self-provided api keys are disabled; use a global provider"))
			}
		}
		storedAcpConfig = convertToStoreAgentACPConfig(reqACP)
		// Inherit the default allow_env set when the caller left it empty so the
		// child process still receives the baseline passthrough env.
		if len(storedAcpConfig.AllowEnv) == 0 {
			storedAcpConfig.AllowEnv = executor.DefaultAllowEnv
		}
		capability = buildCapabilityForACPConfig(reqACP)
	} else {
		storedAcpConfig = &storepb.AgentACPConfig{AllowEnv: executor.DefaultAllowEnv}
	}

	agentMessage := &store.AgentMessage{
		Name:              req.Msg.Agent.Title,
		Description:       req.Msg.Agent.GetDescription(),
		TokenVersion:      1,
		MachineID:         machine.ID,
		AllowAddToChannel: req.Msg.Agent.GetAllowAddToChannel(),
		Info: &storepb.AgentInfo{
			Labels:     req.Msg.Agent.Labels,
			AcpConfig:  storedAcpConfig,
			Capability: convertToStoreAgentCapability(capability),
		},
		Status:    &storepb.AgentStatus{},
		CreatedBy: creatorID,
		OwnerID:   ownerID,
	}

	created, err := s.store.CreateAgent(ctx, agentMessage)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to create agent, error: %v", err))
	}
	created.MachineResourceID = machine.ResourceID

	// Best-effort: tell the machine app to host the new agent now. A missed push
	// (machine offline, send race) is recovered on the next ConnectMachine
	// resync, so a send failure is logged, not returned.
	if s.dispatcher != nil {
		assignmentAcp, resolveErr := resolveAcpConfigForDaemon(ctx, s.store, convertToV1AgentACPConfig(created.Info.GetAcpConfig()))
		if resolveErr != nil {
			slog.Info("failed to resolve acp config for agent assignment", "agent", created.ResourceID, log.WithError(resolveErr))
		}
		assignment := &v1pb.AgentAssignment{
			AgentName:        common.FormatAgentUID(created.ResourceID),
			AgentDisplayName: created.Name,
			AcpConfig:        assignmentAcp,
		}
		if pushErr := s.dispatcher.SendAgentAssignment(machine.ID, assignment); pushErr != nil {
			slog.Info("best-effort agent assignment push skipped", "agent", created.ResourceID, "machine", machine.ResourceID, "error", pushErr)
		}
	}

	response := &v1pb.CreateAgentResponse{
		Agent: s.convertToAgent(ctx, created, agentReachable(s.dispatcher, created.ID, created.MachineID)),
	}
	return connect.NewResponse(response), nil
}

func (s *AgentService) ListAgents(ctx context.Context, req *connect.Request[v1pb.ListAgentsRequest]) (*connect.Response[v1pb.ListAgentsResponse], error) {
	offset, err := parseLimitAndOffset(&pageSize{
		token:   req.Msg.PageToken,
		limit:   int(req.Msg.PageSize),
		maximum: 1000,
	})
	if err != nil {
		return nil, err
	}
	limitPlusOne := offset.limit + 1

	find := &store.FindAgentMessage{
		Limit:       &limitPlusOne,
		Offset:      &offset.offset,
		ShowDeleted: req.Msg.ShowDeleted,
	}

	agents, err := s.store.ListAgents(ctx, find)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to list agents, error: %v", err))
	}

	nextPageToken := ""
	if len(agents) == limitPlusOne {
		agents = agents[:offset.limit]
		if nextPageToken, err = offset.getNextPageToken(); err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to marshal next page token, error: %v", err))
		}
	}

	response := &v1pb.ListAgentsResponse{
		NextPageToken: nextPageToken,
	}
	// ListAgents returns a summary view (AgentSummary): identity, state,
	// connection status, and the provider/executable lifecycle signal only. The
	// full Agent — available_providers, the rest of acp_config, capability, host
	// info, token fields, created_by, and can_edit — is returned by GetAgent, so
	// the two RPCs don't overlap. can_edit is omitted here in particular because
	// resolving it per row would N+1 the IAM policy lookup for non-admin
	// callers. can_delete is the cheap subset the list gates its delete
	// affordance on: one workspace-scope agents.edit lookup for the page plus
	// the per-row owner comparison (per-agent policy bindings are not
	// consulted, so a custom role bound on the agent may still delete
	// server-side while the list hides the button).
	for _, agent := range agents {
		summary := convertToAgentSummary(ctx, s.store, agent, agentReachable(s.dispatcher, agent.ID, agent.MachineID))
		response.Agents = append(response.Agents, summary)
	}
	return connect.NewResponse(response), nil
}

func (s *AgentService) GetAgent(ctx context.Context, req *connect.Request[v1pb.GetAgentRequest]) (*connect.Response[v1pb.Agent], error) {
	resourceID, err := common.GetAgentResourceID(req.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	agent, err := s.store.GetAgentByResourceID(ctx, resourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to get agent, error: %v", err))
	}
	if agent == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("agent %s not found", resourceID))
	}
	out := s.convertToAgent(ctx, agent, agentReachable(s.dispatcher, agent.ID, agent.MachineID))
	caller, _ := GetUserFromContext(ctx)
	out.CanEdit = s.canEditAgent(ctx, caller, agent)
	// persona_prompt is the agent's private self prompt: it defines the agent to
	// itself and is only visible to the agent's owner or a workspace admin.
	// Everyone else (including other agents) sees only the public description.
	if !out.CanEdit && out.Info != nil && out.Info.AcpConfig != nil {
		out.Info.AcpConfig.PersonaPrompt = ""
	}
	// The builtin-pi api_key is a plaintext secret. Admins see the full key;
	// the owner sees a masked preview when the workspace toggle enables
	// self-provided keys; everyone else sees nothing. Global-provider agents
	// never carry an inline key.
	if out.GetInfo().GetAcpConfig().GetProvider() == pi.BuiltinPiProvider && out.Info.AcpConfig != nil && out.Info.AcpConfig.ApiKey != "" {
		if view, masked := s.canViewInlineKey(ctx, caller, agent); view && masked {
			out.Info.AcpConfig.ApiKey = maskKeyPreview(out.Info.AcpConfig.ApiKey)
		} else if !view {
			out.Info.AcpConfig.ApiKey = ""
		}
	}
	mcpServers, err := s.store.ListAgentMcpServers(ctx, agent.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to list agent mcp servers"))
	}
	for _, m := range mcpServers {
		out.McpServers = append(out.McpServers, common.FormatMcpServerUID(m.ServerResourceID))
	}
	return connect.NewResponse(out), nil
}

// UpdateAgent patches one or more mutable agent fields (allow_add_to_channel,
// follow_owner_permissions, can_manage_channel_members, description); any other
// update_mask path is rejected. The IAM interceptor skips this RPC (no
// permission annotation — agents.edit is admin-only), so authorization is
// enforced here for the agent's owner or a workspace admin.
func (s *AgentService) UpdateAgent(ctx context.Context, req *connect.Request[v1pb.UpdateAgentRequest]) (*connect.Response[v1pb.Agent], error) {
	if req.Msg.Agent == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("agent must be set"))
	}
	resourceID, err := common.GetAgentResourceID(req.Msg.Agent.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	// allow_add_to_channel, follow_owner_permissions, can_manage_channel_members,
	// and description are the supported mutable fields; reject any other path.
	// Empty mask defaults to allow_add_to_channel (the original sole field).
	paths := req.Msg.UpdateMask.GetPaths()
	if len(paths) == 0 {
		paths = []string{"allow_add_to_channel"}
	}
	patch := &store.UpdateAgentMessage{}
	for _, p := range paths {
		switch p {
		case "allow_add_to_channel":
			allowAdd := req.Msg.Agent.GetAllowAddToChannel()
			patch.AllowAddToChannel = &allowAdd
		case "follow_owner_permissions":
			follow := req.Msg.Agent.GetFollowOwnerPermissions()
			patch.FollowOwnerPermissions = &follow
		case "can_manage_channel_members":
			canManage := req.Msg.Agent.GetCanManageChannelMembers()
			patch.CanManageChannelMembers = &canManage
		case "description":
			desc := req.Msg.Agent.GetDescription()
			patch.Description = &desc
		default:
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("update_mask path %q is not supported; only allow_add_to_channel, follow_owner_permissions, can_manage_channel_members, and description", p))
		}
	}

	agent, err := s.store.GetAgentByResourceID(ctx, resourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to get agent, error: %v", err))
	}
	if agent == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("agent %s not found", resourceID))
	}

	user, _ := GetUserFromContext(ctx)
	if !s.canEditAgent(ctx, user, agent) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("only the agent's owner or a workspace admin can modify this agent"))
	}

	updated, err := s.store.UpdateAgent(ctx, agent, patch)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to update agent, error: %v", err))
	}

	out := s.convertToAgent(ctx, updated, agentReachable(s.dispatcher, updated.ID, updated.MachineID))
	out.CanEdit = true // caller just proved edit authorization
	return connect.NewResponse(out), nil
}

// TransferAgentOwnership reassigns the agent's owner to another user. The
// caller must be the agent's current owner or a workspace admin (canEditAgent);
// the transfer is unilateral and effective immediately — the target user does
// not accept, and the previous owner loses owner authority at once. The change
// reaches the agent's prompt on its next drain session (BeginSession re-sends
// the owner display name) and the runner force-re-anchors on a warm turn.
func (s *AgentService) TransferAgentOwnership(ctx context.Context, req *connect.Request[v1pb.TransferAgentOwnershipRequest]) (*connect.Response[v1pb.TransferAgentOwnershipResponse], error) {
	resourceID, err := common.GetAgentResourceID(req.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	agent, err := s.store.GetAgentByResourceID(ctx, resourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to get agent, error: %v", err))
	}
	if agent == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("agent %s not found", resourceID))
	}

	user, _ := GetUserFromContext(ctx)
	if !s.canEditAgent(ctx, user, agent) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("only the agent's owner or a workspace admin can transfer ownership"))
	}

	newOwnerHandle, err := common.GetUserHandle(req.Msg.NewOwner)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid new_owner %q: %v", req.Msg.NewOwner, err))
	}
	ownerHandle := resolveUserHandle(ctx, s.store, agent.OwnerID)
	if newOwnerHandle == ownerHandle {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("new_owner is already the agent's owner"))
	}
	target, err := s.store.GetUserByHandle(ctx, newOwnerHandle)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to look up new owner, error: %v", err))
	}
	if target == nil || target.MemberDeleted {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("new owner user %s not found", req.Msg.NewOwner))
	}
	newOwnerID := target.ID

	updated, err := s.store.UpdateAgent(ctx, agent, &store.UpdateAgentMessage{OwnerID: &newOwnerID})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to transfer agent ownership, error: %v", err))
	}

	// Push a prompt release notice so a running agent re-anchors with the new
	// owner on the current or next turn.
	if s.dispatcher != nil {
		if pushErr := s.dispatcher.PushPromptReleaseNotice(ctx, updated.ID); pushErr != nil {
			slog.Info("best-effort prompt release notice push skipped after ownership transfer", "agent", updated.ResourceID, "error", pushErr)
		}
	}

	out := s.convertToAgent(ctx, updated, agentReachable(s.dispatcher, updated.ID, updated.MachineID))
	out.CanEdit = true // caller just proved edit authorization
	return connect.NewResponse(&v1pb.TransferAgentOwnershipResponse{Agent: out}), nil
}

// isAgentOwner reports whether the caller owns the agent.
func isAgentOwner(user *store.UserMessage, agent *store.AgentMessage) bool {
	return user != nil && agent.OwnerID != 0 && agent.OwnerID == user.ID
}

// canEditAgent reports whether the caller may modify the agent: its owner, or a
// holder of laelia.agents.edit on the agent. With the per-agent editor role
// removed, agents.edit is granted only by the workspaceAdmin role (via the
// all-permissions union) and by any custom role bound on the agent's IAM policy
// that includes agents.edit. A lookup failure is treated as not-editable
// (fail-closed) so a stale can_edit never grants modification. Agent-daemon
// callers and unauthenticated requests get false.
func (s *AgentService) canEditAgent(ctx context.Context, user *store.UserMessage, agent *store.AgentMessage) bool {
	if user == nil || s.iam == nil {
		return false
	}
	// The owner may modify their own agent.
	if isAgentOwner(user, agent) {
		return true
	}
	agentName := common.FormatAgentUID(agent.ResourceID)
	ok, err := s.iam.CheckPermission(ctx, permission.AgentsEdit, user, nil, &iam.ResourceRef{
		ResourceType: storepb.Policy_AGENT,
		Name:         agentName,
	})
	if err != nil {
		slog.Error("failed to resolve agents.edit", slog.String("agent", agentName), slog.Any("err", err))
		return false
	}
	return ok
}

// canDeleteAgentWorkspace reports whether the caller holds laelia.agents.edit
// at workspace scope (workspaceAdmin or a custom workspace role). Per-agent
// policy bindings are not consulted: the list views use this cheap lookup once
// per page plus the per-row owner comparison to populate can_delete, so a page
// of N agents costs one IAM lookup, not N+1. A lookup failure is fail-closed.
func canDeleteAgentWorkspace(ctx context.Context, im *iam.Manager, user *store.UserMessage) bool {
	if user == nil || im == nil {
		return false
	}
	ok, err := im.CheckPermission(ctx, permission.AgentsEdit, user, nil, nil)
	if err != nil {
		return false
	}
	return ok
}

// resolveEditableAgent loads a non-deleted agent by its resource name and
// checks the caller may edit it (owner or laelia.agents.edit). Used by
// DeleteAgent, StopAgent, and StartAgent.
func (s *AgentService) resolveEditableAgent(ctx context.Context, name string, verb string) (*store.AgentMessage, error) {
	resourceID, err := common.GetAgentResourceID(name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	agent, err := s.store.GetAgentByResourceID(ctx, resourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to get agent, error: %v", err))
	}
	if agent == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("agent %s not found", resourceID))
	}
	if agent.Deleted {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("agent %s has been deleted", resourceID))
	}
	user, _ := GetUserFromContext(ctx)
	if !s.canEditAgent(ctx, user, agent) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.Errorf("only the agent's owner or a holder of laelia.agents.edit can %s this agent", verb))
	}
	return agent, nil
}

func (s *AgentService) DeleteAgent(ctx context.Context, req *connect.Request[v1pb.DeleteAgentRequest]) (*connect.Response[emptypb.Empty], error) {
	agent, err := s.resolveEditableAgent(ctx, req.Msg.Name, "delete")
	if err != nil {
		return nil, err
	}
	if err := s.store.DeleteAgent(ctx, agent.ResourceID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to delete agent, error: %v", err))
	}

	// Best-effort: tear down the deleted agent's runner and remove its
	// workspace on the machine. A missed push is harmless — the agent row is
	// soft-deleted and won't appear in the next ConnectMachine resync, so the
	// runner is simply not restarted.
	if s.dispatcher != nil && agent.MachineID > 0 {
		uid := common.FormatAgentUID(agent.ResourceID)
		if pushErr := s.dispatcher.SendRemoveAgent(agent.MachineID, uid); pushErr != nil {
			slog.Info("best-effort remove-agent push skipped", "agent", agent.ResourceID, "machineID", agent.MachineID, "error", pushErr)
		}
		if pushErr := s.dispatcher.SendDeleteAgentWorkspace(agent.MachineID, uid); pushErr != nil {
			slog.Info("best-effort delete-agent-workspace push skipped", "agent", agent.ResourceID, "machineID", agent.MachineID, "error", pushErr)
		}
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

// StopAgent stops an agent: its machine runner is torn down and it processes
// no session messages until StartAgent. The agent row is preserved.
func (s *AgentService) StopAgent(ctx context.Context, req *connect.Request[v1pb.StopAgentRequest]) (*connect.Response[emptypb.Empty], error) {
	agent, err := s.resolveEditableAgent(ctx, req.Msg.Name, "stop")
	if err != nil {
		return nil, err
	}
	if _, err := s.store.StopAgent(ctx, agent.ResourceID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to stop agent, error: %v", err))
	}

	// Best-effort: tear down the runner on the machine. A missed push is
	// harmless — the agent is excluded from assigned_agents on the next resync,
	// so the runner is simply not restarted.
	if s.dispatcher != nil && agent.MachineID > 0 {
		if pushErr := s.dispatcher.SendRemoveAgent(agent.MachineID, common.FormatAgentUID(agent.ResourceID)); pushErr != nil {
			slog.Info("best-effort stop-agent push skipped", "agent", agent.ResourceID, "machineID", agent.MachineID, "error", pushErr)
		}
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

// StartAgent resumes a stopped agent: its machine runner is re-spawned and it
// resumes processing session messages. No-op (still succeeds) when already
// enabled.
func (s *AgentService) StartAgent(ctx context.Context, req *connect.Request[v1pb.StartAgentRequest]) (*connect.Response[emptypb.Empty], error) {
	agent, err := s.resolveEditableAgent(ctx, req.Msg.Name, "start")
	if err != nil {
		return nil, err
	}
	if _, err := s.store.StartAgent(ctx, agent.ResourceID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to start agent, error: %v", err))
	}

	// Best-effort: tell the machine app to host the agent again. A missed push
	// is recovered on the next ConnectMachine resync.
	if s.dispatcher != nil && agent.MachineID > 0 {
		assignmentAcp, resolveErr := resolveAcpConfigForDaemon(ctx, s.store, convertToV1AgentACPConfig(agent.Info.GetAcpConfig()))
		if resolveErr != nil {
			slog.Info("failed to resolve acp config for agent assignment", "agent", agent.ResourceID, log.WithError(resolveErr))
		}
		assignment := &v1pb.AgentAssignment{
			AgentName:        common.FormatAgentUID(agent.ResourceID),
			AgentDisplayName: agent.Name,
			AcpConfig:        assignmentAcp,
		}
		if pushErr := s.dispatcher.SendAgentAssignment(agent.MachineID, assignment); pushErr != nil {
			slog.Info("best-effort start-agent assignment push skipped", "agent", agent.ResourceID, "machineID", agent.MachineID, "error", pushErr)
		}
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}
