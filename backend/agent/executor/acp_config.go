package executor

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

const (
	defaultACPMaxTimeoutSeconds = 1800
	defaultACPMaxEventCount     = 10000
	defaultACPMaxOutputBytes    = 1 << 20
	defaultOutputFlushBytes     = 4096
)

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

func DefaultACPConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".laelia", "acp.yaml")
}

func LoadACPConfigFromFile(path string) (*ACPConfig, error) {
	if path == "" {
		path = DefaultACPConfigPath()
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return LoadACPConfigFromYAML(string(data))
}

func LoadACPConfigFromYAML(yamlStr string) (*ACPConfig, error) {
	if yamlStr == "" {
		return nil, nil
	}

	var cfg ACPConfig
	if err := yaml.Unmarshal([]byte(yamlStr), &cfg); err != nil {
		return nil, err
	}

	if cfg.Executable == "" {
		return nil, nil
	}

	if cfg.MaxTimeoutSeconds <= 0 {
		cfg.MaxTimeoutSeconds = defaultACPMaxTimeoutSeconds
	}
	if cfg.MaxEventCount <= 0 {
		cfg.MaxEventCount = defaultACPMaxEventCount
	}
	if cfg.MaxOutputBytes <= 0 {
		cfg.MaxOutputBytes = defaultACPMaxOutputBytes
	}
	if cfg.OutputFlushBytes <= 0 {
		cfg.OutputFlushBytes = defaultOutputFlushBytes
	}

	return &cfg, nil
}

func (c *ACPConfig) Capability() *v1pb.AgentCapability {
	if c == nil || c.Executable == "" {
		return &v1pb.AgentCapability{
			SupportsAcp:        false,
			MaxTimeoutSeconds:  defaultACPMaxTimeoutSeconds,
			SupportsDiff:       false,
			SupportsRawEvents:  false,
			SupportsToolTraces: false,
			MaxEventCount:      defaultACPMaxEventCount,
			MaxOutputBytes:     defaultACPMaxOutputBytes,
		}
	}

	return &v1pb.AgentCapability{
		SupportsAcp:        true,
		MaxTimeoutSeconds:  c.MaxTimeoutSeconds,
		SupportsDiff:       c.SupportsDiff,
		SupportsRawEvents:  c.SupportsRawEvents,
		SupportsToolTraces: c.SupportsToolTraces,
		MaxEventCount:      c.MaxEventCount,
		MaxOutputBytes:     c.MaxOutputBytes,
	}
}
