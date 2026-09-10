package client

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"github.com/Ranxy/laelia/backend/agent/chattools"
	daemonsrv "github.com/Ranxy/laelia/backend/agent/daemon"
	"github.com/Ranxy/laelia/backend/agent/executor"
	"github.com/Ranxy/laelia/backend/agent/home"
	"github.com/Ranxy/laelia/backend/agent/outbox"
	"github.com/Ranxy/laelia/backend/agent/pi"
	"github.com/Ranxy/laelia/backend/agent/provider"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

// agentRunner owns one agent's drain loop. A machine hosts one
// runner per assigned agent; the runner is spawned on AgentAssignment (or on
// connect from the assigned_agents list) and torn down on RemoveAgent. The
// runner's runtime config is hot-reloadable via AgentConfigUpdate /
// ReloadAgentAssignment, picked up at the next BeginSession.
//
// An agent is backed by EXACTLY ONE runtime: either an ACP config (claude-code
// / opencode, spawned per turn) OR a pi config (builtin-pi or user-installed
// pi, one long-lived `pi --mode rpc` subprocess shared across turns). The two
// never coexist on the same runner; applyAssignment flips between them and
// tears down the other side. All runners share the machine's access token and
// the machine-level daemon socket.
type agentRunner struct {
	machine     *MachineClient
	daemon      *daemonsrv.Server
	agentName   string // full agents/{id}
	agentID     string // bare uuid
	displayName string

	mu        sync.Mutex
	acpConfig *executor.ACPConfig
	piConfig  *pi.PiConfig
	piSession *pi.Session
	// threadSession, when non-nil, is the agent's resident ACP v2 app-server
	// (resident mode, env LAELIA_ACP2_SESSION=1): one long-lived subprocess
	// shared across turns. It never coexists with piSession; applyAssignment
	// tears the thread side down when pi takes over and vice versa.
	threadSession *executor.ThreadSession
	// cs is this runner's command stream, set in start and read by applyAssignment
	// to coordinate an in-flight drain turn on a config hot-reload.
	cs     *commandStream
	cancel context.CancelFunc
	done   chan struct{}
}

// buildAcpConfig resolves the server-owned AgentACPConfig into a runnable
// ACPConfig for this agent + machine, creating the per-agent working dir. It
// returns nil for an agent that is not yet configured (no provider/executable),
// which keeps the runner inert until the admin sets a config.
func (r *agentRunner) buildAcpConfig(assignment *v1pb.AgentAssignment) *executor.ACPConfig {
	cfg := executor.BuildACPConfig(assignment.GetAcpConfig(), r.machine.machineID, r.agentID)
	if cfg == nil {
		return nil
	}
	if err := os.MkdirAll(cfg.WorkingDir, 0o700); err != nil {
		slog.Warn("failed to create agent working dir", "dir", cfg.WorkingDir, "error", err)
		return nil
	}
	return cfg
}

// userPiDetectTimeout bounds the local user-pi version probe. A slow or hung
// `pi` on PATH must not stall agent config application.
const userPiDetectTimeout = 5 * time.Second

// buildPiConfig resolves the server-owned AgentACPConfig into a pi config +
// creates the per-agent working dir. Returns nil if the assignment is not a
// configured pi agent (provider != builtin-pi/user-pi, unknown api_provider,
// missing key/model, or unavailable binary), which keeps the runner inert.
func (r *agentRunner) buildPiConfig(assignment *v1pb.AgentAssignment) *pi.PiConfig {
	var piBinary string
	if assignment.GetAcpConfig().GetProvider() == pi.UserPiProvider {
		p, ok := provider.Default().Lookup(provider.PiProviderID)
		if !ok {
			slog.Warn("user pi provider not registered; agent stays inert", "agent", r.agentName)
			return nil
		}
		detectCtx, cancel := context.WithTimeout(context.Background(), userPiDetectTimeout)
		defer cancel()
		info, present, err := p.Detect(detectCtx)
		if err != nil || !present || info == nil {
			slog.Warn("user pi not detected; agent stays inert", "agent", r.agentName, "error", err)
			return nil
		}
		if !info.Compatible {
			slog.Warn("user pi incompatible; agent stays inert", "agent", r.agentName, "version", info.Version, "reason", info.IncompatibilityReason)
			return nil
		}
		piBinary = info.ExecutablePath
	} else {
		var err error
		piBinary, err = pi.ResolveBinary()
		if err != nil {
			slog.Warn("pi binary unavailable; agent stays inert", "agent", r.agentName, "error", err)
			return nil
		}
	}
	cfg := pi.BuildPiConfig(
		assignment.GetAcpConfig(),
		r.machine.machineID, r.agentID, r.agentID,
		piBinary, r.daemon.SocketPath(), r.daemon.SessionToken(), r.machine.binaryDir,
	)
	if cfg == nil {
		return nil
	}
	if proxyURL, proxyErr := r.daemon.McpProxyURLForAgent(r.agentID); proxyErr != nil {
		slog.Warn("failed to resolve managed mcp proxy for pi agent", "agent", r.agentName, "error", proxyErr)
	} else {
		cfg.McpProxyURL = proxyURL
	}
	if err := os.MkdirAll(cfg.WorkingDir, 0o700); err != nil {
		slog.Warn("failed to create agent working dir", "dir", cfg.WorkingDir, "error", err)
		return nil
	}
	return cfg
}

