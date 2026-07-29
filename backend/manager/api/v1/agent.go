package v1

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/laelia/backend/agent/executor"
	"github.com/Ranxy/laelia/backend/agent/pi"
	"github.com/Ranxy/laelia/backend/agent/provider"
	"github.com/Ranxy/laelia/backend/common"
	"github.com/Ranxy/laelia/backend/common/permission"
	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/laelia/backend/manager/api/auth"
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

	// Record the creator for display (Agent.created_by). CreateAgent is
	// admin-tier (only workspaceAdmin holds laelia.agents.create), so a user is
	// always present; guard defensively against a missing one. Editing the agent
	// is authorized by the workspaceAdmin role (which holds agents.edit via the
	// all-permissions union), not by a per-agent binding.
	creatorID := 0
	if user, _ := GetUserFromContext(ctx); user != nil {
		creatorID = user.ID
	}

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
		Name:         req.Msg.Agent.Title,
		TokenVersion: 1,
		MachineID:    machine.ID,
		Info: &storepb.AgentInfo{
			Labels:     req.Msg.Agent.Labels,
			AcpConfig:  storedAcpConfig,
			Capability: convertToStoreAgentCapability(capability),
		},
		Status:    &storepb.AgentStatus{},
		CreatedBy: creatorID,
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
		assignment := &v1pb.AgentAssignment{
			AgentName:        common.FormatAgentUID(created.ResourceID),
			AgentDisplayName: created.Name,
			AcpConfig:        convertToV1AgentACPConfig(created.Info.GetAcpConfig()),
		}
		if pushErr := s.dispatcher.SendAgentAssignment(machine.ID, assignment); pushErr != nil {
			slog.Info("best-effort agent assignment push skipped", "agent", created.ResourceID, "machine", machine.ResourceID, "error", pushErr)
		}
	}

	response := &v1pb.CreateAgentResponse{
		Agent: convertToAgent(created, agentReachable(s.dispatcher, created.ID, created.MachineID)),
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
	// callers, and the list view does not gate affordances on it (delete is
	// enforced server-side via agents.edit).
	for _, agent := range agents {
		response.Agents = append(response.Agents, convertToAgentSummary(agent, agentReachable(s.dispatcher, agent.ID, agent.MachineID)))
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
	out := convertToAgent(agent, agentReachable(s.dispatcher, agent.ID, agent.MachineID))
	caller, _ := GetUserFromContext(ctx)
	out.CanEdit = s.canEditAgent(ctx, caller, out.Name)
	// The builtin-pi api_key is a plaintext secret. Redact it for callers who
	// cannot edit this agent; editors (workspaceAdmin / agents.edit) still see
	// it so they can populate the password field on the config form.
	if !out.CanEdit && out.GetInfo().GetAcpConfig().GetProvider() == pi.BuiltinPiProvider {
		if out.Info.AcpConfig != nil {
			out.Info.AcpConfig.ApiKey = ""
		}
	}
	return connect.NewResponse(out), nil
}

// canEditAgent reports whether the caller holds laelia.agents.edit on the
// agent. With the per-agent editor role removed, agents.edit is granted only by
// the workspaceAdmin role (via the all-permissions union) and by any custom
// role bound on the agent's IAM policy that includes agents.edit. A lookup
// failure is treated as not-editable (fail-closed) so a stale can_edit never
// grants modification. Agent-daemon callers and unauthenticated requests get
// false.
func (s *AgentService) canEditAgent(ctx context.Context, user *store.UserMessage, agentName string) bool {
	if user == nil || s.iam == nil {
		return false
	}
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

func (s *AgentService) DeleteAgent(ctx context.Context, req *connect.Request[v1pb.DeleteAgentRequest]) (*connect.Response[emptypb.Empty], error) {
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
	if err := s.store.DeleteAgent(ctx, resourceID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to delete agent, error: %v", err))
	}

	// Best-effort: tell the machine app to tear down the deleted agent's runner.
	// A missed push is harmless — the agent row is soft-deleted and won't appear
	// in the next ConnectMachine resync, so the runner is simply not restarted.
	if s.dispatcher != nil && agent.MachineID > 0 {
		if pushErr := s.dispatcher.SendRemoveAgent(agent.MachineID, common.FormatAgentUID(agent.ResourceID)); pushErr != nil {
			slog.Info("best-effort remove-agent push skipped", "agent", agent.ResourceID, "machineID", agent.MachineID, "error", pushErr)
		}
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (s *AgentService) RotateAgentToken(ctx context.Context, req *connect.Request[v1pb.RotateAgentTokenRequest]) (*connect.Response[v1pb.RotateAgentTokenResponse], error) {
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

	newTokenVersion := agent.TokenVersion + 1
	newTokenFamily := fmt.Sprintf("%s:v%d", agent.ResourceID, newTokenVersion)

	bootstrapToken, err := auth.GenerateAgentTokenWithFamily(agent.Name, agent.ResourceID, newTokenVersion, auth.TokenTypeBootstrap, newTokenFamily, s.profile.Mode, s.secret, bootstrapTokenDuration)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate agent token, error: %v", err))
	}

	nowRotated := time.Now()
	if _, err := s.store.UpdateAgent(ctx, agent, &store.UpdateAgentMessage{
		TokenVersion:       &newTokenVersion,
		LastTokenRotatedAt: &nowRotated,
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to update agent token version, error: %v", err))
	}

	if err := s.store.RevokeAllAgentTokens(ctx, agent.ID); err != nil {
		// Abort the rotation: leaving old tokens live while minting a new
		// bootstrap token means a previously-issued (possibly leaked) refresh
		// token would keep working under the old token_version until it
		// expired. Failing closed forces the admin to retry and keeps the
		// "rotation revokes everything" invariant intact. The version bump
		// above is contained: old refresh tokens embed the prior version and
		// RefreshAgentToken rejects version mismatches.
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to revoke old tokens during rotation, error: %v", err))
	}

	tokenHash := hashToken(bootstrapToken)
	if err := s.store.CreateAgentToken(ctx, &store.AgentTokenMessage{
		AgentID:     agent.ID,
		TokenHash:   tokenHash,
		TokenType:   storepb.AgentTokenType_BOOTSTRAP,
		TokenFamily: newTokenFamily,
		State:       storepb.AgentTokenState_ACTIVE,
		ExpiresAt:   time.Now().Add(bootstrapTokenDuration),
		CreatedBy:   "system",
	}); err != nil {
		slog.Error("failed to store new token after rotation — agent has no valid bootstrap token", "agent", resourceID, "error", err)
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to store new agent token, error: %v", err))
	}

	terminateReason := "token_rotated"
	if req.Msg.Reason != "" {
		terminateReason = req.Msg.Reason
	}
	if err := s.store.TerminateAllAgentSessions(ctx, agent.ID, terminateReason); err != nil {
		slog.Warn("failed to terminate agent sessions after rotation", "agent", resourceID, "error", err)
	}

	return connect.NewResponse(&v1pb.RotateAgentTokenResponse{
		BootstrapToken: bootstrapToken,
	}), nil
}

func (s *AgentService) RevokeAgentToken(ctx context.Context, req *connect.Request[v1pb.RevokeAgentTokenRequest]) (*connect.Response[v1pb.RevokeAgentTokenResponse], error) {
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

	newTokenVersion := agent.TokenVersion + 1
	nowRotated := time.Now()
	if _, err := s.store.UpdateAgent(ctx, agent, &store.UpdateAgentMessage{
		TokenVersion:       &newTokenVersion,
		LastTokenRotatedAt: &nowRotated,
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to update agent token version, error: %v", err))
	}

	if err := s.store.RevokeAllAgentTokens(ctx, agent.ID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to revoke agent tokens, error: %v", err))
	}

	terminateReason := "token_revoked"
	if req.Msg.Reason != "" {
		terminateReason = req.Msg.Reason
	}
	if err := s.store.TerminateAllAgentSessions(ctx, agent.ID, terminateReason); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to terminate agent sessions, error: %v", err))
	}

	return connect.NewResponse(&v1pb.RevokeAgentTokenResponse{}), nil
}

func (s *AgentService) ForceDisconnectAgent(ctx context.Context, req *connect.Request[v1pb.ForceDisconnectAgentRequest]) (*connect.Response[emptypb.Empty], error) {
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

	reason := "admin_forced"
	if req.Msg.Reason != "" {
		reason = req.Msg.Reason
	}
	if err := s.store.TerminateAllAgentSessions(ctx, agent.ID, reason); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to terminate agent sessions, error: %v", err))
	}

	patch := &store.UpdateAgentMessage{
		Status: &storepb.AgentStatus{
			State: storepb.AgentStatus_OFFLINE,
		},
	}
	if _, err := s.store.UpdateAgent(ctx, agent, patch); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to update agent status, error: %v", err))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (s *AgentService) ListAgentSessions(ctx context.Context, req *connect.Request[v1pb.ListAgentSessionsRequest]) (*connect.Response[v1pb.ListAgentSessionsResponse], error) {
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

	sessions, err := s.store.ListAgentSessions(ctx, agent.ID, req.Msg.IncludeTerminated)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to list agent sessions, error: %v", err))
	}

	response := &v1pb.ListAgentSessionsResponse{}
	for _, session := range sessions {
		response.Sessions = append(response.Sessions, convertToV1Session(session))
	}
	return connect.NewResponse(response), nil
}

func (s *AgentService) ConnectAgent(ctx context.Context, req *connect.Request[v1pb.ConnectAgentRequest]) (*connect.Response[v1pb.ConnectAgentResponse], error) {
	agent, ok := GetAgentFromContext(ctx)
	tokenFamily := ""
	bootstrapTokenID := 0
	if !ok || agent == nil {
		if req.Msg.BootstrapToken == "" {
			return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("agent not authenticated and no bootstrap token provided"))
		}
		authResult, err := s.authenticateBootstrapToken(req.Msg.BootstrapToken)
		if err != nil {
			return nil, err
		}
		agent = authResult.agent
		tokenFamily = authResult.tokenFamily
		bootstrapTokenID = authResult.tokenID
	}
	if tokenFamily == "" {
		tokenFamily = agent.ResourceID
	}

	sessionID := generateRandomString(sessionIDLength)
	nonce := s.stateCfg.NonceManager.GenerateNonce(agent.ResourceID, sessionID)

	now := time.Now()
	nowSec := now.Unix()

	// ACP config is owned by the server (set by the admin via
	// UpdateAgentACPConfig). Always echo it back to the agent and derive the
	// capability from it, regardless of what the agent reports.
	var storedAcpConfig *storepb.AgentACPConfig
	if agent.Info != nil {
		storedAcpConfig = agent.Info.GetAcpConfig()
	}

	patch := &store.UpdateAgentMessage{
		Status: &storepb.AgentStatus{
			State:           storepb.AgentStatus_ONLINE,
			ConnectedAt:     nowSec,
			LastHeartbeatAt: nowSec,
			ActiveSessionId: sessionID,
		},
	}
	if req.Msg.Info != nil {
		patch.Info = convertToStoreAgentInfo(req.Msg.Info)
	} else {
		patch.Info = &storepb.AgentInfo{}
	}
	patch.Info.Capability = convertToStoreAgentCapability(buildCapabilityForACPConfig(convertToV1AgentACPConfig(storedAcpConfig)))
	patch.Info.AcpConfig = storedAcpConfig

	updated, err := s.store.UpdateAgent(ctx, agent, patch)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to update agent on connect, error: %v", err))
	}

	if err := s.store.TerminateAllAgentSessions(ctx, agent.ID, "replaced"); err != nil {
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

	if err := s.store.CreateAgentSession(ctx, &store.AgentSessionMessage{
		SessionID:    sessionID,
		AgentID:      agent.ID,
		TokenFamily:  tokenFamily,
		State:        "ACTIVE",
		SourceIP:     sourceIP,
		Fingerprint:  req.Msg.Fingerprint,
		AgentVersion: "",
		ConnectedAt:  now,
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to create agent session, error: %v", err))
	}

	// Mint the agent's initial access + refresh tokens only on the bootstrap
	// (first connect) path. On a reconnect the agent authenticated via an
	// access token it already holds from RefreshAgentToken, which also minted
	// and persisted the current refresh token — so minting another refresh
	// token here is redundant and was the source of both the
	// idx_agent_token_hash collision (a second identical-token insert in the
	// same second) and the unbounded growth of the refresh-token table.
	accessToken := ""
	refreshToken := ""
	accessTokenExpiresAt := time.Time{}
	if bootstrapTokenID != 0 {
		accessToken, err = auth.GenerateAgentTokenWithSession(updated.Name, updated.ResourceID, updated.TokenVersion, auth.TokenTypeAccess, sessionID, s.profile.Mode, s.secret, accessTokenDuration)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate access token, error: %v", err))
		}

		refreshToken, err = auth.GenerateAgentTokenWithSession(updated.Name, updated.ResourceID, updated.TokenVersion, auth.TokenTypeRefresh, "", s.profile.Mode, s.secret, refreshTokenDuration)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate refresh token, error: %v", err))
		}

		refreshTokenHash := hashToken(refreshToken)
		if err := s.store.CreateAgentToken(ctx, &store.AgentTokenMessage{
			AgentID:     agent.ID,
			TokenHash:   refreshTokenHash,
			TokenType:   storepb.AgentTokenType_REFRESH,
			TokenFamily: tokenFamily,
			State:       storepb.AgentTokenState_ACTIVE,
			Fingerprint: req.Msg.Fingerprint,
			ExpiresAt:   time.Now().Add(refreshTokenDuration),
		}); err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to store refresh token, error: %v", err))
		}

		accessTokenExpiresAt = time.Now().Add(accessTokenDuration)

		// The bootstrap token is single-use: once the connection has fully
		// succeeded (agent updated, old sessions terminated, new session + tokens
		// persisted), mark it CONSUMED so a leaked bootstrap token cannot be
		// replayed within its validity window to kick the legitimate agent off.
		consumedAt := time.Now()
		if err := s.store.UpdateAgentTokenState(ctx, bootstrapTokenID, storepb.AgentTokenState_CONSUMED, &consumedAt); err != nil {
			slog.Warn("failed to consume bootstrap token after connect", "agent", agent.ResourceID, "error", err)
		}
	}

	resp := &v1pb.ConnectAgentResponse{
		SessionId:     sessionID,
		NextNonce:     nonce,
		InitialStatus: convertToV1AgentStatus(updated.Status, updated.Deleted, true),
		AcpConfig:     convertToV1AgentACPConfig(storedAcpConfig),
	}
	if accessToken != "" {
		resp.AccessToken = accessToken
		resp.RefreshToken = refreshToken
		resp.AccessTokenExpiresAt = timestamppb.New(accessTokenExpiresAt)
	}
	return connect.NewResponse(resp), nil
}

