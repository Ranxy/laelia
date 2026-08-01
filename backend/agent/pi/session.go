package pi

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	pkgerrors "github.com/pkg/errors"

	"github.com/Ranxy/laelia/backend/agent/atomicfile"
	"github.com/Ranxy/laelia/backend/agent/executor"
)

// turnEventBuffer bounds the per-turn event channel. The drain loop consumes
// continuously during a turn, so this only needs to absorb bursts (e.g. many
// tool_execution_update chunks). Overflow drops non-terminal events to never
// block the pi stdout pipe (which would stall the subprocess); the terminal
// agent_settled is sent with a blocking send (see sendEvent) so it is never
// dropped — a dropped settled would hang the turn to its timeout.
const turnEventBuffer = 256

// defaultStartupTimeout (declared in config.go) bounds the single
// get_state/switch_session round trip at session start; it is the fallback when
// PiConfig.StartupTimeout is unset. See Session.Start for the wedged-startup
// kill path.

// Session owns one long-lived `pi --mode rpc` subprocess for a pi agent. Turns
// are serialized by the drain loop (one BeginSession at a time), so the session
// serves them one at a time over the same process: each turn calls beginTurn to
// get a fresh event channel, sends a prompt, drains events until agent_settled,
// then endTurn. Between turns the active channel is nil and streamed events are
// dropped (there should be none while idle).
//
// The session survives across turns (warm) and across machine restarts: the pi
// session file is persisted to pi-session.json and reloaded via switch_session
// on the next Start so the LLM conversation + init prompt are inherited.
type Session struct {
	cfg *PiConfig

	ctx    context.Context
	cancel context.CancelFunc
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader

	startMu sync.Mutex
	started bool
	// waitDone is closed by waitPump once the subprocess is reaped, so Stop can
	// block until the process is truly gone (and started is reset) before
	// returning. Created fresh on each successful Start.
	waitDone chan struct{}

	writeMu sync.Mutex

	respMu sync.Mutex
	resp   map[string]chan response

	turnMu sync.Mutex
	active chan *event // nil between turns; events dropped while nil

	// sessionFile is the pi session .jsonl path, captured from get_state and
	// persisted for resume across machine restarts.
	sessionFile atomic.Value // string

	// resumedFromDisk records whether Start switched to a persisted session
	// (warm history inherited across a machine restart). primed records
	// whether a cold init prompt has already been sent on this process. A turn
	// is warm (no init prompt) when either is true.
	resumedFromDisk bool
	primed          atomic.Bool

	startedAt time.Time
}

// NewSession constructs a (not-yet-started) Session bound to a runner-level
// ctx. The ctx MUST be independent of any single turn's ctx: the subprocess is
// spawned with exec.CommandContext(s.ctx, ...), so cancelling a turn ctx must
// NOT cancel s.ctx (that would SIGKILL the persistent process at every turn
// end). The runner derives ctx from context.Background() and cancels it only on
// explicit teardown (runner stop, launch-fingerprint change, RemoveAgent). The
// runner starts the session lazily on the first turn so the opening turn's
// command id can seed LAELIA_COMMAND.
func NewSession(ctx context.Context, cancel context.CancelFunc, cfg *PiConfig) *Session {
	return &Session{cfg: cfg, ctx: ctx, cancel: cancel, resp: map[string]chan response{}}
}

