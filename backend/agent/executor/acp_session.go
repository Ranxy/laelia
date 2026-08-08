package executor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/Ranxy/laelia/backend/agent/atomicfile"
)

// maxResumeFailuresBeforeWarning is the consecutive ResumeSession failure count
// that surfaces a WARNING event (and resets the counter).
const maxResumeFailuresBeforeWarning = 3

// acpSessionState is the durable record of the ACP session an agent is reusing
// across drain turns. Each turn spawns a fresh ACP subprocess (cold start is
// cheap and frees resources while idle), but resumes the SAME Acp SessionId so
// the LLM conversation — and the init prompt sent once at cold start — is
// inherited. This file is what makes the session survive between turns.
//
// It lives at ~/.laelia/<machineID>/<agentID>/acp-session.json, a sibling of
// the per-command command-state.json (see state.go) under the same machine +
// agent directory. Fingerprint invalidates it when the admin changes the
// provider/model/working dir, so a config change drops back to a cold
// NewSession + fresh init prompt rather than resuming a session the provider
// no longer recognizes.
type acpSessionState struct {
	SessionID   string `json:"session_id"`
	ThreadID    string `json:"thread_id,omitempty"`
	Fingerprint string `json:"fingerprint"`
	CreatedAt   int64  `json:"created_at"`
}

// sessionFingerprint derives a stable identity for the ACP session from the
// inputs that define it: the provider, the selected model, the working
// directory, and the protocol generation (v1 session vs v2 thread). A change
// in any of these means the persisted SessionId/ThreadId belongs to a
// different session and must not be resumed.
func sessionFingerprint(provider, model, workingDir, protocol string) string {
	h := sha256.New()
	_, _ = h.Write([]byte(provider + "\x00" + model + "\x00" + workingDir + "\x00" + protocol))
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func acpSessionPath(machineID, agentID string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".laelia", machineID, agentID, "acp-session.json")
}

// loadACPSession reads the persisted ACP session state. A missing file is not
// an error: it means the agent has never opened a session and must cold-start.
func loadACPSession(machineID, agentID string) (*acpSessionState, error) {
	data, err := os.ReadFile(acpSessionPath(machineID, agentID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var s acpSessionState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// saveACPSession persists the ACP session state so the next drain turn can
// resume it instead of cold-starting. It is best-effort: a write failure only
// means the next turn cold-starts (re-sends the init prompt), never a lost
// message — the durable per-channel cursor is the source of truth.
func saveACPSession(machineID, agentID string, state *acpSessionState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return atomicfile.WriteFileAtomic(acpSessionPath(machineID, agentID), data, 0o600)
}

// clearACPSession drops the persisted ACP session so the next turn cold-starts.
// Called when a ResumeSession fails (the provider lost the session) so we do
// not loop forever retrying a dead id.
func clearACPSession(machineID, agentID string) {
	_ = os.Remove(acpSessionPath(machineID, agentID))
}

// recordResumeFailure increments the consecutive resume-failure counter in the
// agent's context state and reports whether the warning threshold was crossed
// (the counter is reset to 0 once it is). It does not save: the drain loop
// persists the counter at the end of the turn via Result.ResumeFailures, so the
// context state keeps a single writer.
func recordResumeFailure(machineID, agentID string) (failures int, warned bool) {
	state, err := LoadContextState(machineID, agentID)
	if err != nil || state == nil {
		state = &ContextState{}
	}
	state.Session.ResumeFailures++
	if state.Session.ResumeFailures >= maxResumeFailuresBeforeWarning {
		state.Session.ResumeFailures = 0
		return 0, true
	}
	return state.Session.ResumeFailures, false
}
