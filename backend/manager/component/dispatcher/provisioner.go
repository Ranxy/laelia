package dispatcher

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/pkg/errors"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

// ProvisionerSendFunc is the raw send function for a provisioner's
// ProvisionerChannel control stream (manager→provisioner direction).
type ProvisionerSendFunc func(*v1pb.ManagerProvisionerStreamMessage) error

// ProvisionerSession is the manager-side handle on a connected provisioner's
// ProvisionerChannel stream. Mirrors MachineSession: the provisioner
// authenticates once with its long-lived token and holds this stream for its
// lifetime; the manager pushes provisioning jobs down and disconnect notices
// on rotate/delete.
type ProvisionerSession struct {
	provisionerID         int
	provisionerResourceID string
	send                  atomic.Pointer[ProvisionerSendFunc]
	sendMu                sync.Mutex
	lastPingAt            time.Time
	connectedAt           time.Time
	mu                    sync.Mutex // guards lastPingAt, connectedAt
}

func (s *ProvisionerSession) deliver(msg *v1pb.ManagerProvisionerStreamMessage) error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	fn := s.send.Load()
	if fn == nil {
		return errors.New("provisioner session invalidated")
	}
	return (*fn)(msg)
}

// Send sends a control message to the provisioner over its ProvisionerChannel.
// Safe for concurrent use: sends are serialized per session (gRPC bidi sends
// are not safe for concurrent use).
func (s *ProvisionerSession) Send(msg *v1pb.ManagerProvisionerStreamMessage) error {
	return s.deliver(msg)
}

// TouchPing records a received ping so the liveness monitor can judge the
// session healthy.
func (s *ProvisionerSession) TouchPing() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastPingAt = time.Now()
}

func (r *sessionRegistry) getProvisioner(provisionerID int) (*ProvisionerSession, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	sess, ok := r.provisioners[provisionerID]
	return sess, ok
}

func (r *sessionRegistry) deleteProvisioner(provisionerID int) (*ProvisionerSession, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	sess, ok := r.provisioners[provisionerID]
	if ok {
		delete(r.provisioners, provisionerID)
	}
	return sess, ok
}

func (r *sessionRegistry) deleteProvisionerIf(provisionerID int, sess *ProvisionerSession) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.provisioners[provisionerID]
	if !ok || current != sess {
		return false
	}
	delete(r.provisioners, provisionerID)
	return true
}

func (r *sessionRegistry) snapshotProvisioners() []*ProvisionerSession {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*ProvisionerSession, 0, len(r.provisioners))
	for _, p := range r.provisioners {
		out = append(out, p)
	}
	return out
}

func (r *sessionRegistry) sendToProvisioner(provisionerID int, msg *v1pb.ManagerProvisionerStreamMessage) error {
	sess, ok := r.getProvisioner(provisionerID)
	if !ok {
		return errors.New("provisioner is not connected")
	}
	return sess.Send(msg)
}