// Start spawns the pi subprocess and primes session resume. commandID seeds
// LAELIA_COMMAND for the persistent process (see PiConfig.buildPiEnv). The
// subprocess is bound to s.ctx (the session ctx), NOT the caller's turn ctx, so
// the process survives across turns.
//
// The startup RPC (resumeOrCapture) runs OUTSIDE startMu so a wedged startup
// does not hold the lock across the whole StartupTimeout window (Stop must be
// able to act on the process meanwhile). A startup that times out (pi spawned
// but never answered get_state/switch_session) is fatal: the wedged process is
// killed so the next turn respawns, and the error is returned so this turn
// fails at ~StartupTimeout instead of hanging to the turn timeout. A fast
// startup failure (e.g. a corrupt saved-session file) is non-fatal: the turn
// degrades to a cold init prompt.
func (s *Session) Start(commandID string) error {
	s.startMu.Lock()
	if s.started {
		s.startMu.Unlock()
		return nil
	}

	if s.cfg == nil || s.cfg.PiBinaryPath == "" {
		s.startMu.Unlock()
		return errors.New("pi: binary path not configured")
	}
	if err := os.MkdirAll(s.cfg.WorkingDir, 0o700); err != nil {
		s.startMu.Unlock()
		return pkgerrors.Wrap(err, "pi: create working dir")
	}

	cmd := exec.CommandContext(s.ctx, s.cfg.PiBinaryPath, s.cfg.launchArgs()...)
	cmd.Dir = s.cfg.WorkingDir
	cmd.Env = s.cfg.buildPiEnv(commandID)
	// Own process group so Stop/KillGroup reaps the whole tree (pi may spawn
	// bash/tool subprocesses under --approve); on Linux also kill on parent death.
	executor.SetProcessGroup(cmd)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		s.startMu.Unlock()
		return pkgerrors.Wrap(err, "pi: stdin pipe")
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		s.startMu.Unlock()
		return pkgerrors.Wrap(err, "pi: stdout pipe")
	}
	// Stderr is logged for diagnostics; it is not part of the JSONL protocol.
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		s.startMu.Unlock()
		return pkgerrors.Wrap(err, "pi: start subprocess")
	}
	s.cmd = cmd
	s.stdin = stdin
	s.stdout = bufio.NewReader(stdoutPipe)
	s.started = true
	s.startedAt = time.Now()
	s.waitDone = make(chan struct{})

	go s.readPump()
	go s.waitPump()
	s.startMu.Unlock()

	// Resume the prior session if one was persisted and the config fingerprint
	// still matches; otherwise pi has already created a fresh session. Either
	// way, capture the session file for the next resume. This is the startup
	// RPC round trip, bounded by StartupTimeout and run outside startMu.
	if err := s.resumeOrCapture(); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			// pi spawned but never answered the startup RPC: it is wedged. Kill
			// it so the next turn respawns, and fail this turn fast at
			// ~StartupTimeout rather than hanging to MaxTimeoutSeconds. The
			// session ctx is left intact so the next Start re-spawns on it.
			slog.Warn("pi: startup timed out; killing wedged process",
				"agent", s.cfg.AgentID, "startup_timeout", s.startupTimeout(), "error", err)
			s.killProcess()
			return pkgerrors.Wrap(err, "pi: startup timed out")
		}
		// Non-fatal: a fast failure (corrupt saved session) falls back to a
		// fresh session. The first turn re-sends the init prompt (cold), the
		// correct degraded mode. Log and continue rather than killing the process.
		slog.Warn("pi: session resume/capture failed; starting cold", "agent", s.cfg.AgentID, "error", err)
	}
	return nil
}

// startupTimeout returns the configured startup RPC timeout, falling back to
// the default when unset (e.g. a test-built PiConfig that omits it).
func (s *Session) startupTimeout() time.Duration {
	if s.cfg != nil && s.cfg.StartupTimeout > 0 {
		return s.cfg.StartupTimeout
	}
	return defaultStartupTimeout
}

// killProcess kills the subprocess and its process group and blocks until
// waitPump has reaped it and reset started, WITHOUT cancelling the session ctx.
// The session can therefore be re-spawned on the next turn (exec.CommandContext
// over a still-alive s.ctx). Used for a wedged startup: the process is running
// but not responding, so the next turn should respawn rather than reuse it.
func (s *Session) killProcess() {
	s.startMu.Lock()
	started := s.started
	waitDone := s.waitDone
	cmd := s.cmd
	s.startMu.Unlock()
	if !started || cmd == nil || cmd.Process == nil {
		return
	}
	_ = executor.KillGroup(cmd, syscall.SIGKILL)
	if waitDone != nil {
		<-waitDone
	}
}

