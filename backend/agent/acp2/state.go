package acp2

import "sync"

// TurnState is the lifecycle phase of the current turn.
type TurnState int

// Turn lifecycle phases. A turn is Idle until it starts and returns to Idle
// once completed.
const (
	TurnIdle TurnState = iota
	TurnStarted
)

// completedIDLimit bounds the remembered completed turn ids used to ignore
// late non-empty-input marks.
const completedIDLimit = 16

// TurnGate tracks the current turn lifecycle so the executor can gate
// steering and detect empty turns. It mirrors the app-server's own state
// machine: a turn is accepted (pending), started, then completed; progress
// and tool boundaries reset the post-tool steering gate.
type TurnGate struct {
	mu            sync.Mutex
	currentID     string
	pendingID     string
	inProgress    bool
	activity      bool
	tokenUsage    bool
	nonEmptyInput bool
	gate          bool // post-tool window where steering may be rejected
	completedIDs  map[string]struct{}
}

// NewTurnGate returns an idle TurnGate.
func NewTurnGate() *TurnGate {
	return &TurnGate{completedIDs: map[string]struct{}{}}
}

// Reset clears all turn state.
func (g *TurnGate) Reset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.currentID = ""
	g.pendingID = ""
	g.inProgress = false
	g.activity = false
	g.tokenUsage = false
	g.nonEmptyInput = false
	g.gate = false
	g.completedIDs = map[string]struct{}{}
}

// NoteTurnAccepted records the server's acceptance of a turn request before
// the turn/started notification arrives.
func (g *TurnGate) NoteTurnAccepted(id string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.pendingID = id
}

// MarkTurnStarted transitions the gate into the started phase.
func (g *TurnGate) MarkTurnStarted(id string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	startedID := id
	if startedID == "" {
		startedID = g.pendingID
	}
	if startedID != "" {
		g.currentID = startedID
	}
	g.pendingID = ""
	g.inProgress = true
	g.activity = false
	g.tokenUsage = false
	g.nonEmptyInput = false
	g.gate = false
}

// MarkTurnCompleted transitions the gate back to idle and forgets the turn.
func (g *TurnGate) MarkTurnCompleted() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.currentID != "" {
		g.completedIDs[g.currentID] = struct{}{}
		if len(g.completedIDs) > completedIDLimit {
			for id := range g.completedIDs {
				delete(g.completedIDs, id)
				break
			}
		}
	}
	g.currentID = ""
	g.pendingID = ""
	g.inProgress = false
	g.activity = false
	g.tokenUsage = false
	g.nonEmptyInput = false
	g.gate = false
}

// MarkProgress records runtime activity and reopens the steering gate.
func (g *TurnGate) MarkProgress() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.activity = true
	g.gate = false
}

// MarkToolBoundary records a completed tool call and closes the steering
// gate until the next progress signal or turn end.
func (g *TurnGate) MarkToolBoundary() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.activity = true
	g.gate = true
}

// MarkTokenUsage records that a token usage update arrived this turn.
func (g *TurnGate) MarkTokenUsage() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.tokenUsage = true
}

// MarkNonEmptyInput records that this turn carried user text. Marks for
// already-completed turns are ignored.
func (g *TurnGate) MarkNonEmptyInput(turnID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if turnID == "" {
		return
	}
	if _, done := g.completedIDs[turnID]; done {
		return
	}
	if g.inProgress && g.currentID == turnID {
		g.nonEmptyInput = true
	}
}

// ActiveTurnID returns the id of the in-flight turn, if any.
func (g *TurnGate) ActiveTurnID() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.currentID
}

// State returns the current lifecycle phase.
func (g *TurnGate) State() TurnState {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.inProgress {
		return TurnStarted
	}
	return TurnIdle
}

// CanSteerBusy reports whether steering into the in-flight turn is safe:
// a turn is active, no turn request is pending, and the post-tool steering
// gate is open.
func (g *TurnGate) CanSteerBusy() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.currentID != "" && g.pendingID == "" && !g.gate
}

// CompletedWithoutActivity reports whether the current turn ended without any
// runtime activity or token usage, which the executor surfaces as an empty
// turn diagnostic.
func (g *TurnGate) CompletedWithoutActivity() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.inProgress && !g.activity && !g.tokenUsage
}

// HasNonEmptyInput reports whether the current turn carried user text.
func (g *TurnGate) HasNonEmptyInput() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.nonEmptyInput
}