func (s *AgentService) AgentHeartbeat(ctx context.Context, req *connect.Request[v1pb.AgentHeartbeatRequest]) (*connect.Response[v1pb.AgentHeartbeatResponse], error) {
	agent, ok := GetAgentFromContext(ctx)
	if !ok || agent == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("agent not authenticated"))
	}

	if req.Msg.SessionId != "" {
		session, err := s.store.GetAgentSession(ctx, req.Msg.SessionId)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to get agent session, error: %v", err))
		}
		if session == nil {
			return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("session not found"))
		}
		if session.State == "KICKED" {
			return nil, connect.NewError(connect.CodePermissionDenied, errors.New("session has been replaced by a new connection"))
		}
		if session.AgentID != agent.ID {
			return nil, connect.NewError(connect.CodePermissionDenied, errors.New("session does not belong to this agent"))
		}

		if !s.stateCfg.NonceManager.VerifyNonce(req.Msg.PreviousNonce, agent.ResourceID, req.Msg.SessionId) {
			if req.Msg.PreviousNonce != "" {
				return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("invalid nonce"))
			}
		}

		if err := s.store.TouchAgentSession(ctx, req.Msg.SessionId); err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to touch agent session, error: %v", err))
		}
	}

	nonce := s.stateCfg.NonceManager.GenerateNonce(agent.ResourceID, req.Msg.SessionId)

	nowSec := time.Now().Unix()
	activeSessionID := agent.Status.GetActiveSessionId()
	patch := &store.UpdateAgentMessage{
		Status: &storepb.AgentStatus{
			State:           storepb.AgentStatus_ONLINE,
			LastHeartbeatAt: nowSec,
			ConnectedAt:     agent.Status.GetConnectedAt(),
			ActiveSessionId: activeSessionID,
		},
	}

	if _, err := s.store.UpdateAgent(ctx, agent, patch); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to update agent heartbeat, error: %v", err))
	}

	if s.stateCfg.HeartbeatBuffer != nil {
		s.stateCfg.HeartbeatBuffer.Record(&state.HeartbeatUpdate{
			AgentID:         agent.ID,
			LastHeartbeatAt: nowSec,
			SessionID:       req.Msg.SessionId,
		})
	}

	resp := &v1pb.AgentHeartbeatResponse{
		NextNonce:       nonce,
		NextHeartbeatAt: timestamppb.New(time.Now().Add(30 * time.Second)),
	}

	if expiresAt, ok := common.GetAccessTokenExpiresAtFromContext(ctx); ok && expiresAt > 0 {
		if time.Now().Unix() >= expiresAt-int64(accessTokenDuration.Seconds()/3) {
			newAccessToken, err := auth.GenerateAgentTokenWithSession(agent.Name, agent.ResourceID, agent.TokenVersion, auth.TokenTypeAccess, req.Msg.SessionId, s.profile.Mode, s.secret, accessTokenDuration)
			if err != nil {
				slog.Warn("failed to generate new access token during heartbeat", "error", err)
			} else {
				resp.AccessToken = newAccessToken
				resp.AccessTokenExpiresAt = timestamppb.New(time.Now().Add(accessTokenDuration))
			}
		}
	}

	if s.dispatcher != nil && !s.dispatcher.IsAgentConnected(agent.ID) {
		pending, err := s.store.GetNextPendingCommand(ctx, agent.ID)
		if err != nil {
			slog.Warn("failed to check pending commands during heartbeat", "error", err)
		} else if pending != nil {
			resp.CommandStreamRequired = true
			resp.PendingCommandHint = &v1pb.PendingCommandHint{
				CommandId:      pending.ID.String(),
				Command:        pending.Command,
				WorkingDir:     pending.WorkingDir,
				TimeoutSeconds: pending.TimeoutSeconds,
			}
		}
	}

	return connect.NewResponse(resp), nil
}

