package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"
)

// ActivityCategory bit flags mirror the laelia.v1.ActivityCategory enum. Kept
// as untyped int32 constants on the store side so the persistence layer does not
// depend on the generated proto package, matching SenderType / TaskStatus /
// ReminderStatus. Values are bit flags so a single activity row can carry several
// categories OR-ed together in the categories column.
const (
	ActivityCategoryMention  int32 = 1
	ActivityCategoryTask     int32 = 2
	ActivityCategoryReminder int32 = 4
	ActivityCategoryThread   int32 = 8
	ActivityCategoryDirect   int32 = 16
)

// ActivityState mirrors the laelia.v1.ActivityState enum. UNREAD -> READ happens
// when MarkConversationRead advances the user's channel cursor past the message's
// room_version; DONE is an explicit MarkActivityDone action.
const (
	ActivityStateUnspecified int32 = 0
	ActivityStateUnread      int32 = 1
	ActivityStateRead        int32 = 2
	ActivityStateDone        int32 = 3
)

// ErrActivityNotFound is returned by MarkActivityDone when no row exists for the
// (principal_id, activity_key) pair. The API layer maps it to connect.CodeNotFound.
var ErrActivityNotFound = errors.New("activity not found")

// activityReadStateClause returns the WHERE fragment that filters an activity
// feed by read state. The states mirror ActivityState*:
//   - Unread: done=false AND read_at IS NULL  (the default product view)
//   - Read:   done=false AND read_at IS NOT NULL
//   - Done:   done=true
//   - Unspecified: done=false  (all visible, neither dismissed)
//
// Done takes precedence over read in the state derivation (see storeToV1Activity),
// so a Done row is excluded from every non-Done view by the done=false clause.
func activityReadStateClause(readState int32) string {
	switch readState {
	case ActivityStateUnread:
		return " AND a.done = false AND a.read_at IS NULL"
	case ActivityStateRead:
		return " AND a.done = false AND a.read_at IS NOT NULL"
	case ActivityStateDone:
		return " AND a.done = true"
	default: // Unspecified: all visible (not done).
		return " AND a.done = false"
	}
}

// Activity is one row of a user's per-user activity feed. The base columns are the
// per-user state (categories, read_at, done, done_at); the joined columns
// (Content, SenderType, SenderName) come from chat_message + principal + agent and
// are populated by ListActivities / MarkActivityDone so the handler can build the
// proto Activity (summary, sender_name, sender_type) without an extra round trip.
type Activity struct {
	PrincipalID         int
	ActivityKey         uuid.UUID
	MessageID           uuid.UUID
	ConversationID      uuid.UUID
	ThreadRootMessageID uuid.NullUUID
	Categories          int32
	RoomVersion         int64
	ReadAt              sql.NullTime
	Done                bool
	DoneAt              sql.NullTime
	CreatedAt           time.Time
	// Joined from chat_message + principal + agent for list/detail rendering.
	Content    string
	SenderType int32
	SenderName string
}

const activityColumns = `a.principal_id, a.activity_key, a.message_id, a.conversation_id,
       a.thread_root_message_id, a.categories, a.room_version, a.read_at, a.done,
       a.done_at, a.created_at,
       cm.content, cm.sender_type,
       CASE WHEN cm.sender_type = 2 THEN COALESCE(ag.name, '') ELSE COALESCE(p.name, '') END`

const activityFromJoin = `FROM activity a
JOIN chat_message cm ON cm.id = a.message_id
LEFT JOIN principal p ON p.id = cm.principal_id
LEFT JOIN agent ag ON ag.id = cm.sender_agent_id`

func scanActivityRow(row interface {
	Scan(dest ...any) error
}) (*Activity, error) {
	var a Activity
	if err := row.Scan(
		&a.PrincipalID, &a.ActivityKey, &a.MessageID, &a.ConversationID, &a.ThreadRootMessageID,
		&a.Categories, &a.RoomVersion, &a.ReadAt, &a.Done, &a.DoneAt, &a.CreatedAt,
		&a.Content, &a.SenderType, &a.SenderName,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrActivityNotFound
		}
		return nil, errors.Wrapf(err, "failed to scan activity")
	}
	return &a, nil
}

