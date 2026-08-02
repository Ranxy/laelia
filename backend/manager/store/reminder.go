package store

import (
	"context"
	"database/sql"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"
)

// ReminderStatus values mirror the laelia.v1.ReminderStatus enum (status column
// on reminder). Kept as untyped int16 constants on the store side so the
// persistence layer does not depend on the generated proto package, matching
// TaskStatus / SenderType.
const (
	ReminderStatusPending   int16 = 1
	ReminderStatusDue       int16 = 2
	ReminderStatusCompleted int16 = 3
	ReminderStatusCancelled int16 = 4
	ReminderStatusMissed    int16 = 5
	ReminderStatusFailed    int16 = 6
)

// Sentinel errors for reminder mutations. The API layer maps these to connect
// codes (FailedPrecondition / NotFound).
var (
	// ErrReminderNotFound is returned when no reminder row exists for a message id.
	ErrReminderNotFound = errors.New("reminder not found")
	// ErrReminderAlreadyExists is returned by ConvertMessageToReminder when the
	// message is already a reminder.
	ErrReminderAlreadyExists = errors.New("message is already a reminder")
	// ErrReminderNotOwner is returned when the caller is not the reminder's assignee.
	ErrReminderNotOwner = errors.New("reminder is assigned to another agent")
	// ErrReminderInvalidTransition is returned when a mutation targets a reminder
	// not in the required status (e.g. CompleteReminder on a non-DUE reminder,
	// UpdateReminder on a terminal reminder).
	ErrReminderInvalidTransition = errors.New("reminder status transition not allowed")
)

// Reminder is the schedule metadata attached to a root chat_message. The
// chat_message (root) remains the source of truth for the trigger content; this
// row carries the schedule, assignee, and lifecycle state. The thread rooted at
// the trigger message is the discussion channel and where completion/miss
// system messages are posted.
type Reminder struct {
	MessageID          uuid.UUID
	ConversationID     uuid.UUID
	AssigneeAgentID    int
	AssigneeName       string
	AssigneeResourceID string
	TaskContent        string
	FireAt             time.Time
	CronExpr           string // "" = one-shot
	Tz                 string
	Status             int16
	RetryCount         int32
	NextRetryAt        sql.NullTime
	LastAttemptAt      sql.NullTime
	LastFiredAt        sql.NullTime
	LastCompletedAt    sql.NullTime
	Result             string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// reminderColumns is the column list joined with agent for assignee name and
// resource_id, in scan order. cron_expr is COALESCE'd to ” so one-shot
// reminders scan into a plain string.
const reminderColumns = `r.message_id, r.conversation_id, r.assignee_agent_id,
       COALESCE(a.name, ''), COALESCE(a.resource_id, ''),
       r.task_content, r.fire_at, COALESCE(r.cron_expr, ''), r.tz, r.status,
       r.retry_count, r.next_retry_at, r.last_attempt_at, r.last_fired_at,
       r.last_completed_at, r.result, r.created_at, r.updated_at`

func scanReminderRow(row interface {
	Scan(dest ...any) error
}) (*Reminder, error) {
	var r Reminder
	if err := row.Scan(
		&r.MessageID, &r.ConversationID, &r.AssigneeAgentID,
		&r.AssigneeName, &r.AssigneeResourceID,
		&r.TaskContent, &r.FireAt, &r.CronExpr, &r.Tz, &r.Status,
		&r.RetryCount, &r.NextRetryAt, &r.LastAttemptAt, &r.LastFiredAt,
		&r.LastCompletedAt, &r.Result, &r.CreatedAt, &r.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrReminderNotFound
		}
		return nil, errors.Wrapf(err, "failed to scan reminder")
	}
	return &r, nil
}