// applyAssignment is the single config-entry point: it resolves the assignment
// to either an ACP or a pi config, hot-reloading the in-place runner. For a pi
// agent, an unchanged launch fingerprint keeps the warm session; a changed one
// restarts the subprocess so the new launch shape (provider/model/key/binary)
// takes effect. The non-active side is always torn down so the two runtimes
// never coexist.
//
// Only a teardown that kills a subprocess under a possibly in-flight turn
// coordinates that turn first (cancel + bounded wait) so the restart never
// races the dying turn's session access and the turn reports an explicit
// reload cause. Everything else — notably a reconnect's roster resync
// re-applying an UNCHANGED assignment — must never touch a running turn: the
// machine reconnects on its own schedule (e.g. a proxy cutting the control
// stream) and reconcileAssignments runs on every connect, so an unconditional
// cancel here would fail every turn that happens to be running across a
// reconnect (§3.2: a dead stream never touches a running turn).
func (r *agentRunner) applyAssignment(a *v1pb.AgentAssignment) {
	acp := a.GetAcpConfig()
	if acp != nil && pi.IsPiProvider(acp.GetProvider()) {
		newPi := r.buildPiConfig(a)
		if newPi == nil {
			// Unusable pi assignment: coordinated teardown of both runtimes.
			r.coordinateInFlightTurn(reloadedMidTurnReason)
			r.stopThreadSession()
			r.stopPiSession()
			r.setConfig(nil)
			return
		}
		prev := r.currentPiConfig()
		if prev != nil && prev.LaunchFingerprint() == newPi.LaunchFingerprint() {
			// Unchanged launch shape: keep the warm session AND the running
			// turn; just refresh the config (e.g. a persona_prompt change).
			// A thread session cannot exist for a pi agent; if one does (a
			// race from a flip-flopping roster), coordinate before the stop.
			if r.currentThreadSession() != nil {
				r.coordinateInFlightTurn(reloadedMidTurnReason)
				r.stopThreadSession()
			}
			r.setPiConfig(newPi)
			return
		}
		// Launch shape changed (or first pi config): cancel any in-flight
		// drain turn and wait for it to end, THEN restart the subprocess. The
		// cancel surfaces an explicit "config reloaded mid-turn" failure to
		// the manager (not a mid-flight "session exited mid-turn") and the
		// wait guarantees the restart never races the dying turn's session
		// access.
		r.coordinateInFlightTurn(reloadedMidTurnReason)
		r.stopThreadSession()
		r.setConfig(nil)
		r.restartPiSession(newPi)
		return
	}
	// ACP (or unconfigured). Only a teardown that can kill the running turn
	// coordinates it: a resident pi session must go (pi→ACP switch), and a
	// resident thread session must go when the agent is no longer a
	// resident-thread agent. A same-shape ACP reload (setConfig only) is a
	// next-turn concern — buildThreadRuntime re-fingerprints at the next turn
	// — so a reconnect's roster resync leaves a running turn alone.
	if r.currentPiConfig() != nil {
		r.coordinateInFlightTurn(reloadedMidTurnReason)
		r.stopPiSession()
	}
	cfg := r.buildAcpConfig(a)
	r.setConfig(cfg)
	// A resident thread session survives config hot-reloads (the next turn's
	// buildThreadRuntime restarts it on a launch-shape change).
	if !threadResidentEligible(cfg) && r.currentThreadSession() != nil {
		r.coordinateInFlightTurn(reloadedMidTurnReason)
		r.stopThreadSession()
	}
}

