package provider

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os/exec"

	"github.com/coder/acp-go-sdk"
)

// probeModelConfigOption spawns the provider's ACP server, runs initialize +
// newSession, and returns the model config option advertised in the response
// (or nil when the provider does not advertise one). The subprocess is torn
// down before returning.
func probeModelConfigOption(ctx context.Context, executable string, args []string, workspaceDir string) (*acp.SessionConfigOptionSelect, error) {
	if workspaceDir == "" {
		workspaceDir = "."
	}
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Dir = workspaceDir
	cmd.Env = probeEnv()

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	// Ensure the subprocess is reaped even on early return or error.
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	// Drain stderr so the provider's logs do not fill its pipe and block.
	go func(r io.Reader) { _, _ = io.Copy(io.Discard, r) }(stderr)

	return probeConn(ctx, workspaceDir, stdin, stdout)
}

// probeConn runs the initialize + newSession handshake against an ACP server
// reachable through stdin (client writes here) and stdout (client reads here),
// and returns the model config option advertised in the response, or nil when
// the server does not advertise one. cwd is sent as the session working
// directory (the protocol requires it, even for a probe that sends no prompt).
func probeConn(ctx context.Context, cwd string, stdin io.Writer, stdout io.Reader) (*acp.SessionConfigOptionSelect, error) {
	if cwd == "" {
		cwd = "."
	}
	client := &probeClient{}
	conn := acp.NewClientSideConnection(client, stdin, stdout)
	// The probe tears the subprocess down (Kill + Wait) once it has the
	// ConfigOptions, which closes the stdout pipe out from under the SDK's
	// reader goroutine. The SDK logs that teardown as `connection closed` at
	// INFO via slog.Default() — noisy and alarming to users for what is a
	// normal probe exit. Silence it with a discard logger; the probe's own
	// failure modes are reported via the returned error / empty models, not
	// through the SDK's internal logs.
	conn.SetLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))

	if _, err := conn.Initialize(ctx, acp.InitializeRequest{
		ProtocolVersion: acp.ProtocolVersionNumber,
		ClientCapabilities: acp.ClientCapabilities{
			Fs:       acp.FileSystemCapabilities{},
			Terminal: false,
		},
		ClientInfo: &acp.Implementation{Name: "laelia-agent-probe", Version: "0.1.0"},
	}); err != nil {
		return nil, err
	}

	resp, err := conn.NewSession(ctx, acp.NewSessionRequest{
		Cwd:        cwd,
		McpServers: []acp.McpServer{},
	})
	if err != nil {
		return nil, err
	}
	_ = resp.SessionId

	return findModelConfigOption(resp.ConfigOptions), nil
}

// findModelConfigOption returns the first select config option whose category
// is "model", or nil when none is advertised.
func findModelConfigOption(opts []acp.SessionConfigOption) *acp.SessionConfigOptionSelect {
	for i := range opts {
		sel := opts[i].Select
		if sel == nil || sel.Category == nil {
			continue
		}
		if *sel.Category == acp.SessionConfigOptionCategoryModel {
			return sel
		}
	}
	return nil
}

// selectOptions flattens both ungrouped and grouped option lists into a single
// slice, preserving advertised order.
func selectOptions(opts acp.SessionConfigSelectOptions) []acp.SessionConfigSelectOption {
	if opts.Ungrouped != nil {
		return *opts.Ungrouped
	}
	if opts.Grouped != nil {
		flat := make([]acp.SessionConfigSelectOption, 0)
		for _, g := range *opts.Grouped {
			flat = append(flat, g.Options...)
		}
		return flat
	}
	return nil
}

// probeClient is a minimal acp.Client used during provider probing. It rejects
// fs/permission/terminal callbacks (the probe never sends a prompt, so the
// agent should not invoke them) and discards session updates.
type probeClient struct{}

func (probeClient) ReadTextFile(context.Context, acp.ReadTextFileRequest) (acp.ReadTextFileResponse, error) {
	return acp.ReadTextFileResponse{}, errors.New("probe: fs.readTextFile not supported")
}

func (probeClient) WriteTextFile(context.Context, acp.WriteTextFileRequest) (acp.WriteTextFileResponse, error) {
	return acp.WriteTextFileResponse{}, errors.New("probe: fs.writeTextFile not supported")
}

func (probeClient) RequestPermission(context.Context, acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	return acp.RequestPermissionResponse{}, errors.New("probe: permission requests not supported")
}

func (probeClient) SessionUpdate(context.Context, acp.SessionNotification) error {
	return nil
}

func (probeClient) CreateTerminal(context.Context, acp.CreateTerminalRequest) (acp.CreateTerminalResponse, error) {
	return acp.CreateTerminalResponse{}, errors.New("probe: terminal not supported")
}

func (probeClient) KillTerminal(context.Context, acp.KillTerminalRequest) (acp.KillTerminalResponse, error) {
	return acp.KillTerminalResponse{}, errors.New("probe: terminal not supported")
}

func (probeClient) TerminalOutput(context.Context, acp.TerminalOutputRequest) (acp.TerminalOutputResponse, error) {
	return acp.TerminalOutputResponse{}, errors.New("probe: terminal not supported")
}

func (probeClient) ReleaseTerminal(context.Context, acp.ReleaseTerminalRequest) (acp.ReleaseTerminalResponse, error) {
	return acp.ReleaseTerminalResponse{}, errors.New("probe: terminal not supported")
}

func (probeClient) WaitForTerminalExit(context.Context, acp.WaitForTerminalExitRequest) (acp.WaitForTerminalExitResponse, error) {
	return acp.WaitForTerminalExitResponse{}, errors.New("probe: terminal not supported")
}
