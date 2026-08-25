package v1

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/pkg/errors"

	"github.com/Ranxy/laelia/backend/common"
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
		PrincipalHandle: common.SystemBotHandle,
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

	// A team-assigned task can only be claimed by the team's leader. Inspect
	// the current task to decide which claim path to use.
	current, err := s.store.GetTaskMessage(ctx, msgID)
	if err != nil {
		if errors.Is(err, store.ErrTaskNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, err)
		}
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to read task before claim"))
	}
	var msg *store.ChatMessage
	if current.TaskInfo != nil && current.TaskInfo.AssigneeType == 3 {
		msg, err = s.store.ClaimTeamTask(ctx, msgID, convID, agent.ID)
	} else {
		msg, err = s.store.ClaimTask(ctx, msgID, convID, agent.ID)
	}
	if err != nil {
		switch {
		case errors.Is(err, store.ErrTaskNotClaimable):
			return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("task is already claimed, not in todo, or only the team leader may claim it"))
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

	// When a team leader claims a team-assigned task, re-post the team
	// instruction into the task thread so the leader immediately sees the team
	// composition, member responsibilities, and the expectation to split and
	// delegate the work.
	if current.TaskInfo != nil && current.TaskInfo.AssigneeType == 3 && msg.TaskInfo != nil && msg.TaskInfo.AssigneeTeamID != "" {
		if team, teamErr := s.store.GetAgentTeamByResourceID(ctx, msg.TaskInfo.AssigneeTeamID); teamErr == nil && team != nil {
			s.postTeamAssignmentMessage(ctx, convID, msgID, team, msg)
		}
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

// UpdateTaskStatus moves a task between any of the four statuses. Any channel
// member (user or agent) may call it; DONE closes the task (sets completed_at),
// and moving out of DONE clears it. Authorization is the IAM interceptor's
// conversations.send check against the task's conversation. Emits a system
// notification row.
func (s *CommandService) UpdateTaskStatus(ctx context.Context, req *connect.Request[v1pb.UpdateTaskStatusRequest]) (*connect.Response[v1pb.UpdateTaskStatusResponse], error) {
	convID, msgID, err := parseMessageName(req.Msg.Message)
	if err != nil {
		return nil, err
	}
	target := int16(req.Msg.Status)
	if target == int16(v1pb.TaskStatus_TASK_STATUS_UNSPECIFIED) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("status must not be unspecified"))
	}

	msg, err := s.store.UpdateTaskStatus(ctx, msgID, target)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrTaskInvalidTransition):
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid task status"))
		case errors.Is(err, store.ErrTaskNotFound):
			return nil, connect.NewError(connect.CodeNotFound, err)
		default:
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to update task status"))
		}
	}

	actor := resolveActorName(ctx)
	switch target {
	case int16(v1pb.TaskStatus_TASK_STATUS_IN_REVIEW):
		s.postTaskSystemNotification(ctx, convID, fmt.Sprintf("👀 %s marked task #%d ready for review", actor, msg.TaskInfo.TaskNumber))
	case int16(v1pb.TaskStatus_TASK_STATUS_DONE):
		s.postTaskSystemNotification(ctx, convID, fmt.Sprintf("✅ %s completed task #%d", actor, msg.TaskInfo.TaskNumber))
	default:
		// TODO / IN_PROGRESS transitions carry no lifecycle notification.
	}
	return connect.NewResponse(&v1pb.UpdateTaskStatusResponse{Message: storeToV1ChatMessage(msg)}), nil
}

// AssignTask assigns a task to a channel member (user or agent). A user
// assignee is a display-only "owner" and does not participate in the
// claim/process flow; an agent assignee is the working owner. Any channel
// member may assign. Authorization is the IAM interceptor's conversations.send
// check against the task's conversation. Emits a system notification row.
func (s *CommandService) AssignTask(ctx context.Context, req *connect.Request[v1pb.AssignTaskRequest]) (*connect.Response[v1pb.AssignTaskResponse], error) {
	convID, msgID, err := parseMessageName(req.Msg.Message)
	if err != nil {
		return nil, err
	}
	if req.Msg.MemberId == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("member_id must not be empty"))
	}
	msg, err := s.store.AssignTask(ctx, msgID, convID, req.Msg.MemberType, req.Msg.MemberId)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrTaskNotFound):
			return nil, connect.NewError(connect.CodeNotFound, err)
		case errors.Is(err, store.ErrTaskAssigneeNotMember):
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("assignee is not a member of this conversation"))
		case errors.Is(err, store.ErrTaskTeamAlreadyAssigned):
			return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("team is already assigned to an active task"))
		default:
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to assign task"))
		}
	}

	// Team assignment: auto-join all team agents to the conversation, write the
	// team instruction system message into the task thread, and wake the team.
	if req.Msg.MemberType == 3 {
		if err := s.afterTeamAssign(ctx, convID, msgID, msg); err != nil {
			return nil, err
		}
	}

	s.postTaskSystemNotification(ctx, convID, fmt.Sprintf("👤 %s assigned task #%d to %s", resolveActorName(ctx), msg.TaskInfo.TaskNumber, msg.TaskInfo.AssigneeName))
	return connect.NewResponse(&v1pb.AssignTaskResponse{Message: storeToV1ChatMessage(msg)}), nil
}

