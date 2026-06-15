package executor

import (
	"errors"
	"os"
	"path/filepath"
	"slices"

	"gopkg.in/yaml.v3"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

const (
	defaultACPMaxTimeoutSeconds = 1800
	defaultACPMaxEventCount     = 10000
	defaultACPMaxOutputBytes    = 1 << 20
)

type ACPConfig struct {
	DefaultProfile    string                `yaml:"default_profile"`
	MaxTimeoutSeconds int32                 `yaml:"max_timeout_seconds"`
	MaxEventCount     int32                 `yaml:"max_event_count"`
	MaxOutputBytes    int64                 `yaml:"max_output_bytes"`
	Profiles          map[string]ACPProfile `yaml:"profiles"`
}

type ACPProfile struct {
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

func LoadACPConfig(path string) (*ACPConfig, error) {
	if path == "" {
		path = DefaultACPConfigPath()
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	var cfg ACPConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	if len(cfg.Profiles) == 0 {
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
	if cfg.DefaultProfile == "" {
		profiles := cfg.AvailableProfiles()
		if len(profiles) > 0 {
			cfg.DefaultProfile = profiles[0]
		}
	}

	return &cfg, nil
}

func (c *ACPConfig) AvailableProfiles() []string {
	if c == nil {
		return nil
	}
	profiles := make([]string, 0, len(c.Profiles))
	for name, profile := range c.Profiles {
		if profile.Executable == "" {
			continue
		}
		profiles = append(profiles, name)
	}
	slices.Sort(profiles)
	return profiles
}

func (c *ACPConfig) ResolveProfile(name string) (string, ACPProfile, error) {
	if c == nil {
		return "", ACPProfile{}, errors.New("ACP is not configured on this agent")
	}
	if name == "" {
		name = c.DefaultProfile
	}
	if name == "" {
		return "", ACPProfile{}, errors.New("ACP profile must be specified")
	}
	profile, ok := c.Profiles[name]
	if !ok || profile.Executable == "" {
		return "", ACPProfile{}, errors.New("ACP profile not found")
	}
	return name, profile, nil
}

func (c *ACPConfig) Capability() *v1pb.AgentCapability {
	profiles := c.AvailableProfiles()
	if len(profiles) == 0 {
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

	supportsDiff := false
	supportsRawEvents := false
	supportsToolTraces := false
	for _, name := range profiles {
		profile := c.Profiles[name]
		supportsDiff = supportsDiff || profile.SupportsDiff
		supportsRawEvents = supportsRawEvents || profile.SupportsRawEvents
		supportsToolTraces = supportsToolTraces || profile.SupportsToolTraces
	}

	return &v1pb.AgentCapability{
		SupportsAcp:        true,
		AvailableProfiles:  profiles,
		MaxTimeoutSeconds:  c.MaxTimeoutSeconds,
		SupportsDiff:       supportsDiff,
		SupportsRawEvents:  supportsRawEvents,
		SupportsToolTraces: supportsToolTraces,
		MaxEventCount:      c.MaxEventCount,
		MaxOutputBytes:     c.MaxOutputBytes,
		DefaultProfile:     c.DefaultProfile,
	}
}