func (s *AgentService) AgentDisconnect(ctx context.Context, req *connect.Request[v1pb.AgentDisconnectRequest]) (*connect.Response[emptypb.Empty], error) {
	agent, ok := GetAgentFromContext(ctx)
	if !ok || agent == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("agent not authenticated"))
	}

	reason := "agent_shutdown"
	if req.Msg.Reason != "" {
		reason = req.Msg.Reason
	}
	sessionID := req.Msg.SessionId
	if sessionID != "" {
		if err := s.store.TerminateAgentSession(ctx, sessionID, reason); err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to terminate agent session, error: %v", err))
		}
	}

	s.stateCfg.NonceManager.DeleteKey(agent.ResourceID)

	patch := &store.UpdateAgentMessage{
		Status: &storepb.AgentStatus{
			State:           storepb.AgentStatus_OFFLINE,
			ActiveSessionId: "",
		},
	}
	if _, err := s.store.UpdateAgent(ctx, agent, patch); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to update agent status, error: %v", err))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (s *AgentService) RefreshAgentToken(ctx context.Context, req *connect.Request[v1pb.RefreshAgentTokenRequest]) (*connect.Response[v1pb.RefreshAgentTokenResponse], error) {
	refreshTokenStr := req.Msg.RefreshToken
	if refreshTokenStr == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("refresh token is required"))
	}

	// Verify the JWT signature before trusting any claim. Without this the
	// handler relied solely on a hash lookup: a refresh token whose
	// token_version was forged (or minted under a since-rotated secret) could
	// pass the hash check and be "upgraded" to the current token_version.
	claims, err := auth.ParseAgentToken(refreshTokenStr, s.secret)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.Wrap(err, "invalid refresh token"))
	}
	if claims.TokenType != auth.TokenTypeRefresh {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.Errorf("expected refresh token, got %s", claims.TokenType))
	}

	tokenHash := hashToken(refreshTokenStr)
	storedToken, err := s.store.GetAgentTokenByHash(ctx, tokenHash)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to look up refresh token, error: %v", err))
	}
	if storedToken == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("invalid refresh token"))
	}

	switch action := refreshReuseAction(storedToken.State); action {
	case refreshActionProceed:
		// Fresh token: proceed to rotate it below.
	case refreshActionRevokeFamily:
		// Reuse detected: a refresh token that was already exchanged (CONSUMED)
		// or revoked is being presented again. Revoke the entire family so
		// every token derived from the same bootstrap/rotation is invalidated,
		// then reject. Reusing a CONSUMED token previously only revoked the
		// single row and still issued a new token — i.e. it silently
		// succeeded, defeating reuse detection.
		if err := s.store.RevokeTokenFamily(ctx, storedToken.TokenFamily); err != nil {
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

	agent, err := s.store.GetAgent(ctx, storedToken.AgentID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to get agent, error: %v", err))
	}
	if agent == nil || agent.Deleted {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("agent not found or deactivated"))
	}

	// Bind the token's version to the agent's current version. After a
	// RotateAgentToken/RevokeAgentToken the version increments and the old
	// family is revoked; if a refresh token from the old family survived the
	// revoke (e.g. a partial failure), its embedded version no longer matches
	// and we reject rather than minting a new token under the current version.
	if claims.TokenVersion != agent.TokenVersion {
		if err := s.store.RevokeTokenFamily(ctx, storedToken.TokenFamily); err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to revoke stale token family, error: %v", err))
		}
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("refresh token version mismatch"))
	}

	if storedToken.State == storepb.AgentTokenState_ACTIVE {
		consumedAt := time.Now()
		if err := s.store.UpdateAgentTokenState(ctx, storedToken.ID, storepb.AgentTokenState_CONSUMED, &consumedAt); err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to mark refresh token as consumed, error: %v", err))
		}
		s.scheduleTokenRevoke(storedToken.ID, storedToken.TokenFamily)
	}

	accessToken, err := auth.GenerateAgentTokenWithSession(agent.Name, agent.ResourceID, agent.TokenVersion, auth.TokenTypeAccess, "", s.profile.Mode, s.secret, accessTokenDuration)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate access token, error: %v", err))
	}

	newRefreshToken, err := auth.GenerateAgentTokenWithSession(agent.Name, agent.ResourceID, agent.TokenVersion, auth.TokenTypeRefresh, "", s.profile.Mode, s.secret, refreshTokenDuration)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate refresh token, error: %v", err))
	}

	newTokenHash := hashToken(newRefreshToken)
	if err := s.store.CreateAgentToken(ctx, &store.AgentTokenMessage{
		AgentID:     agent.ID,
		TokenHash:   newTokenHash,
		TokenType:   storepb.AgentTokenType_REFRESH,
		TokenFamily: storedToken.TokenFamily,
		State:       storepb.AgentTokenState_ACTIVE,
		Fingerprint: req.Msg.Fingerprint,
		ExpiresAt:   time.Now().Add(refreshTokenDuration),
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to store new refresh token, error: %v", err))
	}

	return connect.NewResponse(&v1pb.RefreshAgentTokenResponse{
		AccessToken:          accessToken,
		RefreshToken:         newRefreshToken,
		AccessTokenExpiresAt: timestamppb.New(time.Now().Add(accessTokenDuration)),
	}), nil
}

