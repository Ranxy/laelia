package v1

import (
	"context"
	"strconv"
	"strings"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/laelia/backend/agent/pi"
	"github.com/Ranxy/laelia/backend/common"
	"github.com/Ranxy/laelia/backend/common/permission"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/laelia/backend/manager/component/iam"
	"github.com/Ranxy/laelia/backend/manager/store"
	"github.com/Ranxy/laelia/backend/manager/utils"
)

// providerTypeDefaults maps a provider type to the api_provider id the pi
// runtime expects and the type's default API base URL. A provider type is
// usable by the builtin-pi runtime only when it appears here; the pi runtime
// drives the endpoint internally, so base_url is informational for known types.
var providerTypeDefaults = map[string]struct {
	apiProvider string
	baseURL     string
}{
	"deepseek":   {apiProvider: pi.APIProviderDeepseek, baseURL: "https://api.deepseek.com"},
	"openrouter": {apiProvider: pi.APIProviderOpenRouter, baseURL: "https://openrouter.ai/api/v1"},
}

// APIProviderService manages global LLM API providers. Management RPCs are gated
// by the IAM interceptor with laelia.apiProviders.* (workspaceAdmin or an
// authorized custom role). ListAPIProviders is handler-gated so the agent
// create/edit form can list the providers the caller may use.
type APIProviderService struct {
	v1connect.UnimplementedApiProviderServiceHandler
	store *store.Store
	iam   *iam.Manager
}

// NewAPIProviderService returns a new APIProviderService.
func NewAPIProviderService(s *store.Store, iamManager *iam.Manager) *APIProviderService {
	return &APIProviderService{store: s, iam: iamManager}
}

// GetAPIProvider returns one provider. Management-only (laelia.apiProviders.get).
func (s *APIProviderService) GetAPIProvider(ctx context.Context, req *connect.Request[v1pb.GetApiProviderRequest]) (*connect.Response[v1pb.ApiProvider], error) {
	resourceID, err := common.GetAPIProviderResourceID(req.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	provider, err := s.store.GetAPIProviderByResourceID(ctx, resourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get api provider"))
	}
	if provider == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("api provider %q not found", req.Msg.Name))
	}
	return connect.NewResponse(convertToV1APIProvider(provider)), nil
}

// ListAPIProviders lists providers. It is handler-gated (no IAM annotation): a
// caller holding laelia.apiProviders.list (workspaceAdmin or an authorized
// manager) sees every provider; any other caller sees only the providers they
// may use, so the agent create/edit form can render the dropdown without a
// management permission.
func (s *APIProviderService) ListAPIProviders(ctx context.Context, _ *connect.Request[v1pb.ListApiProvidersRequest]) (*connect.Response[v1pb.ListApiProvidersResponse], error) {
	user, ok := GetUserFromContext(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
	}
	providers, err := s.store.ListAPIProviders(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to list api providers"))
	}

	response := &v1pb.ListApiProvidersResponse{}
	for _, p := range providers {
		ok, err := canUseAPIProvider(ctx, s.iam, s.store, user, p)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to resolve provider access"))
		}
		if !ok {
			continue
		}
		response.ApiProviders = append(response.ApiProviders, convertToV1APIProvider(p))
	}
	return connect.NewResponse(response), nil
}

// CreateAPIProvider creates a provider with its entries and members. Entry
// api keys are required at creation time.
func (s *APIProviderService) CreateAPIProvider(ctx context.Context, req *connect.Request[v1pb.CreateApiProviderRequest]) (*connect.Response[v1pb.ApiProvider], error) {
	in := req.Msg.ApiProvider
	if in == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("api_provider is required"))
	}
	if err := s.validateAPIProviderBase(in); err != nil {
		return nil, err
	}
	user, ok := GetUserFromContext(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
	}

	entries, err := s.buildEntriesForCreate(ctx, in.Entries)
	if err != nil {
		return nil, err
	}
	members, err := validateAndNormalizeAPIProviderMembers(in.Members)
	if err != nil {
		return nil, err
	}

	created, err := s.store.CreateAPIProvider(ctx, &store.APIProviderMessage{
		Name:         strings.TrimSpace(in.Title),
		ProviderType: strings.TrimSpace(in.ProviderType),
		BaseURL:      strings.TrimSpace(in.BaseUrl),
		Description:  strings.TrimSpace(in.Description),
		CreatedBy:    user.ID,
		Entries:      entries,
		Members:      members,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to create api provider"))
	}

	recordAPIProviderChange(ctx, common.FormatAPIProviderUID(created.ResourceID), entryNames(created.Entries), nil)
	return connect.NewResponse(convertToV1APIProvider(created)), nil
}

