package v1

import (
	"context"
	"io"
	"log/slog"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

	"github.com/Ranxy/laelia/backend/common"
	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/laelia/backend/manager/component/dispatcher"
	"github.com/Ranxy/laelia/backend/manager/component/provision"
	"github.com/Ranxy/laelia/backend/manager/config"
	"github.com/Ranxy/laelia/backend/manager/store"
)

// cloneStoreProvisionerStatus returns a deep copy of a provisioner's runtime
// status so connected-flag stamps never mutate the caller's (cached) row in
// place. Mirrors cloneStoreMachineInfo.
func cloneStoreProvisionerStatus(st *storepb.ProvisionerStatus) *storepb.ProvisionerStatus {
	if st == nil {
		return &storepb.ProvisionerStatus{}
	}
	return &storepb.ProvisionerStatus{
		Connected:    st.Connected,
		LastSeen:     st.LastSeen,
		Version:      st.Version,
		Backend:      st.Backend,
		AutoUpgrade:  st.AutoUpgrade,
		ConfigDigest: st.ConfigDigest,
	}
}

// ProvisionerStreamService implements ProvisionerStreamService
// .ProvisionerChannel: the provisioner-level control plane, mirroring the
// machine MachineChannel. A provisioner authenticates once (provisioner token,
// resolved by the auth interceptor into ProvisionerContextKey) and holds this
// single bidi stream for the lifetime of its connection. Over it the manager
// pushes provisioning jobs (and disconnect notices); the provisioner reports
// readiness, job progress, and pings.
type ProvisionerStreamService struct {
	v1connect.UnimplementedProvisionerStreamServiceHandler
	store      *store.Store
	secret     string
	profile    *config.Profile
	dispatcher *dispatcher.Dispatcher
}

func NewProvisionerStreamService(s *store.Store, secret string, profile *config.Profile, d *dispatcher.Dispatcher) *ProvisionerStreamService {
	return &ProvisionerStreamService{store: s, secret: secret, profile: profile, dispatcher: d}
}

// ProvisionerChannel serves one provisioner connection. The first inbound
// frame must be ProvisionerReady (it stamps the provisioner's runtime status);
// afterwards the manager pushes replayed/queued jobs. On stream death the
// provisioner is marked offline — machines never depend on this stream after
// they boot.
func (s *ProvisionerStreamService) ProvisionerChannel(
	ctx context.Context,
	stream *connect.BidiStream[v1pb.ProvisionerStreamMessage, v1pb.ManagerProvisionerStreamMessage],
) error {
	provisioner, ok := GetProvisionerFromContext(ctx)
	if !ok || provisioner == nil {
		return connect.NewError(connect.CodeUnauthenticated, nil)
	}
	if provisioner.Deleted {
		return connect.NewError(connect.CodePermissionDenied, errors.Errorf("provisioner %s is deleted", provisioner.ResourceID))
	}

	sendFunc := func(msg *v1pb.ManagerProvisionerStreamMessage) error {
		return stream.Send(msg)
	}

	sess := s.dispatcher.RegisterProvisioner(provisioner.ID, provisioner.ResourceID, sendFunc)
	// Identity-aware teardown: if a reconnect replaced this session before the
	// old stream ends, do not destroy the new (live) session — and only stamp
	// connected=false when this call actually tore down the session.
	defer func() {
		if s.dispatcher.UnregisterProvisionerIf(provisioner.ID, sess) {
			s.stampProvisionerConnected(context.Background(), provisioner.ID, false)
		}
	}()

	slog.Info("provisioner control stream connected",
		"provisionerID", provisioner.ID, "resourceID", provisioner.ResourceID)

	ready := false
	for {
		msg, err := stream.Receive()
		if err != nil {
			if err == io.EOF {
				slog.Info("provisioner control stream closed", "provisionerID", provisioner.ID)
				return nil
			}
			return err
		}

		switch m := msg.Message.(type) {
		case *v1pb.ProvisionerStreamMessage_Ready:
			if ready {
				// A repeat Ready frame is a protocol violation but harmless:
				// re-stamping would resurrect last_seen; ignore it.
				slog.Warn("duplicate provisioner ready frame ignored", "provisionerID", provisioner.ID)
				break
			}
			ready = true
			s.handleReady(provisioner, m.Ready)
			// Replay every machine in a non-terminal provisioning phase for
			// this provisioner (design §4.3): queued jobs (PENDING), jobs the
			// provisioner acked but never finished (PROVISIONING), and pending
			// teardowns (DEPROVISIONING) — including machines whose row was
			// already soft-deleted.
			s.replayProvisionJobs(provisioner)

		case *v1pb.ProvisionerStreamMessage_JobProgress:
			if !ready {
				slog.Warn("provisioner sent job progress before ready; ignoring", "provisionerID", provisioner.ID)
				break
			}
			s.handleJobProgress(provisioner, m.JobProgress)

		case *v1pb.ProvisionerStreamMessage_Ping:
			s.dispatcher.HandleProvisionerPing(sess, m.Ping)
			if err := s.dispatcher.SendPongToProvisioner(provisioner.ID); err != nil {
				slog.Error("failed to send pong to provisioner", "provisionerID", provisioner.ID, "error", err)
			}

		default:
			slog.Warn("unknown provisioner stream message type", "provisionerID", provisioner.ID)
		}
	}
}