// upsertActivitySQL inserts or bumps one activity row for a single user. The row
// identity is (principal_id, activity_key): a MENTION row is keyed by the
// mentioning message_id; a TASK/REMINDER/THREAD row is keyed by the thread root,
// so the root and every later reply in that thread share one row. On conflict the
// row is bumped to the latest message: message_id / thread_root / room_version
// advance, categories are OR-merged, and created_at refreshes — but only when the
// incoming message is genuinely newer (room_version > the stored one). A newer
// message also re-surfaces the row as UNREAD (read_at/done/done_at cleared),
// including resurrecting a row the user had Marked Done, so a task with a new
// reply notifies again. An identical re-run (same room_version) is a no-op.
const upsertActivitySQL = `INSERT INTO activity (principal_id, activity_key, message_id, conversation_id,
    thread_root_message_id, categories, room_version, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, now())
ON CONFLICT (principal_id, activity_key) DO UPDATE
   SET message_id = EXCLUDED.message_id,
       thread_root_message_id = EXCLUDED.thread_root_message_id,
       room_version = EXCLUDED.room_version,
       categories = activity.categories | EXCLUDED.categories,
       created_at = CASE WHEN EXCLUDED.room_version > activity.room_version THEN now() ELSE activity.created_at END,
       read_at = CASE WHEN EXCLUDED.room_version > activity.room_version THEN NULL ELSE activity.read_at END,
       done = CASE WHEN EXCLUDED.room_version > activity.room_version THEN false ELSE activity.done END,
       done_at = CASE WHEN EXCLUDED.room_version > activity.room_version THEN NULL ELSE activity.done_at END`

// UpsertActivity inserts (or bumps) one activity row for a single user. The row
// is keyed by ActivityKey (the message id for mentions, the thread root for
// task/reminder/thread activity). Idempotent: re-running with the same key and
// room_version only OR-merges categories; a newer room_version bumps the row to
// the latest message and re-surfaces it as unread.
func (s *Store) UpsertActivity(ctx context.Context, a *Activity) error {
	_, err := s.GetDB().ExecContext(ctx, upsertActivitySQL,
		a.PrincipalID, a.ActivityKey, a.MessageID, a.ConversationID, a.ThreadRootMessageID,
		a.Categories, a.RoomVersion)
	if err != nil {
		return errors.Wrapf(err, "failed to upsert activity")
	}
	return nil
}

// ListActivities returns the authenticated user's activity feed, filtered by
// category (items whose categories intersect ANY requested flag) and read-state,
// ordered by created_at DESC with offset pagination (mirroring ListReminders).
//
// readState filters:
//   - Unspecified: all visible (done=false)
//   - Unread: done=false AND read_at IS NULL
//   - Read: done=false AND read_at IS NOT NULL
//   - Done: done=true
//
// categoryFilter: when non-empty, items whose (categories & mask) != 0, where mask
// is the OR of the requested flags. Empty = no category filter (all categories).
func (s *Store) ListActivities(ctx context.Context, principalID int, categoryFilter []int32, readState int32, pageSize int, pageToken string) ([]*Activity, string, error) {
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 50
	}
	offset, err := strconv.Atoi(pageToken)
	if err != nil || offset < 0 {
		offset = 0
	}

	args := []any{principalID}
	where := "WHERE a.principal_id = $1" + activityReadStateClause(readState)
	idx := 2
	var mask int32
	for _, c := range categoryFilter {
		mask |= c
	}
	if mask != 0 {
		where += " AND (a.categories & $" + itoa(idx) + ") <> 0"
		args = append(args, mask)
		idx++
	}
	args = append(args, pageSize, offset)
	query := `SELECT ` + activityColumns + `
		` + activityFromJoin + `
		` + where + `
		ORDER BY a.created_at DESC
		LIMIT $` + itoa(idx) + ` OFFSET $` + itoa(idx+1)

	rows, err := s.GetDB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, "", errors.Wrapf(err, "failed to list activities")
	}
	defer rows.Close()

	var activities []*Activity
	for rows.Next() {
		a, scanErr := scanActivityRow(rows)
		if scanErr != nil {
			return nil, "", scanErr
		}
		activities = append(activities, a)
	}
	if err := rows.Err(); err != nil {
		return nil, "", errors.Wrapf(err, "failed to iterate activities")
	}

	nextToken := ""
	if len(activities) == pageSize {
		nextToken = strconv.Itoa(offset + pageSize)
	}
	return activities, nextToken, nil
}

