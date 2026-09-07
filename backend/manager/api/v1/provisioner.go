package v1

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"regexp"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/laelia/backend/common"
	"github.com/Ranxy/laelia/backend/common/permission"
	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/laelia/backend/manager/api/auth"
	"github.com/Ranxy/laelia/backend/manager/component/dispatcher"
	"github.com/Ranxy/laelia/backend/manager/component/iam"
	"github.com/Ranxy/laelia/backend/manager/component/machinebuild"
	"github.com/Ranxy/laelia/backend/manager/component/provision"
	"github.com/Ranxy/laelia/backend/manager/config"
	"github.com/Ranxy/laelia/backend/manager/store"
)

// knownProvisionerBackends are the backend types this manager can drive
// provisioned machines against. The registry (CreateProvisioner) accepts any
// backend name so future backends register today, but ProvisionMachine fails
// fast until the backend is implemented on the provisioner side (design §9).
var knownProvisionerBackends = map[string]bool{
	"kubernetes": true,
}

// defaultBinaryTarget is the machine binary target provisioned pods install
// when the ProvisioningSetting leaves binary_target empty (design §6.5).
const defaultBinaryTarget = "linux-x64"

// maxCustomImageLength bounds a user-provided runtime image reference.
const maxCustomImageLength = 512

// customImageRefPattern constrains custom runtime image references (and
// allowlist entries, which may end in a "*" prefix wildcard) to safe
// characters: no whitespace, quotes, shell metacharacters, or control
// characters.
var customImageRefPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:@/-]*\*?$`)

// validateCustomImageRef checks the shape of one custom runtime image
// reference (a ProvisionMachineRequest.runtime_image or an allowlist entry).
// Policy (the allowlist) is a separate check against the workspace setting.
func validateCustomImageRef(image string) error {
	if image == "" {
		return errors.New("runtime image is empty")
	}
	if len(image) > maxCustomImageLength {
		return errors.Errorf("runtime image exceeds %d characters", maxCustomImageLength)
	}
	if !customImageRefPattern.MatchString(image) {
		return errors.Errorf("runtime image %q is not a valid container image reference", image)
	}
	return nil
}

// canonicalDockerRegistry is the canonical host for references without an
// explicit registry (Docker Hub), matching what `docker images` displays.
const canonicalDockerRegistry = "docker.io"

// splitImageRef splits a reference's explicit (or implied) registry host from
// its repository path, applying Docker's defaults: a first path component
// without a dot/colon (and not "localhost") is a Docker Hub namespace, and a
// single-component reference is a Docker Hub library image.
func splitImageRef(ref string) (registry, repo string) {
	slash := strings.Index(ref, "/")
	if slash < 0 {
		return canonicalDockerRegistry, "library/" + ref
	}
	first, rest := ref[:slash], ref[slash+1:]
	if strings.ContainsAny(first, ".:") || first == "localhost" {
		return first, rest
	}
	return canonicalDockerRegistry, ref
}

// normalizeImageRef canonicalizes a container image reference the way Docker
// resolves defaults: an implicit registry becomes docker.io, a single-path
// component gains the library/ prefix (Docker Hub official images), and a
// missing tag becomes :latest (never when a digest is present). A trailing "*"
// (allowlist prefix pattern) is kept verbatim after canonicalizing its literal
// head — no :latest defaulting, since the wildcard may stand for the tag or
// any later component.
func normalizeImageRef(image string) string {
	if image == "" {
		return ""
	}
	pattern := ""
	if strings.HasSuffix(image, "*") {
		pattern = "*"
		image = strings.TrimSuffix(image, "*")
		if image == "" {
			return pattern
		}
	}

	registry, repo := splitImageRef(image)
	if pattern != "" {
		return registry + "/" + repo + pattern
	}

	name := repo
	tag := ""
	digest := ""
	if at := strings.Index(repo, "@"); at >= 0 {
		name, digest = repo[:at], repo[at+1:]
	} else if colon := strings.LastIndex(repo, ":"); colon >= 0 {
		// After the registry split, a colon can only be a tag separator
		// (ports only appear in the registry host).
		name, tag = repo[:colon], repo[colon+1:]
	}

	canonical := registry + "/" + name
	switch {
	case digest != "":
		canonical += "@" + digest
	case tag != "":
		canonical += ":" + tag
	default:
		canonical += ":latest"
	}
	return canonical
}

// customImageAllowed reports whether image matches the workspace custom-image
// allowlist. Both the image and the entries are canonicalized first
// (normalizeImageRef), so Docker's implicit defaults never split one image
// across differently-written allowlist entries: "ubuntu:22.04" matches
// "docker.io/library/*". An entry matches exactly, or as a prefix pattern
// when it ends with "*" (e.g. "registry.example.com/team/*"). An empty
// allowlist disables custom images entirely.
func customImageAllowed(image string, allowlist []string) bool {
	canonical := normalizeImageRef(image)
	for _, entry := range allowlist {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		canonicalEntry := normalizeImageRef(entry)
		if pattern, ok := strings.CutSuffix(canonicalEntry, "*"); ok {
			if strings.HasPrefix(canonical, pattern) {
				return true
			}
			continue
		}
		if canonicalEntry == canonical {
			return true
		}
	}
	return false
}

// normalizeCustomImageAllowlist trims each entry, drops empties, canonicalizes
// it (normalizeImageRef — so "ubuntu" and "docker.io/library/ubuntu:latest" are
// the same entry), and dedupes. Returns nil when nothing remains, so a cleared
// allowlist round-trips as an empty list.
func normalizeCustomImageAllowlist(entries []string) []string {
	seen := make(map[string]struct{}, len(entries))
	var out []string
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		entry = normalizeImageRef(entry)
		if _, ok := seen[entry]; ok {
			continue
		}
		seen[entry] = struct{}{}
		out = append(out, entry)
	}
	return out
}

// ProvisionerService implements ProvisionerService: admin RPCs managing the
// provisioner registry (create with one-time token mint, rotate, delete with
// the machines-bound guard) and the user-facing ProvisionMachine that creates
// a machine and enqueues its provisioning job (design §6).
type ProvisionerService struct {
	v1connect.UnimplementedProvisionerServiceHandler
	store      *store.Store
	secret     string
	profile    *config.Profile
	dispatcher *dispatcher.Dispatcher
	iam        *iam.Manager
}

func NewProvisionerService(s *store.Store, secret string, profile *config.Profile, d *dispatcher.Dispatcher, iamManager *iam.Manager) *ProvisionerService {
	return &ProvisionerService{
		store:      s,
		secret:     secret,
		profile:    profile,
		dispatcher: d,
		iam:        iamManager,
	}
}

// isProvisionerCreator reports whether the caller created the provisioner.
func isProvisionerCreator(user *store.UserMessage, p *store.ProvisionerMessage) bool {
	return user != nil && p.CreatedBy != 0 && p.CreatedBy == user.ID
}

// isProvisionerAdmin reports whether the caller may manage a provisioner's IAM
// policy: the provisioner's creator or a workspace admin (laelia.provisioners.delete,
// the admin-tier management permission). Mirrors machine's isMachineAdmin
// (creator or laelia.machines.edit). Fail-closed.
func isProvisionerAdmin(ctx context.Context, im *iam.Manager, user *store.UserMessage, p *store.ProvisionerMessage) bool {
	if isProvisionerCreator(user, p) {
		return true
	}
	if user == nil || im == nil {
		return false
	}
	ok, err := im.CheckPermission(ctx, permission.ProvisionersDelete, user, nil, nil)
	if err != nil {
		return false
	}
	return ok
}

// canProvisionOnProvisioner reports whether the caller may create a machine on
// the provisioner: a workspace-scope holder of laelia.provisioners.provision
// (workspaceAdmin or roles/machineProvisioner) or a principal bound to
// roles/provisionerMachineCreator in the provisioner's IAM policy.
// CheckPermission consults the workspace scope first (coarse grant short
// circuits) and then the per-provisioner policy, so one call expresses both
// the coarse and fine paths. Fail-closed: a nil caller, nil manager, or a
// lookup error denies.
func canProvisionOnProvisioner(ctx context.Context, im *iam.Manager, user *store.UserMessage, p *store.ProvisionerMessage) bool {
	if user == nil || im == nil {
		return false
	}
	ok, err := im.CheckPermission(ctx, permission.ProvisionersProvision, user, nil,
		&iam.ResourceRef{ResourceType: storepb.Policy_PROVISIONER, Name: common.FormatProvisionerUID(p.ResourceID)})
	return err == nil && ok
}

// CreateProvisioner registers a provisioner and mints its one-time token. The
// store keeps only token_version; the plaintext token exists in this response
// alone.
func (s *ProvisionerService) CreateProvisioner(ctx context.Context, req *connect.Request[v1pb.CreateProvisionerRequest]) (*connect.Response[v1pb.CreateProvisionerResponse], error) {
	in := req.Msg.GetProvisioner()
	if in == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("provisioner is required"))
	}
	title := strings.TrimSpace(in.GetTitle())
	if title == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("provisioner.title is required"))
	}
	backend := strings.TrimSpace(in.GetBackend())
	if backend == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("provisioner.backend is required"))
	}

	caller, _ := GetUserFromContext(ctx)
	createdBy := 0
	if caller != nil {
		createdBy = caller.ID
	}

	created, err := s.store.CreateProvisioner(ctx, &store.ProvisionerMessage{
		Name:         title,
		Backend:      backend,
		Description:  in.GetDescription(),
		TokenVersion: 1,
		CreatedBy:    createdBy,
		Status:       &storepb.ProvisionerStatus{},
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to create provisioner"))
	}

	token, err := auth.GenerateProvisionerToken(created.Name, created.ResourceID, created.TokenVersion, s.profile.Mode, s.secret)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to generate provisioner token"))
	}

	return connect.NewResponse(&v1pb.CreateProvisionerResponse{
		Provisioner: s.convertToProvisioner(ctx, created, 0),
		Token:       token,
	}), nil
}

func (s *ProvisionerService) ListProvisioners(ctx context.Context, req *connect.Request[v1pb.ListProvisionersRequest]) (*connect.Response[v1pb.ListProvisionersResponse], error) {
	offset, err := parseLimitAndOffset(&pageSize{
		token:   req.Msg.PageToken,
		limit:   int(req.Msg.PageSize),
		maximum: 200,
	})
	if err != nil {
		return nil, err
	}

	provisioners, err := s.store.ListProvisioners(ctx, &store.FindProvisionerMessage{ShowDeleted: req.Msg.ShowDeleted})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to list provisioners"))
	}

	// Visibility follows the create-machine rule: a caller only sees the
	// provisioners they may create a machine on (coarse workspace grant or a
	// per-provisioner roles/provisionerMachineCreator binding). Workspace admins
	// and machineProvisioner holders see every provisioner.
	caller, _ := GetUserFromContext(ctx)
	visible := make([]*store.ProvisionerMessage, 0, len(provisioners))
	for _, p := range provisioners {
		if canProvisionOnProvisioner(ctx, s.iam, caller, p) {
			visible = append(visible, p)
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
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to marshal next page token"))
		}
	}

	// One batched live-machine count for the whole page (machine_count badge).
	ids := make([]int, 0, len(page))
	for _, p := range page {
		ids = append(ids, p.ID)
	}
	counts, err := s.store.CountMachinesByProvisioner(ctx, ids)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to count provisioner machines"))
	}

	resp := &v1pb.ListProvisionersResponse{NextPageToken: nextPageToken}
	for _, p := range page {
		resp.Provisioners = append(resp.Provisioners, s.convertToProvisioner(ctx, p, counts[p.ID]))
	}
	return connect.NewResponse(resp), nil
}

func (s *ProvisionerService) GetProvisioner(ctx context.Context, req *connect.Request[v1pb.GetProvisionerRequest]) (*connect.Response[v1pb.Provisioner], error) {
	provisioner, err := s.getProvisionerByName(ctx, req.Msg.GetName())
	if err != nil {
		return nil, err
	}
	// Visibility follows the create-machine rule: an invisible provisioner is
	// indistinguishable from a missing one (NotFound), so existence is not
	// leaked to users without access to create machines on it.
	caller, _ := GetUserFromContext(ctx)
	if !canProvisionOnProvisioner(ctx, s.iam, caller, provisioner) {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("provisioner %s not found", provisioner.ResourceID))
	}
	counts, err := s.store.CountMachinesByProvisioner(ctx, []int{provisioner.ID})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to count provisioner machines"))
	}
	return connect.NewResponse(s.convertToProvisioner(ctx, provisioner, counts[provisioner.ID])), nil
}

// RotateProvisionerToken bumps the token version (the old token dies at its
// next use), mints a replacement, and closes the live stream with a disconnect
// notice so the old connection does not linger.
func (s *ProvisionerService) RotateProvisionerToken(ctx context.Context, req *connect.Request[v1pb.RotateProvisionerTokenRequest]) (*connect.Response[v1pb.RotateProvisionerTokenResponse], error) {
	provisioner, err := s.getProvisionerByName(ctx, req.Msg.GetName())
	if err != nil {
		return nil, err
	}

	newVersion := provisioner.TokenVersion + 1
	updated, err := s.store.UpdateProvisioner(ctx, provisioner, &store.UpdateProvisionerMessage{TokenVersion: &newVersion})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to rotate provisioner token version"))
	}

	token, err := auth.GenerateProvisionerToken(updated.Name, updated.ResourceID, updated.TokenVersion, s.profile.Mode, s.secret)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to generate provisioner token"))
	}

	s.closeProvisionerStream(updated.ID, false)
	return connect.NewResponse(&v1pb.RotateProvisionerTokenResponse{
		Provisioner: s.convertToProvisioner(ctx, updated, -1),
		Token:       token,
	}), nil
}

// DeleteProvisioner soft-deletes the provisioner. Refused while any live
// machine still references it: the machines' workloads must never silently
// become unmanaged. The stream is closed and the token version bumped, so a
// still-connected provisioner loses its credential immediately.
func (s *ProvisionerService) DeleteProvisioner(ctx context.Context, req *connect.Request[v1pb.DeleteProvisionerRequest]) (*connect.Response[emptypb.Empty], error) {
	provisioner, err := s.getProvisionerByName(ctx, req.Msg.GetName())
	if err != nil {
		return nil, err
	}

	counts, err := s.store.CountMachinesByProvisioner(ctx, []int{provisioner.ID})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to count provisioner machines"))
	}
	if n := counts[provisioner.ID]; n > 0 {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.Errorf("provisioner %q still has %d machine(s) bound; delete or reassign them first", provisioner.Name, n))
	}

	s.closeProvisionerStream(provisioner.ID, true)

	newVersion := provisioner.TokenVersion + 1
	deleted := true
	if _, err := s.store.UpdateProvisioner(ctx, provisioner, &store.UpdateProvisionerMessage{
		TokenVersion: &newVersion,
		Delete:       &deleted,
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to delete provisioner"))
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

// getProvisionerByName resolves a provisioners/{id} resource name to a live
// (non-deleted) provisioner row.
func (s *ProvisionerService) getProvisionerByName(ctx context.Context, name string) (*store.ProvisionerMessage, error) {
	resourceID, err := common.GetProvisionerResourceID(name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	provisioner, err := s.store.GetProvisionerByResourceID(ctx, resourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get provisioner"))
	}
	if provisioner == nil || provisioner.Deleted {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("provisioner %s not found", resourceID))
	}
	return provisioner, nil
}

// closeProvisionerStream warns a connected provisioner and tears its stream
// down (token rotated or provisioner deleted). Best-effort: the version bump
// already invalidated the credential. deleted tells the operator to scale its
// own Deployment to 0 (stop crash-looping) rather than merely exit.
func (s *ProvisionerService) closeProvisionerStream(provisionerID int, deleted bool) {
	if s.dispatcher == nil {
		return
	}
	reason := "token_rotated"
	if deleted {
		reason = "provisioner_deleted"
	}
	if s.dispatcher.IsProvisionerConnected(provisionerID) {
		if err := s.dispatcher.SendProvisionerDisconnectNotice(provisionerID, reason, deleted); err != nil {
			slog.Warn("failed to send provisioner disconnect notice", "provisionerID", provisionerID, "error", err)
		}
	}
	s.dispatcher.UnregisterProvisioner(provisionerID)
}

// convertToProvisioner builds the v1 Provisioner resource. machineCount < 0
// skips the live-machine count (callers that just need the row, e.g. rotate).
func (s *ProvisionerService) convertToProvisioner(ctx context.Context, p *store.ProvisionerMessage, machineCount int) *v1pb.Provisioner {
	out := &v1pb.Provisioner{
		Name:        common.FormatProvisionerUID(p.ResourceID),
		Title:       p.Name,
		Backend:     p.Backend,
		Description: p.Description,
		CreatedAt:   timestamppb.New(p.CreatedAt),
	}
	if p.CreatedBy != 0 {
		out.CreatedBy = resolveUserResource(ctx, s.store, p.CreatedBy)
	}
	if machineCount >= 0 {
		out.MachineCount = int32(machineCount)
	}
	if st := p.Status; st != nil {
		status := &v1pb.ProvisionerStatus{
			Connected:    st.Connected,
			Version:      st.Version,
			Backend:      st.Backend,
			AutoUpgrade:  st.AutoUpgrade,
			RetainData:   st.RetainData,
			ConfigDigest: st.ConfigDigest,
			// The persisted schema already passed catalog validation at
			// Ready time, so the type lookup always succeeds; an unknown key
			// (impossible to persist) would be skipped rather than shown.
			MachineParams: convertToV1MachineParams(st.GetMachineParams()),
		}
		if st.LastSeen > 0 {
			status.LastSeen = timestamppb.New(time.Unix(st.LastSeen, 0))
		}
		out.Status = status
	}
	return out
}

// convertToV1MachineParams maps the persisted schema into its v1 form,
// resolving each key's value type from the manager catalog.
func convertToV1MachineParams(specs []*storepb.MachineParamSpec) []*v1pb.MachineParamSpec {
	if len(specs) == 0 {
		return nil
	}
	out := make([]*v1pb.MachineParamSpec, 0, len(specs))
	for _, spec := range specs {
		typ, ok := provision.CatalogType(spec.GetKey())
		if !ok {
			// Unpersistable by construction (the schema validator drops
			// unknown keys); skip defensively instead of surfacing it.
			slog.Warn("persisted machine parameter has no catalog type; skipping", "key", spec.GetKey())
			continue
		}
		out = append(out, &v1pb.MachineParamSpec{
			Key:          spec.GetKey(),
			Type:         v1pb.MachineParamType(typ),
			Required:     spec.GetRequired(),
			DefaultValue: spec.GetDefaultValue(),
			MinValue:     spec.GetMinValue(),
			MaxValue:     spec.GetMaxValue(),
			Options:      spec.GetOptions(),
		})
	}
	return out
}

// ---- ProvisionMachine ----

// ProvisionMachine creates a machine owned by `owner` (defaults to the caller;
// naming another user requires a workspace admin) and enqueues its provisioning
// job on the named provisioner — the headless twin of the device-flow approval
// (design §6.2). The machine appears immediately (OFFLINE, provisioning at
// PENDING); the provisioner creates the workload and the pod connects as a
// normal machine without any further user action.
func (s *ProvisionerService) ProvisionMachine(ctx context.Context, req *connect.Request[v1pb.ProvisionMachineRequest]) (*connect.Response[v1pb.Machine], error) {
	title := strings.TrimSpace(req.Msg.GetTitle())
	if title == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("title is required"))
	}

	provisioner, err := s.getProvisionerByName(ctx, req.Msg.GetProvisioner())
	if err != nil {
		return nil, err
	}
	if !knownProvisionerBackends[provisioner.Backend] {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.Errorf("provisioner backend %q is not implemented yet; known backends: kubernetes", provisioner.Backend))
	}
	// A machine can only be created on a connected provisioner: an offline one
	// would park the job at PENDING with no worker to pick it up, so fail fast
	// instead of enqueuing work nothing executes.
	if s.dispatcher == nil || !s.dispatcher.IsProvisionerConnected(provisioner.ID) {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.Errorf("provisioner %q is offline; connect it before provisioning a machine", provisioner.Name))
	}

	owner, err := s.resolveProvisionedMachineOwner(ctx, req.Msg.GetOwner())
	if err != nil {
		return nil, err
	}

	// Per-machine parameters: validated against the manager catalog and this
	// provisioner's reported schema (fresh — the connected check above
	// guarantees the Ready frame was just stamped), then persisted on the
	// machine so replayed jobs rebuild the same workload.
	machineParams, err := provision.ValidateMachineParams(
		provisioner.Status.GetMachineParams(), req.Msg.GetMachineParams())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	// The pod downloads the machine binary from this manager at boot; without
	// embedded binaries (dev build) or without the configured target there is
	// nothing to install — fail fast instead of stranding the pod.
	provisioning, err := s.store.GetProvisioningSetting(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get provisioning setting"))
	}
	// A custom runtime image replaces the workspace default for this machine
	// alone, behind two switches: the feature master switch, and (when on)
	// the optional allowlist enforcement.
	customImage := strings.TrimSpace(req.Msg.GetRuntimeImage())
	if customImage != "" {
		if !provisioning.GetAllowCustomImages() {
			return nil, connect.NewError(connect.CodeFailedPrecondition,
				errors.New("custom runtime images are disabled; clear the custom image or ask a workspace admin to enable it"))
		}
		if err := validateCustomImageRef(customImage); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		if provisioning.GetCustomImageAllowlistEnabled() && !customImageAllowed(customImage, provisioning.GetCustomImageAllowlist()) {
			msg := fmt.Sprintf("custom runtime image %q is not allowed; ask a workspace admin to add it to the custom image allowlist", customImage)
			if canonical := normalizeImageRef(customImage); canonical != customImage {
				// Docker's implicit defaults (docker.io/library/..., :latest)
				// are what the admin must allowlist; surface the canonical
				// form so the right entry is obvious.
				msg = fmt.Sprintf("custom runtime image %q (canonical form %q) is not allowed; ask a workspace admin to add it to the custom image allowlist", customImage, canonical)
			}
			return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New(msg))
		}
		// Persist the canonical form so the pod spec, replayed jobs, and the
		// UI all show the fully-qualified image.
		customImage = normalizeImageRef(customImage)
	}
	if customImage == "" && strings.TrimSpace(provisioning.GetRuntimeImage()) == "" {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.New("no runtime image configured; set it under Settings (machine provisioning) first"))
	}
	if machinebuild.LatestVersion() == "" {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.New("this manager embeds no machine binaries; provisioned pods would have nothing to download"))
	}
	binaryTarget := strings.TrimSpace(provisioning.GetBinaryTarget())
	if binaryTarget == "" {
		binaryTarget = defaultBinaryTarget
	}
	if _, ok := machinebuild.GetTarget(binaryTarget); !ok {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.Errorf("no embedded machine binary for target %q", binaryTarget))
	}

	// The fingerprint binds the refresh token to this exact pod identity
	// (deterministic, recomputable at replay; the pod receives it via env).
	resourceID := uuid.NewString()
	fingerprint := provision.MachineFingerprint(provisioner.ResourceID, resourceID)

	now := time.Now()
	refreshToken, err := auth.GenerateMachineTokenWithFamily(title, resourceID, 1, auth.TokenTypeRefresh, resourceID, s.profile.Mode, s.secret, machineRefreshTokenDuration)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to generate machine refresh token"))
	}

	created, err := s.store.CreateMachineWithToken(ctx, resourceID, &store.MachineMessage{
		Name:         title,
		TokenVersion: 1,
		Info: &storepb.MachineInfo{
			// Replaced by real values when the machine connects, like every
			// machine; the seeded hostname doubles as the planned workload name.
			Hostname: provision.MachineWorkloadName(resourceID),
			Os:       "linux",
			Arch:     "amd64",
			Labels: map[string]string{
				"provisioner": provisioner.Name,
				"owner":       owner.Handle,
			},
		},
		Status:        &storepb.MachineStatus{},
		CreatedBy:     owner.ID,
		ProvisionerID: provisioner.ID,
		Provisioning: &storepb.ProvisioningStatus{
			Phase:     storepb.ProvisioningPhase_PROVISIONING_PHASE_PENDING,
			PendingAt: now.Unix(),
			// The user-provided image (empty = workspace default) is persisted
			// on the row so replayed jobs compose the same workload.
			RuntimeImage:  customImage,
			MachineParams: machineParams,
		},
	}, &store.MachineTokenMessage{
		TokenHash:   hashToken(refreshToken),
		TokenType:   storepb.MachineTokenType_MACHINE_REFRESH,
		TokenFamily: resourceID,
		State:       storepb.MachineTokenState_MACHINE_TOKEN_ACTIVE,
		Fingerprint: fingerprint,
		ExpiresAt:   now.Add(machineRefreshTokenDuration),
		CreatedBy:   owner.Handle,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to create provisioned machine"))
	}

	if err := deliverProvisionJob(ctx, s.store, s.dispatcher, provisioner, created, refreshToken); err != nil {
		// The machine row exists and the job is recorded at PENDING; a push or
		// composition failure must not roll the machine back — the job is
		// replayed on the provisioner's next connect. Surface the reason.
		slog.Error("failed to deliver provision job; parked at PENDING",
			"machine", created.ResourceID, "provisioner", provisioner.ResourceID, "error", err)
	}

	return connect.NewResponse(convertMachineRecord(ctx, s.store, s.dispatcher, created)), nil
}

// provisionedMachineOwner is the resolved owner of a to-be-provisioned
// machine: the caller (default) or an explicit target user (workspace admins
// only).
type provisionedMachineOwner struct {
	ID     int
	Handle string
}

// resolveProvisionedMachineOwner resolves the request's owner field: empty
// means the caller; naming another user requires a workspace admin (same
// admin-tier check as machine ownership transfer).
func (s *ProvisionerService) resolveProvisionedMachineOwner(ctx context.Context, ownerName string) (*provisionedMachineOwner, error) {
	caller, _ := GetUserFromContext(ctx)
	if caller == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("sign in required to provision a machine"))
	}
	if strings.TrimSpace(ownerName) == "" {
		return &provisionedMachineOwner{ID: caller.ID, Handle: caller.Handle}, nil
	}

	if s.iam != nil {
		ok, err := s.iam.CheckPermission(ctx, permission.MachinesEdit, caller, nil, nil)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to check workspace admin permission"))
		}
		if !ok {
			return nil, connect.NewError(connect.CodePermissionDenied,
				errors.New("only a workspace admin can provision a machine for another user"))
		}
	}

	handle, err := common.GetUserHandle(ownerName)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Errorf("invalid owner %q: %v", ownerName, err))
	}
	target, err := s.store.GetUserByHandle(ctx, handle)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to look up owner"))
	}
	if target == nil || target.MemberDeleted {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("owner user %s not found", ownerName))
	}
	return &provisionedMachineOwner{ID: target.ID, Handle: target.Handle}, nil
}

// ---- Job composition + delivery (shared with the reconnect replay) ----

// composeProvisionMachineJob builds the ProvisionMachineJob for one machine:
// credentials, endpoints, the runtime image, and the rendered bootstrap script.
// The refresh token travels in the job (decision #2: the provisioner — a
// trusted enterprise component — stores it in a backend secret).
func composeProvisionMachineJob(
	ctx context.Context,
	st *store.Store,
	provisioner *store.ProvisionerMessage,
	machine *store.MachineMessage,
	refreshToken string,
) (*v1pb.ProvisionMachineJob, error) {
	provisioning, err := st.GetProvisioningSetting(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get provisioning setting")
	}
	runtimeImage := resolveProvisionRuntimeImage(machine.Provisioning.GetRuntimeImage(), provisioning.GetRuntimeImage())
	if runtimeImage == "" {
		return nil, errors.New("no runtime image configured; set it under Settings (machine provisioning) first")
	}
	binaryTarget := strings.TrimSpace(provisioning.GetBinaryTarget())
	if binaryTarget == "" {
		binaryTarget = defaultBinaryTarget
	}
	entry, ok := machinebuild.GetTarget(binaryTarget)
	if !ok {
		return nil, errors.Errorf("no embedded machine binary for target %q", binaryTarget)
	}

	managerURL, err := resolveManagerURL(ctx, st)
	if err != nil {
		return nil, err
	}

	bootstrapScript, err := provision.RenderBootstrapScript(provision.BootstrapParams{
		ManagerURL:   managerURL,
		BinaryTarget: binaryTarget,
		GzSha256:     entry.Gz.Sha256,
		Sha256:       entry.Sha256,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to render bootstrap script")
	}

	ownerHandle := resolveUserHandle(ctx, st, machine.CreatedBy)
	return &v1pb.ProvisionMachineJob{
		Machine:         common.FormatMachineUID(machine.ResourceID),
		Title:           machine.Name,
		OwnerHandle:     ownerHandle,
		RefreshToken:    refreshToken,
		Fingerprint:     provision.MachineFingerprint(provisioner.ResourceID, machine.ResourceID),
		ManagerUrl:      managerURL,
		RuntimeImage:    runtimeImage,
		BinaryTarget:    binaryTarget,
		MachineLabels:   map[string]string{"provisioner": provisioner.Name, "owner": ownerHandle},
		BootstrapScript: bootstrapScript,
		// Verbatim from the machine row (Appendix A finding A5): replayed
		// jobs rebuild the workload the user asked for even if the schema
		// shrank in between. Never re-validated here.
		MachineParams: machine.Provisioning.GetMachineParams(),
	}, nil
}

// resolveProvisionRuntimeImage picks a provisioning job's runtime image: the
// machine's persisted custom image (set at provision time) wins over the
// workspace default, so replayed jobs rebuild the same workload even if the
// setting or allowlist changed in between.
func resolveProvisionRuntimeImage(machineImage, workspaceImage string) string {
	if image := strings.TrimSpace(machineImage); image != "" {
		return image
	}
	return strings.TrimSpace(workspaceImage)
}

// resolveManagerURL resolves the manager URL pods use to download binaries
// from and connect to: the workspace external URL. There is no request context
// on the replay path, so the setting is the only source; without it
// provisioning cannot work.
func resolveManagerURL(ctx context.Context, st *store.Store) (string, error) {
	setting, err := st.GetWorkspaceGeneralSetting(ctx)
	if err != nil {
		return "", errors.Wrap(err, "failed to get workspace profile setting")
	}
	url := strings.TrimRight(setting.GetExternalUrl(), "/")
	if url == "" {
		return "", errors.New("the workspace external URL is not configured; set it under Settings so pods can reach this manager")
	}
	return url, nil
}

// deliverProvisionJob pushes the provisioning job to the provisioner when it
// is connected; otherwise the job stays parked at its recorded phase and is
// replayed on the provisioner's next connect (design §4.3).
func deliverProvisionJob(
	ctx context.Context,
	st *store.Store,
	d *dispatcher.Dispatcher,
	provisioner *store.ProvisionerMessage,
	machine *store.MachineMessage,
	refreshToken string,
) error {
	job, err := composeProvisionMachineJob(ctx, st, provisioner, machine, refreshToken)
	if err != nil {
		return err
	}
	if d == nil || !d.IsProvisionerConnected(provisioner.ID) {
		return nil
	}
	return d.SendProvisionMachineJob(provisioner.ID, job)
}

// mintProvisionedMachineRefreshToken re-mints a machine's refresh token during
// job replay. The plaintext token is only ever held in memory (the store keeps
// the hash), so a replayed job carries a fresh credential bound to the same
// deterministic fingerprint. The new token is stored alongside the old one —
// no version bump — because the pod may already hold the earlier one and both
// expire on the normal rolling schedule (design §4.3, replay note).
func mintProvisionedMachineRefreshToken(
	ctx context.Context,
	st *store.Store,
	secret string,
	mode common.ReleaseMode,
	machine *store.MachineMessage,
	fingerprint string,
) (string, error) {
	refreshToken, err := auth.GenerateMachineTokenWithFamily(machine.Name, machine.ResourceID, machine.TokenVersion, auth.TokenTypeRefresh, machine.ResourceID, mode, secret, machineRefreshTokenDuration)
	if err != nil {
		return "", errors.Wrap(err, "failed to generate machine refresh token")
	}
	if err := st.CreateMachineToken(ctx, &store.MachineTokenMessage{
		MachineID:   machine.ID,
		TokenHash:   hashToken(refreshToken),
		TokenType:   storepb.MachineTokenType_MACHINE_REFRESH,
		TokenFamily: machine.ResourceID,
		State:       storepb.MachineTokenState_MACHINE_TOKEN_ACTIVE,
		Fingerprint: fingerprint,
		ExpiresAt:   time.Now().Add(machineRefreshTokenDuration),
	}); err != nil {
		return "", errors.Wrap(err, "failed to store replayed machine refresh token")
	}
	return refreshToken, nil
}

// ---- Provision-job state machine ----

// provisioningPhaseTransitions are the manager-side phase transitions driven by
// ProvisionJobProgress frames. The manager never guesses cluster state: a
// frame either advances the phase along this map or is ignored as stale
// (terminal phases never change; the MVP retry path is delete + re-create).
var provisioningPhaseTransitions = map[storepb.ProvisioningPhase][]storepb.ProvisioningPhase{
	storepb.ProvisioningPhase_PROVISIONING_PHASE_PENDING: {
		storepb.ProvisioningPhase_PROVISIONING_PHASE_PROVISIONING,
		storepb.ProvisioningPhase_PROVISIONING_PHASE_PROVISIONED, // a fast backend may skip the ack
		storepb.ProvisioningPhase_PROVISIONING_PHASE_FAILED,
	},
	storepb.ProvisioningPhase_PROVISIONING_PHASE_PROVISIONING: {
		storepb.ProvisioningPhase_PROVISIONING_PHASE_PROVISIONED,
		storepb.ProvisioningPhase_PROVISIONING_PHASE_FAILED,
	},
	storepb.ProvisioningPhase_PROVISIONING_PHASE_FAILED: {
		// A failed machine can still be deleted; the teardown may then report
		// DEPROVISIONING → DELETED.
		storepb.ProvisioningPhase_PROVISIONING_PHASE_DEPROVISIONING,
	},
	storepb.ProvisioningPhase_PROVISIONING_PHASE_PROVISIONED: {
		storepb.ProvisioningPhase_PROVISIONING_PHASE_DEPROVISIONING,
	},
	storepb.ProvisioningPhase_PROVISIONING_PHASE_DEPROVISIONING: {
		storepb.ProvisioningPhase_PROVISIONING_PHASE_DELETED,
	},
}

// applyProvisionProgress folds one ProvisionJobProgress frame into the
// machine's stored provisioning status. It returns the (possibly unchanged)
// status to persist: allowed transitions advance the phase and stamp the
// matching timestamp, stale or terminal frames are ignored, and errors /
// workload locators are recorded on every accepted transition.
func applyProvisionProgress(
	current *storepb.ProvisioningStatus,
	phase storepb.ProvisioningPhase,
	progressErr string,
	workloadName string,
) *storepb.ProvisioningStatus {
	if current == nil || phase == storepb.ProvisioningPhase_PROVISIONING_PHASE_UNSPECIFIED {
		return current
	}
	allowed := false
	for _, next := range provisioningPhaseTransitions[current.Phase] {
		if next == phase {
			allowed = true
			break
		}
	}
	if !allowed {
		return current
	}

	// Start from a copy so the caller's cached row is never mutated in place.
	next := cloneProvisioningStatus(current)
	next.Phase = phase
	next.Error = progressErr
	if workloadName != "" {
		next.WorkloadName = workloadName
	}
	switch phase {
	case storepb.ProvisioningPhase_PROVISIONING_PHASE_PROVISIONED:
		next.ProvisionedAt = time.Now().Unix()
	case storepb.ProvisioningPhase_PROVISIONING_PHASE_FAILED:
		next.FailedAt = time.Now().Unix()
	default:
	}
	return next
}

// cloneProvisioningStatus returns a deep copy of a stored provisioning status
// so phase transitions never mutate the caller's (cached) row in place.
func cloneProvisioningStatus(p *storepb.ProvisioningStatus) *storepb.ProvisioningStatus {
	clone := &storepb.ProvisioningStatus{
		Phase:         p.Phase,
		Error:         p.Error,
		WorkloadName:  p.WorkloadName,
		RuntimeImage:  p.RuntimeImage,
		MachineParams: maps.Clone(p.GetMachineParams()),
		PendingAt:     p.PendingAt,
		ProvisionedAt: p.ProvisionedAt,
		FailedAt:      p.FailedAt,
	}
	if p.WorkloadLabels != nil {
		clone.WorkloadLabels = make(map[string]string, len(p.WorkloadLabels))
		for k, v := range p.WorkloadLabels {
			clone.WorkloadLabels[k] = v
		}
	}
	return clone
}