// UpdateAPIProvider replaces the provider's mutable fields and its entries and
// members (full replace). Masked ("****"-prefixed) api keys on existing entries
// mean "keep the stored key"; removing an entry that agents still reference is
// rejected with FailedPrecondition.
func (s *APIProviderService) UpdateAPIProvider(ctx context.Context, req *connect.Request[v1pb.UpdateApiProviderRequest]) (*connect.Response[v1pb.ApiProvider], error) {
	in := req.Msg.ApiProvider
	if in == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("api_provider is required"))
	}
	if err := validateAPIProviderUpdateMask(req.Msg.UpdateMask.GetPaths()); err != nil {
		return nil, err
	}
	resourceID, err := common.GetAPIProviderResourceID(in.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	current, err := s.store.GetAPIProviderByResourceID(ctx, resourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get api provider"))
	}
	if current == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("api provider %q not found", in.Name))
	}
	if err := s.validateAPIProviderBase(in); err != nil {
		return nil, err
	}

	entries, err := s.buildEntriesForUpdate(ctx, current, in.Entries)
	if err != nil {
		return nil, err
	}
	if err := s.checkRemovedEntryReferences(ctx, current, entries); err != nil {
		return nil, err
	}
	members, err := validateAndNormalizeAPIProviderMembers(in.Members)
	if err != nil {
		return nil, err
	}

	updated, err := s.store.UpdateAPIProvider(ctx, current, &store.APIProviderMessage{
		Name:        strings.TrimSpace(in.Title),
		BaseURL:     strings.TrimSpace(in.BaseUrl),
		Description: strings.TrimSpace(in.Description),
		Entries:     entries,
		Members:     members,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to update api provider"))
	}

	recordAPIProviderChange(ctx, in.Name, entryNames(updated.Entries), removedEntryNames(current, entries))
	return connect.NewResponse(convertToV1APIProvider(updated)), nil
}

// DeleteAPIProvider deletes a provider. Providers still referenced by an agent
// are rejected with FailedPrecondition so the daemon-boundary resolution never
// breaks a machine's agent roster mid-flight.
func (s *APIProviderService) DeleteAPIProvider(ctx context.Context, req *connect.Request[v1pb.DeleteApiProviderRequest]) (*connect.Response[emptypb.Empty], error) {
	resourceID, err := common.GetAPIProviderResourceID(req.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	provider, err := s.store.GetAPIProviderByResourceID(ctx, resourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get api provider"))
	}
	if provider == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("api provider %q not found", req.Msg.Name))
	}
	count, err := s.store.CountAgentsReferencingProvider(ctx, resourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to count referencing agents"))
	}
	if count > 0 {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.Errorf(
			"api provider %q is referenced by %d agent(s); reconfigure them before deleting it", req.Msg.Name, count))
	}
	if err := s.store.DeleteAPIProvider(ctx, resourceID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to delete api provider"))
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

// ListAPIProviderModels lists the models a provider type exposes, proxying the
// provider's model-listing API server-side (key hygiene: the api_key never
// reaches the browser and is never logged or echoed in errors). The endpoint is
// the provider type's fixed URL; a base_url override is not supported for the
// known types (fail-closed against SSRF).
func (*APIProviderService) ListAPIProviderModels(ctx context.Context, req *connect.Request[v1pb.ListApiProviderModelsRequest]) (*connect.Response[v1pb.ListApiProviderModelsResponse], error) {
	spec, ok := providerTypeDefaults[req.Msg.ProviderType]
	if !ok {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("unsupported provider_type %q", req.Msg.ProviderType))
	}
	if baseURL := strings.TrimSpace(req.Msg.BaseUrl); baseURL != "" && baseURL != spec.baseURL {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("base_url override is not supported for provider type %q", req.Msg.ProviderType))
	}
	apiKey := strings.TrimSpace(req.Msg.ApiKey)
	models, err := pi.ListModels(ctx, spec.apiProvider, apiKey)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	response := &v1pb.ListApiProviderModelsResponse{}
	for _, m := range models {
		response.Models = append(response.Models, &v1pb.PiModel{Id: m.ID, Name: m.Name})
	}
	return connect.NewResponse(response), nil
}

