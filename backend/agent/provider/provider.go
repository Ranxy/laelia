// Package provider discovers LLM agent providers (opencode, claude-code, ...)
// installed on the agent daemon's host and describes how to launch each one
// over ACP. Adding support for a new provider means implementing the Provider
// interface and registering it in Default; the proto surface is unchanged
// (provider ids are plain strings), and the "custom" provider is always
// available as an escape hatch for providers that are not built in.
package provider

import "context"

// ModelOption is one model selectable via the ACP session config option round
// trip. Value is the valueId the client sends to SetSessionConfigOption.
type ModelOption struct {
	Value       string
	Name        string
	Description string
}

// Discovered describes one provider detected on the host together with the
// models it advertised through the ACP NewSession config options.
type Discovered struct {
	ProviderID     string
	DisplayName    string
	Version        string
	ExecutablePath string
	// Models is empty when the provider does not advertise a model config
	// option (SupportsModelConfigOption false).
	Models                    []ModelOption
	SupportsModelConfigOption bool
}

// Provider is the extension point for a built-in LLM agent provider.
type Provider interface {
	// ID is the stable provider id stored in AgentACPConfig.provider
	// (e.g. "opencode", "claude-code").
	ID() string
	// DisplayName is the human-readable name shown in the UI.
	DisplayName() string
	// Detect reports whether the provider's binary is installed on the host
	// (PATH lookup + --version). Returns (info, true, nil) when present,
	// (nil, false, nil) when absent, and a non-nil error on probe failure.
	Detect(ctx context.Context) (info *Detected, present bool, err error)
	// BuildCommand returns the executable + args to spawn the provider's ACP
	// stdio server rooted at workspaceDir.
	BuildCommand(workspaceDir string) (executable string, args []string)
	// ProbeModels spawns the provider once, runs initialize+newSession, and
	// reads back the model config option advertised in NewSessionResponse.
	// Returns the model list, whether a model config option was observed at
	// all, and any error. An empty list with supportsConfigOption=true means
	// the agent advertised a model selector but exposed no options.
	ProbeModels(ctx context.Context, workspaceDir string) (models []ModelOption, supportsConfigOption bool, err error)
}

// Detected is the cheap, spawn-free result of Detect.
type Detected struct {
	ProviderID     string
	DisplayName    string
	Version        string
	ExecutablePath string
}
