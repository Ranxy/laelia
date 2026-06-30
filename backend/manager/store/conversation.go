package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"
)

type ConversationMessage struct {
	ID        uuid.UUID
	AgentID   sql.NullInt32
	Title     string
	Type      int32
	CreatedBy int
	OwnerID   int
	CreatedAt time.Time
	UpdatedAt time.Time
	Version   int64
}

// insertDirectConversationSQL creates a direct conversation, returning the row.
// ON CONFLICT DO NOTHING is backed by idx_conversation_dm_unique
// (unique on (agent_id, created_by) WHERE type = 1): when two callers race to
// open the same DM, only one INSERT returns a row; the other gets sql.ErrNoRows
// and re-reads the winning row instead of inserting a duplicate. Extracted as a
// named constant so TestGetOrCreateDirectConversationSQL can lock the
// race-free INSERT in place without a live database.
const insertDirectConversationSQL = `
	INSERT INTO conversation (agent_id, title, type, created_by, owner_id)
	VALUES ($1, '', 1, $2, $2)
	ON CONFLICT (agent_id, created_by) WHERE type = 1 DO NOTHING
	RETURNING id, agent_id, title, type, created_by, owner_id, created_at, updated_at, version
`

func (s *Store) GetOrCreateDirectConversation(ctx context.Context, agentID, principalID int) (*ConversationMessage, error) {
	agent, err := s.GetAgentResourceIDByID(ctx, agentID)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get agent resource ID")
	}
	if agent == "" {
		return nil, errors.Errorf("agent %d not found", agentID)
	}

	conv, err := s.findDirectConversation(ctx, principalID, agent)
	if err != nil {
		return nil, err
	}
	if conv != nil {
		return conv, nil
	}

	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var newConv ConversationMessage
	err = tx.QueryRowContext(ctx, insertDirectConversationSQL, agentID, principalID).Scan(
		&newConv.ID, &newConv.AgentID, &newConv.Title, &newConv.Type, &newConv.CreatedBy, &newConv.OwnerID, &newConv.CreatedAt, &newConv.UpdatedAt, &newConv.Version,
	)
	if err != nil {
		// ON CONFLICT DO NOTHING returns no row when another caller won the
		// race to create this DM. Roll back our (empty) tx and return the
		// winning row, which is now committed with its members.
		if errors.Is(err, sql.ErrNoRows) {
			return s.findDirectConversation(ctx, principalID, agent)
		}
		return nil, errors.Wrapf(err, "failed to insert conversation")
	}

	if err := addConversationMemberTx(ctx, tx, newConv.ID, MemberTypeUser, fmt.Sprintf("%d", principalID), MemberRoleOwner); err != nil {
		return nil, err
	}
	if err := addConversationMemberTx(ctx, tx, newConv.ID, MemberTypeAgent, agent, MemberRoleMember); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	// Seed the agent's per-channel cursor to the new conversation's version so
	// it starts caught up and only sees future messages. Seeding only on the
	// creation path is intentional: returning an existing conversation must not
	// re-seed (and thus skip) unread messages.
	if seedErr := s.SeedCursorOnJoin(ctx, agentID, newConv.ID); seedErr != nil {
		return nil, errors.Wrapf(seedErr, "failed to seed agent cursor for new direct conversation")
	}

	// Seed the user's read cursor too, so creating a DM does not mark its
	// (empty) history unread. Re-opening an existing DM takes the early-return
	// path above and is deliberately not re-seeded, preserving unread state.
	if seedErr := s.SeedUserReadCursorOnJoin(ctx, principalID, newConv.ID); seedErr != nil {
		return nil, errors.Wrapf(seedErr, "failed to seed user read cursor for new direct conversation")
	}

	return &newConv, nil
}

func (s *Store) GetConversation(ctx context.Context, id uuid.UUID) (*ConversationMessage, error) {
	var conv ConversationMessage
	err := s.GetDB().QueryRowContext(ctx, `
		SELECT id, agent_id, title, type, created_by, owner_id, created_at, updated_at, version
		FROM conversation
		WHERE id = $1
	`, id).Scan(
		&conv.ID, &conv.AgentID, &conv.Title, &conv.Type, &conv.CreatedBy, &conv.OwnerID, &conv.CreatedAt, &conv.UpdatedAt, &conv.Version,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.Errorf("conversation %s not found", id)
		}
		return nil, errors.Wrapf(err, "failed to get conversation")
	}
	return &conv, nil
}

func (s *Store) CreateChannel(ctx context.Context, title string, ownerID int) (*ConversationMessage, error) {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var conv ConversationMessage
	err = tx.QueryRowContext(ctx, `
		INSERT INTO conversation (title, type, created_by, owner_id)
		VALUES ($1, 2, $2, $2)
		RETURNING id, agent_id, title, type, created_by, owner_id, created_at, updated_at, version
	`, title, ownerID).Scan(
		&conv.ID, &conv.AgentID, &conv.Title, &conv.Type, &conv.CreatedBy, &conv.OwnerID, &conv.CreatedAt, &conv.UpdatedAt, &conv.Version,
	)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create channel")
	}

	if err := addConversationMemberTx(ctx, tx, conv.ID, MemberTypeUser, fmt.Sprintf("%d", ownerID), MemberRoleOwner); err != nil {
		return nil, err
	}

	// Seed the owner's read cursor to the new conversation's version so it
	// starts caught up and only future messages count as unread. Inside the tx
	// so a failure rolls back the seed with the conversation.
	if err := upsertUserReadCursorTx(ctx, tx, ownerID, conv.ID, conv.Version); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &conv, nil
}