// canUseAPIProvider reports whether the caller may use a provider: a caller
// holding laelia.apiProviders.list (workspace admin or an authorized manager)
// may use any provider; otherwise the caller must be a member of the provider's
// member list (users/{uid}, groups/{email|id}, or allUsers), expanded
// single-level like the IAM engine. Shared by the provider list and the agent
// config validation.
func canUseAPIProvider(ctx context.Context, iamChecker *iam.Manager, stores *store.Store, user *store.UserMessage, provider *store.APIProviderMessage) (bool, error) {
	if user == nil {
		return false, nil
	}
	ok, err := iamChecker.CheckPermission(ctx, permission.ApiProvidersList, user, nil, nil)
	if err != nil {
		return false, err
	}
	if ok {
		return true, nil
	}
	for _, member := range provider.Members {
		if utils.MemberContainsUser(ctx, stores, member, user) {
			return true, nil
		}
	}
	return false, nil
}

// validateAPIProviderBase validates the provider identity fields shared by
// create and update.
func (*APIProviderService) validateAPIProviderBase(in *v1pb.ApiProvider) error {
	if strings.TrimSpace(in.ProviderType) == "" {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("provider_type is required"))
	}
	if _, ok := providerTypeDefaults[in.ProviderType]; !ok {
		return connect.NewError(connect.CodeInvalidArgument, errors.Errorf("unsupported provider_type %q (supported: deepseek, openrouter)", in.ProviderType))
	}
	if strings.TrimSpace(in.Title) == "" {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("title is required"))
	}
	return nil
}

// buildEntriesForCreate validates entries for a new provider: each requires a
// model and a real api key (masked sentinels are rejected).
func (*APIProviderService) buildEntriesForCreate(_ context.Context, in []*v1pb.ApiProviderEntry) ([]*store.APIProviderEntryMessage, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]*store.APIProviderEntryMessage, 0, len(in))
	for _, e := range in {
		if strings.TrimSpace(e.Model) == "" {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("entry model is required"))
		}
		key := strings.TrimSpace(e.ApiKey)
		if key == "" || strings.HasPrefix(key, secretMaskPrefix) {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("entry api_key is required for a new provider"))
		}
		out = append(out, &store.APIProviderEntryMessage{
			Label:     strings.TrimSpace(e.Label),
			ModelName: strings.TrimSpace(e.Model),
			APIKey:    key,
		})
	}
	return out, nil
}

// buildEntriesForUpdate resolves the requested entries against the stored ones.
// Existing entries (those carrying a name) keep their stored key when the
// api_key is empty or masked; new entries require a real key. The returned
// entries carry their stored ID when they existed, which feeds the
// reference-protection diff.
func (*APIProviderService) buildEntriesForUpdate(_ context.Context, provider *store.APIProviderMessage, in []*v1pb.ApiProviderEntry) ([]*store.APIProviderEntryMessage, error) {
	storedByID := make(map[string]*store.APIProviderEntryMessage, len(provider.Entries))
	for _, e := range provider.Entries {
		storedByID[strconv.Itoa(e.ID)] = e
	}

	out := make([]*store.APIProviderEntryMessage, 0, len(in))
	for _, e := range in {
		if strings.TrimSpace(e.Model) == "" {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("entry model is required"))
		}
		label := strings.TrimSpace(e.Label)
		model := strings.TrimSpace(e.Model)
		key := strings.TrimSpace(e.ApiKey)
		if e.Name != "" {
			_, entryID, err := common.ParseAPIProviderEntryName(e.Name)
			if err != nil {
				return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid entry name %q", e.Name))
			}
			stored, ok := storedByID[entryID]
			if !ok {
				return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("entry %q does not belong to provider %q", e.Name, provider.ResourceID))
			}
			if key == "" || strings.HasPrefix(key, secretMaskPrefix) {
				key = stored.APIKey
			}
			out = append(out, &store.APIProviderEntryMessage{
				ID:        stored.ID,
				Label:     label,
				ModelName: model,
				APIKey:    key,
			})
		} else {
			if key == "" || strings.HasPrefix(key, secretMaskPrefix) {
				return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("api_key is required for a new entry"))
			}
			out = append(out, &store.APIProviderEntryMessage{
				Label:     label,
				ModelName: model,
				APIKey:    key,
			})
		}
	}
	return out, nil
}

