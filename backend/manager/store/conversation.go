package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"

	"github.com/Ranxy/laelia/backend/common"
)

// ErrConversationNotFound is returned by GetConversation when no row exists.
var ErrConversationNotFound = errors.New("conversation not found")

// Conversation type values, mirrored by the laelia.v1.ConversationType enum.
// 1 = DM (one user + one agent), 2 = channel (many members), 3 = AGENT_DM
// (exactly two agents, no users; owner of record is the SYSTEM_BOT principal),
// 4 = USER_DM (exactly two users, no agents; owner of record is the initiator).
const (
	ConversationTypeDM      int32 = 1
	ConversationTypeChannel int32 = 2
	ConversationTypeAgentDM int32 = 3
	ConversationTypeUserDM  int32 = 4
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

	if err := addConversationMemberTx(ctx, tx, newConv.ID, MemberTypeUser, fmt.Sprintf("%d", principalID), MemberRoleOwner, nil); err != nil {
		return nil, err
	}
	if err := addConversationMemberTx(ctx, tx, newConv.ID, MemberTypeAgent, agent, MemberRoleMember, nil); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	s.invalidateConversationPolicyCache(newConv.ID)

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

// insertAgentDMSQL creates a type-3 agent-DM, returning the row. The pair is
// ordered (lo < hi) by the caller, and idx_conversation_agent_dm_unique
// (partial WHERE type = 3) dedups races: when two callers race to open the same
// agent-DM, only one INSERT returns a row; the other gets sql.ErrNoRows and
// re-reads the winning row. Mirrors insertDirectConversationSQL for type-1 DMs.
const insertAgentDMSQL = `
	INSERT INTO conversation (agent_id, title, type, created_by, owner_id, agent_dm_a, agent_dm_b)
	VALUES (NULL, '', 3, $1, $1, $2, $3)
	ON CONFLICT (agent_dm_a, agent_dm_b) WHERE type = 3 DO NOTHING
	RETURNING id, agent_id, title, type, created_by, owner_id, created_at, updated_at, version
`

// findAgentDM looks up an existing type-3 agent-DM by the ordered agent-id
// pair via the dedup columns.
func (s *Store) findAgentDM(ctx context.Context, lo, hi int) (*ConversationMessage, error) {
	var conv ConversationMessage
	err := s.GetDB().QueryRowContext(ctx, `
		SELECT id, agent_id, title, type, created_by, owner_id, created_at, updated_at, version
		FROM conversation
		WHERE type = 3 AND agent_dm_a = $1 AND agent_dm_b = $2
	`, lo, hi).Scan(
		&conv.ID, &conv.AgentID, &conv.Title, &conv.Type, &conv.CreatedBy, &conv.OwnerID, &conv.CreatedAt, &conv.UpdatedAt, &conv.Version,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to find agent DM")
	}
	return &conv, nil
}

// GetOrCreateAgentDM returns the 1:1 DM between two agents, creating it if
// absent. It is order-independent (the pair is canonicalized to lo < hi) and
// race-free via idx_conversation_agent_dm_unique + ON CONFLICT DO NOTHING, then
// re-reading the winning row. The conversation is owned by the SYSTEM_BOT
// principal (no human owner); both agents are added as members and have their
// per-channel cursors seeded so only future messages surface. Mirrors
// GetOrCreateDirectConversation for type-1 user DMs.
func (s *Store) GetOrCreateAgentDM(ctx context.Context, agentAID, agentBID int) (*ConversationMessage, error) {
	if agentAID == agentBID {
		return nil, errors.Errorf("agent-DM requires two distinct agents (got %d twice)", agentAID)
	}

	resA, err := s.GetAgentResourceIDByID(ctx, agentAID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get agent A resource ID")
	}
	resB, err := s.GetAgentResourceIDByID(ctx, agentBID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get agent B resource ID")
	}
	if resA == "" {
		return nil, errors.Errorf("agent %d not found", agentAID)
	}
	if resB == "" {
		return nil, errors.Errorf("agent %d not found", agentBID)
	}

	lo, hi := agentAID, agentBID
	if lo > hi {
		lo, hi = hi, lo
	}

	if conv, err := s.findAgentDM(ctx, lo, hi); err != nil {
		return nil, err
	} else if conv != nil {
		return conv, nil
	}

	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var newConv ConversationMessage
	err = tx.QueryRowContext(ctx, insertAgentDMSQL, common.SystemBotID, lo, hi).Scan(
		&newConv.ID, &newConv.AgentID, &newConv.Title, &newConv.Type, &newConv.CreatedBy, &newConv.OwnerID, &newConv.CreatedAt, &newConv.UpdatedAt, &newConv.Version,
	)
	if err != nil {
		// ON CONFLICT DO NOTHING returns no row when another caller won the
		// race. Roll back the empty tx and return the winning row.
		if errors.Is(err, sql.ErrNoRows) {
			return s.findAgentDM(ctx, lo, hi)
		}
		return nil, errors.Wrap(err, "failed to insert agent DM")
	}

	if err := addConversationMemberTx(ctx, tx, newConv.ID, MemberTypeAgent, resA, MemberRoleMember, nil); err != nil {
		return nil, err
	}
	if err := addConversationMemberTx(ctx, tx, newConv.ID, MemberTypeAgent, resB, MemberRoleMember, nil); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	s.invalidateConversationPolicyCache(newConv.ID)

	// Seed both agents' cursors to the new conversation's version so they start
	// caught up and only see future messages. Seeding only on the create path
	// is intentional: returning an existing conversation must not re-seed.
	if seedErr := s.SeedCursorOnJoin(ctx, agentAID, newConv.ID); seedErr != nil {
		return nil, errors.Wrap(seedErr, "failed to seed agent A cursor for new agent DM")
	}
	if seedErr := s.SeedCursorOnJoin(ctx, agentBID, newConv.ID); seedErr != nil {
		return nil, errors.Wrap(seedErr, "failed to seed agent B cursor for new agent DM")
	}

	return &newConv, nil
}

// insertUserDMSQL creates a type-4 user-DM, returning the row. The pair is
// ordered (lo < hi) by the caller, and idx_conversation_user_dm_unique
// (partial WHERE type = 4) dedups races: when two callers race to open the same
// user-DM, only one INSERT returns a row; the other gets sql.ErrNoRows and
// re-reads the winning row. Mirrors insertAgentDMSQL for type-3 agent DMs.
// created_by/owner_id are the initiator (the store caller), satisfying the NOT
// NULL FKs; agent_id is NULL since a user-DM has no agent.
const insertUserDMSQL = `
	INSERT INTO conversation (agent_id, title, type, created_by, owner_id, user_dm_a, user_dm_b)
	VALUES (NULL, '', 4, $1, $1, $2, $3)
	ON CONFLICT (user_dm_a, user_dm_b) WHERE type = 4 DO NOTHING
	RETURNING id, agent_id, title, type, created_by, owner_id, created_at, updated_at, version
`

// findUserDM looks up an existing type-4 user-DM by the ordered principal-id
// pair via the dedup columns.
func (s *Store) findUserDM(ctx context.Context, lo, hi int) (*ConversationMessage, error) {
	var conv ConversationMessage
	err := s.GetDB().QueryRowContext(ctx, `
		SELECT id, agent_id, title, type, created_by, owner_id, created_at, updated_at, version
		FROM conversation
		WHERE type = 4 AND user_dm_a = $1 AND user_dm_b = $2
	`, lo, hi).Scan(
		&conv.ID, &conv.AgentID, &conv.Title, &conv.Type, &conv.CreatedBy, &conv.OwnerID, &conv.CreatedAt, &conv.UpdatedAt, &conv.Version,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to find user DM")
	}
	return &conv, nil
}

// GetOrCreateUserUserDM returns the 1:1 DM between two users, creating it if
// absent. callerID is the initiator (owner of record); peerID is the other
// user. It is order-independent for dedup (the pair is canonicalized to
// lo < hi) and race-free via idx_conversation_user_dm_unique + ON CONFLICT DO
// NOTHING, then re-reading the winning row. Both users are added as members
// and have their read cursors seeded so only future messages surface. Mirrors
// GetOrCreateAgentDM for type-3 agent DMs.
func (s *Store) GetOrCreateUserUserDM(ctx context.Context, callerID, peerID int) (*ConversationMessage, error) {
	if callerID == peerID {
		return nil, errors.Errorf("user-DM requires two distinct users (got %d twice)", callerID)
	}

	lo, hi := callerID, peerID
	if lo > hi {
		lo, hi = hi, lo
	}

	if conv, err := s.findUserDM(ctx, lo, hi); err != nil {
		return nil, err
	} else if conv != nil {
		return conv, nil
	}

	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var newConv ConversationMessage
	err = tx.QueryRowContext(ctx, insertUserDMSQL, callerID, lo, hi).Scan(
		&newConv.ID, &newConv.AgentID, &newConv.Title, &newConv.Type, &newConv.CreatedBy, &newConv.OwnerID, &newConv.CreatedAt, &newConv.UpdatedAt, &newConv.Version,
	)
	if err != nil {
		// ON CONFLICT DO NOTHING returns no row when another caller won the
		// race. Roll back the empty tx and return the winning row.
		if errors.Is(err, sql.ErrNoRows) {
			return s.findUserDM(ctx, lo, hi)
		}
		return nil, errors.Wrap(err, "failed to insert user DM")
	}

	if err := addConversationMemberTx(ctx, tx, newConv.ID, MemberTypeUser, fmt.Sprintf("%d", callerID), MemberRoleOwner, nil); err != nil {
		return nil, err
	}
	if err := addConversationMemberTx(ctx, tx, newConv.ID, MemberTypeUser, fmt.Sprintf("%d", peerID), MemberRoleMember, nil); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	s.invalidateConversationPolicyCache(newConv.ID)

	// Seed both users' read cursors to the new conversation's version so they
	// start caught up and only see future messages. Seeding only on the create
	// path is intentional: returning an existing conversation must not re-seed
	// (and thus skip) unread messages.
	if seedErr := s.SeedUserReadCursorOnJoin(ctx, callerID, newConv.ID); seedErr != nil {
		return nil, errors.Wrap(seedErr, "failed to seed caller read cursor for new user DM")
	}
	if seedErr := s.SeedUserReadCursorOnJoin(ctx, peerID, newConv.ID); seedErr != nil {
		return nil, errors.Wrap(seedErr, "failed to seed peer read cursor for new user DM")
	}

	return &newConv, nil
}

// FindChannelByTitle returns the unique type-2 channel with the given title
// (enforced unique by idx_conversation_channel_title_unique). Returns
// (nil, nil) when no such channel exists, so callers can map absence to a
// NOT_FOUND error. Powers the "#<title>" address resolver.
func (s *Store) FindChannelByTitle(ctx context.Context, title string) (*ConversationMessage, error) {
	var conv ConversationMessage
	err := s.GetDB().QueryRowContext(ctx, `
		SELECT id, agent_id, title, type, created_by, owner_id, created_at, updated_at, version
		FROM conversation
		WHERE type = 2 AND title = $1
	`, title).Scan(
		&conv.ID, &conv.AgentID, &conv.Title, &conv.Type, &conv.CreatedBy, &conv.OwnerID, &conv.CreatedAt, &conv.UpdatedAt, &conv.Version,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to find channel by title")
	}
	return &conv, nil
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
			return nil, errors.Wrapf(ErrConversationNotFound, "conversation %s", id)
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

	if err := addConversationMemberTx(ctx, tx, conv.ID, MemberTypeUser, fmt.Sprintf("%d", ownerID), MemberRoleOwner, nil); err != nil {
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
	s.invalidateConversationPolicyCache(conv.ID)

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
		JOIN conversation_member_meta cm ON cm.conversation_id = c.id
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
	// Pinned/PinnedAt are the requesting user's per-conversation pin state from
	// conversation_member_meta. Pinned items sort to the top of the list; PinnedAt
	// orders the pinned group (most-recently-pinned first), independent of
	// Conversation.UpdatedAt so pinned items don't drift as new messages arrive.
	Pinned   bool
	PinnedAt sql.NullTime
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
		       ), 0),
		       cm.pinned, cm.pinned_at
		FROM conversation c
		JOIN conversation_member_meta cm ON cm.conversation_id = c.id
		LEFT JOIN user_channel_cursor ucc ON ucc.principal_id = $3 AND ucc.conversation_id = c.id
		WHERE cm.member_type = $1 AND cm.member_id = $2
		ORDER BY cm.pinned DESC, cm.pinned_at DESC NULLS LAST, c.updated_at DESC
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
			&uc.Pinned, &uc.PinnedAt,
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

// ListAgentConversations returns every conversation the given agent is a member
// of, ordered by updated_at DESC. It mirrors ListUserConversationsWithUnread but
// binds conversation_member_meta to MemberTypeAgent + the agent's resource_id
// string
// (which is how agent memberships are stored, see findDirectConversation).
//
// When viewer is non-nil, results are further restricted to conversations the
// viewer is also a member of, so a non-admin user only sees the agent's
// channels they participate in (workspace admins pass a nil viewer to see all).
//
// The unread count is intentionally 0: agent_channel_cursor tracks the agent's
// own read position for its inbox, which is not meaningful to an admin viewing
// the agent's channel roster from the agent detail page.
func (s *Store) ListAgentConversations(ctx context.Context, agentResourceID string, viewer *ConversationMemberFilter, limit, offset int) ([]*UserConversation, error) {
	args := []any{MemberTypeAgent, agentResourceID}
	query := `
		SELECT c.id, c.agent_id, c.title, c.type, c.created_by, c.owner_id, c.created_at, c.updated_at, c.version
		FROM conversation c
		JOIN conversation_member_meta cm ON cm.conversation_id = c.id
		WHERE cm.member_type = $1 AND cm.member_id = $2`
	if viewer != nil {
		query += ` AND EXISTS (SELECT 1 FROM conversation_member_meta cmv WHERE cmv.conversation_id = c.id AND cmv.member_type = $3 AND cmv.member_id = $4)`
		args = append(args, viewer.MemberType, viewer.MemberID)
	}
	args = append(args, limit, offset)
	query += ` ORDER BY c.updated_at DESC LIMIT $` + itoa(len(args)-1) + ` OFFSET $` + itoa(len(args))

	rows, err := s.GetDB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to list agent conversations")
	}
	defer rows.Close()

	var convs []*UserConversation
	for rows.Next() {
		var uc UserConversation
		conv := &uc.Conversation
		if err := rows.Scan(
			&conv.ID, &conv.AgentID, &conv.Title, &conv.Type, &conv.CreatedBy, &conv.OwnerID, &conv.CreatedAt, &conv.UpdatedAt, &conv.Version,
		); err != nil {
			return nil, errors.Wrapf(err, "failed to scan agent conversation")
		}
		uc.UnreadCount = 0
		convs = append(convs, &uc)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrapf(err, "failed to iterate agent conversations")
	}
	return convs, nil
}

func (s *Store) GetConversationMemberCount(ctx context.Context, id uuid.UUID) (int, error) {
	var count int
	err := s.GetDB().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM conversation_member_meta WHERE conversation_id = $1
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