func (s *AgentService) scheduleTokenRevoke(tokenID int, _ string) {
	timer := time.AfterFunc(refreshTokenReuseWindow, func() {
		if err := s.store.UpdateAgentTokenState(context.Background(), tokenID, storepb.AgentTokenState_REVOKED, nil); err != nil {
			slog.Error("failed to revoke consumed token", "token_id", tokenID, "error", err)
		}
		s.consumedMu.Lock()
		delete(s.consumedTimers, tokenID)
		s.consumedMu.Unlock()
	})
	s.consumedMu.Lock()
	s.consumedTimers[tokenID] = timer
	s.consumedMu.Unlock()
}

// refreshReuseAction is the pure decision for how RefreshAgentToken should
// treat a stored refresh token given its current state. Extracted so the
// reuse-detection matrix is unit-testable without a DB.
//
//   - ACTIVE        → proceed (rotate the token)
//   - CONSUMED/REVOKED → reuse: revoke the whole family and reject
//   - anything else → reject as invalid
//
// CONSUMED is treated as reuse (not as a valid second use) because a refresh
// token is single-use: once exchanged it must never be accepted again.
type refreshAction int

const (
	refreshActionProceed refreshAction = iota
	refreshActionRevokeFamily
	refreshActionInvalid
)

func refreshReuseAction(state storepb.AgentTokenState) refreshAction {
	switch state {
	case storepb.AgentTokenState_ACTIVE:
		return refreshActionProceed
	case storepb.AgentTokenState_CONSUMED, storepb.AgentTokenState_REVOKED:
		return refreshActionRevokeFamily
	default:
		return refreshActionInvalid
	}
}

func (*AgentService) Hello(_ context.Context, _ *connect.Request[v1pb.HelloRequest]) (*connect.Response[v1pb.HelloResponse], error) {
	return connect.NewResponse(&v1pb.HelloResponse{
		CurrentTime:   time.Now().Unix(),
		ServerVersion: "0.1.0",
	}), nil
}

type bootstrapClaims struct {
	Name         string `json:"name"`
	TokenVersion int    `json:"token_version"`
	TokenType    string `json:"token_type"`
	TokenFamily  string `json:"token_family"`
	jwt.RegisteredClaims
}

type bootstrapAuthResult struct {
	agent       *store.AgentMessage
	tokenFamily string
	// tokenID is the DB row id of the bootstrap token, so ConnectAgent can mark
	// it CONSUMED once the connection succeeds (making the bootstrap token
	// single-use). Zero when the agent authenticated via an access token.
	tokenID int
}

func (s *AgentService) authenticateBootstrapToken(tokenStr string) (*bootstrapAuthResult, error) {
	claims := &bootstrapClaims{}
	parsedToken, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
		if t.Method.Alg() != jwt.SigningMethodHS256.Name {
			return nil, errors.Errorf("unexpected signing method %v", t.Header["alg"])
		}
		if kid, ok := t.Header["kid"].(string); ok && kid == "v1" {
			return []byte(s.secret), nil
		}
		return nil, errors.Errorf("unexpected kid %v", t.Header["kid"])
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.Errorf("invalid bootstrap token: %v", err))
	}
	if !parsedToken.Valid {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("bootstrap token is invalid"))
	}
	if claims.TokenType != auth.TokenTypeBootstrap {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.Errorf("expected bootstrap token, got %s", claims.TokenType))
	}

	agent, err := s.store.GetAgentByResourceID(context.Background(), claims.Subject)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to find agent: %v", err))
	}
	if agent == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.Errorf("agent %s not found", claims.Subject))
	}
	if agent.Deleted {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.Errorf("agent %s has been deactivated", claims.Subject))
	}
	if agent.TokenVersion != claims.TokenVersion {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("agent token version mismatch"))
	}

	tokenHash := hashToken(tokenStr)
	storedToken, err := s.store.GetAgentTokenByHash(context.Background(), tokenHash)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to look up token: %v", err))
	}
	if storedToken == nil || storedToken.State != storepb.AgentTokenState_ACTIVE {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("bootstrap token is not active"))
	}
	if time.Now().After(storedToken.ExpiresAt) {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("bootstrap token expired"))
	}

	tokenFamily := claims.TokenFamily
	if tokenFamily == "" {
		tokenFamily = claims.Subject
	}

	return &bootstrapAuthResult{agent: agent, tokenFamily: tokenFamily, tokenID: storedToken.ID}, nil
}