// markActivityDoneSQL is the CTE that flips one not-done activity row to DONE and
// re-joins chat_message/principal/agent in a single round trip. The UPDATE is
// scoped by principal_id (the owning user) and activity_key (the row identity —
// the message id for mentions, the thread root for task/reminder/thread rows)
// and done=false (idempotent — marking an already-done row affects 0 rows →
// ErrActivityNotFound). The plain UPDATE...RETURNING form cannot join the
// content/sender tables portably, so a CTE feeds the updated row into the outer
// SELECT.
const markActivityDoneSQL = `WITH updated AS (
	UPDATE activity
	   SET done = true, done_at = now()
	 WHERE principal_id = $1 AND activity_key = $2 AND done = false
	RETURNING *
)
SELECT ` + activityColumns + `
FROM updated a
JOIN chat_message cm ON cm.id = a.message_id
LEFT JOIN principal p ON p.id = cm.principal_id
LEFT JOIN agent ag ON ag.id = cm.sender_agent_id`

// MarkActivityDone marks one activity row DONE for the owning user and returns
// the updated row (with joined content/sender). Scopes by principal_id so a
// caller cannot mark another user's activity. Returns ErrActivityNotFound when
// no not-done row exists for (principalID, activityKey).
//
// The UPDATE...RETURNING form cannot join chat_message/principal/agent
// portably, so a CTE updates the row and the outer SELECT re-joins to fetch the
// content/sender columns in a single round trip.
func (s *Store) MarkActivityDone(ctx context.Context, principalID int, activityKey uuid.UUID) (*Activity, error) {
	row := s.GetDB().QueryRowContext(ctx, markActivityDoneSQL, principalID, activityKey)
	a, err := scanActivityRow(row)
	if err != nil {
		return nil, err
	}
	return a, nil
}

// markConversationActivitiesReadSQL flips all of a user's unread, not-done
// activity rows in one conversation whose room_version <= the read cursor to
// READ. The done=false guard means a row already dismissed via MarkActivityDone
// is never resurrected as READ; the room_version <= bound means reading a channel
// only marks activity at or below the cursor (a newer reply stays UNREAD).
const markConversationActivitiesReadSQL = `UPDATE activity
   SET read_at = now()
 WHERE principal_id = $1
   AND conversation_id = $2
   AND read_at IS NULL
   AND done = false
   AND room_version <= $3`

// MarkConversationActivitiesRead marks all of the user's unread activity rows in
// a conversation whose room_version <= upToVersion as READ (read_at = now).
// Called by MarkConversationRead after advancing user_channel_cursor. Idempotent.
func (s *Store) MarkConversationActivitiesRead(ctx context.Context, principalID int, convID uuid.UUID, upToVersion int64) error {
	_, err := s.GetDB().ExecContext(ctx, markConversationActivitiesReadSQL, principalID, convID, upToVersion)
	if err != nil {
		return errors.Wrapf(err, "failed to mark conversation activities read")
	}
	return nil
}

