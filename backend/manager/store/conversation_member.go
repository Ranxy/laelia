package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"
)

const (
	MemberTypeUser  int32 = 1
	MemberTypeAgent int32 = 2

	// Chat roles stored in conversation_member.member_role. conversation_member
	// is the single source of truth for chat authorization: the IAM engine maps
	// a caller's role here to its conversation permissions (see chatRolePermissions
	// in component/iam). conversation.owner_id is a denormalized owner-of-record
	// pointer (principal_id source for agent replies + API display + SystemBot for
	// agent-DMs) kept in sync with MemberRoleOwner on create/transfer; it is not
	// consulted for chat authorization.
	MemberRoleOwner  int32 = 1
	MemberRoleMember int32 = 2
	MemberRoleAdmin  int32 = 3
)

type ConversationMember struct {
	ConversationID uuid.UUID
	MemberType     int32
	MemberID       string
	MemberRole     int32
	JoinedAt       time.Time
}

// ConversationMemberFilter identifies a caller whose conversation membership
// restricts a list query: a non-admin user only sees conversations (and their
// reminders) they belong to. A nil filter means "no membership restriction" and
// is used for workspace admins and agent callers (an agent is inherently a
// member of its own conversations).
type ConversationMemberFilter struct {
	MemberType int32
	MemberID   string
}

// ConversationMemberInput identifies a member to add to a conversation.
type ConversationMemberInput struct {
	MemberType int32
	MemberID   string
}

// AddConversationMembers inserts several members into a conversation in one
// transaction, so a batch add is all-or-nothing: a failure mid-list rolls back
// every insertion. The caller is responsible for validating each member
// (ownership/existence/already-a-member) beforehand; this only persists.
func (s *Store) AddConversationMembers(ctx context.Context, convID uuid.UUID, members []ConversationMemberInput) error {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return errors.Wrap(err, "failed to begin add members transaction")
	}
	defer tx.Rollback()
	for _, m := range members {
		if err := addConversationMemberTx(ctx, tx, convID, m.MemberType, m.MemberID, MemberRoleMember); err != nil {
			return err
		}
	}
	return errors.Wrap(tx.Commit(), "failed to commit add members transaction")
}

func addConversationMemberTx(ctx context.Context, tx *sql.Tx, convID uuid.UUID, memberType int32, memberID string, role int32) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO conversation_member (conversation_id, member_type, member_id, member_role)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (conversation_id, member_type, member_id) DO UPDATE SET member_role = $4
	`, convID, memberType, memberID, role)
	if err != nil {
		return errors.Wrapf(err, "failed to add conversation member")
	}
	return nil
}

func (s *Store) RemoveConversationMember(ctx context.Context, convID uuid.UUID, memberType int32, memberID string) error {
	_, err := s.GetDB().ExecContext(ctx, `
		DELETE FROM conversation_member
		WHERE conversation_id = $1 AND member_type = $2 AND member_id = $3
	`, convID, memberType, memberID)
	if err != nil {
		return errors.Wrapf(err, "failed to remove conversation member")
	}
	return nil
}

func (s *Store) ListConversationMembers(ctx context.Context, convID uuid.UUID) ([]*ConversationMember, error) {
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT conversation_id, member_type, member_id, member_role, joined_at
		FROM conversation_member
		WHERE conversation_id = $1
		ORDER BY joined_at ASC
	`, convID)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to list conversation members")
	}
	defer rows.Close()

	var members []*ConversationMember
	for rows.Next() {
		var m ConversationMember
		if err := rows.Scan(&m.ConversationID, &m.MemberType, &m.MemberID, &m.MemberRole, &m.JoinedAt); err != nil {
			return nil, errors.Wrapf(err, "failed to scan conversation member")
		}
		members = append(members, &m)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrapf(err, "failed to iterate conversation members")
	}

	return members, nil
}

