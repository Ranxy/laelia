package v1

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/pkg/errors"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/manager/store"
)

// parseMessageName parses a chat message resource name
// "conversations/{c}/messages/{m}" into its conversation and message ids. Task
// RPCs address a task by its root message, and the agent CLI / frontend build
// this name from the conversation name and the bare message id (the form
// ChatMessage.name and thread_root already exchange).
func parseMessageName(message string) (convID, msgID uuid.UUID, err error) {
	parts := strings.Split(message, "/")
	if len(parts) == 4 && parts[0] == "conversations" && parts[2] == "messages" {
		convID, err = uuid.Parse(parts[1])
		if err != nil {
			return uuid.Nil, uuid.Nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid conversation id in message name"))
		}
		msgID, err = uuid.Parse(parts[3])
		if err != nil {
			return uuid.Nil, uuid.Nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid message id in message name"))
		}
		return convID, msgID, nil
	}
	return uuid.Nil, uuid.Nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid message name, expected conversations/{c}/messages/{m}"))
}

// requireAgentMemberByConvID resolves the calling agent from the auth context
// and ensures it is a member of the conversation. Shared by the agent-only task
// RPCs (ClaimTask, UnclaimTask, UpdateTaskStatus, CreateTask) so they do not
// each re-implement the PostMessage membership pattern.
func (s *CommandService) requireAgentMemberByConvID(ctx context.Context, convID uuid.UUID) (*store.AgentMessage, error) {
	agent, ok := GetAgentFromContext(ctx)
	if !ok || agent == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("agent authentication required"))
	}
	isMember, err := s.store.IsConversationMember(ctx, convID, store.MemberTypeAgent, agent.ResourceID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to check conversation membership"))
	}
	if !isMember {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("not a conversation member"))
	}
	return agent, nil
}

// ConvertMessageToTask turns an existing top-level message into a task. Any
// channel member (user or agent) may convert. The message must be a root in
// the conversation and not already a task. Emits a system notification row;
// per the task discovery model, conversion does not push-wake agents (they
// discover the new TODO task via task list on their next drain).
func (s *CommandService) ConvertMessageToTask(ctx context.Context, req *connect.Request[v1pb.ConvertMessageToTaskRequest]) (*connect.Response[v1pb.ConvertMessageToTaskResponse], error) {
	convID, msgID, err := parseMessageName(req.Msg.Message)
	if err != nil {
		return nil, err
	}

	isRoot, err := s.store.IsThreadRoot(ctx, convID, msgID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to validate message"))
	}
	if !isRoot {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("only a top-level message can be converted to a task"))
	}

	msg, err := s.store.ConvertMessageToTask(ctx, msgID, convID)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrTaskAlreadyExists):
			return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("message is already a task"))
		default:
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to convert message to task"))
		}
	}

	s.postTaskSystemNotification(ctx, convID, fmt.Sprintf("📋 %s converted a message to task #%d %q", resolveActorName(ctx), msg.TaskInfo.TaskNumber, truncateContent(msg.Content)))

	// A converted message is now a task root: every other user member of the
	// conversation gets a TASK activity (best-effort), mirroring as_task sends
	// and agent CreateTask. Mentions on the message additionally tag MENTION.
	s.store.GenerateActivityForMessage(msg, true, false)
	return connect.NewResponse(&v1pb.ConvertMessageToTaskResponse{Message: storeToV1ChatMessage(msg)}), nil
}

// ListTasks returns the task board for a conversation: every task root message
// with task metadata, optionally filtered by status, ordered by task number.
func (s *CommandService) ListTasks(ctx context.Context, req *connect.Request[v1pb.ListTasksRequest]) (*connect.Response[v1pb.ListTasksResponse], error) {
	convID, err := parseConversationID(req.Msg.Conversation)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid conversation name"))
	}
	var filter []int16
	for _, st := range req.Msg.StatusFilter {
		if st == v1pb.TaskStatus_TASK_STATUS_UNSPECIFIED {
			continue
		}
		filter = append(filter, int16(st))
	}
	msgs, nextToken, err := s.store.ListTasks(ctx, convID, filter, int(req.Msg.PageSize), req.Msg.PageToken)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to list tasks"))
	}
	var tasks []*v1pb.ChatMessage
	for _, m := range msgs {
		tasks = append(tasks, storeToV1ChatMessage(m))
	}
	return connect.NewResponse(&v1pb.ListTasksResponse{Tasks: tasks, NextPageToken: nextToken}), nil
}

// ListTaskCounts returns per-status task totals for a conversation, so the task
// board summary stays accurate independent of list pagination.
func (s *CommandService) ListTaskCounts(ctx context.Context, req *connect.Request[v1pb.ListTaskCountsRequest]) (*connect.Response[v1pb.ListTaskCountsResponse], error) {
	convID, err := parseConversationID(req.Msg.Conversation)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrapf(err, "invalid conversation name"))
	}
	todo, inProgress, inReview, done, err := s.store.ListTaskCounts(ctx, convID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to list task counts"))
	}
	return connect.NewResponse(&v1pb.ListTaskCountsResponse{
		TodoCount:       todo,
		InProgressCount: inProgress,
		InReviewCount:   inReview,
		DoneCount:       done,
	}), nil
}