// GenerateActivityForMessage computes the target-user set and category flags for
// a freshly inserted message and writes activity rows, fire-and-forget on a
// background goroutine. It is the single entry point shared by the API message
// handlers (SendMessage, PostMessage, CreateTask, the reminder lifecycle
// handlers) and the scheduler (reminder miss), so activity generation lives in
// the store layer to avoid a circular dependency from the scheduler back into
// the API service.
//
// The work is best-effort: a missed activity row is a missed notification, not
// data corruption, so it must NOT block the message-send critical path. It is
// therefore dispatched on a background goroutine with a detached context
// (context.Background), mirroring the audit interceptor's fire-and-forget write —
// the caller's request ctx may be cancelled as soon as the handler returns, so
// the request ctx cannot be used here. See generateActivityRows for the
// targeting/folding contract.
func (s *Store) GenerateActivityForMessage(msg *ChatMessage, rootIsTask, rootIsReminder bool) {
	go func() {
		s.generateActivityRows(context.Background(), msg, rootIsTask, rootIsReminder)
	}()
}

// activityUpsert is one activity row to write for a target user.
type activityUpsert struct {
	uid     int
	key     uuid.UUID
	message uuid.UUID
	root    uuid.NullUUID
	cats    int32
}

// planActivityUpserts computes the rows to upsert for the given category maps.
// Plain top-level messages in a 1:1 DM fold into a single per-chat row keyed
// by the conversation id, so the activity opens the whole chat. Thread-rooted
// messages (task/reminder roots and every thread reply, including in DMs) fold
// under the thread root instead, so the activity opens the thread where the
// follow-up work actually happens. Mentions inside a thread are merged into
// that folded row, so one task conversation stays one Activity record.
func planActivityUpserts(isDM, inThread bool, messageID, conversationID, effectiveRoot uuid.UUID, mentionCats, threadCats map[int]int32) []activityUpsert {
	// Union mention-only users into threadCats so every targeted user is
	// visited exactly once by the emit loops below.
	for uid := range mentionCats {
		if _, ok := threadCats[uid]; ok {
			continue
		}
		threadCats[uid] = 0
	}

	if isDM && !inThread {
		// A 1:1 DM's plain top-level messages are one continuous chat, not a
		// stream of independent notifications: fold every category for a user
		// into ONE row keyed by the conversation id, bumped to the latest
		// message. The row carries no thread_root so the activity opens the
		// whole chat (scrolled to the user's last-read position), not a single
		// message.
		targets := make([]activityUpsert, 0, len(threadCats))
		for uid, tc := range threadCats {
			cats := tc | mentionCats[uid]
			if cats == 0 {
				continue
			}
			targets = append(targets, activityUpsert{
				uid: uid, key: conversationID, message: messageID, cats: cats,
			})
		}
		return targets
	}

	targets := make([]activityUpsert, 0, len(threadCats))
	for uid, tc := range threadCats {
		mc := mentionCats[uid]
		if inThread {
			// Folded thread row keyed by the root. Any @mention on the root or
			// a reply is merged into this row instead of creating a separate
			// MENTION record, so a task/thread stays a single activity.
			targets = append(targets, activityUpsert{
				uid: uid, key: effectiveRoot, message: messageID,
				root: uuid.NullUUID{UUID: effectiveRoot, Valid: true}, cats: tc | mc,
			})
		} else if mc != 0 {
			// No thread: a top-level mention is its own row keyed by the message.
			targets = append(targets, activityUpsert{
				uid: uid, key: messageID, message: messageID, cats: mc,
			})
		}
	}
	return targets
}

// excludeSenderFromActivity removes the sender from the per-user category maps
// so a user never gets a self-notification. It drops MENTION (self-mention,
// already filtered upstream) and THREAD (the sender's own reply) entirely, but
// keeps TASK/REMINDER: the creator of a task/reminder must see it in their own
// activity feed to track it, mirroring agent-created tasks where the owner sees
// the task because the agent sender is never in the user sets.
func excludeSenderFromActivity(mentionCats, threadCats map[int]int32, senderID int) {
	delete(mentionCats, senderID)
	if cats, ok := threadCats[senderID]; ok {
		cats &^= ActivityCategoryThread
		if cats == 0 {
			delete(threadCats, senderID)
		} else {
			threadCats[senderID] = cats
		}
	}
}

