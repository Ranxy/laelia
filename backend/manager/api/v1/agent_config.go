package v1

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/Ranxy/laelia/backend/agent/executor"
	"github.com/Ranxy/laelia/backend/agent/pi"
	"github.com/Ranxy/laelia/backend/agent/provider"
	"github.com/Ranxy/laelia/backend/common"
	"github.com/Ranxy/laelia/backend/common/log"
	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/manager/store"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/emptypb"
)

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

	// Handler-gated: the agent's owner or a workspace admin may update the ACP
	// config (the proto carries no permission annotation).
	user, _ := GetUserFromContext(ctx)
	if !s.canEditAgent(ctx, user, agent) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("only the agent's owner or a workspace admin can modify this agent"))
	}

	// Legacy inline api_provider/api_key is gated by the workspace toggle: a
	// caller holding agents.edit may always set it; otherwise the toggle must be
	// on (the agent's owner may then self-configure their own key).
	reqACP := req.Msg.AcpConfig
	if reqACP != nil && pi.IsPiProvider(reqACP.Provider) && reqACP.GlobalProvider == "" && reqACP.GlobalProviderEntry == "" && reqACP.ApiProvider != "" {
		if !s.canUseInlineAPIKey(ctx, user, agent) {
			return nil, connect.NewError(connect.CodePermissionDenied, errors.New("self-provided api keys are disabled; use a global provider"))
		}
	}

	// pi api_key is a secret: the config form does not echo it back on save
	// (the password field is left empty to avoid retransmitting it). An empty
	// api_key — or a "****"-prefixed masked preview echoed back — means "keep
	// the existing key", so copy it from the stored config before validation
	// so the required-field check passes. This does not apply to the
	// user-installed pi "own model/auth" mode, where laelia intentionally does
	// not manage a key.
	if reqACP != nil && pi.IsPiProvider(reqACP.Provider) && reqACP.ApiProvider != "" {
		key := strings.TrimSpace(reqACP.ApiKey)
		if key == "" || strings.HasPrefix(key, secretMaskPrefix) {
			if existing := agent.Info.GetAcpConfig(); existing != nil {
				reqACP.ApiKey = existing.ApiKey
			}
		}
	}

	if err := validateAgentACPConfig(reqACP, nil); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if reqACP != nil && reqACP.GetGlobalProvider() != "" {
		if err := s.validateGlobalProviderReference(ctx, user, reqACP); err != nil {
			return nil, err
		}
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
		resolvedAcp, resolveErr := resolveAcpConfigForDaemon(ctx, s.store, reqACP)
		if resolveErr != nil {
			slog.Info("failed to resolve acp config for update push", "agent", agent.ResourceID, log.WithError(resolveErr))
		}
		if pushErr := s.dispatcher.SendAgentConfigUpdate(agent.MachineID, common.FormatAgentUID(agent.ResourceID), resolvedAcp); pushErr != nil {
			slog.Info("best-effort agent config update push skipped", "agent", agent.ResourceID, "machineID", agent.MachineID, "error", pushErr)
		}
	}
	// Push a prompt release notice only when the persona actually changed, so a
	// running agent is not told "your system prompt has been updated" (and forced
	// to re-anchor) when only the model/provider/api-key changed.
	personaChanged := req.Msg.AcpConfig.GetPersonaPrompt() != agent.Info.GetAcpConfig().GetPersonaPrompt()
	if personaChanged && s.dispatcher != nil {
		if pushErr := s.dispatcher.PushPromptReleaseNotice(ctx, agent.ID); pushErr != nil {
			slog.Info("best-effort prompt release notice push skipped", "agent", agent.ResourceID, "error", pushErr)
		}
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

// UpdateAgentMcpConfig replaces the MCP servers enabled on an agent. Handler-
// gated (the proto carries no permission annotation): the agent's owner or a
// workspace admin may update it, and only MCP servers the caller may use
// (members of the server's user/group list, or workspace admin) are accepted.
func (s *AgentService) UpdateAgentMcpConfig(ctx context.Context, req *connect.Request[v1pb.UpdateAgentMcpConfigRequest]) (*connect.Response[emptypb.Empty], error) {
	resourceID, err := common.GetAgentResourceID(req.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	agent, err := s.store.GetAgentByResourceID(ctx, resourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get agent"))
	}
	if agent == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("agent %s not found", resourceID))
	}

	user, _ := GetUserFromContext(ctx)
	if !s.canEditAgent(ctx, user, agent) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("only the agent's owner or a workspace admin can modify this agent"))
	}

	seen := make(map[string]bool, len(req.Msg.McpServers))
	var serverResourceIDs []string
	for _, name := range req.Msg.McpServers {
		serverResourceID, err := common.GetMcpServerResourceID(name)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		if seen[serverResourceID] {
			continue
		}
		server, err := s.store.GetMcpServerByResourceID(ctx, serverResourceID)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get mcp server"))
		}
		if server == nil {
			return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("mcp server %q not found", name))
		}
		ok, err := agentCanUseMcpServer(ctx, s.store, s.iam, agent, server)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to resolve mcp server access"))
		}
		if !ok {
			return nil, connect.NewError(connect.CodePermissionDenied, errors.Errorf("you are not allowed to use mcp server %q", name))
		}
		seen[serverResourceID] = true
		serverResourceIDs = append(serverResourceIDs, serverResourceID)
	}

	if err := s.store.ReplaceAgentMcpServers(ctx, agent.ID, serverResourceIDs); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to update agent mcp servers"))
	}

	// Best-effort full assignment reload so a pi agent's launch fingerprint
	// (which includes the MCP selection) changes and the subprocess restarts on
	// the next turn. ACP agents re-fetch the catalog every turn anyway. A missed
	// push (machine offline) is recovered on reconnect.
	if s.dispatcher != nil && agent.MachineID > 0 {
		resolvedAcp, resolveErr := resolveAcpConfigForDaemon(ctx, s.store, convertToV1AgentACPConfig(agent.Info.GetAcpConfig()))
		if resolveErr != nil {
			slog.Info("failed to resolve acp config for mcp reload push", "agent", agent.ResourceID, log.WithError(resolveErr))
		}
		agentName := common.FormatAgentUID(agent.ResourceID)
		if pushErr := s.dispatcher.SendReloadAgentAssignment(agent.MachineID, &v1pb.ReloadAgentAssignment{
			AgentName: agentName,
			Assignment: &v1pb.AgentAssignment{
				AgentName:        agentName,
				AgentDisplayName: agent.Name,
				AcpConfig:        resolvedAcp,
			},
		}); pushErr != nil {
			slog.Info("best-effort agent reload push skipped after mcp update", "agent", agent.ResourceID, "machineID", agent.MachineID, "error", pushErr)
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
	user, _ := GetUserFromContext(ctx)
	if !s.canEditAgent(ctx, user, agent) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("only the agent's owner or a holder of laelia.agents.edit can refresh this agent's providers"))
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

// RefreshAgentModels probes one provider's models on the agent's machine using
// the given (draft) ACP config's custom_env, so a model picker reflects an
// agent's custom env (e.g. CODEX_HOME) before the config is saved. The result
// is NOT persisted; it is returned to the caller for this session only. The
// probe runs on the agent's host, reached via the MachineChannel.
func (s *AgentService) RefreshAgentModels(ctx context.Context, req *connect.Request[v1pb.RefreshAgentModelsRequest]) (*connect.Response[v1pb.RefreshAgentModelsResponse], error) {
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
	user, _ := GetUserFromContext(ctx)
	if !s.canEditAgent(ctx, user, agent) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("only the agent's owner or a holder of laelia.agents.edit can refresh this agent's models"))
	}
	if agent.MachineID <= 0 {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("agent has no owning machine; cannot probe models"))
	}
	if s.dispatcher == nil || !s.dispatcher.IsMachineConnected(agent.MachineID) {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("agent's machine is not connected; cannot probe models"))
	}
	cfg := req.Msg.AcpConfig
	if cfg == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("acp_config is required to refresh models"))
	}
	providerID := cfg.Provider
	if providerID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("acp_config.provider is required to refresh models"))
	}
	// The per-provider probe only makes sense for a host-detected built-in
	// provider; "custom" (raw command) and builtin-pi (API-based) do not probe
	// models this way.
	if !knownProviderID(providerID) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid acp_config.provider %q", providerID))
	}
	if providerID == "custom" || pi.IsPiProvider(providerID) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("provider %q does not support on-host model probing", providerID))
	}

	requestID := uuid.NewString()
	replyCh := s.dispatcher.RegisterPendingModels(requestID)
	defer s.dispatcher.CancelPendingModels(requestID)

	if err := s.dispatcher.SendDiscoverModelsToMachine(agent.MachineID, providerID, cfg.GetCustomEnv(), requestID); err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.Wrap(err, "failed to request model discovery"))
	}

	select {
	case msg := <-replyCh:
		if msg == nil {
			return nil, connect.NewError(connect.CodeInternal, errors.New("model discovery returned no result"))
		}
		if msg.Error != "" {
			return connect.NewResponse(&v1pb.RefreshAgentModelsResponse{
				Provider: providerID,
				Error:    msg.Error,
			}), nil
		}
		return connect.NewResponse(&v1pb.RefreshAgentModelsResponse{
			Provider: providerID,
			Models:   msg.Models,
		}), nil
	case <-time.After(60 * time.Second):
		return nil, connect.NewError(connect.CodeDeadlineExceeded, errors.New("timed out waiting for model discovery"))
	case <-ctx.Done():
		return nil, connect.NewError(connect.CodeDeadlineExceeded, ctx.Err())
	}
}

