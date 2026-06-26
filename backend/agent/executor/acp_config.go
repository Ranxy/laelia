package executor

import (
	"os"
	"path/filepath"

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
// (executable, args, allow_env), and BuildACPConfig fills in the template.
type ACPConfig struct {
	MaxTimeoutSeconds int32 `yaml:"max_timeout_seconds"`
	MaxEventCount     int32 `yaml:"max_event_count"`
	MaxOutputBytes    int64 `yaml:"max_output_bytes"`
	OutputFlushBytes  int32 `yaml:"output_flush_bytes"`

	Executable            string            `yaml:"executable"`
	Args                  []string          `yaml:"args"`
	Env                   map[string]string `yaml:"env"`
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
// ~/.laelia/<agentID>/. The caller is responsible for creating it.
func AgentWorkingDir(agentID string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".laelia", agentID)
}

// BuildACPConfig resolves the user-configurable AgentACPConfig (executable,
// args, allow_env) into a fully-populated ACPConfig by applying the built-in
// template. It returns nil when the agent has not been configured yet
// (executable empty), which keeps the "not configured" gating in NewACP and
// reports supports_acp=false via Capability().
func BuildACPConfig(user *v1pb.AgentACPConfig, agentID string) *ACPConfig {
	if user == nil || user.Executable == "" {
		return nil
	}

	cfg := &ACPConfig{
		MaxTimeoutSeconds: defaultACPMaxTimeoutSeconds,
		MaxEventCount:     defaultACPMaxEventCount,
		MaxOutputBytes:    defaultACPMaxOutputBytes,
		OutputFlushBytes:  defaultOutputFlushBytes,

		Executable:           user.Executable,
		Args:                 user.Args,
		AllowEnv:             user.AllowEnv,
		WorkingDir:           AgentWorkingDir(agentID),
		ReadTextFiles:        true,
		WriteTextFiles:       true,
		AutoApproveToolKinds: defaultAutoApproveToolKinds,
		SupportsDiff:         true,
		SupportsRawEvents:    true,
		SupportsToolTraces:   true,
	}
	return cfg
}

// BuildCapability derives the agent capability from the user-configurable ACP
// settings (template-provided flags + whether an executable is configured). It
// does not touch the filesystem and ignores the agent id.
func BuildCapability(user *v1pb.AgentACPConfig) *v1pb.AgentCapability {
	return BuildACPConfig(user, "").Capability()
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