// agentReachable reports whether an agent should present as online. Under the
// machine-hosts-many model the machine — not the agent — heartbeats, so an
// agent's liveness is NOT derived from a per-agent heartbeat timestamp
// (which is no longer written and would always read as offline). An agent is
// online when its own runner has a live AgentChannel (the precise signal) OR
// the machine it is bound to is connected (the machine app hosts the agent and
// will pick it up over its MachineChannel). The second clause matches the
// product model — "a connected machine's agents are online" — and covers the
// brief window before a freshly created agent's runner opens its stream, so
// the agent is online the moment it is created on a connected machine. Both
// go false when the machine disconnects (UnregisterMachine detaches every
// owned agent session), so the agent reports offline with its machine.
func agentReachable(d *dispatcher.Dispatcher, agentID, machineID int) bool {
	if d == nil {
		return false
	}
	return d.IsAgentConnected(agentID) || d.IsMachineConnected(machineID)
}

func convertToAgent(agent *store.AgentMessage, connected bool) *v1pb.Agent {
	name := common.FormatAgentUID(agent.ResourceID)
	state := v1pb.State_ACTIVE
	if agent.Deleted {
		state = v1pb.State_DELETED
	}

	status := convertToV1AgentStatus(agent.Status, agent.Deleted, connected)

	result := &v1pb.Agent{
		Name:         name,
		State:        state,
		Title:        agent.Name,
		Info:         convertToV1AgentInfo(agent.Info),
		Status:       status,
		CreatedAt:    timestamppb.New(agent.CreatedAt),
		TokenVersion: int32(agent.TokenVersion),
		Machine:      common.FormatMachineUID(agent.MachineResourceID),
	}
	if !agent.LastTokenRotatedAt.IsZero() {
		result.LastTokenRotatedAt = timestamppb.New(agent.LastTokenRotatedAt)
	}
	if agent.CreatedBy != 0 {
		// Creator's user resource name (users/{id}); empty for legacy agents.
		result.CreatedBy = common.FormatUserUID(agent.CreatedBy)
	}
	if agent.AvatarS3Key != "" {
		result.Avatar = common.FormatAgentAvatar(agent.ResourceID)
	}
	return result
}

// convertToAgentSummary builds the lightweight ListAgents projection of an
// agent: identity, lifecycle state, connection status, the
// provider/executable signal that the frontend agentLifecycle() classifier
// reads, and the creator (created_by) so list consumers can group agents by
// owner without an N+1 of GetAgent. Heavy per-agent data (available_providers,
// the rest of acp_config, capability, host info, token fields, can_edit) is
// omitted — it is only returned by GetAgent. See ListAgents for the contract
// rationale.
func convertToAgentSummary(agent *store.AgentMessage, connected bool) *v1pb.AgentSummary {
	state := v1pb.State_ACTIVE
	if agent.Deleted {
		state = v1pb.State_DELETED
	}
	summary := &v1pb.AgentSummary{
		Name:    common.FormatAgentUID(agent.ResourceID),
		State:   state,
		Title:   agent.Name,
		Status:  convertToV1AgentStatus(agent.Status, agent.Deleted, connected),
		Machine: common.FormatMachineUID(agent.MachineResourceID),
	}
	if agent.Info != nil && agent.Info.AcpConfig != nil {
		summary.Provider = agent.Info.AcpConfig.Provider
		summary.Executable = agent.Info.AcpConfig.Executable
	}
	// Surface the creator on the summary (users/{id}) so list consumers can
	// group agents by creator without an N+1 of GetAgent.
	if agent.CreatedBy != 0 {
		summary.CreatedBy = common.FormatUserUID(agent.CreatedBy)
	}
	return summary
}

// cloneStoreAgentInfo returns a deep copy of info safe to mutate before a
// partial UpdateAgent, or a fresh empty AgentInfo when info is nil. The plain
// type assertion after proto.Clone is unchecked; centralizing it here keeps
// revive's unchecked-type-assertion quiet without hiding the panic risk at
// each call site (the input is always *storepb.AgentInfo here).
func cloneStoreAgentInfo(info *storepb.AgentInfo) *storepb.AgentInfo {
	if info == nil {
		return &storepb.AgentInfo{}
	}
	cloned := proto.Clone(info)
	patchInfo, ok := cloned.(*storepb.AgentInfo)
	if !ok || patchInfo == nil {
		return &storepb.AgentInfo{}
	}
	return patchInfo
}

func convertToV1AgentInfo(info *storepb.AgentInfo) *v1pb.AgentInfo {
	if info == nil {
		return nil
	}
	return &v1pb.AgentInfo{
		AgentType:          info.AgentType,
		Hostname:           info.Hostname,
		Os:                 info.Os,
		Arch:               info.Arch,
		Ip:                 info.Ip,
		Version:            info.Version,
		Labels:             info.Labels,
		Capability:         convertToV1AgentCapability(info.Capability),
		AvailableProviders: convertToV1Providers(info.AvailableProviders),
		AcpConfig:          convertToV1AgentACPConfig(info.AcpConfig),
	}
}

func convertToStoreAgentInfo(info *v1pb.AgentInfo) *storepb.AgentInfo {
	if info == nil {
		return nil
	}
	return &storepb.AgentInfo{
		AgentType:  info.AgentType,
		Hostname:   info.Hostname,
		Os:         info.Os,
		Arch:       info.Arch,
		Ip:         info.Ip,
		Version:    info.Version,
		Labels:     info.Labels,
		Capability: convertToStoreAgentCapability(info.Capability),
		// available_providers is agent-owned: store exactly what the agent reported.
		AvailableProviders: convertToStoreProviders(info.AvailableProviders),
		// AcpConfig is server-owned; never overwrite it from agent-reported info.
		AcpConfig: nil,
	}
}

func convertToV1AgentACPConfig(cfg *storepb.AgentACPConfig) *v1pb.AgentACPConfig {
	if cfg == nil {
		return nil
	}
	return &v1pb.AgentACPConfig{
		Executable:    cfg.Executable,
		Args:          cfg.Args,
		AllowEnv:      cfg.AllowEnv,
		Provider:      cfg.Provider,
		Model:         cfg.Model,
		CustomEnv:     cfg.CustomEnv,
		PersonaPrompt: cfg.PersonaPrompt,
		ApiProvider:   cfg.ApiProvider,
		ApiKey:        cfg.ApiKey,
	}
}

