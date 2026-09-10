package dispatcher

import (
	"sync"

	"github.com/pkg/errors"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

// sessionRegistry owns the live machine/provisioner session maps and their
// locking. The per-agent AgentChannel registry is retired: agent identity is
// bound per-RPC by ownership checks (BeginSession / UploadCommandData name the
// agent and the machine must host it), so only machine-level streams remain.
//
// RegisterMachine still touches the fields directly (see session_lifecycle.go):
// that path needs to invalidate the previous session and install the new one in
// a single critical section, which the simple get/set helpers below cannot
// express without changing the locking order.
type sessionRegistry struct {
	mu           sync.RWMutex
	machines     map[int]*MachineSession
	provisioners map[int]*ProvisionerSession
}

func newSessionRegistry() *sessionRegistry {
	return &sessionRegistry{
		machines:     make(map[int]*MachineSession),
		provisioners: make(map[int]*ProvisionerSession),
	}
}

func (r *sessionRegistry) getMachine(machineID int) (*MachineSession, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	sess, ok := r.machines[machineID]
	return sess, ok
}

func (r *sessionRegistry) deleteMachine(machineID int) (*MachineSession, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	sess, ok := r.machines[machineID]
	if ok {
		delete(r.machines, machineID)
	}
	return sess, ok
}

// deleteMachineIf removes the machine session only when sess is still the
// registered one (a reconnect has replaced it otherwise).
func (r *sessionRegistry) deleteMachineIf(machineID int, sess *MachineSession) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.machines[machineID]
	if !ok || current != sess {
		return false
	}
	delete(r.machines, machineID)
	return true
}

func (r *sessionRegistry) snapshotMachines() []*MachineSession {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*MachineSession, 0, len(r.machines))
	for _, m := range r.machines {
		out = append(out, m)
	}
	return out
}

func (r *sessionRegistry) sendToMachine(machineID int, msg *v1pb.ManagerMachineStreamMessage) error {
	sess, ok := r.getMachine(machineID)
	if !ok {
		return errors.New("machine is not connected")
	}
	return sess.Send(msg)
}
