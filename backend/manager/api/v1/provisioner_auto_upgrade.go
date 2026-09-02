package v1

import (
	"context"
	"log/slog"
	"time"

	storepb "github.com/Ranxy/laelia/backend/generated-go/store"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/manager/component/dispatcher"
	"github.com/Ranxy/laelia/backend/manager/component/machinebuild"
	"github.com/Ranxy/laelia/backend/manager/store"
)

const (
	// provisionerAutoUpgradeInterval is how often the auto-upgrade loop scans
	// for machines whose provisioner opted into auto upgrades (design §6.6).
	provisionerAutoUpgradeInterval = 5 * time.Minute
	// provisionerAutoUpgradeFirstDelay delays the first scan so server boot is
	// not slowed by it; the ticker cadence picks the work up either way.
	provisionerAutoUpgradeFirstDelay = 2 * time.Minute
	// provisionerAutoUpgradeDBTimeout bounds each scan's DB work.
	provisionerAutoUpgradeDBTimeout = 30 * time.Second
)

// StartProvisionerAutoUpgradeLoop runs the manager-side auto-upgrade ticker
// for machines of provisioners that opted in via ProvisionerReady.auto_upgrade:
// every online machine whose reported version lags the embedded latest and
// which has no in-flight upgrade gets the same UpgradeRequest the manual
// UpgradeMachine RPC sends. Progress reporting and crash-safety are the
// machine's existing self-upgrade machinery, unchanged.
func StartProvisionerAutoUpgradeLoop(ctx context.Context, st *store.Store, d *dispatcher.Dispatcher) {
	if st == nil || d == nil {
		return
	}

	run := func() {
		dbCtx, cancel := context.WithTimeout(ctx, provisionerAutoUpgradeDBTimeout)
		defer cancel()
		autoUpgradeProvisioners(dbCtx, st, d)
	}

	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(provisionerAutoUpgradeFirstDelay):
		}
		run()
		ticker := time.NewTicker(provisionerAutoUpgradeInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				run()
			}
		}
	}()
}

// autoUpgradeProvisioners runs one scan: for every connected provisioner with
// auto_upgrade enabled, push UpgradeRequest to its online provisioned machines
// that lag the embedded latest version and have no in-flight upgrade.
func autoUpgradeProvisioners(ctx context.Context, st *store.Store, d *dispatcher.Dispatcher) {
	provisioners, err := st.ListProvisioners(ctx, &store.FindProvisionerMessage{})
	if err != nil {
		slog.Error("provisioner auto-upgrade scan failed to list provisioners", "error", err)
		return
	}

	latest := machinebuild.LatestVersion()
	if latest == "" {
		return
	}
	for _, provisioner := range provisioners {
		if provisioner.Deleted || provisioner.Status == nil || !provisioner.Status.AutoUpgrade {
			continue
		}
		if !d.IsProvisionerConnected(provisioner.ID) {
			continue
		}

		machines, err := st.ListMachines(ctx, &store.FindMachineMessage{ProvisionerID: &provisioner.ID})
		if err != nil {
			slog.Error("provisioner auto-upgrade scan failed to list machines",
				"provisionerID", provisioner.ID, "error", err)
			continue
		}

		// Provisioned machines run the manager's embedded linux-x64 build, so
		// the target is fixed (design §6.5); machines that never connected
		// (empty version) are skipped until they report one.
		entry, ok := machinebuild.GetTarget(defaultBinaryTarget)
		if !ok {
			return
		}
		for _, machine := range machines {
			if machine.Deleted || machine.Status == nil || machine.Status.GetState() != storepb.MachineStatus_ONLINE {
				continue
			}
			if !machinebuild.UpgradeAvailable(machine.Info.GetVersion(), latest) {
				continue
			}
			if d.MachineUpgradeStatus(machine.ID) != nil {
				// An upgrade was already requested during this connection.
				continue
			}
			if err := d.SendUpgradeRequest(machine.ID, &v1pb.UpgradeRequest{
				Version: latest,
				Target:  defaultBinaryTarget,
				Sha256:  entry.Gz.Sha256,
			}); err != nil {
				slog.Warn("provisioner auto-upgrade push failed", "machine", machine.ResourceID, "error", err)
				continue
			}
			d.RecordMachineUpgrade(machine.ID, &v1pb.UpgradeProgress{Version: latest, Stage: "requested"})
			slog.Info("provisioner auto-upgrade requested",
				"machine", machine.ResourceID, "provisioner", provisioner.ResourceID, "version", latest)
		}
	}
}
