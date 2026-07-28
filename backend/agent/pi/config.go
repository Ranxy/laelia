package pi

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

const (
	defaultMaxTimeoutSeconds = 1800
	defaultMaxEventCount     = 10000
	defaultMaxOutputBytes    = 1 << 20
	defaultOutputFlushBytes  = 4096

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
	// runner injects these so the LLM can shell out to `laelia-agent`.
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
// with the LLM API key (named per api_provider) and the laelia-agent bootstrap
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

	// laelia-agent bootstrap so the LLM can drive the chat loop from its shell.
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

	env := make([]string, 0, len(values))
	for k, v := range values {
		env = append(env, k+"="+v)
	}
	return env
}

// launchArgs builds the `pi --mode rpc` argv. --no-extensions/-no-skills etc.
// keep the agent minimal and free of extension-UI dialogs that would block the
// headless drain loop; --approve trusts the working dir so AGENTS.md/CLAUDE.md
// and project settings load.
func (c *PiConfig) launchArgs() []string {
	return []string{
		"--mode", "rpc",
		"--provider", apiProviders[c.APIProvider].piProvider,
		"--model", c.Model,
		"--session-dir", c.WorkingDir,
		"--no-extensions",
		"--no-skills",
		"--no-prompt-templates",
		"--approve",
	}
}