// ConvertMessageToReminder attaches reminder metadata to an existing top-level
// message and atomically claims it for the calling agent (assignee set in the
// INSERT). The chat_message itself is unchanged; the caller must have already
// validated it is a root in the conversation (IsThreadRoot). No conversation
// version bump — the trigger message already has one. Returns
// ErrReminderAlreadyExists if the message is already a reminder.
func (s *Store) ConvertMessageToReminder(ctx context.Context, msgID, convID uuid.UUID, assigneeAgentID int, taskContent string, fireAt time.Time, cronExpr, tz string) (*Reminder, error) {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to begin tx")
	}
	defer tx.Rollback()

	var exists bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM reminder WHERE message_id = $1)
	`, msgID).Scan(&exists); err != nil {
		return nil, errors.Wrapf(err, "failed to check existing reminder")
	}
	if exists {
		return nil, ErrReminderAlreadyExists
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO reminder (message_id, conversation_id, assignee_agent_id, task_content, fire_at, cron_expr, tz, status)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7, $8)
	`, msgID, convID, assigneeAgentID, taskContent, fireAt, cronExpr, tz, ReminderStatusPending); err != nil {
		// A concurrent convert of the same message wins the unique PK; surface
		// ErrReminderAlreadyExists (no side effects committed).
		if isUniqueViolation(err) {
			return nil, ErrReminderAlreadyExists
		}
		return nil, errors.Wrapf(err, "failed to create reminder row")
	}

	if err := tx.Commit(); err != nil {
		return nil, errors.Wrapf(err, "failed to commit reminder tx")
	}
	return s.GetReminder(ctx, msgID)
}

// GetReminder returns a reminder by its trigger message id, with the assignee
// agent's name and resource_id joined. Returns ErrReminderNotFound when no row
// exists.
func (s *Store) GetReminder(ctx context.Context, msgID uuid.UUID) (*Reminder, error) {
	row := s.GetDB().QueryRowContext(ctx, `SELECT `+reminderColumns+`
		FROM reminder r
		JOIN agent a ON a.id = r.assignee_agent_id
		WHERE r.message_id = $1`, msgID)
	return scanReminderRow(row)
}

// ListReminders returns reminders filtered by owning agent and/or conversation
// and/or status, ordered by next fire (soonest first) then most recently
// updated. agentID==0 and convID==uuid.Nil mean "no filter". When viewer is
// non-nil, results are restricted to reminders whose conversation the viewer is
// a member of, so a non-admin user only sees their own reminders (workspace
// admins and agent callers pass nil). Pagination is offset-based: pageToken is
// the decimal offset; pageSize caps the page.
func (s *Store) ListReminders(ctx context.Context, agentID int, convID uuid.UUID, statusFilter []int16, viewer *ConversationMemberFilter, pageSize int, pageToken string) ([]*Reminder, string, error) {
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 50
	}
	offset, err := strconv.Atoi(pageToken)
	if err != nil || offset < 0 {
		offset = 0
	}

	args := []any{}
	where := "WHERE 1=1"
	idx := 1
	if agentID > 0 {
		where += " AND r.assignee_agent_id = $" + itoa(idx)
		args = append(args, agentID)
		idx++
	}
	if convID != uuid.Nil {
		where += " AND r.conversation_id = $" + itoa(idx)
		args = append(args, convID)
		idx++
	}
	if len(statusFilter) > 0 {
		where += " AND r.status = ANY($" + itoa(idx) + ")"
		args = append(args, statusFilter)
		idx++
	}
	if viewer != nil {
		where += " AND EXISTS (SELECT 1 FROM conversation_member_meta cmv WHERE cmv.conversation_id = r.conversation_id AND cmv.member_type = $" + itoa(idx) + " AND cmv.member_id = $" + itoa(idx+1) + ")"
		args = append(args, viewer.MemberType, viewer.MemberID)
		idx += 2
	}
	args = append(args, pageSize, offset)
	query := `SELECT ` + reminderColumns + `
		FROM reminder r
		JOIN agent a ON a.id = r.assignee_agent_id
		` + where + `
		ORDER BY (CASE WHEN r.status = 1 THEN 0 ELSE 1 END), r.fire_at ASC, r.updated_at DESC
		LIMIT $` + itoa(idx) + ` OFFSET $` + itoa(idx+1)

	rows, err := s.GetDB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, "", errors.Wrapf(err, "failed to list reminders")
	}
	defer rows.Close()

	var reminders []*Reminder
	for rows.Next() {
		r, scanErr := scanReminderRow(rows)
		if scanErr != nil {
			return nil, "", scanErr
		}
		reminders = append(reminders, r)
	}
	if err := rows.Err(); err != nil {
		return nil, "", errors.Wrapf(err, "failed to iterate reminders")
	}

	nextToken := ""
	if len(reminders) == pageSize {
		nextToken = strconv.Itoa(offset + pageSize)
	}
	return reminders, nextToken, nil
}