func convertToStoreAgentACPConfig(cfg *v1pb.AgentACPConfig) *storepb.AgentACPConfig {
	if cfg == nil {
		return nil
	}
	return &storepb.AgentACPConfig{
		Executable:    cfg.Executable,
		Args:          cfg.Args,
		AllowEnv:      cfg.AllowEnv,
		Provider:      cfg.Provider,
		Model:         cfg.Model,
		CustomEnv:     cfg.CustomEnv,
		PersonaPrompt: cfg.PersonaPrompt,
		ApiProvider:   cfg.ApiProvider,
		ApiKey:        cfg.ApiKey,
	}
}

// isEmptyAgentACPConfig reports whether cfg carries no user configuration — the
// zero value a caller sends when it omits acp_config. CreateAgent treats an
// empty config as "not provided" so the minimal default is used instead of
// validating (and rejecting) an empty provider.
func isEmptyAgentACPConfig(cfg *v1pb.AgentACPConfig) bool {
	return cfg.Executable == "" && len(cfg.Args) == 0 && len(cfg.AllowEnv) == 0 &&
		cfg.Provider == "" && cfg.Model == "" && len(cfg.CustomEnv) == 0 && cfg.PersonaPrompt == ""
}

// buildCapabilityForACPConfig derives the agent capability from the
// user-configurable ACP settings, branching on the runtime. A builtin-pi agent
// (provider == pi.BuiltinPiProvider) is a non-ACP runtime: its capability comes
// from the pi package (SupportsPi, not SupportsAcp) and does not depend on a
// host-detected executable. Every other provider is an ACP runtime and goes
// through the existing executor.BuildCapability path. This is the single place
// the manager picks a runtime's capability, so the executor package stays
// pi-free (no import cycle: pi already imports executor).
func buildCapabilityForACPConfig(cfg *v1pb.AgentACPConfig) *v1pb.AgentCapability {
	if cfg != nil && cfg.GetProvider() == pi.BuiltinPiProvider {
		return pi.BuildPiCapability(cfg)
	}
	return executor.BuildCapability(cfg)
}

func convertToV1Providers(in []*storepb.AgentProviderInfo) []*v1pb.AgentProviderInfo {
	if len(in) == 0 {
		return nil
	}
	out := make([]*v1pb.AgentProviderInfo, 0, len(in))
	for _, p := range in {
		out = append(out, &v1pb.AgentProviderInfo{
			ProviderId:                p.ProviderId,
			DisplayName:               p.DisplayName,
			Version:                   p.Version,
			ExecutablePath:            p.ExecutablePath,
			Models:                    convertToV1Models(p.Models),
			SupportsModelConfigOption: p.SupportsModelConfigOption,
			DetectedAt:                p.DetectedAt,
		})
	}
	return out
}

func convertToStoreProviders(in []*v1pb.AgentProviderInfo) []*storepb.AgentProviderInfo {
	if len(in) == 0 {
		return nil
	}
	out := make([]*storepb.AgentProviderInfo, 0, len(in))
	for _, p := range in {
		out = append(out, &storepb.AgentProviderInfo{
			ProviderId:                p.ProviderId,
			DisplayName:               p.DisplayName,
			Version:                   p.Version,
			ExecutablePath:            p.ExecutablePath,
			Models:                    convertToStoreModels(p.Models),
			SupportsModelConfigOption: p.SupportsModelConfigOption,
			DetectedAt:                p.DetectedAt,
		})
	}
	return out
}

func convertToV1Models(in []*storepb.AgentModelOption) []*v1pb.AgentModelOption {
	if len(in) == 0 {
		return nil
	}
	out := make([]*v1pb.AgentModelOption, 0, len(in))
	for _, m := range in {
		out = append(out, &v1pb.AgentModelOption{
			Value:       m.Value,
			Name:        m.Name,
			Description: m.Description,
		})
	}
	return out
}

func convertToStoreModels(in []*v1pb.AgentModelOption) []*storepb.AgentModelOption {
	if len(in) == 0 {
		return nil
	}
	out := make([]*storepb.AgentModelOption, 0, len(in))
	for _, m := range in {
		out = append(out, &storepb.AgentModelOption{
			Value:       m.Value,
			Name:        m.Name,
			Description: m.Description,
		})
	}
	return out
}

func convertToV1AgentCapability(capability *storepb.AgentCapability) *v1pb.AgentCapability {
	if capability == nil {
		return nil
	}
	return &v1pb.AgentCapability{
		SupportsAcp:                capability.SupportsAcp,
		MaxTimeoutSeconds:          capability.MaxTimeoutSeconds,
		SupportsDiff:               capability.SupportsDiff,
		SupportsRawEvents:          capability.SupportsRawEvents,
		SupportsToolTraces:         capability.SupportsToolTraces,
		MaxEventCount:              capability.MaxEventCount,
		MaxOutputBytes:             capability.MaxOutputBytes,
		SupportsAutonomousDecision: capability.SupportsAutonomousDecision,
		SupportsPi:                 capability.SupportsPi,
	}
}

func convertToStoreAgentCapability(capability *v1pb.AgentCapability) *storepb.AgentCapability {
	if capability == nil {
		return nil
	}
	return &storepb.AgentCapability{
		SupportsAcp:                capability.SupportsAcp,
		MaxTimeoutSeconds:          capability.MaxTimeoutSeconds,
		SupportsDiff:               capability.SupportsDiff,
		SupportsRawEvents:          capability.SupportsRawEvents,
		SupportsToolTraces:         capability.SupportsToolTraces,
		MaxEventCount:              capability.MaxEventCount,
		MaxOutputBytes:             capability.MaxOutputBytes,
		SupportsAutonomousDecision: capability.SupportsAutonomousDecision,
		SupportsPi:                 capability.SupportsPi,
	}
}

func convertToV1AgentStatus(status *storepb.AgentStatus, deleted bool, connected bool) *v1pb.AgentStatus {
	if status == nil {
		return nil
	}
	state := computeConnectionState(status, deleted, connected)

	var lastHeartbeatTime *timestamppb.Timestamp
	if status.LastHeartbeatAt > 0 {
		lastHeartbeatTime = timestamppb.New(time.Unix(status.LastHeartbeatAt, 0))
	}
	var connectedTime *timestamppb.Timestamp
	if status.ConnectedAt > 0 {
		connectedTime = timestamppb.New(time.Unix(status.ConnectedAt, 0))
	}

	return &v1pb.AgentStatus{
		State:             state,
		LastHeartbeatTime: lastHeartbeatTime,
		ConnectedTime:     connectedTime,
		ErrorMessage:      status.ErrorMessage,
		ActiveSessionId:   status.ActiveSessionId,
	}
}

// computeConnectionState derives an agent's connection state. Under the
// machine-hosts-many model the machine heartbeats, not the agent, so liveness
// is taken from `connected` (the agent's live AgentChannel in the dispatcher),
// not from status.LastHeartbeatAt (which is no longer written and would always
// read as offline). Explicit ERROR/KICKED terminal states and deletion take
// precedence over the live-stream signal.
func computeConnectionState(status *storepb.AgentStatus, deleted bool, connected bool) v1pb.AgentStatus_ConnectionState {
	if status.State == storepb.AgentStatus_ERROR {
		return v1pb.AgentStatus_ERROR
	}
	if status.State == storepb.AgentStatus_KICKED {
		return v1pb.AgentStatus_KICKED
	}
	if deleted {
		return v1pb.AgentStatus_OFFLINE
	}
	if connected {
		return v1pb.AgentStatus_ONLINE
	}
	return v1pb.AgentStatus_OFFLINE
}

