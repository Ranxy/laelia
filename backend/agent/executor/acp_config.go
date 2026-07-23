package executor

import (
	"os"
	"path/filepath"

	"github.com/Ranxy/laelia/backend/agent/provider"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

const (
	defaultACPMaxTimeoutSeconds = 1800
	defaultACPMaxEventCount     = 10000
	defaultACPMaxOutputBytes    = 1 << 20
	defaultOutputFlushBytes     = 4096
)

// DefaultAllowEnv is the env var whitelist seeded onto every newly created
// agent. The admin may add or remove entries per agent via the config UI.
var DefaultAllowEnv = []string{
	"PATH",
	"HOME",
	"LANG",
	"TERM",
	"XDG_CONFIG_HOME",
	"XDG_DATA_HOME",
	"XDG_CACHE_HOME",
	"HTTP_PROXY",
	"HTTPS_PROXY",
	"NO_PROXY",
}

// defaultAutoApproveToolKinds is the template default for tool kinds the agent
// may run without asking for permission. "execute" is required so the LLM can
// shell out to the `laelia-agent` CLI to drive the chat loop during an
// autonomous drain session — without it, every CLI call would block on a
// permission prompt that no human is around to answer.
var defaultAutoApproveToolKinds = []string{"read", "search", "think", "fetch", "edit", "move", "execute"}

// ACPConfig is the internal, fully-resolved executor configuration. It is
// never user-authored: the admin only sets the AgentACPConfig proto fields
// (provider, model, custom_env, executable, args, allow_env), and BuildACPConfig
// fills in the template and derives the launch command from the provider
// registry when a built-in provider is selected.
type ACPConfig struct {
	MaxTimeoutSeconds int32 `yaml:"max_timeout_seconds"`
	MaxEventCount     int32 `yaml:"max_event_count"`
	MaxOutputBytes    int64 `yaml:"max_output_bytes"`
	OutputFlushBytes  int32 `yaml:"output_flush_bytes"`

	Provider      string   `yaml:"provider"`
	Model         string   `yaml:"model"`
	Executable    string   `yaml:"executable"`
	Args          []string `yaml:"args"`
	PersonaPrompt string   `yaml:"persona_prompt"`
	// Env is the template env overlay (currently unused; kept for the built-in
	// template). CustomEnv below is the admin-authored key-value overlay.
	Env                   map[string]string `yaml:"env"`
	CustomEnv             map[string]string `yaml:"custom_env"`
	AllowEnv              []string          `yaml:"allow_env"`
	WorkingDir            string            `yaml:"working_dir"`
	AdditionalDirectories []string          `yaml:"additional_directories"`
	ReadTextFiles         bool              `yaml:"read_text_files"`
	WriteTextFiles        bool              `yaml:"write_text_files"`
	AutoApproveToolKinds  []string          `yaml:"auto_approve_tool_kinds"`
	SupportsDiff          bool              `yaml:"supports_diff"`
	SupportsRawEvents     bool              `yaml:"supports_raw_events"`
	SupportsToolTraces    bool              `yaml:"supports_tool_traces"`
}

// AgentWorkingDir returns the per-agent persistent working directory under
// ~/.laelia/<machineID>/<agentID>/. A machine hosts many agents, so the machine
// id namespaces each agent's state on a shared host. The caller creates it.
func AgentWorkingDir(machineID, agentID string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".laelia", machineID, agentID)
}

// BuildACPConfig resolves the user-configurable AgentACPConfig (provider,
// model, custom_env, executable, args, allow_env) into a fully-populated
// ACPConfig by applying the built-in template. When provider selects a
// built-in provider, the executable + args are derived from the provider
// registry; otherwise (provider empty or "custom") the raw executable/args are
// used as-is. It returns nil when the agent has not been configured yet
// (neither a known provider nor an executable), which keeps the "not
// configured" gating in NewACP and reports supports_acp=false via Capability().
func BuildACPConfig(user *v1pb.AgentACPConfig, machineID, agentID string) *ACPConfig {
	if user == nil {
		return nil
	}

	executable, args := resolvedCommand(user, machineID, agentID)
	if executable == "" {
		return nil
	}

	cfg := &ACPConfig{
		MaxTimeoutSeconds: defaultACPMaxTimeoutSeconds,
		MaxEventCount:     defaultACPMaxEventCount,
		MaxOutputBytes:    defaultACPMaxOutputBytes,
		OutputFlushBytes:  defaultOutputFlushBytes,

		Provider:             user.Provider,
		Model:                user.Model,
		Executable:           executable,
		Args:                 args,
		PersonaPrompt:        user.PersonaPrompt,
		CustomEnv:            user.CustomEnv,
		AllowEnv:             user.AllowEnv,
		WorkingDir:           AgentWorkingDir(machineID, agentID),
		ReadTextFiles:        true,
		WriteTextFiles:       true,
		AutoApproveToolKinds: defaultAutoApproveToolKinds,
		SupportsDiff:         true,
		SupportsRawEvents:    true,
		SupportsToolTraces:   true,
	}
	return cfg
}

// resolvedCommand returns the executable + args to spawn. A built-in provider
// (looked up in the default registry) supplies its own launch command rooted
// at the agent working directory; anything else (provider "custom", empty, or
// unknown) falls back to the raw executable/args fields.
func resolvedCommand(user *v1pb.AgentACPConfig, machineID, agentID string) (string, []string) {
	if p, ok := provider.Default().Lookup(user.Provider); ok {
		return p.BuildCommand(AgentWorkingDir(machineID, agentID))
	}
	return user.Executable, user.Args
}

// BuildCapability derives the agent capability from the user-configurable ACP
// settings (template-provided flags + whether an executable is configured). It
// does not touch the filesystem and ignores the agent/machine ids.
func BuildCapability(user *v1pb.AgentACPConfig) *v1pb.AgentCapability {
	return BuildACPConfig(user, "", "").Capability()
}

func (c *ACPConfig) Capability() *v1pb.AgentCapability {
	if c == nil || c.Executable == "" {
		return &v1pb.AgentCapability{
			SupportsAcp:                false,
			MaxTimeoutSeconds:          defaultACPMaxTimeoutSeconds,
			SupportsDiff:               false,
			SupportsRawEvents:          false,
			SupportsToolTraces:         false,
			MaxEventCount:              defaultACPMaxEventCount,
			MaxOutputBytes:             defaultACPMaxOutputBytes,
			SupportsAutonomousDecision: false,
		}
	}

	return &v1pb.AgentCapability{
		SupportsAcp:                true,
		MaxTimeoutSeconds:          c.MaxTimeoutSeconds,
		SupportsDiff:               c.SupportsDiff,
		SupportsRawEvents:          c.SupportsRawEvents,
		SupportsToolTraces:         c.SupportsToolTraces,
		MaxEventCount:              c.MaxEventCount,
		MaxOutputBytes:             c.MaxOutputBytes,
		SupportsAutonomousDecision: true,
	}
}