// CreateTask posts a new top-level task message in a channel (used by agents to
// break work into subtasks for others to claim). The new task is unassigned
// (status TODO); the posting agent does NOT auto-claim it. Emits a system
// notification and wakes the other agent members so they can claim it.
func (s *CommandService) CreateTask(ctx context.Context, req *connect.Request[v1pb.CreateTaskRequest]) (*connect.Response[v1pb.CreateTaskResponse], error) {
	if req.Msg.Content == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("content must not be empty"))
	}
	convUUID, err := parseConversationID(req.Msg.Conversation)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrap(err, "invalid conversation name"))
	}
	agent, err := s.requireAgentMemberByConvID(ctx, convUUID)
	if err != nil {
		return nil, err
	}

	attachments, err := s.resolveAttachments(ctx, convUUID, req.Msg.Attachments)
	if err != nil {
		return nil, err
	}

	msg, newVersion, err := s.store.CreateTaskMessageBumpVersion(ctx, &store.ChatMessage{
		ConversationID:  convUUID,
		PrincipalID:     1, // system bot owns agent-posted channel messages
		SenderAgentID:   toNullInt32(int32(agent.ID)),
		AgentResourceID: agent.ResourceID,
		AgentName:       agent.Name,
		Role:            2, // ASSISTANT
		Content:         req.Msg.Content,
		SenderType:      store.SenderTypeAgent,
		Mentions:        req.Msg.Mentions,
		Attachments:     attachments,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to create task message"))
	}

	// A created task is work-to-be-done, not a conversational reply: the
	// creator must be able to discover and claim its own subtask (e.g. when it
	// is the only agent, or it broke a big task into steps it will execute).
	// We therefore do NOT advance the creator's cursor past its own task, and
	// we wake ALL agent members including the creator. The task message then
	// sits beyond the creator's cursor so HasUpdates triggers the next drain,
	// whose `task list` step surfaces the TODO for the creator to claim. After
	// the creator acks past the task message, HasUpdates goes false again, so
	// the loop terminates — no infinite self-wake. (Contrast PostMessage,
	// which advances the cursor and excludes self so an agent never re-reads
	// or replies to its own conversational reply.)
	s.notifyConversationAgents(ctx, convUUID, newVersion, nil)
	s.postTaskSystemNotification(ctx, convUUID, fmt.Sprintf("📋 %s created task #%d %q", agent.Name, msg.TaskInfo.TaskNumber, truncateContent(msg.Content)))

	// A created task is a top-level task root: every user member of the
	// conversation gets a TASK activity (best-effort). Mentions on the message
	// additionally tag MENTION.
	s.store.GenerateActivityForMessage(msg, true, false)

	return connect.NewResponse(&v1pb.CreateTaskResponse{Message: storeToV1ChatMessage(msg)}), nil
}

// ClaimTask atomically transitions a TODO task to IN_PROGRESS and assigns it to
// the calling agent, subscribing the agent to the task's thread so the human's
// approval reply later wakes it. Returns FAILED_PRECONDITION when the task is
// already claimed or not in TODO.
func (s *CommandService) ClaimTask(ctx context.Context, req *connect.Request[v1pb.ClaimTaskRequest]) (*connect.Response[v1pb.ClaimTaskResponse], error) {
	convID, msgID, err := parseMessageName(req.Msg.Message)
	if err != nil {
		return nil, err
	}
	agent, err := s.requireAgentMemberByConvID(ctx, convID)
	if err != nil {
		return nil, err
	}

	msg, err := s.store.ClaimTask(ctx, msgID, convID, agent.ID)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrTaskNotClaimable):
			return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("task is already claimed or not in todo"))
		case errors.Is(err, store.ErrTaskNotFound):
			return nil, connect.NewError(connect.CodeNotFound, err)
		default:
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to claim task"))
		}
	}

	// Subscribe the claiming agent to the task's thread so the human's approval
	// reply (and any other reply) wakes it. AddThreadParticipants is idempotent.
	if err := s.store.AddThreadParticipants(ctx, msgID, []int{agent.ID}); err != nil {
		slog.Warn("failed to subscribe claiming agent to task thread", "rootID", msgID, "error", err)
	}
	s.postTaskSystemNotification(ctx, convID, fmt.Sprintf("🙋 %s claimed task #%d %q", agent.Name, msg.TaskInfo.TaskNumber, truncateContent(msg.Content)))

	return connect.NewResponse(&v1pb.ClaimTaskResponse{Message: storeToV1ChatMessage(msg)}), nil
}