// handleReady stamps the provisioner's runtime status from the ProvisionerReady
// frame: connected, last seen, reported version/backend, auto_upgrade flag, and
// config digest. Best-effort with a detached context so a concurrently closing
// stream cannot cancel the persistence halfway.
func (s *ProvisionerStreamService) handleReady(provisioner *store.ProvisionerMessage, ready *v1pb.ProvisionerReady) {
	if ready == nil {
		return
	}
	status := &storepb.ProvisionerStatus{
		Connected:    true,
		LastSeen:     time.Now().Unix(),
		Version:      ready.GetVersion(),
		Backend:      ready.GetBackend(),
		AutoUpgrade:  ready.GetAutoUpgrade(),
		ConfigDigest: ready.GetConfigDigest(),
	}
	if ready.GetBackend() != "" && ready.GetBackend() != provisioner.Backend {
		slog.Warn("provisioner reports a backend that differs from its registry row",
			"provisionerID", provisioner.ID, "registered", provisioner.Backend, "reported", ready.GetBackend())
	}
	if _, err := s.store.UpdateProvisionerStatus(context.Background(), provisioner, status); err != nil {
		slog.Error("failed to stamp provisioner status", "provisionerID", provisioner.ID, "error", err)
	}
}

// stampProvisionerConnected persists the connected flag (connect/teardown).
// Best-effort: status is informational; the registry is the live truth.
func (s *ProvisionerStreamService) stampProvisionerConnected(ctx context.Context, provisionerID int, connected bool) {
	dbCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	provisioner, err := s.store.GetProvisioner(dbCtx, provisionerID)
	if err != nil || provisioner == nil {
		if err != nil {
			slog.Error("failed to load provisioner for status stamp", "provisionerID", provisionerID, "error", err)
		}
		return
	}
	status := cloneStoreProvisionerStatus(provisioner.Status)
	status.Connected = connected
	if connected {
		status.LastSeen = time.Now().Unix()
	}
	if _, err := s.store.UpdateProvisionerStatus(dbCtx, provisioner, status); err != nil {
		slog.Error("failed to stamp provisioner connected status", "provisionerID", provisionerID, "connected", connected, "error", err)
	}
}

