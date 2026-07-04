package store

import (
	"context"
	"database/sql"

	"github.com/google/uuid"
	"github.com/pkg/errors"
)

// TaskStatus values mirror the laelia.v1.TaskStatus enum (status column on
// task). Kept as untyped int16 constants on the store side so the persistence
// layer does not depend on the generated proto package, matching SenderType.
const (
	TaskStatusTodo       int16 = 1
	TaskStatusInProgress int16 = 2
	TaskStatusInReview   int16 = 3
	TaskStatusDone       int16 = 4
)

// Sentinel errors for task mutations. The API layer maps these to connect
// codes (FailedPrecondition / NotFound / PermissionDenied).
var (
	// ErrTaskNotFound is returned when no task row exists for a message id.
	ErrTaskNotFound = errors.New("task not found")
	// ErrTaskAlreadyExists is returned by ConvertMessageToTask when the message
	// is already a task.
	ErrTaskAlreadyExists = errors.New("message is already a task")
	// ErrTaskNotClaimable is returned by ClaimTask when the task is not in TODO
	// (already claimed, in review, or done).
	ErrTaskNotClaimable = errors.New("task is already claimed or not in todo")
	// ErrTaskNotOwner is returned by UnclaimTask / UpdateTaskStatus when the
	// caller is not the task's assignee.
	ErrTaskNotOwner = errors.New("task is assigned to another agent")
	// ErrTaskInvalidTransition is returned by UpdateTaskStatus when the
	// requested status transition is not allowed from the current status.
	ErrTaskInvalidTransition = errors.New("task status transition not allowed")
)

// TaskInfo is the join shape attached to a ChatMessage that is a task. It is
// populated by fillTaskInfo for root messages; nil for non-task messages.
type TaskInfo struct {
	TaskNumber         int32
	Status             int16
	AssigneeAgentID    sql.NullInt32
	AssigneeName       string
	AssigneeResourceID string
}

// CreateTaskMessageBumpVersion atomically bumps the conversation's room version
// and per-conversation task number, inserts a top-level chat_message, and
// inserts a task row (status TODO, no assignee) for it — all in one
// transaction. It is the shared entry point for SendMessage(as_task=true) and
// agent CreateTask; the caller sets SenderType / PrincipalID / SenderAgentID
// to distinguish the two. Returns the created message (with TaskInfo populated)
// and the new conversation version.
func (s *Store) CreateTaskMessageBumpVersion(ctx context.Context, msg *ChatMessage) (*ChatMessage, int64, error) {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, 0, errors.Wrapf(err, "failed to begin tx")
	}
	defer tx.Rollback()

	// Bump room version and task number together so the message and its task
	// row always share a consistent, contiguous number (a rolled-back task
	// creation rolls back the sequence increment too).
	var newVersion int64
	var taskNumber int32
	if err := tx.QueryRowContext(ctx, `
		UPDATE conversation
		   SET version = version + 1,
		       next_task_number = next_task_number + 1,
		       updated_at = now()
		 WHERE id = $1
		RETURNING version, next_task_number - 1
	`, msg.ConversationID).Scan(&newVersion, &taskNumber); err != nil {
		return nil, 0, errors.Wrapf(err, "failed to bump conversation version and task number")
	}

	id, createdAt, err := createChatMessageInTx(ctx, tx, msg, newVersion)
	if err != nil {
		return nil, 0, err
	}

	if err := createTaskRowInTx(ctx, tx, id, msg.ConversationID, taskNumber, TaskStatusTodo, sql.NullInt32{}); err != nil {
		return nil, 0, err
	}

	if err := tx.Commit(); err != nil {
		return nil, 0, errors.Wrapf(err, "failed to commit task message tx")
	}

	return &ChatMessage{
		ID:                  id,
		ConversationID:      msg.ConversationID,
		PrincipalID:         msg.PrincipalID,
		PrincipalName:       msg.PrincipalName,
		SenderAgentID:       msg.SenderAgentID,
		AgentResourceID:     msg.AgentResourceID,
		Role:                msg.Role,
		Content:             msg.Content,
		CommandID:           msg.CommandID,
		CreatedAt:           createdAt,
		RoomVersion:         newVersion,
		SenderType:          msg.SenderType,
		Mentions:            msg.Mentions,
		Attachments:         msg.Attachments,
		ThreadRootMessageID: msg.ThreadRootMessageID,
		TaskInfo: &TaskInfo{
			TaskNumber: taskNumber,
			Status:     TaskStatusTodo,
		},
	}, newVersion, nil
}

