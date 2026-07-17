package v1

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/laelia/backend/common/permission"
	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
	"github.com/Ranxy/laelia/backend/manager/component/schedule"
	"github.com/Ranxy/laelia/backend/manager/store"
)

// parseReminderName parses a reminder resource name "reminders/{message_id}"
// into its trigger message id. The reminder's identity is its trigger message,
// so this is all the handler needs to load a reminder.
func parseReminderName(name string) (uuid.UUID, error) {
	parts := strings.Split(name, "/")
	if len(parts) == 2 && parts[0] == "reminders" {
		id, err := uuid.Parse(parts[1])
		if err != nil {
			return uuid.Nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid reminder id in name"))
		}
		return id, nil
	}
	return uuid.Nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid reminder name, expected reminders/{message_id}"))
}

// storeToV1Reminder maps a store.Reminder to its proto form. Timestamps that
// may be NULL (next_retry_at, last_attempt_at, last_fired_at,
// last_completed_at) are emitted only when valid.
func storeToV1Reminder(r *store.Reminder) *v1pb.Reminder {
	rem := &v1pb.Reminder{
		Name:          fmt.Sprintf("reminders/%s", r.MessageID),
		Conversation:  fmt.Sprintf("conversations/%s", r.ConversationID),
		Message:       fmt.Sprintf("conversations/%s/messages/%s", r.ConversationID, r.MessageID),
		AssigneeAgent: formatAgentName(r.AssigneeResourceID),
		AssigneeName:  r.AssigneeName,
		TaskContent:   r.TaskContent,
		FireAt:        timestamppb.New(r.FireAt),
		CronExpr:      r.CronExpr,
		Tz:            r.Tz,
		Status:        v1pb.ReminderStatus(r.Status),
		RetryCount:    r.RetryCount,
		Result:        r.Result,
		CreatedAt:     timestamppb.New(r.CreatedAt),
		UpdatedAt:     timestamppb.New(r.UpdatedAt),
	}
	if r.NextRetryAt.Valid {
		rem.NextRetryAt = timestamppb.New(r.NextRetryAt.Time)
	}
	if r.LastAttemptAt.Valid {
		rem.LastAttemptAt = timestamppb.New(r.LastAttemptAt.Time)
	}
	if r.LastFiredAt.Valid {
		rem.LastFiredAt = timestamppb.New(r.LastFiredAt.Time)
	}
	if r.LastCompletedAt.Valid {
		rem.LastCompletedAt = timestamppb.New(r.LastCompletedAt.Time)
	}
	return rem
}

// requireReminderAccess gates object-level access to a reminder. Access is
// granted to the owning agent (the assignee), a user holding
// conversations.reviewAll (cross-conversation oversight), or any member of the
// reminder's conversation (the reminder's trigger message lives there, and its
// thread is the discussion channel). Mirrors requireCommandAccess.
func (s *CommandService) requireReminderAccess(ctx context.Context, r *store.Reminder) error {
	if agent, ok := GetAgentFromContext(ctx); ok && agent != nil && agent.ID == r.AssigneeAgentID {
		return nil
	}
	user, _ := GetUserFromContext(ctx)
	if user != nil {
		reviewAll, err := s.iam.CheckPermission(ctx, permission.ConversationsReviewAll, user, nil, nil)
		if err != nil {
			return connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to resolve reviewAll permission"))
		}
		if reviewAll {
			return nil
		}
	}
	memberType, memberID, ok := callerMemberInfo(user, nil)
	if !ok {
		// Agent caller that is not the owner: deny.
		return connect.NewError(connect.CodePermissionDenied, errors.New("no access to reminder"))
	}
	isMember, err := s.store.IsConversationMember(ctx, r.ConversationID, memberType, memberID)
	if err != nil {
		return connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to check conversation membership"))
	}
	if isMember {
		return nil
	}
	return connect.NewError(connect.CodePermissionDenied, errors.New("no access to reminder"))
}

// requireReminderOwner gates mutations that only the owning agent may perform
// (CompleteReminder, FailReminder). A workspace admin may not complete an
// agent's reminder on its behalf — the work is the agent's to report.
func (*CommandService) requireReminderOwner(ctx context.Context, r *store.Reminder) error {
	agent, ok := GetAgentFromContext(ctx)
	if !ok || agent == nil || agent.ID != r.AssigneeAgentID {
		return connect.NewError(connect.CodePermissionDenied, errors.New("only the owning agent may modify the reminder"))
	}
	return nil
}

