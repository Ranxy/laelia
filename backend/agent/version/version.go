// Package version carries the machine binary build metadata. It is injected
// at build time via -ldflags -X (see scripts/build-embedded-machines.sh) so
// the running binary reports exactly the version, git commit, and build time
// recorded in the manager's embedded manifest.
package version

// Version is the machine binary version. "dev" for local go build/run;
// release builds overwrite it with the build VERSION.
var Version = "dev"

// GitCommit is the git commit hash the machine binary was built from.
var GitCommit = "unknown"

// BuildTime is the UTC build timestamp of the machine binary.
var BuildTime = "unknown"

// PromptBundleVersion is the content hash of the machine binary's embedded
// static prompt bundle (communication.md / agent_memory.md / reanchor.md /
// AgentFirstPromptBody). "dev" for local go build/run; release builds overwrite
// it with the build PROMPT_HASH so the manager can detect stale prompt bundles.
var PromptBundleVersion = "dev"
