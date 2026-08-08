package executor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"

	acp "github.com/coder/acp-go-sdk"

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
	Fingerprint string `json:"fingerprint"`
	CreatedAt   int64  `json:"created_at"`
}

// sessionFingerprint derives a stable identity for the ACP session from every
// input that defines it: the provider, the selected model, the working
// directory, the persona prompt, the env overlays (template env + custom env +
// allow-env whitelist) and the MCP server list. A change in any of these means
// the persisted SessionId belongs to a different session and must not be
// resumed — otherwise an admin edit to persona/env/MCP would silently resume
// the old conversation and appear to "not take effect".
func sessionFingerprint(cfg *ACPConfig, workingDir string) string {
	h := sha256.New()
	write := func(s string) { _, _ = h.Write([]byte(s)) }
	write("provider\x00" + cfg.Provider + "\x00")
	write("model\x00" + cfg.Model + "\x00")
	write("workdir\x00" + workingDir + "\x00")
	write("persona\x00" + cfg.PersonaPrompt + "\x00")
	writeEnvMap(h, "env\x00", cfg.Env)
	writeEnvMap(h, "custom_env\x00", cfg.CustomEnv)
	allow := append([]string(nil), cfg.AllowEnv...)
	slices.Sort(allow)
	for _, k := range allow {
		write("allow_env\x00" + k + "\x00")
	}
	write("mcp\x00")
	for _, m := range cfg.McpServers {
		write(mcpServerIdentity(m))
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// writeEnvMap hashes a map deterministically: keys sorted, each as key=value.
func writeEnvMap(h interface{ Write([]byte) (int, error) }, prefix string, m map[string]string) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		_, _ = h.Write([]byte(prefix + k + "=" + m[k] + "\x00"))
	}
}

// mcpServerIdentity is the stable, canonical identity of one MCP server entry.
// The laelia MCP proxy carries a per-turn LAELIA_SESSION_TOKEN in its env;
// that value rotates every turn and must NOT invalidate an otherwise-unchanged
// session, so it is excluded here. Everything else (name, command, args, env)
// participates so an MCP config change forces a cold start. Non-stdio
// transports (unused today) fall back to a full marshal so any future
// transport change still invalidates.
func mcpServerIdentity(m acp.McpServer) string {
	if s := m.Stdio; s != nil {
		env := make(map[string]string, len(s.Env))
		for _, e := range s.Env {
			if e.Name == "LAELIA_SESSION_TOKEN" {
				continue
			}
			env[e.Name] = e.Value
		}
		data, err := json.Marshal(struct {
			Name    string
			Command string
			Args    []string
			Env     map[string]string
		}{s.Name, s.Command, s.Args, env})
		if err == nil {
			return string(data)
		}
	}
	data, _ := json.Marshal(m)
	return string(data)
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