// ListDueReminders returns the DUE reminders owned by an agent, ordered by
// fire_at ascending so the oldest fire is processed first. Used by the agent
// drain loop each session.
func (s *Store) ListDueReminders(ctx context.Context, agentID int) ([]*Reminder, error) {
	rows, err := s.GetDB().QueryContext(ctx, `SELECT `+reminderColumns+`
		FROM reminder r
		JOIN agent a ON a.id = r.assignee_agent_id
		WHERE r.assignee_agent_id = $1 AND r.status = $2
		ORDER BY r.fire_at ASC`, agentID, ReminderStatusDue)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to list due reminders")
	}
	defer rows.Close()
	var reminders []*Reminder
	for rows.Next() {
		r, scanErr := scanReminderRow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		reminders = append(reminders, r)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrapf(err, "failed to iterate due reminders")
	}
	return reminders, nil
}

// HasDueReminders reports whether the agent owns at least one DUE reminder. The
// drain loop's BeginSession gate OR-folds this with HasUpdates so a fired
// reminder drives a session.
func (s *Store) HasDueReminders(ctx context.Context, agentID int) (bool, error) {
	var exists bool
	err := s.GetDB().QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM reminder WHERE assignee_agent_id = $1 AND status = $2)
	`, agentID, ReminderStatusDue).Scan(&exists)
	if err != nil {
		return false, errors.Wrapf(err, "failed to check due reminders")
	}
	return exists, nil
}

// ListDuePending returns PENDING reminders whose fire_at has passed (fire_at <=
// now), ordered by fire_at ascending. It is the scheduler's due-scan window.
func (s *Store) ListDuePending(ctx context.Context, now time.Time) ([]*Reminder, error) {
	rows, err := s.GetDB().QueryContext(ctx, `SELECT `+reminderColumns+`
		FROM reminder r
		JOIN agent a ON a.id = r.assignee_agent_id
		WHERE r.status = $1 AND r.fire_at <= $2
		ORDER BY r.fire_at ASC`, ReminderStatusPending, now)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to list due-pending reminders")
	}
	defer rows.Close()
	var reminders []*Reminder
	for rows.Next() {
		r, scanErr := scanReminderRow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		reminders = append(reminders, r)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrapf(err, "failed to iterate due-pending reminders")
	}
	return reminders, nil
}

// ListDueRetrying returns DUE reminders with a scheduled retry whose
// next_retry_at has passed (next_retry_at <= now). It is the scheduler's
// retry-scan window for reminders whose agent was offline at fire time.
func (s *Store) ListDueRetrying(ctx context.Context, now time.Time) ([]*Reminder, error) {
	rows, err := s.GetDB().QueryContext(ctx, `SELECT `+reminderColumns+`
		FROM reminder r
		JOIN agent a ON a.id = r.assignee_agent_id
		WHERE r.status = $1 AND r.next_retry_at IS NOT NULL AND r.next_retry_at <= $2
		ORDER BY r.next_retry_at ASC`, ReminderStatusDue, now)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to list due-retrying reminders")
	}
	defer rows.Close()
	var reminders []*Reminder
	for rows.Next() {
		r, scanErr := scanReminderRow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		reminders = append(reminders, r)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrapf(err, "failed to iterate due-retrying reminders")
	}
	return reminders, nil
}

// MarkDue transitions a PENDING reminder to DUE, recording the fire time and
// resetting the retry counters. Idempotent: a row already DUE is left as-is
// (rows affected 0 is not an error — the scheduler may re-scan).
func (s *Store) MarkDue(ctx context.Context, msgID uuid.UUID, firedAt time.Time) error {
	_, err := s.GetDB().ExecContext(ctx, `
		UPDATE reminder
		   SET status = $2, last_fired_at = $3, retry_count = 0,
		       next_retry_at = NULL, last_attempt_at = NULL, updated_at = now()
		 WHERE message_id = $1 AND status = $4
	`, msgID, ReminderStatusDue, firedAt, ReminderStatusPending)
	if err != nil {
		return errors.Wrapf(err, "failed to mark reminder due")
	}
	return nil
}

// SetRetry schedules the next offline-retry attempt for a DUE reminder, recording
// the attempt count and the attempt time. The scheduler advances retry_count
// through the backoff schedule [5s,10s,20s,30s,60s].
func (s *Store) SetRetry(ctx context.Context, msgID uuid.UUID, retryCount int32, nextRetryAt, attemptAt time.Time) error {
	_, err := s.GetDB().ExecContext(ctx, `
		UPDATE reminder
		   SET retry_count = $2, next_retry_at = $3, last_attempt_at = $4, updated_at = now()
		 WHERE message_id = $1 AND status = $5
	`, msgID, retryCount, nextRetryAt, attemptAt, ReminderStatusDue)
	if err != nil {
		return errors.Wrapf(err, "failed to set reminder retry")
	}
	return nil
}

// ClearRetry clears the retry timer for a DUE reminder (the agent connected and
// was woken). The reminder stays DUE until the agent completes it.
func (s *Store) ClearRetry(ctx context.Context, msgID uuid.UUID) error {
	_, err := s.GetDB().ExecContext(ctx, `
		UPDATE reminder SET next_retry_at = NULL, updated_at = now()
		 WHERE message_id = $1 AND status = $2
	`, msgID, ReminderStatusDue)
	if err != nil {
		return errors.Wrapf(err, "failed to clear reminder retry")
	}
	return nil
}

// UpdateReminderFields edits the schedule and/or task content of a reminder.
// Editing a DUE or MISSED reminder resets it to PENDING with the new schedule
// (clearing retry state). Terminal statuses (COMPLETED/CANCELLED/FAILED) cannot
// be edited → ErrReminderInvalidTransition. fireAt is the new next fire time.
func (s *Store) UpdateReminderFields(ctx context.Context, msgID uuid.UUID, fireAt time.Time, cronExpr, tz, taskContent string) (*Reminder, error) {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to begin tx")
	}
	defer tx.Rollback()

	var status int16
	if err := tx.QueryRowContext(ctx, `SELECT status FROM reminder WHERE message_id = $1`, msgID).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrReminderNotFound
		}
		return nil, errors.Wrapf(err, "failed to read reminder status")
	}
	switch status {
	case ReminderStatusPending, ReminderStatusDue, ReminderStatusMissed:
		// editable; DUE/MISSED reset to PENDING below
	default:
		return nil, ErrReminderInvalidTransition
	}

	newStatus := status
	if status == ReminderStatusDue || status == ReminderStatusMissed {
		newStatus = ReminderStatusPending
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE reminder
		   SET fire_at = $2, cron_expr = NULLIF($3, ''), tz = $4, task_content = $5,
		       status = $6, retry_count = 0, next_retry_at = NULL, last_attempt_at = NULL,
		       updated_at = now()
		 WHERE message_id = $1
	`, msgID, fireAt, cronExpr, tz, taskContent, newStatus); err != nil {
		return nil, errors.Wrapf(err, "failed to update reminder")
	}
	if err := tx.Commit(); err != nil {
		return nil, errors.Wrapf(err, "failed to commit reminder update")
	}
	return s.GetReminder(ctx, msgID)
}

