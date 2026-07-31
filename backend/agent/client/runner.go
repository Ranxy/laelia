package client

import (
	"context"
	"log/slog"
	"os"
	"sync"

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
	cancel    context.CancelFunc
	done      chan struct{}
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
// never coexist.
func (r *agentRunner) applyAssignment(a *v1pb.AgentAssignment) {
	acp := a.GetAcpConfig()
	if acp != nil && acp.GetProvider() == pi.BuiltinPiProvider {
		r.setConfig(nil)
		newPi := r.buildPiConfig(a)
		if newPi == nil {
			r.stopPiSession()
			r.setPiConfig(nil)
			return
		}
		prev := r.currentPiConfig()
		if prev == nil || prev.LaunchFingerprint() != newPi.LaunchFingerprint() {
			// Launch shape changed (or first pi config): restart the subprocess.
			// An unchanged fingerprint keeps the warm session and its conversation.
			r.restartPiSession(newPi)
		}
		r.setPiConfig(newPi)
		return
	}
	// ACP (or unconfigured): tear down any pi session and load the ACP config.
	r.stopPiSession()
	r.setPiConfig(nil)
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

// restartPiSession stops any existing pi session and starts a fresh one bound
// to cfg. The session is started lazily on the first turn (so the opening
// turn's command id seeds LAELIA_COMMAND), so this only constructs it.
func (r *agentRunner) restartPiSession(cfg *pi.PiConfig) {
	r.stopPiSession()
	r.mu.Lock()
	r.piSession = pi.NewSession(cfg)
	r.mu.Unlock()
}

// stopPiSession tears down the pi subprocess if one is running.
func (r *agentRunner) stopPiSession() {
	r.mu.Lock()
	sess := r.piSession
	r.piSession = nil
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

	go func() {
		defer close(r.done)
		if err := cs.Start(streamCtx); err != nil {
			slog.Warn("agent runner stream exited", "agent", r.agentName, "error", err)
		}
	}()
	slog.Info("opened AgentChannel for agent", "agent", r.agentName, "displayName", r.displayName)
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
	if piCfg := r.currentPiConfig(); piCfg != nil {
		r.mu.Lock()
		sess := r.piSession
		r.mu.Unlock()
		if sess == nil {
			sess = pi.NewSession(piCfg)
			r.mu.Lock()
			r.piSession = sess
			r.mu.Unlock()
		}
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