// ConvertMessageToReminder turns an existing top-level message into a scheduled
// reminder owned by the calling agent (atomic create+claim). The message must be
// a root in the conversation and not already a reminder. The agent is subscribed
// to the reminder's thread so discussion replies wake it. A system message
// records the schedule in the thread.
func (s *CommandService) ConvertMessageToReminder(ctx context.Context, req *connect.Request[v1pb.ConvertMessageToReminderRequest]) (*connect.Response[v1pb.ConvertMessageToReminderResponse], error) {
	convID, msgID, err := parseMessageName(req.Msg.Message)
	if err != nil {
		return nil, err
	}
	agent, err := s.requireAgentMemberByConvID(ctx, convID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Msg.TaskContent) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("task_content must not be empty"))
	}
	cronExpr := strings.TrimSpace(req.Msg.CronExpr)
	tz := strings.TrimSpace(req.Msg.Tz)
	if tz == "" {
		tz = "UTC"
	}
	if cronExpr != "" {
		if err := schedule.Validate(cronExpr, tz); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
	}
	// Determine the first fire. An explicit fire_at (when provided and valid)
	// wins and must be in the future; otherwise, for a recurring reminder (cron
	// set), compute the first fire from the cron expression starting at now. A
	// one-shot reminder (no cron) requires an explicit fire_at.
	fireAt, err := resolveFireAt(req.Msg.FireAt, cronExpr, tz)
	if err != nil {
		return nil, err
	}

	isRoot, err := s.store.IsThreadRoot(ctx, convID, msgID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to validate message"))
	}
	if !isRoot {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("only a top-level message can be converted to a reminder"))
	}

	reminder, err := s.store.ConvertMessageToReminder(ctx, msgID, convID, agent.ID, req.Msg.TaskContent, fireAt, cronExpr, tz)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrReminderAlreadyExists):
			return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("message is already a reminder"))
		default:
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to convert message to reminder"))
		}
	}

	// Subscribe the owning agent to the reminder's thread so discussion replies
	// (the user negotiating the schedule) wake it. AddThreadParticipants is
	// idempotent. Best-effort: a failure here does not roll back the reminder.
	if err := s.store.AddThreadParticipants(ctx, msgID, []int{agent.ID}); err != nil {
		slog.Warn("failed to subscribe agent to reminder thread", "rootID", msgID, "error", err)
	}
	if msg := s.postReminderSystemMessage(ctx, convID, msgID, fmt.Sprintf("⏰ %s scheduled a reminder for %s: %s", agent.Name, fireAt.In(time.UTC).Format(time.RFC3339), truncateContent(req.Msg.TaskContent))); msg != nil {
		s.store.GenerateActivityForMessage(ctx, msg, false, true)
	}

	return connect.NewResponse(&v1pb.ConvertMessageToReminderResponse{Reminder: storeToV1Reminder(reminder)}), nil
}

// ListReminders returns reminders filtered by owning agent and/or conversation
// and/or status. Used by the agent-page Reminders tab (user) and the agent CLI
// self-list. A non-admin user only sees reminders whose conversation they are a
// member of; workspace admins and agent callers see everything their other
// filters match.
func (s *CommandService) ListReminders(ctx context.Context, req *connect.Request[v1pb.ListRemindersRequest]) (*connect.Response[v1pb.ListRemindersResponse], error) {
	var agentID int
	if req.Msg.Agent != "" {
		resourceID := parseAgentResourceID(req.Msg.Agent)
		agent, err := s.store.GetAgentByResourceID(ctx, resourceID)
		if err != nil || agent == nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("unknown agent in filter"))
		}
		agentID = agent.ID
	}
	var convID uuid.UUID
	if req.Msg.Conversation != "" {
		var err error
		convID, err = parseConversationID(req.Msg.Conversation)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.Wrap(err, "invalid conversation name"))
		}
	}
	var statusFilter []int16
	for _, st := range req.Msg.StatusFilter {
		if st == v1pb.ReminderStatus_REMINDER_STATUS_UNSPECIFIED {
			continue
		}
		statusFilter = append(statusFilter, int16(st))
	}

	// Restrict a user without conversations.reviewAll to reminders in
	// conversations they belong to. Agent callers (the CLI self-list) keep the
	// unfiltered query; they are inherently members of their own conversations.
	var viewer *store.ConversationMemberFilter
	if user, _ := GetUserFromContext(ctx); user != nil {
		reviewAll, err := s.iam.CheckPermission(ctx, permission.ConversationsReviewAll, user, nil, nil)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to resolve reviewAll permission"))
		}
		if !reviewAll {
			viewer = &store.ConversationMemberFilter{MemberType: store.MemberTypeUser, MemberID: fmt.Sprintf("%d", user.ID)}
		}
	}

	reminders, nextToken, err := s.store.ListReminders(ctx, agentID, convID, statusFilter, viewer, int(req.Msg.PageSize), req.Msg.PageToken)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to list reminders"))
	}
	out := make([]*v1pb.Reminder, 0, len(reminders))
	for _, r := range reminders {
		out = append(out, storeToV1Reminder(r))
	}
	return connect.NewResponse(&v1pb.ListRemindersResponse{Reminders: out, NextPageToken: nextToken}), nil
}