// checkRemovedEntryReferences rejects an update that removes an entry still
// referenced by an agent.
func (s *APIProviderService) checkRemovedEntryReferences(ctx context.Context, provider *store.APIProviderMessage, newEntries []*store.APIProviderEntryMessage) error {
	kept := make(map[string]bool, len(newEntries))
	for _, e := range newEntries {
		if e.ID != 0 {
			kept[strconv.Itoa(e.ID)] = true
		}
	}
	for _, stored := range provider.Entries {
		if kept[strconv.Itoa(stored.ID)] {
			continue
		}
		name := common.FormatAPIProviderEntryName(provider.ResourceID, strconv.Itoa(stored.ID))
		count, err := s.store.CountAgentsReferencingEntry(ctx, name)
		if err != nil {
			return connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to count referencing agents"))
		}
		if count > 0 {
			return connect.NewError(connect.CodeFailedPrecondition, errors.Errorf(
				"entry %q is referenced by %d agent(s); reconfigure them before removing it", name, count))
		}
	}
	return nil
}

// validateAndNormalizeAPIProviderMembers checks the IAM member format of each
// member and deduplicates.
func validateAndNormalizeAPIProviderMembers(in []string) ([]string, error) {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, m := range in {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		if m != common.AllUsers && !strings.HasPrefix(m, common.UserNamePrefix) && !strings.HasPrefix(m, common.GroupPrefix) {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf(
				"invalid member %q: must be users/{...}, groups/{...}, or %q", m, common.AllUsers))
		}
		if seen[m] {
			continue
		}
		seen[m] = true
		out = append(out, m)
	}
	return out, nil
}

// validateAPIProviderUpdateMask restricts the update mask to the mutable fields.
// An empty mask updates everything mutable (entries/members are full-replace).
func validateAPIProviderUpdateMask(paths []string) error {
	allowed := map[string]bool{
		"title":       true,
		"base_url":    true,
		"description": true,
		"entries":     true,
		"members":     true,
	}
	for _, p := range paths {
		if !allowed[p] {
			return connect.NewError(connect.CodeInvalidArgument, errors.Errorf("update_mask path %q is not supported", p))
		}
	}
	return nil
}

// convertToV1APIProvider converts a stored provider to the v1 view. Api keys are
// masked; the key itself never crosses the API.
func convertToV1APIProvider(p *store.APIProviderMessage) *v1pb.ApiProvider {
	out := &v1pb.ApiProvider{
		Name:         common.FormatAPIProviderUID(p.ResourceID),
		ProviderType: p.ProviderType,
		Title:        p.Name,
		BaseUrl:      p.BaseURL,
		Description:  p.Description,
		CreatedAt:    timestamppb.New(p.CreatedAt),
		UpdatedAt:    timestamppb.New(p.UpdatedAt),
		CreatedBy:    common.FormatUserUID(p.CreatedBy),
	}
	for _, e := range p.Entries {
		out.Entries = append(out.Entries, &v1pb.ApiProviderEntry{
			Name:         common.FormatAPIProviderEntryName(p.ResourceID, strconv.Itoa(e.ID)),
			Label:        e.Label,
			Model:        e.ModelName,
			HasApiKey:    e.APIKey != "",
			MaskedApiKey: maskSecret(e.APIKey),
		})
	}
	out.Members = append(out.Members, p.Members...)
	return out
}

// entryNames returns the resource names of the given entries.
func entryNames(entries []*store.APIProviderEntryMessage) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, strconv.Itoa(e.ID))
	}
	return out
}

// removedEntryNames returns the stored entries that are absent from the new set
// (by id), for the audit delta.
func removedEntryNames(provider *store.APIProviderMessage, newEntries []*store.APIProviderEntryMessage) []string {
	kept := make(map[int]bool, len(newEntries))
	for _, e := range newEntries {
		kept[e.ID] = true
	}
	var out []string
	for _, stored := range provider.Entries {
		if !kept[stored.ID] {
			out = append(out, strconv.Itoa(stored.ID))
		}
	}
	return out
}

// recordAPIProviderChange attaches a masked change summary to the audit record
// the interceptor writes. It carries entry names only — never api keys.
func recordAPIProviderChange(ctx context.Context, provider string, added, removed []string) {
	setServiceData, ok := common.GetSetServiceDataFromContext(ctx)
	if !ok {
		return
	}
	a, err := anypb.New(&v1pb.ApiProviderChange{
		Provider:       provider,
		EntriesAdded:   added,
		EntriesRemoved: removed,
	})
	if err != nil {
		return
	}
	setServiceData(a)
}