// generateActivityRows is the synchronous body of GenerateActivityForMessage.
// See GenerateActivityForMessage for the dispatch policy and the doc below for
// the targeting/folding contract.
//
// Best-effort: every failure is logged and never propagates — a missed activity
// row is a missed notification, not data corruption, mirroring the wake/notify
// helpers. The sender never gets activity for its own message.
//
// Row identity (activity_key) — the core folding rule:
//   - A top-level MENTION is keyed by the mentioning message_id. Each top-level
//     @mention is its own precise pointer and is never folded across messages.
//   - TASK/REMINDER/THREAD is keyed by the thread root, so the root and every
//     later reply in that thread share ONE row bumped to the latest message (a
//     task/reminder "is" its thread — the follow-up work happens there).
//
// A @mention inside a thread (on the root or on a reply) is merged into the
// thread's folded row (MENTION|TASK|REMINDER|THREAD), so a task/thread stays a
// single Activity record rather than spawning a second MENTION row.
//
// Category targeting:
//   - MENTION:  each user mentioned in msg.Mentions (type=="user")
//   - TASK:     every user member of the conversation, when rootIsTask
//   - REMINDER: every user member of the conversation, when rootIsReminder
//   - THREAD:  every user_thread_participant of the thread, when msg is a reply
func (s *Store) generateActivityRows(ctx context.Context, msg *ChatMessage, rootIsTask, rootIsReminder bool) {
	members, err := s.ListConversationMembers(ctx, msg.ConversationID)
	if err != nil {
		slog.Warn("failed to list conversation members for activity",
			"conversationID", msg.ConversationID, "messageID", msg.ID, "error", err)
		return
	}
	userMembers := make(map[int]bool)
	for _, m := range members {
		if m.MemberType != MemberTypeUser {
			continue
		}
		user, err := s.GetUserByHandle(ctx, m.MemberID)
		if err != nil || user == nil {
			continue
		}
		userMembers[user.ID] = true
	}

	// Resolve the conversation once: its type decides whether this is a 1:1 DM,
	// which folds every category into a single per-chat row (see the emit loop
	// below), and whether a top-level message is a DIRECT message addressed to
	// the peer even with no @mention.
	conv, err := s.GetConversation(ctx, msg.ConversationID)
	if err != nil {
		slog.Warn("failed to get conversation for activity",
			"conversationID", msg.ConversationID, "messageID", msg.ID, "error", err)
		return
	}
	isDM := conv.Type == ConversationTypeDM || conv.Type == ConversationTypeUserDM

	// mentionCats (MENTION) are emitted as their own rows; threadCats
	// (TASK|REMINDER|THREAD) fold under the thread root. DIRECT (1:1 DMs) is
	// folded into mentionCats for plain top-level messages only; task/reminder
	// roots and thread replies get their own thread-rooted rows below.
	mentionCats := make(map[int]int32)
	for _, mn := range msg.Mentions {
		if mn.Type != "user" || mn.Id == "" {
			continue
		}
		user, err := s.GetUserByHandle(ctx, mn.Id)
		if err != nil || user == nil {
			continue
		}
		mentionCats[user.ID] |= ActivityCategoryMention
	}
	// DIRECT: a top-level plain message in a 1:1 DM (user<->user type=4 or
	// user<->agent type=1) is addressed to the other user member even with no
	// @mention. Without this, plain DM replies produce no activity row and the
	// recipient gets no notification. Thread replies inside a DM still ride the
	// THREAD category, and a task/reminder root is its own thread workspace, so
	// neither adds DIRECT here. Agent senders are never in userMembers, so the
	// user owner is targeted directly.
	if !msg.ThreadRootMessageID.Valid && !rootIsTask && !rootIsReminder && isDM {
		for uid := range userMembers {
			mentionCats[uid] |= ActivityCategoryDirect
		}
	}
	threadCats := make(map[int]int32)
	if rootIsTask {
		for uid := range userMembers {
			threadCats[uid] |= ActivityCategoryTask
		}
	}
	if rootIsReminder {
		for uid := range userMembers {
			threadCats[uid] |= ActivityCategoryReminder
		}
	}
	if msg.ThreadRootMessageID.Valid {
		participants, err := s.ListUserThreadParticipants(ctx, msg.ThreadRootMessageID.UUID)
		if err != nil {
			slog.Warn("failed to list user thread participants for activity",
				"threadRoot", msg.ThreadRootMessageID.UUID, "messageID", msg.ID, "error", err)
		} else {
			for _, uid := range participants {
				threadCats[uid] |= ActivityCategoryThread
			}
		}
	}
	// Exclude the sender from self-notifications. This applies only to
	// USER-sent messages: their PrincipalID is the sender's own principal id.
	// An AGENT/SYSTEM message carries the conversation owner (or the system
	// bot) as PrincipalID — NOT the agent sender — and the owner IS a
	// conversation member, so deleting by PrincipalID there would wrongly drop
	// the owner from TASK/REMINDER/THREAD activity for agent replies in their
	// own conversations. The agent sender is never in the user sets above (only
	// type=="user" mentions and user members/participants are), so there is
	// nothing to exclude for agent/system messages.
	//
	// A user is excluded from MENTION (self-mention, already dropped upstream)
	// and THREAD (their own reply), but NOT from TASK/REMINDER: the creator of
	// a task/reminder must see it in their own activity feed to track it,
	// mirroring agent-created tasks where the owner sees the task because the
	// agent sender is never in the user sets.
	if msg.SenderType == SenderTypeUser {
		excludeSenderFromActivity(mentionCats, threadCats, msg.PrincipalID)
	}

	// effectiveRoot is the thread this message belongs to, for folding and for the
	// mention row's thread_root (so an in-thread mention opens the thread). A
	// reply uses its thread root; a task/reminder root (top-level) is its own
	// thread root; a standalone top-level message has none.
	var effectiveRoot uuid.UUID
	inThread := false
	if msg.ThreadRootMessageID.Valid {
		effectiveRoot = msg.ThreadRootMessageID.UUID
		inThread = true
	} else if rootIsTask || rootIsReminder {
		effectiveRoot = msg.ID
		inThread = true
	}

	// pushTargets collects the (user, categories) pairs for which an activity
	// row was upserted, so a single detached goroutine can fan Web Push
	// notifications out after the emit loops (rather than one goroutine per
	// row). The webpush sender owns its own per-subscription fan-out pool.
	var pushTargets []pushTarget
	upsert := func(uid int, key, messageID uuid.UUID, root uuid.NullUUID, cats int32) {
		if cats == 0 {
			return
		}
		if err := s.UpsertActivity(ctx, &Activity{
			PrincipalID:         uid,
			ActivityKey:         key,
			MessageID:           messageID,
			ConversationID:      msg.ConversationID,
			ThreadRootMessageID: root,
			Categories:          cats,
			RoomVersion:         msg.RoomVersion,
		}); err != nil {
			slog.Warn("failed to upsert activity",
				"principalID", uid, "activityKey", key, "messageID", messageID, "error", err)
			return
		}
		if s.webPushSender != nil {
			pushTargets = append(pushTargets, pushTarget{uid: uid, cats: cats})
		}
	}

	// Emit rows for every user with any category. The planner keeps plain DM
	// messages folded per chat, but gives task/reminder/thread messages a
	// thread-rooted row so Activity can open the thread (not just the channel);
	// in-thread mentions merge into that row.
	for _, target := range planActivityUpserts(isDM, inThread, msg.ID, msg.ConversationID, effectiveRoot, mentionCats, threadCats) {
		upsert(target.uid, target.key, target.message, target.root, target.cats)
	}

	// Fan Web Push notifications out to every targeted user on a single detached
	// goroutine, mirroring GenerateActivityForMessage's fire-and-forget policy.
	// The request ctx may be cancelled by the time this runs, so use a detached
	// context; the sender's per-subscription fan-out is bounded internally. A
	// missed push is not data corruption, so failures are logged inside the
	// sender and never propagate.
	if s.webPushSender != nil && len(pushTargets) > 0 {
		sender := s.webPushSender
		targets := pushTargets
		go func() {
			ctx := context.Background()
			for _, t := range targets {
				if payload := buildPushPayload(msg, t.cats); payload != nil {
					sender.SendToUser(ctx, t.uid, payload)
				}
			}
		}()
	}
}