// GetReminder returns a single reminder by its resource name. The caller must
// be the owning agent, a workspace admin, or a member of the reminder's
// conversation.
func (s *CommandService) GetReminder(ctx context.Context, req *connect.Request[v1pb.GetReminderRequest]) (*connect.Response[v1pb.GetReminderResponse], error) {
	msgID, err := parseReminderName(req.Msg.Name)
	if err != nil {
		return nil, err
	}
	r, err := s.store.GetReminder(ctx, msgID)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrReminderNotFound):
			return nil, connect.NewError(connect.CodeNotFound, errors.New("reminder not found"))
		default:
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get reminder"))
		}
	}
	if err := s.requireReminderAccess(ctx, r); err != nil {
		return nil, err
	}
	return connect.NewResponse(&v1pb.GetReminderResponse{Reminder: storeToV1Reminder(r)}), nil
}

// UpdateReminder edits the schedule (fire_at/cron_expr/tz) and/or task_content
// of a reminder. Full-replacement semantics: the request carries the full
// intended schedule + content (the frontend edit form always populates from
// the current state). Editing a DUE or MISSED reminder resets it to PENDING
// with the new schedule. The caller must be the owning agent, a workspace
// admin, or a conversation member.
func (s *CommandService) UpdateReminder(ctx context.Context, req *connect.Request[v1pb.UpdateReminderRequest]) (*connect.Response[v1pb.UpdateReminderResponse], error) {
	msgID, err := parseReminderName(req.Msg.Name)
	if err != nil {
		return nil, err
	}
	r, err := s.store.GetReminder(ctx, msgID)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrReminderNotFound):
			return nil, connect.NewError(connect.CodeNotFound, errors.New("reminder not found"))
		default:
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get reminder"))
		}
	}
	if err := s.requireReminderAccess(ctx, r); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Msg.TaskContent) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("task_content must not be empty"))
	}
	cronExpr := strings.TrimSpace(req.Msg.CronExpr)
	tz := strings.TrimSpace(req.Msg.Tz)
	if tz == "" {
		tz = "UTC"
	}
	if cronExpr != "" {
		if err := schedule.Validate(cronExpr, tz); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
	}
	// Determine the new fire time. If fire_at is provided, use it; otherwise
	// (cron set, fire_at omitted) compute the next cron fire from now. One-shot
	// reminders require an explicit fire_at.
	var fireAt time.Time
	if req.Msg.FireAt != nil && req.Msg.FireAt.IsValid() && req.Msg.FireAt.AsTime().Unix() > 0 {
		fireAt = req.Msg.FireAt.AsTime()
	} else if cronExpr != "" {
		fireAt, err = schedule.NextFire(cronExpr, tz, time.Now())
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to compute next fire"))
		}
	} else {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("fire_at is required for a one-shot reminder"))
	}

	updated, err := s.store.UpdateReminderFields(ctx, msgID, fireAt, cronExpr, tz, req.Msg.TaskContent)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrReminderInvalidTransition):
			return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("reminder is in a terminal status; cancel and recreate instead"))
		case errors.Is(err, store.ErrReminderNotFound):
			return nil, connect.NewError(connect.CodeNotFound, errors.New("reminder not found"))
		default:
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to update reminder"))
		}
	}
	actor := resolveActorName(ctx)
	if msg := s.postReminderSystemMessage(ctx, updated.ConversationID, msgID, fmt.Sprintf("📝 %s updated the reminder schedule: %s", actor, fireAt.In(time.UTC).Format(time.RFC3339))); msg != nil {
		s.store.GenerateActivityForMessage(ctx, msg, false, true)
	}
	return connect.NewResponse(&v1pb.UpdateReminderResponse{Reminder: storeToV1Reminder(updated)}), nil
}