func (r *agentRunner) setConfig(cfg *executor.ACPConfig) {
	r.mu.Lock()
	r.acpConfig = cfg
	r.mu.Unlock()
}

func (r *agentRunner) currentConfig() *executor.ACPConfig {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.acpConfig
}

func (r *agentRunner) setPiConfig(cfg *pi.PiConfig) {
	r.mu.Lock()
	r.piConfig = cfg
	r.mu.Unlock()
}

func (r *agentRunner) currentPiConfig() *pi.PiConfig {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.piConfig
}

// currentThreadSession returns the resident thread session (nil when none).
func (r *agentRunner) currentThreadSession() *executor.ThreadSession {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.threadSession
}

// restartPiSession swaps the pi session for a fresh one bound to cfg. The new
// session object is built first (cheap — no process spawn; Start is lazy on the
// first turn so the opening turn's command id seeds LAELIA_COMMAND), then piConfig
// and piSession are swapped together under one r.mu critical section, and the
// OLD session is stopped outside the lock. This leaves no window where a
// concurrent drain turn could see piSession==nil with a stale piConfig and lazily
// create a session bound to the OLD config that this swap would then orphan (its
// Background-derived ctx never cancelled → a stale-shape subprocess runs
// forever). The session ctx is derived from context.Background (NOT the runner's
// stream ctx) so a turn-end cancel or a transient stream drop never SIGKILLs the
// persistent subprocess; only an explicit stopPiSession/Stop cancels it.
func (r *agentRunner) restartPiSession(cfg *pi.PiConfig) {
	ctx, cancel := context.WithCancel(context.Background())
	newSess := pi.NewSession(ctx, cancel, cfg)
	r.mu.Lock()
	old := r.piSession
	r.piSession = newSess
	r.piConfig = cfg
	r.mu.Unlock()
	if old != nil {
		old.Stop()
	}
}

// stopPiSession tears down the pi subprocess and clears the pi config. The
// config and session are cleared together under r.mu BEFORE the blocking Stop so
// a concurrent drain turn cannot see piSession==nil with a stale piConfig and
// lazily create a session that this teardown would orphan.
func (r *agentRunner) stopPiSession() {
	r.mu.Lock()
	sess := r.piSession
	r.piSession = nil
	r.piConfig = nil
	r.mu.Unlock()
	if sess != nil {
		sess.Stop()
	}
}

// stopThreadSession tears down the resident thread subprocess and clears the
// session. The session pointer is cleared under r.mu BEFORE the blocking Stop
// so a concurrent buildThreadRuntime cannot resurrect a torn-down session. The
// caller coordinates any in-flight turn first (see coordinateInFlightTurn).
func (r *agentRunner) stopThreadSession() {
	r.mu.Lock()
	sess := r.threadSession
	r.threadSession = nil
	r.mu.Unlock()
	if sess != nil {
		sess.Stop()
	}
}

// start starts the runner's two long-lived loops — the uploader (command data
// reporting over the unary UploadCommandData RPC) and the drain loop (turn
// execution, pulling work through the unary BeginSession RPC) — both bound to
// the runner's lifetime, not to any single stream. Control-plane interactions
// arrive on the machine's MachineChannel (routed by machine_control.go), so
// there is no per-agent stream to reconnect. It returns immediately; the
// runner's lifetime ends when all loops exit (ctx cancelled). Safe to call
// only once per runner; stop cancels and waits.
func (r *agentRunner) start(ctx context.Context) {
	streamCtx, cancel := context.WithCancel(ctx)
	r.cancel = cancel
	r.done = make(chan struct{})

	cs := newCommandStream(
		r.machine.streamClient,
		r.machine.managerURL,
		r.daemon.SocketPath(),
		r.daemon.SessionToken(),
		r.machine.binaryDir,
		r.agentName,
		r.agentID,
		r.machine.machineID,
	)
	cs.getToken = func() string {
		r.machine.mu.RLock()
		defer r.machine.mu.RUnlock()
		return r.machine.accessToken
	}
	cs.getAcpConfig = r.currentConfig
	cs.newSessionRuntime = r.buildRuntimeForAgent
	cs.buildTurnBatch = func(ctx context.Context) (string, error) {
		return chattools.BuildTurnBatch(ctx, r.daemon.BatchDeps(r.agentID))
	}
	cs.sendMachine = r.machine.sendOnControlStream
	r.wireOutbox(cs)

	r.mu.Lock()
	r.cs = cs
	r.mu.Unlock()

	go func() {
		defer close(r.done)
		// The lifecycle matrix (phase 2/3): the drain loop and the uploader
		// are permanent (runner lifetime); command data and the work pull are
		// unary RPCs, so a dead control stream never touches a running turn.
		var wg sync.WaitGroup
		wg.Go(func() {
			cs.uploader.Run(streamCtx)
		})
		wg.Go(func() {
			cs.drainLoop(streamCtx)
		})
		wg.Wait()
	}()
	slog.Info("started agent runner", "agent", r.agentName, "displayName", r.displayName)
}

