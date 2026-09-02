package dispatcher

import (
	"sync"

	"github.com/pkg/errors"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

// sessionRegistry owns the live agent/machine session maps and their locking.
// It is extracted from Dispatcher so connection lifecycle can be tested and
// evolved independently from command pub/sub and pending replies.
//
// RegisterAgent/RegisterMachine still touch the fields directly (see
// session_lifecycle.go): those paths need to invalidate the previous session,
// cancel grace timers / reset upgrade state, and install the new session in a
// single critical section, which the simple get/set helpers below cannot
// express without changing the locking order.
type sessionRegistry struct {
	mu       sync.RWMutex
	sessions map[int]*AgentSession
	machines map[int]*MachineSession
}

func newSessionRegistry() *sessionRegistry {
	return &sessionRegistry{
		sessions: make(map[int]*AgentSession),
		machines: make(map[int]*MachineSession),
	}
}

func (r *sessionRegistry) getAgent(agentID int) (*AgentSession, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	sess, ok := r.sessions[agentID]
	return sess, ok
}

func (r *sessionRegistry) deleteAgent(agentID int) (*AgentSession, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	sess, ok := r.sessions[agentID]
	if ok {
		delete(r.sessions, agentID)
	}
	return sess, ok
}

func (r *sessionRegistry) deleteAgentIf(agentID int, sess *AgentSession) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.sessions[agentID]
	if !ok || current != sess {
		return false
	}
	delete(r.sessions, agentID)
	return true
}

func (r *sessionRegistry) getMachine(machineID int) (*MachineSession, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	sess, ok := r.machines[machineID]
	return sess, ok
}

// deleteMachineWithAgents removes the machine session and every agent session
// owned by it in one critical section. ok is false when no machine session was
// registered for machineID.
func (r *sessionRegistry) deleteMachineWithAgents(machineID int) (machine *MachineSession, owned []*AgentSession, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	machine, ok = r.machines[machineID]
	if !ok {
		return nil, nil, false
	}
	delete(r.machines, machineID)
	for _, sess := range r.sessions {
		if sess.machineID == machineID {
			owned = append(owned, sess)
			delete(r.sessions, sess.agentID)
		}
	}
	return machine, owned, true
}

// deleteMachineIfWithAgents is deleteMachineWithAgents but only when sess is
// still the registered machine session. It returns the detached agent sessions
// and whether sess was the current session.
func (r *sessionRegistry) deleteMachineIfWithAgents(machineID int, sess *MachineSession) (owned []*AgentSession, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.machines[machineID]
	if !ok || current != sess {
		return nil, false
	}
	delete(r.machines, machineID)
	for _, s := range r.sessions {
		if s.machineID == machineID {
			owned = append(owned, s)
			delete(r.sessions, s.agentID)
		}
	}
	return owned, true
}

func (r *sessionRegistry) snapshotAgents() []*AgentSession {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*AgentSession, 0, len(r.sessions))
	for _, sess := range r.sessions {
		out = append(out, sess)
	}
	return out
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

func (r *sessionRegistry) sendToAgent(agentID int, msg *v1pb.ManagerStreamMessage) error {
	sess, ok := r.getAgent(agentID)
	if !ok {
		return errors.New("agent is not connected")
	}
	return sess.Send(msg)
}