// readPump decodes LF-delimited JSONL from stdout for the process lifetime. It
// routes responses to waiting Send callers and events to the active turn channel.
func (s *Session) readPump() {
	for {
		line, err := s.stdout.ReadString('\n')
		if err != nil {
			if err != io.EOF && !errors.Is(err, os.ErrClosed) {
				slog.Debug("pi stdout read ended", "error", err)
			}
			return
		}
		// RPC framing: split on LF only, strip optional trailing CR.
		if len(line) > 0 && line[len(line)-1] == '\n' {
			line = line[:len(line)-1]
		}
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}
		if line == "" {
			continue
		}
		s.dispatch(line)
	}
}

// waitPump reaps the subprocess and signals completion so the runner can mark
// the session dead (the next turn restarts it). It closes the active turn
// channel so a drain loop blocked waiting for the next event unblocks
// immediately — its !ok branch surfaces a fast "session exited mid-turn"
// failure instead of hanging to the turn timeout.
func (s *Session) waitPump() {
	_ = s.cmd.Wait()
	// Close the active turn channel first so a mid-turn drain loop fails fast.
	// sendEvent holds turnMu for its (non-blocking) send, so this close cannot
	// race a send that would panic on a closed channel: once we set active=nil
	// under the lock, any concurrent sendEvent observes nil and returns, and
	// any in-flight send has already completed before we unlock.
	s.turnMu.Lock()
	ch := s.active
	s.active = nil
	s.turnMu.Unlock()
	if ch != nil {
		close(ch)
	}
	// Unblock any Send in flight.
	s.respMu.Lock()
	for id, respCh := range s.resp {
		close(respCh)
		delete(s.resp, id)
	}
	s.respMu.Unlock()
	// Reset start state so the next Start can re-spawn. resumedFromDisk is
	// re-derived by resumeOrCapture on the new process; primed is irrelevant
	// once the process is gone (a restart resumes from disk, which is warm).
	s.startMu.Lock()
	s.started = false
	s.startedAt = time.Time{}
	s.resumedFromDisk = false
	s.startMu.Unlock()
	// Signal Stop (and any future restart) that the subprocess is reaped and
	// started is reset, so they can block until it is safe to spawn again.
	close(s.waitDone)
}

// dispatch decodes one JSONL line and routes it.
func (s *Session) dispatch(line string) {
	// Peek the type without a full unmarshal to branch cheaply.
	var head struct {
		Type string `json:"type"`
		ID   string `json:"id,omitempty"`
	}
	if err := json.Unmarshal([]byte(line), &head); err != nil {
		slog.Debug("pi: undecodable line", "line", line, "error", err)
		return
	}
	if head.Type == "response" {
		var r response
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			slog.Debug("pi: undecodable response", "line", line, "error", err)
			return
		}
		s.routeResponse(r)
		return
	}
	var ev event
	if err := json.Unmarshal([]byte(line), &ev); err != nil {
		slog.Debug("pi: undecodable event", "line", line, "error", err)
		return
	}
	s.sendEvent(&ev)
}

func (s *Session) routeResponse(r response) {
	s.respMu.Lock()
	ch, ok := s.resp[r.ID]
	if ok {
		delete(s.resp, r.ID)
	}
	s.respMu.Unlock()
	if ok {
		select {
		case ch <- r:
		case <-s.ctx.Done():
		}
	}
}

// sendEvent fans an event to the active turn channel. Non-terminal events drop
// on a full buffer (default) — dropping protects the subprocess from a stalled
// stdout pipe, and the drain loop keeps up during a turn so drops are unexpected.
// The terminal event (agent_settled) is the one event the drain loop strictly
// waits for: a dropped settled would leave the turn hung to its timeout. It is
// therefore sent with a BLOCKING send — no default drop — so it is delivered once
// the drain loop catches up, bounded by s.ctx.Done (the session torn down by
// Stop, which also lets waitPump close the channel). The drain loop consumes the
// channel without turnMu, so a blocked terminal send unblocks as soon as the
// loop drains the buffer; s.ctx.Done bounds the block if the consumer is wedged.
//
// The lock is held across the send so waitPump's close of the channel cannot race
// it: once waitPump sets active=nil under turnMu, a concurrent sendEvent observes
// nil and returns, and any in-flight send has completed before waitPump unlocks
// and closes. Stop cancels s.ctx before waitPump acquires turnMu, so a blocked
// terminal send unblocks (via s.ctx.Done) and releases the lock before waitPump
// needs it — no deadlock between a wedged consumer and process teardown.
func (s *Session) sendEvent(ev *event) {
	s.turnMu.Lock()
	defer s.turnMu.Unlock()
	ch := s.active
	if ch == nil {
		return
	}
	if ev.Type == eventAgentSettled {
		select {
		case ch <- ev:
		case <-s.ctx.Done():
		}
		return
	}
	select {
	case ch <- ev:
	case <-s.ctx.Done():
	default:
		slog.Debug("pi: event dropped (turn channel full)", "type", ev.Type)
	}
}

