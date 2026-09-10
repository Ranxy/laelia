package client

import (
	"context"
	"fmt"
	"log/slog"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/laelia/backend/agent/executor"
	"github.com/Ranxy/laelia/backend/agent/workspace"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

// messageRouter dispatches manager messages arriving on the AgentChannel. It
// keeps the receive pump free of policy: BeginSession replies go to the drain
// runner, NewMessages wakes the drain runner (and steers a pi turn when
// possible), Cancel/Steer act on the in-flight runtime, and workspace requests
// are answered on their own goroutines so a slow disk never blocks the pump.
type messageRouter struct {
	stream *commandStream
}

func newMessageRouter(c *commandStream) *messageRouter {
	return &messageRouter{stream: c}
}

func (r *messageRouter) route(ctx context.Context, conn *agentConn, msg *v1pb.ManagerStreamMessage) {
	switch m := msg.Message.(type) {
	case *v1pb.ManagerStreamMessage_BeginSessionResponse:
		resp := m.BeginSessionResponse
		select {
		case conn.beginResps <- resp:
		case <-conn.done:
		}

	case *v1pb.ManagerStreamMessage_NewMessages:
		// Best-effort wake; the durable cursor recovers anything missed.
		r.stream.wake()
		// Same-turn steering: when the in-flight runtime supports it
		// (pi), push a content-free notice into the running turn so the
		// agent reacts now instead of waiting for the turn to end. Any
		// failure (non-pi runtime, turn about to end, queue full) falls
		// back to the wake above.
		if ex := r.stream.getCurrentExecutor(); ex != nil {
			if s, ok := ex.(steerer); ok {
				if err := s.Steer(buildSteerNotice(m.NewMessages)); err != nil {
					slog.Debug("same-turn steer failed; post-turn wake is the fallback", "error", err)
				}
			}
		}

	case *v1pb.ManagerStreamMessage_Cancel:
		if ex := r.stream.getCurrentExecutor(); ex != nil {
			slog.Info("cancelling command", "commandID", m.Cancel.CommandId)
			ex.Cancel()
		}

	case *v1pb.ManagerStreamMessage_Pong:
	// pong received, link acknowledged

	case *v1pb.ManagerStreamMessage_Steer:
		st := m.Steer
		slog.Info("received steer", "commandID", st.CommandId)
		if ex := r.stream.getCurrentExecutor(); ex != nil {
			if resolver, ok := ex.(executor.SteerResolver); ok {
				resolver.Steer(st.Text)
			}
		}

	case *v1pb.ManagerStreamMessage_PromptReleaseNotice:
		notice := m.PromptReleaseNotice
		if notice == nil {
			break
		}
		// Same-turn steering: only the pi runtime (steerer) reports delivery
		// success/failure, so only it may ack and mark the version as seen. The
		// ACP v2 ThreadExecutor.Steer has no success signal (it silently no-ops
		// when the turn gate is closed), so we do NOT ack on that path — we fall
		// through to queueing for the next drain turn to guarantee delivery.
		if ex := r.stream.getCurrentExecutor(); ex != nil {
			if s, ok := ex.(steerer); ok {
				if err := s.Steer(notice.GetMessage()); err == nil {
					r.stream.recordSteeredPromptVersion(notice.GetPromptVersion())
					_ = sendPromptReleaseNoticeAck(conn.sender, notice)
					break
				}
			}
		}
		// Fallback: queue for the next drain turn and wake so it is picked up
		// promptly.
		r.stream.setPendingPromptNotice(notice)
		r.stream.wake()

	case *v1pb.ManagerStreamMessage_WorkspaceListRequest:
		// File reads run on their own goroutine: a slow disk must not
		// block the receive pump (BeginSession / NewMessages / Cancel).
		go r.stream.handleWorkspaceList(ctx, conn.sender, m.WorkspaceListRequest)

	case *v1pb.ManagerStreamMessage_WorkspaceReadRequest:
		go r.stream.handleWorkspaceRead(ctx, conn.sender, m.WorkspaceReadRequest)

	default:
		slog.Warn("unknown message type from manager")
	}
}

// steerer is the optional in-turn message injection capability a runtime may
// implement (pi does; ACP does not). Steer delivers a notice into the running
// turn; it must be non-blocking and best-effort.
type steerer interface {
	Steer(text string) error
}

// sendPromptReleaseNoticeAck reports to the manager that a prompt release
// notice was injected (steered or queued into a turn), so the manager can stop
// re-pushing it.
func sendPromptReleaseNoticeAck(sender streamSender, notice *v1pb.PromptReleaseNotice) error {
	if sender == nil || notice == nil {
		return nil
	}
	return sender.Send(&v1pb.AgentStreamMessage{
		Message: &v1pb.AgentStreamMessage_PromptReleaseNoticeAck{
			PromptReleaseNoticeAck: &v1pb.PromptReleaseNoticeAck{
				NoticeKey:     notice.GetNoticeKey(),
				PromptVersion: notice.GetPromptVersion(),
			},
		},
	})
}

// buildSteerNotice renders the content-free inbox notice steered into a running
// turn. The agent pulls the real messages itself via `laelia-machine message
// check` / `thread check`; the notice only says that something arrived (and
// whether it is a thread reply), never the payload.
func buildSteerNotice(nm *v1pb.NewMessagesAvailable) string {
	if nm != nil && nm.ThreadRootMessageId != "" {
		return "[Laelia inbox notice: new reply in a thread you follow. Run `laelia-machine thread check` at a natural breakpoint.]"
	}
	count := 0
	if nm != nil {
		count = len(nm.ConversationIds)
	}
	if count <= 1 {
		return "[Laelia inbox notice: new messages arrived. Run `laelia-machine message check` at a natural breakpoint.]"
	}
	return fmt.Sprintf("[Laelia inbox notice: new messages arrived in %d conversations. Run `laelia-machine message check` at a natural breakpoint.]", count)
}

// handleWorkspaceList lists one directory level of this agent's workspace and
// replies over the agent stream. The manager gates this by owner/admin
// permission; the workspace package enforces the never-visible/secret policy.
func (c *commandStream) handleWorkspaceList(_ context.Context, sender streamSender, req *v1pb.WorkspaceListRequest) {
	if req == nil {
		return
	}
	entries, err := workspace.List(executor.AgentWorkingDir(c.machineID, c.agentID), req.DirPath, req.IncludeHidden)
	if err != nil {
		slog.Warn("workspace list failed", "agentID", c.agentID, "dirPath", req.DirPath, "error", err)
	}
	protoEntries := make([]*v1pb.WorkspaceEntry, 0, len(entries))
	for _, e := range entries {
		var modifiedAt *timestamppb.Timestamp
		if !e.ModifiedAt.IsZero() {
			modifiedAt = timestamppb.New(e.ModifiedAt)
		}
		protoEntries = append(protoEntries, &v1pb.WorkspaceEntry{
			Name:        e.Name,
			Path:        e.Path,
			IsDirectory: e.IsDir,
			Size:        e.Size,
			ModifiedAt:  modifiedAt,
			IsHidden:    e.IsHidden,
		})
	}
	_ = sender.Send(&v1pb.AgentStreamMessage{
		Message: &v1pb.AgentStreamMessage_WorkspaceListResponse{
			WorkspaceListResponse: &v1pb.WorkspaceListResponse{
				RequestId: req.RequestId,
				Entries:   protoEntries,
			},
		},
	})
}

// handleWorkspaceRead previews one workspace file and replies over the agent
// stream. Refusals (sensitive file, too large, directory) come back in the
// response's error field; OS failures are logged and returned as errors too.
func (c *commandStream) handleWorkspaceRead(_ context.Context, sender streamSender, req *v1pb.WorkspaceReadRequest) {
	if req == nil {
		return
	}
	result, err := workspace.Read(executor.AgentWorkingDir(c.machineID, c.agentID), req.Path)
	if err != nil {
		slog.Warn("workspace read failed", "agentID", c.agentID, "path", req.Path, "error", err)
		result.Error = err.Error()
	}
	_ = sender.Send(&v1pb.AgentStreamMessage{
		Message: &v1pb.AgentStreamMessage_WorkspaceReadResponse{
			WorkspaceReadResponse: &v1pb.WorkspaceReadResponse{
				RequestId: req.RequestId,
				Content:   result.Content,
				Binary:    result.Binary,
				Size:      result.Size,
				MimeType:  result.MimeType,
				Encoding:  result.Encoding,
				Error:     result.Error,
			},
		},
	})
}
