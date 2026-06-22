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

	MemberRoleOwner  int32 = 1
	MemberRoleMember int32 = 2
)

type ConversationMember struct {
	ConversationID uuid.UUID
	MemberType     int32
	MemberID       string
	MemberRole     int32
	JoinedAt       time.Time
}

func (s *Store) AddConversationMember(ctx context.Context, convID uuid.UUID, memberType int32, memberID string, role int32) error {
	_, err := s.GetDB().ExecContext(ctx, `
		INSERT INTO conversation_member (conversation_id, member_type, member_id, member_role)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (conversation_id, member_type, member_id) DO UPDATE SET member_role = $4
	`, convID, memberType, memberID, role)
	if err != nil {
		return errors.Wrapf(err, "failed to add conversation member")
	}
	return nil
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

func (s *Store) findDirectConversation(ctx context.Context, userPrincipalID int, agentResourceID string) (*ConversationMessage, error) {
	var conv ConversationMessage
	err := s.GetDB().QueryRowContext(ctx, `
		SELECT c.id, c.agent_id, c.title, c.type, c.created_by, c.owner_id, c.created_at, c.updated_at
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
		&conv.ID, &conv.AgentID, &conv.Title, &conv.Type, &conv.CreatedBy, &conv.OwnerID, &conv.CreatedAt, &conv.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrapf(err, "failed to find direct conversation")
	}
	return &conv, nil
}