func (s *Store) IsConversationMember(ctx context.Context, convID uuid.UUID, memberType int32, memberID string) (bool, error) {
	var exists bool
	err := s.GetDB().QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM conversation_member
			WHERE conversation_id = $1 AND member_type = $2 AND member_id = $3
		)
	`, convID, memberType, memberID).Scan(&exists)
	if err != nil {
		return false, errors.Wrapf(err, "failed to check conversation membership")
	}
	return exists, nil
}

// GetConversationMembership returns the caller's chat role for a conversation
// (MemberRoleOwner/MemberRoleAdmin/MemberRoleMember, or 0 when not a member)
// together with the conversation type, in a single query. The IAM engine uses
// the role to map a caller's chat role to its conversation permissions and the
// type to apply the agent-DM review override.
// getConversationMembershipSQL loads a caller's chat role (member_role) and the
// conversation type in one query. It LEFT JOINs conversation_member so a
// non-member caller still yields a row — with NULL member_role — rather than
// ErrNoRows; GetConversationMembership scans that NULL into sql.NullInt32 (0
// when not a member). An INNER JOIN here would make every non-member check
// return ErrNoRows and surface a 404/500 instead of the intended 403.
const getConversationMembershipSQL = `
	SELECT cm.member_role, c.type
	FROM conversation c
	LEFT JOIN conversation_member cm
	       ON cm.conversation_id = c.id AND cm.member_type = $2 AND cm.member_id = $3
	WHERE c.id = $1
`

func (s *Store) GetConversationMembership(ctx context.Context, convID uuid.UUID, memberType int32, memberID string) (role int32, convType int32, err error) {
	// member_role is scanned into sql.NullInt32 because the LEFT JOIN yields NULL
	// for a caller who is not a member of the conversation — scanning that NULL
	// into a bare int32 returns "converting NULL to int32 is unsupported", which
	// would surface as a 500 (and, worse, make the non-member reviewAgentDM
	// override unreachable). NullInt32.Int32 is 0 when Invalid, matching the
	// "0 when not a member" contract the engine relies on. c.type is NOT NULL, so
	// it scans into int32 directly; a missing conversation surfaces as ErrNoRows.
	var roleNull sql.NullInt32
	err = s.GetDB().QueryRowContext(ctx, getConversationMembershipSQL, convID, memberType, memberID).Scan(&roleNull, &convType)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, 0, errors.Wrapf(err, "conversation %s not found", convID)
		}
		return 0, 0, errors.Wrapf(err, "failed to get conversation membership")
	}
	return roleNull.Int32, convType, nil
}

// UpdateConversationMemberRole sets a member's chat role. It is the store
// primitive behind grant/revoke-admin (Member<->Admin) and the role swap in
// TransferChannelOwnership (the tx-scoped sibling updateConversationMemberRoleTx
// runs inside the caller's transaction for atomicity). Returns
// ErrConversationMemberNotFound when the target is not a member.
var ErrConversationMemberNotFound = errors.New("conversation member not found")

type execRunner interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func execUpdateConversationMemberRole(ctx context.Context, runner execRunner, convID uuid.UUID, memberType int32, memberID string, role int32) error {
	res, err := runner.ExecContext(ctx, `
		UPDATE conversation_member SET member_role = $4
		WHERE conversation_id = $1 AND member_type = $2 AND member_id = $3
	`, convID, memberType, memberID, role)
	if err != nil {
		return errors.Wrapf(err, "failed to update conversation member role")
	}
	n, err := res.RowsAffected()
	if err != nil {
		return errors.Wrapf(err, "failed to read conversation member role update result")
	}
	if n == 0 {
		return ErrConversationMemberNotFound
	}
	return nil
}

func (s *Store) UpdateConversationMemberRole(ctx context.Context, convID uuid.UUID, memberType int32, memberID string, role int32) error {
	return execUpdateConversationMemberRole(ctx, s.GetDB(), convID, memberType, memberID, role)
}

// SetConversationPinned sets or clears the requesting user's per-conversation
// pin. pinned_at is stamped on pin (drives stable most-recently-pinned-first
// ordering within the pinned group) and cleared to NULL on unpin. Returns
// ErrConversationMemberNotFound when the user is not a member. Per-user by the
// conversation_member PK; only the caller's own row is touched.
func (s *Store) SetConversationPinned(ctx context.Context, convID uuid.UUID, principalID int, pinned bool) error {
	var pinnedAt any
	if pinned {
		pinnedAt = time.Now()
	}
	res, err := s.GetDB().ExecContext(ctx, `
		UPDATE conversation_member SET pinned = $4, pinned_at = $5
		WHERE conversation_id = $1 AND member_type = $2 AND member_id = $3
	`, convID, MemberTypeUser, fmt.Sprintf("%d", principalID), pinned, pinnedAt)
	if err != nil {
		return errors.Wrapf(err, "failed to set conversation pinned")
	}
	n, err := res.RowsAffected()
	if err != nil {
		return errors.Wrapf(err, "failed to read conversation pinned update result")
	}
	if n == 0 {
		return ErrConversationMemberNotFound
	}
	return nil
}

// GetConversationPinned returns the requesting user's per-conversation pin
// state. A missing membership row yields false (not a member / not pinned).
func (s *Store) GetConversationPinned(ctx context.Context, convID uuid.UUID, principalID int) (bool, error) {
	var pinned bool
	err := s.GetDB().QueryRowContext(ctx, `
		SELECT cm.pinned FROM conversation_member cm
		WHERE cm.conversation_id = $1 AND cm.member_type = $2 AND cm.member_id = $3
	`, convID, MemberTypeUser, fmt.Sprintf("%d", principalID)).Scan(&pinned)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, errors.Wrapf(err, "failed to get conversation pinned")
	}
	return pinned, nil
}

// TransferChannelOwnership atomically hands channel ownership from the old
// owner (a user, identified by principal id) to a new owner: it updates the
// denormalized conversation.owner_id, demotes the old owner to Member, and
// promotes the new owner to Owner, all in one transaction so a crash cannot
// leave a channel with two owners or none. The new owner must already be a
// member (verified by the caller); newOwnerID is its member_id string and
// newOwnerPrincipalID is the user principal id written to owner_id.
func (s *Store) TransferChannelOwnership(ctx context.Context, convID uuid.UUID, oldOwnerPrincipalID, newOwnerPrincipalID int, newOwnerID string) error {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return errors.Wrap(err, "failed to begin transfer ownership transaction")
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		UPDATE conversation SET owner_id = $1, updated_at = now()
		WHERE id = $2
	`, newOwnerPrincipalID, convID); err != nil {
		return errors.Wrap(err, "failed to update conversation owner_id")
	}

	oldOwnerID := fmt.Sprintf("%d", oldOwnerPrincipalID)
	if err := updateConversationMemberRoleTx(ctx, tx, convID, MemberTypeUser, oldOwnerID, MemberRoleMember); err != nil {
		return errors.Wrap(err, "failed to demote old owner")
	}
	if err := updateConversationMemberRoleTx(ctx, tx, convID, MemberTypeUser, newOwnerID, MemberRoleOwner); err != nil {
		return errors.Wrap(err, "failed to promote new owner")
	}

	if err := tx.Commit(); err != nil {
		return errors.Wrap(err, "failed to commit transfer ownership transaction")
	}
	return nil
}