// UnclaimTask releases the calling agent's claim on a task it owns, setting it
// back to TODO so another agent may claim it. Not allowed on DONE (terminal).
func (s *CommandService) UnclaimTask(ctx context.Context, req *connect.Request[v1pb.UnclaimTaskRequest]) (*connect.Response[v1pb.UnclaimTaskResponse], error) {
	convID, msgID, err := parseMessageName(req.Msg.Message)
	if err != nil {
		return nil, err
	}
	agent, err := s.requireAgentMemberByConvID(ctx, convID)
	if err != nil {
		return nil, err
	}

	msg, err := s.store.UnclaimTask(ctx, msgID, agent.ID)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrTaskNotOwner):
			return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("task is not assigned to you or not in progress"))
		case errors.Is(err, store.ErrTaskNotFound):
			return nil, connect.NewError(connect.CodeNotFound, err)
		default:
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to unclaim task"))
		}
	}

	s.postTaskSystemNotification(ctx, convID, fmt.Sprintf("↩️ %s released task #%d back to todo", agent.Name, msg.TaskInfo.TaskNumber))
	return connect.NewResponse(&v1pb.UnclaimTaskResponse{Message: storeToV1ChatMessage(msg)}), nil
}

// UpdateTaskStatus advances a task's status. IN_PROGRESS -> IN_REVIEW marks the
// assignee's work ready for human review; IN_REVIEW -> DONE marks it complete.
// Only the assignee may call this. TODO -> IN_PROGRESS is performed by
// ClaimTask, not this RPC.
func (s *CommandService) UpdateTaskStatus(ctx context.Context, req *connect.Request[v1pb.UpdateTaskStatusRequest]) (*connect.Response[v1pb.UpdateTaskStatusResponse], error) {
	convID, msgID, err := parseMessageName(req.Msg.Message)
	if err != nil {
		return nil, err
	}
	agent, err := s.requireAgentMemberByConvID(ctx, convID)
	if err != nil {
		return nil, err
	}
	target := int16(req.Msg.Status)
	if target == int16(v1pb.TaskStatus_TASK_STATUS_UNSPECIFIED) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("status must not be unspecified"))
	}

	msg, err := s.store.UpdateTaskStatus(ctx, msgID, agent.ID, target)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrTaskInvalidTransition):
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("unsupported status transition (use claim for todo->in_progress; this RPC advances in_progress->in_review or in_review->done)"))
		case errors.Is(err, store.ErrTaskNotOwner):
			return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("task is not assigned to you or not in the required status"))
		case errors.Is(err, store.ErrTaskNotFound):
			return nil, connect.NewError(connect.CodeNotFound, err)
		default:
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to update task status"))
		}
	}

	switch target {
	case int16(v1pb.TaskStatus_TASK_STATUS_IN_REVIEW):
		s.postTaskSystemNotification(ctx, convID, fmt.Sprintf("👀 %s marked task #%d ready for review", agent.Name, msg.TaskInfo.TaskNumber))
	case int16(v1pb.TaskStatus_TASK_STATUS_DONE):
		s.postTaskSystemNotification(ctx, convID, fmt.Sprintf("✅ %s completed task #%d", agent.Name, msg.TaskInfo.TaskNumber))
	default:
		// Other targets are rejected above as InvalidArgument; nothing to notify.
	}
	return connect.NewResponse(&v1pb.UpdateTaskStatusResponse{Message: storeToV1ChatMessage(msg)}), nil
}

// postTaskSystemNotification inserts a sender_type=SYSTEM chat_message into the
// conversation flow so task lifecycle events (created/converted/claimed/
// released/reviewed/done) appear as a system line in the message list. It bumps
// conversation.version so the user poller surfaces it, but does NOT wake agents:
// system messages are excluded from agentRelevantMessageCondition, and we
// intentionally do not call notifyConversationAgents (task discovery is via
// task list, not push). Best-effort: failures are logged, never fatal.
func (s *CommandService) postTaskSystemNotification(ctx context.Context, convID uuid.UUID, content string) {
	if content == "" {
		return
	}
	if _, _, err := s.store.CreateChatMessageBumpVersion(ctx, &store.ChatMessage{
		ConversationID: convID,
		PrincipalID:    1, // system bot (seeded principal id 1)
		PrincipalName:  "SYSTEM",
		Role:           1,
		Content:        content,
		SenderType:     store.SenderTypeSystem,
	}); err != nil {
		slog.Warn("failed to post task system notification", "conversationID", convID, "error", err)
	}
}

// resolveActorName returns the calling user's or agent's display name, for
// system-notification text. Falls back to "Someone" when neither is present
// (should not happen for authenticated RPCs).
func resolveActorName(ctx context.Context) string {
	if user, ok := GetUserFromContext(ctx); ok && user != nil {
		return user.Name
	}
	if agent, ok := GetAgentFromContext(ctx); ok && agent != nil {
		return agent.Name
	}
	return "Someone"
}

// truncateContent collapses a message body to a single-line summary of at most
// maxTaskTitleLen runes, for embedding in system-notification text.
func truncateContent(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= maxTaskTitleLen {
		return s
	}
	return string(r[:maxTaskTitleLen]) + "…"
}

// maxTaskTitleLen is the rune cap on a task title excerpt embedded in system
// notification text — long enough to recognize the task, short enough to keep
// the notification line readable.
const maxTaskTitleLen = 60