// CancelReminder cancels a reminder. A PENDING, DUE, or MISSED reminder may be
// cancelled; terminal statuses are a no-op returning the current state. The
// caller must be the owning agent, a workspace admin, or a conversation member.
func (s *CommandService) CancelReminder(ctx context.Context, req *connect.Request[v1pb.CancelReminderRequest]) (*connect.Response[v1pb.CancelReminderResponse], error) {
	msgID, err := parseReminderName(req.Msg.Name)
	if err != nil {
		return nil, err
	}
	r, err := s.store.GetReminder(ctx, msgID)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrReminderNotFound):
			return nil, connect.NewError(connect.CodeNotFound, errors.New("reminder not found"))
		default:
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get reminder"))
		}
	}
	if err := s.requireReminderAccess(ctx, r); err != nil {
		return nil, err
	}
	updated, err := s.store.CancelReminder(ctx, msgID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to cancel reminder"))
	}
	if updated.Status == int16(v1pb.ReminderStatus_REMINDER_STATUS_CANCELLED) {
		if msg := s.postReminderSystemMessage(ctx, updated.ConversationID, msgID, fmt.Sprintf("🚫 %s cancelled the reminder", resolveActorName(ctx))); msg != nil {
			s.store.GenerateActivityForMessage(ctx, msg, false, true)
		}
	}
	return connect.NewResponse(&v1pb.CancelReminderResponse{Reminder: storeToV1Reminder(updated)}), nil
}

// CompleteReminder marks a DUE reminder completed (one-shot) or reschedules it
// to the next cron fire (recurring), and atomically posts the result as a single
// SYSTEM thread message. Only the owning agent may call this. The store tx is
// the single source of the completion message so it never appears twice.
func (s *CommandService) CompleteReminder(ctx context.Context, req *connect.Request[v1pb.CompleteReminderRequest]) (*connect.Response[v1pb.CompleteReminderResponse], error) {
	msgID, err := parseReminderName(req.Msg.Name)
	if err != nil {
		return nil, err
	}
	r, err := s.store.GetReminder(ctx, msgID)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrReminderNotFound):
			return nil, connect.NewError(connect.CodeNotFound, errors.New("reminder not found"))
		default:
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get reminder"))
		}
	}
	if err := s.requireReminderOwner(ctx, r); err != nil {
		return nil, err
	}
	// The result is posted as a normal agent reply (markdown); the label is a
	// short SYSTEM lifecycle pill. Splitting them keeps long/markdown results
	// readable in the thread while preserving the status event.
	label := fmt.Sprintf("✅ %s completed the reminder", r.AssigneeName)
	nextFireAt := s.nextFireOrNil(r)
	posted, updated, err := s.store.CompleteReminderAndPostNotification(ctx, msgID, req.Msg.Result, label, nextFireAt)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to complete reminder"))
	}
	// The reminder root is a reminder, so its thread replies carry the REMINDER
	// category for every user member (plus THREAD for participants).
	for _, m := range posted {
		s.store.GenerateActivityForMessage(ctx, m, false, true)
	}
	return connect.NewResponse(&v1pb.CompleteReminderResponse{Reminder: storeToV1Reminder(updated)}), nil
}

// FailReminder marks a DUE reminder failed (one-shot) or reschedules it
// (recurring), and atomically posts the error as a SYSTEM thread message. Only
// the owning agent may call this.
func (s *CommandService) FailReminder(ctx context.Context, req *connect.Request[v1pb.FailReminderRequest]) (*connect.Response[v1pb.FailReminderResponse], error) {
	msgID, err := parseReminderName(req.Msg.Name)
	if err != nil {
		return nil, err
	}
	r, err := s.store.GetReminder(ctx, msgID)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrReminderNotFound):
			return nil, connect.NewError(connect.CodeNotFound, errors.New("reminder not found"))
		default:
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get reminder"))
		}
	}
	if err := s.requireReminderOwner(ctx, r); err != nil {
		return nil, err
	}
	label := fmt.Sprintf("❌ %s failed the reminder", r.AssigneeName)
	nextFireAt := s.nextFireOrNil(r)
	posted, updated, err := s.store.FailReminderAndPostNotification(ctx, msgID, req.Msg.Error, label, nextFireAt)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to fail reminder"))
	}
	for _, m := range posted {
		s.store.GenerateActivityForMessage(ctx, m, false, true)
	}
	return connect.NewResponse(&v1pb.FailReminderResponse{Reminder: storeToV1Reminder(updated)}), nil
}