// wireOutbox opens the agent's durable outbox and its uploader. The outbox
// lives on disk under the agent's data dir, so records survive runner and
// machine restarts; the uploader runs on the runner's ctx (outliving
// connections — a dead stream never stops reporting) and re-reads from the
// log start after a restart: the manager's (command, seq) dedup makes the
// replay idempotent. An open failure leaves the runner up but inert for
// reporting: every turn fails fast with the §3.1 bypass unavailable, and the
// manager's reaper closes the RUNNING rows.
func (r *agentRunner) wireOutbox(cs *commandStream) {
	ob, err := outbox.Open(outbox.AgentOutboxDir(home.Dir(), r.machine.machineID, r.agentID))
	if err != nil {
		slog.Error("failed to open agent outbox; turns cannot record until it recovers",
			"agent", r.agentName, "error", err)
		cs.sink = outboxSink{}
		return
	}
	cs.ob = ob
	cs.sink = outboxSink{ob: ob}
	cs.uploader = outbox.NewUploader(ob, r.machine.uploadCommandData(cs.getToken))
	// §3.7 startup self-check: the WAL is opened fresh, so any command group
	// still holding records without a terminal belongs to a turn the previous
	// process died inside (the graceful-stop path already synthesized them).
	// The synthetic terminals are durable here and upload with the first
	// cycle, so the manager closes the commands without waiting for the reaper.
	if err := cs.uploader.SynthesizeInterruptedTerminals("machine restarted mid-turn"); err != nil {
		slog.Warn("outbox startup self-check failed", "agent", r.agentName, "error", err)
	}
}

func (r *agentRunner) currentCommandStream() *commandStream {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cs
}

// reloadedMidTurnReason is the terminal cause a coordinated cancel reports when
// an assignment hot-reload killed the turn.
const reloadedMidTurnReason = "config reloaded mid-turn"

// inFlightTurnTimeout bounds how long coordinateInFlightTurn waits for an
// in-flight turn to end after cancelling it. A runtime that ignores Cancel is
// reaped by the subsequent restartPiSession's stopPiSession (the safe Stop
// blocks on the process reap), so the wait is best-effort and bounded. A
// variable so tests can shorten the wait.
var inFlightTurnTimeout = 5 * time.Second

// coordinateInFlightTurn cancels any in-flight drain turn and waits (bounded)
// for it to end. Callers pass the cause that the dying turn's terminal should
// carry (e.g. "config reloaded mid-turn", or the runner's teardown cause). The
// teardown paths (applyAssignment before every SIGKILL-prone session restart,
// stop before the runner ctx dies) call this so the restart never races the
// dying turn's session access and the manager sees an explicit cause instead
// of a mid-flight "session exited mid-turn". No-op when no turn is in flight.
func (r *agentRunner) coordinateInFlightTurn(reason string) {
	cs := r.currentCommandStream()
	if cs == nil {
		return
	}
	done, cancelled := cs.CancelInFlight(reason)
	if !cancelled {
		return
	}
	select {
	case <-done:
	case <-time.After(inFlightTurnTimeout):
		slog.Warn("in-flight turn did not end after cancel; restarting anyway",
			"agent", r.agentName, "timeout", inFlightTurnTimeout)
	}
}