// handleJobProgress folds an inbound ProvisionJobProgress frame into the
// machine's provisioning row. Frames for machines this provisioner does not
// own (or unknown machines) are ignored.
func (s *ProvisionerStreamService) handleJobProgress(provisioner *store.ProvisionerMessage, progress *v1pb.ProvisionJobProgress) {
	if progress == nil {
		return
	}
	resourceID, err := common.GetMachineResourceID(progress.GetMachine())
	if err != nil {
		slog.Warn("provisioner sent malformed job progress machine name",
			"provisionerID", provisioner.ID, "machine", progress.GetMachine(), "error", err)
		return
	}

	// Detached timeout: a concurrently closing stream must not cancel the
	// persistence halfway (same pattern as the providers-discovered update).
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	machine, err := s.store.GetMachineByResourceID(ctx, resourceID)
	if err != nil {
		slog.Error("failed to load machine for provision progress", "machine", resourceID, "error", err)
		return
	}
	if machine == nil || machine.ProvisionerID != provisioner.ID || machine.Provisioning == nil {
		slog.Warn("provision progress ignored for unowned or unprovisioned machine",
			"provisionerID", provisioner.ID, "machine", resourceID)
		return
	}

	next := applyProvisionProgress(machine.Provisioning, storepb.ProvisioningPhase(progress.GetPhase()), progress.GetError(), progress.GetWorkloadName())
	if next == machine.Provisioning {
		slog.Debug("stale provision progress ignored",
			"machine", resourceID, "phase", progress.GetPhase().String(), "stored", machine.Provisioning.Phase.String())
		return
	}
	if err := s.store.UpdateMachineProvisioning(ctx, machine.ID, next); err != nil {
		slog.Error("failed to persist provision progress", "machine", resourceID, "error", err)
		return
	}
	slog.Info("provision progress recorded",
		"machine", resourceID, "phase", next.Phase.String(), "workload", next.WorkloadName, "error", next.Error)
}

// replayProvisionJobs pushes every machine in a replayable provisioning phase
// to a just-connected provisioner — the provisioning twin of the machine
// roster resync on ConnectMachine. Provision jobs are re-composed with a fresh
// refresh token (the plaintext of the original only ever existed at provision
// time); the backend upsert makes replay idempotent.
func (s *ProvisionerStreamService) replayProvisionJobs(provisioner *store.ProvisionerMessage) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	machines, err := s.store.ListReplayableProvisioningMachines(ctx, provisioner.ID)
	if err != nil {
		slog.Error("failed to list replayable provisioning machines", "provisionerID", provisioner.ID, "error", err)
		return
	}
	for _, machine := range machines {
		switch machine.Provisioning.GetPhase() {
		case storepb.ProvisioningPhase_PROVISIONING_PHASE_DEPROVISIONING:
			job := &v1pb.DeprovisionMachineJob{
				Machine:  common.FormatMachineUID(machine.ResourceID),
				KeepData: false,
			}
			if err := s.dispatcher.SendDeprovisionMachineJob(provisioner.ID, job); err != nil {
				slog.Warn("failed to replay deprovision job; staying replayable",
					"machine", machine.ResourceID, "error", err)
			} else {
				slog.Info("deprovision job replayed", "machine", machine.ResourceID, "provisionerID", provisioner.ID)
			}

		default:
			refreshToken, err := mintProvisionedMachineRefreshToken(ctx, s.store, s.secret, s.profile.Mode, machine,
				provision.MachineFingerprint(provisioner.ResourceID, machine.ResourceID))
			if err != nil {
				slog.Error("failed to re-mint refresh token for replayed job", "machine", machine.ResourceID, "error", err)
				continue
			}
			job, err := composeProvisionMachineJob(ctx, s.store, provisioner, machine, refreshToken)
			if err != nil {
				slog.Error("failed to compose replayed provision job; machine stays in its phase",
					"machine", machine.ResourceID, "error", err)
				continue
			}
			if err := s.dispatcher.SendProvisionMachineJob(provisioner.ID, job); err != nil {
				slog.Warn("failed to replay provision job; staying in its phase",
					"machine", machine.ResourceID, "error", err)
			} else {
				slog.Info("provision job replayed", "machine", machine.ResourceID, "phase", machine.Provisioning.GetPhase().String())
			}
		}
	}
}
