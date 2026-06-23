package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/encoding/protojson"

	v1pb "github.com/Ranxy/laelia/backend/generated-go/v1"
)

// HeldActionState enum values for the held_action.state column.
const (
	HeldActionStateHeld     int32 = 1
	HeldActionStateResolved int32 = 2
	HeldActionStateExpired  int32 = 3
)

// HeldAction stores a held agent action that could not be committed because
// the conversation had advanced between the agent's PullMessages and its
// SubmitAction.
type HeldAction struct {
	ID             uuid.UUID
	AgentID        int
	ConversationID uuid.UUID
	ActionJSON     string
	BaseVersion    int64
	CurrentVersion int64
	State          int32
	Resolution     sql.NullInt32
	CommandID      uuid.NullUUID
	CreatedAt      time.Time
	ResolvedAt     sql.NullTime
	ExpiresAt      time.Time
}

// CreateHeldAction persists a new held action in state=HELD. The caller
// supplies the parsed SubmitAction proto; it is marshalled to JSON for the
// action_json column.
func (s *Store) CreateHeldAction(ctx context.Context, agentID int, conversationID uuid.UUID, action *v1pb.SubmitAction, baseVersion, currentVersion int64) (*HeldAction, error) {
	actionJSON, err := protojson.Marshal(action)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to marshal submit action")
	}

	var ha HeldAction
	err = s.GetDB().QueryRowContext(ctx, `
		INSERT INTO held_action (agent_id, conversation_id, action_json, base_version, current_version, state)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, agent_id, conversation_id, action_json, base_version, current_version, state, resolution, command_id, created_at, resolved_at, expires_at
	`, agentID, conversationID, string(actionJSON), baseVersion, currentVersion, HeldActionStateHeld).Scan(
		&ha.ID, &ha.AgentID, &ha.ConversationID, &ha.ActionJSON, &ha.BaseVersion, &ha.CurrentVersion,
		&ha.State, &ha.Resolution, &ha.CommandID, &ha.CreatedAt, &ha.ResolvedAt, &ha.ExpiresAt,
	)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create held action")
	}
	return &ha, nil
}

// GetHeldActionsByAgent returns all held actions in state=HELD for an agent,
// ordered by creation time ascending. Used during agent reconnect to re-prompt
// the agent for resolution.
func (s *Store) GetHeldActionsByAgent(ctx context.Context, agentID int) ([]*HeldAction, error) {
	rows, err := s.GetDB().QueryContext(ctx, `
		SELECT id, agent_id, conversation_id, action_json, base_version, current_version, state, resolution, command_id, created_at, resolved_at, expires_at
		FROM held_action
		WHERE agent_id = $1 AND state = $2
		ORDER BY created_at ASC
	`, agentID, HeldActionStateHeld)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to query held actions")
	}
	defer rows.Close()

	var actions []*HeldAction
	for rows.Next() {
		var ha HeldAction
		if scanErr := rows.Scan(&ha.ID, &ha.AgentID, &ha.ConversationID, &ha.ActionJSON, &ha.BaseVersion, &ha.CurrentVersion,
			&ha.State, &ha.Resolution, &ha.CommandID, &ha.CreatedAt, &ha.ResolvedAt, &ha.ExpiresAt); scanErr != nil {
			return nil, errors.Wrapf(scanErr, "failed to scan held action")
		}
		actions = append(actions, &ha)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrapf(err, "failed to iterate held actions")
	}
	return actions, nil
}

// ResolveHeldAction updates a held action's state, resolution, and optional
// command_id. All resolution paths (REVISE, SEND_AS_IS, DISCARD, FORCE_SEND)
// go through this method.
func (s *Store) ResolveHeldAction(ctx context.Context, actionID uuid.UUID, resolution int32, commandID uuid.NullUUID) error {
	now := time.Now()
	_, err := s.GetDB().ExecContext(ctx, `
		UPDATE held_action
		   SET state = $1, resolution = $2, command_id = $3, resolved_at = $4
		 WHERE id = $5
	`, HeldActionStateResolved, resolution, commandID, now, actionID)
	if err != nil {
		return errors.Wrapf(err, "failed to resolve held action")
	}
	return nil
}

// ExpireHeldActions marks all held actions whose expires_at has passed as
// state=EXPIRED. This is called periodically by the dispatcher's timeout
// goroutine (every 1 minute). It returns the count of expired actions.
func (s *Store) ExpireHeldActions(ctx context.Context) (int64, error) {
	result, err := s.GetDB().ExecContext(ctx, `
		UPDATE held_action
		   SET state = $1
		 WHERE state = $2 AND expires_at < now()
	`, HeldActionStateExpired, HeldActionStateHeld)
	if err != nil {
		return 0, errors.Wrapf(err, "failed to expire held actions")
	}
	n, _ := result.RowsAffected()
	return n, nil
}