// coldRestart force-cold-restarts this agent's LLM session: it cancels any
// in-flight turn, clears the persisted session state files (acp-session.json /
// pi-session.json), and restarts the long-lived runtime so the next turn
// starts from a fresh cold start (re-sends the init prompt). The runner stays
// alive; only the LLM conversation context is dropped.
func (r *agentRunner) coldRestart() {
	// 1. Cancel any in-flight turn and wait (bounded) so the restart never
	// races the dying turn's session access.
	r.coordinateInFlightTurn("agent cold-restarted mid-turn")

	// 2. Clear persisted LLM session state so the next turn cold-starts.
	executor.ClearSessionState(home.Join(r.machine.machineID, r.agentID, "acp-session.json"))
	executor.ClearSessionState(home.Join(r.machine.machineID, r.agentID, "pi-session.json"))

	// 3. Restart the long-lived runtime so in-memory warm state is also dropped.
	r.mu.Lock()
	piCfg := r.piConfig
	r.mu.Unlock()
	if piCfg != nil {
		// A fresh Session starts lazily on the next turn; because the pi-session
		// file is gone, resumeOrCapture cold-starts instead of resuming.
		r.restartPiSession(piCfg)
	}
	r.stopThreadSession()

	// 4. Wake the drain loop so pending work is picked up immediately with a
	// fresh session.
	if cs := r.currentCommandStream(); cs != nil {
		cs.wake()
	}
	slog.Info("cold restarted agent", "agent", r.agentName)
}

// buildRuntimeForAgent is the per-turn runtime branch point, overriding the
// commandStream's default ACP-only builder. A pi agent gets a per-turn
// PiExecutor over the shared long-lived pi session; every other agent gets the
// existing ACP executor spawned per turn. The drain loop's Request already
// carries the command/turn fields; this fills the machine/daemon wiring.
func (r *agentRunner) buildRuntimeForAgent(req executor.Request) (executor.Runtime, error) {
	ereq := req
	ereq.AgentResourceID = r.agentID
	ereq.AgentID = r.agentID
	ereq.MachineID = r.machine.machineID
	ereq.DaemonSocket = r.daemon.SocketPath()
	ereq.SessionToken = r.daemon.SessionToken()
	ereq.BinaryDir = r.machine.binaryDir
	// Snapshot piConfig and piSession together under one lock so a concurrent
	// restart's atomic swap can't split them: the turn either sees the old pair
	// or the new pair, never a stale config with the wrong session. The invariant
	// (piConfig != nil ⟺ piSession != nil) is maintained by restartPiSession /
	// stopPiSession, so the lazy-create branch is unreachable in normal flow;
	// if it ever fires it binds a session to the CURRENT config and stores it
	// under the same lock, so it can never be orphaned by an overwrite.
	r.mu.Lock()
	piCfg := r.piConfig
	sess := r.piSession
	if piCfg != nil && sess == nil {
		ctx, cancel := context.WithCancel(context.Background())
		sess = pi.NewSession(ctx, cancel, piCfg)
		r.piSession = sess
	}
	r.mu.Unlock()
	if piCfg != nil {
		return pi.NewPi(ereq, sess, piCfg)
	}
	cfg := r.currentConfig()
	if cfg == nil {
		return executor.NewACP(ereq, nil)
	}
	copyCfg := *cfg
	copyCfg.McpServers = r.buildMcpServers(ereq)
	threadCfg := executor.BuildThreadConfig(&copyCfg)
	// Thread-protocol providers (codex and future agents) run on the v2
	// thread executor. A built-in provider's protocol is fixed by its
	// implementation; a "custom" provider that declares protocol "acp-v2"
	// runs the thread executor through an adapter over its raw command.
	// Everything else keeps the v1 session executor.
	if p, ok := provider.Default().Lookup(cfg.Provider); ok {
		if tp, ok2 := p.(provider.ThreadProvider); ok2 {
			return r.buildThreadRuntime(ereq, threadCfg, tp)
		}
	} else if cfg.Protocol == executor.ProtocolV2 && cfg.Executable != "" {
		tp := provider.NewCustomThreadProvider(copyCfg.Executable, copyCfg.Args)
		return r.buildThreadRuntime(ereq, threadCfg, tp)
	}
	return executor.NewACP(ereq, &copyCfg)
}

