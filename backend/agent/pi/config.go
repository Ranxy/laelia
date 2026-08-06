package pi

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"time"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

const (
	defaultMaxTimeoutSeconds = 1800
	defaultMaxEventCount     = 10000
	defaultMaxOutputBytes    = 1 << 20
	defaultOutputFlushBytes  = 4096

	// defaultStartupTimeout bounds the pi startup RPC round trip (spawn + first
	// get_state / switch_session). A pi that spawns but never answers within
	// this window is wedged (bad config, stuck download) and the turn fails fast
	// at ~StartupTimeout instead of hanging to MaxTimeoutSeconds. Overridable
	// per-agent via PiConfig.StartupTimeout.
	defaultStartupTimeout = 30 * time.Second

	// defaultIdleTimeout is how long a pi session stays resident after its last
	// turn ends before idle eviction tears down the subprocess to free memory.
	// The conversation is preserved (pi-session.json), so the next turn resumes
	// it via switch_session (warm, no init prompt) — the only cost is the 1-3s
	// cold-start respawn. A chat agent with a ~2s median cold start tolerates
	// 5min well; batch-heavy agents can lower it per-agent. Zero or negative
	// disables eviction (process stays resident). Overridable via
	// PiConfig.IdleTimeout.
	defaultIdleTimeout = 5 * time.Minute

	// APIProviderDeepseek and APIProviderOpenRouter are the LLM API providers
	// supported in phase 1. Each maps to a pi provider id + the env var pi reads
	// the API key from.
	APIProviderDeepseek   = "deepseek"
	APIProviderOpenRouter = "openrouter"
)

// apiProviderSpec maps an AgentACPConfig.api_provider to the pi provider id and
// the env var that carries its API key.
type apiProviderSpec struct {
	piProvider string
	keyEnv     string
}

var apiProviders = map[string]apiProviderSpec{
	APIProviderDeepseek:   {piProvider: "deepseek", keyEnv: "DEEPSEEK_API_KEY"},
	APIProviderOpenRouter: {piProvider: "openrouter", keyEnv: "OPENROUTER_API_KEY"},
}

// IsKnownAPIProvider reports whether id is a supported phase-1 API provider
// (deepseek or openrouter). Used by manager-side validation so the rule lives
// with the provider spec table rather than being duplicated in the API layer.
func IsKnownAPIProvider(id string) bool {
	_, ok := apiProviders[id]
	return ok
}

// piAllowEnv is the env whitelist the pi subprocess inherits from the host. It is
// narrower than the ACP executor's DefaultAllowEnv: pi is a self-contained
// binary and only needs PATH/HOME/locale/proxy to find its assets and reach the
// LLM API. The admin cannot widen this per-agent (pi config is provider+key, not
// a custom command), so it is a fixed set.
var piAllowEnv = []string{
	"PATH",
	"HOME",
	"LANG",
	"LC_ALL",
	"TERM",
	"HTTP_PROXY",
	"HTTPS_PROXY",
	"NO_PROXY",
}

// PiConfig is the fully-resolved configuration for a long-lived pi RPC session.
// The admin only sets AgentACPConfig.{provider, api_provider, api_key, model,
// persona_prompt}; BuildPiConfig fills in the launch shape and the daemon
// bootstrap env.
//
//nolint:revive // stutter: mirrors executor.ACPConfig sibling for symmetry.
type PiConfig struct {
	APIProvider   string // AgentACPConfig.api_provider ("deepseek"|"openrouter")
	Model         string // AgentACPConfig.model
	APIKey        string // AgentACPConfig.api_key
	PersonaPrompt string

	// WorkingDir is the per-agent dir pi runs in AND stores sessions under
	// (--session-dir). ~/.laelia/<machineID>/<agentID>/.
	WorkingDir string

	// PiBinaryPath is the resolved pi executable (dev env var or embedded blob).
	PiBinaryPath string

	// Agent identity + daemon bootstrap, stable for the machine lifetime. The
	// runner injects these so the LLM can shell out to `laelia-machine`.
	AgentResourceID string
	DaemonSocket    string
	SessionToken    string
	BinaryDir       string

	// MachineID/AgentID key the pi-session.json resume state file.
	MachineID string
	AgentID   string

	MaxTimeoutSeconds int32
	MaxEventCount     int32
	MaxOutputBytes    int64
	OutputFlushBytes  int32

	// StartupTimeout bounds the spawn + first get_state / switch_session round
	// trip. A pi that never answers within it is treated as wedged: the turn is
	// killed and failed at ~StartupTimeout rather than hanging to
	// MaxTimeoutSeconds. Defaults to defaultStartupTimeout when zero.
	StartupTimeout time.Duration

	// IdleTimeout is how long the subprocess stays resident after a turn ends
	// before idle eviction tears it down to free memory. The conversation is
	// preserved (pi-session.json), so the next turn resumes it warm via
	// switch_session; the only cost is the cold-start respawn. Zero or negative
	// disables eviction (the process stays resident, useful for debug or
	// batch-dense agents). Defaults to defaultIdleTimeout.
	IdleTimeout time.Duration

	// McpProxyURL is the localhost daemon proxy URL the managed-MCP pi
	// extension calls (LAELIA_MCP_PROXY_URL). Empty disables managed MCP tools.
	McpProxyURL string
}

