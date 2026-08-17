package store

import (
	"context"
	"database/sql"
	"strconv"

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
	// ErrTaskNotOwner is returned by UnclaimTask when the caller is not the
	// task's assignee.
	ErrTaskNotOwner = errors.New("task is assigned to another agent")
	// ErrTaskInvalidTransition is returned by UpdateTaskStatus when the
	// requested status is not a valid TaskStatus.
	ErrTaskInvalidTransition = errors.New("task status transition not allowed")
	// ErrTaskAssigneeNotMember is returned by AssignTask when the target member
	// is not a member of the task's conversation.
	ErrTaskAssigneeNotMember = errors.New("assignee is not a conversation member")
)

// TaskInfo is the join shape attached to a ChatMessage that is a task. It is
// populated by fillTaskInfo for root messages; nil for non-task messages.
type TaskInfo struct {
	TaskNumber         int32
	Status             int16
	AssigneeAgentID    sql.NullInt32
	AssigneeUserID     sql.NullInt32
	AssigneeType       int16
	AssigneeName       string
	AssigneeResourceID string
}

// RootMessageKinds reports whether a message is the root of a task and/or a
// reminder. Used by activity generation to decide whether a new message (or a
// thread reply rooted at this message) carries the TASK / REMINDER category.
// rootID is the message to check: for a thread reply it is the thread root, for
// a top-level message it is the message itself.
func (s *Store) RootMessageKinds(ctx context.Context, rootID uuid.UUID) (isTask, isReminder bool, err error) {
	err = s.GetDB().QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM task WHERE message_id = $1),
		       EXISTS (SELECT 1 FROM reminder WHERE message_id = $1)
	`, rootID).Scan(&isTask, &isReminder)
	if err != nil {
		return false, false, errors.Wrapf(err, "failed to check root message kinds")
	}
	return isTask, isReminder, nil
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

	// Wake long-polling readers (the frontend chat watcher) so they return as
	// soon as the new task message is visible instead of sleeping the full
	// timeout.
	if s.roomNotifier != nil {
		s.roomNotifier.NotifyConversation(msg.ConversationID)
	}

	return &ChatMessage{
		ID:                  id,
		ConversationID:      msg.ConversationID,
		PrincipalID:         msg.PrincipalID,
		PrincipalName:       msg.PrincipalName,
		PrincipalHandle:     msg.PrincipalHandle,
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
		   SET status = $1, assignee_agent_id = $2, assignee_type = 2, updated_at = now()
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
		   SET status = $1, assignee_agent_id = NULL, assignee_type = NULL, updated_at = now()
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

// UpdateTaskStatus moves a task to any of the four statuses. Any channel member
// may call it; there is no transition restriction. Setting DONE closes the task
// (sets completed_at); moving out of DONE clears it. Returns
// ErrTaskInvalidTransition for an invalid target status, and ErrTaskNotFound
// when the message has no task row. On success the returned ChatMessage is
// re-read with TaskInfo populated.
func (s *Store) UpdateTaskStatus(ctx context.Context, msgID uuid.UUID, target int16) (*ChatMessage, error) {
	if target < TaskStatusTodo || target > TaskStatusDone {
		return nil, ErrTaskInvalidTransition
	}
	var stmt string
	switch target {
	case TaskStatusDone:
		stmt = `UPDATE task SET status = $1, completed_at = now(), updated_at = now()
			WHERE message_id = $2`
	default:
		stmt = `UPDATE task SET status = $1, completed_at = NULL, updated_at = now()
			WHERE message_id = $2`
	}
	res, err := s.GetDB().ExecContext(ctx, stmt, target, msgID)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to update task status")
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read task status update result")
	}
	if rows == 0 {
		return nil, ErrTaskNotFound
	}
	return s.GetTaskMessage(ctx, msgID)
}

// AssignTask assigns a task to a channel member (user or agent). A user
// assignee is a display-only "owner" and does not participate in the
// claim/process flow; an agent assignee is the working owner. The target must
// be a member of the task's conversation. Returns ErrTaskNotFound when the
// message has no task row, and ErrTaskAssigneeNotMember when the target is not
// a conversation member. On success the returned ChatMessage is re-read with
// TaskInfo populated.
func (s *Store) AssignTask(ctx context.Context, msgID, convID uuid.UUID, memberType int32, memberID string) (*ChatMessage, error) {
	// Resolve the target member to a principal/agent id and validate membership
	// in one step. memberID is the member's stable handle (user handle or agent
	// resource id).
	var assigneeType int16
	var agentID, userID sql.NullInt32
	switch memberType {
	case MemberTypeAgent:
		var id int
		if err := s.GetDB().QueryRowContext(ctx, `
			SELECT a.id FROM agent a
			JOIN conversation_member_meta cm ON cm.member_type = $1 AND cm.member_id = a.resource_id
			WHERE a.resource_id = $2 AND cm.conversation_id = $3
		`, MemberTypeAgent, memberID, convID).Scan(&id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, ErrTaskAssigneeNotMember
			}
			return nil, errors.Wrapf(err, "failed to resolve agent assignee")
		}
		assigneeType = 2
		agentID = sql.NullInt32{Int32: int32(id), Valid: true}
	case MemberTypeUser:
		var id int
		if err := s.GetDB().QueryRowContext(ctx, `
			SELECT p.id FROM principal p
			JOIN conversation_member_meta cm ON cm.member_type = $1 AND cm.member_id = p.handle
			WHERE p.handle = $2 AND cm.conversation_id = $3
		`, MemberTypeUser, memberID, convID).Scan(&id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, ErrTaskAssigneeNotMember
			}
			return nil, errors.Wrapf(err, "failed to resolve user assignee")
		}
		assigneeType = 1
		userID = sql.NullInt32{Int32: int32(id), Valid: true}
	default:
		return nil, ErrTaskAssigneeNotMember
	}

	res, err := s.GetDB().ExecContext(ctx, `
		UPDATE task
		   SET assignee_type = $1, assignee_agent_id = $2, assignee_user_id = $3, updated_at = now()
		 WHERE message_id = $4 AND conversation_id = $5
	`, assigneeType, agentID, userID, msgID, convID)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to assign task")
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read assign result")
	}
	if rows == 0 {
		return nil, ErrTaskNotFound
	}
	return s.GetTaskMessage(ctx, msgID)
}

// CloseTask transitions any non-DONE task to DONE (terminal), setting
// completed_at. It is the user-facing close path (CloseTask RPC): unlike
// UpdateTaskStatus it does not require assignee ownership and accepts every
// open status (TODO / IN_PROGRESS / IN_REVIEW), so a channel member can close
// a task directly from the UI without going through the assignee. Closing an
// already-DONE task is idempotent: changed=false and the current message is
// returned unchanged. Returns ErrTaskNotFound when the message has no task
// row.
func (s *Store) CloseTask(ctx context.Context, msgID, convID uuid.UUID) (msg *ChatMessage, changed bool, err error) {
	res, err := s.GetDB().ExecContext(ctx, `
		UPDATE task SET status = $1, completed_at = now(), updated_at = now()
		WHERE message_id = $2 AND conversation_id = $3 AND status <> $1
	`, TaskStatusDone, msgID, convID)
	if err != nil {
		return nil, false, errors.Wrapf(err, "failed to close task")
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return nil, false, errors.Wrapf(err, "failed to read close task result")
	}
	// rows == 0 means the task was already DONE; either way the re-read returns
	// the current state. GetTaskMessage reports ErrTaskNotFound when no task
	// row exists; a task whose message lives in another conversation is treated
	// the same (the caller only has authorization for the requested one).
	msg, err = s.GetTaskMessage(ctx, msgID)
	if err != nil {
		return nil, false, err
	}
	if msg.ConversationID != convID {
		return nil, false, ErrTaskNotFound
	}
	return msg, rows > 0, nil
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

// ListTasks returns one page of the task board for a conversation: root
// messages that have a task row, ordered by task_number descending (newest
// first), with TaskInfo (and thread_reply_count) populated. statusFilter, when
// non-empty, restricts the result to the given statuses. Pagination is the same
// OFFSET-token model as ListActivities: pageToken is the string offset, pageSize
// is clamped to [1,100] with a default of 30. The returned nextToken is empty
// when this page is the last.
func (s *Store) ListTasks(ctx context.Context, convID uuid.UUID, statusFilter []int16, pageSize int, pageToken string) ([]*ChatMessage, string, error) {
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 30
	}
	offset, err := strconv.Atoi(pageToken)
	if err != nil || offset < 0 {
		offset = 0
	}

	args := []any{convID}
	where := ` AND cm.thread_root_message_id IS NULL`
	if len(statusFilter) > 0 {
		where += ` AND t.status = ANY($2)`
		args = append(args, statusFilter)
	}
	idx := len(args) + 1
	args = append(args, pageSize, offset)
	rows, err := s.GetDB().QueryContext(ctx, `SELECT `+chatMessageColumns+`
		FROM chat_message cm
		JOIN principal p ON p.id = cm.principal_id
		LEFT JOIN agent a ON a.id = cm.sender_agent_id
		JOIN task t ON t.message_id = cm.id
		WHERE cm.conversation_id = $1`+where+`
		ORDER BY t.task_number DESC
		LIMIT $`+itoa(idx)+` OFFSET $`+itoa(idx+1), args...)
	if err != nil {
		return nil, "", errors.Wrapf(err, "failed to list tasks")
	}
	defer rows.Close()
	var msgs []*ChatMessage
	for rows.Next() {
		msg, scanErr := scanChatMessageRow(rows)
		if scanErr != nil {
			return nil, "", scanErr
		}
		msgs = append(msgs, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, "", errors.Wrapf(err, "failed to iterate tasks")
	}
	if err := s.fillThreadReplyCounts(ctx, msgs); err != nil {
		return nil, "", err
	}
	if err := s.fillTaskInfo(ctx, msgs); err != nil {
		return nil, "", err
	}
	nextToken := ""
	if len(msgs) == pageSize {
		nextToken = strconv.Itoa(offset + pageSize)
	}
	return msgs, nextToken, nil
}

// ListTaskCounts returns per-status task totals for a conversation, so the task
// board summary stays accurate regardless of how many tasks the paginated list
// has loaded. One GROUP BY query covers all four non-unspecified statuses.
func (s *Store) ListTaskCounts(ctx context.Context, convID uuid.UUID) (todo, inProgress, inReview, done int32, err error) {
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT status, COUNT(*)::int FROM task WHERE conversation_id = $1 GROUP BY status
	`, convID)
	if err != nil {
		return 0, 0, 0, 0, errors.Wrapf(err, "failed to list task counts")
	}
	defer rows.Close()
	for rows.Next() {
		var status int16
		var count int32
		if err := rows.Scan(&status, &count); err != nil {
			return 0, 0, 0, 0, errors.Wrapf(err, "failed to scan task count")
		}
		switch status {
		case TaskStatusTodo:
			todo = count
		case TaskStatusInProgress:
			inProgress = count
		case TaskStatusInReview:
			inReview = count
		case TaskStatusDone:
			done = count
		default:
			// Unknown / UNSPECIFIED statuses are not surfaced in the summary.
		}
	}
	if err := rows.Err(); err != nil {
		return 0, 0, 0, 0, errors.Wrapf(err, "failed to iterate task counts")
	}
	return todo, inProgress, inReview, done, nil
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
		       t.assignee_user_id, t.assignee_type,
		       COALESCE(a.name, ''), COALESCE(a.resource_id, ''),
		       COALESCE(u.name, ''), COALESCE(u.handle, '')
		FROM task t
		LEFT JOIN agent a ON a.id = t.assignee_agent_id
		LEFT JOIN principal u ON u.id = t.assignee_user_id
		WHERE t.message_id = ANY($1)
	`, roots)
	if err != nil {
		return errors.Wrapf(err, "failed to query task info")
	}
	defer rows.Close()
	info := make(map[uuid.UUID]*TaskInfo, len(roots))
	for rows.Next() {
		var (
			msgID        uuid.UUID
			ti           TaskInfo
			agentID      sql.NullInt32
			userID       sql.NullInt32
			assigneeType sql.NullInt16
			agentName    string
			agentResID   string
			userName     string
			userHandle   string
		)
		if err := rows.Scan(&msgID, &ti.TaskNumber, &ti.Status, &agentID, &userID, &assigneeType, &agentName, &agentResID, &userName, &userHandle); err != nil {
			return errors.Wrapf(err, "failed to scan task info")
		}
		ti.AssigneeAgentID = agentID
		ti.AssigneeUserID = userID
		ti.AssigneeType = assigneeType.Int16
		// Surface the assignee name/resource id according to the current
		// assignee kind: agent for agent assignees, user for user assignees.
		switch ti.AssigneeType {
		case 2:
			ti.AssigneeName = agentName
			ti.AssigneeResourceID = agentResID
		case 1:
			ti.AssigneeName = userName
			ti.AssigneeResourceID = userHandle
		case 0:
			// assignee_type is NULL: either an unassigned task (no assignee
			// at all) or a legacy task assigned before the assignee_type
			// column existed (assignee_agent_id set but assignee_type not
			// backfilled). Fall back to the agent assignee when present so
			// legacy data still renders; a truly unassigned task leaves the
			// name/resource empty.
			if agentID.Valid {
				ti.AssigneeType = 2
				ti.AssigneeName = agentName
				ti.AssigneeResourceID = agentResID
			}
		default:
			return errors.New("Incorrect AssigneeType")
		}
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
