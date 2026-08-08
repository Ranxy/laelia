package executor

import (
	"context"
	"errors"
	"log/slog"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/Ranxy/laelia/backend/agent/acp2"
	"github.com/Ranxy/laelia/backend/agent/provider"
)

// idleEvictGrace is how long idle eviction waits after SIGTERM for the
// app-server to exit on its own before escalating to SIGKILL. Bounded so an
// unresponsive process does not stall eviction.
const idleEvictGrace = 3 * time.Second

// ThreadSession owns one long-lived ACP v2 app-server subprocess shared across
// turns (the resident counterpart of the per-turn ThreadExecutor). Turns are
// serialized: the per-turn executor calls EnsureStarted (lazy spawn + handshake
// + thread/start|resume), BeginTurn (turn/start), drains mapped events, then
// EndTurn, which re-arms the idle-eviction timer. Between turns the subprocess
// stays up and the thread stays open, so a warm turn starts in seconds instead
// of a cold app-server boot.
//
// The session is default-off: the runner creates it only when resident mode is
// enabled for the agent. It is bound to its own session ctx (NOT any turn
// ctx), so a turn ending, timing out, or being cancelled never SIGKILLs the
// persistent process; only Stop (runner teardown), an explicit Kill (failed /
// cancelled turn), or idle eviction does.
type ThreadSession struct {
	ctx    context.Context
	cancel context.CancelFunc
	req    Request
	cfg    *ThreadConfig
	p      provider.ThreadProvider

	startMu        sync.Mutex
	started        bool
	evicting       bool
	startedAt      time.Time
	cmd            *exec.Cmd
	client         *acp2.Client
	threadID       string
	resumed        bool
	resumeFailures int
	fingerprint    string
	waitDone       chan struct{}
	// primed records whether at least one turn has run on this process. A
	// process that started a thread (cold or resumed) has the init prompt in
	// the thread history, so every later turn on the same process is warm.
	// Reset by waitPump on process exit, mirroring pi's session priming.
	primed atomic.Bool

	launchFingerprint string

	// idleTimer, when non-nil, is armed by EndTurn to fire evict after
	// idleTimeout of turn inactivity, freeing the subprocess while the session
	// stays resumable (the persisted thread id is preserved). BeginTurn /
	// EnsureStarted stop it. Protected by startMu.
	idleTimer   *time.Timer
	idleTimeout time.Duration
}

// NewThreadSession constructs a (not-yet-started) resident thread session.
// The ctx MUST be independent of any single turn's ctx: the subprocess is
// spawned with exec.CommandContext(s.ctx, ...), so a turn ctx ending must NOT
// cancel s.ctx (that would SIGKILL the persistent process at every turn end).
// The runner derives ctx from context.Background() and cancels it only on
// explicit teardown (runner stop, launch-shape change, agent removal).
func NewThreadSession(ctx context.Context, cancel context.CancelFunc, req Request, cfg *ThreadConfig, p provider.ThreadProvider) *ThreadSession {
	s := &ThreadSession{ctx: ctx, cancel: cancel, req: req, cfg: cfg, p: p}
	if cfg != nil {
		s.idleTimeout = cfg.IdleTimeout
	}
	if cfg != nil && p != nil {
		s.launchFingerprint = threadFingerprint(cfg, p)
	}
	return s
}

// LaunchFingerprint is the session's launch identity: provider/model/working
// dir/protocol plus the launch command. The runner compares it against the
// current config to decide whether the resident process must be restarted.
func (s *ThreadSession) LaunchFingerprint() string { return s.launchFingerprint }

// ThreadID is the established thread id (empty until Start succeeds).
func (s *ThreadSession) ThreadID() string { return s.threadID }

// Resumed reports whether the current process resumed a persisted thread.
func (s *ThreadSession) Resumed() bool { return s.resumed }

// Warm reports whether the next turn on this process is warm: the thread
// already carries the init prompt, so only the message batch is sent. True
// when the process resumed a persisted thread or a previous turn already ran
// on it.
func (s *ThreadSession) Warm() bool { return s.resumed || s.primed.Load() }

// ResumeFailures is the accumulated consecutive resume-failure count.
func (s *ThreadSession) ResumeFailures() int { return s.resumeFailures }

// ThreadFingerprint is the acp-session fingerprint the established thread
// belongs to (see sessionFingerprint).
func (s *ThreadSession) ThreadFingerprint() string { return s.fingerprint }

// Client returns the live protocol client, or nil before Start succeeds.
func (s *ThreadSession) Client() *acp2.Client {
	s.startMu.Lock()
	defer s.startMu.Unlock()
	return s.client
}

