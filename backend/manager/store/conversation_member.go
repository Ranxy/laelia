package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"
	exprpb "google.golang.org/genproto/googleapis/type/expr"
)

const (
	MemberTypeUser  int32 = 1
	MemberTypeAgent int32 = 2

	// Chat roles. Since the conversation IAM migration they are expressed as
	// IAM binding roles ("roles/conversationOwner" etc.) on the conversation's
	// policy row; these int values are the API/store-facing chat role values
	// kept for the existing handler and engine contracts.
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
	// ExpireAt, when set, makes the membership temporary: the policy binding
	// carries a request.time < expire condition.
	ExpireAt *time.Time
}

// AddConversationMembers inserts several members into a conversation in one
// transaction, so a batch add is all-or-nothing: a failure mid-list rolls back
// every change. The caller is responsible for validating each member
// (ownership/existence/already-a-member) beforehand; this only persists. Each
// member is written to the conversation IAM policy (authorization source) and
// to conversation_member_meta (join time / pin state / list index).
func (s *Store) AddConversationMembers(ctx context.Context, convID uuid.UUID, members []ConversationMemberInput) error {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return errors.Wrap(err, "failed to begin add members transaction")
	}
	defer tx.Rollback()
	for _, m := range members {
		if err := addConversationMemberTx(ctx, tx, convID, m.MemberType, m.MemberID, MemberRoleMember, m.ExpireAt); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return errors.Wrap(err, "failed to commit add members transaction")
	}
	s.invalidateConversationPolicyCache(convID)
	return nil
}

func addConversationMemberTx(ctx context.Context, tx *sql.Tx, convID uuid.UUID, memberType int32, memberID string, role int32, expireAt *time.Time) error {
	condition := memberExpirationCondition(expireAt)
	if err := addConversationMemberWithConditionTx(ctx, tx, convID, memberType, memberID, conversationRoleName(role), condition); err != nil {
		return err
	}
	return upsertConversationMemberMetaTx(ctx, tx, convID, memberType, memberID)
}

// memberExpirationCondition builds the binding condition for a temporary
// membership: request.time < timestamp("<expire>"). A nil expireAt yields nil.
func memberExpirationCondition(expireAt *time.Time) *exprpb.Expr {
	if expireAt == nil {
		return nil
	}
	return &exprpb.Expr{
		Expression: fmt.Sprintf(`request.time < timestamp(%q)`, expireAt.UTC().Format(time.RFC3339)),
	}
}

func (s *Store) RemoveConversationMember(ctx context.Context, convID uuid.UUID, memberType int32, memberID string) error {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return errors.Wrap(err, "failed to begin remove member transaction")
	}
	defer tx.Rollback()
	if err := patchConversationMemberRolesTx(ctx, tx, convID, memberType, memberID, nil); err != nil {
		return err
	}
	if err := deleteConversationMemberMetaTx(ctx, tx, convID, memberType, memberID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return errors.Wrap(err, "failed to commit remove member transaction")
	}
	s.invalidateConversationPolicyCache(convID)
	return nil
}

func (s *Store) ListConversationMembers(ctx context.Context, convID uuid.UUID) ([]*ConversationMember, error) {
	policy, err := s.GetConversationIamPolicy(ctx, convID)
	if err != nil {
		return nil, err
	}
	roleByMember := map[string]int32{}
	for _, b := range policy.Policy.GetBindings() {
		if r := conversationRoleFromName(b.GetRole()); r != 0 {
			for _, m := range b.GetMembers() {
				roleByMember[m] = r
			}
		}
	}

	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT conversation_id, member_type, member_id, joined_at
		FROM conversation_member_meta
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
		if err := rows.Scan(&m.ConversationID, &m.MemberType, &m.MemberID, &m.JoinedAt); err != nil {
			return nil, errors.Wrapf(err, "failed to scan conversation member")
		}
		m.MemberRole = roleByMember[conversationMemberName(m.MemberType, m.MemberID)]
		members = append(members, &m)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrapf(err, "failed to iterate conversation members")
	}
	return members, nil
}

// policyContainsMember reports whether the conversation IAM policy binds the
// member (any role, including custom roles).
func policyContainsMember(policy *IamPolicyMessage, member string) bool {
	for _, b := range policy.Policy.GetBindings() {
		for _, m := range b.GetMembers() {
			if m == member {
				return true
			}
		}
	}
	return false
}

