package provider

import (
	"bytes"
	"context"
	"os/exec"
	"strings"

	"github.com/coder/acp-go-sdk"
)

// OpenCodeProvider discovers and launches the opencode CLI's ACP server.
type OpenCodeProvider struct{}

func (*OpenCodeProvider) ID() string          { return "opencode" }
func (*OpenCodeProvider) DisplayName() string { return "OpenCode" }

// ToolCallAdapter returns the OpenCodeAdapter: opencode's create carries only
// partial {cwd} metadata under a generic title; the real command and full
// RawInput arrive in the first in_progress status update.
func (*OpenCodeProvider) ToolCallAdapter() ToolCallAdapter { return OpenCodeAdapter{} }

// BuildCommand mirrors the launch shape used by the executor integration test
// (acp_executor_test.go: opencode acp --pure --cwd <workspace>).
func (*OpenCodeProvider) BuildCommand(workspaceDir string) (string, []string) {
	return "opencode", []string{"acp", "--pure", "--cwd", workspaceDir}
}

func (p *OpenCodeProvider) Detect(ctx context.Context) (*Detected, bool, error) {
	path, err := exec.LookPath("opencode")
	if err != nil {
		//nolint:nilerr // binary not on PATH -> provider absent, not a probe error
		return nil, false, nil
	}
	version := runVersionCmd(ctx, "opencode", "--version")
	return &Detected{
		ProviderID:     p.ID(),
		DisplayName:    p.DisplayName(),
		Version:        version,
		ExecutablePath: path,
	}, true, nil
}

func (p *OpenCodeProvider) ProbeModels(ctx context.Context, workspaceDir string) ([]ModelOption, bool, error) {
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

// runVersionCmd runs `<bin> <versionFlag...>` and returns the trimmed stdout
// (combined with stderr). It never returns an error: a missing version is not
// fatal for detection, which has already succeeded via LookPath.
func runVersionCmd(ctx context.Context, bin string, args ...string) string {
	cmd := exec.CommandContext(ctx, bin, args...)
	out, err := cmd.Output()
	if err != nil {
		// Some CLIs write --version to stderr; fall back to combined output.
		var combined bytes.Buffer
		cmd2 := exec.CommandContext(ctx, bin, args...)
		cmd2.Stdout = &combined
		cmd2.Stderr = &combined
		_ = cmd2.Run()
		return strings.TrimSpace(combined.String())
	}
	return strings.TrimSpace(string(out))
}

func selectOptionsToModels(opts acp.SessionConfigSelectOptions) []ModelOption {
	raw := selectOptions(opts)
	models := make([]ModelOption, 0, len(raw))
	for _, o := range raw {
		m := ModelOption{Value: string(o.Value), Name: o.Name}
		if o.Description != nil {
			m.Description = *o.Description
		}
		models = append(models, m)
	}
	return models
}