// writeLine writes one JSONL command to stdin. It is the single writer.
func (s *Session) writeLine(cmd any) error {
	data, err := json.Marshal(cmd)
	if err != nil {
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if _, err := s.stdin.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}

// send sends a command with an id and waits for its response (with a timeout).
func (s *Session) send(ctx context.Context, cmd any) (response, error) {
	id := nextRequestID()
	ch := make(chan response, 1)
	s.respMu.Lock()
	s.resp[id] = ch
	s.respMu.Unlock()

	// Inject the id into the command via a small wrapper.
	payload, err := json.Marshal(cmd)
	if err != nil {
		s.respMu.Lock()
		delete(s.resp, id)
		s.respMu.Unlock()
		return response{}, err
	}
	// Re-marshal with id. All our command structs have an `ID` json field, but
	// marshalling an `any` loses the literal; decode into a map to set id.
	var obj map[string]any
	if err := json.Unmarshal(payload, &obj); err != nil {
		return response{}, err
	}
	obj["id"] = id
	if err := s.writeLine(obj); err != nil {
		s.respMu.Lock()
		delete(s.resp, id)
		s.respMu.Unlock()
		return response{}, err
	}

	select {
	case r, ok := <-ch:
		if !ok {
			return response{}, errors.New("pi: session exited before response")
		}
		return r, nil
	case <-ctx.Done():
		s.respMu.Lock()
		delete(s.resp, id)
		s.respMu.Unlock()
		return response{}, ctx.Err()
	}
}

// beginTurn opens a fresh event channel for this turn and registers it as the
// active destination. The caller drains it until agent_settled, then endTurn.
func (s *Session) beginTurn() chan *event {
	ch := make(chan *event, turnEventBuffer)
	s.turnMu.Lock()
	s.active = ch
	s.turnMu.Unlock()
	return ch
}

// endTurn clears the active channel. Any events still buffered are discarded by
// the caller dropping the reference; the next beginTurn starts clean.
func (s *Session) endTurn() {
	s.turnMu.Lock()
	s.active = nil
	s.turnMu.Unlock()
}

// prompt sends a prompt and waits for its acceptance response. Events stream to
// the active turn channel after acceptance.
func (s *Session) prompt(ctx context.Context, message string) error {
	r, err := s.send(ctx, promptCommand{Type: "prompt", Message: message})
	if err != nil {
		return err
	}
	if !r.Success {
		return pkgerrors.Errorf("pi: prompt rejected: %s", r.Error)
	}
	return nil
}

// sessionStats fetches the session's token usage and current context-window
// estimate via get_session_stats. Unlike ACP's pushed UsageUpdate, pi exposes
// usage only as a pull command, so callers poll it.
func (s *Session) sessionStats(ctx context.Context) (*sessionStatsData, error) {
	r, err := s.send(ctx, getSessionStatsCommand{Type: "get_session_stats"})
	if err != nil {
		return nil, err
	}
	if !r.Success {
		return nil, pkgerrors.Errorf("pi: get_session_stats failed: %s", r.Error)
	}
	var data sessionStatsData
	if err := json.Unmarshal(r.Data, &data); err != nil {
		return nil, pkgerrors.Wrap(err, "pi: decode get_session_stats response")
	}
	return &data, nil
}

// abort cancels the current agent operation. Fire-and-forget (no id, no wait).
func (s *Session) abort() {
	_ = s.writeLine(abortCommand{Type: "abort"})
}

// Alive reports whether the subprocess is still running.
func (s *Session) Alive() bool {
	s.startMu.Lock()
	defer s.startMu.Unlock()
	return s.started && !s.startedAt.Equal(time.Time{})
}

// IsWarm reports whether the next turn should skip the init prompt: true when
// the session resumed a persisted conversation from disk, or a prior turn on
// this process already sent the cold init prompt (primed).
func (s *Session) IsWarm() bool {
	s.startMu.Lock()
	resumed := s.resumedFromDisk
	s.startMu.Unlock()
	return resumed || s.primed.Load()
}

// MarkPrimed records that a cold init prompt has been sent on this process, so
// subsequent turns are warm (init prompt already in the session history).
func (s *Session) MarkPrimed() { s.primed.Store(true) }

// SessionFile returns the pi session .jsonl path captured at start, for
// attribution in the turn result and FinalSummary.
func (s *Session) SessionFile() string {
	v, ok := s.sessionFile.Load().(string)
	if !ok {
		return ""
	}
	return v
}

// resumeOrCapture loads the persisted session file, switches to it if the
// fingerprint matches, and captures the current session file for next time.
func (s *Session) resumeOrCapture() error {
	ctx, cancel := context.WithTimeout(s.ctx, s.startupTimeout())
	defer cancel()

	saved, err := loadPiSession(s.cfg.MachineID, s.cfg.AgentID)
	if err != nil {
		return err
	}
	if saved != nil && saved.Fingerprint == piFingerprint(s.cfg) && saved.SessionPath != "" {
		if _, err := s.send(ctx, switchSessionCommand{Type: "switch_session", SessionPath: saved.SessionPath}); err != nil {
			return err
		}
		s.resumedFromDisk = true
	}
	// Capture the current session file (the one pi created or the one we switched to).
	r, err := s.send(ctx, getStateCommand{Type: "get_state"})
	if err != nil {
		return err
	}
	if !r.Success {
		return pkgerrors.Errorf("get_state failed: %s", r.Error)
	}
	var data getStateData
	if err := json.Unmarshal(r.Data, &data); err != nil {
		return err
	}
	s.sessionFile.Store(data.SessionFile)
	if err := savePiSession(s.cfg.MachineID, s.cfg.AgentID, &piSessionState{
		SessionPath: data.SessionFile,
		Fingerprint: piFingerprint(s.cfg),
	}); err != nil {
		slog.Warn("pi: failed to persist session state", "error", err)
	}
	return nil
}

// Stop tears down the subprocess: cancel the session ctx (which
// exec.CommandContext translates to a SIGKILL of the direct child), kill the
// whole process group (so pi's tool subprocesses do not survive as orphans),
// and block until waitPump has reaped it. s.ctx is the session ctx (independent
// of any turn ctx), so this is the ONLY path that cancels it. Only waitPump
// resets started (after cmd.Wait returns), so a subsequent Start never races a
// dying process.
func (s *Session) Stop() {
	s.startMu.Lock()
	started := s.started
	waitDone := s.waitDone
	s.startMu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
	if started && s.cmd != nil && s.cmd.Process != nil {
		_ = executor.KillGroup(s.cmd, syscall.SIGKILL)
	}
	if started && waitDone != nil {
		<-waitDone
	}
}

// --- pi-session.json resume state ---

type piSessionState struct {
	SessionPath string `json:"session_path"`
	Fingerprint string `json:"fingerprint"`
}

func piFingerprint(c *PiConfig) string {
	h := sha256.New()
	_, _ = h.Write([]byte(c.APIProvider + "\x00" + c.Model + "\x00" + c.WorkingDir))
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func piSessionPath(machineID, agentID string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".laelia", machineID, agentID, "pi-session.json")
}

func loadPiSession(machineID, agentID string) (*piSessionState, error) {
	data, err := os.ReadFile(piSessionPath(machineID, agentID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var s piSessionState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func savePiSession(machineID, agentID string, s *piSessionState) error {
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return atomicfile.WriteFileAtomic(piSessionPath(machineID, agentID), data, 0o600)
}

var requestIDCounter atomic.Int64

func nextRequestID() string {
	return fmt.Sprintf("laelia-%d", requestIDCounter.Add(1))
}