func updateConversationMemberRoleTx(ctx context.Context, tx *sql.Tx, convID uuid.UUID, memberType int32, memberID string, role int32) error {
	return execUpdateConversationMemberRole(ctx, tx, convID, memberType, memberID, role)
}

func (s *Store) findDirectConversation(ctx context.Context, userPrincipalID int, agentResourceID string) (*ConversationMessage, error) {
	var conv ConversationMessage
	err := s.GetDB().QueryRowContext(ctx, `
		SELECT c.id, c.agent_id, c.title, c.type, c.created_by, c.owner_id, c.created_at, c.updated_at, c.version
		FROM conversation c
		WHERE c.id IN (
			SELECT cmu.conversation_id
			FROM conversation_member cmu
			WHERE cmu.member_type = $1 AND cmu.member_id = $2
			INTERSECT
			SELECT cma.conversation_id
			FROM conversation_member cma
			WHERE cma.member_type = $3 AND cma.member_id = $4
		)
		AND c.type = 1
		LIMIT 1
	`, MemberTypeUser, fmt.Sprintf("%d", userPrincipalID), MemberTypeAgent, agentResourceID).Scan(
		&conv.ID, &conv.AgentID, &conv.Title, &conv.Type, &conv.CreatedBy, &conv.OwnerID, &conv.CreatedAt, &conv.UpdatedAt, &conv.Version,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrapf(err, "failed to find direct conversation")
	}
	return &conv, nil
}
