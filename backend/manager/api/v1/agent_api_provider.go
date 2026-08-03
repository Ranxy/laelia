package v1

import (
	"context"
	"log/slog"
	"strconv"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

	"github.com/Ranxy/laelia/backend/agent/pi"
	"github.com/Ranxy/laelia/backend/common"
	"github.com/Ranxy/laelia/backend/common/log"
	"github.com/Ranxy/laelia/backend/common/permission"
	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/manager/component/iam"
	"github.com/Ranxy/laelia/backend/manager/store"
)

// canManageAgentKey reports whether the caller may see/set the legacy inline
// api_provider/api_key on an agent's ACP config. Only a caller holding
// agents.edit (workspace admin today) handles the plaintext key; an owner
// without it may edit the agent but must use the global-provider path.
func (s *AgentService) canManageAgentKey(ctx context.Context, user *store.UserMessage, agent *store.AgentMessage) bool {
	if user == nil || s.iam == nil {
		return false
	}
	agentName := common.FormatAgentUID(agent.ResourceID)
	ok, err := s.iam.CheckPermission(ctx, permission.AgentsEdit, user, nil, &iam.ResourceRef{
		ResourceType: storepb.Policy_AGENT,
		Name:         agentName,
	})
	if err != nil {
		slog.Error("failed to resolve agents.edit", slog.String("agent", agentName), log.WithError(err))
		return false
	}
	return ok
}

// canCreateAgentOnMachine reports whether the caller may create an agent on the
// machine: the machine's creator, or a caller holding laelia.agents.create
// (workspace admin today). Machines are created by privileged users, so in
// practice this reaches admins and authorized managers.
func (s *AgentService) canCreateAgentOnMachine(ctx context.Context, user *store.UserMessage, machine *store.MachineMessage) (bool, error) {
	if user == nil {
		return false, nil
	}
	if machine.CreatedBy != 0 && machine.CreatedBy == user.ID {
		return true, nil
	}
	return s.iam.CheckPermission(ctx, permission.AgentsCreate, user, nil, nil)
}

// validateGlobalProviderReference validates a builtin-pi config's global
// provider reference: the provider must exist, the referenced entry must belong
// to it, and the caller must be allowed to use the provider.
func (s *AgentService) validateGlobalProviderReference(ctx context.Context, user *store.UserMessage, cfg *v1pb.AgentACPConfig) error {
	if cfg == nil || cfg.GlobalProvider == "" {
		return nil
	}
	providerResourceID, err := common.GetAPIProviderResourceID(cfg.GlobalProvider)
	if err != nil {
		return connect.NewError(connect.CodeInvalidArgument, errors.Wrap(err, "invalid acp_config.global_provider"))
	}
	provider, err := s.store.GetAPIProviderByResourceID(ctx, providerResourceID)
	if err != nil {
		return connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get global provider"))
	}
	if provider == nil {
		return connect.NewError(connect.CodeInvalidArgument, errors.Errorf("global provider %q not found", cfg.GlobalProvider))
	}
	if cfg.GlobalProviderEntry != "" {
		_, entryID, err := common.ParseAPIProviderEntryName(cfg.GlobalProviderEntry)
		if err != nil {
			return connect.NewError(connect.CodeInvalidArgument, errors.Wrap(err, "invalid acp_config.global_provider_entry"))
		}
		if !providerHasEntry(provider, entryID) {
			return connect.NewError(connect.CodeInvalidArgument, errors.Errorf("entry %q not found in provider %q", cfg.GlobalProviderEntry, cfg.GlobalProvider))
		}
	}
	ok, err := canUseAPIProvider(ctx, s.iam, s.store, user, provider)
	if err != nil {
		return connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to check provider access"))
	}
	if !ok {
		return connect.NewError(connect.CodePermissionDenied, errors.Errorf("you do not have access to global provider %q", cfg.GlobalProvider))
	}
	return nil
}

// resolveAcpConfigForDaemon resolves a stored builtin-pi config to the concrete
// config sent to the agent daemon. A global-provider reference is resolved to
// api_provider/api_key/model from the provider's entry (the key never lives in
// the stored agent config); a legacy inline config passes through unchanged.
// The v1 API surface never calls this for read-back — only the daemon-boundary
// configs (ConnectAgent, dispatcher assignments, machine sync) do.
func resolveAcpConfigForDaemon(ctx context.Context, stores *store.Store, cfg *v1pb.AgentACPConfig) (*v1pb.AgentACPConfig, error) {
	if cfg == nil || cfg.Provider != pi.BuiltinPiProvider || cfg.GlobalProvider == "" {
		return cfg, nil
	}
	providerResourceID, err := common.GetAPIProviderResourceID(cfg.GlobalProvider)
	if err != nil {
		return nil, errors.Wrap(err, "invalid global_provider")
	}
	provider, err := stores.GetAPIProviderByResourceID(ctx, providerResourceID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get global provider")
	}
	if provider == nil {
		return nil, errors.Errorf("global provider %q not found", cfg.GlobalProvider)
	}
	_, entryID, err := common.ParseAPIProviderEntryName(cfg.GlobalProviderEntry)
	if err != nil {
		return nil, errors.Wrap(err, "invalid global_provider_entry")
	}
	var entry *store.APIProviderEntryMessage
	for _, e := range provider.Entries {
		if strconv.Itoa(e.ID) == entryID {
			entry = e
			break
		}
	}
	if entry == nil {
		return nil, errors.Errorf("entry %q not found in provider %q", cfg.GlobalProviderEntry, cfg.GlobalProvider)
	}
	return &v1pb.AgentACPConfig{
		Provider:      cfg.Provider,
		ApiProvider:   provider.ProviderType,
		ApiKey:        entry.APIKey,
		Model:         entry.ModelName,
		PersonaPrompt: cfg.PersonaPrompt,
	}, nil
}

func providerHasEntry(provider *store.APIProviderMessage, entryID string) bool {
	for _, e := range provider.Entries {
		if strconv.Itoa(e.ID) == entryID {
			return true
		}
	}
	return false
}