// CancelReminder cancels a reminder. A PENDING, DUE, or MISSED reminder may be
// cancelled; terminal statuses are a no-op (return current state). Returns
// ErrReminderNotFound when no row exists.
func (s *Store) CancelReminder(ctx context.Context, msgID uuid.UUID) (*Reminder, error) {
	res, err := s.GetDB().ExecContext(ctx, `
		UPDATE reminder SET status = $2, next_retry_at = NULL, updated_at = now()
		 WHERE message_id = $1 AND status = ANY($3)
	`, msgID, ReminderStatusCancelled, []int16{ReminderStatusPending, ReminderStatusDue, ReminderStatusMissed})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to cancel reminder")
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read cancel result")
	}
	if rows == 0 {
		// Either it does not exist, or it is already terminal. Distinguish so the
		// caller can return NotFound vs. the current (terminal) state.
		r, err := s.GetReminder(ctx, msgID)
		if err != nil {
			return nil, err
		}
		return r, nil
	}
	return s.GetReminder(ctx, msgID)
}

// completeReminderTx is the shared tx body for CompleteReminder and FailReminder:
// it flips a DUE reminder to finalStatus (or back to PENDING with nextFireAt for
// recurring), bumps the conversation version, and inserts TWO thread replies —
// a short SYSTEM lifecycle pill (label, e.g. "✅ Jane completed the reminder")
// and a normal AGENT message carrying the result/error text as markdown content
// so it renders like any other agent reply (avatar, name, markdown) instead of
// being jammed into a system notification line. All atomic. Idempotent: if the
// reminder is not DUE (already completed/cancelled, or a duplicate complete), no
// row is updated and no messages are posted; the current state is returned. The
// caller computes nextFireAt (nil for one-shot or no reschedule) and the label.
// The tx posts directly via createChatMessageInTx and does NOT call NotifyWake,
// so posting never wakes any agent (the owner consumes its own message in the
// same drain session via message check / thread check, IsOwn → ignored → ack).
//
// Returns the inserted thread messages (the SYSTEM pill and the agent result
// reply) so the caller can generate REMINDER activity for each. The returned
// ChatMessages carry only the fields GenerateActivityForMessage needs
// (ID/ConversationID/PrincipalID/ThreadRootMessageID/RoomVersion/SenderType);
// they are NOT full re-reads and must not be used for rendering. On the
// idempotent no-op path (reminder no longer DUE) no messages are posted and the
// slice is nil.
func (s *Store) completeReminderTx(ctx context.Context, msgID uuid.UUID, result, label string, finalStatus int16, nextFireAt *time.Time) ([]*ChatMessage, *Reminder, error) {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "failed to begin tx")
	}
	defer tx.Rollback()

	var convID uuid.UUID
	var assigneeAgentID int32
	if nextFireAt != nil {
		// Recurring: reset to PENDING with the next cron fire.
		if err := tx.QueryRowContext(ctx, `
			UPDATE reminder
			   SET last_completed_at = now(), result = $2,
			       retry_count = 0, next_retry_at = NULL, last_attempt_at = NULL,
			       status = $5, fire_at = $3, updated_at = now()
			 WHERE message_id = $1 AND status = $4
			RETURNING conversation_id, assignee_agent_id
		`, msgID, result, *nextFireAt, ReminderStatusDue, ReminderStatusPending).Scan(&convID, &assigneeAgentID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				r, getErr := s.GetReminder(ctx, msgID)
				return nil, r, getErr
			}
			return nil, nil, errors.Wrapf(err, "failed to complete reminder (recurring)")
		}
	} else {
		// One-shot: terminal finalStatus.
		if err := tx.QueryRowContext(ctx, `
			UPDATE reminder
			   SET last_completed_at = now(), result = $2,
			       retry_count = 0, next_retry_at = NULL, last_attempt_at = NULL,
			       status = $3, updated_at = now()
			 WHERE message_id = $1 AND status = $4
			RETURNING conversation_id, assignee_agent_id
		`, msgID, result, finalStatus, ReminderStatusDue).Scan(&convID, &assigneeAgentID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				r, getErr := s.GetReminder(ctx, msgID)
				return nil, r, getErr
			}
			return nil, nil, errors.Wrapf(err, "failed to complete reminder")
		}
	}

	// The conversation owner's principal anchors the agent message (mirrors
	// PostMessage, which uses conv.OwnerID as principal_id for agent replies).
	var principalID int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(owner_id, 1) FROM conversation WHERE id = $1`, convID).Scan(&principalID); err != nil {
		return nil, nil, errors.Wrapf(err, "failed to load conversation owner")
	}

	threadRoot := uuid.NullUUID{UUID: msgID, Valid: true}

	// First message: the short SYSTEM lifecycle pill (e.g. "✅ … completed").
	var pillVersion int64
	if err := tx.QueryRowContext(ctx, conversationVersionBumpSQL, convID).Scan(&pillVersion); err != nil {
		return nil, nil, errors.Wrapf(err, "failed to bump conversation version")
	}
	pillID, _, err := createChatMessageInTx(ctx, tx, &ChatMessage{
		ConversationID:      convID,
		PrincipalID:         1, // system bot (seeded principal id 1)
		Role:                1,
		Content:             label,
		SenderType:          SenderTypeSystem,
		ThreadRootMessageID: threadRoot,
	}, pillVersion)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "failed to post reminder lifecycle message")
	}

	// Second message: the result/error as a normal agent reply (markdown,
	// avatar, name) so it reads like a conversational turn, not a notification.
	var resultVersion int64
	if err := tx.QueryRowContext(ctx, conversationVersionBumpSQL, convID).Scan(&resultVersion); err != nil {
		return nil, nil, errors.Wrapf(err, "failed to bump conversation version")
	}
	resultID, _, err := createChatMessageInTx(ctx, tx, &ChatMessage{
		ConversationID:      convID,
		PrincipalID:         principalID,
		SenderAgentID:       sql.NullInt32{Int32: assigneeAgentID, Valid: assigneeAgentID > 0},
		Role:                2,
		Content:             result,
		SenderType:          SenderTypeAgent,
		ThreadRootMessageID: threadRoot,
	}, resultVersion)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "failed to post reminder result message")
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, errors.Wrapf(err, "failed to commit reminder completion tx")
	}
	posted := []*ChatMessage{
		{ID: pillID, ConversationID: convID, PrincipalID: 1, SenderType: SenderTypeSystem, ThreadRootMessageID: threadRoot, RoomVersion: pillVersion},
		{ID: resultID, ConversationID: convID, PrincipalID: principalID, SenderAgentID: sql.NullInt32{Int32: assigneeAgentID, Valid: assigneeAgentID > 0}, SenderType: SenderTypeAgent, ThreadRootMessageID: threadRoot, RoomVersion: resultVersion},
	}
	r, getErr := s.GetReminder(ctx, msgID)
	return posted, r, getErr
}

// CompleteReminderAndPostNotification marks a DUE reminder COMPLETED (one-shot)
// or reschedules it to the next cron fire (recurring, nextFireAt non-nil), and
// atomically posts a SYSTEM lifecycle pill (label) plus the result as a normal
// agent thread reply. Idempotent: a duplicate call on a non-DUE reminder returns
// the current state without posting again (posted is nil).
func (s *Store) CompleteReminderAndPostNotification(ctx context.Context, msgID uuid.UUID, result, label string, nextFireAt *time.Time) ([]*ChatMessage, *Reminder, error) {
	return s.completeReminderTx(ctx, msgID, result, label, ReminderStatusCompleted, nextFireAt)
}

// FailReminderAndPostNotification marks a DUE reminder FAILED (one-shot) or
// reschedules it (recurring), and atomically posts a SYSTEM lifecycle pill
// (label) plus the error as a normal agent thread reply. Idempotent like
// CompleteReminderAndPostNotification.
func (s *Store) FailReminderAndPostNotification(ctx context.Context, msgID uuid.UUID, errMsg, label string, nextFireAt *time.Time) ([]*ChatMessage, *Reminder, error) {
	return s.completeReminderTx(ctx, msgID, errMsg, label, ReminderStatusFailed, nextFireAt)
}

// MarkMissedAndPostNotification is called by the scheduler when the offline-retry
// backoff is exhausted. One-shot reminders become terminal MISSED; recurring
// reminders reschedule to the next cron fire (nextFireAt non-nil). A single
// SYSTEM thread message records the miss and the retry count. Idempotent: on
// the no-op path (reminder no longer DUE) no message is posted and posted is nil.
// Returns the inserted message so the scheduler can generate REMINDER activity.
func (s *Store) MarkMissedAndPostNotification(ctx context.Context, msgID uuid.UUID, nextFireAt *time.Time) ([]*ChatMessage, *Reminder, error) {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "failed to begin tx")
	}
	defer tx.Rollback()

	var convID uuid.UUID
	var retryCount int32
	if nextFireAt != nil {
		if err := tx.QueryRowContext(ctx, `
			UPDATE reminder
			   SET last_attempt_at = now(), retry_count = 0, next_retry_at = NULL,
			       status = $4, fire_at = $2, updated_at = now()
			 WHERE message_id = $1 AND status = $3
			RETURNING conversation_id, retry_count
		`, msgID, *nextFireAt, ReminderStatusDue, ReminderStatusPending).Scan(&convID, &retryCount); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				r, getErr := s.GetReminder(ctx, msgID)
				return nil, r, getErr
			}
			return nil, nil, errors.Wrapf(err, "failed to mark reminder missed (recurring)")
		}
	} else {
		if err := tx.QueryRowContext(ctx, `
			UPDATE reminder
			   SET last_attempt_at = now(), retry_count = 0, next_retry_at = NULL,
			       status = $3, updated_at = now()
			 WHERE message_id = $1 AND status = $4
			RETURNING conversation_id, retry_count
		`, msgID, ReminderStatusMissed, ReminderStatusDue).Scan(&convID, &retryCount); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				r, getErr := s.GetReminder(ctx, msgID)
				return nil, r, getErr
			}
			return nil, nil, errors.Wrapf(err, "failed to mark reminder missed")
		}
	}

	var newVersion int64
	if err := tx.QueryRowContext(ctx, conversationVersionBumpSQL, convID).Scan(&newVersion); err != nil {
		return nil, nil, errors.Wrapf(err, "failed to bump conversation version")
	}

	content := "⏰ Reminder missed after " + itoa(int(retryCount)) + " delivery retries (agent offline)"
	threadRoot := uuid.NullUUID{UUID: msgID, Valid: true}
	missedID, _, err := createChatMessageInTx(ctx, tx, &ChatMessage{
		ConversationID:      convID,
		PrincipalID:         1,
		Role:                1,
		Content:             content,
		SenderType:          SenderTypeSystem,
		ThreadRootMessageID: threadRoot,
	}, newVersion)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "failed to post reminder missed message")
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, errors.Wrapf(err, "failed to commit reminder missed tx")
	}
	posted := []*ChatMessage{
		{ID: missedID, ConversationID: convID, PrincipalID: 1, SenderType: SenderTypeSystem, ThreadRootMessageID: threadRoot, RoomVersion: newVersion},
	}
	r, getErr := s.GetReminder(ctx, msgID)
	return posted, r, getErr
}