// pushTarget pairs a targeted user with the activity categories that made the
// message relevant to them, so the push payload's title can reflect the reason.
type pushTarget struct {
	uid  int
	cats int32
}

// maxPushSummaryLen caps the message excerpt embedded in a push payload body.
// Web Push payloads must stay under 4078 bytes (RFC 8291); the JSON envelope
// leaves ample headroom well below this cap.
const maxPushSummaryLen = 200

// pushPayload is the JSON contract between the manager and the frontend service
// worker (see frontend/public/sw.js). The SW shows {title}/{body} and uses
// {route} to deep-link on click; {conversation} is the notification tag so
// repeats in the same conversation coalesce.
type pushPayload struct {
	Title        string `json:"title"`
	Body         string `json:"body"`
	Conversation string `json:"conversation"`
	MessageID    string `json:"messageId"`
	Category     string `json:"category"`
	Route        string `json:"route"`
}

// buildPushPayload marshals the Web Push notification body for one targeted
// user. The title reflects the highest-priority category in cats; the body is
// "{sender}: {summary}". Returns nil when no recognizable category is set
// (nothing to push). Best-effort: a marshal error is logged and returns nil.
func buildPushPayload(msg *ChatMessage, cats int32) []byte {
	category := pushCategoryName(cats)
	if category == "" {
		return nil
	}
	sender := pushSenderName(msg)
	summary := truncatePushSummary(msg.Content)
	body := strings.TrimSpace(sender + " " + summary)
	if body == "" {
		body = category
	}
	payload := pushPayload{
		Title:        pushTitleFor(cats),
		Body:         body,
		Conversation: "conversations/" + msg.ConversationID.String(),
		MessageID:    msg.ID.String(),
		Category:     category,
		Route:        "/" + msg.ConversationID.String(),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		slog.Warn("failed to marshal push payload", "messageID", msg.ID, "error", err)
		return nil
	}
	return data
}