func (s *Store) IsConversationMember(ctx context.Context, convID uuid.UUID, memberType int32, memberID string) (bool, error) {
	policy, err := s.GetConversationIamPolicy(ctx, convID)
	if err != nil {
		return false, err
	}
	return policyContainsMember(policy, conversationMemberName(memberType, memberID)), nil
}

// GetConversationMembership returns the caller's chat role for a conversation
// (MemberRoleOwner/MemberRoleAdmin/MemberRoleMember, or 0 when not a member)
// together with the conversation type. The IAM engine uses the role to map a
// caller's chat role to its conversation permissions and the type to apply the
// agent-DM review override. Membership is read from the conversation IAM
// policy; a caller bound only by a custom role is not a chat member (role 0).
func (s *Store) GetConversationMembership(ctx context.Context, convID uuid.UUID, memberType int32, memberID string) (role int32, convType int32, err error) {
	var convTypeVal int32
	err = s.GetDB().QueryRowContext(ctx, `SELECT type FROM conversation WHERE id = $1`, convID).Scan(&convTypeVal)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, 0, errors.Wrapf(err, "conversation %s not found", convID)
		}
		return 0, 0, errors.Wrapf(err, "failed to get conversation membership")
	}
	policy, err := s.GetConversationIamPolicy(ctx, convID)
	if err != nil {
		return 0, 0, err
	}
	member := conversationMemberName(memberType, memberID)
	for _, b := range policy.Policy.GetBindings() {
		r := conversationRoleFromName(b.GetRole())
		if r == 0 {
			continue
		}
		for _, m := range b.GetMembers() {
			if m == member {
				return r, convTypeVal, nil
			}
		}
	}
	return 0, convTypeVal, nil
}

// ErrConversationMemberNotFound is returned by role/pin updates when the target
// is not a member of the conversation.
var ErrConversationMemberNotFound = errors.New("conversation member not found")

// UpdateConversationMemberRole sets a member's chat role. It is the store
// primitive behind grant/revoke-admin (Member<->Admin) and the role swap in
// TransferChannelOwnership. Returns ErrConversationMemberNotFound when the
// target is not a member.
func (s *Store) UpdateConversationMemberRole(ctx context.Context, convID uuid.UUID, memberType int32, memberID string, role int32) error {
	tx, err := s.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return errors.Wrap(err, "failed to begin update member role transaction")
	}
	defer tx.Rollback()

	resource := conversationPolicyResource(convID)
	policy, err := getConversationPolicyForUpdate(ctx, tx, resource)
	if err != nil {
		return err
	}
	member := conversationMemberName(memberType, memberID)
	if !policyContainsMember(&IamPolicyMessage{Policy: policy}, member) {
		return ErrConversationMemberNotFound
	}
	applyIamPolicyPatch(policy, &PatchIamPolicyMessage{Member: member, Roles: []string{conversationRoleName(role)}})
	if err := upsertConversationPolicyTx(ctx, tx, resource, policy); err != nil {
		return err
	}
	if err := upsertConversationMemberMetaTx(ctx, tx, convID, memberType, memberID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return errors.Wrap(err, "failed to commit update member role transaction")
	}
	s.invalidateConversationPolicyCache(convID)
	return nil
}

// SetConversationPinned sets or clears the requesting user's per-conversation
// pin. pinned_at is stamped on pin (drives stable most-recently-pinned-first
// ordering within the pinned group) and cleared to NULL on unpin. Returns
// ErrConversationMemberNotFound when the user is not a member. Pin state is
// per-user UI metadata, stored in conversation_member_meta — it is not
// authorization data.
func (s *Store) SetConversationPinned(ctx context.Context, convID uuid.UUID, principalID int, pinned bool) error {
	var pinnedAt any
	if pinned {
		pinnedAt = time.Now()
	}
	res, err := s.GetDB().ExecContext(ctx, `
		UPDATE conversation_member_meta SET pinned = $4, pinned_at = $5
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
		SELECT pinned FROM conversation_member_meta
		WHERE conversation_id = $1 AND member_type = $2 AND member_id = $3
	`, convID, MemberTypeUser, fmt.Sprintf("%d", principalID)).Scan(&pinned)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, errors.Wrapf(err, "failed to get conversation pinned")
	}
	return pinned, nil
}

