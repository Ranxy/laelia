package executor

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"time"

	pkgerrors "github.com/pkg/errors"

	"github.com/Ranxy/laelia/backend/agent/acp2"
	"github.com/Ranxy/laelia/backend/agent/provider"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

// spawnThreadAppServer launches the provider's app-server subprocess and
// returns the running command, a started protocol client over its stdio, and
// its stderr pipe. The command is bound to procCtx: a per-turn executor passes
// its turn ctx (the process dies with the turn), while a resident ThreadSession
// passes its long-lived session ctx (the process survives across turns).
func spawnThreadAppServer(procCtx context.Context, req Request, cfg *ThreadConfig, p provider.ThreadProvider) (*exec.Cmd, *acp2.Client, io.Reader, error) {
	exe, args := p.ThreadCommand(cfg.WorkingDir)
	if c, ok := p.(provider.ThreadCompatChecker); ok {
		resolved, err := c.CheckThreadCompat(procCtx)
		if err != nil {
			return nil, nil, nil, err
		}
		exe = resolved
	}
	args = append(args, p.ThreadMcpArgs(cfg.McpServers)...)
	cmd := exec.CommandContext(procCtx, exe, args...)
	cmd.Dir = cfg.WorkingDir
	cmd.Env = buildThreadEnv(cfg, req.Env, req)
	// Own process group so KillGroup reaps the whole tree (node, MCP servers);
	// on Linux also kill on parent death so a SIGKILL'd manager leaves no orphans.
	SetProcessGroup(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, nil, pkgerrors.Wrap(err, "thread stdin pipe")
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, nil, pkgerrors.Wrap(err, "thread stdout pipe")
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, nil, nil, pkgerrors.Wrap(err, "thread stderr pipe")
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, nil, pkgerrors.Wrap(err, "start thread subprocess")
	}
	client := acp2.NewClient(acp2.NewTransport(stdin), stdout, p.NewThreadMapper())
	client.Start()
	return cmd, client, stderr, nil
}

// drainThreadStderr logs app-server stderr for diagnostics; it is not part of
// the protocol stream. The resident session uses it (the per-turn executor
// surfaces stderr as command output instead).
func drainThreadStderr(stderr io.Reader, agentID string) {
	scanner := bufio.NewScanner(stderr)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			slog.Debug("thread app-server stderr", "agent", agentID, "line", line)
		}
	}
}

// threadFingerprint derives a resident session's launch identity: provider,
// model, working dir, protocol, and the launch command. A change in any of
// these means the shared subprocess must be restarted.
func threadFingerprint(cfg *ThreadConfig, p provider.ThreadProvider) string {
	exe, args := p.ThreadCommand(cfg.WorkingDir)
	protocol := cfg.Protocol
	if protocol == "" {
		protocol = ProtocolV2
	}
	h := sha256.New()
	_, _ = h.Write([]byte(cfg.Provider + "\x00" + cfg.Model + "\x00" + cfg.WorkingDir + "\x00" +
		protocol + "\x00" + exe + "\x00" + strings.Join(args, "\x00")))
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// threadStartParams builds the thread/start|resume params from the config.
func threadStartParams(cfg *ThreadConfig) acp2.ThreadStartParams {
	return acp2.ThreadStartParams{
		Cwd:                   cfg.WorkingDir,
		ApprovalPolicy:        "never",
		Sandbox:               "danger-full-access",
		DeveloperInstructions: cfg.DeveloperInstructions,
		Model:                 cfg.Model,
		ExperimentalRawEvents: true,
	}
}

// establishThread performs the startup handshake (Initialize/Initialized) and
// thread/start|resume over an already-connected client, persisting the thread
// id for the next turn. emit surfaces a WARNING when repeated resume failures
// force a cold start. It returns the established thread id, whether it was
// resumed, the accumulated resume-failure count, and the session fingerprint
// the thread belongs to. The caller owns the client and its lifecycle.
//
// The startup handshake is bounded by startupCtx, NOT the turn ctx: a server
// that spawns but never completes the handshake is failed fast at
// ~StartupTimeout instead of hanging to the turn timeout. The turn/start call
// stays on the turn ctx so a slow turn still respects the turn timeout.
func establishThread(startupCtx context.Context, req Request, cfg *ThreadConfig, client *acp2.Client, emit func(Event)) (threadID string, resumed bool, resumeFailures int, fingerprint string, err error) {
	if _, err := client.Initialize(startupCtx, "laelia-machine", "0.2.0"); err != nil {
		return "", false, 0, "", err
	}
	if err := client.Initialized(); err != nil {
		return "", false, 0, "", err
	}

	// Thread inheritance: each turn spawns a fresh subprocess but resumes the
	// SAME thread id when one is persisted for this agent with a matching
	// config fingerprint. The init prompt is sent only on a cold thread/start
	// and lives in the resumed thread's history thereafter — that is the
	// per-turn token saving.
	fingerprint = threadSessionFingerprint(cfg)
	if existing, loadErr := loadACPSession(req.MachineID, req.AgentID); loadErr != nil {
		slog.Warn("failed to load persisted thread session state; cold-starting", "agent", req.AgentID, "error", loadErr)
	} else if existing != nil && existing.ThreadID != "" && existing.Fingerprint == fingerprint {
		thread, resumeErr := client.ResumeThread(startupCtx, existing.ThreadID, threadStartParams(cfg))
		if resumeErr != nil {
			// The provider lost the thread (crash, eviction, config drift the
			// fingerprint did not catch). Drop the stale id and cold-start so
			// we do not loop forever on a dead thread — the cursor is the
			// source of truth, so no message is lost, only the init prompt is
			// re-sent.
			slog.Warn("thread resume failed; cold-starting", "agent", req.AgentID, "thread_id", existing.ThreadID, "error", resumeErr)
			clearACPSession(req.MachineID, req.AgentID)
			failures, warned := recordResumeFailure(req.MachineID, req.AgentID)
			resumeFailures = failures
			if warned && emit != nil {
				emit(Event{
					Type:    v1pb.CommandEventType_WARNING,
					Summary: "thread resume failed repeatedly; starting a fresh thread",
					Warning: &v1pb.WarningPayload{Message: "Thread resume failed 3 times in a row; cold-starting a fresh thread."},
				})
			}
		} else {
			threadID = thread.ID
			resumed = true
		}
	}
	if threadID == "" {
		thread, startErr := client.StartThread(startupCtx, threadStartParams(cfg))
		if startErr != nil {
			return "", false, resumeFailures, fingerprint, startErr
		}
		threadID = thread.ID
	}

	// Persist the thread id now that thread/start|resume has accepted it, so
	// the next turn can resume even if the turn below fails — the cursor is
	// the source of truth, so a re-prompt next turn is safe.
	if saveErr := saveACPSession(req.MachineID, req.AgentID, &acpSessionState{
		ThreadID:    threadID,
		Fingerprint: fingerprint,
		CreatedAt:   time.Now().Unix(),
	}); saveErr != nil {
		slog.Warn("failed to persist thread session state; next turn will cold-start", "agent", req.AgentID, "error", saveErr)
	}
	return threadID, resumed, resumeFailures, fingerprint, nil
}

// ThreadLaunchFingerprint derives the resident-session launch identity for a
// candidate config+provider. The runner uses it to detect launch-shape changes
// (provider/model/working dir/protocol/command) without constructing a session.
func ThreadLaunchFingerprint(cfg *ThreadConfig, p provider.ThreadProvider) string {
	return threadFingerprint(cfg, p)
}