// EnsureStarted claims the process for an incoming turn: it waits for any
// in-progress idle eviction to finish (so a turn arriving while evict is
// SIGTERMing the process does not see started=true, skip the respawn, and
// prompt against a dying process), stops any pending idle-eviction timer (a
// turn is in flight, so the process must stay resident), then starts the
// process if it is not alive. Start is idempotent, so the alive fast path is
// unchanged when no eviction is in flight.
func (s *ThreadSession) EnsureStarted(emit func(Event)) error {
	s.startMu.Lock()
	for s.evicting {
		// Eviction is tearing the process down; wait for the reap (waitDone
		// closes) before re-checking, then fall through to Start which respawns.
		waitDone := s.waitDone
		s.startMu.Unlock()
		if waitDone != nil {
			<-waitDone
		}
		s.startMu.Lock()
	}
	if s.idleTimer != nil {
		s.idleTimer.Stop()
		s.idleTimer = nil
	}
	alive := s.started && !s.startedAt.IsZero()
	s.startMu.Unlock()
	if alive {
		return nil
	}
	return s.Start(emit)
}

// Start spawns the app-server subprocess and establishes the thread. The
// subprocess is bound to s.ctx (the session ctx), NOT the caller's turn ctx,
// so the process survives across turns. The startup handshake runs OUTSIDE
// startMu so a wedged startup does not hold the lock across the whole
// StartupTimeout window (Stop/Kill must be able to act on the process
// meanwhile). A startup that times out (spawned but never answered
// Initialize/thread-start) is fatal: the wedged process is killed so the next
// turn respawns, and the error is returned so this turn fails at
// ~StartupTimeout instead of hanging to the turn timeout. A thread/resume
// failure is non-fatal: the turn degrades to a cold thread/start.
func (s *ThreadSession) Start(emit func(Event)) error {
	s.startMu.Lock()
	if s.started {
		s.startMu.Unlock()
		return nil
	}
	if s.cfg == nil || s.p == nil {
		s.startMu.Unlock()
		return errors.New("thread session is not configured")
	}
	cmd, client, stderr, err := spawnThreadAppServer(s.ctx, s.req, s.cfg, s.p)
	if err != nil {
		s.startMu.Unlock()
		return err
	}
	s.cmd = cmd
	s.client = client
	s.started = true
	s.startedAt = time.Now()
	s.waitDone = make(chan struct{})
	s.startMu.Unlock()

	go s.waitPump()
	go drainThreadStderr(stderr, s.req.AgentID)

	startupTimeout := s.cfg.StartupTimeout
	if startupTimeout <= 0 {
		startupTimeout = defaultACPStartupTimeout
	}
	startupCtx, cancelStartup := context.WithTimeout(s.ctx, startupTimeout)
	defer cancelStartup()

	threadID, resumed, resumeFailures, fingerprint, err := establishThread(startupCtx, s.req, s.cfg, client, emit)
	if err != nil {
		// Spawned but the handshake/thread establish failed (typically a
		// startup timeout): kill the process so the next turn respawns, and
		// fail this turn fast. The session ctx is left intact so the next
		// EnsureStarted re-spawns on it.
		slog.Warn("thread session start failed; killing subprocess", "agent", s.req.AgentID, "error", err)
		s.killProcess()
		return err
	}
	s.threadID = threadID
	s.resumed = resumed
	s.resumeFailures = resumeFailures
	s.fingerprint = fingerprint
	slog.Info("thread session started", "agent", s.req.AgentID, "threadID", threadID, "resumed", resumed)
	return nil
}

// BeginTurn starts a turn on the resident thread and returns the accepted turn
// id (the per-turn executor notes it on its turn gate). Leftover mapped events
// buffered past the previous turn/completed frame are discarded first so the
// new turn drains cleanly.
func (s *ThreadSession) BeginTurn(ctx context.Context, prompt string) (string, error) {
	s.startMu.Lock()
	client := s.client
	threadID := s.threadID
	s.startMu.Unlock()
	if client == nil || threadID == "" {
		return "", errors.New("thread session is not started")
	}
	for {
		select {
		case <-client.Events():
		default:
			goto drained
		}
	}
drained:
	turn, err := client.StartTurn(ctx, threadID, prompt)
	if err != nil {
		return "", err
	}
	s.primed.Store(true)
	return turn.ResolvedID(), nil
}

// EndTurn marks the turn finished and (re)arms the idle-eviction timer: once
// the turn is over and the process is still alive, the subprocess becomes
// eligible for eviction after idleTimeout of further inactivity.
func (s *ThreadSession) EndTurn() {
	s.armIdleTimer()
}

// Alive reports whether the subprocess is currently up.
func (s *ThreadSession) Alive() bool {
	s.startMu.Lock()
	defer s.startMu.Unlock()
	return s.started && !s.startedAt.IsZero()
}