// createTaskRowInTx inserts a task row within an existing transaction. Used by
// CreateTaskMessageBumpVersion (new message) and ConvertMessageToTask (existing
// message, after bumping next_task_number separately).
func createTaskRowInTx(ctx context.Context, tx *sql.Tx, msgID, convID uuid.UUID, taskNumber int32, status int16, assignee sql.NullInt32) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO task (message_id, conversation_id, task_number, status, assignee_agent_id)
		VALUES ($1, $2, $3, $4, $5)
	`, msgID, convID, taskNumber, status, assignee)
	if err != nil {
		return errors.Wrapf(err, "failed to create task row")
	}
	return nil
}

// ConvertMessageToTask attaches task metadata (number, status TODO, no
// assignee) to an existing top-level message. The chat_message itself is
// unchanged. Returns ErrTaskAlreadyExists if the message is already a task.
// The caller must have already validated the message is a root in the
// conversation (IsThreadRoot). On success the returned ChatMessage is re-read
// with TaskInfo populated.
func (s *Store) ConvertMessageToTask(ctx context.Context, msgID, convID uuid.UUID) (*ChatMessage, error) {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to begin tx")
	}
	defer tx.Rollback()

	// Fast path: skip the number bump entirely if a task row already exists.
	var exists bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM task WHERE message_id = $1)
	`, msgID).Scan(&exists); err != nil {
		return nil, errors.Wrapf(err, "failed to check existing task")
	}
	if exists {
		return nil, ErrTaskAlreadyExists
	}

	var taskNumber int32
	if err := tx.QueryRowContext(ctx, `
		UPDATE conversation SET next_task_number = next_task_number + 1
		WHERE id = $1
		RETURNING next_task_number - 1
	`, convID).Scan(&taskNumber); err != nil {
		return nil, errors.Wrapf(err, "failed to bump task number")
	}

	if err := createTaskRowInTx(ctx, tx, msgID, convID, taskNumber, TaskStatusTodo, sql.NullInt32{}); err != nil {
		// A concurrent convert of the same message wins the unique PK on
		// task.message_id; the INSERT fails, the tx aborts, and we surface
		// ErrTaskAlreadyExists (the next_task_number bump rolls back, so no
		// gap in the sequence).
		if isUniqueViolation(err) {
			return nil, ErrTaskAlreadyExists
		}
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, errors.Wrapf(err, "failed to commit convert task tx")
	}

	return s.GetTaskMessage(ctx, msgID)
}

// ClaimTask atomically transitions a TODO task to IN_PROGRESS and assigns it to
// the calling agent. The atomic UPDATE ... WHERE status=TODO AND assignee IS
// NULL is race-free: concurrent claims on the same task serialize on the row
// lock, only one affects a row, the others get sql.ErrNoRows. Returns
// ErrTaskNotClaimable when the task is already claimed or not in TODO. On
// success the returned ChatMessage is re-read with TaskInfo populated.
func (s *Store) ClaimTask(ctx context.Context, msgID, convID uuid.UUID, agentID int) (*ChatMessage, error) {
	res, err := s.GetDB().ExecContext(ctx, `
		UPDATE task
		   SET status = $1, assignee_agent_id = $2, updated_at = now()
		 WHERE message_id = $3 AND conversation_id = $4 AND status = $5 AND assignee_agent_id IS NULL
	`, TaskStatusInProgress, agentID, msgID, convID, TaskStatusTodo)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to claim task")
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read claim result")
	}
	if rows == 0 {
		return nil, ErrTaskNotClaimable
	}
	return s.GetTaskMessage(ctx, msgID)
}

