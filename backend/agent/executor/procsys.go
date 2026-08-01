// Package executor helpers SetProcessGroup and KillGroup make an agent
// subprocess the leader of its own process group so the whole tree — the agent
// binary plus any descendants it spawns (npx/node, bash tool children, MCP
// servers) — can be torn down together on cancel/timeout/exit, and (on Linux)
// is killed automatically if the parent dies. Without these, a cancel only
// signals the direct child, leaving descendants orphaned and still able to run
// tool side effects after the user believes the command was stopped.
//
// The implementations are split by platform: Pdeathsig is Linux-only, and
// process groups use a different API on Windows the runtime does not wire up,
// so SetProcessGroup and KillGroup degrade there (see procsys_windows.go).
package executor