func convertToV1Session(session *store.AgentSessionMessage) *v1pb.AgentSession {
	var connectedAt, lastHeartbeatAt, disconnectedAt *timestamppb.Timestamp
	if !session.ConnectedAt.IsZero() {
		connectedAt = timestamppb.New(session.ConnectedAt)
	}
	if !session.LastHeartbeatAt.IsZero() {
		lastHeartbeatAt = timestamppb.New(session.LastHeartbeatAt)
	}
	if !session.DisconnectedAt.IsZero() {
		disconnectedAt = timestamppb.New(session.DisconnectedAt)
	}

	var state v1pb.AgentStatus_ConnectionState
	switch session.State {
	case "ACTIVE":
		state = v1pb.AgentStatus_ONLINE
	case "KICKED":
		state = v1pb.AgentStatus_KICKED
	case "TERMINATED":
		state = v1pb.AgentStatus_OFFLINE
	default:
		state = v1pb.AgentStatus_CONNECTION_STATE_UNSPECIFIED
	}

	return &v1pb.AgentSession{
		SessionId:        session.SessionID,
		AgentName:        common.FormatAgentUID(session.AgentResourceID),
		SourceIp:         session.SourceIP,
		AgentVersion:     session.AgentVersion,
		Fingerprint:      session.Fingerprint,
		ConnectedAt:      connectedAt,
		LastHeartbeatAt:  lastHeartbeatAt,
		DisconnectedAt:   disconnectedAt,
		DisconnectReason: session.DisconnectReason,
		State:            state,
	}
}

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func generateRandomString(length int) string {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)[:length]
}

func (s *AgentService) UpdateAgentACPConfig(ctx context.Context, req *connect.Request[v1pb.UpdateAgentACPConfigRequest]) (*connect.Response[emptypb.Empty], error) {
	resourceID, err := common.GetAgentResourceID(req.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	agent, err := s.store.GetAgentByResourceID(ctx, resourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if agent == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("agent %s not found", resourceID))
	}

	// builtin-pi api_key is a secret: the config form does not echo it back on
	// save (the password field is left empty to avoid retransmitting it). An
	// empty api_key here means "keep the existing key", so copy it from the
	// stored config before validation so the required-field check passes.
	if req.Msg.AcpConfig != nil && req.Msg.AcpConfig.Provider == pi.BuiltinPiProvider && strings.TrimSpace(req.Msg.AcpConfig.ApiKey) == "" {
		if existing := agent.Info.GetAcpConfig(); existing != nil {
			req.Msg.AcpConfig.ApiKey = existing.ApiKey
		}
	}

	if err := validateAgentACPConfig(req.Msg.AcpConfig, nil); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	// Re-validate against the owning machine's discovered providers now that
	// we know the binding. A built-in provider must be runnable on the host.
	if err := validateAgentACPConfig(req.Msg.AcpConfig, s.machineAvailableProviders(ctx, agent.MachineID)); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	// Preserve the rest of AgentInfo (hostname/os/capability/available_providers/
	// labels); only AcpConfig is admin-owned and replaced here. Previously this
	// built a fresh Info{AcpConfig:...} and clobbered the agent-reported fields.
	patchInfo := cloneStoreAgentInfo(agent.Info)
	patchInfo.AcpConfig = convertToStoreAgentACPConfig(req.Msg.AcpConfig)
	// Re-derive the capability from the new config so it stays in sync. The
	// capability is a pure function of the config (buildCapabilityForACPConfig);
	// without this, changing a provider (e.g. ACP → builtin-pi) left a stale
	// capability — the dispatcher's BeginSession gate then mis-classified the
	// runtime (a pi agent with supports_pi=false stays idle forever). This also
	// self-repairs existing pi agents whose capability was written by a converter
	// that predated the supports_pi field: re-saving their config now writes the
	// correct capability.
	patchInfo.Capability = convertToStoreAgentCapability(buildCapabilityForACPConfig(req.Msg.AcpConfig))

	patch := &store.UpdateAgentMessage{Info: patchInfo}
	if _, err := s.store.UpdateAgent(ctx, agent, patch); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	// Best-effort: hot-reload the agent's ACP config on the machine. The runner
	// picks up the new config at its next BeginSession; a provider/model change
	// invalidates the persisted ACP SessionId so the next turn cold-starts with
	// the new config. A missed push (machine offline) is recovered on reconnect.
	if s.dispatcher != nil && agent.MachineID > 0 {
		if pushErr := s.dispatcher.SendAgentConfigUpdate(agent.MachineID, common.FormatAgentUID(agent.ResourceID), req.Msg.AcpConfig); pushErr != nil {
			slog.Info("best-effort agent config update push skipped", "agent", agent.ResourceID, "machineID", agent.MachineID, "error", pushErr)
		}
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

// RefreshAgentProviders asks the agent daemon to re-probe its host for
// installed LLM agent providers + models, then persists the fresh result into
// agent.info.available_providers and returns it. Requires the agent to be
// online (the probe runs on the agent's host, reached via the bidi stream).
func (s *AgentService) RefreshAgentProviders(ctx context.Context, req *connect.Request[v1pb.RefreshAgentProvidersRequest]) (*connect.Response[v1pb.RefreshAgentProvidersResponse], error) {
	resourceID, err := common.GetAgentResourceID(req.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	agent, err := s.store.GetAgentByResourceID(ctx, resourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if agent == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("agent %s not found", resourceID))
	}
	if !s.dispatcher.IsAgentConnected(agent.ID) {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("agent is not connected; cannot probe providers"))
	}

	requestID := uuid.NewString()
	replyCh := s.dispatcher.RegisterPendingDiscover(requestID)
	defer s.dispatcher.CancelPendingDiscover(requestID)

	if err := s.dispatcher.SendDiscoverProviders(agent.ID, requestID); err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.Wrap(err, "failed to request provider discovery"))
	}

	select {
	case msg := <-replyCh:
		if msg == nil {
			return nil, connect.NewError(connect.CodeInternal, errors.New("provider discovery returned no result"))
		}
		// Persist the fresh provider list into agent.info, preserving every
		// other AgentInfo field (only available_providers is agent-owned here).
		patchInfo := cloneStoreAgentInfo(agent.Info)
		patchInfo.AvailableProviders = convertToStoreProviders(msg.Providers)
		if _, err := s.store.UpdateAgent(ctx, agent, &store.UpdateAgentMessage{Info: patchInfo}); err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to persist discovered providers"))
		}
		return connect.NewResponse(&v1pb.RefreshAgentProvidersResponse{
			Providers: convertToV1Providers(patchInfo.AvailableProviders),
		}), nil
	case <-time.After(60 * time.Second):
		return nil, connect.NewError(connect.CodeDeadlineExceeded, errors.New("timed out waiting for provider discovery"))
	case <-ctx.Done():
		return nil, connect.NewError(connect.CodeDeadlineExceeded, ctx.Err())
	}
}