// UnclaimTask releases the calling agent's claim on a task it owns, setting it
// back to TODO so another agent may claim it. DONE is terminal and cannot be
// unclaimed. Returns ErrTaskNotOwner when the caller is not the assignee or the
// task is not IN_PROGRESS. On success the returned ChatMessage is re-read.
func (s *Store) UnclaimTask(ctx context.Context, msgID uuid.UUID, agentID int) (*ChatMessage, error) {
	res, err := s.GetDB().ExecContext(ctx, `
		UPDATE task
		   SET status = $1, assignee_agent_id = NULL, updated_at = now()
		 WHERE message_id = $2 AND assignee_agent_id = $3 AND status = $4
	`, TaskStatusTodo, msgID, agentID, TaskStatusInProgress)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to unclaim task")
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read unclaim result")
	}
	if rows == 0 {
		return nil, ErrTaskNotOwner
	}
	return s.GetTaskMessage(ctx, msgID)
}

// UpdateTaskStatus advances a task's status. Only IN_PROGRESS -> IN_REVIEW and
// IN_REVIEW -> DONE are allowed here; TODO -> IN_PROGRESS is performed by
// ClaimTask. The caller must be the assignee. Returns ErrTaskInvalidTransition
// for an unsupported target, and ErrTaskNotOwner when the caller is not the
// assignee or the task is not in the required predecessor status. On success
// the returned ChatMessage is re-read with TaskInfo populated.
func (s *Store) UpdateTaskStatus(ctx context.Context, msgID uuid.UUID, agentID int, target int16) (*ChatMessage, error) {
	requiredCurrent, ok := taskStatusPredecessor(target)
	if !ok {
		return nil, ErrTaskInvalidTransition
	}
	var stmt string
	switch target {
	case TaskStatusInReview:
		stmt = `UPDATE task SET status = $1, updated_at = now()
			WHERE message_id = $2 AND assignee_agent_id = $3 AND status = $4`
	case TaskStatusDone:
		stmt = `UPDATE task SET status = $1, updated_at = now(), completed_at = now()
			WHERE message_id = $2 AND assignee_agent_id = $3 AND status = $4`
	default:
		return nil, ErrTaskInvalidTransition
	}
	res, err := s.GetDB().ExecContext(ctx, stmt, target, msgID, agentID, requiredCurrent)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to update task status")
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read task status update result")
	}
	if rows == 0 {
		// Distinguish "not the owner" from "wrong status": a separate read of
		// the row tells the caller why, but the mutation is atomic so the read
		// is best-effort. The API layer maps either to FailedPrecondition.
		return nil, ErrTaskNotOwner
	}
	return s.GetTaskMessage(ctx, msgID)
}

// taskStatusPredecessor returns the status a task must be in to advance to
// target via UpdateTaskStatus, and whether target is a valid UpdateTaskStatus
// target at all.
func taskStatusPredecessor(target int16) (int16, bool) {
	switch target {
	case TaskStatusInReview:
		return TaskStatusInProgress, true
	case TaskStatusDone:
		return TaskStatusInReview, true
	default:
		return 0, false
	}
}

