package dispatcher

import (
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

// sendToProvisioner is the single provisioner-session send path on the facade:
// look up the connected provisioner session and deliver a control message,
// returning an error when the provisioner is offline.
func (d *Dispatcher) sendToProvisioner(provisionerID int, msg *v1pb.ManagerProvisionerStreamMessage) error {
	return d.registry.sendToProvisioner(provisionerID, msg)
}

// SendProvisionMachineJob pushes a provisioning job to a connected
// provisioner. Best-effort by contract: a provisioner that is offline misses
// the push, but the machine row sits at PENDING and the job is replayed on the
// provisioner's next connect.
func (d *Dispatcher) SendProvisionMachineJob(provisionerID int, job *v1pb.ProvisionMachineJob) error {
	return d.sendToProvisioner(provisionerID, &v1pb.ManagerProvisionerStreamMessage{
		Message: &v1pb.ManagerProvisionerStreamMessage_ProvisionJob{
			ProvisionJob: job,
		},
	})
}

// SendDeprovisionMachineJob pushes a workload-teardown job to a connected
// provisioner. Best-effort like SendProvisionMachineJob: a missed push is
// recovered by the reconnect replay (DEPROVISIONING rows are replayable).
func (d *Dispatcher) SendDeprovisionMachineJob(provisionerID int, job *v1pb.DeprovisionMachineJob) error {
	return d.sendToProvisioner(provisionerID, &v1pb.ManagerProvisionerStreamMessage{
		Message: &v1pb.ManagerProvisionerStreamMessage_DeprovisionJob{
			DeprovisionJob: job,
		},
	})
}

// SendPongToProvisioner replies to a provisioner Ping on its control stream.
func (d *Dispatcher) SendPongToProvisioner(provisionerID int) error {
	return d.sendToProvisioner(provisionerID, &v1pb.ManagerProvisionerStreamMessage{
		Message: &v1pb.ManagerProvisionerStreamMessage_Pong{
			Pong: &v1pb.Pong{},
		},
	})
}

// SendProvisionerDisconnectNotice warns a connected provisioner that its token
// was rotated or the provisioner deleted, so it can stop retrying with a dead
// credential before the manager tears the stream down. deleted tells the
// operator to tear down its own hosting (scale its Deployment to 0) instead of
// merely exiting, so a deleted provisioner stops crash-looping.
func (d *Dispatcher) SendProvisionerDisconnectNotice(provisionerID int, reason string, deleted bool) error {
	return d.sendToProvisioner(provisionerID, &v1pb.ManagerProvisionerStreamMessage{
		Message: &v1pb.ManagerProvisionerStreamMessage_DisconnectNotice{
			DisconnectNotice: &v1pb.ProvisionerDisconnectNotice{Reason: reason, Deleted: deleted},
		},
	})
}