// BuildPiConfig resolves the user-configurable AgentACPConfig into a PiConfig
// when provider == "builtin-pi". It returns nil otherwise (or when the required
// api_provider/api_key are missing), which the runner treats as "not a pi agent
// / not yet configured".
func BuildPiConfig(
	user *v1pb.AgentACPConfig,
	machineID, agentID, agentResourceID, piBinaryPath, daemonSocket, sessionToken, binaryDir string,
) *PiConfig {
	if user == nil || user.Provider != BuiltinPiProvider {
		return nil
	}
	if _, ok := apiProviders[user.ApiProvider]; !ok {
		return nil
	}
	if strings.TrimSpace(user.ApiKey) == "" {
		return nil
	}

	return &PiConfig{
		APIProvider:       user.ApiProvider,
		Model:             user.Model,
		APIKey:            user.ApiKey,
		PersonaPrompt:     user.PersonaPrompt,
		WorkingDir:        agentWorkingDir(machineID, agentID),
		PiBinaryPath:      piBinaryPath,
		AgentResourceID:   agentResourceID,
		DaemonSocket:      daemonSocket,
		SessionToken:      sessionToken,
		BinaryDir:         binaryDir,
		MachineID:         machineID,
		AgentID:           agentID,
		MaxTimeoutSeconds: defaultMaxTimeoutSeconds,
		MaxEventCount:     defaultMaxEventCount,
		MaxOutputBytes:    defaultMaxOutputBytes,
		OutputFlushBytes:  defaultOutputFlushBytes,
		StartupTimeout:    defaultStartupTimeout,
		IdleTimeout:       defaultIdleTimeout,
	}
}

// BuildPiCapability derives the agent capability for a builtin-pi config. It does
// not touch the filesystem; it only reflects that the pi runtime supports the
// same structured surface (diff, raw events, tool traces, autonomous decisions)
// as the ACP runtimes.
func BuildPiCapability(user *v1pb.AgentACPConfig) *v1pb.AgentCapability {
	if user == nil || user.Provider != BuiltinPiProvider {
		return &v1pb.AgentCapability{SupportsAcp: false, SupportsPi: false}
	}
	return &v1pb.AgentCapability{
		SupportsPi:                 true,
		SupportsAcp:                false,
		MaxTimeoutSeconds:          defaultMaxTimeoutSeconds,
		SupportsDiff:               true,
		SupportsRawEvents:          true,
		SupportsToolTraces:         true,
		MaxEventCount:              defaultMaxEventCount,
		MaxOutputBytes:             defaultMaxOutputBytes,
		SupportsAutonomousDecision: true,
	}
}

// agentWorkingDir is the per-agent pi session/working directory. It mirrors
// executor.AgentWorkingDir so pi agents share the same ~/.laelia/<m>/<a>/ home.
func agentWorkingDir(machineID, agentID string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".laelia", machineID, agentID)
}

// LaunchFingerprint covers everything that shapes the subprocess launch (api
// provider, model, api key, binary path). The runner compares it across config
// hot-reloads: an unchanged fingerprint keeps the warm session (conversation +
// init prompt preserved), a changed one means the running process is stale
// (e.g. a rotated API key baked into its env) and must be restarted.
func (c *PiConfig) LaunchFingerprint() string {
	h := sha256.New()
	_, _ = h.Write([]byte(c.APIProvider + "\x00" + c.Model + "\x00" + c.APIKey + "\x00" + c.PiBinaryPath))
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// buildPiEnv constructs the subprocess env: the whitelisted host env, overlaid
// with the LLM API key (named per api_provider) and the laelia-machine bootstrap
// vars. commandID is the opening turn's command id; the session is persistent so
// later turns inherit it (the manager treats CommandId as attribution only —
// see AckProcessedVersion, which advances the cursor via agent+version, not
// command — so staleness is harmless).
func (c *PiConfig) buildPiEnv(commandID string) []string {
	values := map[string]string{}
	for _, item := range os.Environ() {
		if k, v, ok := strings.Cut(item, "="); ok {
			values[k] = v
		}
	}
	filtered := map[string]string{}
	for _, key := range piAllowEnv {
		if v, ok := values[key]; ok {
			filtered[key] = v
		}
	}
	values = filtered

	// LLM API key for the configured provider.
	if spec, ok := apiProviders[c.APIProvider]; ok {
		values[spec.keyEnv] = c.APIKey
	}

	// laelia-machine bootstrap so the LLM can drive the chat loop from its shell.
	if c.DaemonSocket != "" {
		values["LAELIA_DAEMON_SOCKET"] = c.DaemonSocket
	}
	if c.SessionToken != "" {
		values["LAELIA_SESSION_TOKEN"] = c.SessionToken
	}
	if c.AgentResourceID != "" {
		values["LAELIA_AGENT"] = c.AgentResourceID
	}
	if commandID != "" {
		values["LAELIA_COMMAND"] = commandID
	}
	if c.BinaryDir != "" {
		existing := values["PATH"]
		if existing == "" {
			values["PATH"] = c.BinaryDir
		} else {
			values["PATH"] = c.BinaryDir + string(os.PathListSeparator) + existing
		}
	}
	if c.McpProxyURL != "" {
		values["LAELIA_MCP_PROXY_URL"] = c.McpProxyURL
	}

	env := make([]string, 0, len(values))
	for k, v := range values {
		env = append(env, k+"="+v)
	}
	return env
}

// launchArgs builds the `pi --mode rpc` argv. --no-skills/-no-prompt-templates
// keep the agent minimal and free of extension-UI dialogs that would block the
// headless drain loop; extensions stay enabled so the managed-MCP extension
// (written under .pi/extensions) can register MCP tools. --approve trusts the
// working dir so AGENTS.md/CLAUDE.md and project settings load.
func (c *PiConfig) launchArgs() []string {
	return []string{
		"--mode", "rpc",
		"--provider", apiProviders[c.APIProvider].piProvider,
		"--model", c.Model,
		"--session-dir", c.WorkingDir,
		"--no-skills",
		"--no-prompt-templates",
		"--approve",
	}
}