// ListDueReminders returns the DUE reminders owned by the calling agent, for
// the autonomous drain loop to pick up fired work. Agent identity is resolved
// from the auth context.
func (s *CommandService) ListDueReminders(ctx context.Context, _ *connect.Request[v1pb.ListDueRemindersRequest]) (*connect.Response[v1pb.ListDueRemindersResponse], error) {
	agent, ok := GetAgentFromContext(ctx)
	if !ok || agent == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("agent authentication required"))
	}
	reminders, err := s.store.ListDueReminders(ctx, agent.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to list due reminders"))
	}
	out := make([]*v1pb.Reminder, 0, len(reminders))
	for _, r := range reminders {
		out = append(out, storeToV1Reminder(r))
	}
	return connect.NewResponse(&v1pb.ListDueRemindersResponse{Reminders: out}), nil
}

// nextFireOrNil returns the next cron fire time for a recurring reminder, or nil
// for a one-shot reminder (no reschedule). Used by CompleteReminder/FailReminder
// to reschedule recurring reminders in the same tx.
func (*CommandService) nextFireOrNil(r *store.Reminder) *time.Time {
	if r.CronExpr == "" {
		return nil
	}
	next, err := schedule.NextFire(r.CronExpr, r.Tz, time.Now())
	if err != nil {
		slog.Warn("failed to compute next fire for recurring reminder", "messageID", r.MessageID, "cron", r.CronExpr, "error", err)
		return nil
	}
	return &next
}

// resolveFireAt determines a reminder's first fire time. An explicit fire_at
// (when provided and valid) wins and must be in the future; otherwise, for a
// recurring reminder (cronExpr set), the first fire is computed from the cron
// expression starting at now. A one-shot reminder (no cron) requires an
// explicit fire_at. Used by ConvertMessageToReminder.
func resolveFireAt(ts *timestamppb.Timestamp, cronExpr, tz string) (time.Time, error) {
	if ts != nil && ts.IsValid() && ts.AsTime().Unix() > 0 {
		fireAt := ts.AsTime()
		if !fireAt.After(time.Now()) {
			return time.Time{}, connect.NewError(connect.CodeInvalidArgument, errors.New("fire_at must be in the future"))
		}
		return fireAt, nil
	}
	if cronExpr != "" {
		fireAt, err := schedule.NextFire(cronExpr, tz, time.Now())
		if err != nil {
			return time.Time{}, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to compute first fire"))
		}
		return fireAt, nil
	}
	return time.Time{}, connect.NewError(connect.CodeInvalidArgument, errors.New("fire_at is required for a one-shot reminder (or set cron_expr and the manager computes the first fire)"))
}

// postReminderSystemMessage inserts a sender_type=SYSTEM thread reply rooted at
// the reminder's trigger message, bumping the conversation version so the user
// poller and thread view surface it. It does NOT wake agents: system messages
// are excluded from agentRelevantMessageCondition. Best-effort: failures are
// logged, never fatal. Returns the inserted message so the caller can generate
// REMINDER activity for it; returns nil on the empty-content fast path or on
// insert failure (callers treat nil as "no activity to generate").
func (s *CommandService) postReminderSystemMessage(ctx context.Context, convID, rootMsgID uuid.UUID, content string) *store.ChatMessage {
	if content == "" {
		return nil
	}
	msg, _, err := s.store.CreateChatMessageBumpVersion(ctx, &store.ChatMessage{
		ConversationID:      convID,
		PrincipalID:         1, // system bot (seeded principal id 1)
		Role:                1,
		Content:             content,
		SenderType:          store.SenderTypeSystem,
		ThreadRootMessageID: uuid.NullUUID{UUID: rootMsgID, Valid: true},
	})
	if err != nil {
		slog.Warn("failed to post reminder system message", "conversationID", convID, "rootID", rootMsgID, "error", err)
		return nil
	}
	return msg
}