// afterTeamAssign performs the team-specific side effects after a task is
// assigned to a team: auto-join all team agents to the conversation, write the
// team instruction system message into the task thread, and wake the team.
func (s *CommandService) afterTeamAssign(ctx context.Context, convID, msgID uuid.UUID, msg *store.ChatMessage) error {
	if msg.TaskInfo == nil || msg.TaskInfo.AssigneeTeamID == "" {
		return nil
	}
	team, err := s.store.GetAgentTeamByResourceID(ctx, msg.TaskInfo.AssigneeTeamID)
	if err != nil {
		return connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to load assigned team"))
	}
	if team == nil {
		return connect.NewError(connect.CodeInternal, errors.New("assigned team not found"))
	}

	// Auto-join all team agents to the conversation. Their cursors are seeded to
	// the current version BEFORE the activation message is written, so they only
	// see the new team-assignment message (and follow-ups), not the conversation
	// history before it.
	var inputs []store.ConversationMemberInput
	var teamAgentIDs []int
	for _, m := range team.Members {
		existingRole, _, err := s.store.GetConversationMembership(ctx, convID, store.MemberTypeAgent, m.AgentResourceID)
		if err != nil {
			return connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to check team member membership"))
		}
		if existingRole != 0 {
			if agent, agentErr := s.store.GetAgentByResourceID(ctx, m.AgentResourceID); agentErr == nil && agent != nil {
				teamAgentIDs = append(teamAgentIDs, agent.ID)
			}
			continue
		}
		inputs = append(inputs, store.ConversationMemberInput{MemberType: store.MemberTypeAgent, MemberID: m.AgentResourceID})
	}
	if len(inputs) > 0 {
		if err := s.store.AddConversationMembers(ctx, convID, inputs); err != nil {
			return connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to add team members to conversation"))
		}
		for _, m := range inputs {
			if agent, agentErr := s.store.GetAgentByResourceID(ctx, m.MemberID); agentErr == nil && agent != nil {
				teamAgentIDs = append(teamAgentIDs, agent.ID)
			}
		}
	}

	// Subscribe every team agent to the task thread BEFORE the activation
	// message so the new thread reply counts as relevant work for them.
	if len(teamAgentIDs) > 0 {
		if err := s.store.AddThreadParticipants(ctx, msgID, teamAgentIDs); err != nil {
			slog.Warn("failed to subscribe team agents to task thread", "rootID", msgID, "error", err)
		}
		// Seed cursors to the current version so only the activation message
		// below (which bumps the version) is seen as new.
		for _, agentID := range teamAgentIDs {
			if err := s.store.SeedCursorOnJoin(ctx, agentID, convID); err != nil {
				slog.Warn("failed to seed team member cursor", "agentID", agentID, "conversationID", convID, "error", err)
			}
		}
	}

	// Write the team instruction as an AGENT message (not SYSTEM) so it is a
	// real wake signal: agent wake routing deliberately excludes SYSTEM rows.
	// Using the leader as the sender keeps the message attributable to the
	// Write the team instruction as an AGENT message (not SYSTEM) so it is a
	// real wake signal: agent wake routing deliberately excludes SYSTEM rows.
	// Using the leader as the sender keeps the message attributable to the
	// team lead, which is the natural actor for a team assignment.
	s.postTeamAssignmentMessage(ctx, convID, msgID, team, msg)

	// Wake all team agents so they discover the assignment.
	s.notifyConversationAgents(ctx, convID, 0, nil)
	return nil
}

// postTeamAssignmentMessage writes the team instruction into the task thread
// using the leader's agent identity. It is called both when a team is assigned
// and when the leader claims the task, so the leader always sees the team
// composition and responsibilities right where the work happens.
func (s *CommandService) postTeamAssignmentMessage(ctx context.Context, convID, msgID uuid.UUID, team *store.AgentTeamMessage, msg *store.ChatMessage) {
	content := buildTeamAssignmentMessage(team, msg)
	if content == "" {
		return
	}
	leader := teamLeaderMember(team)
	if leader == nil {
		return
	}
	if _, _, err := s.store.CreateChatMessageBumpVersion(ctx, &store.ChatMessage{
		ConversationID:      convID,
		PrincipalID:         1,
		PrincipalHandle:     common.SystemBotHandle,
		Role:                2,
		Content:             content,
		SenderType:          store.SenderTypeAgent,
		SenderAgentID:       sql.NullInt32{Int32: int32(leader.AgentID), Valid: true},
		AgentResourceID:     leader.AgentResourceID,
		AgentName:           leader.AgentResourceID,
		ThreadRootMessageID: uuid.NullUUID{UUID: msgID, Valid: true},
	}); err != nil {
		slog.Warn("failed to post team assignment message", "conversationID", convID, "rootID", msgID, "error", err)
	}
}