// threadSessionEnabled reports whether ACP v2 thread agents run as one
// long-lived resident subprocess shared across turns (env LAELIA_ACP2_SESSION=1).
// Default off: each turn spawns a fresh app-server, which is simpler and frees
// the (memory-heavy) agent runtime while idle.
func threadSessionEnabled() bool {
	return os.Getenv("LAELIA_ACP2_SESSION") == "1"
}

// threadSessionIdleTimeout is how long a resident subprocess stays alive after
// its last turn before idle eviction (env LAELIA_ACP2_SESSION_IDLE, default
// 5m). Zero disables eviction.
func threadSessionIdleTimeout() time.Duration {
	if v := os.Getenv("LAELIA_ACP2_SESSION_IDLE"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d >= 0 {
			return d
		}
	}
	return 5 * time.Minute
}

// threadResidentEligible reports whether the ACP config runs on the v2 thread
// protocol with resident mode enabled: a built-in ThreadProvider (its protocol
// is fixed by its implementation) or a custom provider declaring acp-v2.
func threadResidentEligible(cfg *executor.ACPConfig) bool {
	if cfg == nil || !threadSessionEnabled() {
		return false
	}
	if p, ok := provider.Default().Lookup(cfg.Provider); ok {
		_, isThread := p.(provider.ThreadProvider)
		return isThread
	}
	return cfg.Protocol == executor.ProtocolV2 && cfg.Executable != ""
}

// buildThreadRuntime returns a v2 thread runtime for the given provider: the
// resident ThreadSession mode (one long-lived app-server shared across turns)
// or the per-turn ThreadExecutor. In resident mode the session is created
// lazily on the first turn; a launch-shape change (provider/model/working dir/
// protocol/command) swaps the session — the old subprocess is stopped outside
// the lock, a matching fingerprint keeps the warm process.
func (r *agentRunner) buildThreadRuntime(ereq executor.Request, threadCfg *executor.ThreadConfig, tp provider.ThreadProvider) (executor.Runtime, error) {
	if !threadSessionEnabled() {
		return executor.NewThread(ereq, threadCfg, tp)
	}
	threadCfg.IdleTimeout = threadSessionIdleTimeout()
	fp := executor.ThreadLaunchFingerprint(threadCfg, tp)
	r.mu.Lock()
	sess := r.threadSession
	var old *executor.ThreadSession
	if sess == nil || sess.LaunchFingerprint() != fp {
		ctx, cancel := context.WithCancel(context.Background())
		sess = executor.NewThreadSession(ctx, cancel, ereq, threadCfg, tp)
		old = r.threadSession
		r.threadSession = sess
	}
	r.mu.Unlock()
	// The old launch shape is torn down outside the lock so a concurrent turn
	// never sees a half-swapped pair (mirrors restartPiSession).
	if old != nil {
		old.Stop()
	}
	return executor.NewThreadWithSession(ereq, threadCfg, tp, sess)
}

// buildMcpServers discovers the agent's managed MCP tools through the daemon
// and returns an ACP stdio MCP server entry pointing at `laelia-machine
// mcp-proxy`. The proxy is spawned per turn by the ACP runtime and forwards
// tools/list / tools/call to the local daemon, so the machine never holds MCP
// transport secrets. Returns nil (no MCP servers) when discovery fails or the
// catalog is empty — MCP unavailability degrades gracefully.
func (r *agentRunner) buildMcpServers(req executor.Request) []acp.McpServer {
	if req.DaemonSocket == "" || req.SessionToken == "" || req.AgentResourceID == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	catalog, err := r.daemon.McpTools(ctx, r.agentName)
	if err != nil || catalog == nil || len(catalog.Tools) == 0 {
		if err != nil {
			slog.Warn("managed mcp discovery unavailable; continuing without mcp", "agent", r.agentName, "error", err)
		}
		return nil
	}
	path := req.BinaryDir
	if existing := os.Getenv("PATH"); existing != "" {
		path = path + string(os.PathListSeparator) + existing
	}
	env := []acp.EnvVariable{
		{Name: "PATH", Value: path},
		{Name: "LAELIA_DAEMON_SOCKET", Value: req.DaemonSocket},
		{Name: "LAELIA_SESSION_TOKEN", Value: req.SessionToken},
		{Name: "LAELIA_AGENT", Value: req.AgentResourceID},
	}
	// Propagate LAELIA_HOME unconditionally when the parent has it, so the MCP
	// proxy subprocess resolves the same data root even though it is not part
	// of the fixed env list above.
	if v := os.Getenv(home.EnvDir); v != "" {
		env = append(env, acp.EnvVariable{Name: home.EnvDir, Value: v})
	}
	return []acp.McpServer{
		{
			Stdio: &acp.McpServerStdio{
				Name:    "laelia-mcp",
				Command: "laelia-machine",
				Args:    []string{"mcp-proxy"},
				Env:     env,
			},
		},
	}
}

