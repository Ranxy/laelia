package client

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/Ranxy/laelia/backend/agent/chattools"
	daemonsrv "github.com/Ranxy/laelia/backend/agent/daemon"
	"github.com/Ranxy/laelia/backend/agent/executor"
	"github.com/Ranxy/laelia/backend/agent/pi"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

// agentRunner owns one agent's AgentChannel drain loop. A machine hosts one
// runner per assigned agent; the runner is spawned on AgentAssignment (or on
// connect from the assigned_agents list) and torn down on RemoveAgent. The
// runner's runtime config is hot-reloadable via AgentConfigUpdate /
// ReloadAgentAssignment, picked up at the next BeginSession.
//
// An agent is backed by EXACTLY ONE runtime: either an ACP config (claude-code
// / opencode, spawned per turn) OR a pi config (builtin-pi, one long-lived
// `pi --mode rpc` subprocess shared across turns). The two never coexist on the
// same runner; applyAssignment flips between them and tears down the other side.
// All runners share the machine's access token and the machine-level daemon
// socket.
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

// buildPiConfig resolves the server-owned AgentACPConfig into a pi config +
// creates the per-agent working dir. Returns nil if the assignment is not a
// configured builtin-pi agent (provider != builtin-pi, unknown api_provider,
// or empty api key), which keeps the runner inert.
func (r *agentRunner) buildPiConfig(assignment *v1pb.AgentAssignment) *pi.PiConfig {
	piBinary, err := pi.ResolveBinary()
	if err != nil {
		slog.Warn("pi binary unavailable; agent stays inert", "agent", r.agentName, "error", err)
		return nil
	}
	cfg := pi.BuildPiConfig(
		assignment.GetAcpConfig(),
		r.machine.machineID, r.agentID, r.agentID,
		piBinary, r.daemon.SocketPath(), r.daemon.SessionToken(), r.machine.binaryDir,
	)
	if cfg == nil {
		return nil
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
// never coexist. Every teardown that SIGKILLs a pi process under a possibly
// in-flight turn first coordinates that turn (cancel + wait) so the restart
// never races the dying turn's session access and the turn reports an explicit
// reload cause instead of a generic "session exited mid-turn".
func (r *agentRunner) applyAssignment(a *v1pb.AgentAssignment) {
	acp := a.GetAcpConfig()
	if acp != nil && acp.GetProvider() == pi.BuiltinPiProvider {
		r.setConfig(nil)
		newPi := r.buildPiConfig(a)
		if newPi == nil {
			r.coordinateInFlightTurn()
			r.stopPiSession()
			return
		}
		prev := r.currentPiConfig()
		if prev == nil || prev.LaunchFingerprint() != newPi.LaunchFingerprint() {
			// Launch shape changed (or first pi config): cancel any in-flight
			// drain turn and wait for it to end, THEN restart the subprocess. The
			// cancel surfaces an explicit "config reloaded mid-turn" failure to
			// the manager (not a mid-flight "session exited mid-turn") and the
			// wait guarantees the restart never races the dying turn's session
			// access. No-op when no turn is in flight (e.g. the first config).
			r.coordinateInFlightTurn()
			r.restartPiSession(newPi)
			return
		}
		// Unchanged launch shape: keep the warm session, just refresh the config
		// (e.g. a persona_prompt change). The session's launch shape still
		// matches, so it stays valid for the new config.
		r.setPiConfig(newPi)
		return
	}
	// ACP (or unconfigured): coordinate any in-flight pi turn, then tear down
	// the pi session and load the ACP config.
	r.coordinateInFlightTurn()
	r.stopPiSession()
	r.setConfig(r.buildAcpConfig(a))
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

// start opens the agent's AgentChannel and runs its drain loop in a background
// goroutine. It returns immediately; the runner's lifetime ends when the
// goroutine exits (ctx cancelled or the stream dies). Safe to call only once
// per runner; stop cancels and waits.
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
	cs.getSessID = func() string { return "" } // no per-agent session; AgentReady carries agent_name only
	cs.getAcpConfig = r.currentConfig
	cs.newSessionRuntime = r.buildRuntimeForAgent
	cs.buildTurnBatch = func(ctx context.Context) (string, error) {
		return chattools.BuildTurnBatch(ctx, r.daemon.BatchDeps(r.agentID))
	}

	r.mu.Lock()
	r.cs = cs
	r.mu.Unlock()

	go func() {
		defer close(r.done)
		if err := cs.Start(streamCtx); err != nil {
			slog.Warn("agent runner stream exited", "agent", r.agentName, "error", err)
		}
	}()
	slog.Info("opened AgentChannel for agent", "agent", r.agentName, "displayName", r.displayName)
}

func (r *agentRunner) currentCommandStream() *commandStream {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cs
}

// inFlightTurnTimeout bounds how long coordinateInFlightTurn waits for an
// in-flight turn to end after cancelling it. A runtime that ignores Cancel is
// reaped by the subsequent restartPiSession's stopPiSession (the safe Stop
// blocks on the process reap), so the wait is best-effort and bounded.
const inFlightTurnTimeout = 5 * time.Second

// coordinateInFlightTurn cancels any in-flight drain turn and waits (bounded)
// for it to end. applyAssignment calls this before every teardown that would
// SIGKILL a pi process under a possibly in-flight turn (a launch-fingerprint
// change, a pi agent becoming unconfigured, or a pi→ACP switch) so the restart
// never races the dying turn's session access and the turn reports an explicit
// "config reloaded mid-turn" failure instead of a mid-flight "session exited
// mid-turn". No-op when no turn is in flight.
func (r *agentRunner) coordinateInFlightTurn() {
	cs := r.currentCommandStream()
	if cs == nil {
		return
	}
	done, cancelled := cs.CancelInFlight("config reloaded mid-turn")
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
	return executor.NewACP(ereq, r.currentConfig())
}

// stop cancels the runner's drain loop, tears down any pi subprocess, and
// waits for the loop to exit.
func (r *agentRunner) stop() {
	if r.cancel != nil {
		r.cancel()
	}
	if r.done != nil {
		<-r.done
	}
	r.mu.Lock()
	r.cs = nil
	r.mu.Unlock()
	r.stopPiSession()
	slog.Info("tore down agent runner", "agent", r.agentName)
}

// spawnAssignedAgents opens a runner for every agent the manager assigned at
// (re)connect. Idempotent: an agent that already has a live runner is
// re-configured in place rather than double-spawned.
func (c *MachineClient) spawnAssignedAgents(ctx context.Context, assignments []*v1pb.AgentAssignment) {
	for _, a := range assignments {
		c.spawnOrUpdate(ctx, a)
	}
}

// spawnOrUpdate is the single entry point for "the manager wants this agent
// hosted with this assignment": it creates a runner if none exists, otherwise
// hot-reloads the existing runner's config + display name.
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

// stopRunner tears down one agent's runner (on RemoveAgent). Missing is a no-op.
func (c *MachineClient) stopRunner(agentName string) {
	agentID := bareAgentID(agentName)
	c.runnersMu.Lock()
	r, ok := c.runners[agentID]
	if ok {
		delete(c.runners, agentID)
	}
	c.runnersMu.Unlock()
	if ok {
		r.stop()
	}
}

// teardownRunners stops every live runner. Called on disconnect / reconnect so
// the next connect re-spawns the full roster from assigned_agents.
func (c *MachineClient) teardownRunners() {
	c.runnersMu.Lock()
	runners := make([]*agentRunner, 0, len(c.runners))
	for id, r := range c.runners {
		runners = append(runners, r)
		delete(c.runners, id)
	}
	c.runnersMu.Unlock()
	for _, r := range runners {
		r.stop()
	}
}