// GetTaskMessage returns a chat_message by id with TaskInfo (and
// thread_reply_count) populated, for task mutation handlers to return the
// updated state. Returns ErrTaskNotFound when the message has no task row.
func (s *Store) GetTaskMessage(ctx context.Context, msgID uuid.UUID) (*ChatMessage, error) {
	row := s.GetDB().QueryRowContext(ctx, `SELECT `+chatMessageColumns+`
		FROM chat_message cm
		JOIN principal p ON p.id = cm.principal_id
		LEFT JOIN agent a ON a.id = cm.sender_agent_id
		WHERE cm.id = $1`, msgID)
	msg, err := scanChatMessageRow(row)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get task message")
	}
	if err := s.fillThreadReplyCounts(ctx, []*ChatMessage{msg}); err != nil {
		return nil, err
	}
	if err := s.fillTaskInfo(ctx, []*ChatMessage{msg}); err != nil {
		return nil, err
	}
	if msg.TaskInfo == nil {
		return nil, ErrTaskNotFound
	}
	return msg, nil
}

// ListTasks returns the task board for a conversation: every root message that
// has a task row, ordered by task_number ascending, with TaskInfo (and
// thread_reply_count) populated. statusFilter, when non-empty, restricts the
// result to the given statuses.
func (s *Store) ListTasks(ctx context.Context, convID uuid.UUID, statusFilter []int16) ([]*ChatMessage, error) {
	args := []any{convID}
	where := ` AND cm.thread_root_message_id IS NULL`
	if len(statusFilter) > 0 {
		where += ` AND t.status = ANY($2)`
		args = append(args, statusFilter)
	}
	rows, err := s.GetDB().QueryContext(ctx, `SELECT `+chatMessageColumns+`
		FROM chat_message cm
		JOIN principal p ON p.id = cm.principal_id
		LEFT JOIN agent a ON a.id = cm.sender_agent_id
		JOIN task t ON t.message_id = cm.id
		WHERE cm.conversation_id = $1`+where+`
		ORDER BY t.task_number ASC`, args...)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to list tasks")
	}
	defer rows.Close()
	var msgs []*ChatMessage
	for rows.Next() {
		msg, scanErr := scanChatMessageRow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		msgs = append(msgs, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrapf(err, "failed to iterate tasks")
	}
	if err := s.fillThreadReplyCounts(ctx, msgs); err != nil {
		return nil, err
	}
	if err := s.fillTaskInfo(ctx, msgs); err != nil {
		return nil, err
	}
	return msgs, nil
}

// fillTaskInfo populates TaskInfo on each root message in msgs by joining the
// task table (and the assignee agent) for the page's root ids. Thread replies
// keep TaskInfo nil. One grouped query covers the page; a nil/empty input is a
// no-op. Mirrors fillThreadReplyCounts.
func (s *Store) fillTaskInfo(ctx context.Context, msgs []*ChatMessage) error {
	var roots []uuid.UUID
	for _, m := range msgs {
		if m == nil || m.ThreadRootMessageID.Valid {
			continue
		}
		roots = append(roots, m.ID)
	}
	if len(roots) == 0 {
		return nil
	}
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT t.message_id, t.task_number, t.status, t.assignee_agent_id,
		       COALESCE(a.name, ''), COALESCE(a.resource_id, '')
		FROM task t
		LEFT JOIN agent a ON a.id = t.assignee_agent_id
		WHERE t.message_id = ANY($1)
	`, roots)
	if err != nil {
		return errors.Wrapf(err, "failed to query task info")
	}
	defer rows.Close()
	info := make(map[uuid.UUID]*TaskInfo, len(roots))
	for rows.Next() {
		var (
			msgID      uuid.UUID
			ti         TaskInfo
			assigneeID sql.NullInt32
		)
		if err := rows.Scan(&msgID, &ti.TaskNumber, &ti.Status, &assigneeID, &ti.AssigneeName, &ti.AssigneeResourceID); err != nil {
			return errors.Wrapf(err, "failed to scan task info")
		}
		ti.AssigneeAgentID = assigneeID
		info[msgID] = &ti
	}
	if err := rows.Err(); err != nil {
		return errors.Wrapf(err, "failed to iterate task info")
	}
	for _, m := range msgs {
		if m == nil || m.ThreadRootMessageID.Valid {
			continue
		}
		if ti, ok := info[m.ID]; ok {
			m.TaskInfo = ti
		}
	}
	return nil
}