// stop tears down the runner. cause names why the runner is going away
// ("machine shutting down" on machine exit, "agent removed from this machine"
// on RemoveAgent, "agent unassigned from this machine" on a roster reconcile)
// and becomes the in-flight turn's terminal cause (design §3.6 rule 5: the
// manager must not wait for the reaper when this machine itself cancels the
// turn).
//
// Order matters (§3.7): the in-flight turn is cancelled while the runner ctx
// is still alive so its own FAILED terminal records through the normal path;
// then the loops exit; then any turn that outlived the bounded cancel leaves
// records without a terminal, and the outbox self-check synthesizes those
// terminals durably BEFORE the WAL closes — a later replay (or the drain)
// delivers them. The outbox then gets one bounded window to drain to a
// healthy manager; if the machine is killed before it finishes, the WAL keeps
// everything and the next startup replays it.
func (r *agentRunner) stop(cause string) {
	r.coordinateInFlightTurn(cause)
	if r.cancel != nil {
		r.cancel()
	}
	if r.done != nil {
		<-r.done
	}
	r.mu.Lock()
	cs := r.cs
	r.cs = nil
	r.mu.Unlock()
	r.closeOutbox(cs, cause)
	r.stopPiSession()
	r.stopThreadSession()
	slog.Info("tore down agent runner", "agent", r.agentName)
}

// shutdownDrainTimeout bounds the runner's final upload window in stop(): long
// enough for a healthy manager to take the terminal records, short enough that
// a dead manager does not stall the shutdown (the supervisor force-kills the
// process after its grace anyway, and the WAL keeps the records either way).
const shutdownDrainTimeout = 5 * time.Second

// closeOutbox finishes the runner's outbox after the loops exited (no
// concurrent writer): it synthesizes FAILED terminals for command groups whose
// turn died without reporting (the bounded cancel window expired — §3.7), then
// gives a healthy manager one bounded window to drain the log, and only then
// closes the WAL. No-op when the outbox never opened (the turn's own failure
// path and the manager's reaper own the commands).
func (r *agentRunner) closeOutbox(cs *commandStream, cause string) {
	if cs == nil || cs.ob == nil {
		return
	}
	if cs.uploader != nil {
		if err := cs.uploader.SynthesizeInterruptedTerminals(cause); err != nil {
			slog.Warn("failed to synthesize interrupted-turn terminals",
				"agent", r.agentName, "error", err)
		} else {
			drainCtx, cancel := context.WithTimeout(context.Background(), shutdownDrainTimeout)
			cs.uploader.Drain(drainCtx)
			cancel()
		}
	}
	// SynthesizeInterruptedTerminals flushes its own appends and the drain's
	// reads flush the rest; this covers the degraded paths (no uploader, a
	// failed synthesis) so Close never drops buffered records.
	if err := cs.ob.Flush(); err != nil {
		slog.Warn("failed to flush agent outbox", "agent", r.agentName, "error", err)
	}
	if err := cs.ob.Close(); err != nil {
		slog.Warn("failed to close agent outbox", "agent", r.agentName, "error", err)
	}
}

// spawnAssignedAgents aligns the runner set with the roster the manager
// assigned at (re)connect: it opens a runner for every assigned agent
// (idempotent — a live runner is hot-reloaded in place) and stops the runners
// of agents that disappeared from the roster, since the runner lifecycle is
// assignment-driven.
func (c *MachineClient) spawnAssignedAgents(ctx context.Context, assignments []*v1pb.AgentAssignment) {
	assigned := make(map[string]bool, len(assignments))
	for _, a := range assignments {
		if a == nil || a.GetAgentName() == "" {
			continue
		}
		assigned[bareAgentID(a.GetAgentName())] = true
		c.spawnOrUpdate(ctx, a)
	}
	c.runnersMu.Lock()
	var removed []*agentRunner
	for id, r := range c.runners {
		if !assigned[id] {
			removed = append(removed, r)
			delete(c.runners, id)
		}
	}
	c.runnersMu.Unlock()
	for _, r := range removed {
		slog.Info("agent no longer assigned to this machine; stopping runner", "agent", r.agentName)
		r.stop("agent unassigned from this machine")
	}
}

