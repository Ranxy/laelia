package provider

import (
	"context"

	"github.com/coder/acp-go-sdk"

	"github.com/Ranxy/laelia/backend/agent/acp2"
)

// ThreadProvider marks a provider that speaks the ACP v2 thread protocol.
// Providers implementing it are driven by the thread executor; any future v2
// agent only needs to implement this interface and register, without touching
// the executor or the runner.
type ThreadProvider interface {
	// ThreadCommand returns the executable + args that launch the provider's
	// v2 thread server rooted at workspaceDir (e.g. codex app-server
	// --listen stdio://). It may differ from the v1 BuildCommand.
	ThreadCommand(workspaceDir string) (executable string, args []string)
	// NewThreadMapper returns the notification -> neutral event mapper for
	// this provider's wire shape.
	NewThreadMapper() acp2.EventMapper
	// ThreadMcpArgs converts managed MCP servers into provider-specific CLI
	// args (codex: -c mcp_servers.<name>.url=<url>). The executor prepends
	// them to the ThreadCommand args.
	ThreadMcpArgs(servers []acp.McpServer) []string
	// ProbeModelsV2 returns the models the provider advertises over the v2
	// protocol (model/list with a cache fallback).
	ProbeModelsV2(ctx context.Context, workspaceDir string) ([]ModelOption, error)
}

// ThreadCompatChecker is implemented by ThreadProviders that must verify
// their binary is compatible before spawning (e.g. a minimum codex version).
// The executor calls it before launching the app-server so an incompatible
// install fails fast with a clear error instead of a confusing handshake
// failure.
type ThreadCompatChecker interface {
	// CheckThreadCompat verifies the provider's binary is compatible and
	// returns the executable to spawn, or an error describing the
	// incompatibility.
	CheckThreadCompat(ctx context.Context) (executable string, err error)
}
