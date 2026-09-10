package v1

import (
	"context"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/laelia/backend/agent/executor"
	"github.com/Ranxy/laelia/backend/agent/pi"
	"github.com/Ranxy/laelia/backend/common"
	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/manager/store"
)

func (s *AgentService) convertToAgent(ctx context.Context, agent *store.AgentMessage, connected bool) *v1pb.Agent {
	name := common.FormatAgentUID(agent.ResourceID)
	state := v1pb.State_ACTIVE
	if agent.Deleted {
		state = v1pb.State_DELETED
	}

	status := convertToV1AgentStatus(agent.Status, agent.Deleted, connected, agent.Enabled)

	result := &v1pb.Agent{
		Name:                    name,
		Handle:                  agent.ResourceID,
		State:                   state,
		Title:                   agent.Name,
		Description:             agent.Description,
		Info:                    convertToV1AgentInfo(agent.Info),
		Status:                  status,
		CreatedAt:               timestamppb.New(agent.CreatedAt),
		TokenVersion:            int32(agent.TokenVersion),
		AllowAddToChannel:       agent.AllowAddToChannel,
		FollowOwnerPermissions:  agent.FollowOwnerPermissions,
		CanManageChannelMembers: agent.CanManageChannelMembers,
		Machine:                 common.FormatMachineUID(agent.MachineResourceID),
		Enabled:                 agent.Enabled,
	}
	if agent.MachineID != 0 {
		// The machine's display name rides along so clients can render a
		// human-readable machine name without a second GetMachine round-trip
		// (which would 404 for machines the caller may not see).
		if machine, err := s.store.GetMachine(ctx, agent.MachineID); err == nil && machine != nil {
			result.MachineTitle = machine.Name
		}
	}
	if !agent.LastTokenRotatedAt.IsZero() {
		result.LastTokenRotatedAt = timestamppb.New(agent.LastTokenRotatedAt)
	}
	if agent.CreatedBy != 0 {
		// Creator's user resource name (users/{handle}); empty for legacy
		// agents. Display-only — authorization uses Owner.
		result.CreatedBy = resolveUserResource(ctx, s.store, agent.CreatedBy)
	}
	if agent.OwnerID != 0 {
		// Owner's user resource name (users/{handle}) + display name; empty for
		// legacy agents. The display name is what the agent's prompt tells it to
		// write dm:@<owner_handle> to for high-risk approvals.
		result.Owner = resolveUserResource(ctx, s.store, agent.OwnerID)
		result.OwnerName = resolveUserName(ctx, s.store, agent.OwnerID)
	}
	if agent.AvatarS3Key != "" {
		result.Avatar = common.FormatAgentAvatar(agent.ResourceID)
	}
	return result
}

// convertToAgentSummary builds the lightweight ListAgents projection of an
// agent: identity, lifecycle state, connection status, the
// provider/executable signal that the frontend agentLifecycle() classifier
// reads, and the creator/owner (created_by, owner) so list consumers can show
// or group by them without an N+1 of GetAgent. Heavy per-agent data
// (available_providers, the rest of acp_config, capability, host info, token
// fields, can_edit) is omitted — it is only returned by GetAgent. See
// ListAgents for the contract rationale.
func convertToAgentSummary(ctx context.Context, s *store.Store, agent *store.AgentMessage, connected bool) *v1pb.AgentSummary {
	state := v1pb.State_ACTIVE
	if agent.Deleted {
		state = v1pb.State_DELETED
	}
	summary := &v1pb.AgentSummary{
		Name:                    common.FormatAgentUID(agent.ResourceID),
		Handle:                  agent.ResourceID,
		State:                   state,
		Title:                   agent.Name,
		Description:             agent.Description,
		Status:                  convertToV1AgentStatus(agent.Status, agent.Deleted, connected, agent.Enabled),
		AllowAddToChannel:       agent.AllowAddToChannel,
		FollowOwnerPermissions:  agent.FollowOwnerPermissions,
		CanManageChannelMembers: agent.CanManageChannelMembers,
		Machine:                 common.FormatMachineUID(agent.MachineResourceID),
		Enabled:                 agent.Enabled,
	}
	if agent.Info != nil && agent.Info.AcpConfig != nil {
		summary.Provider = agent.Info.AcpConfig.Provider
		summary.Executable = agent.Info.AcpConfig.Executable
	}
	// Surface the creator on the summary (users/{handle}) so list consumers can
	// show it without an N+1 of GetAgent. Display-only — authorization uses
	// Owner.
	if agent.CreatedBy != 0 {
		summary.CreatedBy = resolveUserResource(ctx, s, agent.CreatedBy)
	}
	// Surface the owner on the summary (users/{handle}) so list consumers (the
	// Members page's per-user "Owned Agents" view and the channel member picker)
	// can group agents by owner without an N+1 of GetAgent.
	if agent.OwnerID != 0 {
		summary.Owner = resolveUserResource(ctx, s, agent.OwnerID)
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
		Executable:          cfg.Executable,
		Args:                cfg.Args,
		AllowEnv:            cfg.AllowEnv,
		Provider:            cfg.Provider,
		Model:               cfg.Model,
		CustomEnv:           cfg.CustomEnv,
		PersonaPrompt:       cfg.PersonaPrompt,
		ApiProvider:         cfg.ApiProvider,
		ApiKey:              cfg.ApiKey,
		ApiBaseUrl:          cfg.ApiBaseUrl,
		GlobalProvider:      cfg.GlobalProvider,
		GlobalProviderEntry: cfg.GlobalProviderEntry,
		Protocol:            cfg.Protocol,
		ContextWindow:       cfg.ContextWindow,
		MaxTokens:           cfg.MaxTokens,
	}
}

