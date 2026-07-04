package provider

import (
	"context"
	"os/exec"
)

// ClaudeCodeProvider discovers and launches the @agentclientprotocol/claude-agent-acp
// npm package via npx, the ACP server wrapper around Claude Code.
type ClaudeCodeProvider struct{}

func (*ClaudeCodeProvider) ID() string          { return "claude-code" }
func (*ClaudeCodeProvider) DisplayName() string { return "Claude Code" }

// BuildCommand launches the ACP wrapper via npx. The package pins to latest;
// npx caches it after the first download.
func (*ClaudeCodeProvider) BuildCommand(_ string) (string, []string) {
	return "npx", []string{"-y", "@agentclientprotocol/claude-agent-acp@latest"}
}

func (p *ClaudeCodeProvider) Detect(ctx context.Context) (*Detected, bool, error) {
	// The ACP server is launched through npx, so npx on PATH is the real
	// requirement. A locally installed `claude` CLI is a secondary signal but
	// not required (the npm wrapper bundles its own agent).
	path, err := exec.LookPath("npx")
	if err != nil {
		//nolint:nilerr // npx not on PATH -> provider absent, not a probe error
		return nil, false, nil
	}
	version := runVersionCmd(ctx, "npx", "--version")
	return &Detected{
		ProviderID:     p.ID(),
		DisplayName:    p.DisplayName(),
		Version:        version,
		ExecutablePath: path,
	}, true, nil
}

func (p *ClaudeCodeProvider) ProbeModels(ctx context.Context, workspaceDir string) ([]ModelOption, bool, error) {
	exe, args := p.BuildCommand(workspaceDir)
	sel, err := probeModelConfigOption(ctx, exe, args, workspaceDir)
	if err != nil {
		return nil, false, err
	}
	if sel == nil {
		return nil, false, nil
	}
	return selectOptionsToModels(sel.Options), true, nil
}