// buildTeamAssignmentMessage renders the system message posted into a task
// thread when a team is assigned.
func buildTeamAssignmentMessage(team *store.AgentTeamMessage, msg *store.ChatMessage) string {
	var b strings.Builder
	b.WriteString("[TEAM ASSIGNMENT]\n")
	b.WriteString("Team: " + team.Title + "\n")
	b.WriteString("Team ID: " + common.FormatAgentTeamName(team.ResourceID) + "\n")
	for _, m := range team.Members {
		if m.Role == store.AgentTeamRoleLeader {
			b.WriteString("Leader: " + agentNameForTeam(m) + "\n")
		}
	}
	if team.TeamPrompt != "" {
		b.WriteString("Team Prompt: " + team.TeamPrompt + "\n")
	}
	b.WriteString("\nTask Instruction: " + msg.Content + "\n\n")
	b.WriteString("Roles:\n")
	for _, m := range team.Members {
		role := "member"
		if m.Role == store.AgentTeamRoleLeader {
			role = "leader"
		}
		line := "- " + agentNameForTeam(m) + " (" + role + ")"
		if m.Responsibility != "" {
			line += ": " + m.Responsibility
		}
		if m.Role == store.AgentTeamRoleLeader {
			line += ". You are the leader. You own the task outcome. You must break the work into subtasks, coordinate other members, track progress, and ensure the task moves from IN_PROGRESS to IN_REVIEW/DONE."
		} else {
			line += ". Follow the leader's coordination and complete the assigned subtasks."
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

// agentNameForTeam returns a display name for a team member. The store member
// row carries the agent resource id; we use it as a stable handle.
// teamLeaderMember returns the LEADER member of a team, or nil.
func teamLeaderMember(team *store.AgentTeamMessage) *store.AgentTeamMemberMessage {
	for _, m := range team.Members {
		if m.Role == store.AgentTeamRoleLeader {
			return m
		}
	}
	return nil
}

func agentNameForTeam(m *store.AgentTeamMemberMessage) string {
	if m.AgentResourceID != "" {
		return m.AgentResourceID
	}
	return fmt.Sprintf("agent-%d", m.AgentID)
}

// CloseTask lets a channel member close a task directly from the UI: any
// non-DONE task transitions to DONE (terminal), setting completed_at. Unlike
// UpdateTaskStatus it does not require assignee ownership and accepts every
// open status (TODO / IN_PROGRESS / IN_REVIEW), so the user can close a task
// without going through the assignee. Closing an already-DONE task is
// idempotent (no duplicate system notification). Authorization is the IAM
// interceptor's conversations.send check against the task's conversation.
func (s *CommandService) CloseTask(ctx context.Context, req *connect.Request[v1pb.CloseTaskRequest]) (*connect.Response[v1pb.CloseTaskResponse], error) {
	convID, msgID, err := parseMessageName(req.Msg.Message)
	if err != nil {
		return nil, err
	}
	msg, changed, err := s.store.CloseTask(ctx, msgID, convID)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrTaskNotFound):
			return nil, connect.NewError(connect.CodeNotFound, err)
		default:
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to close task"))
		}
	}
	if changed {
		s.postTaskSystemNotification(ctx, convID, fmt.Sprintf("✅ %s closed task #%d %q", resolveActorName(ctx), msg.TaskInfo.TaskNumber, truncateContent(msg.Content)))
	}
	return connect.NewResponse(&v1pb.CloseTaskResponse{Message: storeToV1ChatMessage(msg)}), nil
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

// singleLinePreview collapses a message body to a single-line excerpt of at
// most maxRunes runes. Newlines fold to spaces and an overlong body is cut
// with a trailing ellipsis; visual truncation is left to the client's CSS, so
// this only bounds the payload. Rune-safe for multi-byte text.
func singleLinePreview(s string, maxRunes int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes]) + "…"
}

// truncateContent collapses a message body to a single-line summary of at most
// maxTaskTitleLen runes, for embedding in system-notification text.
func truncateContent(s string) string {
	return singleLinePreview(s, maxTaskTitleLen)
}

// maxTaskTitleLen is the rune cap on a task title excerpt embedded in system
// notification text — long enough to recognize the task, short enough to keep
// the notification line readable.
const maxTaskTitleLen = 60