// ListPiModels proxies an LLM API provider's model-listing API so the agent
// config form can populate the model picker dynamically (no hardcoded model
// list). Not agent-scoped — the add-agent form calls it before the agent exists.
// The api_key is used only for the outbound provider call and is never logged
// (the audit interceptor records method/actor/status only, not the body).
func (*AgentService) ListPiModels(ctx context.Context, req *connect.Request[v1pb.ListPiModelsRequest]) (*connect.Response[v1pb.ListPiModelsResponse], error) {
	apiProvider := strings.TrimSpace(req.Msg.ApiProvider)
	if !pi.IsKnownAPIProvider(apiProvider) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("unsupported api_provider %q", req.Msg.ApiProvider))
	}
	// DeepSeek's /models requires the caller's key; OpenRouter's is public.
	if apiProvider == pi.APIProviderDeepseek && strings.TrimSpace(req.Msg.ApiKey) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("api_key is required to list models for this provider"))
	}

	models, err := pi.ListModels(ctx, apiProvider, req.Msg.ApiKey)
	if err != nil {
		// Validation already ruled out client-side errors; anything left is an
		// upstream provider/network failure (auth, timeout, non-2xx).
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.Wrap(err, "failed to list models from provider"))
	}

	out := make([]*v1pb.PiModel, 0, len(models))
	for _, m := range models {
		out = append(out, &v1pb.PiModel{Id: m.ID, Name: m.Name})
	}
	return connect.NewResponse(&v1pb.ListPiModelsResponse{Models: out}), nil
}

// validateAgentACPConfig checks the user-configurable ACP fields. A provider is
// required (a built-in id or "custom"); a built-in provider (opencode,
// claude-code) supplies its own launch command, so executable is only required
// for the "custom" provider. model is required when the owning machine has
// probed the provider and the provider exposes a model config option with
// advertised models — a provider that does not expose model selection via the
// protocol (or has not probed) does not require a model. Every allow_env and
// custom_env key must be a valid env var name.
func validateAgentACPConfig(cfg *v1pb.AgentACPConfig, machineAvailableProviders []*storepb.AgentProviderInfo) error {
	if cfg == nil {
		return errors.New("acp_config must be set")
	}
	if cfg.Provider == "" {
		return errors.New("acp_config.provider must be set")
	}
	if !knownProviderID(cfg.Provider) {
		return errors.Errorf("invalid acp_config.provider %q: must be a built-in id or \"custom\"", cfg.Provider)
	}
	// builtin-pi is a non-ACP runtime: it needs an API provider + API key +
	// model, not a host-detected executable. Validate its fields and skip the
	// host-availability / model-config-option checks (pi is always available —
	// it is bundled with laelia, not installed on the host).
	if cfg.Provider == pi.BuiltinPiProvider {
		if !pi.IsKnownAPIProvider(cfg.ApiProvider) {
			return errors.Errorf("acp_config.api_provider %q is not supported (phase 1: deepseek, openrouter)", cfg.ApiProvider)
		}
		if strings.TrimSpace(cfg.ApiKey) == "" {
			return errors.New("acp_config.api_key must be set for builtin-pi")
		}
		if strings.TrimSpace(cfg.Model) == "" {
			return errors.New("acp_config.model must be set for builtin-pi")
		}
		return nil
	}
	// A built-in provider derives its command from the registry; anything else
	// requires a raw executable.
	_, isBuiltin := provider.Default().Lookup(cfg.Provider)
	if !isBuiltin && cfg.Executable == "" {
		return errors.New("acp_config.executable must be set when provider is not a built-in")
	}
	// If the owning machine has discovered its available providers, a built-in
	// provider must be among them — otherwise the agent is configured for a
	// provider the host cannot run, which only surfaces at BeginSession. When
	// the machine has not probed yet (empty list) or the provider is "custom"
	// (uses an explicit executable, not discovered), skip this check.
	if len(machineAvailableProviders) > 0 && isBuiltin {
		if !providerAvailable(cfg.Provider, machineAvailableProviders) {
			return errors.Errorf("acp_config.provider %q is not available on the owning machine (available: %s)",
				cfg.Provider, availableProviderIDs(machineAvailableProviders))
		}
		// A provider that exposes a model config option with advertised models
		// requires a model selection. When the machine has not probed (or the
		// provider does not expose model selection) the requirement cannot be
		// confirmed, so model is left optional and may be set later.
		if providerSupportsModel(cfg.Provider, machineAvailableProviders) && cfg.Model == "" {
			return errors.Errorf("acp_config.model must be set for provider %q", cfg.Provider)
		}
	}
	for _, name := range cfg.AllowEnv {
		if !envVarNameRegex.MatchString(name) {
			return errors.Errorf("invalid allow_env entry %q: must match ^[A-Za-z_][A-Za-z0-9_]*$", name)
		}
	}
	for key := range cfg.CustomEnv {
		if !envVarNameRegex.MatchString(key) {
			return errors.Errorf("invalid custom_env key %q: must match ^[A-Za-z_][A-Za-z0-9_]*$", key)
		}
	}
	return nil
}

// providerSupportsModel reports whether the provider exposes a model config
// option with at least one advertised model on the owning machine. Used to
// decide whether acp_config.model is required. Returns false when the provider
// is not in the machine's discovered set (including "custom" and the
// not-yet-probed case), so model is not enforced in those cases.
func providerSupportsModel(providerID string, available []*storepb.AgentProviderInfo) bool {
	for _, p := range available {
		if p.ProviderId == providerID {
			return p.SupportsModelConfigOption && len(p.Models) > 0
		}
	}
	return false
}

// machineAvailableProviders returns the owning machine's discovered providers,
// or nil if machineID is zero or the machine/providers are unknown. Used to
// validate that a configured built-in provider is runnable on the host.
func (s *AgentService) machineAvailableProviders(ctx context.Context, machineID int) []*storepb.AgentProviderInfo {
	if machineID <= 0 {
		return nil
	}
	machine, err := s.store.GetMachine(ctx, machineID)
	if err != nil || machine == nil || machine.Info == nil {
		return nil
	}
	return machine.Info.AvailableProviders
}

func providerAvailable(providerID string, available []*storepb.AgentProviderInfo) bool {
	for _, p := range available {
		if p.ProviderId == providerID {
			return true
		}
	}
	return false
}

func availableProviderIDs(available []*storepb.AgentProviderInfo) string {
	ids := make([]string, 0, len(available))
	for _, p := range available {
		ids = append(ids, p.ProviderId)
	}
	return strings.Join(ids, ", ")
}

// knownProviderID reports whether id is a recognized provider id (a built-in,
// the bundled non-ACP pi runtime, or the "custom" escape hatch).
func knownProviderID(id string) bool {
	if id == "custom" || id == pi.BuiltinPiProvider {
		return true
	}
	_, ok := provider.Default().Lookup(id)
	return ok
}
