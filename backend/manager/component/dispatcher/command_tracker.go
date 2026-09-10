package dispatcher

import (
	"sync"
)

// commandTracker records each agent's current in-flight drain command id. The
// per-agent session registry is retired (agent control runs on the machine
// control stream); this is the piece of that state which survives — it powers
// the conversation activity feed's "working on" link. Set at BeginSession
// (mint) and cleared when the terminal result is acked via UploadCommandData.
// In-memory only by design: after a manager restart the tracker is empty until
// the next turn, and the activity feed falls back to "idle".
type commandTracker struct {
	mu      sync.Mutex
	current map[int]string
}

func (t *commandTracker) set(agentID int, commandID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.current == nil {
		t.current = make(map[int]string)
	}
	t.current[agentID] = commandID
}

// clear forgets the agent's current command only when it still points at the
// one that finished, so a mint racing a late terminal is not wiped.
func (t *commandTracker) clear(agentID int, commandID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.current[agentID] == commandID {
		delete(t.current, agentID)
	}
}

func (t *commandTracker) get(agentID int) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.current[agentID]
}