// spawnOrUpdate is the single entry point for "the manager wants this agent
// hosted with this assignment": it creates a runner if none exists, otherwise
// hot-reloads the existing runner's config + display name. Either path kicks
// the runner's drain loop, so a fresh spawn (or a reconnect's roster
// reconcile) discovers pending work immediately.
func (c *MachineClient) spawnOrUpdate(ctx context.Context, a *v1pb.AgentAssignment) {
	if a == nil || a.GetAgentName() == "" {
		return
	}
	agentID := bareAgentID(a.GetAgentName())

	c.runnersMu.Lock()
	if existing, ok := c.runners[agentID]; ok {
		c.runnersMu.Unlock()
		existing.displayName = a.GetAgentDisplayName()
		existing.applyAssignment(a)
		if cs := existing.currentCommandStream(); cs != nil {
			cs.wake()
		}
		slog.Info("hot-reloaded agent assignment", "agent", a.GetAgentName())
		return
	}
	r := &agentRunner{
		machine:     c,
		daemon:      c.daemon,
		agentName:   a.GetAgentName(),
		agentID:     agentID,
		displayName: a.GetAgentDisplayName(),
	}
	r.applyAssignment(a)
	c.runners[agentID] = r
	c.runnersMu.Unlock()

	r.start(ctx)
}

// stopRunner tears down one agent's runner (on RemoveAgent / a full
// ReloadAgentAssignment). Missing is a no-op.
func (c *MachineClient) stopRunner(agentName string) {
	agentID := bareAgentID(agentName)
	c.runnersMu.Lock()
	r, ok := c.runners[agentID]
	if ok {
		delete(c.runners, agentID)
	}
	c.runnersMu.Unlock()
	if ok {
		r.stop("agent removed from this machine")
	}
}

// coldRestartAgent force-cold-restarts one agent's runner: it clears the
// agent's persisted LLM session state and restarts its long-lived runtime so
// the next turn starts from a fresh cold start. Missing runner is a no-op.
func (c *MachineClient) coldRestartAgent(agentName string) {
	agentID := bareAgentID(agentName)
	c.runnersMu.Lock()
	r, ok := c.runners[agentID]
	c.runnersMu.Unlock()
	if !ok {
		slog.Warn("cold restart for unknown agent runner; ignoring", "agent", agentName)
		return
	}
	r.coldRestart()
}

// teardownRunners stops every live runner with the machine-shutdown cause.
// Called on machine shutdown only: since phase 2 the runner outlives
// connections (a dead stream never tears down a runner), so disconnect paths
// keep the runners and the roster is reconciled from the next connect's
// assignment list. The graceful shutdown hook runs first in each stop()
// (§3.7): in-flight turns cancel with an explicit terminal and the WAL
// self-check synthesizes whatever outlived the bounded cancel.
func (c *MachineClient) teardownRunners() {
	c.runnersMu.Lock()
	runners := make([]*agentRunner, 0, len(c.runners))
	for id, r := range c.runners {
		runners = append(runners, r)
		delete(c.runners, id)
	}
	c.runnersMu.Unlock()
	for _, r := range runners {
		r.stop("machine shutting down")
	}
}

// deleteAgentWorkspace permanently removes an agent's per-machine workspace
// directory (used on DeleteAgent). Best-effort: a missing or already-removed
// directory is treated as success.
func (c *MachineClient) deleteAgentWorkspace(agentName string) {
	agentID := bareAgentID(agentName)
	dir := executor.AgentWorkingDir(c.machineID, agentID)
	if err := os.RemoveAll(dir); err != nil {
		slog.Warn("failed to remove agent workspace", "agent", agentName, "dir", dir, "error", err)
		return
	}
	slog.Info("removed agent workspace", "agent", agentName, "dir", dir)
}