// pushCategoryName maps the highest-priority category flag set in cats to the
// name the service worker uses for analytics/tagging. Empty when no category is
// set. Priority order is MENTION > DIRECT > TASK > REMINDER > THREAD so a
// mention-on-task-thread reports as MENTION.
func pushCategoryName(cats int32) string {
	switch {
	case cats&ActivityCategoryMention != 0:
		return "MENTION"
	case cats&ActivityCategoryDirect != 0:
		return "DIRECT"
	case cats&ActivityCategoryTask != 0:
		return "TASK"
	case cats&ActivityCategoryReminder != 0:
		return "REMINDER"
	case cats&ActivityCategoryThread != 0:
		return "THREAD"
	}
	return ""
}

// pushTitleFor returns a human notification title for the highest-priority
// category in cats.
func pushTitleFor(cats int32) string {
	switch {
	case cats&ActivityCategoryMention != 0:
		return "You were mentioned"
	case cats&ActivityCategoryDirect != 0:
		return "New direct message"
	case cats&ActivityCategoryTask != 0:
		return "Task update"
	case cats&ActivityCategoryReminder != 0:
		return "Reminder update"
	case cats&ActivityCategoryThread != 0:
		return "New thread reply"
	}
	return "New message"
}

// pushSenderName returns the display name of the message sender for the push
// body prefix.
func pushSenderName(msg *ChatMessage) string {
	switch msg.SenderType {
	case SenderTypeUser:
		return msg.PrincipalName
	case SenderTypeAgent:
		return msg.AgentName
	}
	return ""
}

// truncatePushSummary collapses a message body to a single-line excerpt of at
// most maxPushSummaryLen runes for the push notification body.
func truncatePushSummary(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= maxPushSummaryLen {
		return s
	}
	return string(r[:maxPushSummaryLen]) + "…"
}