func (s *Store) UpdateChannel(ctx context.Context, id uuid.UUID, title string) (*ConversationMessage, error) {
	var conv ConversationMessage
	err := s.GetDB().QueryRowContext(ctx, `
		UPDATE conversation SET title = $1, updated_at = now()
		WHERE id = $2
		RETURNING id, agent_id, title, type, created_by, owner_id, created_at, updated_at, version
	`, title, id).Scan(
		&conv.ID, &conv.AgentID, &conv.Title, &conv.Type, &conv.CreatedBy, &conv.OwnerID, &conv.CreatedAt, &conv.UpdatedAt, &conv.Version,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.Errorf("conversation %s not found", id)
		}
		return nil, errors.Wrapf(err, "failed to update channel")
	}
	return &conv, nil
}

func (s *Store) DeleteChannel(ctx context.Context, id uuid.UUID) error {
	_, err := s.GetDB().ExecContext(ctx, `DELETE FROM conversation WHERE id = $1`, id)
	if err != nil {
		return errors.Wrapf(err, "failed to delete channel")
	}
	return nil
}

func (s *Store) ListUserConversations(ctx context.Context, principalID int, limit, offset int) ([]*ConversationMessage, error) {
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT c.id, c.agent_id, c.title, c.type, c.created_by, c.owner_id, c.created_at, c.updated_at, c.version
		FROM conversation c
		JOIN conversation_member cm ON cm.conversation_id = c.id
		WHERE cm.member_type = $1 AND cm.member_id = $2
		ORDER BY c.updated_at DESC
		LIMIT $3 OFFSET $4
	`, MemberTypeUser, fmt.Sprintf("%d", principalID), limit, offset)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to list user conversations")
	}
	defer rows.Close()

	var convs []*ConversationMessage
	for rows.Next() {
		var conv ConversationMessage
		if err := rows.Scan(&conv.ID, &conv.AgentID, &conv.Title, &conv.Type, &conv.CreatedBy, &conv.OwnerID, &conv.CreatedAt, &conv.UpdatedAt, &conv.Version); err != nil {
			return nil, errors.Wrapf(err, "failed to scan conversation")
		}
		convs = append(convs, &conv)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrapf(err, "failed to iterate conversations")
	}

	return convs, nil
}

// UserConversation pairs a conversation with its per-user unread count, used to
// render the left-rail channel list with unread badges.
type UserConversation struct {
	Conversation ConversationMessage
	UnreadCount  int32
}

// ListUserConversationsWithUnread returns every conversation the user is a
// member of, ordered by updated_at DESC, together with the number of
// chat_message rows whose room_version is beyond the user's read cursor. A
// missing cursor row is treated as caught-up (COALESCE to conversation.version),
// mirroring agent_channel_cursor semantics, so a newly joined user does not see
// existing history as unread.
//
// Only main-channel messages (thread_root_message_id IS NULL) count toward the
// channel unread badge: thread replies are a side conversation whose
// unread/reply state is surfaced via the root's reply count, not the channel
// badge (see fillThreadReplyCounts). This mirrors the agent inbox's
// thread-aware relevance filter, so a thread reply never pings the left-rail
// badge for a user who has the channel open.
func (s *Store) ListUserConversationsWithUnread(ctx context.Context, principalID int, limit, offset int) ([]*UserConversation, error) {
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT c.id, c.agent_id, c.title, c.type, c.created_by, c.owner_id, c.created_at, c.updated_at, c.version,
		       COALESCE((
		         SELECT count(*)::int
		         FROM chat_message m
		         WHERE m.conversation_id = c.id
		           AND m.thread_root_message_id IS NULL
		           AND m.room_version > COALESCE(ucc.read_version, c.version)
		       ), 0)
		FROM conversation c
		JOIN conversation_member cm ON cm.conversation_id = c.id
		LEFT JOIN user_channel_cursor ucc ON ucc.principal_id = $3 AND ucc.conversation_id = c.id
		WHERE cm.member_type = $1 AND cm.member_id = $2
		ORDER BY c.updated_at DESC
		LIMIT $4 OFFSET $5
	`, MemberTypeUser, fmt.Sprintf("%d", principalID), principalID, limit, offset)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to list user conversations with unread")
	}
	defer rows.Close()

	var convs []*UserConversation
	for rows.Next() {
		var uc UserConversation
		conv := &uc.Conversation
		if err := rows.Scan(
			&conv.ID, &conv.AgentID, &conv.Title, &conv.Type, &conv.CreatedBy, &conv.OwnerID, &conv.CreatedAt, &conv.UpdatedAt, &conv.Version,
			&uc.UnreadCount,
		); err != nil {
			return nil, errors.Wrapf(err, "failed to scan user conversation")
		}
		convs = append(convs, &uc)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrapf(err, "failed to iterate user conversations")
	}

	return convs, nil
}

func (s *Store) GetConversationMemberCount(ctx context.Context, id uuid.UUID) (int, error) {
	var count int
	err := s.GetDB().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM conversation_member WHERE conversation_id = $1
	`, id).Scan(&count)
	if err != nil {
		return 0, errors.Wrapf(err, "failed to get member count")
	}
	return count, nil
}

func (s *Store) GetAgentResourceIDByID(ctx context.Context, agentID int) (string, error) {
	var resourceID string
	err := s.GetDB().QueryRowContext(ctx, `SELECT resource_id FROM agent WHERE id = $1`, agentID).Scan(&resourceID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", errors.Wrapf(err, "failed to get agent resource ID")
	}
	return resourceID, nil
}
