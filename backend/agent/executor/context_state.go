package executor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/Ranxy/laelia/backend/agent/atomicfile"
)

// ContextState tracks one agent's session context across drain turns: observed
// usage, compaction history, warm-turn health, and the decision flag that makes
// the next warm turn carry a re-anchor prompt. It is persisted at
// ~/.laelia/<machineID>/<agentID>/context-state.json, a sibling of
// command-state.json / acp-session.json under the same machine + agent dir.
//
// Fingerprint mirrors the session fingerprint (acp-session.json / pi-session
// fingerprint inputs); when it changes (admin changed provider/model/working
// dir) the accumulated stats are reset because they describe a different
// session.
type ContextState struct {
	// Usage is the most recent context-window observation.
	Usage ContextUsage `json:"usage,omitempty"`
	// Compaction counts/stamps observed compactions.
	Compaction CompactionInfo `json:"compaction"`
	// Session tracks warm-turn continuity for periodic re-anchoring.
	Session SessionHealth `json:"session"`
	// NeedsReanchor marks that the next warm turn must prepend the identity
	// anchor (set after a compaction; consumed by the runner).
	NeedsReanchor bool `json:"needs_reanchor"`
	// Fingerprint is the session fingerprint this state describes. A change
	// resets the accumulated stats.
	Fingerprint string `json:"fingerprint,omitempty"`
	// OwnerDisplayName is the owner display name the last turn's init/re-anchor
	// prompt carried. The runner compares it against the manager's fresh
	// BeginSessionResponse owner on each turn and forces NeedsReanchor when the
	// owner changed, so a warm session whose init prompt named the old owner
	// re-anchors with the new owner before the old owner's authority could be
	// relied on. Empty for legacy agents (no ownership section).
	OwnerDisplayName string `json:"owner_display_name,omitempty"`
}

// ContextUsage is a point-in-time snapshot of the session context window.
type ContextUsage struct {
	Size      int64     `json:"size"`
	Used      int64     `json:"used"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CompactionInfo records compaction observations. Active is true between a
// CONTEXT_COMPACTION_STARTED and its matching FINISHED (the watchdog runs
// while active).
type CompactionInfo struct {
	Count       int64     `json:"count"`
	LastAt      time.Time `json:"last_at"`
	LastStartAt time.Time `json:"last_start_at,omitempty"`
	Active      bool      `json:"active"`
}

// SessionHealth tracks consecutive warm turns (used for the periodic re-anchor
// threshold), resume failures, and cold starts.
type SessionHealth struct {
	Turns          int `json:"turns"`
	ResumeFailures int `json:"resume_failures"`
	ColdStarts     int `json:"cold_starts"`
}

// UsageRatio returns the used/size ratio, or 0 when no size is known.
func (c *ContextState) UsageRatio() float64 {
	if c == nil || c.Usage.Size <= 0 {
		return 0
	}
	return float64(c.Usage.Used) / float64(c.Usage.Size)
}

// ResetForFingerprint drops all accumulated stats when the session config
// fingerprint changes (the old stats describe a different session).
func (c *ContextState) ResetForFingerprint(fingerprint string) {
	c.Usage = ContextUsage{}
	c.Compaction = CompactionInfo{}
	c.Session = SessionHealth{}
	c.NeedsReanchor = false
	c.Fingerprint = fingerprint
}

func contextStatePath(machineID, agentID string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".laelia", machineID, agentID, "context-state.json")
}

// LoadContextState reads the persisted context state. A missing file is not an
// error: it means the agent has no context observations yet.
func LoadContextState(machineID, agentID string) (*ContextState, error) {
	data, err := os.ReadFile(contextStatePath(machineID, agentID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var s ContextState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// SaveContextState persists the context state with an atomic temp+rename write
// (0600), so a crash mid-write can never leave a truncated file that reads as a
// fresh-but-empty state.
func SaveContextState(machineID, agentID string, state *ContextState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return atomicfile.WriteFileAtomic(contextStatePath(machineID, agentID), data, 0o600)
}