// ListAgentWorkspace lists one directory level of an agent's workspace on its
// host machine. Requires the caller to be the agent owner or a workspace admin
// (canEditAgent), and the agent to be online: the listing runs on the agent's
// host and is relayed over the bidi stream.
func (s *AgentService) ListAgentWorkspace(ctx context.Context, req *connect.Request[v1pb.ListAgentWorkspaceRequest]) (*connect.Response[v1pb.ListAgentWorkspaceResponse], error) {
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
	user, _ := GetUserFromContext(ctx)
	if !s.canEditAgent(ctx, user, agent) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("workspace access requires owner or admin permission"))
	}
	if !s.dispatcher.IsAgentConnected(agent.ID) {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("agent is not connected; cannot list workspace"))
	}

	requestID := uuid.NewString()
	replyCh := s.dispatcher.RegisterPendingWorkspaceList(requestID)
	defer s.dispatcher.CancelPendingWorkspaceList(requestID)

	if err := s.dispatcher.SendWorkspaceListRequest(agent.ID, requestID, req.Msg.DirPath, req.Msg.IncludeHidden); err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.Wrap(err, "failed to request workspace listing"))
	}

	select {
	case msg := <-replyCh:
		if msg == nil {
			return nil, connect.NewError(connect.CodeInternal, errors.New("workspace listing returned no result"))
		}
		return connect.NewResponse(&v1pb.ListAgentWorkspaceResponse{Entries: msg.Entries}), nil
	case <-time.After(60 * time.Second):
		return nil, connect.NewError(connect.CodeDeadlineExceeded, errors.New("timed out waiting for workspace listing"))
	case <-ctx.Done():
		return nil, connect.NewError(connect.CodeDeadlineExceeded, ctx.Err())
	}
}