// Kill tears down the subprocess but keeps the session ctx alive so the next
// turn respawns (and resumes the persisted thread). Used to un-wedge the
// server after a cancelled or failed turn left an in-flight turn behind.
func (s *ThreadSession) Kill() {
	s.startMu.Lock()
	started := s.started
	waitDone := s.waitDone
	cmd := s.cmd
	if s.idleTimer != nil {
		s.idleTimer.Stop()
		s.idleTimer = nil
	}
	s.startMu.Unlock()
	if !started || cmd == nil || cmd.Process == nil {
		return
	}
	slog.Info("thread session: killing subprocess", "agent", s.req.AgentID)
	_ = KillGroup(cmd, syscall.SIGKILL)
	if waitDone != nil {
		<-waitDone
	}
}

// killProcess kills the subprocess and its process group and blocks until
// waitPump has reaped it and reset started, WITHOUT cancelling the session
// ctx. The session can therefore be re-spawned on the next turn. Used for a
// failed startup and for eviction.
func (s *ThreadSession) killProcess() {
	s.startMu.Lock()
	started := s.started
	waitDone := s.waitDone
	cmd := s.cmd
	if s.idleTimer != nil {
		s.idleTimer.Stop()
		s.idleTimer = nil
	}
	s.startMu.Unlock()
	if !started || cmd == nil || cmd.Process == nil {
		return
	}
	_ = KillGroup(cmd, syscall.SIGKILL)
	if waitDone != nil {
		<-waitDone
	}
}

// Stop tears down the subprocess and cancels the session ctx. Idempotent;
// the session cannot be restarted afterwards.
func (s *ThreadSession) Stop() {
	s.startMu.Lock()
	started := s.started
	waitDone := s.waitDone
	if s.idleTimer != nil {
		s.idleTimer.Stop()
		s.idleTimer = nil
	}
	s.startMu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
	if started && s.cmd != nil && s.cmd.Process != nil {
		_ = KillGroup(s.cmd, syscall.SIGKILL)
	}
	if started && waitDone != nil {
		<-waitDone
	}
}

// waitPump reaps the subprocess and resets the session to the not-started
// state so the next EnsureStarted respawns it (and resumes the persisted
// thread). waitDone is closed once the reap completes.
func (s *ThreadSession) waitPump() {
	waitErr := s.cmd.Wait()
	s.startMu.Lock()
	started := s.started
	s.started = false
	s.cmd = nil
	waitDone := s.waitDone
	s.waitDone = nil
	s.startMu.Unlock()
	s.primed.Store(false)
	if started && waitDone != nil {
		close(waitDone)
	}
	if waitErr != nil && s.ctx.Err() == nil {
		slog.Debug("thread session subprocess exited", "agent", s.req.AgentID, "error", waitErr)
	}
}

// armIdleTimer (re)arms the idle-eviction timer when eviction is enabled and
// the process is alive. Called by EndTurn. A consecutive turn's EnsureStarted
// stops it first, so rapid turns never evict (no thrash).
func (s *ThreadSession) armIdleTimer() {
	if s.idleTimeout <= 0 {
		return
	}
	s.startMu.Lock()
	defer s.startMu.Unlock()
	if !s.started {
		return
	}
	if s.idleTimer != nil {
		s.idleTimer.Stop()
	}
	s.idleTimer = time.AfterFunc(s.idleTimeout, s.evict)
}

// evict is the idle-eviction callback armed by EndTurn. After idleTimeout of
// turn inactivity, the subprocess is freed while the session stays resumable
// (the persisted thread id is preserved; the next turn respawns and resumes).
func (s *ThreadSession) evict() {
	s.startMu.Lock()
	if !s.started || s.idleTimer == nil {
		// Process already gone, or EnsureStarted already stopped the timer (a
		// turn is in flight) — abort the eviction.
		s.startMu.Unlock()
		return
	}
	s.idleTimer = nil
	s.evicting = true
	cmd := s.cmd
	waitDone := s.waitDone
	s.startMu.Unlock()
	if cmd == nil || cmd.Process == nil {
		s.clearEvicting()
		return
	}
	slog.Info("thread session: idle-evicting subprocess", "agent", s.req.AgentID, "idle_timeout", s.idleTimeout)
	// Graceful: SIGTERM the group so the app-server can flush, then SIGKILL
	// the group after a short grace (or once it exits) so a descendant that
	// ignored SIGTERM cannot outlive eviction orphaned to init — the same
	// group-reap invariant Stop relies on.
	_ = KillGroup(cmd, syscall.SIGTERM)
	select {
	case <-waitDone:
		_ = KillGroup(cmd, syscall.SIGKILL)
	case <-time.After(idleEvictGrace):
		_ = KillGroup(cmd, syscall.SIGKILL)
		<-waitDone
	}
	s.clearEvicting()
}

// clearEvicting clears the in-progress eviction flag after the reap.
func (s *ThreadSession) clearEvicting() {
	s.startMu.Lock()
	s.evicting = false
	s.startMu.Unlock()
}