func convertToStoreAgentACPConfig(cfg *v1pb.AgentACPConfig) *storepb.AgentACPConfig {
	if cfg == nil {
		return nil
	}
	return &storepb.AgentACPConfig{
		Executable:          cfg.Executable,
		Args:                cfg.Args,
		AllowEnv:            cfg.AllowEnv,
		Provider:            cfg.Provider,
		Model:               cfg.Model,
		CustomEnv:           cfg.CustomEnv,
		PersonaPrompt:       cfg.PersonaPrompt,
		ApiProvider:         cfg.ApiProvider,
		ApiKey:              cfg.ApiKey,
		ApiBaseUrl:          cfg.ApiBaseUrl,
		GlobalProvider:      cfg.GlobalProvider,
		GlobalProviderEntry: cfg.GlobalProviderEntry,
		Protocol:            cfg.Protocol,
		ContextWindow:       cfg.ContextWindow,
		MaxTokens:           cfg.MaxTokens,
	}
}

// isEmptyAgentACPConfig reports whether cfg carries no user configuration — the
// zero value a caller sends when it omits acp_config. CreateAgent treats an
// empty config as "not provided" so the minimal default is used instead of
// validating (and rejecting) an empty provider.
func isEmptyAgentACPConfig(cfg *v1pb.AgentACPConfig) bool {
	return cfg.Executable == "" && len(cfg.Args) == 0 && len(cfg.AllowEnv) == 0 &&
		cfg.Provider == "" && cfg.Model == "" && len(cfg.CustomEnv) == 0 && cfg.PersonaPrompt == "" &&
		cfg.ApiBaseUrl == "" && cfg.GlobalProvider == "" && cfg.GlobalProviderEntry == "" && cfg.Protocol == "" &&
		cfg.ContextWindow == 0 && cfg.MaxTokens == 0
}

// buildCapabilityForACPConfig derives the agent capability from the
// user-configurable ACP settings, branching on the runtime. A pi agent
// (builtin-pi or user-installed "pi") is a non-ACP runtime: its capability
// comes from the pi package (SupportsPi, not SupportsAcp) and does not depend
// on a host-detected executable. Every other provider is an ACP runtime and
// goes through the existing executor.BuildCapability path. This is the single
// place the manager picks a runtime's capability, so the executor package stays
// pi-free (no import cycle: pi already imports executor).
func buildCapabilityForACPConfig(cfg *v1pb.AgentACPConfig) *v1pb.AgentCapability {
	if cfg != nil && pi.IsPiProvider(cfg.GetProvider()) {
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
			Compatible:                p.Compatible,
			IncompatibilityReason:     p.IncompatibilityReason,
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
			Compatible:                p.Compatible,
			IncompatibilityReason:     p.IncompatibilityReason,
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

func convertToV1AgentStatus(status *storepb.AgentStatus, deleted bool, connected bool, enabled bool) *v1pb.AgentStatus {
	if status == nil {
		return nil
	}
	state := computeConnectionState(status, deleted, connected, enabled)

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
// is taken from `connected` (the agent's machine's live MachineChannel in the dispatcher),
// not from status.LastHeartbeatAt (which is no longer written and would always
// read as offline). Deletion and being stopped (StopAgent, enabled=false) are
// lifecycle states that take precedence over the live-stream signal and over
// any stale ERROR/KICKED connection state: a stopped agent is not processing
// sessions even if its machine is still connected or its last connection ended
// in an error.
func computeConnectionState(status *storepb.AgentStatus, deleted bool, connected bool, enabled bool) v1pb.AgentStatus_ConnectionState {
	if deleted {
		return v1pb.AgentStatus_OFFLINE
	}
	if !enabled {
		return v1pb.AgentStatus_STOPPED
	}
	if status.State == storepb.AgentStatus_ERROR {
		return v1pb.AgentStatus_ERROR
	}
	if status.State == storepb.AgentStatus_KICKED {
		return v1pb.AgentStatus_KICKED
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