// ReadAgentWorkspaceFile previews one text/image file from an agent's
// workspace. Same authorization and online requirement as ListAgentWorkspace;
// the agent side enforces size limits and never serves sensitive files.
func (s *AgentService) ReadAgentWorkspaceFile(ctx context.Context, req *connect.Request[v1pb.ReadAgentWorkspaceFileRequest]) (*connect.Response[v1pb.ReadAgentWorkspaceFileResponse], error) {
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
	user, _ := GetUserFromContext(ctx)
	if !s.canEditAgent(ctx, user, agent) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("workspace access requires owner or admin permission"))
	}
	if !s.dispatcher.IsAgentConnected(agent.ID) {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("agent is not connected; cannot read workspace file"))
	}

	requestID := uuid.NewString()
	replyCh := s.dispatcher.RegisterPendingWorkspaceRead(requestID)
	defer s.dispatcher.CancelPendingWorkspaceRead(requestID)

	if err := s.dispatcher.SendWorkspaceReadRequest(agent.ID, requestID, req.Msg.Path); err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.Wrap(err, "failed to request workspace file read"))
	}

	select {
	case msg := <-replyCh:
		if msg == nil {
			return nil, connect.NewError(connect.CodeInternal, errors.New("workspace file read returned no result"))
		}
		// The machine reports preview-disabled reasons (sensitive file, too
		// large, missing) in the response's error field instead of failing the
		// RPC, mirroring raft so the frontend can show the specific reason.
		return connect.NewResponse(&v1pb.ReadAgentWorkspaceFileResponse{File: msg}), nil
	case <-time.After(60 * time.Second):
		return nil, connect.NewError(connect.CodeDeadlineExceeded, errors.New("timed out waiting for workspace file read"))
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
	var models []pi.Model
	var err error
	if apiProvider == pi.APIProviderCustom {
		baseURL := strings.TrimSpace(req.Msg.ApiBaseUrl)
		if baseURL == "" {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("api_base_url is required for custom provider"))
		}
		models, err = pi.ListCustomModels(ctx, nil, baseURL, req.Msg.ApiKey)
	} else {
		if !pi.IsKnownAPIProvider(apiProvider) {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("unsupported api_provider %q", req.Msg.ApiProvider))
		}
		// DeepSeek's /models requires the caller's key; OpenRouter's is public.
		if apiProvider == pi.APIProviderDeepseek && strings.TrimSpace(req.Msg.ApiKey) == "" {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("api_key is required to list models for this provider"))
		}
		models, err = pi.ListModels(ctx, nil, apiProvider, req.Msg.ApiKey)
	}
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
	if cfg.Protocol != "" && cfg.Protocol != executor.ProtocolV1 && cfg.Protocol != executor.ProtocolV2 {
		return errors.Errorf("invalid acp_config.protocol %q: must be \"acp-v1\" or \"acp-v2\"", cfg.Protocol)
	}
	// These fields are only meaningful for builtin-pi, but the non-negative
	// invariant is cheap to enforce for every provider and must also cover the
	// global-provider branch below (which returns before the inline checks).
	if cfg.ContextWindow < 0 {
		return errors.New("acp_config.context_window must be non-negative")
	}
	if cfg.MaxTokens < 0 {
		return errors.New("acp_config.max_tokens must be non-negative")
	}
	// pi is a non-ACP runtime. builtin-pi needs an API provider + API key +
	// model (or a global provider); user-installed pi additionally supports
	// pi's own model/auth mode where laelia does not manage a key.
	if pi.IsPiProvider(cfg.Provider) {
		// User-installed pi must actually be installed on the owning machine and
		// satisfy the minimum version when the machine has already probed. This
		// applies regardless of auth mode (own/self/global).
		if cfg.Provider == pi.UserPiProvider && len(machineAvailableProviders) > 0 {
			if !providerAvailable(cfg.Provider, machineAvailableProviders) {
				return errors.Errorf("acp_config.provider %q is not available on the owning machine (available: %s)",
					cfg.Provider, availableProviderIDs(machineAvailableProviders))
			}
			for _, p := range machineAvailableProviders {
				if p.ProviderId == cfg.Provider && !p.Compatible {
					return errors.Errorf("acp_config.provider %q is not compatible: %s", cfg.Provider, p.IncompatibilityReason)
				}
			}
		}
		// A global-provider reference replaces the inline api_provider/api_key
		// (the model resolves from the referenced entry). Both fields must be
		// set and consistent; access to the provider is checked by the handler.
		if cfg.GlobalProvider != "" || cfg.GlobalProviderEntry != "" {
			if cfg.GlobalProvider == "" || cfg.GlobalProviderEntry == "" {
				return errors.New("acp_config.global_provider and global_provider_entry must both be set")
			}
			providerID, err := common.GetAPIProviderResourceID(cfg.GlobalProvider)
			if err != nil {
				return errors.Errorf("invalid acp_config.global_provider %q", cfg.GlobalProvider)
			}
			entryProvider, _, err := common.ParseAPIProviderEntryName(cfg.GlobalProviderEntry)
			if err != nil {
				return errors.Errorf("invalid acp_config.global_provider_entry %q", cfg.GlobalProviderEntry)
			}
			if entryProvider != providerID {
				return errors.Errorf("acp_config.global_provider_entry %q does not belong to global_provider %q", cfg.GlobalProviderEntry, cfg.GlobalProvider)
			}
			return nil
		}
		// User-installed pi may use pi's own model/auth: no api_provider and no
		// global provider means laelia does not manage the key.
		if cfg.Provider == pi.UserPiProvider && cfg.ApiProvider == "" {
			if strings.TrimSpace(cfg.Model) == "" {
				return errors.New("acp_config.model must be set for user pi")
			}
			return nil
		}
		if !pi.IsKnownAPIProvider(cfg.ApiProvider) && cfg.ApiProvider != pi.APIProviderCustom {
			return errors.Errorf("acp_config.api_provider %q is not supported (phase 1: deepseek, openrouter, custom)", cfg.ApiProvider)
		}
		if cfg.ApiProvider == pi.APIProviderCustom && strings.TrimSpace(cfg.ApiBaseUrl) == "" {
			return errors.New("acp_config.api_base_url must be set for custom pi")
		}
		if strings.TrimSpace(cfg.ApiKey) == "" {
			return errors.New("acp_config.api_key must be set for pi")
		}
		if strings.TrimSpace(cfg.Model) == "" {
			return errors.New("acp_config.model must be set for pi")
		}
		return nil
	}
	// A built-in provider derives its command from the registry; anything else
	// requires a raw executable. A "acp-v2" declaration is only honored when
	// the provider actually speaks the thread protocol: a built-in provider's
	// protocol is fixed by its implementation, and a custom provider needs an
	// explicit executable to launch.
	p, isBuiltin := provider.Default().Lookup(cfg.Provider)
	if !isBuiltin && cfg.Executable == "" {
		return errors.New("acp_config.executable must be set when provider is not a built-in")
	}
	if isBuiltin {
		if _, ok := p.(provider.ThreadProvider); ok {
			if cfg.Protocol == executor.ProtocolV1 {
				return errors.Errorf("acp_config.provider %q only supports the acp-v2 thread protocol", cfg.Provider)
			}
		} else if cfg.Protocol == executor.ProtocolV2 {
			return errors.Errorf("acp_config.provider %q does not support the acp-v2 thread protocol", cfg.Provider)
		}
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

// knownProviderID reports whether id is a recognized provider id (a built-in
// ACP provider, the bundled or user-installed non-ACP pi runtime, or the
// "custom" escape hatch).
func knownProviderID(id string) bool {
	if id == "custom" || id == pi.BuiltinPiProvider {
		return true
	}
	_, ok := provider.Default().Lookup(id)
	return ok
}