// SetConversationClosed sets or clears the requesting user's per-conversation
// close state. closed_at is stamped on close and cleared to NULL on reopen.
// Closing hides the conversation from the user's left-rail list; the first new
// main-channel message (thread replies excluded) clears the flag for every
// member, so the conversation reappears automatically. Returns
// ErrConversationMemberNotFound when the user is not a member. Close state is
// per-user UI metadata, stored in conversation_member_meta — it is not
// authorization data.
func (s *Store) SetConversationClosed(ctx context.Context, convID uuid.UUID, principalID int, closed bool) error {
	var closedAt any
	if closed {
		closedAt = time.Now()
	}
	res, err := s.GetDB().ExecContext(ctx, `
		UPDATE conversation_member_meta SET closed = $4, closed_at = $5
		WHERE conversation_id = $1 AND member_type = $2 AND member_id = $3
	`, convID, MemberTypeUser, fmt.Sprintf("%d", principalID), closed, closedAt)
	if err != nil {
		return errors.Wrapf(err, "failed to set conversation closed")
	}
	n, err := res.RowsAffected()
	if err != nil {
		return errors.Wrapf(err, "failed to read conversation closed update result")
	}
	if n == 0 {
		return ErrConversationMemberNotFound
	}
	return nil
}

// GetConversationClosed returns the requesting user's per-conversation close
// state. A missing membership row yields false (not a member / not closed).
func (s *Store) GetConversationClosed(ctx context.Context, convID uuid.UUID, principalID int) (bool, error) {
	var closed bool
	err := s.GetDB().QueryRowContext(ctx, `
		SELECT closed FROM conversation_member_meta
		WHERE conversation_id = $1 AND member_type = $2 AND member_id = $3
	`, convID, MemberTypeUser, fmt.Sprintf("%d", principalID)).Scan(&closed)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, errors.Wrapf(err, "failed to get conversation closed")
	}
	return closed, nil
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
	if err := patchConversationMemberRolesTx(ctx, tx, convID, MemberTypeUser, oldOwnerID, []string{conversationRoleName(MemberRoleMember)}); err != nil {
		return errors.Wrap(err, "failed to demote old owner")
	}
	if err := patchConversationMemberRolesTx(ctx, tx, convID, MemberTypeUser, newOwnerID, []string{conversationRoleName(MemberRoleOwner)}); err != nil {
		return errors.Wrap(err, "failed to promote new owner")
	}
	if err := upsertConversationMemberMetaTx(ctx, tx, convID, MemberTypeUser, oldOwnerID); err != nil {
		return err
	}
	if err := upsertConversationMemberMetaTx(ctx, tx, convID, MemberTypeUser, newOwnerID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return errors.Wrap(err, "failed to commit transfer ownership transaction")
	}
	s.invalidateConversationPolicyCache(convID)
	return nil
}

func (s *Store) findDirectConversation(ctx context.Context, userPrincipalID int, agentResourceID string) (*ConversationMessage, error) {
	var conv ConversationMessage
	err := s.GetDB().QueryRowContext(ctx, `
		SELECT c.id, c.agent_id, c.title, c.type, c.created_by, c.owner_id, c.created_at, c.updated_at, c.version
		FROM conversation c
		WHERE c.id IN (
			SELECT cmu.conversation_id
			FROM conversation_member_meta cmu
			WHERE cmu.member_type = $1 AND cmu.member_id = $2
			INTERSECT
			SELECT cma.conversation_id
			FROM conversation_member_meta cma
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

// conversation_member_meta helpers. The meta table mirrors membership for
// list-index and per-user UI state; authorization reads the IAM policy.

func upsertConversationMemberMetaTx(ctx context.Context, tx *sql.Tx, convID uuid.UUID, memberType int32, memberID string) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO conversation_member_meta (conversation_id, member_type, member_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (conversation_id, member_type, member_id) DO NOTHING
	`, convID, memberType, memberID)
	if err != nil {
		return errors.Wrapf(err, "failed to upsert conversation member meta")
	}
	return nil
}

func deleteConversationMemberMetaTx(ctx context.Context, tx *sql.Tx, convID uuid.UUID, memberType int32, memberID string) error {
	_, err := tx.ExecContext(ctx, `
		DELETE FROM conversation_member_meta
		WHERE conversation_id = $1 AND member_type = $2 AND member_id = $3
	`, convID, memberType, memberID)
	if err != nil {
		return errors.Wrapf(err, "failed to delete conversation member meta")
	}
	return nil
}
